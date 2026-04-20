# Legacy TUI Archive

Archived: 2026-04-01

This package contains the pre-control-plane terminal UI that previously lived at `packages/tui/`.

It is retained for historical reference only. New terminal control-plane work should target:

- `cmd/foxctl_tui/`
- `internal/interfaces/tui/`
- `make go-tui-agent`

The intermediate TypeScript control-plane TUI is also archived at
`archive/packages/tui-agent/`.

Reason for archive:

1. the old TUI was diagnostics-first rather than operator-first
2. its information architecture diverged from the current `gui-agent` direction
3. continuing to evolve it in place would preserve the wrong product model
