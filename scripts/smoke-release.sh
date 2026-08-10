#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
DIST=${1:-"$ROOT/dist"}
TEMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TEMP_ROOT"' EXIT

die() {
    printf 'smoke-release: %s\n' "$*" >&2
    exit 1
}

verify_manifest() {
    [[ -f "$DIST/SHA256SUMS" ]] || die "missing SHA256SUMS"
    (cd "$DIST" && sha256sum -c SHA256SUMS)
    [[ $(wc -l < "$DIST/SHA256SUMS") -eq 4 ]] || die "manifest must contain four archives"
}

archive_name() {
    local os=$1
    local arch=$2
    printf '%s/cattery_*_%s_%s.tar.gz' "$DIST" "$os" "$arch"
}

verify_archive() {
    local archive=$1
    local name
    name=$(basename "$archive" .tar.gz)
    [[ $(tar -tzf "$archive" | wc -l) -eq 3 ]] || die "unexpected member count: $archive"
    tar -tzf "$archive" | while read -r member; do
        case "$member" in
            "$name/"|"$name/LICENSE"|"$name/cattery") ;;
            *) die "unexpected archive member $member in $archive";;
        esac
    done
    tar -xzf "$archive" -C "$TEMP_ROOT"
    [[ $(stat -c '%a' "$TEMP_ROOT/$name/cattery" 2>/dev/null || stat -f '%Lp' "$TEMP_ROOT/$name/cattery") == 755 ]] || die "binary mode is not 0755"
    [[ $(stat -c '%a' "$TEMP_ROOT/$name/LICENSE" 2>/dev/null || stat -f '%Lp' "$TEMP_ROOT/$name/LICENSE") == 644 ]] || die "LICENSE mode is not 0644"
}

host_target() {
    local os arch
    case "$(uname -s)" in
        Linux) os=linux;;
        Darwin) os=darwin;;
        *) die "unsupported smoke host: $(uname -s)";;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64;;
        arm64|aarch64) arch=arm64;;
        *) die "unsupported smoke architecture: $(uname -m)";;
    esac
    printf '%s %s' "$os" "$arch"
}

run_basic_commands() {
    local binary=$1
    local repo="$TEMP_ROOT/repository"
    local home="$TEMP_ROOT/home"
    local state="$TEMP_ROOT/state"
    mkdir -p "$repo" "$home" "$state"
    XDG_STATE_HOME="$state" "$binary" init "$repo"
    XDG_STATE_HOME="$state" "$binary" validate --repo "$repo"
}

main() {
    verify_manifest
    local os arch archive name binary
    for archive in "$DIST"/cattery_*.tar.gz; do
        [[ -f $archive ]] || die "no release archives found"
        verify_archive "$archive"
    done
    read -r os arch <<< "$(host_target)"
    archive=$(printf '%s\n' "$DIST"/cattery_*_"$os"_"$arch".tar.gz)
    name=$(basename "$archive" .tar.gz)
    binary="$TEMP_ROOT/$name/cattery"
    run_basic_commands "$binary"
    printf 'smoke passed for %s\n' "$name"
}

main "$@"
