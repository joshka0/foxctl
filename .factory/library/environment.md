# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** Required env vars, external API keys/services, dependency quirks, platform-specific notes.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

## Required Environment Variables

| Variable | Purpose | Required |
|----------|---------|----------|
| `TS_AUTHKEY` | Tailscale auth key for gateway tsnet | Yes (production), No (dev mode) |
| `OPEN_SANDBOX_API_KEY` | OpenSandbox API authentication | Only for --runtime opensandbox |
| `OPEN_SANDBOX_BASE_URL` | OpenSandbox server URL | Only for --runtime opensandbox |

## External Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| git | 2.53.0 | Worktree management |
| tmux | 3.6a | Terminal multiplexing |
| zellij | 0.43.1 | Alternative terminal multiplexer |
| Go | 1.25+ | Build and test |

## Platform Notes

- **macOS (primary):** PTY handling via creack/pty works on macOS
- **Linux:** Gateway SSH server may need permissions for port 22 on Tailscale IP
- **tsnet:** Userspace networking only (no TUN device). Performance adequate for terminal use (~1-2ms latency overhead)
- **Binary size:** tsnet adds ~15-20 MB to binary (gVisor netstack + WireGuard)
