# Environment — TUI Mission

Environment variables, toolchain, and external dependencies.

**What belongs here:** Required env vars, toolchain pins, external service expectations.
**What does NOT belong here:** Service ports/commands (see `.factory/services.yaml`).

---

## Toolchain

- **Go 1.25+** (pinned by `go.mod`). No toolchain upgrades during this mission.
- **CGO** off by default; `make build` and `go test` should work with
  `CGO_ENABLED=0`.
- **golangci-lint** + **gofumpt** available on PATH (used by `make lint` /
  `make check`).
- **tuistory** Factory skill (invoked by workers and validators via the Skill
  tool).

## Framework pin

- `github.com/grindlemire/go-tui v0.11.0` — **pinned**. No upgrades unless M1
  explicitly motivates one. VAL-CROSS-005 asserts this line in `go.mod` is
  unchanged at mission end.

## Environment variables

Only used when spawning a test daemon or a TUI client:

| Variable                | Purpose                                       | Scope                  |
| ----------------------- | --------------------------------------------- | ---------------------- |
| `FOXCTL_STORAGE_ROOT`   | Isolated storage directory for test daemon   | Per-validator (temp)  |
| `FOXCTL_PATHS_CAS`      | CAS directory (optional; derived from root) | Per-validator (temp)  |
| `FOXCTL_API_URL`        | URL the TUI client points at                 | Per-validator         |

All of these are set by the per-test-daemon fixture; workers should not set
them globally.

## External dependencies

None. The mission is local-only: no network APIs, no external credentials, no
third-party services.

## Branch / VCS

- Working branch: `feat/tui-go`.
- Base branch: `main`.
- No pushes to remote during the mission (enforced by orchestrator policy).
- Workers create commits locally; the orchestrator handles any PR work
  separately if needed.
