# Cattery

Cattery is a safe, cross-platform dotfiles manager. It stores literal files in a
central repository, materializes ordinary files beneath `$HOME`, detects drift
with SQLite-backed three-way reconciliation, supports literal platform overlays,
protects file-level secrets with SOPS and age, and runs deterministic hooks.

Cattery targets Linux and macOS. It copies ordinary files (not symlinks), never
silently destroys existing content, and records what it installed in a local
state database so it can tell an intentional edit from a conflict.

## Install

Cattery is a single static binary. Build it with Go 1.25 or newer:

```
go build ./cmd/cattery
```

A Nix flake is provided for development:

```
nix develop
just check
```

## Repository example

A repository is a plain directory of literal files. Root files and dot-trees
deploy beneath `$HOME`; other top-level directories are groups.

```
cattery/
  .bashrc                     -> ~/.bashrc
  .config/starship.toml       -> ~/.config/starship.toml
  atuin/
    .config/atuin/config.toml -> ~/.config/atuin/config.toml
  ghostty/
    .config/ghostty/config        base layer
    _darwin/.config/ghostty/config   macOS override
    _linux/.config/ghostty/config    Linux override
    _secrets/.config/ghostty/key     encrypted secret
    _hooks/after/reload.sh
```

Register a repository as the default for your home:

```
cattery init ~/src/cattery
```

See [repository layout](docs/repository-layout.md) for the full grammar.

## Commands

| Command | Purpose |
|---|---|
| `cattery init [PATH]` | register a repository as the default for this home |
| `cattery validate [GROUP ...]` | compile and validate the repository |
| `cattery status [GROUP ...]` | report drift without changing anything |
| `cattery diff [GROUP ...]` | show safe content differences |
| `cattery apply [GROUP ...]` | reconcile targets with the repository |
| `cattery add [OPTIONS] FILE ...` | adopt target content into the repository |
| `cattery version` | print version, commit, and build metadata |

Global options are `--repo PATH` and `--verbose`. Apply accepts `--dry-run`,
`--non-interactive`, and `--no-hooks`. Add accepts `--group NAME`,
`--platform linux|darwin`, `--secret`, and `--dry-run`.

When `cattery apply` needs a decision it prompts for overwrite, skip, abort, or
diff, and collects every decision before changing anything. Piped or
non-terminal input is non-interactive: any required prompt stops the command
before any write.

## Safety model

- Cattery compares source, target, and the last baseline, so it never silently
  overwrites a file you edited.
- Every write goes through a same-directory temporary file, an `fsync`, a
  revalidation, an atomic rename, and a parent-directory sync before its
  baseline row is committed.
- Cattery never deletes a target and never rolls back a completed write.
- Deleting the state database is recoverable: equal files are re-adopted and
  differing files are left for an explicit decision.

See [reconciliation and recovery](docs/reconciliation.md) for the full model.

## Secrets

Secrets are stored as SOPS-encrypted binary payloads under `_secrets/` and
decrypted only in memory when needed. Plaintext never enters the database, logs,
arguments, errors, diff output, or a general temporary directory. Secret targets
are always mode `0600`, or `0700` when executable. See
[secret operations](docs/secrets.md).

## Hooks

Hooks are trusted programs that run before and after an apply, for the whole
repository and per group. They inherit your environment plus Cattery variables,
run in their own process group, and are cancelled with SIGTERM, a five-second
grace period, then SIGKILL. See [trusted hooks](docs/hooks.md).

## Non-goals

Cattery is deliberately narrow. The initial release has:

- no templating, interpolation, or patch language;
- no GNU Stow-style symlink farm (targets are ordinary files);
- no daemon, file watcher, or automatic sync;
- no plugin system;
- no package-manager integration (install software with hooks);
- no rollback or target deletion (Cattery never destroys existing files);
- no Windows support in the initial release.

## License

MIT.
