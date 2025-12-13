---
description: "Implementation Notes: Phase 7 PR2 Capture Hooks"
status: Draft
owner: jkatigbak
---

# Implementation Notes: Phase 7 PR2 Capture Hooks

## Scope

Implementation notes for `docs/specs/2025-12-12_phase7_pr2_capture_hooks.md`.

## Files (expected)

- `internal/runservice/*` – attach correlation/job metadata and persist
  trajectory records for `agentctl run`.
- `cmd/agentctl/cmd/dspy_agent.go` – capture spawn as a user request +
  trajectory.
- `internal/trajectorycapture/*` – shared capture helpers.

## Validation

- `CGO_ENABLED=0 go test ./...`
- `make lint`
