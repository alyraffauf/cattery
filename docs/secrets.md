# Secret operations

Cattery stores file-level secrets as SOPS-encrypted binary payloads and
decrypts them only in memory, only when a command needs secret semantics. This
document describes the storage and state model. Cattery does not embed or reimplement
SOPS cryptography; it shells out to the installed `sops` executable.

## Setup

Install the verified external tools and put them on `PATH`:

- `sops` v3.13.3 or newer
- `age` v1.3.1 or newer

Cattery runs SOPS with the repository root as its working directory. SOPS owns
recipient selection, key discovery, and `.sops.yaml` creation rules. Cattery
recommends age, but does not reject another SOPS-supported backend.

Create an age identity pair outside Cattery (age identities are an external
bootstrap prerequisite). A typical repository `.sops.yaml` references the age
**public** recipient:

```yaml
creation_rules:
  - path_regex: .*
    age: age1<your-public-recipient>
```

Never commit an age private identity as the sole key required to decrypt a
Cattery-managed secret. Cattery cannot bootstrap the only key it needs.

## Storage layout

Secrets live under `_secrets/` while preserving the target path literally. A
platform-specific secret composes by putting `_secrets/` inside the platform
layer:

```
repo/app/_secrets/.config/app/credentials          base secret
repo/app/_darwin/_secrets/.config/app/credentials  macOS secret override
```

```
repo/app/_secrets/.config/app/credentials -> $HOME/.config/app/credentials
```

Rules:

- `_secrets/` at a scope root holds base-layer secret sources.
- `_darwin/_secrets/` and `_linux/_secrets/` hold platform-layer secret sources.
- A normal source and a secret source at the same path in the same layer is an
  error.
- A platform source may replace a base source regardless of which is normal or
  secret; the selected source decides secrecy and mode.

## Binary storage format

Cattery treats every secret as an opaque binary file, even when the target is
YAML, JSON, ENV, or INI.

- Plaintext is encrypted with `sops encrypt --input-type binary --output-type json`.
- Encrypted JSON is stored at the literal source path. There is no `.sops`,
  `.enc`, or other filename transformation.
- Decryption uses `sops decrypt --input-type json --output-type binary`.
- Plaintext is sent to SOPS on stdin; Cattery never asks SOPS to reopen a mutable
  repository path.

Before adopting encrypted output, Cattery decrypts the candidate bytes back and
requires a byte-exact match with the original plaintext. Syntax-valid but
undecryptable output never replaces a repository source.

## add and apply

- `cattery add --secret FILE` adopts target bytes as an encrypted source under
  the selected layer's `_secrets/`. A `--platform` may target the active
  platform's overlay; absence writes the base layer.
- `cattery apply` decrypts a secret only when raw encrypted bytes differ from
  the stored source hash, the row is unbaselined, or target bytes must be
  written. An unchanged encrypted source is classified without decrypting it.

A SOPS re-encryption that leaves plaintext unchanged is not a semantic change;
Cattery refreshes only the raw storage hash.

## Modes

Secret modes are exact and enforced even when content is otherwise a no-op:

- a non-executable secret target is always `0600`;
- an executable secret target is always `0700`;
- the executable bit of the encrypted source records whether the decrypted
  target is executable.

## Plaintext boundaries

Plaintext may exist briefly in Cattery memory during hashing, classification,
encryption, and target writing. It is never:

- stored in the SQLite database;
- written to logs, command-line arguments, or error messages;
- printed by `status`, `diff`, or interactive prompts;
- written to a general temporary directory.

Cattery uses bounded, overflow-checked writers when capturing SOPS output and
clears its owned plaintext buffers best effort after use. Go does not guarantee
elimination of every memory copy, so this is exposure reduction, not guaranteed
erasure.

## The hash key

A 32-byte keyed-hash secret lives beside the state database so secret baseline
fingerprints are keyed (BLAKE3 keyed mode) rather than stored as plain hashes.
This reduces offline guessing if the database alone is disclosed.

- The key is created from `crypto/rand` the first time a secret baseline is
  recorded.
- Only an identifier (`BLAKE3` of the key) is stored, so replacement can be
  detected without storing the key itself.
- If secret baseline rows exist but the key is missing or does not match its
  identifier, Cattery fails safely and requires you to restore the matching key
  or explicitly reset state. It never silently generates a replacement that
  would make old baselines incomparable.

Back up the hash key with your age identity. Both are operational prerequisites
that Cattery cannot reconstruct.

## Missing SOPS

A missing `sops` executable is checked only when a selected operation needs
encryption or decryption. The error names the missing dependency, returns exit
status 4, and leaves repository, target, and managed state rows untouched.

See [repository-layout.md](repository-layout.md) for the `_secrets/` grammar and
[reconciliation.md](reconciliation.md) for the broader decision model.
