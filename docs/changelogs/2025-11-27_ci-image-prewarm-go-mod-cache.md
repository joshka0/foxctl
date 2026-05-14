# CI Image: Pre-warm Go module cache

Date: 2025-11-27

- Updated `deploy/docker/Dockerfile.ci` to:
  - Set `WORKDIR /workspace`.
  - Copy `go.mod` and `go.sum` into the image.
  - Run `go mod download` to pre-populate the Go module cache.
  - Install `gofumpt` and `golangci-lint` after modules are downloaded.
- Updated `.github/workflows/ci-image.yml` to rebuild the CI image whenever
  `go.mod` or `go.sum` change, keeping the pre-warmed cache in sync with
  dependencies.
- Updated `.github/workflows/ci.yml` to remove `actions/cache` usage for
  container-based jobs (`lint`, `unit-tests`, `race-tests`, `coverage`,
  `build`), relying instead on the pre-warmed CI image.

Expected impact:

- Larger CI image, but faster and more stable container-based CI jobs (no
  repeated upload/download of large Go caches per job).
- First build after Dockerfile or module changes may be slower while the new
  image is built and pushed; subsequent PRs should see reduced per-job startup
  time.
