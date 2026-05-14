# Start Here: Testing & CI Expectations for foxctl

This document expands on the brief testing notes in `AGENTS.md`. It explains how
tests, coverage, and CI fit together so agents and humans can reason about
"done" correctly.

---

## Testing Philosophy

- **Single source of truth:** Use `make` targets and CI workflows as the
  canonical contract for how tests run.
- **Deterministic:** No network in default `go test ./...` or `make test`.
- **Race awareness:** Use `-race` through the Makefile so package sharding and
  timeouts stay consistent.
- **Golden tests:** Prefer stable JSON/NDJSON fixtures in `tests/golden/` and
  `testdata/` to lock in envelope shapes.

---

## Local Commands (developer loop)

These are the core commands you should assume exist when proposing or verifying
changes. See `Makefile` for the authoritative definitions.

- `make test` – unit tests (no network), all packages.
- `make test-short` – unit tests with `-short` flag (fastest feedback loop).
- `make test-race` – unit tests with `-race`.
- `make test-integration` – integration tests in `tests/integration/...` (may
  require network/LLM APIs; gated with `//go:build integration`).
- `make test-integration-cmd` – cmd integration tests in `cmd/foxctl/cmd/...`
  (requires `make skills-build` first; gated with `//go:build integration`).
- `make lint` – `golangci-lint` + `staticcheck` + `govet`.
- `make fmt` – `gofumpt` formatting.
- `make check-coverage` – run tests with coverage and enforce the default
  repository floor:
  - Lines: **≥ 40%**
  - Functions: **≥ 40%** (approximated via line coverage)
  - Branches: **≥ 40%** (approximated via line coverage)
- `make check-coverage-strict` – run the same coverage check with stricter
  aspirational local thresholds:
  - Lines: **≥ 85%**
  - Functions: **≥ 80%** (approximated via line coverage)
  - Branches: **≥ 75%** (approximated via line coverage)

> The default local gate enforces the current repository floor (currently 40%).
> Local development should aim for the stricter `check-coverage-strict` target.

---

## Storage Builds

Turso is the canonical SQLite-family storage path and is used from the default
non-CGO CLI build:

```bash
make build
make test
```

The old libsqlite3/sqlite-vector build lane has been removed. Do not add
`github.com/mattn/go-sqlite3`, `-tags=libsqlite3`, or sqlite-vector extension
loading back to the storage path.

## Race Tests

Run the sharded race suite through Make:

```bash
make test-race
```

`internal/storage/vector` now contains only pure float32 encoding and cosine
helpers. Native vector search belongs in the Turso/Postgres storage backends.

---

## Integration Tests

Integration tests live in two places, both gated with `//go:build integration`:

1. **`tests/integration/`** – Full integration tests that may require network
   access, LLM API keys (e.g., `FOXCTL_LLM_API_KEY`, `GEMINI_API_KEY`), or
   external binaries. These test end-to-end workflows like agent spawning,
   symbol indexing, and the SWE Grep pipeline.

2. **`cmd/foxctl/cmd/`** – Command integration tests that verify CLI behavior
   with real skill binaries. Requires `make skills-build` before running.

Run them via:

```bash
make test-integration       # tests/integration/... (may need API keys)
make test-integration-cmd   # cmd/foxctl/cmd/... (needs skills-build)
```

## Must-Have Test Suites

For new features or changes in the following subsystems, expect to add tests of
these kinds:

- **Envelope & protocol:**
  - Valid/invalid envelopes
  - Large-output → CAS wrapper (summary + artifact + optional `meta.cas_digest`
    matching it)
  - Error envelopes with actionable `error.code` + `data.hint`
- **CAS:**
  - Integrity failures (digest mismatch)
  - Concurrent `Put`/`Get`
  - Tagging and pinning behavior
- **Jobs:**
  - Lifecycle transitions (`queued → running → ok|error|canceled`)
  - Crash recovery and resumption
  - `--dedupe` behavior
- **OpenAPI (`http/openapi`):**
  - Dry-run output (`request_plan`)
  - Auth: bearer, apiKey, basic, OAuth2 client-credentials
  - Pagination: link, cursor, offset
  - Retries and rate limiting (429/5xx → backoff → ERATELIMIT/ERUNTIME)
  - Non-UTF-8 bodies rejected with `EPARSE`
- **Plugins:**
  - Example auth and pagination plugins invoked via subprocess (WASI and exec)

Live integration tests (e.g. real OpenAPI calls, live LLM providers) should be
**opt-in** via env flags and never run in default `go test ./...`.

---

## Test Watcher & Feedback Hooks

The test infrastructure is designed to give fast, local feedback and surface it
back to agents:

- `foxctl watch tests` – daemon that watches the workspace and runs configured
  test commands (see `cmd/foxctl/cmd/watch.go` and `internal/tooling/testwatch/`).
- Status is persisted in SQLite (`~/.foxctl/storage/test_watch.db`) via the
  `testwatch` store.
- The `hooks/test_feedback` skill reads this store and returns a summary of
  failing watchers/tests back to Claude after edits (see
  `skills/hooks_test_feedback/`).

When making code changes that affect tests, prefer to:

- Update or add watcher configurations via `foxctl test-watch add`.
- Let the watcher + feedback hook surface failures instead of hard-coding
  bespoke test commands into docs.

---

## CI Overview

GitLab CI (`.gitlab-ci.yml`) runs a containerized pipeline using the Go image
built from `deploy/docker/Dockerfile.ci`. Key jobs:

- **static-analysis** – `make fmt`, `make lint`, large-file checks, and tech
  debt checks.
- **unit-tests** – impacted `make test-short-impacted` on merge requests, or
  `go test -short ./...` on main/full runs.
- **race-tests-\*** – sharded race checks through `make test-race-shard`.
- **integration-tests** – impacted `make test-integration-impacted` on merge
  requests, or `make test-integration` on main/full runs.
- **build** – `make build`, impacted or full skill builds, and manifest checks.

Agents proposing CI changes should:

- Keep CI and local `make` targets in sync.
- Avoid adding new CGO dependencies or networked tests without explicit human
  approval.

---

## Related Documents

- `AGENTS.md` – high-level expectations and guardrails for agents.
- `docs/spec/core_profile_v1.md` – canonical Core Profile spec (envelopes, CAS,
  jobs, memory, OpenAPI skill).
- `docs/impl_plan/universal_swe_grep_and_agents_testing.md` – phase-by-phase
  test plan for the SWE Grep, symbol/semantic index, and agents work.
- `docs/ci/checks.md`, `docs/ci/prcomments.md` – CI-related skills and
  workflows.
