---
name: go-gateway
description: Go worker specialized for gateway/terminal features — handles tsnet, WebSocket, SSH, PTY, and xterm.js integration with specific expertise in networking and terminal protocols.
---

# Go Gateway Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Features involving the gateway service: tsnet networking, web terminal (xterm.js + WebSocket), SSH server, PTY management, and embedded static assets. Used for M2 (Gateway + Terminal) features.

## Required Skills

None — this worker uses native Go tooling directly.

## Work Procedure

### 1. Read Context (MANDATORY FIRST STEP)

Before writing any code, read:

1. `{missionDir}/AGENTS.md` — boundaries, architecture, conventions
2. `.factory/library/architecture.md` — system architecture
3. `.factory/library/user-testing.md` — validation surface info
4. Your assigned feature from `{missionDir}/features.json`
5. Existing WebSocket patterns: `internal/web/consolews/hub.go`, `internal/web/consolews/session.go`
6. Existing server patterns: `internal/web/server.go`
7. Existing tmux bridge: `internal/tmuxbridge/client.go`
8. Existing zellij bridge: `internal/zellijbridge/client.go`
9. Existing Cobra patterns: `cmd/agentctl/cmd/web.go`

### 2. Add Dependencies (if needed)

For gateway features, these dependencies may need adding to go.mod:
```bash
go get tailscale.com/tsnet
go get github.com/creack/pty
go get golang.org/x/crypto/ssh
```

Note: `github.com/coder/websocket` is already in the project.

### 3. Write Tests First (TDD)

1. For tsnet: test with `--dev` mode (localhost HTTP) since CI won't have Tailscale
2. For WebSocket: use `httptest.Server` + `websocket.Accept` for in-process testing
3. For PTY: verify tmux is available, create test sessions, attach via PTY
4. For SSH: test SSH server in-process using `net.Pipe()` or localhost listener
5. Table-driven tests for routing, error handling, concurrent access
6. Race detection: `go test -race` for all concurrent code

### 4. Implement

Key architectural patterns:

**Gateway command (`cmd/agentctl/cmd/gateway.go`):**
```go
func newGatewayCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "gateway",
        Short: "Start the room terminal gateway",
        RunE:  runGateway,
    }
    // flags: --ts-authkey, --state-dir, --dev, --hostname, --port
    return cmd
}
```

**HTTP/WebSocket server:**
- Use stdlib `net/http` + `http.ServeMux` (no external framework, matching existing patterns)
- WebSocket via `github.com/coder/websocket`
- Static assets via `embed.FS`

**SSH server:**
- `golang.org/x/crypto/ssh` for SSH protocol
- `tsnet.Server.WhoIs()` for identity verification
- Room routing via SSH username: `room-<id>@<hostname>`

**PTY bridge:**
- `github.com/creack/pty` for PTY management
- `exec.Command("tmux", "attach", "-t", session)` or `exec.Command("tmux", "new", "-A", "-s", name)`
- Resize via `pty.Setsize(ptmx, &pty.Winsize{Rows, Cols})`

### 5. Verify

1. `go test ./internal/gateway/... -v` — all tests pass
2. `go test -race ./internal/gateway/...` — no races
3. `go vet ./internal/gateway/...` — clean
4. `golangci-lint run --timeout 10m ./internal/gateway/...`
5. Build: `go build ./cmd/agentctl/...` — compiles cleanly
6. Manual test with `--dev` mode: start gateway, curl healthz, open terminal in browser

### 6. Commit

1. Only commit files related to your feature
2. If adding new dependencies, `go mod tidy` before committing
3. Commit go.sum changes alongside go.mod

## Example Handoff

```json
{
  "salientSummary": "Implemented gateway tsnet core + web terminal. agentctl gateway starts with --dev mode, serves xterm.js at /terminal/{room-id}, WebSocket bridge to tmux sessions. 15 tests passing including concurrent access.",
  "whatWasImplemented": "cmd/agentctl/cmd/gateway.go (Cobra command), internal/gateway/server.go (tsnet+HTTP server), internal/gateway/webterm/handler.go (WebSocket→PTY bridge), internal/gateway/static/ (embedded xterm.js assets), tests for each",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {"command": "go test ./internal/gateway/... -v", "exitCode": 0, "observation": "15 tests passing"},
      {"command": "go test -race ./internal/gateway/...", "exitCode": 0, "observation": "No races"},
      {"command": "make build", "exitCode": 0, "observation": "Binary builds cleanly"},
      {"command": "./bin/agentctl gateway --dev & sleep 2 && curl -sf http://localhost:8765/healthz", "exitCode": 0, "observation": "Returns {\"tsnet\":\"dev-mode\",\"tmux\":\"ok\"}"}
    ],
    "interactiveChecks": [
      {"action": "Opened http://localhost:8765/terminal/test-room in browser", "observed": "xterm.js terminal rendered, shell prompt visible, typed 'echo hello' and saw output"}
    ]
  },
  "tests": {
    "added": [
      {"file": "internal/gateway/webterm/handler_test.go", "cases": [
        {"name": "TestWebSocketConnect", "verifies": "VAL-GW-006"},
        {"name": "TestWebSocketBidirectional", "verifies": "VAL-GW-007"},
        {"name": "TestConcurrentClients", "verifies": "VAL-GW-014"},
        {"name": "TestMaxConnections", "verifies": "VAL-GW-029"}
      ]},
      {"file": "cmd/agentctl/cmd/gateway_test.go", "cases": [
        {"name": "TestGatewayDevMode", "verifies": "VAL-GW-027"},
        {"name": "TestGatewayHealthz", "verifies": "VAL-GW-028"}
      ]}
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- tsnet dependency causes build issues that can't be resolved
- PTY behavior differs across platforms (macOS vs Linux)
- tmux/zellij version incompatibility
- Feature scope requires modifying off-limits files
- SSH authentication design needs security review
