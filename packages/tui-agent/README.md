# tui-agent

Terminal-first operator control plane for `agentctl`.

This package is the active replacement for the archived legacy TUI. It is intentionally starting small:

1. shell and navigation
2. control-plane surface framing
3. workspace-aware operator workflow

Base:

- `pi-mono` / `@mariozechner/pi-tui` for terminal UI primitives
- `agentctl` for runtime, orchestration, rooms, and evidence contracts

Implementation plan:

- [`docs/plans/tui-agent-control-plane.md`](../../docs/plans/tui-agent-control-plane.md)
