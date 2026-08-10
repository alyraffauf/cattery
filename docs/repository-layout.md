# Repository layout

A Cattery repository is a plain directory tree of literal files. There is no
manifest, no marker file, and no templating. Cattery copies ordinary files
beneath `$HOME` and records what it installed in a local SQLite state database.

This document describes the layout Cattery's compiler accepts and is the
contract every command validates against.

## Root files

A regular file at the repository root deploys directly beneath `$HOME`:

```
repo/.bashrc      -> $HOME/.bashrc
repo/Brewfile     -> $HOME/Brewfile
```

A top-level directory whose name begins with a dot is an ungrouped HOME-relative
tree:

```
repo/.config/starship.toml -> $HOME/.config/starship.toml
```

## Groups

Every other ordinary top-level directory is a **group**: a named bundle of files
for one application, integration, or project. The directory name is the group
name.

```
repo/atuin/.config/atuin/config.toml        -> $HOME/.config/atuin/config.toml
repo/atuin/.config/fish/conf.d/atuin.fish    -> $HOME/.config/fish/conf.d/atuin.fish
```

Group names are single path segments. They cannot begin with `.` or `_`, cannot
contain a slash, and cannot be `.` or `..`. Two group names that differ only by
Unicode case or NFC/NFD form are rejected, even when their target trees do not
overlap.

A non-dot target tree such as `Library/` or `bin/` cannot live at the root; put
it inside a group.

## Reserved control namespace

Names beginning with `_` are reserved at the repository root and at each group
root. Cattery recognizes these controls:

```
_darwin/         macOS-only overlay
_linux/          Linux-only overlay
_secrets/        SOPS-encrypted secret sources
_hooks/          trusted hook scripts
_routes.toml     explicit symlink aliases
```

Unknown underscore-prefixed entries (for example `_notes/` or `_README.md`) are
ignored recursively and never deployed.

The reservation applies only at a control-bearing scope root. Once you are
inside an ordinary target tree, every name is literal, including names that
begin with `_`:

```
repo/app/.config/app/_internal/value -> $HOME/.config/app/_internal/value
```

## Repository metadata

These repository-root entries are version-control metadata and are never
deployed:

```
.git  .github  .gitignore  .gitattributes  .gitmodules  .sops.yaml
```

A group may deploy a file named `.gitignore`; the metadata exclusion applies
only at the repository root. Other root regular files are literal sources, so
repository-only prose should be named `_README.md` rather than `README.md`.

## Platform overlays

A root scope or a group may contain `_darwin/` and `_linux/` overlays. Cattery
uses the active runtime (`runtime.GOOS`) and initially supports only `darwin`
and `linux`.

```
repo/ghostty/.config/ghostty/config      base layer
repo/ghostty/_darwin/.config/ghostty/config   macOS override
repo/ghostty/_linux/.config/ghostty/config    Linux override
```

Overlay merge rules:

- a platform file always wins over a base file at the same relative path;
- a platform directory replaces a base file at that path;
- a platform file replaces a base directory and suppresses the base descendants
  beneath it;
- two platform directories merge recursively, with platform entries winning.

Validation compiles and collision-checks both the Linux and Darwin plans
regardless of host OS, so an error in an inactive overlay is caught before it
reaches another machine.

See [secrets.md](secrets.md) for platform-specific secrets and [hooks.md](hooks.md)
for hook placement.

## Supported source entries

- Regular files, including empty and binary files.
- Directories are structural and merged.
- Source symlinks are rejected; Cattery does not follow them.
- FIFOs, sockets, and device entries are rejected.
- Timestamps, ownership, ACLs, and extended attributes are not copied.

## Selecting scopes

- `cattery validate` selects root scope plus every current group; explicit group
  names must exist in the compiled repository.
- `cattery status`, `cattery diff`, and `cattery apply` with no arguments select
  root scope plus every current and active state-only group. An explicit name
  may also name a deleted group that still has state rows, so it can be retired.
- Unknown or duplicate group arguments are usage errors.

See [reconciliation.md](reconciliation.md) for how selected scopes are compared,
and the [README](../README.md) for the command summary.
