# Trusted hooks

Hooks let a repository run arbitrary trusted programs around an apply, such as
installing packages, reloading a service, or bootstrapping a tool. Cattery has
no plugin runtime and no built-in package-manager integration; hooks are the
extensibility point.

## Layout

Hooks live under `_hooks/` at the repository root and optionally at each group
root:

```
repo/_hooks/before/01-install.sh
repo/_hooks/after/reload.sh
repo/atuin/_hooks/after/sync.fish
```

Repository hooks run for every apply, including group-selective applies. Group
hooks run only when their group is selected.

## Validation

- Hook files must be direct regular-file children of `before/` or `after/`.
- Every hook must have at least one executable bit.
- Non-executable files, nested directories, symlinks, and special files are
  validation errors, not silently ignored entries.
- Hooks execute directly without an implicit shell, so each script needs a valid
  shebang.
- Hook order is bytewise path order within a phase.

## Order

For selected groups sorted lexicographically, a full apply runs:

```
resolve and validate the entire plan
collect every interactive decision
repository before hooks
all selected group before hooks
revalidate every selected source and target snapshot
filesystem file application
symlink alias application
retire missing state rows
all selected group after hooks
repository after hooks
post-verify selected sources, targets, and aliases
refresh no-write equality baselines that passed verification
```

All before hooks complete before the first managed write. If any before hook
fails, Cattery stops immediately and performs none of those writes. After hooks
run only when the filesystem phase completed. If an after hook fails, remaining
after hooks still run, every failure is reported, the command exits non-zero,
completed writes stay applied, and Cattery never rolls back.

## Environment and streams

Hooks inherit the caller environment and receive:

```
CATTERY_REPO=<absolute repository path>
CATTERY_HOME=<absolute home path>
CATTERY_PLATFORM=linux|darwin
CATTERY_PHASE=before|after
CATTERY_GROUP=<group name, or empty for a repository hook>
CATTERY_RESULT=pending|success|partial
```

Before hooks receive `CATTERY_RESULT=pending`. After hooks receive `success`
when every selected item converged, or `partial` when the filesystem phase
completed but one or more items were skipped. The working directory is always
the repository root, and hooks receive no positional arguments. Hook stdout and
stderr are inherited directly so interactive tools behave normally.

## When hooks run

Hooks run even when selected files are no-ops, so a bootstrap hook can represent
work outside Cattery's file model. `--dry-run` and `--no-hooks` suppress hooks
entirely. A mid-filesystem operational failure skips every after hook.

## Trust and cancellation

Hooks are trusted arbitrary programs. Cattery does not sandbox them and cannot
redact a hook that deliberately prints deployed secret files. Secret-output
guarantees apply to Cattery-generated diagnostics, not inherited hook output.

There is no default timeout. Each hook starts in its own Unix process group. On
cancellation Cattery sends SIGTERM to the group, allows a five-second grace
period, then SIGKILL any remaining members. This prevents package-manager
descendants from outliving an interrupted Cattery process.

See [reconciliation.md](reconciliation.md) for how hook results interact with
exit status, and [repository-layout.md](repository-layout.md) for hook placement
rules.
