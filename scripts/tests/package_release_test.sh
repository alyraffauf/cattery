#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
TEMP_ROOT=$(mktemp -d)
EXTRACT_ROOT=$(mktemp -d)
trap 'rm -rf "$TEMP_ROOT" "$EXTRACT_ROOT"' EXIT

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
    local archive name expected_members actual_members epoch header
    [[ $(find "$dist" -maxdepth 1 -name 'cattery_*.tar.gz' | wc -l) -eq 4 ]] || fail "expected four archives"
    [[ $(wc -l < "$dist/SHA256SUMS") -eq 4 ]] || fail "expected four checksums"
    (cd "$dist" && sha256sum -c SHA256SUMS >/dev/null)
    epoch=$(git -C "$TEMP_ROOT" show -s --format=%ct HEAD)
    for archive in "$dist"/cattery_*.tar.gz; do
        name=$(basename "$archive" .tar.gz)
        expected_members=$(printf '%s/\n%s/LICENSE\n%s/cattery\n' "$name" "$name" "$name")
        actual_members=$(tar -tzf "$archive")
        [[ $actual_members == "$expected_members" ]] || fail "unexpected archive member order in $archive"
        tar -xOf "$archive" "$name/cattery" >/dev/null || fail "binary cannot be extracted"
        [[ $(tar -tvzf "$archive" | grep -E " $name/cattery$") =~ 'rwxr-xr-x' ]] || fail "binary mode is not 0755"
        [[ $(tar -tvzf "$archive" | grep -E " $name/LICENSE$") =~ 'rw-r--r--' ]] || fail "LICENSE mode is not 0644"
        tar -tvzf "$archive" | grep -q -E " 0/0 .* $name/cattery$" || fail "binary owner/group is not zero"
        [[ $(gzip -lv "$archive" | wc -l) -eq 2 ]] || fail "gzip metadata is malformed"
        header=$(od -An -t u1 -j 4 -N 4 "$archive" | tr -d ' \n')
        [[ $header == 0000 ]] || fail "gzip timestamp is not zeroed"
        tar -xzf "$archive" -C "$EXTRACT_ROOT"
        [[ $(stat -c '%Y' "$EXTRACT_ROOT/$name/cattery") == "$epoch" ]] || fail "archive mtime is not SOURCE_DATE_EPOCH"
    done
}

assert_rejects_invalid_checkout() {
    printf 'dirty\n' > "$TEMP_ROOT/dirty.txt"
    if (cd "$TEMP_ROOT" && scripts/package-release.sh >/dev/null 2>&1); then
        fail "dirty checkout was accepted"
    fi
    rm -f "$TEMP_ROOT/dirty.txt"
    git -C "$TEMP_ROOT" commit --allow-empty -qm second
    if (cd "$TEMP_ROOT" && scripts/package-release.sh >/dev/null 2>&1); then
        fail "non-HEAD tag was accepted"
    fi
    git -C "$TEMP_ROOT" tag -d v0.0.1 >/dev/null
    git -C "$TEMP_ROOT" tag v0.0.2
    if (cd "$TEMP_ROOT" && scripts/package-release.sh >/dev/null 2>&1); then
        fail "lightweight tag was accepted"
    fi
    git -C "$TEMP_ROOT" tag -d v0.0.2 >/dev/null
    git -C "$TEMP_ROOT" tag -a -m malformed vbad
    if (cd "$TEMP_ROOT" && scripts/package-release.sh >/dev/null 2>&1); then
        fail "malformed tag was accepted"
    fi
    git -C "$TEMP_ROOT" tag -d vbad >/dev/null
    git -C "$TEMP_ROOT" tag -a -m release v0.0.1
}

main() {
    prepare_repository
    assert_rejects_invalid_checkout
    grep -q -- '-buildvcs=false' scripts/package-release.sh || fail "buildvcs=false is missing"
    grep -q -- '-trimpath' scripts/package-release.sh || fail "trimpath is missing"
    grep -q -- 'CGO_ENABLED=0' scripts/package-release.sh || fail "CGO_ENABLED=0 is missing"
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
