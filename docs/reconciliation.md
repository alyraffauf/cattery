# Reconciliation and recovery

Cattery reconciles three views of every managed file: the **source** in the
repository, the **target** beneath `$HOME`, and the **baseline** recorded in the
local SQLite state database from the last successful apply. This three-way model
is what lets Cattery distinguish an intentional edit from a conflict without
ever silently destroying a file.

This document describes reconciliation, retirement, and recovery behavior.

## The core matrix

For a managed file that has a baseline, Cattery compares exact bytes (line
endings and encoding are never normalized) and classifies the result:

| Source vs baseline | Target vs baseline | Source vs target | Result |
|---|---|---|---|
| unchanged | unchanged | equal | no-op (mode may still be corrected) |
| changed | unchanged | different | apply source automatically |
| unchanged | changed | different | target drift; decision required |
| changed | changed | equal | already converged; baseline is refreshed |
| changed | changed | different | conflict; decision required |

Executable bits are reconciled independently. A source executable-bit change is
intended metadata and applies automatically; an application-side executable-bit
change is corrected automatically rather than treated as adoptable drift.

## Targets without a baseline

When a file has no baseline (for example after the state database is deleted),
Cattery is deliberately safe:

- target absent: create it from source automatically;
- target bytes equal source: adopt the equal pair as a new baseline without
  rewriting;
- target differs: decision required.

Deleting the database is therefore recoverable: equal files are re-adopted and
differing files are never overwritten silently.

## Decisions

When a target has drifted, a target conflicts with a source change, or an
unbaselined target differs, `cattery apply` collects every decision before it
mutates anything. The prompt offers:

```
[d]iff
[o]verwrite target from repository
[s]kip
[a]bort
```

- `diff` prints a safe unified diff and repeats the prompt (secrets never show a
  diff and never expose plaintext).
- `overwrite` records permission for that one target only.
- `skip` leaves the target unresolved, continues with other work, and produces a
  nonzero final status.
- `abort` performs no hooks and no writes; every decision is collected first.

To adopt target content into the repository instead, run `cattery add`. Apply
never imports target bytes into the repository, including through a prompt.

When standard input is not a terminal, behavior is non-interactive: any prompt
that would be required stops the whole command before any write. `--non-interactive`
forces that mode.

## Modes

- A new ordinary target defaults to mode `0644` plus the source executable bits
  (commonly `0755`).
- An existing ordinary target keeps its read/write bits; only executable bits are
  replaced from the source.
- A mode-only correction rematerializes the target so a hard-linked alias
  elsewhere is never mutated by `chmod`.
- Secret targets are always `0600`, or `0700` when the source is executable. See
  [secrets.md](secrets.md).

## Atomic writes and crash safety

Every target and source write streams bytes into a uniquely named temporary file
in the destination directory, applies the final mode, syncs, revalidates, and
then renames over the destination. The parent directory is synced before the
corresponding baseline row is committed. Cattery updates state only after the
filesystem operation succeeds.

Consequences:

- a crash after rename but before the database update leaves source and target
  equal; the next run establishes a converged baseline;
- the database can never claim bytes were installed before they actually were;
- earlier files remain accurately recorded when a later file fails.

This is what Cattery means by crash safe. It does not promise survival against
storage hardware that acknowledges sync without honoring it.

## No rollback

Cattery never rolls back a completed write and never deletes a target. There is
no rollback, no generation history, and no automatic target removal. If a source
is removed, the target is left in place and only its tracking row is retired.

## Source removal

`cattery forget DIRECTORY --yes` removes every base and platform-specific
repository source that manages the named subtree, then retires its active file
rows. It never deletes or rewrites anything under `$HOME`. Use `--dry-run` to
inspect the exact sources first. Forget refuses a subtree involved in an alias,
so aliases must be removed deliberately before the canonical files are
forgotten.

When a selected state row's target has no producer anywhere in the current plan,
Cattery retires only the tracking row:

- the target is left untouched on disk;
- the row is marked retired after a successful apply;
- the previous baseline is retained for diagnostics and safe reactivation;
- if the source later reappears, the row is reactivated and reconciled against
  the retained baseline.

## Representation transitions

A path that changes between a regular file and an explicit alias is a
representation transition, not ordinary retirement. Cattery first checks that
the old representation is intact (matching baseline fingerprint and mode, or the
exact recorded symlink payload). When it is intact, the representation change
applies automatically under the normal source-only rule. When the old
representation has drifted or is missing, Cattery asks before replacing it. See
[repository-layout.md](repository-layout.md) for how aliases are declared.

## Exit status

```
0  command completed and selected state is converged
1  operational, validation, usage, or filesystem error
2  unresolved drift, conflict, or reported differences
3  hook failure
4  required external dependency missing
```

Interactive skips yield exit 2 after all other resolved work completes. See the
[README](../README.md) for the command surface.
