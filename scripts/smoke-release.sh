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
    printf 'original\n' > "$repo/.bashrc"
    run_command "$binary" "$home" "$state" init "$repo"
    run_command "$binary" "$home" "$state" validate --repo "$repo"
    run_command "$binary" "$home" "$state" apply --repo "$repo" --non-interactive
    cmp "$repo/.bashrc" "$home/.bashrc" || die "ordinary apply did not materialize the source"
    printf 'adopted\n' > "$home/.bashrc"
    run_command "$binary" "$home" "$state" add --repo "$repo" "$home/.bashrc"
    cmp "$repo/.bashrc" "$home/.bashrc" || die "ordinary add did not adopt target bytes"
}

run_command() {
    local binary=$1
    local home=$2
    local state=$3
    shift 3
    HOME="$home" XDG_STATE_HOME="$state" "$binary" "$@"
}

run_secret_round_trip() {
    local binary=$1
    local home=$2
    local state=$3
    local repo=$4
    mkdir -p "$home" "$state" "$repo"
    command -v age-keygen >/dev/null 2>&1 || die "age-keygen is required for the secret smoke"
    command -v sops >/dev/null 2>&1 || die "sops is required for the secret smoke"
    local key_file="$TEMP_ROOT/age-key.txt"
    age-keygen -o "$key_file" >/dev/null
    local recipient
    recipient=$(awk '/public key:/ { print $4 }' "$key_file")
    printf 'creation_rules:\n  - age: %s\n' "$recipient" > "$repo/.sops.yaml"
    mkdir -p "$home/.config/cattery"
    local secret_path="$home/.config/cattery/secret"
    printf 'release-smoke-secret\n' > "$secret_path"
    SOPS_AGE_KEY_FILE="$key_file" HOME="$home" XDG_STATE_HOME="$state" "$binary" add --repo "$repo" --secret "$secret_path"
    rm -f "$secret_path"
    SOPS_AGE_KEY_FILE="$key_file" HOME="$home" XDG_STATE_HOME="$state" "$binary" apply --repo "$repo" --non-interactive
    [[ $(cat "$secret_path") == 'release-smoke-secret' ]] || die "secret round trip did not restore plaintext"
}

main() {
    verify_manifest
    local os arch archive name binary
    for target in "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do
        read -r os arch <<< "$target"
        archive=$(printf '%s\n' "$DIST"/cattery_*_"$os"_"$arch".tar.gz)
        [[ -f $archive ]] || die "missing archive for $os/$arch"
        verify_archive "$archive"
    done
    read -r os arch <<< "$(host_target)"
    archive=$(printf '%s\n' "$DIST"/cattery_*_"$os"_"$arch".tar.gz)
    name=$(basename "$archive" .tar.gz)
    binary="$TEMP_ROOT/$name/cattery"
    run_basic_commands "$binary"
    run_secret_round_trip "$binary" "$TEMP_ROOT/home-secret" "$TEMP_ROOT/state-secret" "$TEMP_ROOT/repository-secret"
    printf 'smoke passed for %s\n' "$name"
}

main "$@"
