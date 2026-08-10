#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
DIST=${CATTERY_DIST_DIR:-"$ROOT/dist"}
TARGETS=("linux amd64" "darwin arm64")

die() {
    printf 'package-release: %s\n' "$*" >&2
    exit 1
}

usage() {
    cat <<'EOF'
Usage: scripts/package-release.sh [--print-reproducibility-status]

Build deterministic static release archives from the exact annotated version
tag at HEAD. Set CATTERY_DIST_DIR to choose the output directory.
EOF
}

require_commands() {
    local command_name
    for command_name in git go tar gzip sha256sum; do
        command -v "$command_name" >/dev/null 2>&1 || die "missing required command: $command_name"
    done
}

require_clean_tree() {
    [[ -z $(git status --porcelain) ]] || die "checkout is dirty"
}

release_tag() {
    local tag
    tag=$(git describe --exact-match --tags --match 'v[0-9]*' HEAD 2>/dev/null) || die "HEAD must be an exact vX.Y.Z tag"
    [[ $(git cat-file -t "$tag") == tag ]] || die "release tag must be annotated: $tag"
    [[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "tag is not semver: $tag"
    printf '%s' "$tag"
}

build_timestamp() {
    local tag=$1
    local epoch=${SOURCE_DATE_EPOCH:-}
    if [[ -z $epoch ]]; then
        epoch=$(git log -1 --format=%ct "$tag^{commit}")
    fi
    [[ $epoch =~ ^[0-9]+$ ]] || die "SOURCE_DATE_EPOCH must be an integer"
    printf '%s' "$epoch"
}

release_time() {
    local epoch=$1
    date -u -d "@$epoch" '+%Y-%m-%dT%H:%M:%SZ'
}

prepare_archive_tree() {
    local tree=$1
    local binary=$2
    local archive_name=$3
    mkdir -p "$tree/$archive_name"
    install -m 0755 "$binary" "$tree/$archive_name/cattery"
    install -m 0644 "$ROOT/LICENSE" "$tree/$archive_name/LICENSE"
}

build_target() {
    local goos=$1
    local goarch=$2
    local tag=$3
    local commit=$4
    local timestamp=$5
    local epoch=$6
    local archive_name="cattery_${tag#v}_${goos}_${goarch}"
    local binary="$DIST/build/$goos-$goarch/cattery"
    local tree="$DIST/tree"
    local ldflags
    ldflags="-s -w -buildid= -X github.com/alyraffauf/cattery/internal/buildinfo.Version=$tag -X github.com/alyraffauf/cattery/internal/buildinfo.Commit=$commit -X github.com/alyraffauf/cattery/internal/buildinfo.BuildTimestamp=$timestamp"

    mkdir -p "$(dirname "$binary")"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$binary" ./cmd/cattery
    prepare_archive_tree "$tree" "$binary" "$archive_name"
    create_archive "$archive_name" "$epoch"
}

create_archive() {
    local archive_name=$1
    local epoch=$2
    local tar_file="$DIST/$archive_name.tar"
    local archive="$DIST/$archive_name.tar.gz"
    tar --format=ustar --sort=name --numeric-owner --owner=0 --group=0 --mtime="@$epoch" -cf "$tar_file" -C "$DIST/tree" "$archive_name"
    gzip -n -c "$tar_file" > "$archive"
    rm -f "$tar_file"
}

write_checksums() {
    (cd "$DIST" && sha256sum cattery_*.tar.gz | LC_ALL=C sort -k2) > "$DIST/SHA256SUMS"
}

package_release() {
    local tag=$1
    local epoch=$2
    local commit timestamp
    commit=$(git rev-parse HEAD)
    timestamp=$(release_time "$epoch")
    rm -rf "$DIST/build" "$DIST/tree"
    rm -f "$DIST"/cattery_*.tar.gz "$DIST/SHA256SUMS"
    mkdir -p "$DIST"
    local target
    for target in "${TARGETS[@]}"; do
        read -r goos goarch <<< "$target"
        build_target "$goos" "$goarch" "$tag" "$commit" "$timestamp" "$epoch"
    done
    write_checksums
}

print_status() {
    [[ -n ${SOURCE_DATE_EPOCH:-} ]] || SOURCE_DATE_EPOCH=$(git show -s --format=%ct HEAD)
    [[ $SOURCE_DATE_EPOCH =~ ^[0-9]+$ ]] || die "cannot derive SOURCE_DATE_EPOCH"
    [[ -f "$DIST/SHA256SUMS" ]] || die "no package manifest at $DIST/SHA256SUMS"
    (cd "$DIST" && sha256sum -c SHA256SUMS >/dev/null)
    printf 'reproducible\n'
}

main() {
    if [[ ${1:-} == --help ]]; then
        usage
        return 0
    fi
    if [[ ${1:-} == --print-reproducibility-status ]]; then
        print_status
        return 0
    fi
    [[ $# -eq 0 ]] || die "unknown argument: $1"
    require_commands
    require_clean_tree
    [[ -f "$ROOT/LICENSE" ]] || die "missing LICENSE"
    local tag epoch
    tag=$(release_tag)
    epoch=$(build_timestamp "$tag")
    export SOURCE_DATE_EPOCH=$epoch
    package_release "$tag" "$epoch"
}

main "$@"
