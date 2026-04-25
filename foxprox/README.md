# foxprox

Agent Terminal Coordination Protocol — a local-first daemon for coordinating
interactive CLI processes, agent messages, PTY-backed terminal sessions, and
multi-agent rooms.

## Structure

```
cmd/
  foxproxd/       daemon binary
  foxproxctl/     CLI client
  foxprox-smoke/  smoke test runner
  foxprox-live/   live integration test
  foxprox-room-smoke/ room smoke test
internal/foxprox/
  adapter/        intent-to-bytes compilers (generic-tty, profiles)
  addressing/     agent address parsing
  broker/         core broker (sessions, rooms, routing, storage, PTY mgmt)
  client/         HTTP/JSON client library
  daemon/         daemon lifecycle (broker + transport + socket)
  envelope/       event envelope type
  intents/        typed intent definitions
  kinds/          event kind registry
  transport/      HTTP/JSON and Unix socket transports
docs/             protocol spec, smoke test notes, TODO
```

## Build & Test

```bash
go build ./...
go test ./...
```

## Dependencies

- `github.com/oklog/ulid/v2` — ULID generation
- `github.com/creack/pty` — PTY management
- `modernc.org/sqlite` — pure-Go SQLite for broker persistence

## Quick Start

```bash
# Start the daemon
go run ./cmd/foxproxd

# In another terminal, create a session
go run ./cmd/foxproxctl sessions create --cmd bash --rows 24 --cols 80

# List sessions
go run ./cmd/foxproxctl sessions list
```

## Protocol

See [docs/ATCP-v0.1.md](docs/ATCP-v0.1.md) for the full protocol specification.
