# Repository Guidelines

## Project Structure & Module Organization

`cmd/cattery/` contains the CLI entry point. Most production code lives under `internal/`: `cli` handles commands and rendering, `application` coordinates use cases, and packages such as `filesystem`, `reconcile`, `repository`, `secrets`, and `state` own domain-specific behavior. Keep test helpers in `internal/testfixture/`. End-to-end command flows belong in `integration/`, user-facing design notes in `docs/`, and Nix packaging in `flake/` and `nix/`.

## Build, Test, and Development Commands

- `nix develop` enters the supported shell with Go 1.26, Just, and formatters.
- `just check` runs the normal local gate: formatting, all tests, and a Linux build.
- `just test` or `go test ./...` runs the complete Go test suite.
- `just fmt-check` reports Go files that need `gofmt`.
- `go build ./cmd/cattery` builds the local CLI; `nix build` builds the packaged binary.
- `nix flake check` runs the Nix and treefmt checks used by CI.

## Coding Style & Naming Conventions

Use standard Go formatting and tabs as produced by `gofmt`. Keep package names short, lowercase, and descriptive; exported identifiers use `PascalCase`, internal identifiers use `camelCase`, and tests follow `TestName`. Prefer small, single-purpose functions, explicit error handling, and package boundaries already established under `internal/`. Format Nix with Alejandra; treefmt also runs deadnix and statix checks.

## Testing Guidelines

Place unit tests beside the implementation as `*_test.go`. Integration tests prove user-visible command flows; filesystem tests own write and precondition safety; reconciliation tests own snapshot and classification behavior. Add regression coverage for bug fixes and test failure paths when changing safety-sensitive file, state, secret, or process behavior. Run `just check` before submitting.

## Commit & Pull Request Guidelines

Recent history favors concise, imperative subjects, commonly using prefixes such as `fix:`, `test:`, `refactor:`, and `chore:`. Keep each commit focused and explain the observable reason for the change. Pull requests should summarize behavior and safety implications, list verification performed, and link relevant issues. Include terminal output or screenshots only when CLI rendering changes.

## Security & Safety

Never commit plaintext secrets or generated state databases. Preserve the documented guarantees around atomic writes, path validation, non-destructive reconciliation, and in-memory secret handling; consult `docs/secrets.md` and `docs/reconciliation.md` before changing those paths.
