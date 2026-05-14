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
- `features/opentui-agent-terminal-facades.md` — facade backlog for a greenfield OpenTUI agent terminal over v2 runtime, room, orchestration, skills, jobs, MCP, and CAS APIs.
- `features/remote-workbench-session-handoff.md` — plan for moving a pi-mono-inspired TUI workbench to a Tailscale remote and continuing from browser/terminal attachments.
- `features/semantic-code-anchors.md` — plan for typed code comments as repo graph edges, semantic envelope anchors, and ContextWiki retrieval signals.
- `features/ladybugdb-graph-projection-spike.md` — gated spike plan for testing LadybugDB as a disposable repo graph projection without changing canonical repoindex/Turso storage or default Go builds.
- `features/official-docs-production-release.md` — vetted Starlight docs-site plan, supply-chain gate, information architecture, and production documentation release matrix.
- `features/durable-execution-recovery.md` — plan for v2 orchestration-first crash recovery using the orchestration card projection as the durable retry queue; runner checkpointing is deferred until side effects are replay-safe.
- `features/durable-execution-layer1-side-effects.md` — Layer 1 runner side-effect safety sequence: Turn Request Registry, idempotent event append, then model/tool effect journal.
- `features/foxctl-benchmark-suite-epic.md` — baseline-implemented benchmark epic for Go runtime, DAG/repoindex, RLM, rooms, hooks, and retrieval evidence.
- `features/longcot-eval-contract-plan.md` — LongCoT eval contract plan for measuring RLM scaffold, staged reasoning, and token efficiency under the canonical runtime surface.
- `features/room-epic-pipeline.md` — room-agile epic pipeline and factory mission parity.
- `features/smolvm-foxctl-agent-runtime-plan.md` — SmolVM-backed agent runtime plan.
- `features/rlm-recursive-fanout-runtime-plan.md` — RLM recursive fan-out runtime plan.
- `features/rlm-helper-pipeline-repair-plan.md` — RLM helper pipeline repair plan.
- `features/factory-mission-import.md` — factory mission import for room-agile epics.
- `follow_up_durable_execution.md` — follow-up backlog after the completed LLM-chat Layer 1 durable execution baseline.
- `go-tui-agent-shell.md` — Go-native interactive coding shell plan with memory, continuity, and worker rails.
- `v2-implementation-todo.md`
- `v2-greenfield-bootstrap.md`
- `v2-symphony-kanban-implementation.md`
- `gui-agent-improvement-roadmap.md`
- `gui-agent-room-control-center.md`
- `gui-agent-v2-rearchitecture.md`
- `context-buffer-design.md`
- `atcp-rooms-replacement.md`
- `index-improvements.md`
- `interactive-agent-system-integration.md`
- `symbolkey-followup-improvements.md`
- `features/slop-function-detection.md`
- `features/refactor-intelligence-substrate.md`
- `features/refactor-phase1-status-and-snapshot.md`
- `features/refactor-deterministic-detection-backlog.md`
- `features/agent-mux-room-hierarchy.md`
- `features/opensandbox-sandbox-workspace-integration.md`
- `features/foxctl-evolve-plan.md` — plan for a foxctl-native repo-evolution tool built on DB-backed experiment state, CAS artifacts, and existing worktree primitives.
- `features/memory-core-curator-layer.md`
- `features/semantic-commenting-cleanup-notes.md`
- `jidoctl-v2-hybrid-interface.md`
- `k8s/00-overview.md`
- `gui-v2/` — GUI v2 Svelte SPA implementation plans
- `tui-redesign/architecture.md`
- `tui-redesign/component-spec.md`
- `tui-redesign/adrs/`

## Legacy Note

Older phase-based planning docs also exist under `docs/impl_plan/`. Those are retained for historical context and should not be treated as the default planning location for new work.
