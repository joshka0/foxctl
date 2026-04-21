# User Testing — TUI Mission

How the user-testing validator exercises the TUI and how flow validators are spawned. Read this before the user-testing validator's first run.

**What belongs here:** Validation surface per milestone, concurrency limits, isolation strategy, tool requirements, resource costs, how to spawn/seed the test daemon.
**What does NOT belong here:** Generic project docs (see `AGENTS.md`), architecture (see `architecture.md`), env vars (see `environment.md`).

---

## Validation Surfaces

Three distinct testing surfaces across the mission's milestones:

### M1 — Docs (manual-read + make)

M1 is docs-only. Validation is structural (section presence, headings, word
counts, citation counts, link integrity).

- **Primary tool:** subagent with Grep/Read inspecting markdown files.
- **Secondary tool:** `make check-doc-links` (hard gate for VAL-DOCS-008).
- **No daemon or TUI required.**

### M2 — Component Library (go-test + tuistory)

M2 is pure Go components with unit tests and widget-level tuistory snapshots.

- **Primary tool:** `go test -race ./internal/interfaces/tui/...` runs
  MockTerminal-based unit tests for every widget and runtime.
- **Secondary tool:** `tuistory` skill drives tiny demo programs to capture
  snapshots of each widget in each state variant (focused/unfocused,
  empty/loading/error, etc.).
- **No daemon required** for M2; widget demos render standalone.

### M3 — Walking Skeleton (tuistory + per-test daemon)

M3 exercises the full cockpit against a live daemon. This is the highest-cost
validation surface.

- **Primary tool:** `tuistory` skill driving the compiled `cmd/foxctl_tui`
  binary.
- **Daemon setup:** each flow validator gets its **own isolated**
  `foxctl web serve` daemon, on an **OS-chosen port** (use `-p 0`, parse the
  actual port from stderr), rooted at a temp `FOXCTL_STORAGE_ROOT`.
- **Seeding:** the per-test-daemon fixture seeds N agents (typically 3 with
  roles researcher/coder/planner) deterministically before the TUI launches.
- **Teardown:** the fixture registers `t.Cleanup` that stops the daemon and
  removes the temp storage root. Leaks fail the test.

---

## Required Skills

Validators and engineer workers must invoke these skills via the Skill tool:

- **`tuistory`** (REQUIRED for M2 widget snapshots and all M3 flows) — Factory
  skill for terminal UI automation. Drives the TUI binary via PTY, captures
  frames, asserts on content.

**Not required:**
- `agent-browser` — this is a TUI mission, not a web UI mission.
- Any playwright/headless-browser skills.

---

## Validation Concurrency

**Hard cap: 5 concurrent flow validators at any time.**

Derivation (from pre-mission dry run):

| Cost component                 | Per-validator |
| ------------------------------ | ------------- |
| TUI binary (`cmd/foxctl_tui`)  | ~30–50 MiB RSS |
| Tuistory harness (PTY + frame) | ~150 MiB     |
| Isolated daemon                | ~50–100 MiB  |
| **Budget**                     | **~250 MiB** |

Machine headroom at mission start: 16 CPU / 64 GB RAM / ~5 GB free.
5 × 250 MiB = 1.25 GB — well within 70% of free headroom (3.5 GB).

Hard cap of 5 applies even if more parallelism would fit. This keeps
validators deterministic under contention.

---

## Isolation Strategy

Because each flow validator needs its own daemon and storage root, isolation
is **per-validator**, not per-mission:

1. Each validator invocation of the fixture gets a freshly chosen OS port.
2. Each validator uses a unique temp directory for `FOXCTL_STORAGE_ROOT`
   (Go's `t.TempDir()` is idiomatic; the fixture handles this).
3. Each validator seeds only the agents it needs; no shared seed state.
4. Cleanup is mandatory on every exit path (success, fail, skip, fatal).

**Port rule:** Never hardcode a port. Never use port **8090** (default daemon
port; may collide with a user-running instance). Always use `-p 0` and parse
the assigned port.

---

## Test Environment Setup

The user-testing validator must, before spawning flow validators, ensure:

1. The repo builds:
   ```
   make build
   ```
   This produces `bin/foxctl` (the daemon CLI) and `bin/foxctl_tui` (the TUI
   binary). If either is missing, the validator returns to the orchestrator
   rather than trying to patch the build.

2. `go test -race ./internal/interfaces/tui/...` passes.

3. The per-test-daemon fixture is importable at the path declared by the
   M3 `skel-fixture` feature (typically
   `internal/interfaces/tui/.../testfixture`). This is the canonical
   helper. Flow validators spawn daemons only via this fixture.

4. `tuistory` skill is available.

---

## Known Gotchas

- **TUI binary is large-ish (~9 MiB) but safe to build repeatedly.** Builds
  are incremental via `go build`'s cache.
- **Goroutine leaks** cause flaky flows. Every M2/M3 test that spawns a
  runtime must check leaks (via `goleak.VerifyNone` or
  `runtime.NumGoroutine()` delta).
- **SIGWINCH** is explicitly in scope (VAL-SKEL-015). Tuistory's resize
  API drives the mid-stream resize test.
- **Malformed SSE** is explicitly in scope (VAL-SKEL-016). Use an
  `httptest.Server` producing invalid frames rather than the real daemon.
- **`-smoke-agent` / `-smoke-console`** modes on `cmd/foxctl_tui` exit
  cleanly even without a reachable API; they are smoke-fixture entry points
  and must remain schema-compatible (VAL-CROSS-002).

---

## Flow Validator Guidance: M3 Skeleton (tuistory + per-test daemon + code-inspection + git-inspect)

**Surface:** Full TUI cockpit (`cmd/foxctl_tui`) driven by tuistory against a live per-test daemon.

### Fixture API

The per-test-daemon fixture lives at `internal/interfaces/tui/testfixture/testfixture.go` and is the **only** way flow validators should start a daemon:

```go
import "github.com/joshkatz/foxctl/v2/internal/interfaces/tui/testfixture"

func TestFoo(t *testing.T) {
    fx := testfixture.BootDaemon(t, testfixture.SeedOpts{
        Roles:       []string{"researcher", "coder", "planner"},
        WorkspaceID: "test-workspace",
    })
    defer fx.Close()

    // fx.Port()      — the OS-assigned port (never hardcode)
    // fx.BaseURL()  — "http://localhost:" + port
    // fx.AgentIDs() — []string of seeded agent IDs
    // fx.APIClient() — *http.Client pointed at the daemon
    // fx.StorageRoot() — temp storage dir path
}
```

**Key behaviors:**
- Port is **always** OS-chosen (parse from `fx.Port()`). Never hardcode 8090.
- Temp `FOXCTL_STORAGE_ROOT` is created via `t.TempDir()` and removed on `t.Cleanup`.
- On `t.Fatal`, teardown still runs via `sync.Once`.
- Fixture seeds agents deterministically by role. IDs are returned in `fx.AgentIDs()`.
- The TUI binary path: `bin/foxctl_tui` (built by `make build`).

### Tools Used

| Tool | Purpose |
|------|---------|
| `tuistory` skill | Drives `bin/foxctl_tui` via PTY; captures frames; asserts on content |
| `go-test` (`go test -race -count=1 ./internal/interfaces/tui/...`) | Runs existing M3 integration tests (VAL-SKEL-013, VAL-SKEL-016) |
| `code-inspection` (grep, Read) | Checks source code for patterns, no regressions |
| `git-inspect` | Verifies `git status --porcelain` clean and `go.mod` unchanged |
| `cli` | Runs `foxctl agent spawn` externally for VAL-SKEL-009 |

### Isolation Rules

Each flow validator operates **fully independently**:

1. **One isolated daemon per validator.** No shared daemon, no shared port, no shared storage root.
2. **Temp storage root per validator.** The fixture handles this. Validators MUST use `testfixture.BootDaemon`, not `foxctl web serve` directly.
3. **No port collision.** Use `fx.Port()` from the fixture. Never hardcode any port — especially not 8090 (default daemon port).
4. **Mandatory teardown.** On every exit path (success, fail, skip, fatal), the daemon must be stopped and temp storage removed. The fixture's `t.Cleanup` handles this. Validators MUST NOT leave a daemon process running.
5. **No shared seed state.** Each validator seeds only the agents it needs. No inter-validator coordination on agent state.

### Concurrency

**Hard cap: 5 concurrent flow validators.** This is the validated budget ceiling from the pre-mission dry run. Even if fewer would suffice, do not exceed 5.

### Groups

Organize the 25 pending assertions into groups for parallel spawning:

- **Group A** (tuistory + fixture, 3 agents): VAL-SKEL-001, VAL-SKEL-002, VAL-SKEL-004, VAL-SKEL-010, VAL-SKEL-011, VAL-SKEL-012 — entry/boot/inventory/empty/error/footer
- **Group B** (tuistory + fixture, 3 agents): VAL-SKEL-003, VAL-SKEL-005, VAL-SKEL-018 — resize/selection/min-size
- **Group C** (tuistory + fixture, 3 agents): VAL-SKEL-006, VAL-SKEL-007, VAL-SKEL-017 — ask/chat streaming, cancel, double-submit
- **Group D** (tuistory + fixture + external CLI, 3 agents): VAL-SKEL-008, VAL-SKEL-009 — evidence drawer, live refresh
- **Group E** (tuistory + fixture + httptest, 3 agents): VAL-SKEL-015, VAL-SKEL-016 — SIGWINCH mid-stream, malformed SSE
- **Group F** (code-inspection + go-test + git-inspect, no daemon): VAL-SKEL-013, VAL-SKEL-014, VAL-CROSS-001, VAL-CROSS-002, VAL-CROSS-004, VAL-CROSS-005, VAL-CROSS-007, VAL-CROSS-008

Spawn Groups A–E (tuistory) first. Spawn Group F (no daemon) synchronously in the main session since it requires no isolation.

### Known Gotchas

- **TUI binary is large (~9 MiB) but builds are incremental.** `make build` from the main session has already produced `bin/foxctl_tui` — do not rebuild unless the binary is missing.
- **SIGWINCH mid-stream (VAL-SKEL-015).** Tuistory's resize API drives the mid-stream resize test. Verify cancel still works immediately after resize.
- **Malformed SSE (VAL-SKEL-016).** Use `httptest.Server` producing invalid SSE frames, not the real daemon.
- **`-smoke-agent` / `-smoke-console` modes (VAL-CROSS-002).** These smoke modes exit cleanly without a reachable API. Must remain schema-compatible.
- **`make check` coverage timeout.** `make check` times out at ~300s due to the coverage step being killed by the OS, not actual test failures. Individual `go test -race -count=1 ./internal/interfaces/tui/...` passes. VAL-CROSS-007 uses `make test` as proxy evidence.

### Shared Resources (read-only)

- Repo tree under `internal/interfaces/tui/`
- Doc tree under `docs/plans/tui-redesign/`
- `bin/foxctl_tui` binary (pre-built)
- `go.mod` (read-only; check line count unchanged for VAL-CROSS-005)

### Off-Limits

- Do not modify any source files.
- Do not start daemon processes outside the test fixture.
- Do not hardcode port 8090.
- Do not touch `archive/`, `packages/gui-agent/`, or `internal/interfaces/web/`.
- Do not run `make check` (times out). Use `make test` instead.

---

## Flow Validator Guidance: M2 Components (go-test + tuistory + code-inspection + manual-read)

**Surface:** Pure Go component library under `internal/interfaces/tui/`. No daemon, no network.

**Tools used:**
- `go test -race -count=1 ./internal/interfaces/tui/...` — MockTerminal unit tests
- `tuistory` skill — widget demo snapshots (standalone binaries, no daemon)
- `code-inspection` (grep, Read) — structural checks on source code
- `manual-read` — README and doc checks

**Isolation rules:**
- Validators are fully independent. No shared mutable state.
- `go test` runs may be parallelized freely (Go's test framework handles this).
- Tuistory widget demos each run in their own PTY — no conflicts.
- Code inspection is read-only — no conflicts.

**Concurrency:** Up to 5 validators concurrently (well within budget since there is no daemon).

**Shared resources (read-only):**
- Source tree under `internal/interfaces/tui/`
- Doc tree under `docs/plans/tui-redesign/`

**Off-limits:**
- Do not modify any source files.
- Do not start any daemon processes.
- Do not touch `archive/`, `packages/gui-agent/`, or `internal/interfaces/web/`.

---

## Resource Updates (runtime findings)

> The user-testing validator appends runtime findings here during execution
> (isolation approach used, new constraints from this milestone's
> implementation, gotchas discovered).
