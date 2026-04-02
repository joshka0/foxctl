# agentctl Documentation

This is the canonical map for docs in this repository.

## Start Here

- [AGENTS.md](../AGENTS.md) - Contributor + AI assistant operating rules.
- [README.md](../README.md) - Product overview and quick start.
- [docs/start/README.md](start/README.md) - Fast orientation for common workflows.
- [docs/DOC_LIFECYCLE.md](DOC_LIFECYCLE.md) - Documentation lifecycle and maintenance policy.

## Current Reference Docs

- [docs/general/](general/) - Core subsystem guides (skills, hooks, memory, storage, sessions).
- [docs/general/embedding-rebuilds.md](general/embedding-rebuilds.md) - Canonical rebuild commands for embedding-backed stores after provider/model changes.
- [docs/general/task-continuity.md](general/task-continuity.md) - Deterministic task continuity pack, command/wrapper split, and artifact-backed delivery.
- [docs/general/core-package-coverage.md](general/core-package-coverage.md) - Machine-friendly core package coverage matrix.
- [docs/general/refactor-scout.md](general/refactor-scout.md) - Local refactor scout/advisor workflow, seam vocabulary, language coverage, and ACA fit.
- [docs/general/code-search-evals.md](general/code-search-evals.md) - Stable code-search eval suites, checked-in policies, and wrapper commands.
- [docs/general/retrieval-evals.md](general/retrieval-evals.md) - Stable ACA retrieval eval suites, wrapper commands, and current expected bands.
- [docs/general/retrieval-stack-snapshot.md](general/retrieval-stack-snapshot.md) - Compact current benchmark snapshot across code-search and ACA retrieval.
- [docs/general/tmux-collaboration.md](general/tmux-collaboration.md) - tmux-based live collaboration setup, structured pane inspection, and ACA promotion flow.
- [docs/architecture/](architecture/) - Current runtime architecture docs.
- [docs/architecture/context-architecture.md](architecture/context-architecture.md) - Workspace-local ACA control plane and top-of-mind slice.
- [docs/architecture/jido-hybrid-runtime.md](architecture/jido-hybrid-runtime.md) - Canonical hybrid-runtime split for Jido orchestration + `agentctl` semantics.
- [docs/architecture/auth-identity.md](architecture/auth-identity.md) - Auth, identity, and verification architecture map.
- [docs/kubernetes.md](kubernetes.md) - Kubernetes deployment guide tied to current overlays.
- [docs/observability/README.md](observability/README.md) - Event schema and persistence docs.

## Specifications

- [docs/spec/README.md](spec/README.md) - Canonical protocol and behavior specs.
- [docs/spec/v1/README.md](spec/v1/README.md) - Foundational v1 contracts.
- [docs/spec/agent_hierarchy.md](spec/agent_hierarchy.md) - Current hierarchy and spawn protocol.
- [docs/spec/overseer_profile.md](spec/overseer_profile.md) - Overseer coordination profile.
- [docs/spec/repo_graph_index_and_dag_grep.md](spec/repo_graph_index_and_dag_grep.md) - Canonical repo graph schema and `dag_grep` behavior contract.
- [docs/spec/rlm_query_runtime.md](spec/rlm_query_runtime.md) - Experimental RLM query-time runtime contract over ACA, companion memory, and external state.
- [docs/spec/v2_greenfield_bootstrap.md](spec/v2_greenfield_bootstrap.md) - Evolving target-state v2 design spec, not the canonical as-built runtime map.
- [docs/spec/v2_repo_rules_and_skills.md](spec/v2_repo_rules_and_skills.md) - v2 repo rules and core skills governance.

## Current Runtime Reading Order

1. [docs/architecture/system-architecture.md](architecture/system-architecture.md)
2. [docs/architecture/context-architecture.md](architecture/context-architecture.md)
3. [docs/architecture/jido-hybrid-runtime.md](architecture/jido-hybrid-runtime.md)
4. [docs/general/runtime-orchestration.md](general/runtime-orchestration.md)
5. [docs/general/agent-daemon.md](general/agent-daemon.md)
6. [docs/spec/agent_hierarchy.md](spec/agent_hierarchy.md)
7. [docs/spec/overseer_profile.md](spec/overseer_profile.md)

## Planning Docs

- [docs/plans/README.md](plans/README.md) - Active and current planning docs.
- [docs/plans/gui-agent-improvement-roadmap.md](plans/gui-agent-improvement-roadmap.md) - Converged roadmap for turning `gui-agent` into a coherent operator control plane.
- [docs/plans/features/v2-skills-parity-plan.md](plans/features/v2-skills-parity-plan.md) - Bring v2 profiles/tooling up to parity with ACA, Obsidian, and newer retrieval surfaces.
- [docs/plans/features/agentctl-rlm-integration-outline.md](plans/features/agentctl-rlm-integration-outline.md) - Concrete outline for adding an RLM query-time runtime over ACA, companion memory, and repo/vault state.
- [docs/plans/features/agentctl-rlm-next-steps.md](plans/features/agentctl-rlm-next-steps.md) - Routed and staged next-step plan for turning the experimental RLM runtime into a practical retrieval/controller layer.
- [docs/plans/features/slop-function-detection.md](plans/features/slop-function-detection.md) - Plan for treating "slop" as deterministic structural findings on top of refactor scout, with optional advisor reranking.
- [docs/plans/features/refactor-intelligence-substrate.md](plans/features/refactor-intelligence-substrate.md) - Plan for making refactor scout index-aware through status, snapshots, dependency queries, change cursors, and evidence packs.
- [docs/plans/features/refactor-phase1-status-and-snapshot.md](plans/features/refactor-phase1-status-and-snapshot.md) - Concrete Phase 1 spec for `agentctl refactor status` and `agentctl refactor snapshot`, including CLI contract, envelopes, and snapshot persistence.
- [docs/plans/features/refactor-deterministic-detection-backlog.md](plans/features/refactor-deterministic-detection-backlog.md) - Prioritized backlog for improving refactor scout through symbol hotness, opportunity scoring, co-change signals, stronger dead-code roots, and other deterministic detection upgrades.
- [docs/plans/features/rlm-retrieval-findings.md](plans/features/rlm-retrieval-findings.md) - Current benchmark snapshot comparing ACA, direct repoindex lanes, and RLM retrieval modes.
- [docs/plans/features/aca-retrieval-hypotheses.md](plans/features/aca-retrieval-hypotheses.md) - Deterministic ACA retrieval hypotheses and eval modes for testing control-plane, vault, trust, and repo-hint improvements.
- [docs/plans/features/aca-self-corrective-loop.md](plans/features/aca-self-corrective-loop.md) - First slice for observing ACA retrieval misses, classifying them, and proposing deterministic corrections.
- [docs/plans/features/aca-self-evolving-memory-layer.md](plans/features/aca-self-evolving-memory-layer.md) - Plan for turning ACA into a proposal-driven, eval-gated memory control loop with `L5` external evidence intake.
- [docs/plans/features/generic-agent-experiment-loop.md](plans/features/generic-agent-experiment-loop.md) - Design for turning long-running agents into hypothesis-driven, evaluator-bounded experiment loops.
- [docs/plans/features/workspace-embedding-overrides-research.md](plans/features/workspace-embedding-overrides-research.md) - Checklist for enabling workspace-local embedding overrides safely without dimension drift.
- [docs/plans/features/opensandbox-sandbox-workspace-integration.md](plans/features/opensandbox-sandbox-workspace-integration.md) - Plan for running public-GUI agents in isolated OpenSandbox workspaces with shallow clones and controlled retrieval access.
- [docs/impl_plan/README.md](impl_plan/README.md) - Legacy phased plan stream kept for history.

## Historical and Generated Material

- [docs/archive/README.md](archive/README.md) - Archived/superseded docs.
- [docs/changelogs/README.md](changelogs/README.md) - Historical change notes.
- [docs/designs/README.md](designs/README.md) - Design proposals (mixed status).
- [docs/codemaps/README.md](codemaps/README.md) - Generated codemap artifacts.

## Additional Topic Areas

- [docs/examples/README.md](examples/README.md) - Example workflows.
- [docs/examples/public-openai-compat-demo-config.yaml](examples/public-openai-compat-demo-config.yaml) - Example config for a public/demo OpenAI-compatible backend.
- [docs/ci/README.md](ci/README.md) - CI-specific docs.
- [docs/guides/README.md](guides/README.md) - How-to guides.
- [docs/research/README.md](research/README.md) - Exploratory research notes.
- [docs/workflows/README.md](workflows/README.md) - Workflow-specific docs.
- [docs/godot/README.md](godot/README.md) - Godot integration docs.
- [docs/notes/README.md](notes/README.md) - Working notes (non-canonical).

## Automated Checks

- Local: `make check-doc-links`
- CI: `.github/workflows/docs.yml`
