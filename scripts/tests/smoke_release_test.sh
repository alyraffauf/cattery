#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
TEMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TEMP_ROOT"' EXIT

make_fake_binary() {
    local path=$1
    cat > "$path" <<'EOF'
#!/usr/bin/env sh
exit 0
EOF
    chmod 0755 "$path"
}

make_archive() {
    local os=$1
    local arch=$2
    local name="cattery_0.0.1_${os}_${arch}"
    local tree="$TEMP_ROOT/tree/$name"
    mkdir -p "$tree"
    make_fake_binary "$tree/cattery"
    printf 'MIT\n' > "$tree/LICENSE"
    chmod 0644 "$tree/LICENSE"
    tar --format=ustar --sort=name --numeric-owner --owner=0 --group=0 --mtime='@0' -czf "$TEMP_ROOT/$name.tar.gz" -C "$TEMP_ROOT/tree" "$name"
}

main() {
    mkdir -p "$TEMP_ROOT/tree"
    for target in "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do
        read -r os arch <<< "$target"
        make_archive "$os" "$arch"
    done
    (cd "$TEMP_ROOT" && sha256sum cattery_*.tar.gz | LC_ALL=C sort -k2 > SHA256SUMS)
    "$ROOT/scripts/smoke-release.sh" "$TEMP_ROOT" >/dev/null
    printf 'smoke release tests passed\n'
}

main "$@"
