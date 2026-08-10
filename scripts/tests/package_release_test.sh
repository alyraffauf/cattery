#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
TEMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TEMP_ROOT"' EXIT

fail() {
    printf 'package-release-test: %s\n' "$*" >&2
    exit 1
}

prepare_repository() {
    git archive HEAD | tar -x -C "$TEMP_ROOT"
    mkdir -p "$TEMP_ROOT/scripts"
    cp "$ROOT/scripts/package-release.sh" "$TEMP_ROOT/scripts/package-release.sh"
    chmod 0755 "$TEMP_ROOT/scripts/package-release.sh"
    mkdir -p "$TEMP_ROOT/cmd/cattery"
    cat > "$TEMP_ROOT/cmd/cattery/main.go" <<'EOF'
package main

func main() {}
EOF
    (cd "$TEMP_ROOT" && git init -q && git config user.email test@example.invalid && git config user.name test && git add . && git commit -qm initial && git tag -a -m release v0.0.1)
}

assert_archives() {
    local dist=$1
    [[ $(find "$dist" -maxdepth 1 -name 'cattery_*.tar.gz' | wc -l) -eq 4 ]] || fail "expected four archives"
    [[ $(wc -l < "$dist/SHA256SUMS") -eq 4 ]] || fail "expected four checksums"
    (cd "$dist" && sha256sum -c SHA256SUMS >/dev/null)
}

main() {
    prepare_repository
    (cd "$TEMP_ROOT" && scripts/package-release.sh)
    assert_archives "$TEMP_ROOT/dist"
    first=$(sha256sum "$TEMP_ROOT/dist"/*.tar.gz "$TEMP_ROOT/dist/SHA256SUMS")
    (cd "$TEMP_ROOT" && scripts/package-release.sh)
    second=$(sha256sum "$TEMP_ROOT/dist"/*.tar.gz "$TEMP_ROOT/dist/SHA256SUMS")
    [[ $first == "$second" ]] || fail "repeated package builds differ"
    [[ $(cd "$TEMP_ROOT" && scripts/package-release.sh --print-reproducibility-status) == reproducible ]] || fail "manifest was not reproducible"
    printf 'package release tests passed\n'
}

main "$@"
