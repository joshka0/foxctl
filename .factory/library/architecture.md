# Architecture — Room Sandbox Mission

How the room sandbox system works at a high level.

## What belongs here
System-level architectural knowledge that workers need to understand how components relate. NOT implementation details — those belong in code comments.

---

## Component Overview

```
agentctl CLI
├── agentctl gateway (long-lived daemon)
│   ├── tsnet listener (Tailscale networking, userspace WireGuard)
│   ├── HTTP server
│   │   ├── /healthz (subsystem status)
│   │   ├── /terminal/{room-id} (xterm.js frontend, embedded)
│   │   └── /ws/terminal/{room-id} (WebSocket→PTY bridge)
│   └── SSH server
│       └── room-<id>@<hostname> (SSH→PTY bridge)
├── agentctl room create --sandbox
│   ├── Git worktree (via internal/platform/worktree/)
│   ├── tmux/zellij session
│   └── Gateway registration
└── agentctl room destroy
    ├── Worktree cleanup
    ├── tmux session kill
    └── Gateway deregistration
```

## Data Flow

### Room Create --sandbox
1. CLI parses `--sandbox` flag and options (--worktree-root, --base-ref, --runtime)
2. If runtime=opensandbox: provision container via OpenSandbox client
3. If runtime=worktree (default): create git worktree via internal/platform/worktree/
4. Create tmux session named after room
5. Persist SandboxConfig in Room struct (board store)
6. Register room with gateway (terminal route)
7. Return envelope with sandbox metadata

### Web Terminal Access
1. Browser loads /terminal/{room-id} (embedded xterm.js)
2. xterm.js opens WebSocket to /ws/terminal/{room-id}
3. Gateway resolves room-id → tmux session name (from Room.SandboxConfig)
4. Gateway spawns `tmux attach -t <session>` in a new PTY (creack/pty)
5. PTY stdin/stdout piped to WebSocket bidirectionally
6. Resize events: browser → WebSocket message → pty.Setsize()
7. On WebSocket close: PTY exits, tmux client detaches (session survives)

### SSH Access
1. User runs `ssh room-<id>@<gateway-hostname>`
2. Gateway SSH server accepts connection on tsnet listener
3. WhoIs lookup verifies Tailscale identity
4. SSH username parsed for room ID
5. Gateway spawns `tmux attach -t <session>` in PTY
6. SSH channels connected to PTY
7. On SSH disconnect: PTY exits, tmux detaches cleanly

### Room Destroy
1. Verify no active agents in room
2. Kill tmux session (`tmux kill-session -t <name>`)
3. Remove worktree (via internal/platform/worktree/)
4. Or: delete OpenSandbox container
5. Deregister from gateway
6. Clear SandboxConfig from Room struct

## Key Invariants

- **One gateway process** routes to all rooms (not one per room)
- **tmux sessions survive** terminal disconnect (web/SSH detach, not kill)
- **Worktrees are outside** the main repo directory (sibling pattern)
- **Tailscale-only access** (no public ports) unless --dev mode
- **SandboxConfig persisted** in board store alongside room metadata
- **Idempotent operations**: re-creating sandbox on existing room returns existing state
- **Rollback on failure**: if any step in create fails, previous steps are cleaned up

## Existing Code Integration Points

| Component | Location | Integration |
|-----------|----------|-------------|
| Room struct | internal/domain/agent/board_message.go | Add SandboxConfig field |
| Board store | internal/storage/blackboard/board_store.go | Persist/restore SandboxConfig |
| Room create | cmd/agentctl/cmd/room.go | Add --sandbox flag handling |
| Room destroy | cmd/agentctl/cmd/room.go | Add sandbox cleanup |
| tmux bridge | internal/tmuxbridge/client.go | Use for session creation |
| zellij bridge | internal/zellijbridge/client.go | Use for zellij sessions |
| WebSocket hub | internal/web/consolews/hub.go | Reference for WS patterns |
| Web server | internal/web/server.go | Reference for HTTP patterns |
| OpenSandbox | internal/sandbox/opensandbox/client.go | Extend, don't rewrite |
| Cobra root | cmd/agentctl/cmd/root.go | Register gateway command |
