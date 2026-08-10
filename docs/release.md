# Release operations

Cattery releases are deterministic static binaries for Linux and macOS on
amd64 and arm64. The release process is intentionally tag-driven and does not
depend on workflow wall-clock time.

## Requirements

The pinned Nix development shell supplies the release tools:

- Go 1.26.5 for the primary build;
- Go 1.25.12 for the compatibility floor;
- GNU tar 1.35 and gzip 1.14;
- GitHub CLI 2.97.0;
- SOPS 3.13.3 and age 1.3.1 for the smoke round trip.

Run the local script tests before a release:

```text
bash scripts/tests/package_release_test.sh
bash scripts/tests/smoke_release_test.sh
```

These tests use isolated temporary repositories and remove them on every exit
path. They never use a user's HOME, state directory, or credentials.

## Tag and metadata

Create an annotated semantic version tag at the exact commit to release:

```text
git tag -a vX.Y.Z -m "Cattery vX.Y.Z"
git push origin vX.Y.Z
```

The package script rejects dirty trees, lightweight tags, malformed tags, and
tags that do not name `HEAD`. It derives `SOURCE_DATE_EPOCH` from the tagged
commit when the environment does not provide it. Version metadata contains the
exact tag, full commit SHA, UTC build timestamp, Go version, and target.

## Package format

`bash scripts/package-release.sh` builds exactly four archives:

```text
cattery_X.Y.Z_linux_amd64.tar.gz
cattery_X.Y.Z_linux_arm64.tar.gz
cattery_X.Y.Z_darwin_amd64.tar.gz
cattery_X.Y.Z_darwin_arm64.tar.gz
```

Every archive has one top-level directory and exactly these members:

```text
cattery_X.Y.Z_<goos>_<goarch>/cattery
cattery_X.Y.Z_<goos>_<goarch>/LICENSE
```

The binary is mode `0755`; `LICENSE` is mode `0644`. Archives use sorted ustar
entries, zero numeric owner/group, empty owner names, a common source date, and

The package script uses `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and the
release linker values required by PLAN.md Section 11.7. Repeating a package
build with the same checkout and `SOURCE_DATE_EPOCH` produces identical bytes:

```text
scripts/package-release.sh --print-reproducibility-status
```

## Smoke verification

`bash scripts/smoke-release.sh` verifies the checksum manifest, archive member
set, modes, and host-applicable executable. It then runs isolated `init` and

## CI and publication

`.github/workflows/ci.yml` runs required native jobs on explicit

`.github/workflows/release.yml` has two boundaries:

- `build-and-smoke` is read-only and rebuilds the exact tag;
- `publish` has `contents: write`, requires the read-only job, runs only for a
  matching version tag, rebuilds and re-smokes locally, then invokes
  `gh release create --verify-tag`.

A workflow-dispatch dry run validates the read-only release tooling and never
instantiates the publishing job. No artifact upload/download or release action
is used, and no pull-request event can reach write permission.

## Recovery

If packaging or smoke fails, do not edit generated archives manually. Fix the
source or recipe, rerun both local tests from a clean tagged checkout, and push
the corrected tag only after the checksum manifest and smoke verification pass.
