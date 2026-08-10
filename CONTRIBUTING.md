# Contributing

Thanks for helping improve Cattery.

## Local setup

The Nix development shell provides the supported toolchain:

```sh
nix develop
just check
```

Without Nix, use Go 1.26 or newer and run:

```sh
go test ./...
nix flake check
```

## Making a change

Keep a change focused, explain the user-visible behavior, and include tests for
new behavior or regressions. Safety-sensitive work—filesystem writes, state,
secrets, paths, and hooks—needs failure-path coverage as well as the happy path.

Use unit tests beside the code they cover. Use `integration/` for complete CLI
flows. Format Go with `gofmt`; `just check` is the normal pre-submit gate.

## Pull requests

Describe what changed, why it is safe, and how you verified it. Keep commits
small and imperative. Do not commit plaintext secrets, generated state, or
machine-specific build outputs.
