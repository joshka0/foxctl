# Goal: Remote Workbench PR-A/PR-B

## Goal

Implement the first two executable slices from
`docs/plans/features/remote-workbench-session-handoff.md`:

1. PR-A: harden the existing `internal/interfaces/gateway/webterm` WebSocket
   protocol so binary frames are terminal input and text frames are explicit
   control messages only.
2. PR-B: bridge authenticated web/gateway requests into
   `internal/domain/identity.Principal` and forward explicit Better Auth
   identity headers through the GUI auth gateway for both `/api` and `/ws`
   proxy paths.

The branch must leave durable workbench sessions, opaque workbench terminal
attachments, WebSocket tickets, write leases, remote host handoff, and pi-mono
adapter implementation out of scope.

## Context

- Current branch: `feat/remote-workbench-pr-a-b`, stacked on
  `feat/harden-remote-workbench-plan`.
- Planning doc:
  `docs/plans/features/remote-workbench-session-handoff.md`.
- PR-A files likely involved:
  - `internal/interfaces/gateway/webterm/client.go`
  - `internal/interfaces/gateway/webterm/types.go`
  - `internal/interfaces/gateway/webterm/handler.go`
  - `internal/interfaces/gateway/webterm/handler_test.go`
  - `internal/interfaces/gateway/static/index.html`
- PR-B files likely involved:
  - `internal/interfaces/web/api/auth.go`
  - `internal/interfaces/web/api/auth_test.go`
  - `internal/interfaces/gateway/whois.go`
  - `internal/interfaces/gateway/whois_test.go`
  - `packages/gui-auth-gateway/src/server.ts`
  - `packages/gui-auth-gateway/src/config.ts`
  - `packages/gui-auth-gateway/README.md`

## Constraints

- Preserve the existing `/terminal/{room-id}` and `/ws/terminal/{room-id}`
  compatibility endpoints; they remain development/tailnet surfaces.
- Do not add durable `WorkbenchSession`, `TerminalAttachment`, `AttachTicket`,
  or `WriteLease` storage.
- Do not add `/ws/workbench-terminal/{attachment_id}`.
- Do not add remote host provisioning or remote handoff orchestration.
- Do not implement Pi or pi-mono adapter code in this branch.
- Do not use terminal scrollback as runtime, room, task, memory, or session
  truth.
- Do not expose raw tmux, zellij, pane, backend, or filesystem identifiers as
  browser authority.
- Do not add dependencies without explicit approval.
- Follow `AGENTS.md`: preserve envelope invariants, avoid keyword heuristics for
  behavioral routing, keep package placement aligned with
  `docs/architecture/package-topology.md`, and run doc link checks when docs
  change.

## Milestones

### Milestone 1: PR-A Web Terminal Protocol

Done when:

- WebSocket binary frames are the only implicit terminal input path.
- WebSocket text frames are parsed as explicit JSON control messages or rejected
  with a JSON control error.
- JSON-looking shell input is covered by a binary-frame test.
- Unknown control messages and invalid JSON text do not silently disappear.
- Resize controls reject zero, negative, malformed, and excessive dimensions.
- Browser static terminal code sends xterm input as binary frames and resize as
  JSON text control.
- Disconnect cleanup remains deterministic.

### Milestone 2: PR-B Principal Bridge

Done when:

- Web API identity can be converted into `identity.Principal` through a
  `PrincipalFromRequest` seam near web auth/middleware.
- Tailscale identity headers, Better Auth headers, anonymous requests,
  conflicting identity headers, missing tenant behavior, and workspace mismatch
  behavior are covered by tests.
- Better Auth gateway proxying forwards explicit identity headers to the
  upstream for both `/api` and `/ws` paths after session validation.
- Trusted identity headers are stripped/replaced at the proxy boundary so browser
  clients cannot spoof upstream identity by sending their own headers.
- Request logging redacts future `ticket` query parameters.

## Verification

Run narrow checks after each milestone and full checks before finalizing:

```bash
go test ./internal/interfaces/gateway/webterm
go test ./internal/interfaces/gateway/...
go test ./internal/interfaces/web/...
bun run --cwd packages/gui-auth-gateway typecheck
make check-doc-links
./bin/foxctl index anchors lint --workspace . --summary
git diff --check
```

If package-wide tests are too broad or fail for unrelated existing reasons,
record the exact failure and run the narrowest relevant package tests that prove
the changed behavior.

## Stop Conditions

- Stop after 3 failed attempts at the same failing check and summarize the
  blocker.
- Stop before adding storage tables, public workbench terminal routes, tickets,
  leases, remote handoff orchestration, or Pi adapter code.
- Stop before changing authentication semantics outside the documented proxy and
  principal bridge boundary.
- Stop before adding dependencies.
- Stop if the implementation would require exposing raw mux/backend identifiers
  to browser clients.

## Final Self-Review

Before completion, write a short review note covering:

- What PR-A and PR-B behavior changed.
- Which tests prove the binary/text WebSocket split.
- Which tests prove principal bridging and proxy identity forwarding.
- Any compatibility risk for existing `/terminal/{room-id}` users.
- Residual risks and confidence score.
