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
