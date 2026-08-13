# CLI output

Cattery's default output is written for people. Every command uses the same
order: the outcome, the affected paths, and the next safe action.

## Reading changes

`status`, `diff`, and every `--dry-run` command are read-only. Their output
always says that no files were changed. `status` reports the work Cattery sees;
`diff` adds a safe text difference where one can be shown.

Action labels describe the effect on your home directory:

| Label | Meaning |
| --- | --- |
| `Create` | Create a target reported by `status`. |
| `Update` | Bring a managed file or its permissions up to date. This may create a missing file during `apply`. |
| `Replace` | Replace a conflicting local file after your confirmation. |
| `Link` | Create or update an explicit symlink route. |
| `Forget` | Stop tracking a repository source. Files in `$HOME` stay in place. |
| `Skipped` | Leave an item unchanged because it still needs attention. |

Paths beginning with `~/` are under the current home directory. Repository
sources are shown only as context. Cattery quotes paths containing ambiguous
whitespace or control characters so a filename cannot change the terminal
layout.

## Prompts and safety

When the repository and local versions conflict, Cattery explains that it
cannot safely choose between them and shows a safe difference for ordinary
files. The choices always mean the same thing:

- `r`: replace the local file with the repository version.
- `s`: skip the file and keep the local version.
- `a`: abort before Cattery changes anything.

Before writing, Cattery presents the selected changes once more. Pressing Enter
at confirmation is safe: it does not apply changes.

Secret plaintext is never shown in output, differences, or prompts.

## Exit codes

The human-facing wording does not change Cattery's exit codes. See
[reconciliation](reconciliation.md#exit-status) for their meanings. In
particular, exit code `2` means Cattery found work that needs review or a
decision; it does not mean an unsafe write occurred.
