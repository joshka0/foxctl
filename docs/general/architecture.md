# Architecture (General Entry Point)

This page is a lightweight entry point for architecture topics.

## Canonical Architecture Docs

| Topic | Canonical doc |
|------|----------------|
| System architecture map | `docs/architecture/system-architecture.md` |
| Auth and identity architecture | `docs/architecture/auth-identity.md` |
| Chat adapter runtime | `docs/architecture/chat-platform-adapter.md` |
| Kubernetes runtime topology | `docs/architecture/kubernetes-runtime.md` |
| PostgreSQL + CAS architecture | `docs/architecture/postgres-storage.md` |

## General Companion Docs

| Concern | Doc |
|--------|-----|
| Runtime orchestration details | `docs/general/runtime-orchestration.md` |
| Storage model | `docs/general/storage.md` |
| Search/index model | `docs/general/search.md`, `docs/general/repoindex.md` |
| Hooks and context injection | `docs/general/hooks.md`, `docs/general/context-and-observability.md` |
| Agent policy/prompt model | `docs/general/agent-policy-and-prompts.md` |

## Why this split

- `docs/architecture/*` = canonical system/component architecture.
- `docs/general/*` = operational subsystem references for day-to-day usage.
