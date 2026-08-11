# Reconciliation and recovery

Cattery compares three things for every managed path: the repository source,
the target in `$HOME`, and the **baseline** recorded after the last successful
apply. That lets it distinguish a repository update from a local edit without
silently overwriting either.

## What happens when files differ

For a path with a baseline, Cattery compares exact bytes; it never normalizes
line endings or encoding.

| Repository | `$HOME` target | Result |
| --- | --- | --- |
| unchanged | unchanged | Nothing to write. |
| changed | unchanged | Apply the repository version automatically. |
| unchanged | changed | Ask before replacing the local edit. |
| changed | changed, now equal to source | Refresh the baseline. |
| changed | changed, still different | Ask before replacing the local edit. |

Executable bits are handled separately. A repository executable-bit change is
intentional and applies automatically. A target-side mode change is corrected
without treating it as content to import.

## First apply and lost state

If a path has no baseline—on its first apply or after state is restored poorly—
Cattery behaves conservatively:

- Missing target: create it from the repository.
- Equal target and source: record a baseline without rewriting either file.
- Different target and source: ask first.

You can therefore recover from losing the state database. You do not need to
choose between silently replacing local work and abandoning the repository.

## Decisions

Before any write, interactive `apply` gathers every decision it needs:

Ordinary files show a safe unified diff automatically. Secrets only report that
encrypted content differs and never expose plaintext. Choose `r` (or
`repository`) to authorize that target, `s`/`skip` to leave it, or `a`/`abort`
to stop. After all choices, `apply` shows a resolution summary and requires
`y` before hooks or writes begin. `apply --force` selects repository for every
selected conflict without disabling validation or write safeguards. To adopt
local content into the repository, use `cattery add` instead.
`skip` leaves it unresolved while other safe work can continue; `abort` stops
before hooks or writes begin.

Piped input is non-interactive. So is `--non-interactive`: if a decision would
be needed, Cattery stops before changing anything. To put the target into the
repository instead, use `cattery add`; apply never imports target content.

## Writes, modes, and crashes

New ordinary targets use `0644` plus any executable bits from the source. An
existing ordinary target keeps its read/write bits and receives the source’s
executable bits. Secret targets always use `0600`, or `0700` when executable.

Every source or target write is staged in the destination directory, synced,
revalidated, atomically renamed into place, then followed by a directory sync.
Cattery records the new baseline only after that succeeds. If the machine fails
between publication and the database update, the next run sees equal files and
repairs the baseline. This is crash safety, not a promise against faulty storage
that lies about `fsync`.

## Removing management

Cattery never deletes a target in `$HOME`, rolls back completed work, or keeps
generations. If a source disappears, its state row becomes retired and the
target remains in place.

Use `cattery forget DIRECTORY --dry-run` to preview source removal, then
`cattery forget DIRECTORY --yes` to remove every matching base and platform
source from the repository. It retires active file rows and leaves the target
tree untouched. Forget refuses a directory involved in a route; remove that
route deliberately first.

## Routes

Changing a path between a regular file and an explicit symlink route is a
representation transition. Cattery verifies the old representation first. If
it has drifted or disappeared, it asks before replacing it. See
[repository layout](repository-layout.md#symlink-routes).

## Exit status

| Code | Meaning |
| --- | --- |
| `0` | Completed and selected paths are converged. |
| `1` | Usage, validation, operational, or filesystem failure. |
| `2` | Unresolved drift, conflict, skip, or reported difference. |
| `3` | Hook failure. |
| `4` | A required external dependency is missing. |
