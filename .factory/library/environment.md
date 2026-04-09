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
- **macOS symlink gotcha:** `/var` is a symlink to `/private/var` on macOS. Any code comparing paths from git output (which resolves symlinks) against filesystem paths must use `filepath.EvalSymlinks()` on both sides before comparison. See `internal/worktree/manager.go` for the pattern.
- **Linux:** Gateway SSH server may need permissions for port 22 on Tailscale IP
- **tsnet:** Userspace networking only (no TUN device). Performance adequate for terminal use (~1-2ms latency overhead)
- **Binary size:** tsnet adds ~15-20 MB to binary (gVisor netstack + WireGuard)
- **Go version:** tailscale.com v1.96.5 requires Go 1.26+. The golangci-lint binary must be built with Go 1.26+ to lint packages that import tailscale. Install via: `GOBIN=/tmp/gobin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`
- **genproto conflict:** `google.golang.org/genproto` (flat module v0.0.0-20231106174013) must be kept in go.mod as a direct indirect dependency to resolve ambiguity with `google.golang.org/genproto/googleapis/rpc` (apache/arrow transitive dep). Do NOT let `go mod tidy` remove it.
- **tmux test dependency:** Several room-sandbox tests require tmux to be installed and available in PATH. Tests automatically skip with `t.Skip()` when tmux is unavailable. This applies to room destroy, room list, room show, and sandbox integration tests.
- **Test HOME isolation:** Use `t.Setenv("HOME", t.TempDir())` before `config.Load()` to isolate test config/storage from the real home directory. This pattern is used extensively in room-sandbox tests.

## Git Subprocess Patterns

- **GIT_CONFIG_NOSYSTEM=1:** Set on all git command executions in the worktree manager to prevent system-level git config from interfering with operations. Append to `os.Environ()` when building cmd.Env. See `internal/worktree/manager.go`.
- **filepath.Walk does not follow directory symlinks:** By design, for safety (prevents infinite loops). Symlinks to directories are visited but not recursed into. Code in `internal/worktree/manager.go` handles symlinks via `os.ModeSymlink` branch before `IsDir` check.
