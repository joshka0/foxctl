# Start Here: Testing & CI Expectations for agentctl

This document expands on the brief testing notes in `AGENTS.md`. It explains
how tests, coverage, and CI fit together so agents and humans can reason about
"done" correctly.

---

## Testing Philosophy

- **Single source of truth:** Use `make` targets and CI workflows as the
  canonical contract for how tests run.
- **Deterministic:** No network in default `go test ./...` or `make test`.
- **Race & CGO awareness:** Use `-race` and `CGO_ENABLED` carefully around the
  vector / SQLite driver combination.
- **Golden tests:** Prefer stable JSON/NDJSON fixtures in `test/golden/` and
  `testdata/` to lock in envelope shapes.

---

## Local Commands (developer loop)

These are the core commands you should assume exist when proposing or verifying
changes. See `Makefile` for the authoritative definitions.

- `make test` – unit tests (no network), all packages.
- `make test-race` – unit tests with `-race`, excluding the vector storage
  package which requires special handling.
- `make lint` – `golangci-lint` + `staticcheck` + `govet`.
- `make fmt` – `gofumpt` formatting.
- `make check-coverage` – run tests with coverage and enforce **local**
  thresholds:
  - Lines: **≥ 85%**
  - Functions: **≥ 80%**
  - Branches: **≥ 75%** (approximated via line coverage)

> CI enforces a lower coverage floor (currently 40%) but local development
> should aim for the stricter `check-coverage` target.

---

## Race Tests and the Vector Package

The `internal/storage/vector` package links `github.com/mattn/go-sqlite3` for
sqlite-vector support, which conflicts with `github.com/tursodatabase/go-libsql`
when both are present in the same binary under `-race`.

To avoid linker conflicts:

- Default race runs **exclude** `internal/storage/vector`.
- To test vector storage under race:

```bash
CGO_ENABLED=1 go test -race -tags vector ./internal/storage/vector/...
```

Under `-race` or `-tags vector`, libSQL/Turso drivers are compiled out via
build tags and replaced with clear runtime errors instead of linking embedded
SQLite twice.

---

## Must-Have Test Suites

For new features or changes in the following subsystems, expect to add tests of
these kinds:

- **Envelope & protocol:**
  - Valid/invalid envelopes
  - Large-output → CAS wrapper (summary + artifact + `meta.cas_digest`)
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

- `agentctl watch tests` – daemon that watches the workspace and runs
  configured test commands (see `cmd/agentctl/cmd/watch.go` and
  `internal/testwatch/`).
- Status is persisted in SQLite (`~/.agentctl/storage/test_watch.db`) via the
  `testwatch` store.
- The `hooks/test_feedback` skill reads this store and returns a summary of
  failing watchers/tests back to Claude after edits (see
  `skills/hooks_test_feedback/`).

When making code changes that affect tests, prefer to:

- Update or add watcher configurations via `agentctl test-watch add`.
- Let the watcher + feedback hook surface failures instead of hard-coding
  bespoke test commands into docs.

---

## CI Overview

GitHub Actions (`.github/workflows/ci.yml`) runs a containerized CI pipeline
using a pre-warmed Go image built from `Dockerfile.ci`. Key jobs:

- **lint** – `make lint` in the CI image.
- **test** – `CGO_ENABLED=0 go test -short ./...` with coverage, enforcing a
  floor (currently 40%).
- **race/tests/coverage** – additional jobs for race detection and coverage
  reporting, aligned with local `make` targets.
- **LLM planner integration** – gated on env vars and secrets; only runs when
  an OpenRouter API key is configured.

Agents proposing CI changes should:

- Keep CI and local `make` targets in sync.
- Avoid adding new CGO dependencies or networked tests without explicit human
  approval.

---

## Related Documents

- `AGENTS.md` – high-level expectations and guardrails for agents.
- `docs/spec/core_profile_v1.md` – canonical Core Profile spec (envelopes,
  CAS, jobs, memory, OpenAPI skill).
- `docs/impl_plan/universal_swe_grep_and_agents_testing.md` – phase-by-phase
  test plan for the SWE Grep, symbol/semantic index, and agents work.
- `docs/ci/github_checks.md`, `docs/ci/prcomments.md` – CI-related skills and
  workflows.
