---
vault_refs:
  - foxprox/docs/ATCP-v0.1.md
---

# AGENTS.md — foxprox AI Assistant Guide

**Target Audience:** AI coding assistants (Claude, Cursor, Copilot) and human
contributors working on the foxprox module.

---

## Quick Links

| Resource | Purpose |
|---|---|
| [README.md](README.md) | Overview, quick start, architecture diagram |
| [docs/ATCP-v0.1.md](docs/ATCP-v0.1.md) | Canonical protocol specification (860 lines) |
| [docs/TODO.md](docs/TODO.md) | Known gaps and planned work |
| [docs/LIVE-SMOKE.md](docs/LIVE-SMOKE.md) | Live smoke test procedure and findings |

---

## TL;DR

1. **foxprox is a Go module** (`github.com/joshka/foxprox`) — it has its own `go.mod` and builds independently
2. **Public packages live at** `foxprox/foxprox/...` (not internal) so external consumers can import them
3. **Envelope version is** `foxprox/0.1` — do not change without updating the spec
4. **Three deps only:** `oklog/ulid`, `creack/pty`, `modernc.org/sqlite`
5. **No CGO required** — modernc.org/sqlite provides pure-Go SQLite
6. **No external service deps** — no LLM providers, no HTTP upstreams, no network calls
7. **Test before committing:** `go test ./...` (all 20 packages must pass)

---

## Module Overview

foxprox is a local-first coordination layer for interactive CLI agents. It solves
a specific problem: AI agent CLIs are PTY processes, not HTTP APIs. You cannot
reliably drive them with `text + \n`.

The architecture is three layers:

```
transport (HTTP/JSON or Unix socket)
    └── broker (session manager, router, rooms, leases, storage)
            └── adapter (compiles intents into PTY bytes)
```

The **transport** receives JSON requests and calls broker methods. The **broker**
owns PTY sessions, coordinates room fan-out, and manages input leases. The
**adapter** translates typed intents into the byte sequence the target process
expects.

---

## Package Topology

```
foxprox/
  adapter/generictty/   terminal byte compilation (keys, paste, submit)
  adapter/profiles/     JSON-driven adapter profiles
  addressing/           agent/room/inbox address parsing
  broker/               top-level broker facade
  broker/lease/         input lease arbitration (single-writer guarantee)
  broker/modetrack/     canonical vs raw terminal mode tracking
  broker/room/          room membership, join/leave, member queries
  broker/router/        message fan-out, delivery policies, safe-prompt
  broker/safeprompt/    heuristic: wait for terminal to reach a safe prompt
  broker/session/       PTY session lifecycle, spawn/wait, output log, screen
  broker/storage/       Store interface (SaveRoom, AppendMessage, etc.)
  broker/storage/sqlite/ SQLite implementation of Store
  broker/termcaps/      terminal capability query/response
  broker/vtscreen/      VT100 virtual terminal screen model
  client/               HTTP/JSON client (ForSocket, ForURL)
  daemon/               daemon lifecycle (broker + httpjson + unixsocket)
  envelope/             structured event envelope (v, id, kind, ts, body)
  intents/              typed request bodies (TerminalText, TerminalKey, etc.)
  kinds/                event kind registry (message.send, terminal.submit, etc.)
  transport/httpjson/   HTTP/JSON REST handlers (sessions, rooms, messages)
  transport/unixsocket/ Unix domain socket listener
```

### Dependency Direction

```
cmd/* ──▶ daemon ──▶ broker ──▶ session, router, room, lease, storage
               │         │
               │         └──▶ adapter ──▶ intents
               │
               └──▶ transport/httpjson ──▶ broker types, envelope, kinds
               └──▶ transport/unixsocket
               └──▶ client ──▶ httpjson types, vtscreen
```

Rules:
- Packages under `broker/` may import each other but NOT `transport/`, `client/`, or `daemon/`
- `adapter/` imports `intents/` only
- `transport/` imports `broker/` types but not the reverse
- `daemon/` composes everything — it is the only package allowed to import both `broker/` and `transport/`

---

## Key Concepts

### Intents → Bytes

The adapter compiles typed intents into PTY byte streams:

```go
// Intent (typed, structured)
intents.TerminalSubmit{Text: "hello", SubmitKey: "Enter"}

// Adapter output (raw bytes)
[]byte("hello\r")
```

Different adapters may produce different bytes for the same intent. The
`generic-tty` adapter handles bracketed paste, named key compilation, and
mode-aware submit keys.

### Input Leases

Only one controller can write to a session at a time. The lease manager
enforces this with TTL-based exclusive locks:

```go
lease, err := broker.AcquireLease(sessionID, scope, owner, ttl)
// ... use the session ...
broker.ReleaseLease(lease.ID)
```

Preemption is supported for room-driven message delivery.

### Rooms and Fan-Out

A room message goes through the router, which delivers to each member:

```
msg.send(room=R, text="refactor auth")
  └── router delivers to member A (terminal.submit)
  └── router delivers to member B (terminal.submit)
```

Delivery policies control *how* each member receives the message:
- `immediate` — write now
- `queue` — wait for safe prompt
- `safe-prompt-only` — reject if not at prompt
- `interrupt` — send interrupt key first

### Readiness Detection

Sessions track output activity. A session is "ready" when:

1. Output bytes-per-second drops below a threshold
2. Sustained idle for a debounce window
3. (Optional) terminal screen matches a prompt regex

This lets callers wait for agent completion before sending the next message.

---

## Testing Requirements

```bash
go test ./...          # unit tests
go test -race ./...    # race detection
```

### Test Conventions

- Broker tests use in-memory stores (no files on disk)
- Session tests spawn real PTY processes (via `creack/pty`)
- Transport tests use `httptest.NewServer` (no real sockets)
- Daemon tests use temp directories for SQLite and Unix sockets
- All tests clean up after themselves (no temp file leaks)

### Writing New Tests

- Table-driven tests for adapter compilation and envelope parsing
- Use `t.Parallel()` where safe (most broker tests are safe to parallelize)
- Use `context.WithTimeout` for any test that blocks on I/O
- Use real PTYs for integration tests, fakes for unit tests

---

## Common Tasks

### Adding a New Intent

1. Define the type in `foxprox/intents/intents.go`
2. Add the kind string in `foxprox/kinds/kinds.go`
3. Add compile method in `foxprox/adapter/generictty/adapter.go`
4. Add handler in `foxprox/transport/httpjson/server.go`
5. Add client method in `foxprox/client/client.go`
6. Add tests in each layer

### Adding a New Adapter Profile

1. Add the profile JSON to `foxprox/adapter/profiles/profiles.json`
2. Test that the profile loads correctly (profiles_test.go covers this)
3. If the profile needs custom compilation logic, extend the adapter factory

### Adding a New Delivery Policy

1. Add the policy constant in `foxprox/broker/router/router.go`
2. Implement the delivery logic in the router's `deliverToSession` path
3. Add the policy name to the `transport/httpjson` request type
4. Test with a real PTY session

---

## Engineering Principles

| Principle | Rule |
|---|---|
| **Functional core** | Broker logic is pure Go — no IO, no env reads, no `time.Now()` in core paths |
| **Adapter at the edge** | Byte compilation happens only in the adapter, never in the broker or transport |
| **Lease-gated writes** | Every PTY write goes through the lease manager — no raw writes |
| **Immutable snapshots** | Session state and screen models are snapshot-safe for concurrent reads |
| **No network calls** | The daemon never dials out — it is purely local |
| **Structured errors** | Every package uses `errors.New("foxprox <pkg>: ...")` prefixes |
| **No CGO** | modernc.org/sqlite provides pure-Go SQLite; no C toolchain needed |

---

## Hard Fails (AUTO-REJECT)

| Pattern | Why |
|---|---|
| Network calls from broker/adapter | foxprox is local-only by design |
| Raw PTY writes bypassing the lease manager | Breaks single-writer guarantee |
| Changing envelope `Version` constant | Wire contract break |
| CGO dependency | Breaks cross-compilation and container builds |
| `time.Now()` in broker core logic | Untestable; inject clock interface |
| Importing `transport/` from `broker/` | Architecture violation |

---

## Code Smells (Flag in Review)

| Smell | Why |
|---|---|
| Unbounded goroutines in session manager | Resource leak on shutdown |
| `map[string]any` deep in broker logic | Stringly-typed bugs; parse at boundary |
| Missing `context.Context` on blocking calls | No cancellation safety |
| Shared mutable state without single-owner goroutine | Races and non-determinism |
| Giant in-memory output buffers | OOM risk; stream to storage instead |

---

## Quick Reference

```yaml
Module: github.com/joshka/foxprox
Language: Go 1.26+ (no CGO)
CLI: foxproxctl (Cobra)
Transport: HTTP/JSON over Unix socket
Storage: SQLite via modernc.org/sqlite (pure Go)
Lint: golangci-lint + gofumpt
Tests: go test ./...
```
