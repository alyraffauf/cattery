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
