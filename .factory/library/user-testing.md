# User Testing — Room Sandbox Mission

Testing surface, required tools, and resource cost classification for validation.

**What belongs here:** Validation surface discoveries, testing tool requirements, resource constraints. Updated by user-testing validators with runtime findings.

---

## Validation Surface

### Surface 1: CLI (agentctl commands)
- **Tools:** `tuistory`, shell assertions
- **Entry points:** `agentctl room create --sandbox`, `agentctl room list`, `agentctl room show`, `agentctl room destroy`, `agentctl gateway`
- **Setup:** Build agentctl binary, ensure git/tmux/zellij on PATH

### Surface 2: Web Terminal (xterm.js)
- **Tools:** `agent-browser`
- **Entry points:** `http://localhost:8765/terminal/{room-id}` (dev mode) or `https://<hostname>/terminal/{room-id}` (Tailscale)
- **Setup:** Start gateway in --dev mode, create sandbox room, open browser

### Surface 3: SSH Terminal
- **Tools:** Shell-based SSH commands
- **Entry points:** `ssh room-<id>@<hostname>` (Tailscale) or local SSH to gateway
- **Setup:** Gateway running, sandbox room with tmux session

### Surface 4: Gateway API
- **Tools:** `curl`
- **Entry points:** `GET /healthz`, `GET /terminal/{room-id}`, WebSocket upgrade
- **Setup:** Gateway running in any mode

## Validation Concurrency

**Machine:** 64 GB RAM, 16 cores (Apple Silicon)

| Surface | Per-instance Cost | Max Concurrent | Rationale |
|---------|------------------|----------------|-----------|
| CLI (tuistory) | ~50 MB | 5 | Lightweight, process spawn only |
| Web (agent-browser) | ~300 MB | 5 | Browser instance per validator |
| SSH | ~10 MB | 5 | Just SSH processes |
| Gateway API (curl) | ~5 MB | 5 | HTTP requests only |

**Gateway overhead:** ~100 MB (single process, shared across all validators)

**Total worst case:** 5 × 300 MB + 100 MB = 1.6 GB — well within 64 GB headroom.

## Notes

- Gateway --dev mode (localhost:8765) should be used for CI/local validation
- Tailscale integration (tsnet) requires TS_AUTHKEY for full validation
- For validation without Tailscale: use --dev mode for web terminal tests
- SSH validation in --dev mode: SSH server listens on localhost in dev mode
- tmux must be available for all terminal tests; skip gracefully if missing
