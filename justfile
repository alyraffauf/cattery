# Cattery development recipes. Frozen by Task 4; no later card edits this file.

# default: show available recipes.
default:
    @just --list

# Fail when any Go file needs formatting.
fmt-check:
    out="$(gofmt -l . 2>&1)"; test -z "$out"

# Run go vet across the module.
vet:
    go vet ./...

# Run every unit, application, and state test.
test:
    go test ./...

# Run the test suite with the race detector.
test-race:
    go test -race ./...

# Build every package and command.
build:
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...

# Run the mandatory real SOPS/age secret round trip.
test-sops:
    go test ./integration/ -run '^TestExecutableSecrets$'

# Fail when go.mod/go.sum would change after tidying.
tidy-guard:
    go mod tidy -diff

# Validate documented commands, flags, and links.
check-docs path='':
    python3 scripts/check-docs.py {{path}}

# Scan for credential-shaped values in the given paths.
check-credentials path='':
    python3 scripts/check-credentials.py {{path}}

# Full local gate for the supported Linux build environment.
check: fmt-check vet test-race test-sops build check-docs check-credentials tidy-guard
