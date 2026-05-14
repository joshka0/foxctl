# foxctl Documentation

This is the canonical map for docs in this repository.

## Start Here

- [AGENTS.md](../AGENTS.md) - Contributor + AI assistant operating rules.
- [README.md](../README.md) - Product overview and quick start.
- [docs/start/README.md](start/README.md) - Fast orientation for common workflows.
- [docs/glossary.md](glossary.md) - Foxctl-specific terminology for agents and contributors.
- [docs/architecture/package-topology.md](architecture/package-topology.md) - Read this first before introducing a new `internal/*` root or placing new code under `internal/v2/*`.
- [docs/DOC_LIFECYCLE.md](DOC_LIFECYCLE.md) - Documentation lifecycle and maintenance policy.

## Current Reference Docs

- [docs/general/](general/) - Core subsystem guides (skills, hooks, memory, storage, sessions).
- [docs/general/embedding-rebuilds.md](general/embedding-rebuilds.md) - Canonical rebuild commands for embedding-backed stores after provider/model changes.
- [docs/general/task-continuity.md](general/task-continuity.md) - Deterministic task continuity pack, command/wrapper split, and artifact-backed delivery.
- [docs/general/repoindex.md](general/repoindex.md) - Repo graph index terminology, build/query commands, language coverage, and storage notes.
- [docs/general/repoindex-pageindex.md](general/repoindex-pageindex.md) - PageIndex-inspired retrieval model for repoindex, DAG grep, and semantic comments.
- [docs/general/core-package-coverage.md](general/core-package-coverage.md) - Machine-friendly core package coverage matrix.
- [docs/general/refactor-scout.md](general/refactor-scout.md) - Local refactor scout/advisor workflow, seam vocabulary, language coverage, and ContextWiki fit.
- [docs/general/code-search-evals.md](general/code-search-evals.md) - Stable code-search eval suites, checked-in policies, and wrapper commands.
- [docs/general/retrieval-evals.md](general/retrieval-evals.md) - Stable ContextWiki retrieval eval suites, wrapper commands, and current expected bands.
- [docs/general/tmux-collaboration.md](general/tmux-collaboration.md) - tmux-based live collaboration setup, structured pane inspection, and ContextWiki promotion flow.
- [docs/general/room-runtime-adoption-pass.md](general/room-runtime-adoption-pass.md) - Current adoption matrix for hardened room-runtime semantics and the remaining queued-draft dispatch gap.
- [configs/skills-pack/foxctl-room/SKILL.md](../configs/skills-pack/foxctl-room/SKILL.md) - Durable shared room coordination skill for room chat, relay, loop, and room tasks.
- [docs/architecture/](architecture/) - Current runtime architecture docs.
- [docs/architecture/context-architecture.md](architecture/context-architecture.md) - Workspace-local ContextWiki control plane and top-of-mind slice.
- [docs/architecture/rlm-gather-context.md](architecture/rlm-gather-context.md) - RLM `gather_context` tool over contextengine retrieval, reduction, and certification.
- [docs/architecture/package-topology.md](architecture/package-topology.md) - Canonical grouping map for `internal/*`, including what is legacy runtime and what `internal/v2/*` is replacing.
- [docs/architecture/jido-hybrid-runtime.md](architecture/jido-hybrid-runtime.md) - Canonical hybrid-runtime split for Jido orchestration + `foxctl` semantics.
- [docs/architecture/go-native-runtime-and-optional-jido.md](architecture/go-native-runtime-and-optional-jido.md) - Go-native replacements for Jido dependencies, optional frameworks, and where Eino fits (Jido optional for Elixir/BEAM users).
- [docs/architecture/auth-identity.md](architecture/auth-identity.md) - Auth, identity, and verification architecture map.
- [docs/guides/kubernetes.md](guides/kubernetes.md) - Kubernetes deployment guide tied to current overlays.
- [docs/general/events.md](general/events.md) - Event schema and persistence docs.

## Specifications

- [docs/spec/README.md](spec/README.md) - Canonical protocol and behavior specs.
- [docs/spec/v1/README.md](spec/v1/README.md) - Foundational v1 contracts.
- [docs/spec/agent_hierarchy.md](spec/agent_hierarchy.md) - Current hierarchy and spawn protocol.
- [docs/spec/overseer_profile.md](spec/overseer_profile.md) - Overseer coordination profile.
- [docs/spec/repo_graph_index_and_dag_grep.md](spec/repo_graph_index_and_dag_grep.md) - Canonical repo graph schema and `dag_grep` behavior contract.
- [docs/spec/rlm_query_runtime.md](spec/rlm_query_runtime.md) - Experimental RLM query-time runtime contract over ContextWiki, companion memory, and external state.
- [docs/spec/v2_greenfield_bootstrap.md](spec/v2_greenfield_bootstrap.md) - Evolving target-state v2 design spec, not the canonical as-built runtime map.
- [docs/spec/v2_repo_rules_and_skills.md](spec/v2_repo_rules_and_skills.md) - v2 repo rules and core skills governance.

## Current Runtime Reading Order

1. [docs/architecture/system-architecture.md](architecture/system-architecture.md)
2. [docs/architecture/package-topology.md](architecture/package-topology.md)
3. [docs/architecture/context-architecture.md](architecture/context-architecture.md)
4. [docs/architecture/rlm-gather-context.md](architecture/rlm-gather-context.md)
5. [docs/architecture/jido-hybrid-runtime.md](architecture/jido-hybrid-runtime.md)
5b. [docs/architecture/go-native-runtime-and-optional-jido.md](architecture/go-native-runtime-and-optional-jido.md) (target / migration alignment)
6. [docs/general/runtime-orchestration.md](general/runtime-orchestration.md)
7. [docs/general/agent-daemon.md](general/agent-daemon.md)
8. [docs/spec/agent_hierarchy.md](spec/agent_hierarchy.md)
9. [docs/spec/overseer_profile.md](spec/overseer_profile.md)

## Planning Docs

- [docs/plans/README.md](plans/README.md) - Active and current planning docs.
- [docs/plans/features/eino-go-native-runtime-plan.md](plans/features/eino-go-native-runtime-plan.md) - Eino `AgentEngine` integration + Go-native orchestration runtime; Jido optional.
- [docs/plans/features/opentui-agent-terminal-facades.md](plans/features/opentui-agent-terminal-facades.md) - Backend facade backlog for a greenfield OpenTUI agent terminal over v2 runtime, room, orchestration, skills, jobs, MCP, and CAS APIs.
- [docs/plans/gui-agent-improvement-roadmap.md](plans/gui-agent-improvement-roadmap.md) - Converged roadmap for turning `gui-agent` into a coherent operator control plane.
- [docs/plans/features/slop-function-detection.md](plans/features/slop-function-detection.md) - Plan for treating "slop" as deterministic structural findings on top of refactor scout, with optional advisor reranking.
- [docs/plans/features/refactor-intelligence-substrate.md](plans/features/refactor-intelligence-substrate.md) - Plan for making refactor scout index-aware through status, snapshots, dependency queries, change cursors, and evidence packs.
- [docs/plans/features/refactor-phase1-status-and-snapshot.md](plans/features/refactor-phase1-status-and-snapshot.md) - Concrete Phase 1 spec for `foxctl refactor status` and `foxctl refactor snapshot`, including CLI contract, envelopes, and snapshot persistence.
- [docs/plans/features/refactor-deterministic-detection-backlog.md](plans/features/refactor-deterministic-detection-backlog.md) - Prioritized backlog for improving refactor scout through symbol hotness, opportunity scoring, co-change signals, stronger dead-code roots, and other deterministic detection upgrades.
- [docs/plans/features/agent-mux-room-hierarchy.md](plans/features/agent-mux-room-hierarchy.md) - Proposed policy for mux-backed agent panes, parent-private subagents, and room membership boundaries.
- [docs/plans/features/opensandbox-sandbox-workspace-integration.md](plans/features/opensandbox-sandbox-workspace-integration.md) - Plan for running public-GUI agents in isolated OpenSandbox workspaces with shallow clones and controlled retrieval access.

## Historical and Generated Material

- [docs/archive/README.md](archive/README.md) - Archived/superseded docs.
- [docs/changelogs/README.md](changelogs/README.md) - Historical change notes.
- [docs/designs/README.md](designs/README.md) - Design proposals (mixed status).
- [docs/codemaps/README.md](codemaps/README.md) - Generated codemap artifacts.
- [docs/foxprox/](foxprox/) - ATCP protocol docs, live smoke findings, and room integration notes.
- [docs/goals/](goals/) - Goal documents for active initiatives (coordinator, memory, refactor, etc.).
- [docs/analysis/](analysis/) - Deep analysis documents (RLM retrieval lanes, validation contracts).
- [docs/checklists/](checklists/) - Operational checklists (security pre-launch, etc.).

## Additional Topic Areas

- [docs/examples/README.md](examples/README.md) - Example workflows.
- [docs/examples/public-openai-compat-demo-config.yaml](examples/public-openai-compat-demo-config.yaml) - Example config for a public/demo OpenAI-compatible backend.
- [docs/ci/README.md](ci/README.md) - CI-specific docs.
- [docs/guides/README.md](guides/README.md) - How-to guides.
- [docs/research/README.md](research/README.md) - Exploratory research notes.
- [docs/general/runtime-orchestration.md](general/runtime-orchestration.md) - Workflow-specific docs.
- [docs/godot/README.md](godot/README.md) - Godot integration docs.
- [docs/notes/README.md](notes/README.md) - Working notes (non-canonical).
- [docs/viewer.md](viewer.md) - Viewer applications (GUI, TUI) and runtime topology.
- [docs/SECURITY.md](SECURITY.md) - Security policy, supported versions, and vulnerability reporting.
- [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md) - Diagnosis guide for common installation, build, skill, job, and performance issues.

## Automated Checks

- Local: `make check-doc-links`
- CI: `.github/workflows/docs.yml`
