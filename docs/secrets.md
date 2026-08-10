# Secrets

Cattery uses [SOPS](https://getsops.io/) for file-level secrets. You keep
control of recipients and identities; Cattery keeps encrypted files in the
repository and writes plaintext only to its intended target under `$HOME`.

## Before you begin

Install `sops` and your chosen key backend, such as `age`, and make them
available on `PATH`. Define recipients in `.sops.yaml` at the repository root.

```yaml
creation_rules:
  - path_regex: .*
    age: age1replace-with-your-public-recipient
```

Keep private identities outside the repository. Make sure more than one trusted
recovery path can decrypt important secrets; Cattery cannot recover a lost
identity for you.

If you install Cattery through its Nix package, the packaged command includes
`sops` and `age` on its runtime path. Other installations use the tools from
your shell’s `PATH`.

## Add a secret

Adopt an existing file as an encrypted source:

```sh
cattery add --secret ~/.config/example/token
```

The source is stored below `_secrets/` while keeping its target path:

```text
repo/app/_secrets/.config/example/token -> ~/.config/example/token
```

For a platform-specific secret, put `_secrets/` inside `_darwin/` or `_linux/`:

```text
repo/app/_darwin/_secrets/.config/example/token
```

Use `--group` or `--platform` with `add` when you need to choose where the
source belongs. Run `cattery status` and `cattery apply` as usual afterward.

To deploy ordinary files while leaving every secret target and secret baseline
untouched, run:

```sh
cattery apply --skip-secrets
```

The flag also prevents secret decryption and SOPS dependency checks. Group
selection still applies normally to the remaining ordinary files and aliases.

`cattery add --secret` is the adoption workflow. The lifecycle commands below
inventory, verify, or rotate sources that are already managed; they do not
create, edit, or delete plaintext secrets.

## Inventory and verification

List every managed encrypted source across base, Linux, and macOS layers:

```sh
cattery secrets list
cattery secrets verify
```

Both commands accept group arguments and repeatable exact source selectors.
Selectors are combined as a union:

```sh
cattery secrets verify work --source _secrets/.config/example/root-token
```

`list` reads repository metadata only and does not invoke SOPS. `verify`
decrypts each selected source in memory, reports only `verified` or `failed`,
continues after independent failures, and never reads the corresponding file
under `$HOME`.

## Rotate recipients or creation rules

SOPS and `.sops.yaml` remain the authority for recipients. A practical
rotation is:

1. Edit the repository's `.sops.yaml` creation rules or recipients.
2. Run `cattery secrets verify` to confirm the existing identities work.
3. Preview with `cattery secrets reencrypt` (or explicitly add `--dry-run`).
4. Apply with `cattery secrets reencrypt --yes`.
5. Review and commit the encrypted repository changes.

Re-encryption decrypts the source in memory, encrypts the same bytes using the
repository-relative source path as SOPS's filename override, then decrypts the
new ciphertext and compares it with the original plaintext before publishing.
Publication preserves the source mode and uses Cattery's atomic replacement
path. Each source is processed independently, so a failure does not prevent
other selected sources from being checked or rotated.

The command never synchronizes or inspects `$HOME`, runs hooks, or writes
plaintext state. A successful replacement refreshes only a matching active
baseline's raw encrypted-source fingerprint; inactive platform sources and
secrets without a baseline do not change state.

## What Cattery protects

- Encrypted source files stay encrypted in the repository.
- Secret plaintext is not printed by `status`, `diff`, or interactive prompts.
- Plaintext is not stored in Cattery’s state database, logs, command arguments,
  errors, or a general temporary directory.
- Secret targets are always mode `0600`, or `0700` when executable.

Cattery treats a secret as bytes, not as YAML, JSON, or an environment file.
Re-encrypting a file without changing its plaintext does not create a content
conflict.

Plaintext necessarily exists briefly in memory while Cattery encrypts,
decrypts, compares, or writes it. Cattery minimizes that exposure, but no Go
program can promise that every memory copy is erased.

## Backup and recovery

Back up both your SOPS identities and Cattery’s local state directory. The
state directory includes a local key used to recognize unchanged secret
plaintext across applies. If that key no longer matches existing secret state,
Cattery stops safely rather than guessing.

If `sops` is missing when an operation needs it, Cattery exits with code `4`
without changing the repository, target, or state.

See [repository layout](repository-layout.md#secrets) for placement and
[reconciliation](reconciliation.md) for apply behavior.
