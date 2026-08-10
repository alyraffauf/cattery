# Cattery development recipes. Frozen by Task 4; no later card edits this file.

# Primary Go toolchain. The default Nix shell exposes Go 1.26; use the
# go-floor shell to run the same recipes against Go 1.25.
go := env_var_or_default("CATTERY_GO", "go")

# default: show available recipes.
default:
    @just --list

# Fail when any Go file needs formatting.
fmt-check:
    out="$({{go}}fmt -l . 2>&1)"; test -z "$out"

# Run the architecture and dependency-boundary quality gates.
quality-tests:
    {{go}} test ./internal/quality/...

# Run quality gates plus vet and staticcheck.
quality: quality-tests vet staticcheck

# Run go vet across the module.
vet:
    {{go}} vet ./...

# Run staticcheck across the module.
staticcheck:
    staticcheck ./...

# Run every unit, application, and state test.
test:
    {{go}} test ./...

# Run the test suite with the race detector.
test-race:
    {{go}} test -race ./...

# Build every package and command.
build:
    {{go}} build ./...

# Run the black-box integration suite.
test-integration:
    {{go}} test ./integration/...

# Run the mandatory real SOPS/age secret round trip.
test-sops:
    {{go}} test ./integration/ -run '^TestExecutableSecrets$'

# Fail when go.mod/go.sum would change after tidying.
tidy-guard:
    {{go}} mod tidy -diff

# Local dependency tidy guard plus the networked update audit.
deps-check: tidy-guard
    {{go}} list -m -u all

# Validate documented commands, flags, and links.
check-docs path='':
    python3 scripts/check-docs.py {{path}}

# Scan for credential-shaped values in the given paths.
check-credentials path='':
    python3 scripts/check-credentials.py {{path}}

# Full local gate: formatting, quality, vet, tests, integration, build, tidy.
check: fmt-check quality test test-integration build tidy-guard
