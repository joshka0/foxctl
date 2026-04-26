# foxprox

> Local-first daemon for driving interactive CLI agents over PTY sessions with structured intents, multi-agent rooms, and reliable message delivery.

Most AI agent CLIs (Claude Code, Codex, Gemini CLI, Aider, etc.) are interactive terminal programs -- they read from a PTY, emit ANSI sequences, and expect keyboard input. Treating them like HTTP APIs with `text + \n` is unreliable: prompts get corrupted, output is lost mid-stream, and you can't tell when an agent is ready for the next message.

**foxprox** fixes this by separating *intent* from *bytes*. You send structured intents (`message.send`, `terminal.submit`, `terminal.key`) and the broker handles PTY modes, bracketed paste, submit-key detection, output readiness, and input lease arbitration automatically.

## Why foxprox?

| Problem | What foxprox does |
|---|---|
| Sending `text + \n` to an agent CLI corrupts prompts or gets eaten by readline | Compiles typed intents into the exact byte sequence the terminal expects (bracketed paste, submit keys, raw mode) |
| Multiple controllers writing to the same PTY causes garbled input | Input lease arbitration ensures only one writer at a time |
| You can't tell when an agent is done thinking vs. still streaming | Output-idle readiness detection with configurable thresholds and screen-regex matching |
| Coordinating N agents in a "room" requires custom glue per pair | Rooms with fan-out message delivery, member tracking, and persistent history |
| Every tool builds its own PTY management from scratch | Zero-dependency broker that owns sessions, PTYs, screen models, and event logs |

## Architecture

```
                          foxprox architecture

 ┌──────────┐   HTTP/JSON    ┌─────────────────────────────────────────┐
 │          │   over Unix     │              broker                     │
 │  foxprox ───────────────▶ │                                         │
 │  ctl     │   socket        │  ┌─────────┐  ┌─────────┐  ┌────────┐ │
 │          │                 │  │ session  │  │  router  │  │  room  │ │
 └──────────┘                 │  │ manager  │  │ (fan-out)│  │        │ │
                              │  └────┬────┘  └────┬────┘  └────────┘ │
 ┌──────────┐   HTTP/JSON     │       │             │                   │
 │          │   or embed      │  ┌────┴────┐  ┌────┴────┐             │
 │  your    ───────────────▶ │  │   PTY   │  │  lease  │             │
 │  app     │                 │  │ (creack)│  │ manager │             │
 └──────────┘                 │  └────┬────┘  └─────────┘             │
                              │       │                                │
                              │  ┌────┴────────────────┐              │
                              │  │      adapter         │              │
                              │  │  intent  ──▶  bytes  │              │
                              │  │  (generic-tty, etc.) │              │
                              │  └─────────────────────┘              │
                              │                                       │
                              │  ┌──────────┐  ┌───────────┐         │
                              │  │ vtscreen │  │  storage   │         │
                              │  │ (VT100)  │  │  (sqlite)  │         │
                              │  └──────────┘  └───────────┘         │
                              └─────────────────────────────────────────┘

  Sessions own a PTY + adapter + screen model + output log.
  Rooms own members, message history, and fan-out routing.
  The daemon bundles broker + HTTP transport + Unix socket listener.
```

## Quick Start

```bash
# Build
go build ./...

# Start the daemon (listens on ~/.foxctl/foxprox.sock)
go run ./cmd/foxproxd

# In another terminal:
# Create a room
go run ./cmd/foxproxctl room create --workspace my-project --title agents

# Spawn two agent sessions
go run ./cmd/foxproxctl session create --cmd "claude" --submit-key Enter
go run ./cmd/foxproxctl session create --cmd "aider" --submit-key Enter

# Join them to the room
go run ./cmd/foxproxctl room join <room-id> --agent claude --session <id-1>
go run ./cmd/foxproxctl room join <room-id> --agent aider --session <id-2>

# Send a message (fan-out delivered to both agents)
go run ./cmd/foxproxctl msg send --room <room-id> --text "Refactor the auth module"

# Check if an agent is ready for input
go run ./cmd/foxproxctl session activity <session-id>
```

## Structure

```
foxprox/
  cmd/
    foxproxd/           long-running daemon
    foxproxctl/         CLI client
    foxprox-smoke/      smoke test runner
    foxprox-live/       live integration test
    foxprox-room-smoke/ room fan-out smoke test
  foxprox/
    adapter/            intent-to-bytes compilers
      generictty/       generic terminal adapter (bracketed paste, keys, submit)
      profiles/         built-in adapter profiles (JSON config)
    addressing/         agent address parsing (agent:<id>, room:<id>, etc.)
    broker/             core coordination engine
      lease/            input lease arbitration
      modetrack/        terminal mode state tracking (canonical vs raw)
      room/             room membership and message history
      router/           message fan-out and delivery policies
      safeprompt/       wait-for-safe-prompt heuristic
      session/          PTY session lifecycle, output log, screen model
      storage/          persistence interface
        sqlite/         pure-Go SQLite implementation
      termcaps/         terminal capability negotiation
      vtscreen/         VT100 virtual terminal screen model
    client/             HTTP/JSON client library (for socket or URL)
    daemon/             daemon lifecycle (broker + transport + socket)
    envelope/           structured event envelope (foxprox/0.1)
    intents/            typed intent definitions (text, key, submit, paste, write_bytes)
    kinds/              event kind registry
    transport/          wire transport implementations
      httpjson/         HTTP/JSON REST API (sessions, rooms, messages, events)
      unixsocket/       Unix domain socket listener
  docs/                 protocol spec, smoke test notes, TODO
```

## Key Concepts

### Sessions

A session is one interactive process tree under a PTY. The broker owns the PTY, tracks terminal modes, maintains a virtual screen model, and records an output log. Every session gets an adapter that compiles typed intents into target-specific bytes.

### Rooms

A room groups sessions as members. When a message is sent to a room, the router fan-out delivers it to every member's session (via the adapter). Delivery is serialized per-session through input leases. Rooms track membership, message history, and can be persisted to SQLite.

### Intents

Intents are the typed operations a controller sends. The broker translates them into PTY bytes at the edge:

| Intent | What it does |
|---|---|
| `message.send` | Fan-out text to all room members (router + adapter) |
| `terminal.submit` | Type text and press a submit key (Enter, etc.) |
| `terminal.key` | Send a named key press (Escape, Tab, Ctrl+C, arrows...) |
| `terminal.paste` | Paste text with bracketed-paste handling |
| `terminal.write_bytes` | Raw byte escape hatch (capability-gated) |

### Adapters

Adapters compile intents into bytes the target process understands. The built-in `generic-tty` adapter handles:

- UTF-8 text encoding
- Bracketed paste (auto-detect, force, or off)
- Named key compilation (Enter, Tab, Escape, arrows, F-keys, Ctrl/Ctrl+Shift combos)
- Per-profile submit keys and mode defaults

### Delivery Policies

When sending a message to a room member, you can choose how the broker delivers it:

| Policy | Behavior |
|---|---|
| `immediate` | Write bytes right now regardless of terminal state |
| `queue` | Queue until the terminal is at a safe prompt |
| `safe-prompt-only` | Reject if the terminal is not at a safe prompt |
| `reject` | Do not deliver to this member |
| `interrupt` | Send an interrupt key first, then deliver |

### Readiness

The broker tracks whether a session's output has gone idle (stopped emitting new bytes). This lets you wait for an agent to finish thinking before sending the next message. Readiness is configurable via:

- Bytes-per-second threshold (output has slowed below a rate)
- Debounce window (sustained idle for N milliseconds)
- Screen regex (terminal shows a known prompt pattern)

## Embedding

foxprox can run as a standalone daemon (`foxproxd`) or embedded in another Go binary. The daemon package exposes a clean lifecycle:

```go
d := daemon.New(daemon.Options{
    DataDir:    "/path/to/data",   // empty = in-memory
    SocketPath: "/tmp/foxprox.sock",
    Logger:     slog.Default(),
})
d.Start()           // bootstrap broker + listen
d.Wait(ctx)         // block until stopped
d.Shutdown(ctx)     // graceful teardown
```

The client library works over Unix sockets or plain HTTP:

```go
c := client.ForSocket("/tmp/foxprox.sock")
sessions, _ := c.ListSessions(ctx)
c.SendMessage(ctx, httpjson.SendMessageRequest{
    RoomID: "room-id",
    Text:   "Hello agents",
})
```

## Build & Test

```bash
go build ./...
go test ./...          # all 20 packages
go test -race ./...    # with race detector
```

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/oklog/ulid/v2` | Time-sortable unique IDs for envelopes, rooms, sessions |
| `github.com/creack/pty` | Cross-platform PTY (pseudo-terminal) management |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) for broker persistence |

## Protocol

See [docs/ATCP-v0.1.md](docs/ATCP-v0.1.md) for the full protocol specification (860 lines covering intents, envelope format, delivery policies, room semantics, and terminal mode handling).

## License

Apache License 2.0
