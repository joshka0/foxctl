# Archived TypeScript TUI Agent

Archived: 2026-04-18

This package previously contained the Bun/TypeScript terminal-first operator
control plane for `foxctl`.

It is retained for historical reference only. New terminal work should target the
canonical Go TUI:

- `cmd/foxctl_tui/`
- `internal/interfaces/tui/`
- `make go-tui-agent`
- `make go-tui-build`

Reason for archive:

1. the Go TUI is now the canonical companion-agent shell
2. the TypeScript TUI target wrote to the same `bin/foxctl-tui` path
3. keeping both active made it unclear which interface should replace Codex or
   Claude Code workflows

Historical implementation plan:

- [`docs/plans/tui-agent-control-plane.md`](../../../docs/plans/tui-agent-control-plane.md)
