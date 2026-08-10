# Hooks

Hooks are trusted programs Cattery runs before or after `apply`. Use them for
intentional work outside Cattery’s file model: installing packages, restarting
a service, or generating a cache. They are not a plugin system and they are not
sandboxed.

## Layout

Place hooks at the repository root or inside a group:

```text
repo/_hooks/before/01-install.sh
repo/_hooks/after/reload.sh
repo/atuin/_hooks/after/sync.fish
```

Repository hooks run on every apply, including a group-selective apply. A group
hook runs only when that group is selected.

Hook files must be direct, executable regular files in `before/` or `after/`.
They run directly, so include a shebang. Symlinks, nested directories, and
non-executable files are validation errors.

## When they run

For an apply with selected groups, Cattery:

1. Validates the whole plan and collects every required decision.
2. Runs repository `before` hooks, then selected group `before` hooks.
3. Revalidates sources and applies files, routes, and state retirement.
4. Runs selected group `after` hooks, then repository `after` hooks.

All `before` hooks must succeed before Cattery changes managed files. `after`
hooks run only after the filesystem phase completes. If an after hook fails,
Cattery reports the failure but does not roll back work that already succeeded.

Hooks run even when files are already converged. `--dry-run` and `--no-hooks`
skip them entirely.

## Environment

Hooks inherit your environment, run from the repository root, receive no
positional arguments, and additionally receive:

```text
CATTERY_REPO      absolute repository path
CATTERY_HOME      absolute home path
CATTERY_PLATFORM  linux or darwin
CATTERY_PHASE     before or after
CATTERY_GROUP     group name; empty for repository hooks
CATTERY_RESULT    pending, success, or partial
```

`CATTERY_RESULT` is `pending` before changes. After changes it is `success`
when the selected work converged, otherwise `partial`.

## Trust and interruption

Treat hooks as code review territory. They can read files, change the machine,
and print secret plaintext; Cattery’s secret-redaction guarantees do not apply
to hook output.

There is no default timeout. On cancellation, Cattery terminates the hook’s
process group and then force-stops anything that remains after a short grace
period, so descendants do not outlive the command.
