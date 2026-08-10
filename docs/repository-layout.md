# Repository layout

A Cattery repository is an ordinary directory tree. Its files are the desired
contents of `$HOME`; there is no template language, marker file, or generated
manifest to maintain. Cattery validates this layout before it applies anything.

## Files, directories, and groups

A file at the repository root maps directly below `$HOME`:

```text
repo/.bashrc                    -> ~/.bashrc
repo/.config/starship.toml       -> ~/.config/starship.toml
```

Top-level dot-directories are also direct `$HOME` trees. Any other ordinary
top-level directory is a **group**: a named bundle you can select independently.

```text
repo/atuin/.config/atuin/config.toml       -> ~/.config/atuin/config.toml
repo/atuin/.config/fish/conf.d/atuin.fish  -> ~/.config/fish/conf.d/atuin.fish
```

Group names are one path segment. They cannot start with `.` or `_`, contain a
slash, or be `.` or `..`. Names that only differ by Unicode case or
normalization are rejected so the repository stays portable between filesystems.

Put a non-dot target tree such as `Library/` or `bin/` inside a group. Root
directories with those names are groups, not direct target trees.

## Controls

At the repository root and each group root, names beginning with `_` are
reserved:

| Control | Purpose |
| --- | --- |
| `_darwin/` | macOS-only file overrides. |
| `_linux/` | Linux-only file overrides. |
| `_secrets/` | SOPS-encrypted sources. |
| `_hooks/` | Trusted scripts run around `apply`. |
| `_routes.toml` | Explicit target symlinks. |

Unknown underscore-prefixed entries are ignored. Within a normal target tree,
underscore-prefixed names are literal: `app/.config/app/_cache/value` deploys
to `~/.config/app/_cache/value`.

## Platform overrides

Use `_darwin/` or `_linux/` beside a base tree when a file differs by platform:

```text
repo/ghostty/.config/ghostty/config              base
repo/ghostty/_darwin/.config/ghostty/config      macOS override
repo/ghostty/_linux/.config/ghostty/config       Linux override
```

An override wins at the same path. Directories merge recursively; an override
file replaces a base directory at that path, and an override directory replaces
a base file. `cattery validate` compiles both Linux and macOS plans, including
the inactive one, so an error does not wait for another machine to discover it.

## Symlink routes

Source symlinks are rejected. If a target needs a symlink, declare it explicitly
in `_routes.toml` instead. The canonical path must name a managed file or a
managed directory in the same scope.

```toml
version = 1

[symlinks.linux]
".config/zed" = [".var/app/dev.zed.Zed/config/zed"]

[symlinks.darwin]
".config/Code/User" = ["Library/Application Support/Code/User"]
```

The paths on the right become relative symlinks to the path on the left. Cattery
does not follow the destination while it creates a route. It will not replace an
occupied directory automatically; resolve that case yourself, then apply again.

## Secrets

Put encrypted sources under `_secrets/` and preserve the target path below it:

```text
repo/app/_secrets/.config/app/credentials          -> ~/.config/app/credentials
repo/app/_darwin/_secrets/.config/app/credentials  -> macOS-only secret
```

See [secret operations](secrets.md) for setup and handling details.

## Repository-only files

At the repository root, `.git`, `.github`, `.gitignore`, `.gitattributes`,
`.gitmodules`, `.sops.yaml`, and `.catteryignore` are metadata and never deploy.
Use `.catteryignore` for other helper material:

```gitignore
# Repository-only material
README.md
scripts/
**/*.example
```

Patterns are relative to the repository root. Blank lines and `#` comments are
ignored; `*`, `?`, `**`, and trailing `/` are supported. Negated patterns are
not supported.

## What can be a source

Regular files, including binary and empty files, are supported. Directories are
structural. Symlinks, FIFOs, sockets, and device entries are rejected. Cattery
does not preserve timestamps, ownership, ACLs, or extended attributes.

## Selecting groups

`validate`, `status`, `diff`, and `apply` accept group names. With no names,
they select the root scope and every current group; status-like commands also
include state-only groups so removed sources can be retired safely. Unknown or
repeated group names are errors.

Next: [reconciliation and recovery](reconciliation.md), [secret operations](secrets.md),
and [trusted hooks](hooks.md).
