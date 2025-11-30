# AGENTS.md and docs/start split

Date: 2025-11-30

## Summary

- Restructured `AGENTS.md` to be a concise, agentctl-specific guide that supplements `global_rules.md`. Removed generic guardrails, Go conventions, and checklists that are now covered by the global rules. Updated provider-specific docs (`CLAUDE.md`, `GEMINI.md`) to defer to `AGENTS.md` as the canonical source for agentctl conventions.
- Updated branch naming guidance from `codex/<feature-name>` to `feature/<short-name>`.
- Bumped Quick Reference Go version in `AGENTS.md` to Go >= 1.24 to match `go.mod`.
- Added new canonical source links from `AGENTS.md` to:
  - `docs/agent_profile.md` (Agent Profile)
  - `docs/spec/dspy_go_agents.md` (dspy-go agent runtime & tools)
  - `docs/start/testing_and_ci.md` and `docs/start/openapi_and_plugins.md`.
- Clarified that the repository layout in `AGENTS.md` is simplified and pointed to `ARCHITECTURE.md` for the full view.
- Replaced detailed Testing Requirements and Plugin Skeletons sections in `AGENTS.md` with concise summaries that point into docs.
- Created `docs/start/README.md` as an index for deep-dive docs referenced from `AGENTS.md`.
- Created `docs/start/testing_and_ci.md` for detailed local/CI testing expectations, race/CGO notes, test watcher, and feedback hooks.
- Created `docs/start/openapi_and_plugins.md` for OpenAPI skill behavior and plugin architecture, preserving example skeletons there.
- Added canonical-reference notes to `.claude/CLAUDE.md` and `GEMINI.md` so provider-specific docs explicitly defer to `AGENTS.md` for protocol and invariants.

## Notes

- The new docs/start entry points should be kept in sync with future CI or OpenAPI changes.
- AGENTS.md now routes to both Core Profile and Agent Profile specs plus dspy-go agent details, aligning the top-level guide with the current architecture.
