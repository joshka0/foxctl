# Specifications Index

Use this folder for canonical behavior and wire-contract specifications.

## Current Behavior Specs

- `v1/` - Foundational, versioned contracts (protocol, core profile, plugin, daemon).
- `agent_hierarchy.md` - Agent hierarchy and spawn protocol.
- `overseer_profile.md` - Overseer responsibilities and coordination profile.
- `v2_symphony_kanban_orchestration.md` - Symphony ingress alignment and Kanban orchestration read model for v2.
- `mailbox_blackboard.md` - Mailbox/blackboard model.
- `openapi_skill.md` - `http/openapi` behavior spec.
- `semantic_file_index.md` - Semantic file indexing spec.
- `code_symbol_index_and_swe_grep.md` - Symbol index + snippet extraction behavior.
- `repo_graph_index_and_dag_grep.md` - Repo graph data model and `dag_grep` explanation-query contract.

## Evolving V2 Design Specs

- `v2_greenfield_bootstrap.md` - Target-state v2 architecture and execution design. Use this as direction-setting guidance, not the exact as-built package map.
- `v2_repo_rules_and_skills.md` - v2 repo rules and core skills governance for maintainability.

## Supporting References

- `docs/architecture/system-architecture.md` - Canonical current-state architecture map.
- `docs/general/runtime-orchestration.md` - Canonical current runtime execution/orchestration map.
- `docs/general/agent-daemon.md` - Current daemon and command-routing behavior.
- `docs/plans/v2-greenfield-bootstrap.md` - Sequenced implementation plan for ongoing v2 cutover work.
- `docs/general/memory.md` - Current memory/storage contract baseline.
- `docs/general/companion-memory.md` - Companion memory layering and temporal context model.
- `docs/general/context-and-observability.md` - Existing observability/context primitives that inform v2 events/trace design.

## Status Guidance

- `docs/spec/v1/*` should be treated as stable contracts unless explicitly versioned.
- Non-`v1` docs may be evolving implementation specs.
- Historical or superseded specs should move to `docs/archive/specs/`.
