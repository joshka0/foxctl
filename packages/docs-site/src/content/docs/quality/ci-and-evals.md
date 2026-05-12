---
title: CI and evals
description: Local checks, CI pipeline, coverage enforcement, integration tests, test watchers, and review gates.
---

Quality documentation tells contributors which checks protect which behavior. foxctl separates production CI expectations from local developer feedback loops and experimental evaluation harnesses.

## Local checks

### Core commands

These `make` targets are the canonical contract for local quality checks:

| Command | What it does |
|---|---|
| `make test` | Unit tests (no network), all packages |
| `make test-short` | Unit tests with `-short` flag (fastest feedback loop) |
| `make test-race` | Unit tests with `-race` detection |
| `make lint` | `golangci-lint` + `staticcheck` + `govet` |
| `make fmt` | `gofumpt` formatting |
| `make check` | Runs `fmt`, `lint`, `vet`, and `test` together |

### Coverage enforcement

| Command | Lines | Functions | Branches |
|---|---|---|---|
| `make check-coverage` | ≥ 40% | ≥ 40% | ≥ 40% |
| `make check-coverage-strict` | ≥ 85% | ≥ 80% | ≥ 75% |

The default gate enforces the current repository floor (40%). Local development should aim for the stricter `check-coverage-strict` target.

### Documentation checks

```bash
# Check markdown links in repo docs
make check-doc-links

# Check for whitespace errors
git diff --check
```

## Docs-site checks

```bash
# Build the Starlight site
bun run --cwd packages/docs-site build

# Run Astro checks
bun run --cwd packages/docs-site check
```

## CI pipeline

GitHub Actions (`.github/workflows/ci.yml`) runs a containerized CI pipeline using a pre-warmed Go image built from `Dockerfile.ci`. Key jobs:

| Job | What it runs |
|---|---|
| **lint** | `make lint` in the CI image |
| **test** | `CGO_ENABLED=0 go test -short ./...` with 40% coverage floor |
| **race/tests/coverage** | Race detection and coverage reporting |

### CI guidelines

- Keep CI and local `make` targets in sync.
- Do not add new CGO dependencies or networked tests without explicit human approval.
- The default test suite is deterministic: no network access in `go test ./...`.

## Integration tests

Integration tests live in two locations, both gated with `//go:build integration`:

### `test/integration/`

Full integration tests that may require network access, LLM API keys (`FOXCTL_LLM_API_KEY`, `GEMINI_API_KEY`), or external binaries. Tests end-to-end workflows like agent spawning, symbol indexing, and the SWE Grep pipeline.

```bash
make test-integration
```

### `cmd/foxctl/cmd/`

Command integration tests that verify CLI behavior with real skill binaries. Requires `make skills-build` before running.

```bash
make skills-build
make test-integration-cmd
```

## Must-have test suites

For new features or changes in these subsystems, expect tests of these kinds:

### Envelope and protocol

- Valid and invalid envelopes
- Large-output → CAS wrapper (summary + artifact + optional `meta.cas_digest`)
- Error envelopes with actionable `error.code` + `data.hint`

### CAS

- Integrity failures (digest mismatch)
- Concurrent `Put`/`Get`
- Tagging and pinning behavior

### Jobs

- Lifecycle transitions (`queued → running → ok|error|canceled`)
- Crash recovery and resumption
- `--dedupe` behavior

### OpenAPI (`http/openapi`)

- Dry-run output (`request_plan`)
- Auth: bearer, apiKey, basic, OAuth2 client-credentials
- Pagination: link, cursor, offset
- Retries and rate limiting (429/5xx → backoff → `ERATELIMIT`/`ERUNTIME`)
- Non-UTF-8 bodies rejected with `EPARSE`

### Plugins

- Example auth and pagination plugins invoked via subprocess (WASI and exec)

### Golden tests

Golden fixtures in `testdata/*.json` and `test/golden/` must be reproducible:

- Sort keys and arrays in output
- Inject timestamps via clock interface (no `time.Now()` in core logic)
- Use stable IDs or inject UUID generator
- Prefer testing the functional core with table-driven tests (no IO)

## Test watcher and feedback hooks

The test infrastructure provides fast local feedback and surfaces results to agents:

### Test watcher

```bash
foxctl watch tests
```

A daemon that watches the workspace and runs configured test commands. Status persists in SQLite (`~/.foxctl/storage/test_watch.db`) via the `testwatch` store.

### Test feedback hook

The `hooks/test_feedback` skill reads the test watch store and returns a summary of failing watchers/tests back to Claude after edits.

### Recommended workflow

When making code changes that affect tests:

1. Add or update watcher configurations: `foxctl test-watch add`
2. Let the watcher and feedback hook surface failures automatically
3. Prefer this over hard-coding bespoke test commands into docs

## Storage builds

Turso is the canonical SQLite-family storage path:

```bash
make build
make test
```

The old libsqlite3/sqlite-vector build lane has been removed. Do not add `github.com/mattn/go-sqlite3`, `-tags=libsqlite3`, or sqlite-vector extension loading back to the storage path.

## Determinism rules

| Rule | Why it matters |
|---|---|
| No `time.Now()` in core logic | Breaks determinism; use injected clock interface |
| No `rand` or UUID generation in core | Use injected UUID generator |
| No `map[string]any` deep in domain logic | Stringly-typed bugs; parse at boundary |
| Sort before emit | Prevents flaky golden tests |
| Core packages must not import `os`, `database/sql`, adapters | Architecture violation; invert dependency |

## Review gates

Before merging, every PR must pass:

1. `make check` — fmt, lint, vet, test
2. `make check-doc-links` — when markdown changed
3. Coverage floor: `make check-coverage`
4. CI pipeline (lint, test, race)
5. At least one human approval

