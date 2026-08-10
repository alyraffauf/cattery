# Cattery development recipes. Frozen by Task 4; no later card edits this file.

# default: show available recipes.
default:
    @just --list

# Fail when any Go file needs formatting.
fmt-check:
    out="$(gofmt -l . 2>&1)"; test -z "$out"

# Run every unit, application, state, and executable test.
test:
    go test ./...

# Build the Linux amd64 output.
build-linux:
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...

# Build the Darwin arm64 output.
build-darwin:
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...

# Minimal local gate.
check: fmt-check test build-linux
