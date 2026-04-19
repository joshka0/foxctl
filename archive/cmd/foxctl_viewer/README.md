# Archived foxctl-viewer

Archived: 2026-04-18

This directory contains the legacy Bubble Tea viewer that previously lived at
`cmd/foxctl_viewer/` and built as `bin/foxctl-viewer`.

It is retained for historical reference only. New terminal companion-agent work
should target the canonical Go TUI:

- `cmd/foxctl_tui/`
- `internal/interfaces/tui/`
- `make go-tui-agent`
- `make go-tui-build`

The archived Go files use the `archived` build tag so they are not part of
normal `go list ./...` or `go test ./...` runs.

Reason for archive:

1. `cmd/foxctl_tui` is now the active terminal shell for live foxctl agents
2. keeping `foxctl-viewer` build targets active made the terminal UI surface
   ambiguous
3. historical viewer specs remain in `docs/spec/` for reference
