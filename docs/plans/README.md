# Plans Index

This directory contains active planning documents and implementation roadmaps.

## How to Use

- Treat documents here as current planning references.
- When a plan is completed or superseded, move it to `docs/archive/` (or mark it historical in-file).
- Keep architecture reality in `docs/architecture/`; keep rollout sequencing and backlog in `docs/plans/`.
- Do not treat plan docs as the canonical as-built runtime map. For current behavior, prefer `docs/architecture/*` and `docs/general/*`.

## High-Signal Entrypoints

- `features/internal-package-topology-migration.md` — incremental `internal/*` family cleanup plan; makes the legacy runtime vs `v2` boundary explicit and sequences package-family consolidation.
- `features/eino-go-native-runtime-plan.md` — Eino `AgentEngine` integration + Go-native `RuntimeSpawner`/reconciler; Jido optional path.
- `tui-agent-control-plane.md` — archived TypeScript TUI plan; current terminal work is `go-tui-agent-shell.md`.
- `go-tui-agent-shell.md` — Go-native interactive coding shell plan with memory, continuity, and worker rails.
- `v2-implementation-todo.md`
- `v2-greenfield-bootstrap.md`
- `v2-symphony-kanban-implementation.md`
- `gui-agent-improvement-roadmap.md`
- `gui-agent-room-control-center.md`
- `features/slop-function-detection.md`
- `features/refactor-intelligence-substrate.md`
- `features/refactor-phase1-status-and-snapshot.md`
- `features/refactor-deterministic-detection-backlog.md`
- `features/agent-mux-room-hierarchy.md`
- `features/opensandbox-sandbox-workspace-integration.md`
- `features/foxctl-evolve-plan.md` — plan for a foxctl-native repo-evolution tool built on DB-backed experiment state, CAS artifacts, and existing worktree primitives.
- `features/longcot-rlm-evaluation-plan.md` — LongCoT × RLM internal paired eval plan for measuring RLM scaffold/staged-reasoning accuracy and token efficiency.
- `features/aca-self-evolving-memory-layer.md`
- `jidoctl-v2-hybrid-interface.md`
- `chat-platform-adapter.md`
- `teams-sre-integrations-plan.md`
- `k8s-sql-storage.md`
- `embedding-quality-roadmap/README.md`
- `k8s/00-overview.md`
- `migration_to_v2/INTEGRATION_PLAN.md`

## Legacy Note

Older phase-based planning docs also exist under `docs/impl_plan/`. Those are retained for historical context and should not be treated as the default planning location for new work.
