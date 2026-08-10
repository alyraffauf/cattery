# Release Operations

Cattery releases are deterministic static binaries for Linux amd64 and macOS
arm64. The process is tag-driven and does not depend on workflow wall-clock
time.

## Requirements

The pinned Nix shell supplies Go 1.26.5, GNU tar 1.35, gzip 1.14,
GitHub CLI 2.97.0, SOPS 3.13.3, and age 1.3.1.

Run local packaging checks before a release:

```text
bash scripts/tests/package_release_test.sh
bash scripts/tests/smoke_release_test.sh
```

The fixtures use isolated temporary repositories and clean them up on every
exit path. They never use a user's HOME, state directory, or credentials.

## Tag and Metadata

Create an annotated semantic-version tag at the exact commit to release:

```text
git tag -a vX.Y.Z -m "Cattery vX.Y.Z"
git push origin vX.Y.Z
```

The package script rejects dirty trees, lightweight tags, malformed tags, and
tags that do not name `HEAD`. It derives `SOURCE_DATE_EPOCH` from the tagged
commit when the environment does not provide it. Version metadata contains the
exact tag, full commit SHA, UTC build timestamp, Go version, and target.

## Package Format

`bash scripts/package-release.sh` builds exactly two archives:

```text
cattery_X.Y.Z_linux_amd64.tar.gz
cattery_X.Y.Z_darwin_arm64.tar.gz
```

Each archive contains one top-level directory with exactly `cattery` mode
`0755` and `LICENSE` mode `0644`. Entries use sorted ustar order, zero numeric
owner and group, empty owner and group names, one source date, and `gzip -n`.
`SHA256SUMS` contains lowercase checksums in lexical filename order.

The package script uses `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and the
release linker values required by PLAN.md Section 11.7. Repeating a package
build with the same checkout and `SOURCE_DATE_EPOCH` produces identical bytes:

```text
scripts/package-release.sh --print-reproducibility-status
```

## Smoke Verification

`bash scripts/smoke-release.sh` verifies the checksum manifest, archive member
set, modes, and host-applicable executable. It then runs isolated `init`,
`validate`, ordinary `apply`, ordinary `add`, and a SOPS/age secret add/apply
round trip. The smoke process sets HOME and XDG state paths to temporary trees.

## CI and Publication

`.github/workflows/ci.yml` runs one required Linux check on `ubuntu-22.04`
using the pinned Go setup action. It runs race tests, vet, documentation and
credential checks, and cross-builds the macOS arm64 output. Nix is reserved for
local/release tooling that needs SOPS, age, or pinned archive utilities. Every
external GitHub Action is pinned to a full commit.

`.github/workflows/release.yml` has two boundaries:

- `build-and-smoke` is read-only and rebuilds the exact tag;
- `publish` has `contents: write`, requires the read-only job, runs only for a
  version tag, rebuilds and re-smokes locally, then invokes
  `gh release create --verify-tag`.

A workflow-dispatch dry run validates release tooling and never instantiates the
publishing job. No artifact upload/download or release action is used, and no
pull-request event can reach write permission.

## Recovery

If packaging or smoke fails, do not edit generated archives manually. Fix the
source or recipe, rerun both local tests from a clean tagged checkout, and push
the corrected tag only after the checksum manifest and smoke verification pass.
