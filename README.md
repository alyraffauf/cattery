# Cattery

Cattery is a safe, cross-platform dotfiles manager for Linux and macOS. Keep
your configuration as ordinary files in one repository; Cattery installs them
under `$HOME`, supports platform overrides and SOPS-encrypted secrets, and
shows you when a local edit needs a decision.

It is intentionally small: no templates, daemon, plugin runtime, or surprise
symlink farm. You own plain files and explicit rules; Cattery does the careful
part of getting them into place.

## Start here

Build from source with Go 1.26 or newer:

```sh
go build -o ~/.local/bin/cattery ./cmd/cattery
```

Or install the package with Nix:

```sh
nix profile install .#cattery
```

Create a repository, register it, inspect the plan, then apply it:

```sh
mkdir -p ~/src/dots/.config/ghostty
cp ~/.config/ghostty/config ~/src/dots/.config/ghostty/config

cattery init ~/src/dots
cattery status
cattery apply
```

`status` never changes anything. `apply` asks before replacing a locally
modified file. See [repository layout](docs/repository-layout.md) for the full
layout and [reconciliation](docs/reconciliation.md) for exactly how decisions
work.

## Repository layout

Root files and dot-directories map directly into `$HOME`. Other top-level
directories are named groups, useful for selecting one application at a time.

```text
dots/
  .bashrc                              -> ~/.bashrc
  .config/starship.toml                 -> ~/.config/starship.toml
  ghostty/
    .config/ghostty/config              -> ~/.config/ghostty/config
    _darwin/.config/ghostty/config      -> macOS-only override
    _linux/.config/ghostty/config       -> Linux-only override
    _secrets/.config/ghostty/token      -> encrypted secret
    _routes.toml                        -> explicit symlink routes
```

The layout is regular files first. Cattery rejects source symlinks, but you can
declare a deliberate target symlink—for example, a Flatpak application’s
configuration directory—in `_routes.toml`.

## Everyday commands

| Command | What it does |
| --- | --- |
| `cattery init [PATH]` | Register a repository as this home’s default. |
| `cattery validate [GROUP ...]` | Check repository structure on Linux and macOS. |
| `cattery status [GROUP ...]` | Report pending changes without modifying anything. |
| `cattery diff [GROUP ...]` | Show safe differences for ordinary files. |
| `cattery apply [GROUP ...]` | Reconcile selected files with the repository. |
| `cattery add [OPTIONS] TARGET ...` | Adopt existing files or directories into the repository. |
| `cattery forget DIRECTORY --yes` | Stop managing a directory; leaves its files in `$HOME`. |
| `cattery secrets list [GROUP ...]` | List safe metadata for encrypted sources. |
| `cattery secrets verify [GROUP ...]` | Check that encrypted sources can be decrypted. |
| `cattery secrets reencrypt [GROUP ...]` | Preview or apply current SOPS rules and recipients. |
| `cattery version` | Print build information. |

Global options are `--repo PATH` and `--verbose`. `apply` supports `--dry-run`,
`--non-interactive`, `--no-hooks`, and `--skip-secrets`; `add` supports `--group`, `--platform`,
`--secret`, and `--dry-run`; `forget` supports `--dry-run` and requires `--yes`
to remove repository sources. Secret lifecycle commands accept repeatable
`--source REPOSITORY_PATH` selectors; `secrets reencrypt` previews by default
and requires `--yes` to replace encrypted sources.

## Safety, in plain English

- Cattery tracks the repository file, the file in `$HOME`, and the last known
  good baseline. It does not silently overwrite a local edit.
- Every file publication uses a temporary file, revalidation, atomic rename,
  and directory sync before state is updated.
- Cattery never deletes a target in `$HOME`. `forget` removes management, not
  your files.
- If the local state database is lost, equal files are adopted again and
  different files require a decision.
- A non-interactive session stops before any change that would need a prompt.

## Secrets and hooks

Secrets use your existing [SOPS](https://getsops.io/) setup. Cattery stores
encrypted payloads in `_secrets/`, decrypts only when necessary, and never
prints secret plaintext in status, diff, or prompts. Read
[secret operations](docs/secrets.md) before adopting your first secret.

Hooks are trusted scripts run before and after `apply`. They are a good place
for intentional work outside the file model, such as installing packages or
reloading a service. Read [trusted hooks](docs/hooks.md) before enabling them.

## What Cattery does not do

- Template, interpolate, or patch configuration files.
- Watch files or sync automatically.
- Manage packages directly.
- Provide rollback, generations, or target deletion.
- Support Windows.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The normal local check is `just check`.

## License

MIT.
