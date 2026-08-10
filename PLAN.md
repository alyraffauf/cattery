# Cattery Implementation Plan

> **For future agents and humans:** This document is the product specification, architecture decision record, implementation roadmap, and acceptance contract for Cattery. Implement it task-by-task. Do not silently reinterpret settled behavior. If a requirement must change, update this document in the same change and explain why.

**Goal:** Build a safe, cross-platform dotfiles manager that stores literal files in a central repository, materializes ordinary files beneath `$HOME`, detects drift with SQLite-backed three-way reconciliation, supports literal platform overlays, protects file-level secrets with SOPS and age, and runs deterministic hooks.

**Architecture:** Cattery compiles a literal repository tree into a validated deployment plan. It compares each current source and target with the last successfully applied baseline, obtains explicit decisions for destructive or ambiguous cases, then executes atomic per-file updates while recording each completed mutation immediately. The repository remains the source of intent; SQLite is only local operational history.

**Tech stack:** Go 1.25.0+ with Go 1.26.x as the primary development toolchain, Cobra v1.10.2 as a thin CLI adapter, the Go standard library, `modernc.org/sqlite`, BLAKE3, `go-toml/v2`, `adrg/xdg`, `gofrs/flock`, `go-difflib`, `x/text`, `x/term`, and the installed `sops` CLI. Linux and macOS are supported initially.

**Tested runtime baselines:** GitHub's `ubuntu-22.04` x64 runner and `macos-15` runner, plus CGo-free cross-builds for Linux and Darwin on amd64 and arm64. The initial release makes no separate older-kernel, older-distribution, or pre-macOS-15 runtime claim that its available CI cannot execute.

---

## 1. Product definition

Cattery is a deliberately narrow dotfiles manager. It is not a complete declarative `$HOME` manager and does not try to own everything beneath the home directory.

Its defining behaviors are:

1. Source files live in a central, human-readable repository.
2. The normal target is a regular file copied beneath `$HOME`, not a symlink.
3. Applications may edit or atomically replace deployed files normally.
4. Cattery detects those edits later by comparing source, target, and the last applied baseline.
5. `cattery add` is the explicit operation that adopts target contents into the repository.
6. Optional groups organize files belonging to one application, integration, project, or function.
7. Literal `_darwin/` and `_linux/` overlays replace or add files for the current platform without templates.
8. File-level secrets are stored as SOPS-encrypted binary payloads and decrypted only in memory by commands that need secret semantics; `validate` never decrypts them.
9. Hooks provide general extensibility, including package installation, without a plugin or package-manager subsystem.
10. Symlinks are a narrow, explicit alias feature for alternate application lookup paths. They are never the primary deployment strategy.
11. Removing source does not authorize deleting a target. Cattery leaves that target in place and retires only its local tracking record.
12. Existing filesystem content is never silently destroyed.

### 1.1 Non-goals for the initial release

Do not implement any of the following unless this plan is explicitly revised:

- a daemon, file watcher, background synchronization service, or bidirectional automatic sync;
- templates, interpolation, patches, or a general expression language;
- GNU Stow-style symlink farms;
- automatic target deletion, pruning, generations, rollback, or full `$HOME` ownership;
- package-manager-specific operations for Nix, Homebrew, apt, or others;
- arbitrary destinations outside `$HOME`;
- a plugin system or embedded scripting runtime;
- broad ownership, ACL, extended-attribute, timestamp, or permission management;
- source filename attributes such as `private_`, `executable_`, or `dot_`;
- source symlink traversal;
- directory symlink aliases;
- Windows support in the initial release;
- a graphical interface;
- Git operations, repository hosting, or automatic commits.

### 1.2 Naming and terminology

- The project and executable are **Cattery** and `cattery`.
- A **repository** is the central source tree.
- A **group** is an optional top-level organizational unit. `group` is the public CLI and documentation term.
- The **root scope** consists of ungrouped source entries.
- A **canonical target** is the regular file Cattery materializes beneath `$HOME`.
- An **alias** is an explicitly declared symlink pointing to a canonical target.
- A **layer** is `base`, `darwin`, or `linux`.
- A **baseline** is the semantic content fingerprint last established by a successful `apply` or `add`.
- **Drift** means current target content or type differs from the baseline.
- A **conflict** means both source and target differ from the baseline and do not equal one another.

---

## 2. User-facing repository model

### 2.1 Root files and groups

Root-level regular files are ungrouped and deploy directly beneath `$HOME`:

```text
repo/
  .bashrc
  Brewfile
```

```text
repo/.bashrc  ->  $HOME/.bashrc
repo/Brewfile ->  $HOME/Brewfile
```

A top-level directory whose name starts with `.` is also an ungrouped HOME-relative tree:

```text
repo/
  .config/
    starship.toml
```

```text
repo/.config/starship.toml -> $HOME/.config/starship.toml
```

Every other ordinary top-level directory is a group:

```text
repo/
  atuin/
    .config/
      atuin/
        config.toml
      fish/
        conf.d/
          atuin-integration.fish
```

```text
repo/atuin/.config/atuin/config.toml
  -> $HOME/.config/atuin/config.toml

repo/atuin/.config/fish/conf.d/atuin-integration.fish
  -> $HOME/.config/fish/conf.d/atuin-integration.fish
```

This rule resolves the unavoidable ambiguity between a top-level group and a top-level target directory without requiring per-group metadata:

- root regular files: ungrouped targets;
- root dot-prefixed directories: ungrouped target trees;
- root non-dot, non-underscore directories: groups;
- root underscore-prefixed entries: control or ignored entries.

Consequences that must be documented rather than hidden:

- A non-dot target tree such as `Library/` or `bin/` must live inside a group, unless it appears inside a root platform overlay.
- Therefore an ungrouped source can represent a one-segment root file or a tree whose first segment begins with `.`, but it cannot directly represent `Library/...` or `bin/...`; put those paths in a group. A HOME-relative path whose first segment begins with `_` is intentionally unrepresentable because that namespace is reserved in every scope.
- Base-layer entries under the root scope's `_secrets/` obey that same root representability rule. For example, `_secrets/.config/app/token` is valid, while `_secrets/bin/token` requires a group or a root platform overlay. `_secrets/` changes storage kind, not root-path grammar.
- The repository-root metadata names in Section 2.3 are unrepresentable as base-layer root targets for both ordinary and secret storage; use a group or root platform overlay when one of those literal HOME targets is intentional.
- A group name cannot begin with `.` or `_`.
- A group name is one nonempty filesystem path segment: no slash, platform separator, `.` value, `..` value, or NUL. Spaces and Unicode are allowed. Names are case-sensitive at the CLI.
- Distinct group names that are equivalent under Section 6.3's NFC-plus-`EqualFold` comparison are a portability validation error even when their compiled target trees are disjoint.
- Groups exist only one level below the repository root; nested organization is ordinary target path structure.

### 2.2 Reserved control namespace

At the repository root and at each group root, names beginning with `_` are reserved from deployment.

Recognized controls are:

```text
_darwin/
_linux/
_secrets/
_hooks/
_routes.toml
```

Unknown underscore-prefixed files or directories are ignored recursively, are never deployed, and are not errors:

```text
_notes/
_experiments/
_README.md
```

The reservation applies only at a control-bearing scope root. An underscore-prefixed path inside an ordinary target tree is literal and deployable:

```text
repo/app/.config/app/_internal/value
  -> $HOME/.config/app/_internal/value
```

The grammar is exact:

- repository and group roots recognize `_darwin`, `_linux`, `_secrets`, `_hooks`, and `_routes.toml`;
- a platform-layer root recognizes `_secrets` and ignores every other leading-underscore entry;
- hooks and routes are scope-wide and cannot be platform-specific directories/files inside an overlay;
- after entering an ordinary target tree or a `_secrets` target tree, every child name is literal, including names beginning with `_`.

### 2.3 Repository metadata ignored at the root

The following repository-root entries are controls or version-control metadata and are never deployed:

```text
.git
.github
.gitignore
.gitattributes
.gitmodules
.sops.yaml
```

These entry names are ignored regardless of filesystem type and only at repository root; Cattery never follows them. This supports both normal Git directories and worktree `.git` files. A group may intentionally deploy a file named `.gitignore`.

Other root regular files are literal sources. For example, `README.md` would target `$HOME/README.md`; repository-only prose should be named `_README.md` or placed under `_notes/`.

### 2.4 Supported source filesystem entries

- Regular files are supported, including empty and binary files.
- Directories are structural and merged as needed.
- Source symlinks are rejected. Cattery must not follow them.
- FIFOs, sockets, devices, and other special entries are rejected.
- Hard-linked regular files are read as ordinary content and deployed independently.
- File timestamps, ownership, ACLs, and extended attributes are not copied.

---

## 3. Platform overlays

### 3.1 Layout

A root scope or group may contain `_darwin/` and `_linux/`:

```text
repo/
  ghostty/
    .config/
      ghostty/
        config
    _darwin/
      .config/
        ghostty/
          config
    _linux/
      .config/
        ghostty/
          config
```

Cattery uses `runtime.GOOS` and initially recognizes only `darwin` and `linux`. An unsupported runtime fails before hooks or mutations.

Repository validation compiles and collision-checks both Linux and Darwin plans regardless of the host OS. This catches inactive-overlay errors before they reach another machine. Apply/status/diff then use only the runtime's active plan. Route canonical ownership is checked per platform plan: an `all` route must be valid on both plans, while a platform route needs a canonical source only on its named platform.

### 3.2 Merge semantics

For each scope:

1. Compile the base tree.
2. Compile the matching platform tree, if present.
3. Overlay platform entries by HOME-relative target path.
4. The platform entry always wins when the same relative path appears on both sides.
5. The platform tree may add paths absent from base.

Structural replacement is literal:

- base file + platform file: platform file wins;
- base file + platform directory: platform directory and its descendants replace the base file;
- base directory + platform file: platform file wins and suppresses base descendants beneath that path;
- base directory + platform directory: recursively merge, with platform descendants winning.

The overlay is resolved within a scope before cross-scope collision detection. A deliberate base/platform replacement is not a collision.

### 3.3 No general templates

Platform overlays are whole literal files. There is no manager-level templating, patching, interpolation, conditional syntax, or variable expansion in the initial release.

---

## 4. Secrets

### 4.1 Layout and mapping

Secrets use `_secrets/` while preserving the target path literally:

```text
repo/
  atuin/
    _secrets/
      .config/
        atuin/
          token.env
```

```text
repo/atuin/_secrets/.config/atuin/token.env
  -> $HOME/.config/atuin/token.env
```

Platform-specific secrets compose by putting `_secrets/` inside the platform layer:

```text
repo/
  app/
    _secrets/
      .config/app/credentials
    _darwin/
      _secrets/
        .config/app/credentials
```

Rules:

- `_secrets/` at scope root contains base-layer secret sources.
- `_darwin/_secrets/` and `_linux/_secrets/` contain platform-layer secret sources.
- Root-scope secret candidates are rejected when their derived target is not representable by the active root layer under Section 2.1. Group secrets have no additional first-segment restriction.
- Normal and secret sources at the same path in the same layer are an error.
- A platform source may replace a base source regardless of whether either source is normal or secret. The selected source determines target secrecy and mode.

### 4.2 Storage format

Cattery treats every secret as an opaque binary file, even when the target happens to be YAML, JSON, ENV, or INI.

- Plaintext is encrypted through SOPS with `--input-type binary --output-type json`.
- Encrypted JSON is stored at the literal source path under `_secrets/`; there is no `.sops`, `.enc`, or other filename transformation.
- Decryption uses `--input-type json --output-type binary`.
- Byte-exact round trips take priority over exposing structured SOPS keys.

This avoids extension ambiguity and gives one deterministic representation for every file type.

### 4.3 SOPS and age policy boundary

Cattery shells out to the installed `sops` executable. It does not embed, reimplement, or vendor SOPS cryptography.

Encryption invocation conceptually is:

```text
sops encrypt \
  --filename-override <repository-relative-secret-source-path> \
  --input-type binary \
  --output-type json \
  /dev/stdin
```

Plaintext is sent through stdin; encrypted JSON is captured from stdout.

Before persisting encryption output, require nonempty valid JSON with `json.Valid`, then decrypt those exact candidate ciphertext bytes through SOPS and require a byte-exact match with the original plaintext. The candidate remains only in memory until this round trip succeeds; syntax-valid but undecryptable or semantically different output never replaces a repository source. Empty decrypted binary output is valid and represents an empty secret file.

Decryption invocation conceptually is:

```text
sops decrypt \
  --filename-override <repository-relative-secret-source-path> \
  --input-type json \
  --output-type binary \
  /dev/stdin
```

Every encryption/decryption supplies the exact caller-validated input bytes on stdin; Cattery never asks SOPS to reopen a mutable repository path. The filename override remains the safe repository-relative source path so `.sops.yaml` creation rules and type behavior see the intended name. `/dev/stdin` is required and is available on both supported Unix platforms. SOPS v3.13.3 with age v1.3.1 was exercised with this exact command shape for a payload containing NUL and invalid UTF-8.

Cattery runs SOPS with the repository root as its working directory. SOPS owns recipient selection, key discovery, age identity lookup, and `.sops.yaml` creation rules. Cattery documentation recommends age, but the adapter does not reject another SOPS-supported key backend.

Age private identities are an external bootstrap prerequisite and are never copied into Cattery configuration or state. A repository may manage a backup copy only if some other already-available identity can decrypt it; Cattery cannot bootstrap the sole key required to decrypt its own managed key file.

- A missing `sops` executable is checked only when a selected operation needs secret encryption or decryption. The error must name the missing dependency and leave repository/target files plus managed state rows untouched; only Section 8.5 operational state-store effects may already exist.

On a nonzero SOPS exit or cancellation, Cattery reports the operation, safe source path, and exit status, but returns neither partial stdout nor captured stderr to its caller. A subprocess can echo its input, so either stream can contain plaintext. The secrets adapter uses overflow-checked bounded writers: encryption stdout is limited to `2*len(plaintext)+1 MiB`, decryption stdout to `len(ciphertext)+1 MiB`, and stderr to 64 KiB while excess stderr is drained and discarded. Exceeding an stdout limit terminates the process group and returns a sanitized operational error. Keep captured bytes only in memory long enough to determine success/failure, zero their owned buffers best-effort, and discard them; `--verbose` does not weaken this rule.

### 4.4 Plaintext handling

- Plaintext may exist in Go memory while hashing, diff classification, encryption, or target writing.
- Plaintext is never stored in SQLite, logs, command-line arguments, error messages, or normal diff output.
- Cattery never writes plaintext to a general temporary directory.
- Atomic secret target writes may use a same-directory temporary file created with mode `0600`; it must be removed on every failure path.
- The SOPS adapter returns a caller-owned plaintext byte slice only after a successful exit. The caller must zero its owned slice immediately after semantic hashing or atomic writing and must not retain it in actions, state objects, errors, or caches. Go may still make unobservable copies, so this is exposure reduction rather than guaranteed erasure.
- Memory clearing is best-effort only because Go does not guarantee elimination of copies. Do not claim stronger guarantees.
- Secret content is never printed by `status`, `diff`, or interactive conflict prompts.

### 4.5 Secret permissions

- A non-executable secret target is always mode `0600`.
- An executable secret target is mode `0700`.
- These modes are enforced even when content is otherwise a no-op.
- The executable bit of the encrypted source file represents whether the decrypted target is executable.

---

## 5. Explicit symlink aliases

### 5.1 Purpose

A canonical source always materializes as one regular canonical target. Aliases exist only for applications that look for the same file at another platform-specific location, especially XDG versus macOS paths.

Aliases are not independent copies and never participate as another editable reconciliation source.

### 5.2 `_routes.toml` schema

`_routes.toml` is optional at repository root and at group root. If present, it must contain `version = 1` and only the schema below:

```toml
version = 1

[symlinks.all]
".config/example/config" = [
  ".example/config",
]

[symlinks.darwin]
".config/ghostty/config" = [
  "Library/Application Support/com.mitchellh.ghostty/config",
]

[symlinks.linux]
".config/example/config" = [
  ".local/share/example/config",
]
```

Semantics:

- A quoted table key is the canonical HOME-relative target path.
- Array values are HOME-relative alias destinations.
- `all`, `darwin`, and `linux` are the only accepted sections.
- The active plan takes the union of `all` and the current platform section, then sorts by alias destination.
- Repeating an alias destination anywhere in the active union is a validation error, even if both declarations name the same canonical target; Cattery does not use declaration order as override policy.
- Unknown fields, versions, or platform sections are validation errors.
- Paths use forward slashes in TOML and are converted with `filepath.FromSlash` only after validation.

### 5.3 Alias validation

- The canonical key must identify a managed regular file in the same scope after platform overlay resolution.
- Aliases may target files only, not directories.
- An alias destination cannot equal its canonical target.
- An alias destination cannot collide with a managed file, another alias, or a parent/child file conflict.
- Absolute paths, empty paths, `.` segments, `..`, and paths escaping `$HOME` are rejected.
- Cross-scope references are rejected; a group route owns only that group's canonical files.

### 5.4 Alias realization

The alias is a relative filesystem symlink from the alias parent directory to the canonical target. Relative links keep the home tree relocatable.

At apply time:

- missing alias: create it after canonical files are complete;
- existing symlink whose `os.Readlink` payload exactly equals the normalized relative path computed from the lexical alias parent to the canonical target: no-op;
- existing absolute symlink or any symlink with a different payload: drift, even when it happens to resolve to the canonical target;
- existing wrong symlink or regular file: prompt to replace, skip, or abort; confirmed replacement atomically renames a prepared relative symlink over the path entry and never follows the old referent;
- existing directory, whether empty or non-empty, or any special entry: error and require manual intervention;
- non-interactive mode: any unexpected existing path is unresolved drift and prevents hooks plus planned repository/target/managed state-row mutations.

Replacing an occupied alias path is never silent. Alias removal from `_routes.toml` does not delete the old symlink; its state row is retired and the filesystem entry remains.

An alias operation is eligible only when its canonical file is converged or will be made converged in the same resolved apply. If the user skips the canonical file, automatically skip its aliases and include them in the unresolved result; do not create a new lookup path to knowingly drifted canonical content.

---

## 6. Path safety and collision rules

### 6.1 Lexical validation

Every configured or derived destination must be a normalized HOME-relative path.

Reject:

- absolute paths;
- platform volume prefixes;
- empty paths;
- NUL bytes;
- strings that are not valid UTF-8;
- empty path segments;
- `.` or `..` segments;
- paths whose cleaned or joined result escapes the allowed root;
- alias or source paths that become the HOME root itself.

Do not silently clean unsafe input into a different accepted path. Report the original path and reason.

Repository source destinations created by `add` use the same containment algorithm with canonical repository root as the allowed root. An `add` operation must never escape the repository through a derived path or an existing symlinked parent.

Canonical repository root, canonical `$HOME`, and canonical state directory have these overlap rules:

- repository root may be disjoint from `$HOME` or a strict descendant of it, but it may not equal or be an ancestor of `$HOME`;
- state directory may be disjoint from `$HOME` or a strict descendant of it, but it may not equal or be an ancestor of `$HOME`;
- repository root and state directory must be disjoint: neither may equal or contain the other;
- when repository or state is beneath `$HOME`, reject every target or alias that equals or descends into that protected tree;
- reject any compiled source whose physical file is the same filesystem object as its intended target.

Evaluate every repository/HOME/state equality, ancestor, descendant, and protected-target relation twice: once with canonical native absolute path segments and once with Section 6.3's pairwise NFC-plus-`EqualFold` segment equivalence. Reject when either comparison overlaps. This intentionally rejects portable aliases such as case-only or NFC/NFD variants before creation, even on a case-sensitive host; never derive a lowercase absolute-path key.

These rules prevent self-application, source/target identity, and corruption of the live database, lock, or keyed-hash material.

### 6.2 Filesystem containment

Lexical containment is insufficient when an existing parent is a symlink. The initial release therefore rejects **every symlinked parent component** between canonical `$HOME` and a canonical target or alias, even when that symlink resolves elsewhere beneath the same home. Explicit final aliases from Section 5 are the only symlinks Cattery creates or accepts as managed representation.

Before inspecting and immediately before mutating a target or alias:

1. Resolve `$HOME` itself to its canonical existing path.
2. Walk each existing component from canonical `$HOME` through the destination parent with `os.Lstat`; every component must be a real directory, never a symlink or special entry.
3. If a suffix is absent, require its nearest existing ancestor to be a real directory; after `MkdirAll`, repeat the complete `Lstat` walk before preparing or renaming an entry.
4. Use `os.Lstat` on the final component and never follow a final target symlink as though it were a regular managed file.
5. Recheck the same parent identities and final-entry preconditions after before hooks and immediately before each rename or mode-only replacement.

This deliberately rejects homes that use paths such as `$HOME/.config -> other-directory`; users must materialize Cattery targets through real directories or choose a different canonical target plus an explicit final alias. The v1 threat model assumes no malicious same-UID process is swapping directories between adjacent syscalls; routine application races are detected by the repeated snapshots. Defending against an adversarial same-user swap would require descriptor-relative `openat` operations and is outside the initial release.

Resolve home once at command start with `os.UserHomeDir`, require an existing absolute directory, and canonicalize it with `filepath.EvalSymlinks`. There is no public `--home` override; tests inject home through application dependencies rather than mutating the developer's real environment.

Before prompts or hooks, run equality and parent/child collision checks over all canonical file and alias destinations and treat two existing final regular entries for which `os.SameFile` is true as a collision. The one exception is the intended identity between a declared alias and its own canonical target. Recheck parent-component identity with each target precondition before mutation.

### 6.3 Deployment collisions

After all overlays are resolved, the full repository is validated before group filtering or mutation.

- Two producers resolving to the same target file are an error, even if bytes match.
- A file at `a` and any producer beneath `a/...` are an error.
- Shared parent directories are valid and merged.
- Root scope and groups are subject to the same collision checks.
- Alias destinations participate in collision checks.
- Base/platform replacements inside one scope are resolved before this check and are not collisions.
- For repository portability, define path-segment equivalence by first normalizing each segment to Unicode NFC with `golang.org/x/text/unicode/norm`, then comparing the two normalized strings with Go's `strings.EqualFold`. Two complete paths collide when they have the same segment count and every corresponding segment is equivalent; parent/child checks use the same segment-prefix rule. Because `EqualFold` is a predicate rather than a canonicalizing transform, do not invent a lowercase “key”; pairwise comparison is acceptable for dotfile-scale plans. This deliberately rejects case-only and canonically equivalent NFC/NFD distinctions even on a Linux filesystem that could store both, preventing common APFS aliases before mutation.

Validation errors identify both source owners and the target path.

---

## 7. Permissions and atomic filesystem behavior

### 7.1 Ordinary files

Cattery manages content and source executable bits, not broad mode policy.

Executable bits are reconciled independently from the content drift matrix. A source executable-bit change is intentional desired metadata and applies automatically; an application-side executable-bit change is corrected automatically rather than treated as adoptable content drift. Ordinary target read/write-bit changes are preserved and do not affect convergence. Secret modes remain the exception and are forced as specified in Section 4.5.

For a new ordinary target:

- default mode is `0644`;
- copy source executable bits, yielding the common `0755` case;
- do not copy setuid, setgid, or sticky bits.

For an existing ordinary target:

- preserve its existing read/write permission bits;
- replace only executable bits with the source executable bits;
- content no-op plus executable mismatch is a mode-only apply action;
- never call `chmod` on the existing inode. A mode-only action atomically rematerializes the target from its current bytes with the computed mode, so a hard-linked name elsewhere is not mutated. This intentionally breaks the managed path's hard-link identity while leaving every other link unchanged.

Directory creation uses `os.MkdirAll` and the process umask. Cattery does not later assert directory modes.

For repository source files written by `add`, use `0644` plus target executable bits when creating a new source. When replacing an existing source, preserve its read/write bits and replace its executable bits with the target's. Apply this rule to encrypted sources too: ciphertext need not be private, while its executable bits intentionally encode the decrypted target's executable status.

Atomic replacement creates a new inode owned by the invoking user. It intentionally does not preserve hard-link identity, ownership from a differently owned target, ACLs, extended attributes, file flags, or timestamps. Parent-directory inheritance may apply new ACLs or attributes. The documented secret modes are POSIX mode guarantees only; Cattery does not claim to remove a platform ACL that independently grants access. README must call out this replacement behavior before release.

### 7.2 Atomic writes

Every source write, target content write, and target mode-only correction uses a temporary entry in the destination directory. The atomic writer receives an explicit allowed root: canonical `$HOME` for targets and canonical repository root for sources.

1. Validate parent containment.
2. Create parents.
3. Repeat the no-symlink parent walk after directory creation.
4. Create a uniquely named temporary regular file with initial mode `0600`.
5. Stream or write all bytes.
6. Apply the computed final mode with `Chmod`/`Fchmod` on the still-open temporary file.
7. `Sync` the temporary file so both bytes and final mode precede the durability barrier.
8. Close it.
9. Revalidate the destination parent identities plus final type, content hash, and relevant mode bits.
10. Rename the temporary file over the destination atomically.
11. Open and `Sync` the parent directory before committing the corresponding SQLite baseline. If directory sync fails or is unsupported, report the already-completed rename as a partial operation, do not update state, and stop; the next run recovers from source/target equality.
12. Remove the temporary path on any failure that occurs before rename.

Parent directories created in step 1 are structural side effects and are not rolled back; a later failure may leave empty directories behind. Report them in the partial-operation summary when known, but never recursively remove them because a hook or racing application may have populated them.

Alias creation and replacement use the equivalent same-directory sequence: prepare a uniquely named relative symlink, revalidate parent and destination preconditions, rename it into place, then sync the parent directory before recording alias state.

“Crash safe” in this plan means that Cattery never publishes a partially written file and never records state before the corresponding rename and directory sync. It does not promise survival against storage hardware or filesystems that acknowledge sync operations without honoring them.

Do not create backups or rollback generations in the initial release.

Cattery supports Linux and macOS rename semantics. Windows behavior is explicitly out of scope.

### 7.3 Unexpected target types

- A final symlink where a regular target is expected is drift; never write through it.
- A regular file may be explicitly overwritten after reconciliation permits it.
- A directory at a planned regular-file target is never removed or replaced automatically, whether empty or non-empty; return a manual-intervention error.
- A regular file, symlink, or special entry at any planned target/alias parent component is a preflight manual-intervention error. Detect every blocking ancestor across the selected plan before decisions, hooks, directory creation, or mutation.
- A temporal file-to-directory or directory-to-file source transition can therefore require the user to move or remove the retained old target manually. Source-removal retirement does not override this rule and never authorizes deletion.
- Special filesystem entries are never replaced automatically.

---

## 8. State database

### 8.1 Location and lock

State lives at:

```text
$XDG_STATE_HOME/cattery/state.db
```

with fallback:

```text
$HOME/.local/state/cattery/state.db
```

Use `github.com/adrg/xdg` for this resolution.

Create the Cattery state directory with mode `0700`; after creating a new directory or operational file, explicitly `Chmod`/`Fchmod` it to the required mode so an unusually restrictive umask cannot leave Cattery unable to use its own state. Resolve a missing state path through its nearest existing canonical ancestor, then use the canonical effective directory for all Section 6 overlap checks and state operations. An existing state directory must be a real directory with mode `0700`; fail with a corrective `chmod` instruction rather than silently widening or tightening it. Open/create `state.db` and the lock file with mode `0600`, and require existing instances plus `hash.key` to be final regular files with exactly mode `0600`, never symlinks or special entries. The private parent directory also protects SQLite WAL and shared-memory side files. Reject a relative `XDG_STATE_HOME` rather than resolving it against the working directory.

A process lock lives beside it:

```text
$XDG_STATE_HOME/cattery/cattery.lock
```

Use `github.com/gofrs/flock`. Every command that opens state acquires the lock before reading a baseline; mutating commands hold it through hooks, filesystem changes, and state updates. `TryLock` failure returns immediately with a clear message. Store the current PID in the lock file after acquisition for diagnostics.

A 32-byte keyed-hash secret lives beside the database at `hash.key`. Create it from `crypto/rand` with `O_CREATE|O_EXCL`, mode `0600`, file sync, and parent-directory sync while holding the Cattery lock. Never store it in SQLite, logs, or command output. Store only `BLAKE3("cattery/hash-key-id/v1\x00" || key)` in the `metadata` row whose key is `hash_key_id` so replacement can be detected without storing the key itself. If secret baseline rows exist but the key is absent, malformed, or does not match that identifier, fail safely and require restoration of the matching key or explicit state reset; never silently generate a replacement that makes old baselines incomparable. When no secret baseline row exists, defer key work until the first secret baseline operation: reuse an existing valid key whose identifier matches; if a valid key exists but no identifier does, recover from an interrupted first initialization by committing its derived identifier; create and identify a key only when both are absent; and treat a missing key with a stale identifier or any mismatch as a safe failure requiring explicit cleanup. Commit the matching identifier in the same transaction that inserts the first secret baseline row.

### 8.2 Repository selection

No repository-side marker or mandatory manifest exists.

For `validate`, `status`, `diff`, `apply`, and `add`, repository resolution order is:

1. global `--repo PATH`;
2. `CATTERY_REPO`;
3. the repository marked default for the current canonical home in SQLite by `cattery init`;
4. otherwise fail with instructions to run `cattery init PATH` or pass `--repo`.

Presence is significant. An explicitly changed empty `--repo` or a `CATTERY_REPO` that is set to the empty string is invalid and blocks fallback; an unset environment variable falls through. Do not trim path whitespace or perform shell-style expansion.

`init` is exempt from that resolver and uses only its optional positional `PATH` (defaulting to the initial working directory); reject global `--repo` with `init` and do not consult `CATTERY_REPO`. `version` performs no repository resolution and likewise rejects `--repo`.

`cattery init [PATH]` defaults to the current directory, creates it if absent, canonicalizes it, registers the `(repository root, canonical home)` pair, and makes it the sole default for that home. It is valid for an existing non-empty directory and does not scaffold source files.

Resolve a relative CLI or environment repository path against the command's initial working directory, then use `filepath.Abs` and `filepath.EvalSymlinks`. Commands other than `init` require the canonical repository directory to already exist and never create it implicitly.

`apply` and `add` look up an explicitly selected repository by canonical `(root, home)` without inserting a row; absence means there are no baselines. They create the non-default repository row only inside the first successful baseline/state transaction after all validation, dependency checks, decisions, before hooks, and final precondition checks have passed. Consequently dry-run, unresolved non-interactive operation, missing SOPS, and before-hook failure do not register a repository. Only `init` changes the default. `validate`, `status`, and `diff` never register a previously unseen repository.

Deleting `state.db` never changes repository or target contents. It removes baselines and the default-repository pointer. The user can run `cattery --repo PATH apply` or `cattery init PATH` to re-register; existing targets are then treated as unbaselined.

### 8.3 Hash model

Use BLAKE3 over exact bytes. Do not normalize line endings, encoding, whitespace, or structured data.

- ordinary baseline content hashes are unkeyed BLAKE3;
- secret baseline content hashes use BLAKE3 keyed mode with `hash.key`, reducing offline guessing if `state.db` alone is disclosed;
- encrypted raw source-storage hashes are unkeyed because ciphertext bytes are not semantic plaintext;
- possession of the complete state directory, including `hash.key`, still permits guessing attacks, so state-directory mode and backup protection remain part of the threat model.

Each active file row stores:

- repository identity;
- HOME-relative target path;
- group name, empty for root scope;
- repository-relative source path;
- source kind (`ordinary` or `secret`);
- selected layer (`base`, `darwin`, or `linux`);
- baseline semantic content fingerprint, unkeyed for ordinary content and keyed for secrets;
- baseline raw source-storage hash;
- source executable bits;
- active or retired status;
- last successful application timestamp.

The raw source-storage hash allows an unchanged encrypted source to be classified without decrypting it. If encrypted storage bytes changed, decrypt and compare plaintext with the baseline; a SOPS re-encryption with unchanged plaintext is not a semantic source change.

SQLite stores keyed fingerprints, never plain hashes or plaintext bytes, for secret semantic content.

### 8.4 Initial schema

Use an embedded migration at `internal/state/migrations/001_initial.sql` and `PRAGMA user_version`.

```sql
CREATE TABLE metadata (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL
);

CREATE TABLE repositories (
    id INTEGER PRIMARY KEY,
    root_path TEXT NOT NULL,
    home_path TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    UNIQUE (root_path, home_path)
);

CREATE UNIQUE INDEX repositories_one_default
ON repositories(home_path)
WHERE is_default = 1;

CREATE TABLE files (
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    target_path TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    source_path TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('ordinary', 'secret')),
    layer TEXT NOT NULL CHECK (layer IN ('base', 'darwin', 'linux')),
    baseline_content_hash BLOB NOT NULL CHECK (length(baseline_content_hash) = 32),
    baseline_source_hash BLOB NOT NULL CHECK (length(baseline_source_hash) = 32),
    executable_bits INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    applied_at TEXT NOT NULL,
    retired_at TEXT,
    PRIMARY KEY (repository_id, target_path)
);

CREATE INDEX files_by_scope
ON files(repository_id, group_name, status);

CREATE TABLE aliases (
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    alias_path TEXT NOT NULL,
    canonical_target_path TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    layer TEXT NOT NULL CHECK (layer IN ('all', 'darwin', 'linux')),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    applied_at TEXT NOT NULL,
    retired_at TEXT,
    PRIMARY KEY (repository_id, alias_path)
);
```

Store target and source paths in slash-normalized HOME- or repository-relative form, never as absolute target paths. `root_path` and `home_path` are the deliberate canonical absolute identity anchors.

SQLite cannot express cross-table active-representation uniqueness directly, so after migration and before returning any state snapshot, query for paths active in both `files.target_path` and `aliases.alias_path` for one repository. Treat any result as state corruption and fail without choosing a winner. Normal writes preserve this invariant; the representation-transition transaction in Sections 9.5 and 12.4 retires one table while activating the other atomically.

### 8.5 SQLite behavior

- Use `database/sql` with `modernc.org/sqlite` and `SetMaxOpenConns(1)`.
- Configure the sole connection with `PRAGMA foreign_keys = ON`, `PRAGMA busy_timeout = 5000`, `PRAGMA journal_mode = WAL`, and `PRAGMA synchronous = FULL`.
- Migrations execute in an exclusive transaction.
- Acquiring/creating the advisory lock, opening SQLite, and applying a required schema migration are operational state-store maintenance and may occur before command-level preflight. Statements that dry-run, prompt abort, non-interactive refusal, or before-hook failure make “no mutation” mean no repository/target writes and no repository-registration, baseline, alias, or retirement row changes; they do not promise that lock/SQLite/WAL files remain byte-for-byte untouched. Output and tests must use this exact distinction.
- Store timestamps as UTC RFC3339Nano text produced by an injected clock.
- Filesystem changes are not one global transaction and have no rollback.
- After each successful target rename, establish baseline in a short DB transaction immediately.
- For a verified source/target equality that needs no rewrite, establish or refresh the baseline only after post-hook precondition revalidation. This includes recording a new encrypted-source storage hash after a semantics-preserving SOPS re-encryption.
- After a successful mode-only correction, update the recorded executable bits immediately.
- After each successful alias realization, update its row immediately.
- Never update state before its corresponding filesystem operation succeeds.
- Mark removed source/alias rows retired only after the selected filesystem phase completes.

Selection and ownership use both the current compiled plan and persisted state:

- no-group `status`, `diff`, and `apply` select root scope, every current group, and every state-only group that still has an active file or alias row;
- an explicit group name is valid when it exists in either the current plan or any active/retired state row, allowing a deleted group to be retired and its retained diagnostics to remain inspectable; no-argument selection still excludes retired-only groups;
- retirement occurs only when a selected active row's target has no producer anywhere in the full current platform plan, not merely no producer in the row's old scope;
- moving one target from root/group A to group B preserves the row's baseline because target path is its identity. Reconcile under the new owner, then update group/source/layer metadata only after successful convergence. Selecting only old group A does not retire a target currently owned by unselected group B.
- a current file producer at a path with an active alias row, or a current alias producer at a path with an active file row, is a temporal representation transition rather than ordinary ownership movement or source removal. It follows Section 9.5's explicit replacement contract.

This ordering makes crash recovery safe:

- crash after rename but before DB update: source and target will match on the next run and Cattery can establish a converged baseline;
- DB can never claim bytes were installed before they actually were;
- earlier files remain accurately recorded if a later file fails.

---

## 9. Reconciliation model

### 9.1 Inputs

For each current managed file, obtain:

- current semantic source content hash;
- current raw source-storage hash;
- current target content hash or absence/type;
- previous baseline content hash, if any;
- current source and target executable bits;
- source kind and expected target mode.

Secret source content is decrypted only when raw encrypted bytes differ from the stored raw source hash, the row is unbaselined, or target bytes must be written.

### 9.2 Core matrix

For a previously baselined regular target:

| Source vs baseline | Target vs baseline | Source vs target | Result |
|---|---|---|---|
| unchanged | unchanged | equal | no-op, except mode correction |
| changed | unchanged | different | apply source automatically |
| unchanged | changed | different | target drift; require explicit decision |
| changed | changed | equal | already converged; update baseline without rewriting |
| changed | changed | different | conflict; require explicit decision |

In this table, changed/unchanged refers to exact content bytes. Executable-bit reconciliation follows Section 7.1 independently.

Target absence or an unexpected target type counts as a target change when a baseline exists.

### 9.3 Unbaselined targets

When no baseline exists:

- target absent: create from source automatically;
- regular target bytes equal source: establish baseline without rewriting;
- regular target differs: require explicit decision;
- final target symlink, directory, or special entry: require type-specific handling described above.

This makes database loss safe: equal files are adopted as an operational baseline, while differing files are never silently overwritten.

### 9.4 Interactive resolution

`apply` is allowed to mutate targets, never sources.

For target drift, an unbaselined mismatch, or a source/target conflict, offer:

```text
[d]iff
[o]verwrite target from repository
[s]kip
[a]bort
```

- `diff` redisplays the prompt after rendering an allowed diff.
- `overwrite` records explicit permission for this target.
- `skip` leaves it unresolved, continues with other resolved work, and causes a nonzero final status.
- `abort` performs no hooks, repository/target writes, or managed state-row changes because all decisions are collected before execution.
- The prompt tells the user to run `cattery add <target>` to adopt target content.
- `apply` never imports target bytes into the repository, including through a prompt shortcut.

Skipping a file skips its associated mode correction as well. Cattery must not modify metadata on a target whose content decision the user declined.

Secret prompts omit `diff` and never expose plaintext.

Prompt parsing is deterministic:

- trim surrounding Unicode whitespace and compare case-insensitively;
- accept `o` or `overwrite`, `s` or `skip`, and `a` or `abort` for every prompt; for eligible ordinary files only, also accept `d` or `diff`;
- `diff` prints the safe ordinary diff to stderr and then repeats the same prompt without recording a decision;
- an empty or unknown answer prints the accepted choices and repeats the prompt;
- secret and occupied-alias prompts offer `overwrite`, `skip`, and `abort`; `diff` is invalid there;
- EOF before a valid answer aborts the entire command before hooks or planned repository/target/managed state-row mutations with exit 1;
- SIGINT follows the signal contract and exits 130.

Continue until a valid answer, EOF, or cancellation; do not choose a default on empty input. Prompt targets in normalized target-path order.

If stdin is not a TTY, behavior is automatically non-interactive. `--non-interactive` forces that behavior. If any prompt would be required, the entire command exits before hooks, repository/target writes, or managed state-row changes.

Detect a terminal with `golang.org/x/term`: stdin is interactive only when it is an `*os.File` and `term.IsTerminal(int(file.Fd()))` succeeds. Represent this as an injected `IsTerminal func(io.Reader) bool` boundary in CLI tests; arbitrary readers, pipes, redirected files, and `/dev/null` are non-interactive.

There is deliberately no global `--yes` or blind conflict-overwrite flag in the initial release.

### 9.5 Source removal

When a selected active state row's target has no producer anywhere in the full current platform plan:

- leave the target untouched;
- mark the row `retired` after a successful apply of the affected scope;
- report that the target remains unmanaged;
- retain its previous baseline for diagnostics and safe reactivation;
- do not repeatedly treat it as current drift.

If the source later reappears, reactivate the row and reconcile against the retained baseline.

Temporal file-to-alias and alias-to-file changes at one target path are representation transitions, not ordinary source retirement. First compare the current path entry with the old active representation: for a file row, require its baseline semantic fingerprint and managed mode (executable bits for ordinary files, exact `0600`/`0700` for secrets); for an alias row, require its exact previously recorded relative payload. When the old representation is intact, the repository-only representation change applies automatically under the normal source-only rule. When the path is missing, drifted, or cannot be proven equal to the old representation, report the old managed representation and current desired representation and require `overwrite`, `skip`, or `abort`; non-interactive mode fails unresolved. File-to-alias replacement uses Section 5's atomic occupied-alias behavior. Alias-to-file replacement treats the final symlink as the path entry to replace and never follows its referent. A directory or special entry still requires manual intervention. After successful replacement and parent sync, one state transaction activates the current representation row and retires the opposite-table row; a skip leaves the old row active so the transition remains visible. Current-plan file/alias collisions remain validation errors and never reach this temporal path.

### 9.6 Diff rendering

- Compare current semantic source bytes with current target bytes.
- For valid UTF-8 text of at most 1 MiB per side, render a unified diff with `go-difflib` only when every rune is newline, tab, or `unicode.IsPrint`. Treat carriage return, NUL, ESC, DEL, C0/C1 controls, bidi/zero-width format controls, and every other non-printing rune as binary for output purposes. This prevents terminal-control and line-forging injection.
- Labels are `repo/<source-path>` and `$HOME/<target-path>`.
- For binary or larger files, show sizes and BLAKE3 hashes only.
- For secrets, show only classification such as `secret source and target differ`; never show content, size, or hash unless a later security review explicitly approves it.

---

## 10. Hooks

### 10.1 Layout

```text
repo/
  _hooks/
    before/
    after/
  ghostty/
    _hooks/
      before/
      after/
```

Repository hooks run for every `apply`, including group-selective applies. Group hooks run only for selected groups.

### 10.2 Validation

- Hook files must be direct regular-file children of `before/` or `after/`.
- Every hook must have at least one executable bit.
- Non-executable files, nested directories, symlinks, and special files are validation errors rather than silently ignored.
- Hook files execute directly, without an implicit shell. Scripts therefore need a valid shebang.
- Hook names sort by bytewise path order.

### 10.3 Order

For selected groups sorted lexicographically:

```text
resolve and validate the entire plan
collect all interactive decisions
repository before hooks
all selected group before hooks
revalidate every selected source and target snapshot
filesystem file application
symlink alias application
retire missing state rows
all selected group after hooks
repository after hooks
post-verify selected sources, targets, and aliases
establish or refresh no-write equality baselines that passed post-verification
```

All before hooks complete before the first planned Cattery executor or managed state-row mutation. If any before hook fails, stop immediately and perform none of those planned mutations. Operational lock/database setup may already have occurred under Section 8.5, and hooks are trusted arbitrary programs, so Cattery cannot promise either that state-store files are byte-identical or that the failed hook itself did not edit a managed path.

After hooks run only when the filesystem phase completes. If an after hook fails:

- continue running remaining after hooks;
- report every failure;
- return nonzero;
- keep applied files and accurate state;
- do not roll back.

If the filesystem phase fails midway, preserve accurate per-file state, skip all after hooks, and report a partial apply.

After all successful/failed after hooks have been attempted, revalidate selected source types, raw storage hashes, executable bits, canonical target types/content/modes, and exact alias payloads against the just-recorded plan and baselines. If a hook or racing application changed any of them, report immediate post-apply drift and return the unresolved-difference status unless a hook failure takes precedence. Do not rewrite state: the recorded baseline must continue to describe what Cattery installed, so a later `apply` sees source changes and `add` can explicitly adopt target changes.

Hooks run even when selected files are no-ops. This permits package/bootstrap hooks to represent work outside Cattery's file model. `--dry-run` and `--no-hooks` do not run hooks.

### 10.4 Process environment

Hooks inherit the caller environment and receive:

```text
CATTERY_REPO=<absolute repository path>
CATTERY_HOME=<absolute home path>
CATTERY_PLATFORM=linux|darwin
CATTERY_PHASE=before|after
CATTERY_GROUP=<group name or empty for repository hook>
CATTERY_RESULT=pending|success|partial
```

Before hooks receive `CATTERY_RESULT=pending`. After hooks receive `success` when every selected item is converged, or `partial` when the filesystem phase completed but the user skipped one or more unresolved files/aliases. A mid-filesystem operational failure still skips after hooks entirely.

The working directory is always repository root. Hooks receive no positional arguments. Hook stdout and stderr are inherited so interactive tools and package managers behave normally.

Hooks are trusted arbitrary programs. Cattery does not pass secret plaintext in hook-specific variables, but it cannot redact a hook that deliberately prints deployed secret files. Secret-output guarantees apply to Cattery-generated diagnostics, not inherited hook output.

There is no default timeout. Start each hook in its own Unix process group. On context cancellation, signal the group with SIGTERM, allow a five-second grace period, then SIGKILL any remaining group. Use the same subprocess helper for SOPS. This prevents package-manager descendants from surviving an interrupted Cattery process. Cattery does not sandbox hooks; repositories containing hooks are trusted code.

---

## 11. Command-line interface

Use `github.com/spf13/cobra` v1.10.2 strictly as the CLI adapter. Cobra owns command discovery, help text, argument-count checks, and flag parsing. It does not own repository resolution, environment precedence, repository compilation, target inspection, reconciliation, state access, prompting policy, hooks, SOPS, mutation planning, or filesystem execution. Prompts use `bufio.Reader` and injected input/output streams; do not add a terminal UI framework.

Every Cobra command follows the same adapter contract:

1. its constructor receives a narrow service interface plus only the explicit CLI runtime values it needs;
2. it binds flags into a command-local options struct and uses Cobra only for syntactic arity/flag validation;
3. `RunE` converts options and arguments into one typed application request, calls exactly one application service method, passes the typed result to a renderer, and returns the service or rendering error;
4. semantic validation, repository/default selection, current-directory path resolution, decision eligibility, and all side effects remain in application/backend packages; for repository-using commands the adapter merely copies the raw `--repo` value plus `Changed("repo")`, raw `CATTERY_REPO` value plus `LookupEnv` presence, and initial working directory into the request, while Task 56 alone applies precedence and canonicalization;
5. renderers own human-readable stdout/stderr formatting but do not classify state or choose actions;
6. exit-code mapping happens once in `internal/cli/execute.go`, outside command handlers.

`RunE` callbacks are at most 15 physical lines and contain no filesystem traversal, hashing, SQL, subprocess execution, reconciliation loops, hook ordering, SOPS calls, or direct `os.Exit`. Cobra command constructors and renderers remain independently testable with fake one-method services. No package-level command, flag, service, stream, environment, or mutable options variable is permitted.

The initial operational command set is exactly `init`, `validate`, `status`, `diff`, `apply`, `add`, and `version`, plus Cobra's standard `help`/`--help` surface. Root execution with no command prints root help to stdout and exits 0; help paths invoke no application service. Leave the root `Version` field empty, so `--version` is an unknown flag with usage exit 1 and the `version` subcommand is the sole version interface. Disable Cobra's autogenerated `completion` command and command-name suggestions. Do not add `list`, `doctor`, `remove`, `prune`, shell-completion generation, or compatibility aliases before the first release.

Global form:

```text
cattery [GLOBAL-OPTIONS] COMMAND [OPTIONS] [ARG ...]
```

The global options are `--repo PATH` and `--verbose`. Implement both as root persistent flags, so Cobra accepts them before the command and interspersed after it; command-local flags are likewise interspersed with positional arguments, while `--` terminates option parsing. `--repo` is valid only for `validate`, `status`, `diff`, `apply`, and `add`; `init` and `version` reject an explicitly changed `--repo` before invoking a service. `--verbose` is accepted for every command. Tests freeze global/local flags before, after, and between positional arguments, `--` termination, and unsupported-command rejection.

For commands accepting `[GROUP ...]`, one or more arguments select only those exact case-sensitive group names and exclude root scope; globs, comma lists, and an implicit root selector are unsupported. For `validate`, no arguments select root plus every current group, and explicit names must exist in the compiled repository. For `status`, `diff`, and `apply`, no arguments select root plus the union of current groups and active state-only groups; an explicit name may exist in the current plan or any active/retired state row so a deleted group can be retired and later inspected. Unknown or duplicate arguments are usage errors.

### 11.1 `cattery init [PATH]`

- Default `PATH` is current working directory.
- If the directory is missing, resolve and canonicalize its nearest existing ancestor, append the validated missing suffix, and apply the repository/HOME/state overlap rules before `MkdirAll`; after creation, canonicalize and recheck before registration.
- Reject a non-directory.
- Canonicalize and register it in SQLite.
- Clear previous `is_default` rows for the current canonical home and set this repository as that home's default.
- Do not create a manifest, marker, sample files, Git repository, or commit.

### 11.2 `cattery validate [GROUP ...]`

- Compile and validate the full repository, then optionally report selected groups.
- Check source entry types, overlays, controls, hook executability, route schema, path safety, all global collisions, and that every secret source is nonempty syntactically valid JSON. This is storage-shape validation only and does not claim that SOPS can decrypt it.
- Do not access targets, run SOPS, run hooks, or mutate state other than opening/migrating the DB if repository resolution needs it.
- Output exactly two deterministic count lines, `linux files=<n> secrets=<n> aliases=<n> groups=<n>` followed by `darwin ...`. Counts describe the selected current scopes in each independently compiled platform plan; global validation still covers every scope on both platforms.

### 11.3 `cattery status [GROUP ...]`

- Compare plan, target, and baseline without mutation or hooks.
- Print one concise line per non-no-op file and a summary.
- Attempt SOPS decryption only when required to classify a changed encrypted source.
- Show retired records for selected scopes separately.
- Exit 0 only when selected active files and aliases are converged; otherwise exit 2.

### 11.4 `cattery diff [GROUP ...]`

- Perform status classification and render allowed content differences.
- Never prompt, mutate, run hooks, or print secret content.
- Exit 0 only when the same selected state that `status` evaluates is fully converged. Exit 2 for any status-classified difference, including alias-only drift or pending retirement when there is no content diff to render.

### 11.5 `cattery apply [GROUP ...]`

Options:

```text
--dry-run
--non-interactive
--no-hooks
```

- No groups means root scope plus every current and active state-only group under Section 8.5.
- Specifying groups applies only those groups' files and aliases; root files are excluded, but repository hooks still run.
- Compile and validate the full repository before filtering so an invalid unselected group cannot hide collisions.
- Gather every interactive decision before any hook or planned repository/target/managed state-row mutation; operational state-store setup remains the Section 8.5 exception.
- Dry-run prints the intended action plan, does not prompt, and performs no hooks, repository/target writes, or managed state-row updates; Section 8.5's operational lock/database exception still applies.
- Render one target-sorted line per non-no-op action with a stable leading verb: `CREATE`, `UPDATE`, `CHMOD`, `BASELINE`, `RETIRE`, `ALIAS-CREATE`, `ALIAS-REPLACE`, or `NEEDS-DECISION`. When one path has multiple lines, use that listed verb order; `NEEDS-DECISION` replaces rather than accompanies the unresolved destructive action.
- A dry-run decision point always renders `NEEDS-DECISION <safe-path> <reason>`; it never assumes overwrite or skip. Continue rendering independent planned actions, print verb counts, and exit 2 when any decision remains.
- Secret dry-run lines include kind, action, and safe paths but no content hash or diff.

### 11.6 `cattery add [OPTIONS] FILE ...`

Options:

```text
--group NAME
--platform linux|darwin
--secret
--dry-run
```

Initial release accepts regular files only; directories and symlinks are errors.

For already-managed targets, only options actually present on the command line constrain inferred ownership; record `cmd.Flags().Changed("secret")`, `Changed("group")`, and `Changed("platform")` in the typed add request so an omitted value is not mistaken for an explicit ordinary/root/base request. For unmanaged targets, omitted options retain the documented root/base/ordinary defaults. The application service, not Cobra, interprets these presence bits.

Resolve relative `FILE` arguments against the command's initial working directory. Do not implement `~` expansion inside Cattery; shells normally perform it, and a literal unexpanded `~` is just a relative path subject to the same HOME containment check.

Behavior:

1. Require every target to be beneath `$HOME` after safe containment checks. Reject duplicate canonical target arguments and two distinct arguments naming the same existing filesystem object before inferring sources or writing anything.
2. Compile the current repository first.
3. If the target is already a canonical managed file, infer and update its existing source location. Explicit options must agree with that owner or return an error. If the argument names a configured alias, reject it and print the canonical target to add instead.
4. If unmanaged, use root scope unless `--group` is explicit. Never guess a group from the path. Before writing, prove that the derived source path recompiles to the same target under Section 2's grammar. In the base root, a multi-segment path beginning with a non-dot segment or a metadata name ignored by Section 2.3 requires an explicit group; an explicit `--platform` may instead make it representable inside the root platform layer. A path beginning with `_` is rejected as unrepresentable. A valid explicit group may be new; create its directory only after the entire add batch passes preflight.
5. `--platform` writes into the explicit platform layer; absence writes base.
6. `--secret` writes under the selected layer's `_secrets/` using SOPS.
7. Reject any destination already owned by another source.
8. Snapshot the target bytes and existing source precondition, then revalidate both immediately before atomically replacing the source.
9. Preserve target executable bits on the source.
10. Revalidate the target after the source write and establish the equal source/target content as baseline only if it still matches. If it changed during adoption, keep the explicit source update, omit the baseline update, and report a partial conflict for the next reconciliation.

`add` is itself the explicit target-wins operation, so it does not ask an additional overwrite question. `--dry-run` exists for inspecting the inferred source destination. Converting an existing source between normal/secret or base/platform storage is not implicit; return a clear error requiring manual source relocation followed by `add`.

`add --dry-run` emits one target-sorted `ADD <target> -> <repository-relative-source>` or `ADD-SECRET ...` line after full batch validation, followed by counts. It performs no SOPS invocation, source writes, group-directory creation, or baseline changes; ownership and destination inference use only the compiled repository plan.

An explicit `--platform` must equal the current `runtime.GOOS`; reject `--platform darwin` on Linux and vice versa. A deployed target is evidence from the current platform and must never establish a baseline for an inactive platform layer. Seed or edit an inactive overlay manually, or run `add` on that platform.

`add` never runs repository or group hooks. For multiple files it validates the complete batch first, then writes sources and baselines sequentially in target-path order. There is no batch rollback; a later failure leaves earlier adopted files accurately recorded and reports the partial result. Newly created group/source parent directories may remain empty under Section 7.2 and are reported rather than recursively removed.

### 11.7 `cattery version`

Print exactly one newline-terminated line:

```text
cattery <version> commit=<commit> built=<timestamp> go=<go-version> target=<os>/<arch>
```

The development defaults are `version=dev`, `commit=unknown`, and `timestamp=unknown`; `<go-version>` is `runtime.Version()`. A release uses the exact annotated tag, full commit SHA, and the tagged commit's UTC RFC 3339 timestamp injected with `-ldflags`. Derive release time through `SOURCE_DATE_EPOCH`, never the workflow wall clock, and build every archive for one release from the same version/commit/timestamp tuple.

Release assets are exactly four `cattery_<semver-without-v>_<goos>_<goarch>.tar.gz` archives plus `SHA256SUMS`, in lexical filename order. Each archive contains one top-level directory with the same stem and exactly `cattery` mode `0755` plus `LICENSE` mode `0644`. Build with `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and linker flags `-s -w -buildid=` plus `-X github.com/alyraffauf/cattery/internal/buildinfo.Version=<tag>`, `-X github.com/alyraffauf/cattery/internal/buildinfo.Commit=<full-sha>`, and `-X github.com/alyraffauf/cattery/internal/buildinfo.BuildTimestamp=<utc-rfc3339>`. The Nix shell pins GNU tar and gzip; package with sorted ustar entries, numeric owner/group zero, empty user/group names, every mtime equal to `SOURCE_DATE_EPOCH`, and `gzip -n`. Generate lowercase SHA-256 lines in lexical order. The reproducibility test builds from two different absolute checkout paths and requires identical binaries, archives, and checksum manifest bytes.

### 11.8 Exit status contract

```text
0  command completed and selected state is converged
1  operational, validation, usage, or filesystem error
2  unresolved drift/conflict or differences reported by status/diff/dry-run
3  hook failure after or before apply
4  required external dependency missing
130 interrupted by SIGINT where the platform permits
143 terminated by SIGTERM where the platform permits
```

Interactive `skip` yields exit 2 after completing all other resolved work.

For mixed outcomes, signal termination wins (`130` for SIGINT, `143` for SIGTERM); a missing required dependency returns 4 because dependency preflight occurs before mutation; any hook failure returns 3 even when skips or post-hook drift also exist; an operational, validation, state, or filesystem error returns 1, including a later error after earlier `add` or `apply` success; and exit 2 is used only when differences or partial target races remain without a higher-priority outcome. The command summary still reports completed and pending actions regardless of the winning code.

### 11.9 Streams and output stability

- Normal results, status lines, dry-run plans, command summaries, `version`, and non-interactive diffs go to stdout.
- Errors, warnings, verbose diagnostics, and interactive prompts go to stderr; answers are read from stdin.
- A diff requested from inside an interactive prompt also goes to stderr so redirecting stdout cannot hide prompt context.
- Hook stdout/stderr remain directly inherited and are the documented exception to Cattery's own stream separation.
- Initial output has no ANSI color and no JSON mode. Scripts may rely on exit statuses, not prose wording.
- Paths beneath home render as `$HOME/<relative-path>`; repository sources render as `repo/<relative-path>` so diagnostics do not unnecessarily expose absolute user paths.
- Escape control characters and ambiguous whitespace in displayed paths with a stable Go-style quoted representation; a filename must never inject extra terminal lines or forged diagnostics.

---

## 12. Internal architecture

### 12.1 Package layout

```text
cmd/cattery/main.go

internal/bootstrap/build.go
internal/bootstrap/adapters.go
internal/bootstrap/applications.go
internal/buildinfo/info.go
internal/failure/error.go

internal/cli/root.go
internal/cli/execute.go
internal/cli/options.go
internal/cli/runtime.go
internal/cli/init.go
internal/cli/validate.go
internal/cli/status.go
internal/cli/diff.go
internal/cli/apply.go
internal/cli/add.go
internal/cli/version.go
internal/cli/prompt.go
internal/cli/render_validate.go
internal/cli/render_status.go
internal/cli/render_diff.go
internal/cli/render_apply.go
internal/cli/render_add.go

internal/application/initialize/types.go
internal/application/initialize/service.go
internal/application/validate/types.go
internal/application/validate/service.go
internal/application/inspect/types.go
internal/application/inspect/service.go
internal/application/inspect/status.go
internal/application/inspect/diff.go
internal/application/apply/types.go
internal/application/apply/evaluate.go
internal/application/apply/dependencies.go
internal/application/apply/decisions.go
internal/application/apply/prepare.go
internal/application/apply/source_guard.go
internal/application/apply/execute_files.go
internal/application/apply/execute_aliases.go
internal/application/apply/hooks.go
internal/application/apply/verify.go
internal/application/apply/service.go
internal/application/add/types.go
internal/application/add/infer.go
internal/application/add/preflight.go
internal/application/add/plan.go
internal/application/add/write_ordinary.go
internal/application/add/write_secret.go
internal/application/add/execute.go
internal/application/add/service.go
internal/application/version/types.go
internal/application/version/service.go

internal/deployment/scope.go
internal/deployment/file.go
internal/deployment/alias.go
internal/deployment/plan.go
internal/deployment/hash.go
internal/deployment/sort.go
internal/pathsafe/path.go
internal/pathsafe/equivalence.go
internal/pathsafe/root.go
internal/pathsafe/ancestor.go
internal/pathsafe/protected.go
internal/pathsafe/identity.go
internal/subprocess/run.go
internal/subprocess/process_unix.go

internal/state/types.go
internal/state/database.go
internal/state/lock.go
internal/state/migrations.go
internal/state/migrations/001_initial.sql
internal/state/store.go
internal/state/repositories.go
internal/state/keyfile.go
internal/state/keyid.go
internal/state/recovery.go
internal/state/files.go
internal/state/files_read.go
internal/state/files_decode.go
internal/state/aliases.go
internal/state/aliases_read.go
internal/state/aliases_decode.go
internal/state/transitions.go

internal/repository/controls.go
internal/repository/scan.go
internal/repository/overlay.go
internal/repository/collisions.go
internal/repository/compiler.go
internal/routes/config.go
internal/routes/activate.go
internal/hooks/discover.go
internal/hooks/order.go
internal/hooks/execute.go
internal/secrets/client.go
internal/secrets/encrypt.go
internal/secrets/decrypt.go
internal/secrets/candidate.go

internal/filesystem/precondition.go
internal/filesystem/target.go
internal/filesystem/source.go
internal/filesystem/freeze.go
internal/filesystem/parents.go
internal/filesystem/sync.go
internal/filesystem/replace.go
internal/filesystem/mode.go
internal/filesystem/alias.go
internal/reconcile/types.go
internal/reconcile/source_snapshot.go
internal/reconcile/target_snapshot.go
internal/reconcile/state_snapshot.go
internal/reconcile/state_records.go
internal/reconcile/snapshot.go
internal/reconcile/classify_file.go
internal/reconcile/classify_alias.go
internal/reconcile/classify_retirement.go
internal/reconcile/decisions.go
internal/diff/safe.go
internal/selection/repository.go
internal/selection/groups.go

internal/quality/scan_test.go
internal/quality/shape_test.go
internal/quality/source_limits_test.go
internal/quality/naming_test.go
internal/quality/architecture_test.go
internal/quality/third_party_test.go
internal/testfixture/filesystem/tree.go
internal/testfixture/sops/executable.go
internal/testfixture/database/store.go
```

This is the exhaustive initial-release Go package/file layout, not permission to collect unrelated behavior. Root documentation, Nix, workflow, and script paths are exhaustively owned in Section 16. A needed file rename, split, or addition requires a plan amendment before coding; a card may not invent it locally. Every non-test Go file receives a focused test named by exactly one roadmap card. `internal/testfixture` is a directory only, never a Go package: its three child packages expose one narrowly named fixture role each, and non-fixture production code may not import them.

**CLI/application seam:**

- `cmd/cattery/main.go` creates the signal-aware root context, asks `internal/bootstrap` for an opaque `cli.Application`, calls `internal/cli.Execute`, and is the only production file that calls `os.Exit`.
- `internal/bootstrap` is the composition root. It constructs concrete state, compiler, filesystem, subprocess, SOPS, hook, and application services, places them in the explicit `cli.Dependencies` struct, and calls `cli.NewApplication`. It contains wiring only and no reconciliation or rendering rules. Bootstrap and service constructors are side-effect-free: they create lazy stores/factories but do not open SQLite, acquire locks, create state paths/keys, inspect repositories, resolve SOPS, or run hooks. Those effects begin only inside the selected application service, so `version`, help, and Cobra parse failures touch no backend resource.
- Each command file declares its own exported, one-method service interface using typed request/result types from the corresponding `internal/application/...` package. `cli.Dependencies` has one field per interface. Interfaces live at this consuming edge; application packages satisfy them structurally and do not import CLI.
- `internal/cli` is the only package allowed to import Cobra. No exported CLI API exposes `*cobra.Command` or a pflag type: the root command stays inside the opaque `cli.Application`. `internal/bootstrap`, `internal/application/...`, and every backend/domain package must compile and test without importing Cobra or `pflag`.
- The only exported CLI construction/execution surface is `cli.NewApplication(cli.Dependencies, cli.Runtime) *cli.Application` and `cli.Execute(context.Context, *cli.Application, []string) int`. `Dependencies` has one explicitly named field per one-method command service; `Runtime` contains stdin/stdout/stderr plus injected current-directory, environment, terminal predicates, and a per-application `SetVerbose(bool)` callback. Root construction calls `SetIn`, `SetOut`, and `SetErr`, sets `SilenceErrors` and `SilenceUsage`, leaves `Version` empty, and never uses `cobra.OnInitialize`. Bootstrap owns one `slog.LevelVar` and one logger per application, injects that logger into backend services, and supplies a callback that changes only that level variable; never call `slog.SetDefault`. Every `new...Command` constructor is unexported, and the `*cobra.Command` stored inside `Application` is unexported, so bootstrap and `main` cannot inspect or mutate Cobra state.
- `cli.Application` is explicitly single-use because Cobra mutates command/flag/help state during execution. `cli.Execute` marks the value consumed before parsing; a second call returns exit 1, writes one stable error, and invokes no service. Every test case and every process execution constructs a fresh application, preventing flag/output state leakage without a reset shim.
- An operational command, prompt, or renderer file may import Cobra, its one corresponding application request/result package, `internal/failure`, and standard-library context/IO/formatting packages only. `root.go`/`execute.go` may additionally import the command application packages needed to type `Dependencies`; `runtime.go` alone may import `golang.org/x/term`. No CLI file may import `selection`, `repository`, `state`, `reconcile`, `diff`, `filesystem`, `hooks`, `secrets`, `database/sql`, or `os/exec`.
- Application services accept plain typed requests and return plain typed results plus categorized errors. Requests may contain paths, selection, explicit-option presence, dry-run/non-interactive policy, and a presentation-neutral decision resolver; they never contain `*cobra.Command`, `pflag.FlagSet`, or Cobra annotations. Results contain sorted semantic records and counts, not already formatted terminal lines. Every value crossing from an application package into CLI is owned by that application package; lower deployment/reconciliation/diff records are projected into application DTOs before return or prompt resolution so CLI never needs a backend import.
- Each application package declares its own purpose-named input ports beside the service that consumes them, such as plan compilation, state reads, baseline writes, snapshots, hooks, SOPS, and atomic replacement. Constructors receive one explicit `Dependencies` struct containing only those narrow ports. Concrete state/filesystem/process types are constructed only in bootstrap and satisfy the ports structurally; application packages never open SQLite, call `os` mutation APIs, or launch processes directly.
- Lower packages never import `internal/application`, `internal/cli`, or `internal/bootstrap`. Shared immutable records live in `deployment`, `reconcile`, or the owning adapter package rather than being pushed downward from an application service.
- In every application package, `service.go` contains only the `Service` type, constructor validation, and short public pipeline method. Named phase files own preparation, decision collection, execution, and verification; `service.go` is not a sanctioned orchestration dumping ground.
- The import checker also enforces file-level purity inside intentionally mixed adapter packages: `reconcile/classify_*.go`, `reconcile/decisions.go`, `hooks/discover.go`, and `hooks/order.go` may import only standard-library and immutable domain packages, never state, secrets, subprocess, filesystem, SQL, or OS mutation APIs. Effectful snapshot files and `hooks/execute.go` are the sole exceptions in those packages.
- `internal/subprocess` owns process creation, explicit streams, process-group cancellation, and exit status only. It neither captures nor redacts content by policy. `internal/hooks` supplies inherited streams; `internal/secrets` owns bounded capture, diagnostic redaction, and clearing every secret-bearing buffer.
- Prompt IO implements the `DecisionResolver` interface consumed by the apply application package. The resolver is testable without executing Cobra, and application tests use deterministic fake resolvers without importing `internal/cli`.
- Renderers receive typed results and an `io.Writer`; they do not call services, read state, or choose reconciliation outcomes.
- Third-party libraries stay behind their owning adapters: Cobra in `internal/cli`, SQLite in `internal/state`, TOML in `internal/routes`, SOPS process details in `internal/secrets`, and terminal detection in `internal/cli/runtime.go`.
- Keep interfaces at the consuming package boundary. Do not create a service locator, generic repository pattern, generic dependency-injection framework, or generic `utils`, `common`, `helpers`, `manager`, or `misc` package/file.

The normative command shape is:

```go
type ApplyService interface {
    Apply(context.Context, applicationapply.Request) (applicationapply.Result, error)
}

command.RunE = func(command *cobra.Command, arguments []string) error {
    request := buildApplyRequest(command, arguments, globalOptions)
    result, serviceError := service.Apply(command.Context(), request)
    if renderError := renderApply(command.OutOrStdout(), result); renderError != nil {
        return errors.Join(serviceError,
            failure.New(failure.Operational, "render apply", renderError))
    }
    return serviceError
}
```

`buildApplyRequest` performs mechanical copying and explicit-flag presence capture only. Anything requiring repository, state, filesystem, or reconciliation knowledge belongs in `applicationapply.Service` or a lower package. Application services return a zero or partial typed result alongside categorized errors when there is safe progress to summarize; renderers therefore run before the service error is returned to the single exit mapper.

The application-facing command contracts are frozen as follows. Every concrete service uses `NewService(Dependencies) *Service`; every `Dependencies` field is a purpose-named local port, not a concrete adapter or generic container.

| Command | Single service call | Typed request fields | Typed result responsibility |
|---|---|---|---|
| `init` | `Initialize(context.Context, initialize.Request)` | raw optional path, path-present bit, initial working directory | canonical repository path and whether it was created/registered |
| `validate` | `Validate(context.Context, validate.Request)` | raw explicit/env/current-directory repository fields with explicit/env presence bits, raw ordered groups | sorted Linux/Darwin count records |
| `status` | `Status(context.Context, inspect.Request)` | raw explicit/env/current-directory repository fields with explicit/env presence bits, raw ordered groups | sorted semantic status records and summary counts |
| `diff` | `Diff(context.Context, inspect.Request)` | same fields as status | sorted sanitized diff/status records and summary counts |
| `apply` | `Apply(context.Context, apply.Request)` | raw repository fields, raw ordered groups, dry-run/non-interactive/no-hooks booleans, `DecisionResolver` | sorted planned/completed/skipped/partial records and summary counts |
| `add` | `Add(context.Context, add.Request)` | raw repository fields, raw target arguments, group/platform/secret values plus separate presence bits, dry-run | sorted per-target planned/completed/partial records and summary counts |
| `version` | `Version(context.Context, version.Request)` | empty request value | version, commit, UTC build timestamp, Go version, OS, architecture |

Each application request that needs a repository defines its own small `RepositoryInput` value containing only raw explicit path, explicit-path presence, raw `CATTERY_REPO`, environment-presence, and initial working directory. The CLI imports only that command's application package. Inside the service, a named mapper converts `RepositoryInput` to `selection.RepositoryRequest`; this deliberate five-field duplication prevents a lower selection type or generic shared request package from leaking through the CLI seam. The selection component receives canonical HOME/default-state ports in its own constructor and returns a canonical repository identity. Requests never contain CLI option structs; results never contain preformatted lines. An application service may return a nonzero partial result and an error together, and every renderer must handle that combination deterministically.

**Readable-code and size contract:**

- Every hand-written implementation, test, script, Nix, workflow, and migration file is at most 400 physical lines. The live scan includes `.go`, `.sh`, `.bash`, `.py`, `.nix`, `.yml`, `.yaml`, `.sql`, and the root `justfile`; generated `go.sum`, `flake.lock`, and prose documentation are not implementation source. There are no initial-release source exceptions: split tables, fakes, renderers, workflows, scripts, and orchestration phases by cohesive behavior before crossing the limit.
- Every named function, method, and function literal spans at most 40 physical lines from signature through closing brace. A Cobra `RunE` function literal has the stricter 15-line limit.
- Functions have one responsibility and one abstraction level. Prefer guard clauses; control-flow nesting may not exceed two levels. Dense one-liners and boolean puzzles are not acceptable substitutes for named steps.
- A function has at most 25 recursively counted `ast.Stmt` nodes and at most ten decision points. The decision walker counts `if`, `for`/`range`, each non-default switch/type-switch/select clause, and each `&&` or `||`. These limits apply to tests and function literals; meeting either count never excuses mixed abstraction levels.
- Functions take at most three explicit parameters, excluding a method receiver and including `context.Context`. Four or more inputs require a purpose-named request or dependency struct. Command service interfaces have exactly one method; every other interface has at most three methods, counting embedded methods transitively, and is split by consumer role when callers need less. Accept interfaces and return concrete types.
- Production packages have no mutable package globals and no `init` functions. The only package-variable exceptions are linker-populated strings named `Version`, `Commit`, and `BuildTimestamp` in `internal/buildinfo/info.go`, plus the read-only `initialMigrationSQL` string with `//go:embed migrations/001_initial.sql` in `internal/state/migrations.go`; the checker rejects assignments to or address-taking of those values outside their declarations. Dependencies, loggers, clocks, environment lookups, current-directory lookup, terminal detection, filesystems, and subprocess execution are injected explicitly.
- Names reveal intent and avoid local abbreviations such as `cfg`, `mgr`, `svc`, `req`, `res`, `opts`, `fsys`, `curr`, and `prev`. Idiomatic Go names such as `ctx`, `err`, `tx`, `db`, `io`, `id`, short loop indices, and conventional receiver names remain acceptable. Comments explain non-obvious safety reasons, never restate code.
- No file named `manager.go`, `helpers.go`, `utils.go`, `common.go`, or `misc.go` is permitted; no package may use those names. Purpose-prefixed fixture files still need one cohesive role, and broad `Manager`, `Helper`, or `Util` types are forbidden.
- Initial-release code contains no `//nolint`, `//lint:ignore`, `//revive:disable`, generated-code marker, or quality-test path exception. A real false positive requires fixing the checker or amending this plan, not hiding the code locally.
- `internal/quality/source_limits_test.go` owns file/function/`RunE`/statement/parameter/nesting/decision checks; `naming_test.go` owns globals, `init`, suppressions, generated markers, and forbidden names; `architecture_test.go` owns the complete Section 12.5 internal package DAG and adapter-constructor prohibition; `third_party_test.go` owns third-party import placement and exported-signature checks. Together they use `go/parser`, `go/types`, `go list`, and a repository walk, not best-effort grep. Checker unit tests synthesize invalid source snippets in temporary directories; no repository path is excluded from the live scan. Application packages may import lower packages solely for shared immutable request/result types while receiving all behavior through their local ports.
- `just quality` runs all quality tests plus `go vet` and `staticcheck`; `just check` includes `just quality`. A threshold exception requires an explicit plan amendment and owner approval rather than an inline allowlist.

### 12.2 Central deployment model

The purpose-split files in `internal/deployment` should establish explicit types equivalent to:

```go
type Scope struct {
    Group string // empty means root scope
}

type Layer string

const (
    LayerBase   Layer = "base"
    LayerDarwin Layer = "darwin"
    LayerLinux  Layer = "linux"
)

type FileKind string

const (
    FileOrdinary FileKind = "ordinary"
    FileSecret   FileKind = "secret"
)

type ManagedFile struct {
    Scope                  Scope
    Layer                  Layer
    Kind                   FileKind
    SourceAbsolutePath     string
    SourceRepositoryPath   string
    TargetRelativePath     string
    SourceExecutableBits   fs.FileMode
}

type Alias struct {
    Scope                       Scope
    Platform                    string
    CanonicalTargetRelativePath string
    AliasRelativePath           string
}

type HookPhase string

const (
    HookBefore HookPhase = "before"
    HookAfter  HookPhase = "after"
)

type Hook struct {
    Scope          Scope
    Phase          HookPhase
    Name           string
    AbsolutePath   string
    RepositoryPath string
}

type Plan struct {
    RepositoryRoot string
    Platform       string
    Groups         []string
    Files          []ManagedFile
    Aliases        []Alias
    Hooks          []Hook
}
```

All file, alias, and group slices are sorted deterministically before crossing package boundaries. `Hooks` is a validated descriptor set, not an execution-order promise. The apply orchestrator constructs two explicit sequences without rescanning: before hooks sort repository scope first, then groups lexically, then bytewise hook name; after hooks sort groups lexically first, repository scope last, then bytewise hook name. Tests assert both comparators independently.

### 12.3 Compiler phases

Repository compilation is pure with respect to target and state:

1. identify root files, root dot directories, groups, and controls;
2. scan base regular and secret candidates;
3. scan active platform regular and secret candidates;
4. resolve structural overlay precedence within each scope;
5. parse and activate routes;
6. validate hooks;
7. validate target and alias paths;
8. run global collision detection;
9. sort the final plan.

Run these phases for both Linux and Darwin during validation, with collisions isolated per platform; emit the runtime's plan for execution. Do not decrypt secrets or inspect `$HOME` during compilation.

### 12.4 Snapshot and reconciliation phases

Use an explicit pipeline:

```text
Deployment Plan
  -> Current Snapshots
  -> Classified Reconciliation Actions
  -> User-Resolved Action Plan
  -> Hook/Filesystem Executor
```

Suggested action enum:

```text
NoOp
CorrectMode
CreateTarget
WriteSourceToTarget
EstablishBaseline
NeedsDecision
RetireState
CreateAlias
ReplaceAlias
VerifyAlias
RetireAliasState
```

Every destructive action carries an immutable target precondition captured during snapshotting: absent/present state, `Lstat` type, filesystem identity when present, exact content hash for regular files, relevant read/write and executable mode bits, exact symlink payload for aliases, and identities of existing parent directories. Immediately before execution, verify all fields still match. If not, abort that operation rather than applying a stale user decision.

Every source-backed action carries a source precondition containing `Lstat` regular-file type, filesystem identity, raw source-storage hash, and executable bits. After all before hooks and before the first managed write, revalidate every selected source and target precondition as one pass. If anything changed, abort before Cattery executor mutations and require a fresh apply. Before each individual write, `Lstat` and reject non-regular sources, open the source, compare `fstat` identity to the path snapshot, read/hash the bytes, `fstat` and `Lstat` again, and compare type, identity, size, raw hash, and executable bits with the action snapshot. Revalidate the target immediately before rename. For an ordinary source, the validated bytes retained in the action-local buffer are the exact atomic-writer input; do not reopen or restream by path. For an encrypted secret, pass those exact validated ciphertext bytes to SOPS stdin, repeat the complete source identity/raw-hash check immediately after SOPS returns, and use the returned caller-owned plaintext as the exact atomic-writer input only if both checks match. This prevents SOPS from reopening a raced path and prevents an ordinary or secret source later in a long apply from bypassing the one-time post-hook check.

Define narrow consuming-package interfaces rather than letting CLI concepts leak downward:

```go
type DecisionResolver interface {
    Resolve(context.Context, []DecisionRequest) ([]Decision, error)
}

type AtomicWriter interface {
    Replace(context.Context, filesystem.ReplaceRequest) (filesystem.ReplaceResult, error)
}
```

The apply application package owns `DecisionRequest`, `Decision`, `DecisionClass`, `DecisionChoice`, `SafeDifference`, and `DecisionResolver`. It mechanically projects one `reconcile.DecisionSpec` and optional `diff.SafeRecord` into those application-owned values before calling the resolver. Neither lower package imports upward or carries source/target byte slices or secret plaintext. Backend `diff.SafeRecord` is Cattery-owned and tagged as `None`, `Text`, `Binary`, or `Secret`: `Text` carries precomputed printable unified-diff lines only, `Binary` carries ordinary-file sizes and hashes only, and `Secret` carries no content, size, hash, or SOPS metadata. Application `SafeDifference` preserves exactly that sanitized tag/payload contract without exposing a `diff` type to CLI. Neither representation contains a go-difflib type, ANSI sequence, terminal width, writer, or rendering callback. `filesystem.ReplaceRequest` carries destination root/path, byte source, final mode, and the complete precondition above. `filesystem.ReplaceResult` states whether rename and directory sync completed so the service can decide whether a baseline transaction is permitted. The application package owns only the consuming `AtomicWriter` interface; the request/result values live with the filesystem adapter, so the adapter never imports application. CLI implements `DecisionResolver` by importing `application/apply` only; `apply` owns orchestration and does not import CLI.

State repository methods own short transactions internally. A file-baseline transaction creates the repository row if needed and upserts exactly one file row; alias realization and retirement each use one analogous transaction. A representation-transition transaction is the sole multi-row exception: after the synced replacement, it activates the current file/alias row and retires the opposite-table row atomically. Callers never hold a `*sql.Tx` across filesystem or hook work, and no returned row or iterator outlives the method that owns it.

### 12.5 Dependency direction

```text
deployment, failure, pathsafe, subprocess, buildinfo
        ↑
repository, routes, state, secrets, hooks, filesystem
        ↑
reconcile, diff, selection
        ↑
application/initialize, application/validate, application/inspect,
application/apply, application/add, application/version
        ↑
cli
        ↑
bootstrap composition root
        ↑
cmd/cattery
```

The diagram describes policy flow, not permission for bootstrap to move behavior. The architecture test enforces this production import allowlist; omitted internal imports are forbidden rather than implicitly allowed:

| Package family | Internal packages it may import |
|---|---|
| `buildinfo`, `deployment`, `failure`, `pathsafe`, `subprocess` | none |
| `routes` | `deployment`, `pathsafe` |
| `state` | `deployment`, `pathsafe` |
| `secrets` | `failure`, `subprocess` |
| `hooks` | `deployment`, `subprocess` |
| `filesystem` | `deployment`, `pathsafe` |
| `repository` | `deployment`, `hooks`, `pathsafe`, `routes` |
| `reconcile` | `deployment`, `pathsafe`, `state`, `secrets` |
| `diff` | `deployment`, `reconcile` |
| `selection` | `deployment`, `pathsafe`, `state` |
| `application/initialize` | `failure`, `pathsafe`, `state` |
| `application/validate` | `deployment`, `failure`, `repository`, `selection` |
| `application/inspect` | `deployment`, `diff`, `failure`, `reconcile`, `repository`, `secrets`, `selection`, `state` |
| `application/apply` | `deployment`, `diff`, `failure`, `filesystem`, `hooks`, `pathsafe`, `reconcile`, `repository`, `secrets`, `selection`, `state` |
| `application/add` | `deployment`, `failure`, `filesystem`, `pathsafe`, `reconcile`, `repository`, `secrets`, `selection`, `state` |
| `application/version` | `buildinfo` |
| `cli` | `application/...`, `failure` only, subject to the per-file restrictions in Section 12.1 |
| `bootstrap` | the concrete packages needed for construction plus `cli`; no business call and no Cobra/pflag import |
| `cmd/cattery` | `bootstrap`, `cli`, `failure` |
| `testfixture/filesystem`, `testfixture/sops`, `quality` | none |
| `testfixture/database` | `state` |

Non-fixture production packages never import `internal/testfixture` or `internal/quality`. Tests beside one package may import that package's narrow test fixture; the `integration` test package may import production packages explicitly because its purpose is cross-package verification. The architecture test exempts `_test.go` files from the fixture-import restriction so each package's tests may import its narrow `testfixture/` family (e.g. `repository` tests import `testfixture/filesystem`, `secrets` tests import `testfixture/sops`, `state` tests import `testfixture/database`); production files never receive that exemption. The allowlist is directional: a listed lower package never imports an application, CLI, bootstrap, or command package.

Third-party imports are similarly exclusive: Cobra and `x/term` belong to `internal/cli`; SQLite, XDG, and flock to `internal/state`; TOML to `internal/routes`; BLAKE3 to `internal/deployment`; `x/text` to `internal/pathsafe`; and go-difflib to `internal/diff`. `pflag` and `mousetrap` remain indirect and are imported nowhere. No third-party concrete type may appear in an exported Cattery signature; `internal/quality/architecture_test.go` verifies exported signatures with `go/types`, package imports with `go/parser`, and the file-level purity rules in Section 12.1. The quality suite also checks that every external GitHub Actions `uses:` entry equals one of Section 13's full immutable commits; floating tags are forbidden.

Bootstrap imports the concrete application/adapters and CLI only to construct `cli.Dependencies`. Do not allow CLI, Cobra, pflag, rendering, or exit-code concepts into application, repository, state, reconciliation, filesystem, hook, or secret packages. Backend tests instantiate application services directly and never construct a Cobra command.

### 12.6 Logging and output

- Use `log/slog` for diagnostic logs.
- Default output is concise human-readable command output, not slog records.
- `--verbose` enables debug logs to stderr.
- Verbosity is applied by the root adapter through the injected per-application level callback after Cobra parsing and before the selected service call; it is not copied into domain requests or stored globally.
- Never log file contents, secret hashes, SOPS stdin/stdout, or full environment values.
- Errors wrap stable context such as operation and safe path, using `%w`.
- `internal/failure` defines presentation-neutral categories `InvalidInput`, `Operational`, `Difference`, `Hook`, and `Dependency`; it never contains numeric exit codes or Cobra types. Application services use `InvalidInput` for semantic request errors such as unknown/duplicate groups or incompatible explicit add options, and wrap lower failures with the other applicable category. `internal/cli/execute.go` combines those categories with Cobra syntax/arity errors and signal cancellation to implement Section 11.8 exactly.
- Render an error once at the outer CLI boundary. Backend packages add context and return errors but never print them.

---

## 13. Verified current dependencies

The dependency set below was resolved from the Go module proxy with each direct module requested as `@latest` on 2026-08-09. A subsequent `go list -m -u` reported no newer direct-module versions.

```text
github.com/pelletier/go-toml/v2 v2.4.3
modernc.org/sqlite                 v1.56.0
github.com/zeebo/blake3           v0.2.4
github.com/adrg/xdg               v0.5.3
github.com/gofrs/flock            v0.13.0
github.com/pmezard/go-difflib     v1.0.0
github.com/spf13/cobra            v1.10.2
golang.org/x/text                 v0.40.0
golang.org/x/term                 v0.45.0
```

With one production import for each owned direct package and no third-party test framework, Go 1.26.5 `go mod tidy` produced this exact indirect block; Task 1 pins it up front so `go.mod` has one lifetime owner, and final `go mod tidy -diff` must be empty:

```text
github.com/dustin/go-humanize                    v1.0.1
github.com/google/uuid                           v1.6.0
github.com/inconshreveable/mousetrap             v1.1.0
github.com/klauspost/cpuid/v2                     v2.0.12
github.com/mattn/go-isatty                       v0.0.24
github.com/ncruces/go-strftime                   v1.0.0
github.com/remyoudompheng/bigfft                 v0.0.0-20230129092748-24d4a6f8daec
github.com/spf13/pflag                           v1.0.9
golang.org/x/sys                                 v0.47.0
modernc.org/libc                                 v1.74.4
modernc.org/mathutil                             v1.7.1
modernc.org/memory                               v1.11.0
```

GitHub Actions are supply-chain pins too. The latest releases on 2026-08-09 resolved to these immutable commits:

| Action | Release | Required full commit |
|---|---|---|
| `actions/checkout` | `v7.0.1` | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | `v7.0.0` | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `cachix/install-nix-action` | `v31.11.0` | `630ae543ea3a38a9a4166f03376c02c50f408342` |

Workflow `uses:` entries pin these three full commits and include the release tag in a comment. Checkout uses `persist-credentials: false`; release jobs additionally use `fetch-depth: 0` so the annotated tag object is available for local verification. CI sets top-level `permissions: contents: read` and never uses `pull_request_target`. The release workflow separates a read-only build/smoke job from a tag-only publish job with `contents: write`; the publish job requires the read-only job to succeed, workflow-dispatch dry runs never instantiate the publish job, and no untrusted pull-request event can reach write permission. The initial workflows need no artifact upload/download or release action: the tag-only publish job checks out the same immutable tag, deterministically rebuilds and re-smokes all assets rather than trusting transferred files, then publishes the local paths with Nix-pinned `gh release create --verify-tag`.

The Nix input is `github:NixOS/nixpkgs/nixos-26.05` locked at commit `ee48b147c18c7de1e6ec97dc74792be42724bed1`, resolved on 2026-08-09. The current unstable revision was deliberately rejected because Nixpkgs 26.11 dropped `x86_64-darwin`; 26.05 is the last stable release supporting all four required systems. Every system evaluated the required shell tools at the versions below; `pkgs.go-tools` is the Staticcheck suite from staticcheck.io:

| Nix attribute | Version |
|---|---|
| `go_1_26` | `1.26.5` |
| `go_1_25` | `1.25.12` |
| `just` | `1.51.0` |
| `go-tools` | `2026.1` |
| `sops` | `3.13.3` |
| `age` | `1.3.1` |
| `python3` | `3.13.14` |
| `shellcheck` | `0.11.0` |
| `gh` | `2.97.0` |
| `gnutar` | `1.35` |
| `gzip` | `1.14` |

The flake exposes development shells and checks for `x86_64-linux`, `aarch64-linux`, `x86_64-darwin`, and `aarch64-darwin`. All eleven attributes above were evaluation-checked on all four systems. The exact lock entry uses `narHash = sha256-ZuvZfVW2tB5tXYEcpcfhVXB3N4OhdZLOqOviy4OD4b4=` and `lastModified = 1786089803`; implementation may not substitute a floating registry input. If 26.05 is outside its security-support window before implementation starts, Task 4 stops for a plan amendment that either proves a newer four-system pin or explicitly revises the supported system set.

The exact Go module set passed all of the following in a clean temporary module:

- `go mod tidy` and `go mod verify`;
- native `go test`, CGo-disabled build, and an executed runtime smoke test under Go 1.26.5;
- the same tidy, test, CGo-disabled build, runtime smoke test, and module verification under Go 1.25.12 from Nix;
- CGo-disabled cross-builds for Linux amd64/arm64 and Darwin amd64/arm64 under Go 1.26.5;
- actual Cobra root/subcommand execution, persistent and command-local flags before/after the subcommand and interspersed between positional arguments, `--` termination, explicit `Flags().Changed` detection, disabled completion, TOML strict decoding, unkeyed and keyed BLAKE3 hashing, unified diff generation, advisory lock/unlock, XDG state resolution, Unicode NFC normalization, terminal detection, and in-memory SQLite create/insert/query operations.

`modernc.org/sqlite v1.56.0`, `golang.org/x/text v0.40.0`, and `golang.org/x/term v0.45.0` require Go 1.25.0, the highest minimum among the direct modules. Therefore use `go 1.25.0` in `go.mod`, test the latest Go 1.25 patch as the compatibility floor, and test current stable Go 1.26.x as the primary toolchain. Do not raise the module floor merely because development uses a newer patch release.

The external tool pair was independently checked against the latest GitHub releases on 2026-08-09:

```text
getsops/sops v3.13.3
FiloSottile/age v1.3.1
```

The official SOPS v3.13.3 Linux binary and age v1.3.1 completed an exact binary encrypt/decrypt round trip through the planned stdin/stdout invocation, `.sops.yaml` filename override, JSON ciphertext, and ephemeral age identity. The 27-byte fixture included NUL and invalid UTF-8 bytes, and plaintext was absent from the encrypted JSON. SOPS v3.13.3 also exposes the required `--filename-override`, `--input-type`, and `--output-type` flags.

Pin these external versions in CI and secret integration tests. Do not run `sops --version` on every command or parse its human output. A launch failure caused by no executable is dependency exit 4. Once SOPS launches, any nonzero operation, including an older binary rejecting a required flag, is a safe operational exit 1 that reports the exit status and recommends the verified v3.13.3+ interface without parsing or echoing potentially secret-bearing stderr.

Notes:

- `modernc.org/sqlite` avoids CGo and supports portable binaries, at the cost of a larger dependency and compile footprint.
- `go-difflib` v1.0.0 is old but remains the latest published module version; it is tiny, stable, pure Go, and narrowly sufficient for unified diffs.
- `gofrs/flock` v0.13.0 is the latest validated version and is the version implementation must use.
- Cobra v1.10.2 is the only CLI framework. Its pflag and mousetrap modules remain indirect; production code imports neither directly.
- Immediately before Task 1, repeat direct `@latest` resolution and the complete compatibility matrix above. Immediately before Task 4, re-resolve the pinned nixpkgs revision and four-system package availability. A changed pin requires a plan-only amendment before implementation. Immediately before Task 115, re-resolve the three workflow-action release tags and commits; any change likewise requires a plan-only amendment before the workflow card starts. After Task 1/4 freeze their files, a future dependency update is separate roadmap work with explicit ownership of `go.mod`, `go.sum`, `flake.lock`, and any affected source; no current implementation or release card may reopen them opportunistically.
- Do not add an ORM, Bubble Tea, `huh`, another CLI/UI framework, a web framework, or an async runtime without a demonstrated need.
- Standard library packages should cover traversal, subprocesses, atomic operations, prompting, structured logging, and most tests.

---

## 14. Concurrency, failure, and recovery

### 14.1 Determinism

- Scan and output order is bytewise lexical by normalized target path.
- Group order is lexical.
- Hook names are bytewise lexical within the explicit repository/group phase order from Section 10.3.
- Files apply in target-path order; aliases follow files in alias-path order.
- Initial implementation is sequential. Dotfile volume does not justify concurrent writes or hashing.

### 14.2 Process concurrency

- One mutating Cattery process per state directory.
- Every command that opens state (`init`, `validate`, `status`, `diff`, `apply`, and `add`) acquires the same exclusive advisory lock. The expected workload is small, and one rule is safer than exposing inconsistent read snapshots. `version` does not acquire it.
- Cattery cannot lock applications. Hash/type preconditions protect against an application changing a target after the prompt.

### 14.3 Partial failure

There is no rollback.

- Before-hook failure: no planned Cattery executor or managed state-row changes; Section 8.5 operational state-store setup and trusted-hook side effects are not rolled back.
- Mid-filesystem failure: earlier successful files and state remain; later files do not run; after hooks are skipped.
- After-hook failure: all Cattery file changes remain, state accurately records what Cattery installed, remaining after hooks continue, and final verification reports any hook-induced source/target/alias drift.
- State write failure after a successful file rename: stop; next run recovers from source/target equality.
- Alias failure after files: earlier files remain; state is accurate for completed work.

All errors clearly report that a partial apply may have occurred and list completed versus pending actions when known.

### 14.4 Signals

`cmd/cattery/main.go` uses `signal.Notify`, `context.WithCancelCause`, and the typed `failure.Interruption` from Task 2 so the first SIGINT or SIGTERM cancels the root context with an unambiguous `Interrupt` or `Terminate` cause. Pass that context to application services, SOPS, and hooks and apply the process-group shutdown policy in Section 10.4. Do not begin a new file mutation after cancellation. If cancellation arrives before rename, remove the temporary entry and stop. Once rename succeeds, the operation is irrevocable: derive a five-second bounded cleanup context from `context.WithoutCancel(rootContext)`, attempt parent-directory sync and the one corresponding state transaction, then stop regardless of later actions. A cleanup timeout reports the rename as partial and leaves recovery to source/target equality. `internal/cli.Execute` inspects the joined error and `context.Cause`: return 130 for `Interrupt`, 143 for `Terminate`, and 1 for an ordinary non-signal context cancellation.

---

## 15. Test strategy

Tests use temporary repository, home, and XDG state directories. Never read or write the developer's real home or state.

### 15.1 Unit tests

Create focused table tests for:

- path lexical validation and containment;
- root/group classification;
- group-name NFC/case-fold portability collisions;
- unknown underscore controls being ignored;
- VCS metadata exclusions;
- overlay file/file, file/directory, directory/file, and directory/directory cases;
- normal/secret same-layer errors;
- empty/malformed secret JSON storage-shape errors without decryption;
- cross-scope file, parent/child, case-folded, and NFC/NFD-normalized collisions;
- route TOML strict decoding and platform activation;
- exact relative alias payload calculation and rejection of absolute or merely resolution-equivalent payloads;
- BLAKE3 exact-byte behavior;
- permission calculation and copy-on-write mode correction for multiply linked targets;
- blocking parent entries and file-to-directory/directory-to-file temporal transitions;
- text/binary/large/secret diff behavior;
- complete reconciliation matrix, including missing and unexpected target types;
- exit-code categorization.

### 15.2 State tests

For every migration and state method:

- resolve canonical state paths, enforce exact directory/file modes, and reject state-file symlink/special entries;
- initialize empty DB;
- reopen existing DB;
- reject unknown future schema version;
- register repository/home pairs and switch defaults independently per home;
- upsert applied file and alias rows;
- atomically switch active state between file and alias representations at one target;
- reject a corrupted snapshot with both representations active at one target;
- retire and reactivate rows;
- select and retire state-only groups, and preserve baselines across cross-scope ownership moves;
- generate, persist, and validate `hash.key` identifiers; fail on missing/mismatched keys when secret rows exist;
- prove equal low-entropy secret plaintext produces keyed fingerprints rather than plain BLAKE3 digests;
- verify secret plaintext is absent from every text/blob column;
- verify per-file committed state survives later failures;
- verify concurrent mutator lock rejection.

### 15.3 SOPS adapter tests

Use a fake `sops` executable placed first in a temporary `PATH`:

- assert exact arguments, working directory, and stdin/stdout routing;
- round-trip arbitrary bytes including NUL and invalid UTF-8;
- simulate missing executable, nonzero exit, cancellation, malformed output, large stderr, and stderr that repeats plaintext;
- discard partial stdout on failure and reject syntax-valid ciphertext whose decrypt round trip differs;
- scan captured CLI/log output to ensure plaintext never appears;
- verify no plaintext is written to a general temporary directory, every same-directory secret target temporary file is removed on target-write failure, and retained in-memory plaintext buffers are cleared on every return path.

Add a real integration test using an ephemeral age identity to prove actual binary-mode round trips. It may skip in a bare local `go test` when `sops` and `age` are unavailable, but Nix checks and CI must install both tools and require this test to run and pass.

### 15.4 Hook tests

- repository/group order across multiple groups;
- hooks run on no-op apply;
- before failure causes zero target mutations;
- after failure preserves files/state and runs remaining after hooks;
- non-executable and nested hook entries fail validation;
- environment and working directory are exact;
- cancellation reaches child process;
- post-hook source, source-mode, target-mode, target-content, and alias-payload changes are all reported as unresolved without rewriting baselines;
- `--no-hooks` and `--dry-run` execute none.

### 15.5 Application and CLI boundary tests

Test the seam in three independent layers:

1. Application-service tests construct `initialize`, `validate`, `inspect`, `apply`, `add`, and `version` services directly with fake consuming interfaces. They pass typed requests, assert typed results/failure categories, and never construct `cli.Application` or import Cobra.
2. CLI adapter tests construct the opaque `cli.Application` with one-method fake services. For every command, assert exact request mapping, argument order, explicit flag-presence bits, one service invocation, typed-result rendering, writer failures, and categorized error propagation. Help, unknown-command/flag, arity, and unsupported `--repo` cases must invoke zero services. Test root flags before and after the subcommand, disabled completion/suggestions, `SilenceErrors`, `SilenceUsage`, and one-time error rendering/exit mapping.
3. Architecture tests enforce the import and source-size rules from Section 12.1, including the absence of Cobra/pflag imports outside `internal/cli` and the absence of concrete backend imports inside command adapters.

Build `cmd/cattery` once and run the resulting executable as a subprocess with isolated `HOME`, XDG state, repository, `PATH`, stdin/stdout/stderr, and process environment. In-process `cli.Execute` tests may complement this suite but cannot substitute for it. The black-box suite must assert real exit statuses, signal behavior, TTY/non-TTY handling, inherited hook streams, process-group cleanup, and build metadata. Cover:

1. init existing and missing repositories;
2. first apply to empty home;
3. root and grouped paths;
4. all groups by default and one selected group;
5. source-only update;
6. target-only drift with diff/overwrite/skip/abort;
7. simultaneous source/target conflict;
8. source and target independently changing to equal content;
9. `add` adopting drift and updating baseline;
10. database deletion with equal and unequal existing targets;
11. source removal leaving target untouched;
12. platform overlay selection;
13. secret add/apply and mode enforcement;
14. executable-bit-only changes;
15. expected and occupied aliases, plus explicit temporal file-to-alias and alias-to-file transitions;
16. rejection of both internal and escaping parent symlinks, plus source symlink and special-file rejection;
17. exact relative alias payloads, absolute/wrong aliases, and occupied-path decisions;
18. Unicode NFC/NFD collisions and hard-linked target mode correction without changing the other link;
19. state-only group retirement and root/group ownership moves preserving the baseline;
20. file-to-directory and directory-to-file transitions failing complete preflight with zero hooks/writes;
21. source bytes, source executable bits, target modes, and target bytes racing before hooks and between two writes;
22. prompt whitespace/case/invalid-answer retry, `abort`, EOF, non-TTY, skip, and diff loops;
23. every dry-run action verb, decision rendering, exit 2, zero repository/target/hook or managed state-row mutation, and only the operational state-store effects allowed by Section 8.5;
24. explicit repository/default/environment precedence, deferred registration, unknown groups, duplicate group/file arguments, hard-linked add arguments, and deleted state-only groups;
25. rejection of inactive-platform `add` and baseline correctness for active base/platform layers;
26. missing SOPS and hash-key failures leaving repository, targets, and state rows untouched;
27. after-hook source/target/alias drift and mixed skip/hook/error exit-code precedence;
28. SIGINT 130 and SIGTERM 143 during hooks, SOPS, pre-rename writes, and post-rename bounded cleanup;
29. injected write, file-sync, rename, directory-sync, and state-commit failures at every mandatory boundary, including accurate reporting without deletion of newly created parent directories;
30. deterministic output over repeated runs.

### 15.6 Required verification commands

The final project provides these `just` recipes and keeps them green:

```text
just fmt-check
just quality
just vet
just test
just test-race
just staticcheck
just build
just test-integration
just test-sops
just deps-check
just check
nix flake check
```

`just check` runs formatting, architecture/source-limit quality gates, vet, unit/application/state tests, black-box integration tests, staticcheck, build, and the local dependency-tidy guard; `just deps-check` additionally performs the networked update audit for release preparation. `just test-sops` is mandatory in Nix checks and CI with pinned real SOPS/age. CI runs native jobs on explicit `ubuntu-22.04` and `macos-15` labels, never moving `*-latest` labels. Race tests may run on Linux only if macOS runtime cost is excessive.

---

## 16. Implementation roadmap

The roadmap is a contract-first set of bounded Jira cards. One agent or human owns one card from a recorded failing test through green review. A card consumes only the frozen outputs of its listed dependencies and may not redesign an adjacent package.

### Dispatch prerequisite

Before Task 1, the integration owner commits this exact `PLAN.md` snapshot alone and records its SHA-256 and commit. No worker starts from an untracked or moving plan. Any normative change requires a new plan-only commit and invalidates every not-yet-started base assignment.

### Universal card contract

Every card must:

1. start from the exact integration commit containing every listed dependency;
2. edit exactly the fully qualified paths in `Owns`—no bare paths, directories, globs, shared lifetime ownership, or invented files;
3. write the named focused test first, run it, and record the expected failure;
4. prove the selector is nonempty with the exact `go test -list ... | grep '^Test'` prefix in the card's `Verify` command before the green run;
5. run the card's `Verify` command and, once Task 5 is in the base, `go test ./internal/quality`, `just fmt-check`, and `just quality` with zero skipped core tests; Tasks 1-4 use only their named verifiers because the immutable quality tooling does not yet exist;
6. stage exactly `Owns`, record `git diff --cached --name-only` equal to that ownership set, and prove `git status --short` contains no other path;
7. keep every production/test file and function inside Section 12.1 limits;
8. commit only its owned paths with the listed subject; and
9. report the base commit, plan hash, exact changed paths, red/green outputs, named assertions, and frozen API summary.

Documentation, workflow, module, and shell cards use their named deterministic verifier instead of `go test -list`; they still provide red/green evidence and exact changed paths. A mismatch, missing API, or needed new file stops the card and becomes a plan amendment by the integration owner. Integration cards never patch production code.

### Delivery lanes

| Tasks | Frozen contract family |
|---|---|
| 1-6 | module, failures, build information, immutable tooling, source/architecture gates |
| 7-13 | deployment values, paths, and subprocess lifecycle |
| 14-25 | state contracts, lifecycle, keys, and rows |
| 26-35 | repository fixtures, controls, scanner, routes, hooks, collisions, compiler |
| 36-45 | SOPS and filesystem mutation adapters |
| 46-58 | snapshots, pure reconciliation, safe diffs, selection, hook execution |
| 59-83 | independently callable application services and phase contracts |
| 84-94 | Cobra-only adapters, opaque root, and centralized exit mapping |
| 95-98 | composition root and process entrypoint |
| 99-114 | direct-backend and black-box acceptance families |
| 115-118 | CI, deterministic packaging, artifact smoke, publication |
| 119-125 | deterministic documentation checks and one documentation artifact per card |

The exact direct prerequisites below, not the ranges above, determine readiness.

### Task 1: Pin the module and dependency set

**Depends on:** none.

**Owns:** `go.mod`, `go.sum`, `.gitignore`, `LICENSE`.

**Deliverable:** Create module `github.com/alyraffauf/cattery` at `go 1.25.0`, pin all nine direct modules plus their final indirect graph from Section 13 exactly, and use the standard MIT License text with copyright `2026 Aly Raffauf`; no later card edits these files. Populate `go.sum` with `go mod download all`; do not run `go mod tidy` before imports exist. `.gitignore` contains only `/.direnv/`, `/result`, `/dist/`, and `/coverage.out` so credentials are never hidden by a broad pattern.

**Tests:** Module graph verification only.

**Acceptance:** Every direct pin and indirect marker is exact, all transitive sums verify, the MIT text is exact without repeated source headers, and the four ignore entries are exact. Task 4's final `deps-check` recipe reserves `go mod tidy -diff` for use after the production imports exist; ordinary per-card `quality` does not run it early.

**Verify:** `go mod download all && go mod verify && go list -m all` passes.

**Commit:** `chore: initialize cattery module`.

### Task 2: Define presentation-neutral failures

**Depends on:** 1.

**Owns:** `internal/failure/error.go`, `internal/failure/error_test.go`.

**Deliverable:** Define the five failure kinds, `failure.New`, `failure.HasKind`, interruption signals, and joined-error traversal without numeric statuses.

**Tests:** `TestFailureContract` covers wrapping, `errors.Is`/`errors.As`, joins, and signal causes.

**Acceptance:** No CLI import or precedence policy appears in this package.

**Verify:** `go test ./internal/failure -list '^TestFailureContract$' | grep '^TestFailureContract$' && go test ./internal/failure -run '^TestFailureContract$'` passes.

**Commit:** `feat: define failure vocabulary`.

### Task 3: Define build information

**Depends on:** 1.

**Owns:** `internal/buildinfo/info.go`, `internal/buildinfo/info_test.go`.

**Deliverable:** Define the exact linker-populated `Version`, `Commit`, and `BuildTimestamp` strings with defaults `dev`, `unknown`, and `unknown`, plus an immutable typed snapshot of Go/runtime fields.

**Tests:** `TestBuildInformation` covers exact development defaults, injected tag/full-SHA/RFC-3339 UTC values, malformed release timestamps, and runtime fields.

**Acceptance:** Only the three Section 12.1 linker variables are package variables.

**Verify:** `go test ./internal/buildinfo -list '^TestBuildInformation$' | grep '^TestBuildInformation$' && go test ./internal/buildinfo -run '^TestBuildInformation$'` passes.

**Commit:** `feat: define build information`.

### Task 4: Pin the development and command environment

**Depends on:** 1.

**Owns:** `flake.nix`, `flake.lock`, `justfile`.

**Deliverable:** Use the exact Section 13 nixpkgs revision, lock hash, four-system outputs, and tool versions to provide the final immutable Nix shell and `just` recipes for both Go toolchains, Staticcheck, SOPS/age, Python, ShellCheck, GitHub CLI, GNU tar/gzip, `go mod tidy -diff`, quality, tests, credential/document checks, SOPS integration, packaging, and smoke checks.

**Tests:** Nix evaluation and recipe inventory.

**Acceptance:** All four systems evaluate, the shell exposes every pinned tool at the exact Section 13 version, recipes are static from this card onward, and no recipe downloads an unpinned tool.

**Verify:** `nix flake check --no-build && nix develop -c just --list` passes; executing the full check is intentionally deferred until every frozen import exists.

**Commit:** `chore: pin development toolchain`.

### Task 5: Enforce source shape and naming

**Depends on:** 2, 3, 4.

**Owns:** `internal/quality/scan_test.go`, `internal/quality/shape_test.go`, `internal/quality/source_limits_test.go`, `internal/quality/naming_test.go`.

**Deliverable:** Provide shared repository-walk, parse, violation-reporting, and AST shape-measurement helpers plus checks for file/function/RunE/statement/nesting/decision/parameter/interface limits, globals, `init`, names, suppressions, and the two exact global exceptions.

**Tests:** `TestSourceShapeChecker` and `TestNamingChecker` synthesize one failing Go snippet per AST/naming rule and an over-limit file for every scanned non-Go extension plus `justfile` in temporary directories.

**Acceptance:** Every bad snippet reports its named rule, every corrected snippet passes, and the live tree has no excluded path.

**Verify:** `go test ./internal/quality -list '^Test(SourceShape|Naming)Checker$' | grep '^Test' && go test ./internal/quality -run '^Test(SourceShape|Naming)Checker$'` passes.

**Commit:** `test: enforce source shape`.

### Task 6: Enforce package and dependency seams

**Depends on:** 5.

**Owns:** `internal/quality/architecture_test.go`, `internal/quality/third_party_test.go`.

**Deliverable:** Enforce the complete Section 12.5 internal DAG, file-level purity rules, concrete-constructor bans, exclusive third-party ownership, application-owned CLI DTOs, no third-party concrete types in exported signatures, and exact full-commit workflow-action pins.

**Tests:** `TestArchitectureChecker` and `TestThirdPartyChecker` cover every allowed and forbidden edge/type using temporary modules, including an application CLI DTO exposing a backend type, plus allowed SHA-pinned and forbidden floating/unknown workflow actions.

**Acceptance:** Cobra/pflag escape, an undeclared same-layer edge, an upward edge, adapter construction in application, an application CLI DTO exposing a backend type, exported third-party types, and a floating or unknown workflow action each fail distinctly.

**Verify:** `go test ./internal/quality -list '^Test(Architecture|ThirdParty)Checker$' | grep '^Test' && go test ./internal/quality -run '^Test(Architecture|ThirdParty)Checker$'` passes.

**Commit:** `test: enforce package seams`.

### Task 7: Define immutable deployment records

**Depends on:** 6.

**Owns:** `internal/deployment/scope.go`, `internal/deployment/file.go`, `internal/deployment/alias.go`, `internal/deployment/plan.go`, `internal/deployment/scope_test.go`, `internal/deployment/file_test.go`, `internal/deployment/alias_test.go`, `internal/deployment/plan_test.go`.

**Deliverable:** Freeze scope, layer, file, alias, hook, and platform-plan values from Section 12.2 with defensive-copy constructors.

**Tests:** `TestScopeContract`, `TestManagedFileContract`, `TestAliasContract`, and `TestPlanContract`.

**Acceptance:** Enums reject unknown values; root scope is explicit; plans cannot be mutated through caller-owned slices.

**Verify:** `go test ./internal/deployment -list '^Test(Scope|ManagedFile|Alias|Plan)Contract$' | grep '^Test' && go test ./internal/deployment -run '^Test(Scope|ManagedFile|Alias|Plan)Contract$'` passes.

**Commit:** `feat: define deployment records`.

### Task 8: Implement ordinary and secret fingerprints

**Depends on:** 7.

**Owns:** `internal/deployment/hash.go`, `internal/deployment/hash_test.go`.

**Deliverable:** Wrap BLAKE3 v0.2.4 behind Cattery digest types for ordinary bytes, keyed secret semantics, raw secret storage, and the domain-separated key identifier.

**Tests:** `TestFingerprintVectors` uses known keyed/unkeyed vectors and low-entropy secret cases.

**Acceptance:** No BLAKE3 concrete type escapes and no unkeyed plaintext-secret digest API exists.

**Verify:** `go test ./internal/deployment -list '^TestFingerprintVectors$' | grep '^TestFingerprintVectors$' && go test ./internal/deployment -run '^TestFingerprintVectors$'` passes.

**Commit:** `feat: implement deployment fingerprints`.

### Task 9: Implement deterministic deployment sorting

**Depends on:** 7.

**Owns:** `internal/deployment/sort.go`, `internal/deployment/sort_test.go`.

**Deliverable:** Define bytewise stable comparators for scopes, targets, aliases, hooks, actions, and result records.

**Tests:** `TestDeploymentOrdering` covers empty root, Unicode bytes, stable ties, and both hook phases.

**Acceptance:** Repeated permutations produce byte-identical ordering without locale or map iteration dependence.

**Verify:** `go test ./internal/deployment -list '^TestDeploymentOrdering$' | grep '^TestDeploymentOrdering$' && go test ./internal/deployment -run '^TestDeploymentOrdering$'` passes.

**Commit:** `feat: sort deployment records`.

### Task 10: Validate lexical paths and portable equivalence

**Depends on:** 6.

**Owns:** `internal/pathsafe/path.go`, `internal/pathsafe/equivalence.go`, `internal/pathsafe/path_test.go`, `internal/pathsafe/equivalence_test.go`.

**Deliverable:** Implement strict slash-relative parsing, group grammar, and pairwise per-segment NFC plus `EqualFold` comparison without cleaning or lowercase keys.

**Tests:** `TestLexicalPath` and `TestPortableEquivalence` cover separators, traversal, controls, spaces, Unicode, case, and NFC/NFD.

**Acceptance:** Accepted values round-trip exactly; rejected values cannot become canonical by cleaning.

**Verify:** `go test ./internal/pathsafe -list '^Test(LexicalPath|PortableEquivalence)$' | grep '^Test' && go test ./internal/pathsafe -run '^Test(LexicalPath|PortableEquivalence)$'` passes.

**Commit:** `feat: validate lexical paths`.

### Task 11: Resolve canonical roots and ancestors

**Depends on:** 10.

**Owns:** `internal/pathsafe/root.go`, `internal/pathsafe/ancestor.go`, `internal/pathsafe/root_test.go`, `internal/pathsafe/ancestor_test.go`.

**Deliverable:** Resolve canonical HOME/repository/state roots and missing suffixes through the nearest existing ancestor while rejecting every symlinked or blocking parent.

**Tests:** `TestCanonicalRoot` and `TestAncestorWalk` cover existing/missing paths, internal/escaping symlinks, files, and special entries.

**Acceptance:** Returned roots and parent identities are immutable and no path is created by validation.

**Verify:** `go test -race ./internal/pathsafe -list '^Test(CanonicalRoot|AncestorWalk)$' | grep '^Test' && go test -race ./internal/pathsafe -run '^Test(CanonicalRoot|AncestorWalk)$'` passes.

**Commit:** `feat: resolve canonical path roots`.

### Task 12: Protect trees and filesystem identities

**Depends on:** 11.

**Owns:** `internal/pathsafe/protected.go`, `internal/pathsafe/identity.go`, `internal/pathsafe/protected_test.go`, `internal/pathsafe/identity_test.go`.

**Deliverable:** Reject repository/state overlap, source-target identity, hard-link aliases, parent-child ownership, and portable case/NFC equivalents.

**Tests:** `TestProtectedTree` and `TestFilesystemIdentity` cover ancestors, descendants, same objects, links, and missing endpoints.

**Acceptance:** All checks are read-only and return typed facts rather than mutate or render.

**Verify:** `go test -race ./internal/pathsafe -list '^Test(ProtectedTree|FilesystemIdentity)$' | grep '^Test' && go test -race ./internal/pathsafe -run '^Test(ProtectedTree|FilesystemIdentity)$'` passes.

**Commit:** `feat: protect deployment trees`.

### Task 13: Run cancellable subprocesses

**Depends on:** 6.

**Owns:** `internal/subprocess/run.go`, `internal/subprocess/process_unix.go`, `internal/subprocess/run_test.go`, `internal/subprocess/process_unix_test.go`.

**Deliverable:** Provide synchronous execution with explicit cwd/environment/streams, Unix process groups, TERM then five-second grace then KILL, and typed launch/exit facts; capture/redaction policy stays with callers.

**Tests:** `TestProcessRun` and `TestProcessCancellation` cover normal/nonzero/missing/cancel-before/cancel-after/descendant cases.

**Acceptance:** No package global, secret policy, hook policy, or retained output buffer exists.

**Verify:** `go test -race ./internal/subprocess -list '^TestProcess(Run|Cancellation)$' | grep '^Test' && go test -race ./internal/subprocess -run '^TestProcess(Run|Cancellation)$'` passes.

**Commit:** `feat: run cancellable subprocesses`.

### Task 14: Freeze state DTOs

**Depends on:** 2, 7, 8, 12.

**Owns:** `internal/state/types.go`, `internal/state/types_test.go`.

**Deliverable:** Define Cattery-owned repository/file/alias/key/transaction DTOs and concrete store method values without SQL, XDG, or flock types. Consumer-owned interfaces are declared only by later packages that call the store.

**Tests:** `TestStateContract` validates enums, path forms, digest widths, defensive copies, and the absence of provider-owned interfaces.

**Acceptance:** The package exports no provider-owned interface; later consumers can define one- to three-method roles over concrete store methods.

**Verify:** `go test ./internal/state -list '^TestStateContract$' | grep '^TestStateContract$' && go test ./internal/state -run '^TestStateContract$'` passes.

**Commit:** `feat: freeze state contracts`.

### Task 15: Open the SQLite database safely

**Depends on:** 14.

**Owns:** `internal/state/database.go`, `internal/state/database_test.go`.

**Deliverable:** Resolve the canonical XDG state path, enforce entry modes/types, open modernc SQLite v1.56.0 with one connection, and set required PRAGMAs without locking or migration.

**Tests:** `TestDatabaseOpen` covers path modes, WAL/foreign keys/busy policy, symlink/special rejection, and cleanup after open failures.

**Acceptance:** Opening returns a Cattery-owned handle and creates no managed row.

**Verify:** `go test -race ./internal/state -list '^TestDatabaseOpen$' | grep '^TestDatabaseOpen$' && go test -race ./internal/state -run '^TestDatabaseOpen$'` passes.

**Commit:** `feat: open state database`.

### Task 16: Acquire the state advisory lock

**Depends on:** 14.

**Owns:** `internal/state/lock.go`, `internal/state/lock_test.go`.

**Deliverable:** Wrap gofrs/flock v0.13.0 with immediate exclusive acquisition, exact lock-file safety, PID diagnostics, and idempotent release.

**Tests:** `TestStateLock` covers contention, malformed/symlink/special lock entries, cancellation, and release failures.

**Acceptance:** No database is opened while lock acquisition fails.

**Verify:** `go test -race ./internal/state -list '^TestStateLock$' | grep '^TestStateLock$' && go test -race ./internal/state -run '^TestStateLock$'` passes.

**Commit:** `feat: lock state directory`.

### Task 17: Apply embedded state migrations

**Depends on:** 15.

**Owns:** `internal/state/migrations.go`, `internal/state/migrations/001_initial.sql`, `internal/state/migrations_test.go`.

**Deliverable:** Embed the exact Section 8 schema, transact `PRAGMA user_version`, and reject unknown newer schemas without lock or store orchestration.

**Tests:** `TestStateMigration` covers fresh, current, rollback, interrupted, and newer-version databases.

**Acceptance:** `initialMigrationSQL` is the sole checked embed variable and migrations are idempotent.

**Verify:** `go test -race ./internal/state -list '^TestStateMigration$' | grep '^TestStateMigration$' && go test -race ./internal/state -run '^TestStateMigration$'` passes.

**Commit:** `feat: migrate state schema`.

### Task 18: Assemble the state store lifecycle

**Depends on:** 15, 16, 17.

**Owns:** `internal/state/store.go`, `internal/state/store_test.go`.

**Deliverable:** Compose canonical path, lock, database open, migration, and close in the only permitted order behind lazy service-time acquisition.

**Tests:** `TestStoreLifecycle` injects failure at each boundary and verifies cleanup/order.

**Acceptance:** Constructors remain side-effect-free; acquisition creates no repository or managed row.

**Verify:** `go test -race ./internal/state -list '^TestStoreLifecycle$' | grep '^TestStoreLifecycle$' && go test -race ./internal/state -run '^TestStoreLifecycle$'` passes.

**Commit:** `feat: assemble state store`.

### Task 19: Provide an isolated database fixture

**Depends on:** 18.

**Owns:** `internal/testfixture/database/store.go`, `internal/testfixture/database/store_test.go`.

**Deliverable:** Provide one narrow test-only factory for isolated HOME/state/store trees with deterministic clocks and complete cleanup.

**Tests:** `TestDatabaseFixture` proves two fixtures share no path, lock, connection, key, or clock state.

**Acceptance:** Only tests import this package and cleanup removes every created path.

**Verify:** `go test -race ./internal/testfixture/database -list '^TestDatabaseFixture$' | grep '^TestDatabaseFixture$' && go test -race ./internal/testfixture/database -run '^TestDatabaseFixture$'` passes.

**Commit:** `test: add isolated state fixture`.

### Task 20: Persist repository selection rows

**Depends on:** 18, 19.

**Owns:** `internal/state/repositories.go`, `internal/state/repositories_test.go`.

**Deliverable:** Implement canonical `(repository, home)` registration, non-registering lookup, one default per home, and deterministic snapshots.

**Tests:** `TestRepositoryRows` covers two homes/repos, default replacement, deleted DB, timestamps, and rollback.

**Acceptance:** Explicit lookup never registers and every transaction is method-local.

**Verify:** `go test -race ./internal/state -list '^TestRepositoryRows$' | grep '^TestRepositoryRows$' && go test -race ./internal/state -run '^TestRepositoryRows$'` passes.

**Commit:** `feat: persist repository rows`.

### Task 21: Create and validate the hash-key file

**Depends on:** 8, 18.

**Owns:** `internal/state/keyfile.go`, `internal/state/keyfile_test.go`.

**Deliverable:** Implement deferred crypto-random 32-byte exclusive creation, mode `0600`, file/directory sync, and strict existing-file validation.

**Tests:** `TestHashKeyFile` covers creation races, malformed lengths/modes/types, interrupted writes, and no key diagnostics.

**Acceptance:** No key is created before a secret baseline requires it.

**Verify:** `go test -race ./internal/state -list '^TestHashKeyFile$' | grep '^TestHashKeyFile$' && go test -race ./internal/state -run '^TestHashKeyFile$'` passes.

**Commit:** `feat: manage hash key file`.

### Task 22: Persist key identity and recovery

**Depends on:** 21.

**Owns:** `internal/state/keyid.go`, `internal/state/recovery.go`, `internal/state/keyid_test.go`, `internal/state/recovery_test.go`.

**Deliverable:** Derive/persist `hash_key_id` and implement exact missing/mismatch recovery rules based on whether secret rows exist.

**Tests:** `TestHashKeyIdentity` and `TestHashKeyRecovery` cover key-only, metadata-only, stale metadata, replacement, and secret-row cases.

**Acceptance:** Recovery never guesses a key and never emits key or plaintext-derived data.

**Verify:** `go test -race ./internal/state -list '^TestHashKey(Identity|Recovery)$' | grep '^Test' && go test -race ./internal/state -run '^TestHashKey(Identity|Recovery)$'` passes.

**Commit:** `feat: recover hash key identity`.

### Task 23: Persist file baselines and retirement

**Depends on:** 20, 22.

**Owns:** `internal/state/files.go`, `internal/state/files_read.go`, `internal/state/files_decode.go`, plus `internal/state/files_test.go`, `internal/state/files_retire_test.go`.

**Deliverable:** Implement active/retired file reads, ordinary/keyed-secret baselines, raw storage hashes, modes, reactivation, state-only scopes, and per-file transactions.

**Tests:** `TestFileRows` covers cross-scope ownership, selected retirement, producer checks, secret columns, and commit rollback.

**Acceptance:** No plaintext column exists and no transaction spans filesystem work.

**Verify:** `go test -race ./internal/state -list '^TestFileRows$' | grep '^TestFileRows$' && go test -race ./internal/state -run '^TestFileRows$'` passes.

**Commit:** `feat: persist file baselines`.

### Task 24: Persist alias rows

**Depends on:** 20, 23.

**Owns:** `internal/state/aliases.go`, `internal/state/aliases_read.go`, `internal/state/aliases_decode.go`, plus `internal/state/aliases_test.go`, `internal/state/aliases_retire_test.go`.

**Deliverable:** Implement alias realization, retirement, reactivation, deterministic diagnostics, and corruption checks within the alias table.

**Tests:** `TestAliasRows` covers exact payloads, retired rows, reactivation, rollback, and duplicate active paths.

**Acceptance:** This card does not switch file representations.

**Verify:** `go test -race ./internal/state -list '^TestAliasRows$' | grep '^TestAliasRows$' && go test -race ./internal/state -run '^TestAliasRows$'` passes.

**Commit:** `feat: persist alias rows`.

### Task 25: Persist representation transitions

**Depends on:** 23, 24.

**Owns:** `internal/state/transitions.go`, `internal/state/transitions_test.go`.

**Deliverable:** Atomically retire one table and activate the other for file-to-alias and alias-to-file transitions, including cross-table active uniqueness validation.

**Tests:** `TestRepresentationTransition` covers success, rollback, skipped transitions, and pre-existing dual-active corruption.

**Acceptance:** Exactly one active representation exists after commit; failures preserve the prior active row.

**Verify:** `go test -race ./internal/state -list '^TestRepresentationTransition$' | grep '^TestRepresentationTransition$' && go test -race ./internal/state -run '^TestRepresentationTransition$'` passes.

**Commit:** `feat: persist representation transitions`.

### Task 26: Provide isolated filesystem fixtures

**Depends on:** 10.

**Owns:** `internal/testfixture/filesystem/tree.go`, `internal/testfixture/filesystem/tree_test.go`.

**Deliverable:** Provide one narrow test-only tree builder for files, directories, symlinks, hard links, modes, and injected mutation points.

**Tests:** `TestFilesystemFixture` proves exact object creation, mutation hooks, isolation, and cleanup.

**Acceptance:** The package contains no product path policy and production code cannot import it.

**Verify:** `go test -race ./internal/testfixture/filesystem -list '^TestFilesystemFixture$' | grep '^TestFilesystemFixture$' && go test -race ./internal/testfixture/filesystem -run '^TestFilesystemFixture$'` passes.

**Commit:** `test: add filesystem fixture`.

### Task 27: Classify repository controls

**Depends on:** 7, 10.

**Owns:** `internal/repository/controls.go`, `internal/repository/controls_test.go`.

**Deliverable:** Classify the exact known scope-root controls, ignored unknown underscore entries, literal nested underscores, and metadata exclusions.

**Tests:** `TestRepositoryControls` covers every known/unknown/nested/VCS case.

**Acceptance:** The classifier performs no directory traversal or platform resolution.

**Verify:** `go test ./internal/repository -list '^TestRepositoryControls$' | grep '^TestRepositoryControls$' && go test ./internal/repository -run '^TestRepositoryControls$'` passes.

**Commit:** `feat: classify repository controls`.

### Task 28: Scan literal repository sources

**Depends on:** 26, 27.

**Owns:** `internal/repository/scan.go`, `internal/repository/scan_test.go`.

**Deliverable:** Scan root/group base candidates and raw hook candidates with strict regular-file types and deterministic source paths.

**Tests:** `TestRepositoryScan` covers root dot trees, groups, empty files, controls, symlinks, specials, and group collisions.

**Acceptance:** Scanning performs no overlay, route, hook semantic, target, state, or SOPS work.

**Verify:** `go test ./internal/repository -list '^TestRepositoryScan$' | grep '^TestRepositoryScan$' && go test ./internal/repository -run '^TestRepositoryScan$'` passes.

**Commit:** `feat: scan repository sources`.

### Task 29: Resolve platform and secret overlays

**Depends on:** 7, 28.

**Owns:** `internal/repository/overlay.go`, `internal/repository/overlay_test.go`, `internal/repository/secrets_test.go`.

**Deliverable:** Resolve deterministic base/Linux/Darwin structural layers and ordinary/secret replacement before global collisions.

**Tests:** `TestRepositoryOverlay` and `TestSecretOverlay` cover all file/directory combinations, additions, replacements, and inactive layers.

**Acceptance:** The output is immutable candidates only and no target/SOPS access occurs.

**Verify:** `go test ./internal/repository -list '^Test(Repository|Secret)Overlay$' | grep '^Test' && go test ./internal/repository -run '^Test(Repository|Secret)Overlay$'` passes.

**Commit:** `feat: resolve repository overlays`.

### Task 30: Decode strict route declarations

**Depends on:** 7, 10.

**Owns:** `internal/routes/config.go`, `internal/routes/config_test.go`.

**Deliverable:** Strictly decode `_routes.toml` version, keys, lexical paths, and declaration duplicates without source lookup or platform activation.

**Tests:** `TestRouteDecode` covers valid forms, unknown keys/versions, duplicate keys, and malformed paths.

**Acceptance:** No TOML concrete type crosses the package API.

**Verify:** `go test ./internal/routes -list '^TestRouteDecode$' | grep '^TestRouteDecode$' && go test ./internal/routes -run '^TestRouteDecode$'` passes.

**Commit:** `feat: decode route declarations`.

### Task 31: Activate routes and aliases

**Depends on:** 11, 29, 30.

**Owns:** `internal/routes/activate.go`, `internal/routes/activate_test.go`, `internal/routes/aliases_test.go`.

**Deliverable:** Resolve canonical sources, platform activation, HOME-relative destinations, and exact relative alias payloads.

**Tests:** `TestRouteActivation` and `TestAliasDeclaration` cover absent/wrong-layer/directory/self/absolute/textually-wrong cases.

**Acceptance:** Activation returns deployment records and performs no target access.

**Verify:** `go test ./internal/routes -list '^Test(RouteActivation|AliasDeclaration)$' | grep '^Test' && go test ./internal/routes -run '^Test(RouteActivation|AliasDeclaration)$'` passes.

**Commit:** `feat: activate deployment routes`.

### Task 32: Discover trusted hook descriptors

**Depends on:** 7, 28.

**Owns:** `internal/hooks/discover.go`, `internal/hooks/discover_test.go`.

**Deliverable:** Validate direct regular executable hook children and emit immutable descriptors without process imports or execution.

**Tests:** `TestHookDiscovery` covers nested/nonregular/nonexecutable/selected-scope cases.

**Acceptance:** The file-level purity rule prevents subprocess and OS mutation imports.

**Verify:** `go test ./internal/hooks -list '^TestHookDiscovery$' | grep '^TestHookDiscovery$' && go test ./internal/hooks -run '^TestHookDiscovery$'` passes.

**Commit:** `feat: discover trusted hooks`.

### Task 33: Order trusted hooks

**Depends on:** 32.

**Owns:** `internal/hooks/order.go`, `internal/hooks/order_test.go`.

**Deliverable:** Implement independent before/after bytewise comparators for repository and selected group hooks.

**Tests:** `TestHookOrdering` covers repository-before-groups, groups-before-repository, group/name ties, and both phases.

**Acceptance:** Ordering is pure and never rescans the repository.

**Verify:** `go test ./internal/hooks -list '^TestHookOrdering$' | grep '^TestHookOrdering$' && go test ./internal/hooks -run '^TestHookOrdering$'` passes.

**Commit:** `feat: order trusted hooks`.

### Task 34: Reject compiled-plan collisions

**Depends on:** 12, 29, 31.

**Owns:** `internal/repository/collisions.go`, `internal/repository/collisions_test.go`.

**Deliverable:** Reject file/file, file/alias, alias/alias, parent/child, case/NFC, protected-tree, and cross-scope ownership collisions.

**Tests:** `TestCompiledCollision` exhaustively names each collision family and legal shared parents.

**Acceptance:** The engine is pure; existing target identity and temporal races remain runtime preflight concerns.

**Verify:** `go test ./internal/repository -list '^TestCompiledCollision$' | grep '^TestCompiledCollision$' && go test ./internal/repository -run '^TestCompiledCollision$'` passes.

**Commit:** `feat: reject plan collisions`.

### Task 35: Assemble deterministic platform plans

**Depends on:** 9, 29, 31, 33, 34.

**Owns:** `internal/repository/compiler.go`, `internal/repository/compiler_test.go`.

**Deliverable:** Compose the exact nine Section 12.3 phases for Linux and Darwin using frozen scanner, route, hook, collision, and sorting contracts.

**Tests:** `TestPlanCompilation` uses Linux/Darwin golden values, determinism permutations, and invalid unselected scopes.

**Acceptance:** Compiler has no target, state, SOPS, hook-execution, or write dependency.

**Verify:** `go test ./internal/repository -list '^TestPlanCompilation$' | grep '^TestPlanCompilation$' && go test ./internal/repository -run '^TestPlanCompilation$'` passes.

**Commit:** `feat: compile deployment plans`.

### Task 36: Provide a controllable SOPS fixture

**Depends on:** 13.

**Owns:** `internal/testfixture/sops/executable.go`, `internal/testfixture/sops/executable_test.go`.

**Deliverable:** Build one test-only executable fixture that records argv/cwd/stdin and emits controlled binary stdout/stderr/exit/cancellation behavior.

**Tests:** `TestSOPSExecutableFixture` covers arbitrary bytes, large output, descendants, and cleanup.

**Acceptance:** The fixture contains no real credential and production code cannot import it.

**Verify:** `go test -race ./internal/testfixture/sops -list '^TestSOPSExecutableFixture$' | grep '^TestSOPSExecutableFixture$' && go test -race ./internal/testfixture/sops -run '^TestSOPSExecutableFixture$'` passes.

**Commit:** `test: add sops executable fixture`.

### Task 37: Invoke the SOPS process safely

**Depends on:** 2, 13, 36.

**Owns:** `internal/secrets/client.go`, `internal/secrets/client_test.go`.

**Deliverable:** Own exact SOPS executable lookup, repository cwd, bounded capture, safe launch/exit diagnostics, redaction, cancellation, and buffer clearing.

**Tests:** `TestSOPSClient` covers missing/nonzero/large/plaintext stderr, working directory, environment, and descendants.

**Acceptance:** No captured byte enters an error; missing executable is `Dependency`, launched failures are `Operational`.

**Verify:** `go test -race ./internal/secrets -list '^TestSOPSClient$' | grep '^TestSOPSClient$' && go test -race ./internal/secrets -run '^TestSOPSClient$'` passes.

**Commit:** `feat: invoke sops safely`.

### Task 38: Encrypt binary secret candidates

**Depends on:** 37.

**Owns:** `internal/secrets/encrypt.go`, `internal/secrets/encrypt_test.go`.

**Deliverable:** Implement exact binary-input/JSON-output encryption from caller-owned bytes through `/dev/stdin`, with repository-relative filename override and strict nonempty JSON output.

**Tests:** `TestSOPSEncrypt` asserts exact argv/stdin, arbitrary binary input, malformed/partial output, and buffer clearing.

**Acceptance:** No plaintext is written to a general temporary file or returned in diagnostics.

**Verify:** `go test -race ./internal/secrets -list '^TestSOPSEncrypt$' | grep '^TestSOPSEncrypt$' && go test -race ./internal/secrets -run '^TestSOPSEncrypt$'` passes.

**Commit:** `feat: encrypt binary secrets`.

### Task 39: Decrypt repository secret sources

**Depends on:** 37.

**Owns:** `internal/secrets/decrypt.go`, `internal/secrets/decrypt_test.go`.

**Deliverable:** Implement exact JSON-input/binary-output decryption from caller-validated ciphertext bytes through `/dev/stdin`, with repository-relative filename override and caller-owned plaintext buffers; never reopen the repository source path.

**Tests:** `TestSOPSDecrypt` covers arbitrary bytes, empty plaintext, wrong JSON, cancellation, and every clear-on-return path.

**Acceptance:** Plaintext ownership is explicit and no reusable object retains it.

**Verify:** `go test -race ./internal/secrets -list '^TestSOPSDecrypt$' | grep '^TestSOPSDecrypt$' && go test -race ./internal/secrets -run '^TestSOPSDecrypt$'` passes.

**Commit:** `feat: decrypt repository secrets`.

### Task 40: Validate encrypted secret candidates

**Depends on:** 38, 39.

**Owns:** `internal/secrets/candidate.go`, `internal/secrets/candidate_test.go`.

**Deliverable:** Round-trip candidate JSON through decrypt and require byte-exact equality before adoption, then clear all candidate plaintext buffers.

**Tests:** `TestSecretCandidate` covers equality/mismatch/empty/malformed/partial/failure and invalid UTF-8 plus NUL bytes.

**Acceptance:** The only durable output is validated encrypted JSON.

**Verify:** `go test -race ./internal/secrets -list '^TestSecretCandidate$' | grep '^TestSecretCandidate$' && go test -race ./internal/secrets -run '^TestSecretCandidate$'` passes.

**Commit:** `feat: validate encrypted candidates`.

### Task 41: Freeze filesystem preconditions

**Depends on:** 7, 12, 14.

**Owns:** `internal/filesystem/precondition.go`, `internal/filesystem/target.go`, `internal/filesystem/source.go`, `internal/filesystem/freeze.go`, `internal/filesystem/parents.go`, plus `internal/filesystem/precondition_test.go`, `internal/filesystem/revalidate_test.go`, `internal/filesystem/source_test.go`, `internal/filesystem/helpers_test.go`.

**Deliverable:** Define and revalidate immutable source/target/parent identity, type, content, mode, and alias-payload tokens.

**Tests:** `TestFilesystemPrecondition` covers every stale field, absent/present transition, symlink, hard link, and blocking parent.

**Acceptance:** No mutation occurs and third-party or application types do not escape.

**Verify:** `go test -race ./internal/filesystem -list '^TestFilesystemPrecondition$' | grep '^TestFilesystemPrecondition$' && go test -race ./internal/filesystem -run '^TestFilesystemPrecondition$'` passes.

**Commit:** `feat: freeze filesystem preconditions`.

### Task 42: Sync replacement directories

**Depends on:** 41.

**Owns:** `internal/filesystem/sync.go`, `internal/filesystem/sync_test.go`.

**Deliverable:** Implement explicit file and parent-directory sync/close sequencing with precise pre/post-commit result facts.

**Tests:** `TestDirectorySync` injects open/sync/close failures and cancellation.

**Acceptance:** Callers can distinguish no rename, renamed-but-unsynced, and fully durable results.

**Verify:** `go test -race ./internal/filesystem -list '^TestDirectorySync$' | grep '^TestDirectorySync$' && go test -race ./internal/filesystem -run '^TestDirectorySync$'` passes.

**Commit:** `feat: sync replacement directories`.

### Task 43: Replace regular files atomically

**Depends on:** 41, 42.

**Owns:** `internal/filesystem/replace.go`, `internal/filesystem/replace_test.go`, `internal/filesystem/replace_failure_test.go`.

**Deliverable:** Perform same-directory create/write/mode/file-sync/close/revalidate/rename/directory-sync with cancellation cleanup.

**Tests:** `TestAtomicReplace` injects every boundary and covers final symlinks, old-target preservation, temp cleanup, created parents, and partial results.

**Acceptance:** The exact validated byte source is written once and state is never touched.

**Verify:** `go test -race ./internal/filesystem -list '^TestAtomicReplace$' | grep '^TestAtomicReplace$' && go test -race ./internal/filesystem -run '^TestAtomicReplace$'` passes.

**Commit:** `feat: replace regular files atomically`.

### Task 44: Apply target mode policy

**Depends on:** 43.

**Owns:** `internal/filesystem/mode.go`, `internal/filesystem/mode_test.go`, `internal/filesystem/hardlink_test.go`.

**Deliverable:** Apply ordinary executable-bit preservation and exact secret `0600`/`0700`, using replacement rather than shared-inode chmod for hard links.

**Tests:** `TestTargetMode` and `TestHardLinkMode` cover new/existing, executable-only, restrictive umask, and multiply linked targets.

**Acceptance:** No mode operation mutates an unmanaged inode alias.

**Verify:** `go test -race ./internal/filesystem -list '^Test(TargetMode|HardLinkMode)$' | grep '^Test' && go test -race ./internal/filesystem -run '^Test(TargetMode|HardLinkMode)$'` passes.

**Commit:** `feat: enforce target modes`.

### Task 45: Realize aliases atomically

**Depends on:** 41, 42.

**Owns:** `internal/filesystem/alias.go`, `internal/filesystem/alias_test.go`.

**Deliverable:** Create, verify, and replace exact relative symlink entries without following final referents.

**Tests:** `TestAliasRealization` covers missing/exact/absolute/wrong/dangling/occupied/special/parent-race/cancellation cases.

**Acceptance:** Only the link entry changes and directory durability is reported precisely.

**Verify:** `go test -race ./internal/filesystem -list '^TestAliasRealization$' | grep '^TestAliasRealization$' && go test -race ./internal/filesystem -run '^TestAliasRealization$'` passes.

**Commit:** `feat: realize explicit aliases`.

### Task 46: Freeze reconciliation records

**Depends on:** 7, 14, 41.

**Owns:** `internal/reconcile/types.go`, `internal/reconcile/types_test.go`.

**Deliverable:** Define immutable source/target/state snapshot, action, reason, convergence, precondition, and decision-spec values; no provider-owned interface lives in this package.

**Tests:** `TestReconciliationContract` covers enum validity, defensive copies, and the absence of adapter or provider-interface types.

**Acceptance:** Records contain no reusable secret plaintext, SQL handle, writer, callback, CLI value, or provider-owned interface.

**Verify:** `go test ./internal/reconcile -list '^TestReconciliationContract$' | grep '^TestReconciliationContract$' && go test ./internal/reconcile -run '^TestReconciliationContract$'` passes.

**Commit:** `feat: freeze reconciliation records`.

### Task 47: Capture source snapshots

**Depends on:** 8, 35, 40, 46.

**Owns:** `internal/reconcile/source_snapshot.go`, `internal/reconcile/source_snapshot_test.go`.

**Deliverable:** Capture exact ordinary bytes or secret raw-storage identity plus short-lived keyed semantics with Lstat/open/fstat/post-read checks.

**Tests:** `TestSourceSnapshot` covers identity races, mode changes, malformed secrets, decrypt-on-demand, and buffer clearing.

**Acceptance:** No target/state access or classification occurs.

**Verify:** `go test -race ./internal/reconcile -list '^TestSourceSnapshot$' | grep '^TestSourceSnapshot$' && go test -race ./internal/reconcile -run '^TestSourceSnapshot$'` passes.

**Commit:** `feat: snapshot deployment sources`.

### Task 48: Capture target snapshots

**Depends on:** 12, 41, 46.

**Owns:** `internal/reconcile/target_snapshot.go`, `internal/reconcile/target_snapshot_test.go`, `internal/reconcile/precondition_test.go`.

**Deliverable:** Capture absent/regular/symlink/special targets, parent and object identities, bytes/modes, alias payloads, and executable preconditions.

**Tests:** `TestTargetSnapshot` and `TestSnapshotPrecondition` cover hard links, blocking ancestors, parent races, and final symlinks.

**Acceptance:** No source/state access or mutation occurs.

**Verify:** `go test -race ./internal/reconcile -list '^Test(TargetSnapshot|SnapshotPrecondition)$' | grep '^Test' && go test -race ./internal/reconcile -run '^Test(TargetSnapshot|SnapshotPrecondition)$'` passes.

**Commit:** `feat: snapshot deployment targets`.

### Task 49: Capture persisted-state snapshots

**Depends on:** 25, 46.

**Owns:** `internal/reconcile/state_snapshot.go`, `internal/reconcile/state_records.go`, `internal/reconcile/state_snapshot_test.go`.

**Deliverable:** Convert active/retired/state-only file and alias rows into immutable evaluation records and reject cross-table corruption.

**Tests:** `TestPersistedStateSnapshot` covers current/retired/deleted scopes, inactive platform rows, and dual-active paths.

**Acceptance:** No filesystem access or state mutation occurs.

**Verify:** `go test -race ./internal/reconcile -list '^TestPersistedStateSnapshot$' | grep '^TestPersistedStateSnapshot$' && go test -race ./internal/reconcile -run '^TestPersistedStateSnapshot$'` passes.

**Commit:** `feat: snapshot persisted state`.

### Task 50: Assemble immutable evaluation snapshots

**Depends on:** 47, 48, 49.

**Owns:** `internal/reconcile/snapshot.go`, `internal/reconcile/snapshot_test.go`.

**Deliverable:** Join sorted source, target, and state observations for one complete selected platform plan without classification.

**Tests:** `TestSnapshotAssembly` covers deterministic joins, missing producers, representation pairs, and defensive-copy behavior.

**Acceptance:** Secret plaintext is cleared before the reusable snapshot returns.

**Verify:** `go test -race ./internal/reconcile -list '^TestSnapshotAssembly$' | grep '^TestSnapshotAssembly$' && go test -race ./internal/reconcile -run '^TestSnapshotAssembly$'` passes.

**Commit:** `feat: assemble evaluation snapshots`.

### Task 51: Classify file reconciliation

**Depends on:** 46, 50.

**Owns:** `internal/reconcile/classify_file.go`, `internal/reconcile/classify_file_test.go`.

**Deliverable:** Purely classify the complete source/target/baseline matrix for regular files, modes, and unbaselined safety.

**Tests:** `TestFileClassification` is an exhaustive Cartesian table including database loss and source-only/target-only changes.

**Acceptance:** No target-to-source action, adapter import, or side effect exists.

**Verify:** `go test ./internal/reconcile -list '^TestFileClassification$' | grep '^TestFileClassification$' && go test ./internal/reconcile -run '^TestFileClassification$'` passes.

**Commit:** `feat: classify file reconciliation`.

### Task 52: Classify alias and representation changes

**Depends on:** 46, 50.

**Owns:** `internal/reconcile/classify_alias.go`, `internal/reconcile/classify_alias_test.go`.

**Deliverable:** Purely classify exact/wrong/occupied aliases and intact/drifted file-to-alias or alias-to-file transitions.

**Tests:** `TestAliasClassification` exhausts representation and target-state combinations.

**Acceptance:** Automatic transition is emitted only when the old representation is intact.

**Verify:** `go test ./internal/reconcile -list '^TestAliasClassification$' | grep '^TestAliasClassification$' && go test ./internal/reconcile -run '^TestAliasClassification$'` passes.

**Commit:** `feat: classify alias reconciliation`.

### Task 53: Classify retirement

**Depends on:** 46, 50.

**Owns:** `internal/reconcile/classify_retirement.go`, `internal/reconcile/classify_retirement_test.go`.

**Deliverable:** Purely classify source removal, whole deleted scopes, moved ownership, inactive platforms, and already-retired rows without target deletion.

**Tests:** `TestRetirementClassification` covers complete-plan producer checks and selected subsets.

**Acceptance:** Retirement changes tracking only and never authorizes target removal or overwrite.

**Verify:** `go test ./internal/reconcile -list '^TestRetirementClassification$' | grep '^TestRetirementClassification$' && go test ./internal/reconcile -run '^TestRetirementClassification$'` passes.

**Commit:** `feat: classify state retirement`.

### Task 54: Validate reconciliation decisions

**Depends on:** 51, 52, 53.

**Owns:** `internal/reconcile/decisions.go`, `internal/reconcile/decisions_test.go`.

**Deliverable:** Produce pure ordered decision specs and validate only choices allowed by each action/reason.

**Tests:** `TestDecisionSpecification` covers overwrite/skip/abort/diff eligibility, order, and invalid choices.

**Acceptance:** No prompt, renderer, or target-to-source choice exists.

**Verify:** `go test ./internal/reconcile -list '^TestDecisionSpecification$' | grep '^TestDecisionSpecification$' && go test ./internal/reconcile -run '^TestDecisionSpecification$'` passes.

**Commit:** `feat: validate reconciliation decisions`.

### Task 55: Build secret-safe diff records

**Depends on:** 8, 46, 50.

**Owns:** `internal/diff/safe.go`, `internal/diff/safe_test.go`.

**Deliverable:** Build the exact tagged `SafeRecord` variants for printable text, binary/large ordinary files, metadata-only changes, and secrets.

**Tests:** `TestSafeDiffRecord` covers controls, bidi, invalid UTF-8, size limits, escaped labels, hashes, and secret zero fields.

**Acceptance:** No ANSI, terminal width, writer, raw secret, or go-difflib type escapes.

**Verify:** `go test ./internal/diff -list '^TestSafeDiffRecord$' | grep '^TestSafeDiffRecord$' && go test ./internal/diff -run '^TestSafeDiffRecord$'` passes.

**Commit:** `feat: build safe diff records`.

### Task 56: Resolve repository selection

**Depends on:** 11, 20.

**Owns:** `internal/selection/repository.go`, `internal/selection/repository_test.go`.

**Deliverable:** Apply explicit raw path plus presence, raw `CATTERY_REPO` plus presence, then canonical-HOME default precedence without implicit registration.

**Tests:** `TestRepositorySelection` covers relative paths, presence bits, empty env, absent defaults, two homes, and canonical results.

**Acceptance:** The component returns typed identity and imports no CLI or concrete state constructor.

**Verify:** `go test ./internal/selection -list '^TestRepositorySelection$' | grep '^TestRepositorySelection$' && go test ./internal/selection -run '^TestRepositorySelection$'` passes.

**Commit:** `feat: resolve repository selection`.

### Task 57: Resolve group selection

**Depends on:** 23, 24, 25, 35, 56.

**Owns:** `internal/selection/groups.go`, `internal/selection/groups_test.go`.

**Deliverable:** Implement `CompiledOnly` and `CompiledAndPersisted` with exact duplicate/unknown checks and current/active/retired state-only semantics.

**Tests:** `TestGroupSelection` covers no args, explicit order, root-only, case variants, current, active, retired, deleted, and inactive-platform rows.

**Acceptance:** Selections are sorted typed values and no compiler execution or mutation occurs.

**Verify:** `go test ./internal/selection -list '^TestGroupSelection$' | grep '^TestGroupSelection$' && go test ./internal/selection -run '^TestGroupSelection$'` passes.

**Commit:** `feat: resolve group selection`.

### Task 58: Execute trusted hooks

**Depends on:** 13, 33.

**Owns:** `internal/hooks/execute.go`, `internal/hooks/execute_test.go`, `internal/hooks/cancellation_test.go`.

**Deliverable:** Execute ordered hooks synchronously with exact cwd/environment, inherited streams, before-stop, after-aggregate, result status, and process-group cancellation.

**Tests:** `TestHookExecution` and `TestHookCancellation` cover no-op/dry-run/no-hooks suppression, failures, output, and descendants.

**Acceptance:** No secret-specific environment or captured-stream policy enters hooks.

**Verify:** `go test -race ./internal/hooks -list '^TestHook(Execution|Cancellation)$' | grep '^Test' && go test -race ./internal/hooks -run '^TestHook(Execution|Cancellation)$'` passes.

**Commit:** `feat: execute trusted hooks`.

### Task 59: Implement repository initialization service

**Depends on:** 2, 12, 18, 20.

**Owns:** `internal/application/initialize/types.go`, `internal/application/initialize/service.go`, `internal/application/initialize/types_test.go`, `internal/application/initialize/service_test.go`.

**Deliverable:** Freeze typed request/result and implement missing/existing repository creation, canonicalization, overlap checks, and registration.

**Tests:** `TestInitializeContract` and `TestInitializeService` cover cwd default, non-directory, creation recheck, defaults, no scaffolding, and failures.

**Acceptance:** The service is Cobra-free and constructor-side-effect-free.

**Verify:** `go test -race ./internal/application/initialize -list '^TestInitialize(Contract|Service)$' | grep '^Test' && go test -race ./internal/application/initialize -run '^TestInitialize(Contract|Service)$'` passes.

**Commit:** `feat: initialize repositories through service`.

### Task 60: Implement repository validation service

**Depends on:** 2, 35, 56, 57.

**Owns:** `internal/application/validate/types.go`, `internal/application/validate/service.go`, `internal/application/validate/types_test.go`, `internal/application/validate/service_test.go`.

**Deliverable:** Freeze typed request/result and compile full Linux/Darwin plans while reporting selected counts and secret JSON storage shape.

**Tests:** `TestValidateContract` and `TestValidateService` cover precedence, groups, invalid unselected scopes, no registration, and exact two records.

**Acceptance:** No target, SOPS, hook execution, prompt, or renderer is reachable.

**Verify:** `go test ./internal/application/validate -list '^TestValidate(Contract|Service)$' | grep '^Test' && go test ./internal/application/validate -run '^TestValidate(Contract|Service)$'` passes.

**Commit:** `feat: validate repositories through service`.

### Task 61: Evaluate inspection state

**Depends on:** 2, 35, 40, 47, 48, 49, 50, 51, 52, 53, 55, 57.

**Owns:** `internal/application/inspect/types.go`, `internal/application/inspect/service.go`, `internal/application/inspect/types_test.go`, `internal/application/inspect/service_test.go`.

**Deliverable:** Freeze inspection DTOs/ports and perform one immutable selection, compile, snapshot, and classification evaluation with on-demand secret semantics.

**Tests:** `TestInspectionContract` and `TestInspectionEvaluation` cover state-only selection, decrypt conditions, determinism, and injected failures.

**Acceptance:** No status/diff rendering, hook, prompt, registration, or mutation occurs.

**Verify:** `go test -race ./internal/application/inspect -list '^TestInspection(Contract|Evaluation)$' | grep '^Test' && go test -race ./internal/application/inspect -run '^TestInspection(Contract|Evaluation)$'` passes.

**Commit:** `feat: evaluate inspection state`.

### Task 62: Produce status results

**Depends on:** 61.

**Owns:** `internal/application/inspect/status.go`, `internal/application/inspect/status_test.go`.

**Deliverable:** Translate one evaluation into sorted semantic status/retired records, counts, convergence, and `Difference` error.

**Tests:** `TestStatusService` covers files, aliases, retirement-only drift, convergence, and partial evaluation errors.

**Acceptance:** No diff generation or formatting occurs.

**Verify:** `go test -race ./internal/application/inspect -list '^TestStatusService$' | grep '^TestStatusService$' && go test -race ./internal/application/inspect -run '^TestStatusService$'` passes.

**Commit:** `feat: produce status results`.

### Task 63: Produce diff results

**Depends on:** 55, 61.

**Owns:** `internal/application/inspect/diff.go`, `internal/application/inspect/diff_test.go`.

**Deliverable:** Translate the same evaluation into sorted safe diff/status records, counts, convergence, and `Difference` error.

**Tests:** `TestDiffService` proves status parity, text/binary/secret behavior, alias-only drift, and no secret fields.

**Acceptance:** No terminal formatting or second snapshot occurs.

**Verify:** `go test -race ./internal/application/inspect -list '^TestDiffService$' | grep '^TestDiffService$' && go test -race ./internal/application/inspect -run '^TestDiffService$'` passes.

**Commit:** `feat: produce diff results`.

### Task 64: Freeze apply contracts

**Depends on:** 2, 46, 54, 55, 57.

**Owns:** `internal/application/apply/types.go`, `internal/application/apply/types_test.go`.

**Deliverable:** Define repository input, request/result/action-plan, application-owned decision class/choice/safe-difference request/response DTOs, and narrow phase ports used by apply; no exported CLI-facing DTO mentions a backend package type.

**Tests:** `TestApplyContract` validates presence bits, defensive copies, partial summaries, interface shapes, and zero CLI/adapter types.

**Acceptance:** `DecisionRequest` preserves the sanitized decision/diff contract after projection but exposes only application-owned field types; CLI can prompt by importing `application/apply` alone.

**Verify:** `go test ./internal/application/apply -list '^TestApplyContract$' | grep '^TestApplyContract$' && go test ./internal/application/apply -run '^TestApplyContract$'` passes.

**Commit:** `feat: freeze apply contracts`.

### Task 65: Evaluate apply candidates

**Depends on:** 35, 40, 47, 48, 49, 50, 51, 52, 53, 64.

**Owns:** `internal/application/apply/evaluate.go`, `internal/application/apply/evaluate_test.go`.

**Deliverable:** Perform selection input mapping, compile, source/target/state snapshotting, and pure classification into immutable candidates.

**Tests:** `TestApplyEvaluation` covers no args/state-only scopes, invalid unselected scopes, secret demand, races, and deterministic order.

**Acceptance:** No dependency probe, decision callback, hook, write, or registration occurs.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyEvaluation$' | grep '^TestApplyEvaluation$' && go test -race ./internal/application/apply -run '^TestApplyEvaluation$'` passes.

**Commit:** `feat: evaluate apply candidates`.

### Task 66: Preflight apply dependencies

**Depends on:** 37, 65.

**Owns:** `internal/application/apply/dependencies.go`, `internal/application/apply/dependencies_test.go`.

**Deliverable:** Determine whether selected candidates require SOPS and preflight only required external capabilities before decisions.

**Tests:** `TestApplyDependencyPreflight` covers ordinary-only, unchanged secret, changed secret, missing executable, and launched failures.

**Acceptance:** No version probing, state registration, prompt, hook, or mutation occurs.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyDependencyPreflight$' | grep '^TestApplyDependencyPreflight$' && go test -race ./internal/application/apply -run '^TestApplyDependencyPreflight$'` passes.

**Commit:** `feat: preflight apply dependencies`.

### Task 67: Collect apply decisions

**Depends on:** 54, 55, 65.

**Owns:** `internal/application/apply/decisions.go`, `internal/application/apply/decisions_test.go`.

**Deliverable:** Build safe ordered decision requests, call the injected resolver for every unresolved candidate, and validate responses before any hook.

**Tests:** `TestApplyDecisionCollection` covers abort/skip/overwrite/diff, invalid responses, EOF/noninteractive errors from resolver, and complete collection ordering.

**Acceptance:** Policy stays in application; resolver owns only presentation IO.

**Verify:** `go test ./internal/application/apply -list '^TestApplyDecisionCollection$' | grep '^TestApplyDecisionCollection$' && go test ./internal/application/apply -run '^TestApplyDecisionCollection$'` passes.

**Commit:** `feat: collect apply decisions`.

### Task 68: Prepare immutable apply plans

**Depends on:** 66, 67.

**Owns:** `internal/application/apply/prepare.go`, `internal/application/apply/prepare_test.go`.

**Deliverable:** Combine resolved candidates into immutable sorted action plans and dry-run records with stable verbs and counts.

**Tests:** `TestApplyPreparation` covers dry-run, noninteractive refusal, skip/abort, no-op, required hooks, and zero registration on refusal.

**Acceptance:** No hook or managed mutation occurs.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyPreparation$' | grep '^TestApplyPreparation$' && go test -race ./internal/application/apply -run '^TestApplyPreparation$'` passes.

**Commit:** `feat: prepare apply plans`.

### Task 69: Revalidate all sources before apply

**Depends on:** 41, 47, 68.

**Owns:** `internal/application/apply/source_guard.go`, `internal/application/apply/source_guard_test.go`.

**Deliverable:** After before hooks, revalidate every selected source and target precondition as one complete gate before the first managed mutation.

**Tests:** `TestApplySourceGuard` covers ordinary/secret storage, modes, target/parent identities, hook edits, and races.

**Acceptance:** Any mismatch stops with zero executor or managed-row change.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplySourceGuard$' | grep '^TestApplySourceGuard$' && go test -race ./internal/application/apply -run '^TestApplySourceGuard$'` passes.

**Commit:** `feat: guard apply sources`.

### Task 70: Execute apply file actions

**Depends on:** 23, 43, 44, 69.

**Owns:** `internal/application/apply/execute_files.go`, `internal/application/apply/execute_files_test.go`.

**Deliverable:** Execute regular-file create/update/mode/baseline actions sequentially with per-target rechecks, durable writes, short state commits, and partial results, including the alias-to-file representation switch when the retained row is an active alias.

**Tests:** `TestApplyFileExecution` injects every filesystem/state boundary and covers equality recovery and action-local secret clearing.

**Acceptance:** A later failure preserves accurate earlier state and never imports target bytes.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyFileExecution$' | grep '^TestApplyFileExecution$' && go test -race ./internal/application/apply -run '^TestApplyFileExecution$'` passes.

**Commit:** `feat: execute apply file actions`.

### Task 71: Execute alias and retirement actions

**Depends on:** 24, 25, 45, 69.

**Owns:** `internal/application/apply/execute_aliases.go`, `internal/application/apply/execute_aliases_test.go`.

**Deliverable:** Execute alias create/replace, file-alias state transitions, and tracking retirement without deployed-target deletion.

**Tests:** `TestApplyAliasExecution` covers intact/drifted transitions, alias races, state rollback, deleted scopes, and continuation.

**Acceptance:** Only durable representation changes switch active state.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyAliasExecution$' | grep '^TestApplyAliasExecution$' && go test -race ./internal/application/apply -run '^TestApplyAliasExecution$'` passes.

**Commit:** `feat: execute apply alias actions`.

### Task 72: Orchestrate apply hooks

**Depends on:** 58, 68, 70, 71.

**Owns:** `internal/application/apply/hooks.go`, `internal/application/apply/hooks_test.go`.

**Deliverable:** Run before hooks, invoke the all-source guard/executors, and run/aggregate after hooks only when the filesystem phase completed. Set exact `CATTERY_RESULT`; skip every after hook on a mid-filesystem operational failure.

**Tests:** `TestApplyHookPipeline` covers no-op/dry-run/no-hooks, before failure, skipped after hooks on executor failure, `partial` after hooks for user skips, aggregated after failures, and cancellation.

**Acceptance:** Before-hook failure causes zero executor/managed-row writes; after failures never roll back completed writes.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyHookPipeline$' | grep '^TestApplyHookPipeline$' && go test -race ./internal/application/apply -run '^TestApplyHookPipeline$'` passes.

**Commit:** `feat: orchestrate apply hooks`.

### Task 73: Verify post-hook convergence

**Depends on:** 47, 48, 49, 50, 51, 52, 53, 70, 71, 72.

**Owns:** `internal/application/apply/verify.go`, `internal/application/apply/verify_test.go`.

**Deliverable:** Re-snapshot selected sources, targets, and aliases after hooks; establish equality baselines only after successful verification.

**Tests:** `TestApplyVerification` covers post-hook source/target/alias drift, no-write baselines, SOPS rechecks, and state commit failure.

**Acceptance:** Verification never rewrites drift and returns precise partial facts.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyVerification$' | grep '^TestApplyVerification$' && go test -race ./internal/application/apply -run '^TestApplyVerification$'` passes.

**Commit:** `feat: verify apply convergence`.

### Task 74: Compose the apply service

**Depends on:** 68, 69, 70, 71, 72, 73.

**Owns:** `internal/application/apply/service.go`, `internal/application/apply/service_test.go`.

**Deliverable:** Implement only the short public pipeline and constructor over frozen phases, including dry-run/no-op branching and joined error propagation.

**Tests:** `TestApplyService` injects one failure at every phase, checks exact order, partial result preservation, cancellation, and function limits.

**Acceptance:** The service is Cobra-free and contains no phase implementation.

**Verify:** `go test -race ./internal/application/apply -list '^TestApplyService$' | grep '^TestApplyService$' && go test -race ./internal/application/apply -run '^TestApplyService$'` passes.

**Commit:** `feat: compose apply service`.

### Task 75: Freeze add contracts

**Depends on:** 2, 7, 14, 41.

**Owns:** `internal/application/add/types.go`, `internal/application/add/types_test.go`.

**Deliverable:** Define repository input, per-option presence values, immutable batch/item plans, write/state ports, and partial result records.

**Tests:** `TestAddContract` validates raw order, explicit false, defensive copies, interface shape, and no CLI/adapter types.

**Acceptance:** The contract fixes add as the sole target-to-repository content path.

**Verify:** `go test ./internal/application/add -list '^TestAddContract$' | grep '^TestAddContract$' && go test ./internal/application/add -run '^TestAddContract$'` passes.

**Commit:** `feat: freeze add contracts`.

### Task 76: Infer add ownership and source paths

**Depends on:** 35, 57, 75.

**Owns:** `internal/application/add/infer.go`, `internal/application/add/infer_test.go`.

**Deliverable:** Infer root/group, base/current-platform, ordinary/secret, and repository source path from explicit presence bits and existing compiled ownership.

**Tests:** `TestAddInference` covers managed/unmanaged targets, omitted/explicit values, aliases, inactive platform, and storage-class conflicts.

**Acceptance:** Inference is read-only and does not inspect target bytes or call SOPS.

**Verify:** `go test -race ./internal/application/add -list '^TestAddInference$' | grep '^TestAddInference$' && go test -race ./internal/application/add -run '^TestAddInference$'` passes.

**Commit:** `feat: infer add ownership`.

### Task 77: Preflight complete add batches

**Depends on:** 12, 48, 76.

**Owns:** `internal/application/add/preflight.go`, `internal/application/add/preflight_test.go`.

**Deliverable:** Validate every argument, target type/identity, duplicate canonical/object path, source collision, containment, and immutable source/target precondition before any write.

**Tests:** `TestAddBatchPreflight` covers ordering, hard-linked arguments, symlink/special targets, collisions, races, and complete-batch refusal.

**Acceptance:** Any failure causes zero repository write, SOPS call, or state change.

**Verify:** `go test -race ./internal/application/add -list '^TestAddBatchPreflight$' | grep '^TestAddBatchPreflight$' && go test -race ./internal/application/add -run '^TestAddBatchPreflight$'` passes.

**Commit:** `feat: preflight add batches`.

### Task 78: Build immutable add plans

**Depends on:** 77.

**Owns:** `internal/application/add/plan.go`, `internal/application/add/plan_test.go`.

**Deliverable:** Convert preflighted arguments into sorted immutable actions and dry-run records while preserving execution order separately.

**Tests:** `TestAddPlan` covers repeated input rejection, stable display ordering, dry-run verbs/counts, and no side effects.

**Acceptance:** The plan contains complete preconditions and no plaintext secret bytes.

**Verify:** `go test ./internal/application/add -list '^TestAddPlan$' | grep '^TestAddPlan$' && go test ./internal/application/add -run '^TestAddPlan$'` passes.

**Commit:** `feat: build add plans`.

### Task 79: Write ordinary add sources

**Depends on:** 43, 44, 78.

**Owns:** `internal/application/add/write_ordinary.go`, `internal/application/add/write_ordinary_test.go`.

**Deliverable:** Revalidate target/source destination and atomically write ordinary target bytes into the inferred repository source with executable-bit policy.

**Tests:** `TestAddOrdinaryWrite` covers missing/existing source, target races, modes, directory creation, and every replace failure.

**Acceptance:** Target bytes are retained exactly and no state write occurs in this phase.

**Verify:** `go test -race ./internal/application/add -list '^TestAddOrdinaryWrite$' | grep '^TestAddOrdinaryWrite$' && go test -race ./internal/application/add -run '^TestAddOrdinaryWrite$'` passes.

**Commit:** `feat: write ordinary add sources`.

### Task 80: Write encrypted add sources

**Depends on:** 22, 40, 43, 44, 78.

**Owns:** `internal/application/add/write_secret.go`, `internal/application/add/write_secret_test.go`.

**Deliverable:** Encrypt target bytes, validate the candidate round trip, atomically write only JSON ciphertext, and clear plaintext on every path.

**Tests:** `TestAddSecretWrite` covers arbitrary bytes, SOPS failures, mismatch, races, modes, temp cleanup, and leakage scans.

**Acceptance:** No plaintext enters repository, state, general temp paths, results, or errors.

**Verify:** `go test -race ./internal/application/add -list '^TestAddSecretWrite$' | grep '^TestAddSecretWrite$' && go test -race ./internal/application/add -run '^TestAddSecretWrite$'` passes.

**Commit:** `feat: write encrypted add sources`.

### Task 81: Execute add batches

**Depends on:** 23, 79, 80.

**Owns:** `internal/application/add/execute.go`, `internal/application/add/execute_test.go`.

**Deliverable:** Execute input-order ordinary/secret actions sequentially and establish baselines only when post-write target equality still holds.

**Tests:** `TestAddBatchExecution` injects source-write/state failures, target races, earlier-item retention, and exact partial summaries.

**Acceptance:** No batch rollback, hook, prompt, or implicit storage-class move occurs.

**Verify:** `go test -race ./internal/application/add -list '^TestAddBatchExecution$' | grep '^TestAddBatchExecution$' && go test -race ./internal/application/add -run '^TestAddBatchExecution$'` passes.

**Commit:** `feat: execute add batches`.

### Task 82: Compose the add service

**Depends on:** 78, 81.

**Owns:** `internal/application/add/service.go`, `internal/application/add/service_test.go`.

**Deliverable:** Implement only the short plan/dry-run/execute public pipeline and constructor over frozen add phases.

**Tests:** `TestAddService` covers dry-run, empty/invalid batch, phase failures, partial results, cancellation, and function limits.

**Acceptance:** The service is Cobra-free and contains no phase implementation.

**Verify:** `go test -race ./internal/application/add -list '^TestAddService$' | grep '^TestAddService$' && go test -race ./internal/application/add -run '^TestAddService$'` passes.

**Commit:** `feat: compose add service`.

### Task 83: Implement the version service

**Depends on:** 3.

**Owns:** `internal/application/version/types.go`, `internal/application/version/service.go`, `internal/application/version/types_test.go`, `internal/application/version/service_test.go`.

**Deliverable:** Return build/runtime information as typed fields through a Cobra-free one-method service.

**Tests:** `TestVersionContract` and `TestVersionService` cover defaults, release values, UTC input, and all runtime fields.

**Acceptance:** Construction and invocation access no repository, state, clock, or external dependency.

**Verify:** `go test ./internal/application/version -list '^TestVersion(Contract|Service)$' | grep '^Test' && go test ./internal/application/version -run '^TestVersion(Contract|Service)$'` passes.

**Commit:** `feat: expose version service`.

### Task 84: Define CLI runtime and options

**Depends on:** 1, 2, 4.

**Owns:** `internal/cli/options.go`, `internal/cli/runtime.go`, `internal/cli/options_test.go`, `internal/cli/runtime_test.go`.

**Deliverable:** Define command-local option values and injected streams/cwd/environment/TTY/verbosity behavior; Cobra v1.10.2 and x/term v0.45.0 remain confined here.

**Tests:** `TestCLIOptions` and `TestCLIRuntime` cover explicit presence, interspersing inputs, TTY/file/pipe readers, and instance isolation.

**Acceptance:** No mutable global or default logger mutation exists.

**Verify:** `go test ./internal/cli -list '^TestCLI(Options|Runtime)$' | grep '^Test' && go test ./internal/cli -run '^TestCLI(Options|Runtime)$'` passes.

**Commit:** `feat: define cli runtime`.

### Task 85: Adapt the init command

**Depends on:** 59, 84.

**Owns:** `internal/cli/init.go`, `internal/cli/init_test.go`.

**Deliverable:** Declare init syntax/help, mechanically map raw path/cwd, call one `InitializeService`, and render its typed result.

**Tests:** `TestInitCommand` covers arity, unsupported repo, one/zero calls, stdout, writer error, and a 15-line `RunE`.

**Acceptance:** No path resolution, registration, or backend import appears.

**Verify:** `go test ./internal/cli -list '^TestInitCommand$' | grep '^TestInitCommand$' && go test ./internal/cli -run '^TestInitCommand$'` passes.

**Commit:** `feat: adapt init command`.

### Task 86: Adapt the validate command

**Depends on:** 60, 84.

**Owns:** `internal/cli/validate.go`, `internal/cli/render_validate.go`, `internal/cli/validate_test.go`, `internal/cli/render_validate_test.go`.

**Deliverable:** Map raw repository/group values to one validate call and render exactly two count records.

**Tests:** `TestValidateCommand` and `TestValidateRenderer` cover argument order, flags, one/zero calls, lines, partial/error, and writer failure.

**Acceptance:** No group/repository semantics or backend import appears.

**Verify:** `go test ./internal/cli -list '^TestValidate(Command|Renderer)$' | grep '^Test' && go test ./internal/cli -run '^TestValidate(Command|Renderer)$'` passes.

**Commit:** `feat: adapt validate command`.

### Task 87: Adapt the version command

**Depends on:** 83, 84.

**Owns:** `internal/cli/version.go`, `internal/cli/version_test.go`.

**Deliverable:** Declare only the `version` subcommand, call one service, and render typed fields without using Cobra root `Version`.

**Tests:** `TestVersionCommand` covers unsupported repo, one/zero calls, the exact Section 11.7 single-line format and trailing newline for development/release values, writer error, and no backend access.

**Acceptance:** `--version` remains an unknown root flag.

**Verify:** `go test ./internal/cli -list '^TestVersionCommand$' | grep '^TestVersionCommand$' && go test ./internal/cli -run '^TestVersionCommand$'` passes.

**Commit:** `feat: adapt version command`.

### Task 88: Adapt the status command

**Depends on:** 62, 84.

**Owns:** `internal/cli/status.go`, `internal/cli/render_status.go`, `internal/cli/status_test.go`, `internal/cli/render_status_test.go`.

**Deliverable:** Map raw repository/groups to one status call and render semantic active/retired/alias records plus summary.

**Tests:** `TestStatusCommand` and `TestStatusRenderer` cover flags/args, one call, output-before-Difference, partial results, escaping, and writer failure.

**Acceptance:** No classification or state import appears.

**Verify:** `go test ./internal/cli -list '^TestStatus(Command|Renderer)$' | grep '^Test' && go test ./internal/cli -run '^TestStatus(Command|Renderer)$'` passes.

**Commit:** `feat: adapt status command`.

### Task 89: Adapt the diff command

**Depends on:** 63, 84.

**Owns:** `internal/cli/diff.go`, `internal/cli/render_diff.go`, `internal/cli/diff_test.go`, `internal/cli/render_diff_test.go`.

**Deliverable:** Map raw repository/groups to one diff call and render only tagged safe records plus summary.

**Tests:** `TestDiffCommand` and `TestDiffRenderer` cover every record tag, output-before-Difference, aliases/retirement, escaping, and zero secret fields.

**Acceptance:** No diff calculation or third-party formatter import appears.

**Verify:** `go test ./internal/cli -list '^TestDiff(Command|Renderer)$' | grep '^Test' && go test ./internal/cli -run '^TestDiff(Command|Renderer)$'` passes.

**Commit:** `feat: adapt diff command`.

### Task 90: Adapt the add command

**Depends on:** 82, 84.

**Owns:** `internal/cli/add.go`, `internal/cli/render_add.go`, `internal/cli/add_test.go`, `internal/cli/render_add_test.go`.

**Deliverable:** Preserve raw target order, cwd, repository input, and exact group/platform/secret presence bits in one add call.

**Tests:** `TestAddCommand` and `TestAddRenderer` cover interspersed flags, `--`, explicit false, repeated args, dry-run, partial errors, and writer failure.

**Acceptance:** No ownership inference or filesystem access appears.

**Verify:** `go test ./internal/cli -list '^TestAdd(Command|Renderer)$' | grep '^Test' && go test ./internal/cli -run '^TestAdd(Command|Renderer)$'` passes.

**Commit:** `feat: adapt add command`.

### Task 91: Implement the decision prompt adapter

**Depends on:** 55, 67, 84.

**Owns:** `internal/cli/prompt.go`, `internal/cli/prompt_test.go`.

**Deliverable:** Implement only DecisionResolver presentation IO with exact overwrite/skip/abort/diff grammar, safe diff display, TTY policy, reprompting, and EOF handling.

**Tests:** `TestDecisionPrompt` covers each choice, invalid input, secret restrictions, non-TTY, EOF, streams, and writer errors.

**Acceptance:** It imports apply DTOs but no Cobra command or backend adapter.

**Verify:** `go test ./internal/cli -list '^TestDecisionPrompt$' | grep '^TestDecisionPrompt$' && go test ./internal/cli -run '^TestDecisionPrompt$'` passes.

**Commit:** `feat: implement decision prompt`.

### Task 92: Adapt the apply command

**Depends on:** 74, 84, 91.

**Owns:** `internal/cli/apply.go`, `internal/cli/render_apply.go`, `internal/cli/apply_test.go`, `internal/cli/render_apply_test.go`.

**Deliverable:** Map raw repository/groups and dry-run/noninteractive/no-hooks into one apply call with the prompt resolver, then render typed partial/action summaries.

**Tests:** `TestApplyCommand` and `TestApplyRenderer` cover flags/args, one call, output/error joins, dry-run verbs, partials, and 15-line `RunE`.

**Acceptance:** No decision policy, hook order, or reconciliation branch appears.

**Verify:** `go test ./internal/cli -list '^TestApply(Command|Renderer)$' | grep '^Test' && go test ./internal/cli -run '^TestApply(Command|Renderer)$'` passes.

**Commit:** `feat: adapt apply command`.

### Task 93: Build the opaque Cobra root

**Depends on:** 85, 86, 87, 88, 89, 90, 92.

**Owns:** `internal/cli/root.go`, `internal/cli/root_test.go`.

**Deliverable:** Construct an opaque single-use application with seven operational commands, persistent flags, injected streams, no root Version, no completion/suggestions, help/no-command behavior, and no `OnInitialize`.

**Tests:** `TestCobraRoot` covers root/help, command inventory, flags before/after/between args, `--`, unknown `--version`, unsupported repo, and zero service calls.

**Acceptance:** No exported signature exposes Cobra/pflag and root construction touches no backend.

**Verify:** `go test ./internal/cli -list '^TestCobraRoot$' | grep '^TestCobraRoot$' && go test ./internal/cli -run '^TestCobraRoot$'` passes.

**Commit:** `feat: build opaque cobra root`.

### Task 94: Centralize CLI execution and exits

**Depends on:** 2, 93.

**Owns:** `internal/cli/execute.go`, `internal/cli/execute_test.go`.

**Deliverable:** Execute one application, preserve service plus renderer errors, write one diagnostic, and map all joined categories/signals to Section 11.8 statuses.

**Tests:** `TestCLIExecute` exhausts precedence, parse/usage, writer plus hook/signal, second-use rejection, silence settings, and no `os.Exit`.

**Acceptance:** Numeric statuses exist only here and signal 130/143 outranks all joined errors.

**Verify:** `go test ./internal/cli -list '^TestCLIExecute$' | grep '^TestCLIExecute$' && go test ./internal/cli -run '^TestCLIExecute$'` passes.

**Commit:** `feat: centralize cli execution`.

### Task 95: Construct concrete infrastructure adapters

**Depends on:** 18, 35, 37, 40, 43, 44, 45, 58.

**Owns:** `internal/bootstrap/adapters.go`, `internal/bootstrap/adapters_test.go`.

**Deliverable:** Construct lazy state/compiler/filesystem/subprocess/SOPS/hook concrete adapters and per-application logger resources without opening or probing them.

**Tests:** `TestBootstrapAdapters` proves construction is side-effect-free and two builds share no mutable dependency.

**Acceptance:** This file performs wiring only and imports no Cobra.

**Verify:** `go test ./internal/bootstrap -list '^TestBootstrapAdapters$' | grep '^TestBootstrapAdapters$' && go test ./internal/bootstrap -run '^TestBootstrapAdapters$'` passes.

**Commit:** `feat: construct infrastructure adapters`.

### Task 96: Construct application services

**Depends on:** 59, 60, 61, 62, 63, 74, 82, 83, 95.

**Owns:** `internal/bootstrap/applications.go`, `internal/bootstrap/applications_test.go`.

**Deliverable:** Wire concrete adapters into each application package through only that consumer’s purpose-named ports.

**Tests:** `TestBootstrapApplications` compile-asserts all service interfaces and injects isolated fakes for every port.

**Acceptance:** No application constructor triggers state, repository, filesystem, or process work.

**Verify:** `go test ./internal/bootstrap -list '^TestBootstrapApplications$' | grep '^TestBootstrapApplications$' && go test ./internal/bootstrap -run '^TestBootstrapApplications$'` passes.

**Commit:** `feat: construct application services`.

### Task 97: Build the CLI application composition

**Depends on:** 84, 93, 94, 96.

**Owns:** `internal/bootstrap/build.go`, `internal/bootstrap/build_test.go`.

**Deliverable:** Assemble runtime streams/environment/cwd/TTY, one logger/LevelVar, application services, and `cli.Dependencies` into a fresh opaque application.

**Tests:** `TestBootstrapBuild` builds two isolated applications and proves version/help/parse failure cause zero backend effects.

**Acceptance:** Bootstrap never executes Cobra or maps exits.

**Verify:** `go test ./internal/bootstrap -list '^TestBootstrapBuild$' | grep '^TestBootstrapBuild$' && go test ./internal/bootstrap -run '^TestBootstrapBuild$'` passes.

**Commit:** `feat: build cli application`.

### Task 98: Implement the process entrypoint

**Depends on:** 2, 97.

**Owns:** `cmd/cattery/main.go`, `cmd/cattery/main_test.go`.

**Deliverable:** Create signal-aware cancellation causes, request one application from bootstrap, call `cli.Execute`, and own the sole production `os.Exit`.

**Tests:** `TestMainBoundary` verifies interrupt/terminate causes, stream/env forwarding, and static architecture constraints without backend logic.

**Acceptance:** `main.go` remains one short process-boundary function.

**Verify:** `go test ./cmd/cattery -list '^TestMainBoundary$' | grep '^TestMainBoundary$' && go test ./cmd/cattery -run '^TestMainBoundary$' && go build ./cmd/cattery` passes.

**Commit:** `feat: implement cattery entrypoint`.

### Task 99: Freeze backend integration fixtures

**Depends on:** 95, 96.

**Owns:** `integration/backend_fixture_test.go`.

**Deliverable:** Provide isolated real repository/HOME/state/application construction for backend tests without importing CLI or Cobra.

**Tests:** `TestBackendFixture` proves isolation, deterministic clocks, no hidden registration, and cleanup.

**Acceptance:** No production file is patched by integration cards.

**Verify:** `go test -race ./integration -list '^TestBackendFixture$' | grep '^TestBackendFixture$' && go test -race ./integration -run '^TestBackendFixture$'` passes.

**Commit:** `test: freeze backend integration fixture`.

### Task 100: Prove apply workflows without Cobra

**Depends on:** 74, 99.

**Owns:** `integration/backend_apply_test.go`.

**Deliverable:** Exercise real apply services/adapters for first apply, source update, target drift via fake resolver, retirement, hooks, and partial state failures.

**Tests:** `TestBackendApply` asserts typed results/categories, files, rows, ordering, and zero CLI/Cobra imports.

**Acceptance:** All named ordinary apply invariants run through application APIs directly.

**Verify:** `go test -race ./integration -list '^TestBackendApply$' | grep '^TestBackendApply$' && go test -race ./integration -run '^TestBackendApply$'` passes.

**Commit:** `test: prove backend apply workflows`.

### Task 101: Prove add workflows without Cobra

**Depends on:** 82, 99.

**Owns:** `integration/backend_add_test.go`.

**Deliverable:** Exercise ordinary and fake-SOPS adoption, dry-run, explicit presence semantics, and partial batches through the add service directly.

**Tests:** `TestBackendAdd` asserts exact source bytes/ciphertext, baselines, partial results, and no hook/prompt.

**Acceptance:** This suite proves add is the only reverse content path.

**Verify:** `go test -race ./integration -list '^TestBackendAdd$' | grep '^TestBackendAdd$' && go test -race ./integration -run '^TestBackendAdd$'` passes.

**Commit:** `test: prove backend add workflows`.

### Task 102: Prove inspection workflows without Cobra

**Depends on:** 63, 99.

**Owns:** `integration/backend_inspect_test.go`.

**Deliverable:** Exercise status/diff parity, state deletion, state-only groups, aliases, retirement, and secret-safe records through services directly.

**Tests:** `TestBackendInspect` asserts typed order/counts/categories and zero mutation/hook/prompt.

**Acceptance:** Both methods consume equivalent immutable evaluations.

**Verify:** `go test -race ./integration -list '^TestBackendInspect$' | grep '^TestBackendInspect$' && go test -race ./integration -run '^TestBackendInspect$'` passes.

**Commit:** `test: prove backend inspection`.

### Task 103: Freeze executable process fixtures

**Depends on:** 98.

**Owns:** `integration/process_fixture_test.go`.

**Deliverable:** Build one binary and provide isolated subprocess invocation with HOME/XDG/env/stdin/stdout/stderr and timeout controls.

**Tests:** `TestProcessFixture` proves binary reuse, stream separation, environment isolation, timeout cleanup, and exact exit capture.

**Acceptance:** No scenario behavior is asserted in this fixture card.

**Verify:** `go test -race ./integration -list '^TestProcessFixture$' | grep '^TestProcessFixture$' && go test -race ./integration -run '^TestProcessFixture$'` passes.

**Commit:** `test: freeze process fixture`.

### Task 104: Prove basic Cobra executable behavior

**Depends on:** 103.

**Owns:** `integration/cli_test.go`.

**Deliverable:** Black-box root/no-command/help/unknown/`--version`/version/init/validate and global/local flag placement with exact streams and statuses.

**Tests:** `TestExecutableCLI` covers every parse/arity/usage case and proves help/version errors touch no state.

**Acceptance:** One built binary produces deterministic repeated output.

**Verify:** `go test -race ./integration -list '^TestExecutableCLI$' | grep '^TestExecutableCLI$' && go test -race ./integration -run '^TestExecutableCLI$'` passes.

**Commit:** `test: prove executable cli behavior`.

### Task 105: Prove ordinary apply through the executable

**Depends on:** 100, 104.

**Owns:** `integration/apply_test.go`.

**Deliverable:** Black-box first apply, source update, target drift choices, dry-run, status/diff convergence, database loss, and source removal.

**Tests:** `TestExecutableApply` asserts prompts, stable verbs/counts, modes, rows, deterministic order, and exits.

**Acceptance:** No secret, hook, path-race, or signal scenario is duplicated here.

**Verify:** `go test -race ./integration -list '^TestExecutableApply$' | grep '^TestExecutableApply$' && go test -race ./integration -run '^TestExecutableApply$'` passes.

**Commit:** `test: prove executable apply`.

### Task 106: Prove add through the executable

**Depends on:** 101, 104.

**Owns:** `integration/add_test.go`.

**Deliverable:** Black-box raw argument order, presence bits, ordinary adoption, dry-run, conflicts, and partial output/state.

**Tests:** `TestExecutableAdd` asserts repository bytes, target preservation, baselines, streams, and exits.

**Acceptance:** Secret adoption remains in the secret scenario card.

**Verify:** `go test -race ./integration -list '^TestExecutableAdd$' | grep '^TestExecutableAdd$' && go test -race ./integration -run '^TestExecutableAdd$'` passes.

**Commit:** `test: prove executable add`.

### Task 107: Freeze real and fake SOPS fixtures

**Depends on:** 36, 98.

**Owns:** `integration/sops_fixture_test.go`.

**Deliverable:** Provide fake executable installation plus ephemeral age identity/config and pinned real SOPS discovery under isolated paths.

**Tests:** `TestIntegrationSOPSFixture` verifies exact versions, no user config/key reuse, and cleanup registration.

**Acceptance:** Credential values are never logged and fixture teardown removes every identity/config/binary copy.

**Verify:** `go test -race ./integration -list '^TestIntegrationSOPSFixture$' | grep '^TestIntegrationSOPSFixture$' && go test -race ./integration -run '^TestIntegrationSOPSFixture$'` passes.

**Commit:** `test: freeze sops integration fixture`.

### Task 108: Prove secret workflows through the executable

**Depends on:** 40, 105, 106, 107.

**Owns:** `integration/secrets_test.go`.

**Deliverable:** Black-box fake and real SOPS apply/add/status/diff with arbitrary binary bytes, key recovery, dependency failures, and full leakage scans.

**Tests:** `TestExecutableSecrets` asserts byte equality, ciphertext shape, modes, SQLite/repository/output absence, cleanup, and exact exits.

**Acceptance:** The real pinned path is mandatory in CI and never silently skipped there.

**Verify:** `go test -race ./integration -list '^TestExecutableSecrets$' | grep '^TestExecutableSecrets$' && go test -race ./integration -run '^TestExecutableSecrets$' && just test-sops` passes.

**Commit:** `test: prove executable secrets`.

### Task 109: Prove hook workflows through the executable

**Depends on:** 58, 105.

**Owns:** `integration/hooks_test.go`.

**Deliverable:** Black-box hook ordering, inherited streams, before/after failure rules, post-hook drift, result environment, no-op suppression, and descendants.

**Tests:** `TestExecutableHooks` asserts exact command order, files/rows, partials, output, and exits.

**Acceptance:** Secret and signal-only scenarios remain in their own cards.

**Verify:** `go test -race ./integration -list '^TestExecutableHooks$' | grep '^TestExecutableHooks$' && go test -race ./integration -run '^TestExecutableHooks$'` passes.

**Commit:** `test: prove executable hooks`.

### Task 110: Freeze race and failure injection fixtures

**Depends on:** 103.

**Owns:** `integration/race_fixture_test.go`.

**Deliverable:** Provide deterministic barriers and injectable filesystem/state failure points for black-box precondition and partial-commit tests.

**Tests:** `TestRaceFixture` proves each barrier fires once, preserves process cleanup, and cannot patch production code.

**Acceptance:** The fixture has no scenario assertions.

**Verify:** `go test -race ./integration -list '^TestRaceFixture$' | grep '^TestRaceFixture$' && go test -race ./integration -run '^TestRaceFixture$'` passes.

**Commit:** `test: freeze race fixture`.

### Task 111: Prove path safety through the executable

**Depends on:** 105, 110.

**Owns:** `integration/path_safety_test.go`.

**Deliverable:** Black-box traversal, every parent symlink, protected overlap, source/target identity, hard links, case/NFC, parent-child, blocking ancestors, and complete-preflight races.

**Tests:** `TestExecutablePathSafety` asserts exact failure categories and zero hooks/writes/managed-row changes.

**Acceptance:** No scenario relies only on lexical checks.

**Verify:** `go test -race ./integration -list '^TestExecutablePathSafety$' | grep '^TestExecutablePathSafety$' && go test -race ./integration -run '^TestExecutablePathSafety$'` passes.

**Commit:** `test: prove executable path safety`.

### Task 112: Prove aliases and representation transitions

**Depends on:** 105, 110.

**Owns:** `integration/aliases_test.go`.

**Deliverable:** Black-box exact/wrong/absolute/dangling/occupied aliases and both intact/drifted file-alias temporal transitions.

**Tests:** `TestExecutableAliases` asserts link text, no referent writes, decisions, active-row switching, rollback, and exits.

**Acceptance:** Structural file-directory transitions remain manual failures.

**Verify:** `go test -race ./integration -list '^TestExecutableAliases$' | grep '^TestExecutableAliases$' && go test -race ./integration -run '^TestExecutableAliases$'` passes.

**Commit:** `test: prove executable aliases`.

### Task 113: Prove partial failure recovery

**Depends on:** 105, 106, 110.

**Owns:** `integration/failures_test.go`.

**Deliverable:** Inject every pre/post-rename, directory-sync, state-commit, hook, and later-item failure across apply and add.

**Tests:** `TestExecutableFailures` asserts exact filesystem/state/partial result after each boundary and equality recovery on retry.

**Acceptance:** The suite promises no whole-operation rollback and detects temporary leaks.

**Verify:** `go test -race ./integration -list '^TestExecutableFailures$' | grep '^TestExecutableFailures$' && go test -race ./integration -run '^TestExecutableFailures$'` passes.

**Commit:** `test: prove partial failure recovery`.

### Task 114: Prove signal behavior through the executable

**Depends on:** 104, 110.

**Owns:** `integration/signals_test.go`.

**Deliverable:** Send SIGINT/SIGTERM during SOPS, hooks, and atomic-write boundaries; verify descendant termination, cleanup, partial state, and exact exits.

**Tests:** `TestExecutableSignals` covers causes, 130/143 precedence, grace/KILL, and no duplicate diagnostic.

**Acceptance:** The test is Unix-only with an explicit Darwin-compatible CI subset.

**Verify:** `go test -race ./integration -list '^TestExecutableSignals$' | grep '^TestExecutableSignals$' && go test -race ./integration -run '^TestExecutableSignals$'` passes.

**Commit:** `test: prove executable signals`.

### Task 115: Enforce the complete CI matrix

**Depends on:** 4, 100, 101, 102, 104, 105, 106, 108, 109, 111, 112, 113, 114.

**Owns:** `.github/workflows/ci.yml`.

**Deliverable:** Define native `ubuntu-22.04` and `macos-15` tests, Go 1.25 floor, Go 1.26 primary, quality/race/SOPS gates, dependency-update audit, and four CGO-free cross-builds using only the exact Section 13 checkout/setup-go/install-Nix action commits, read-only permissions, credential-free checkout, and immutable `just`/Nix recipes.

**Tests:** Workflow run on the integration commit, with named jobs matching Section 15.6.

**Acceptance:** Every job is required; skipped SOPS/floor jobs, source violations, direct updates, or advertised cross-build failures make the check red.

**Verify:** `nix flake check && gh run list --workflow ci.yml --commit "$(git rev-parse HEAD)" --status success --limit 1 --json databaseId --jq 'length == 1' | grep true` passes.

**Commit:** `ci: enforce delivery matrix`.

### Task 116: Package deterministic release archives

**Depends on:** 98, 115.

**Owns:** `scripts/package-release.sh`, `scripts/tests/package_release_test.sh`.

**Deliverable:** Accept only a clean checkout whose `HEAD` is named by a matching annotated `vX.Y.Z` tag, derive all Section 11.7 tag/commit/`SOURCE_DATE_EPOCH` metadata locally, and build the exact four static binaries, ustar+gzip archives, and sorted checksum manifest using pinned GNU tar/gzip.

**Tests:** Shell test covers dirty trees, lightweight/malformed/non-HEAD tags, linker values, build flags, archive member sets/order/modes/owners/mtimes, gzip headers, filenames, checksums, and two clean builds from different absolute paths.

**Acceptance:** No network access or wall-clock metadata affects bytes, and two clean builds produce a byte-identical archive SHA-256 for every target.

**Verify:** `bash scripts/tests/package_release_test.sh && test "$(scripts/package-release.sh --print-reproducibility-status)" = reproducible` passes.

**Commit:** `chore: package deterministic releases`.

### Task 117: Smoke packaged release artifacts

**Depends on:** 108, 116.

**Owns:** `scripts/smoke-release.sh`, `scripts/tests/smoke_release_test.sh`.

**Deliverable:** Verify the checksum and archive metadata for all four targets, unpack the host-applicable Linux/amd64 archive in the release job, and run isolated init/validate/ordinary apply/add plus a real-secret round trip with complete cleanup.

**Tests:** Shell test injects bad checksums, broken archives, command failures, and ephemeral-key cleanup checks.

**Acceptance:** The behavioral smoke uses the packaged host binary only, CI already supplies native macOS behavior plus all four cross-build gates, and no test touches user HOME/state/credentials.

**Verify:** `bash scripts/tests/smoke_release_test.sh` passes.

**Commit:** `test: smoke release artifacts`.

### Task 118: Publish releases safely

**Depends on:** 116, 117.

**Owns:** `.github/workflows/release.yml`.

**Deliverable:** Publish only matching annotated `vX.Y.Z` tags after a full-history credential-free checkout and a tag-only least-privilege job independently rebuilds deterministic packages and repeats artifact smoke, using the exact Section 13 checkout/install-Nix action commits and Nix-pinned `gh release create --verify-tag`; provide a read-only, non-publishing workflow-dispatch validation path and use no artifact transfer/release action.

**Tests:** A workflow-dispatch run records all build/smoke jobs green and proves no release/upload side effect; tag-path policy is statically asserted.

**Acceptance:** Checksums and all four archives are the only published assets.

**Verify:** `gh run list --workflow release.yml --event workflow_dispatch --commit "$(git rev-parse HEAD)" --status success --limit 1 --json databaseId,headSha,url --jq 'length == 1' | grep true` passes after `release.yml` was dispatched with `dry_run=true` on the card commit and completed; the recorded run URL shows no release asset step.

**Commit:** `ci: publish deterministic releases`.

### Task 119: Implement deterministic documentation checks

**Depends on:** 4, 118.

**Owns:** `scripts/check-docs.py`, `scripts/tests/check_docs_test.py`.

**Deliverable:** Implement the immutable `just check-docs [PATH ...]` and `just check-credentials [PATH ...]` backends using only Python's standard library: validate local links, documented command/flag vocabulary, forbidden feature claims, placeholder-only credentials, and deterministic UTF-8 output.

**Tests:** `scripts/tests/check_docs_test.py` creates valid and one-failure-per-rule Markdown fixtures, including broken links, unknown commands/flags, obsolete features, credential-shaped values assembled only at runtime, invalid UTF-8, and multiple input paths.

**Acceptance:** The checker never imports project implementation packages, accesses the network, rewrites documentation, or exempts a repository path; no tracked test fixture itself contains credential-shaped material.

**Verify:** `python3 scripts/tests/check_docs_test.py && just check-docs --help && just check-credentials --help` passes.

**Commit:** `test: validate documentation contracts`.

### Task 120: Document repository layout

**Depends on:** 35, 111, 112, 119.

**Owns:** `docs/repository-layout.md`.

**Deliverable:** Document literal root/groups, controls, overlays, routes, aliases, selection, path/parent/hard-link/case/NFC rules, and unsupported layouts.

**Tests:** `just check-docs` validates every command/path example against the fixed grammar.

**Acceptance:** No manifest, template, package-manager feature, or credential appears.

**Verify:** `just check-docs docs/repository-layout.md` passes.

**Commit:** `docs: explain repository layout`.

### Task 121: Document reconciliation and recovery

**Depends on:** 100, 102, 113, 119.

**Owns:** `docs/reconciliation.md`.

**Deliverable:** Document source/target/baseline matrix, decisions, retirement, representation transitions, partial durability, retry recovery, and no-deletion/no-rollback boundaries.

**Tests:** `just check-docs` verifies stable action verbs and links each destructive DoD item to a section.

**Acceptance:** No prose promises target import through apply or whole-operation rollback.

**Verify:** `just check-docs docs/reconciliation.md` passes.

**Commit:** `docs: explain reconciliation`.

### Task 122: Document secret operations

**Depends on:** 108, 119.

**Owns:** `docs/secrets.md`.

**Deliverable:** Document SOPS v3.13.3/age v1.3.1 setup, binary storage, add/apply flow, hash-key backup/recovery, modes, and leakage boundaries.

**Tests:** `just check-docs` validates commands and a credential-pattern scan returns no match.

**Acceptance:** Examples use placeholders only and state plainly that plaintext never enters state/output.

**Verify:** `just check-docs docs/secrets.md && just check-credentials docs/secrets.md` passes.

**Commit:** `docs: explain secret handling`.

### Task 123: Document trusted hooks

**Depends on:** 109, 119.

**Owns:** `docs/hooks.md`.

**Deliverable:** Document discovery, exact order/environment/streams, suppression, before/after failure semantics, trusted side effects, and cancellation.

**Tests:** `just check-docs` validates executable examples and result values.

**Acceptance:** No package-manager integration is presented as a built-in feature.

**Verify:** `just check-docs docs/hooks.md` passes.

**Commit:** `docs: explain trusted hooks`.

### Task 124: Document release operations

**Depends on:** 118, 119.

**Owns:** `docs/release.md`.

**Deliverable:** Document annotated tags, deterministic metadata, four targets, checksums, CI/floor requirements, artifact smoke, and recovery from failed publication.

**Tests:** `just check-docs` validates all script/workflow names and release steps.

**Acceptance:** The procedure uses immutable recipes and contains no manual byte-editing workaround.

**Verify:** `just check-docs docs/release.md` passes.

**Commit:** `docs: explain release operations`.

### Task 125: Write the user-facing README

**Depends on:** 120, 121, 122, 123, 124.

**Owns:** `README.md`.

**Deliverable:** Provide concise installation, repository example, command reference, safety model, documentation links, and an accurate MVP/non-goal summary.

**Tests:** `just check-docs` validates every command/flag/link and checks all seven operational commands are documented once.

**Acceptance:** README claims only behavior proven by prior acceptance cards.

**Verify:** `just check-docs README.md && just check` passes.

**Commit:** `docs: introduce cattery`.

---

## 17. Parallel-agent coordination

The scheduler derives the ready set mechanically from each card's direct prerequisites. Numeric proximity does not imply permission to start, and “latest” is never a valid base.

### Merge waves

1. **Plan freeze:** commit the authoritative plan and assign its hash.
2. **Foundation:** Tasks 1-6 establish immutable module/tooling and quality contracts; all later work includes those gates.
3. **Independent lower lanes:** path/subprocess work and deployment work proceed only when their explicit prerequisites are green. State, repository, SOPS, filesystem, and snapshot cards branch from named contract commits and rejoin only at their listed consumers.
4. **Application lane:** contract cards 59-64 and 75 precede their phase cards. Apply/add phase cards may parallelize only when they own disjoint files and every consumed phase contract is frozen.
5. **CLI lane:** Task 84 freezes runtime values; command cards 85-92 may then run in parallel. Tasks 93-94 alone assemble root and exit behavior.
6. **Composition and acceptance:** Tasks 95-98 serialize composition. Backend and process fixtures freeze before scenario cards; every scenario card owns one exact test file and no production path.
7. **Delivery and docs:** CI/package/release cards 115-118 serialize where their artifacts depend on one another. Task 119 freezes the documentation checker; documentation cards then own one file each and README lands last.

### Ownership and review rules

- `go.mod`, `go.sum`, `flake.nix`, `flake.lock`, `justfile`, each workflow, each script, and `PLAN.md` have one lifetime owner; no later “finish” card reopens them.
- A worker never edits outside `Owns`, never creates an undeclared file, and never adds a dependency. The integration owner handles a reviewed plan amendment and supplies a new base commit when necessary.
- Every freeze records the plan hash, base/result commits, exact changed paths, nonempty test selector, targeted green output, quality output, and a concise API/schema/artifact summary.
- Review is two-stage: acceptance/specification first, then readable-code/security. The second stage checks 400-line files, 40-line/25-statement functions, 15-line `RunE`, ten decisions, two-level nesting, three parameters, three-method interfaces, exact imports/third-party ownership, globals, exported types, and suppressions.
- No placeholder, TODO, skipped core test, generic helper bucket, compatibility shim, unverified workflow, or undocumented ownership exception crosses a freeze.
- A production defect discovered by an integration card returns to a new plan amendment owned by the original production boundary; the integration worker does not patch it.

---

## 18. Definition of done

The initial release is complete only when all of the following are demonstrated by automated tests and a manual smoke test:

- [ ] `cattery init` creates and canonicalizes a missing repository directory or registers an existing one, without adding metadata/scaffolding.
- [ ] Root files, root dot trees, and groups compile to the specified HOME-relative targets.
- [ ] Unknown scope-root `_...` entries are ignored and not deployed.
- [ ] Linux and Darwin overlays obey literal platform-wins semantics.
- [ ] All cross-source, alias, hard-link-identity, case-folded, and NFC/NFD collisions fail before hooks or writes.
- [ ] New targets are regular files, not symlinks.
- [ ] Source-only changes apply automatically to unchanged targets.
- [ ] Target drift and simultaneous changes require explicit resolution.
- [ ] `cattery add` adopts target content and is the only repository-import path.
- [ ] Source removal, including a whole state-only group, leaves targets and retires local tracking; cross-scope target moves preserve the baseline.
- [ ] Database deletion causes safe unbaselined comparison rather than data loss.
- [ ] Secrets round-trip byte-exactly through SOPS binary mode.
- [ ] Secret plaintext and plain secret hashes are absent from repository plaintext, SQLite, and all Cattery-generated output, errors, and logs; missing/mismatched `hash.key` fails safely.
- [ ] Secret modes are `0600` or `0700`.
- [ ] Before-hook failure causes zero planned Cattery executor/managed state-row writes, and documentation identifies both operational state-store setup and trusted-hook side effects rather than claiming to undo them.
- [ ] After-hook failure does not roll back completed files.
- [ ] Alias destinations contain the exact expected relative symlink payload; absolute/resolution-equivalent alternatives drift and occupied regular/symlink paths prompt.
- [ ] Temporal file/alias representation changes apply automatically only when the old representation is intact, otherwise require an explicit replacement decision, and atomically switch active state only after the replacement is durable.
- [ ] Absolute, traversal, every symlinked parent, protected-tree overlap, and source/target identity are rejected.
- [ ] Structural target transitions and blocking ancestors fail complete preflight and require manual intervention rather than implicit deletion.
- [ ] Mode-only correction of a hard-linked target replaces only the managed path and does not chmod the shared inode.
- [ ] Pre-rename atomic-write failures leave the old destination intact and no temporary leak; any newly created parent directories are reported but not recursively removed; post-rename directory-sync/state failures are reported as partial and recover by equality.
- [ ] Partial applies leave accurate state for every completed target.
- [ ] CLI output and execution order are deterministic.
- [ ] Cobra v1.10.2 is confined to `internal/cli`; no Cobra/pflag type or import reaches bootstrap, application, domain, state, repository, reconciliation, filesystem, hook, or secret code.
- [ ] Every operational Cobra command calls one injected, one-method application service through a typed request/result seam; generated help calls no backend, command tests use fakes, and backend integration tests complete every workflow without constructing Cobra.
- [ ] Every `RunE` is 15 lines or fewer and contains translation/rendering only; all semantic validation, decision policy, orchestration, state access, and side effects are independently callable application/backend behavior.
- [ ] Automated quality tests prove every hand-written implementation/test/script/Nix/workflow/migration file is at most 400 lines, every Go function/literal is at most 40 lines with at most 25 statements and ten decision points, nesting/parameter/three-method-interface limits hold, the exact package/third-party DAG and exported-type boundary hold, no mutable globals or `init` functions exist outside the exact build-info and embedded-migration exceptions, and no generic dumping-ground, suppression, or scan exclusion exists.
- [ ] Every implementation card in Section 16 has its targeted acceptance evidence and no unresolved TODO, skipped core test, placeholder, cross-card file edit, or undocumented plan exception.
- [ ] Linux and macOS CI pass.
- [ ] Native black-box jobs pass on explicit `ubuntu-22.04` and `macos-15` runners; no unsupported older-runtime floor is advertised.
- [ ] Direct modules are the latest versions that pass the Section 13 compatibility matrix; `go list -m -u` reports no pending direct-module update at release preparation time.
- [ ] CI and release workflows use only the exact three full GitHub Actions commits in Section 13; no floating action tag or undeclared external action appears.
- [ ] Go 1.25.x compatibility, Go 1.26.x primary builds, all four release cross-builds, and the pinned SOPS/age binary round trip pass.
- [ ] Packaged archives have deterministic tag/commit timestamps, verified SHA-256 checksums, and pass the isolated ordinary-plus-secret artifact smoke script.
- [ ] `just check` and `nix flake check` pass from a clean checkout.
- [ ] README accurately describes every destructive and non-destructive behavior.

---

## 19. Fixed decisions and future extension points

These decisions are fixed for the first release:

- Go, not Rust.
- Cobra v1.10.2 as a thin, replaceable CLI adapter over Cobra-free typed application services.
- No Cobra or pflag types cross the `internal/cli` boundary, and no command contains business logic.
- Strict hand-written source limits: 400 lines per implementation/test/build-automation file, 40 lines and 25 statements per Go function/literal, ten decision points, and 15 lines per `RunE`, enforced without an initial-release source allowlist.
- Materialized files by default.
- Group is the public organizational term.
- SQLite state under XDG state directories.
- BLAKE3 exact-byte baselines, keyed with local `hash.key` for secret semantics.
- SOPS CLI as the encryption compatibility boundary.
- Binary SOPS representation for every secret.
- Explicit `add` for target adoption.
- No automatic source or target deletion.
- No templates.
- No broad permission management.
- No daemon or plugin system.
- Symlinks only as marked aliases in `_routes.toml`.
- Symlinked target and alias parents rejected in the initial release.
- Unknown underscore controls ignored.
- Package management delegated to hooks.

Likely future extensions, intentionally excluded now, are recursive directory `add`, explicit target removal, additional platforms, directory aliases, machine/host overlays, structured secret editing, machine-readable JSON output, and package-manager-specific helpers. Their possibility must not distort the initial interfaces beyond keeping paths, layers, source kinds, and state migrations explicit.

The repository tree is the declaration. SQLite is a baseline ledger. Cattery should remain understandable by looking at those two facts and the command being run.
