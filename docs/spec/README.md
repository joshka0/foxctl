# Specifications Index

Use this folder for canonical behavior and wire-contract specifications.

## Primary Specs

- `v1/` - Foundational, versioned contracts (protocol, core profile, plugin, daemon).
- `agent_hierarchy.md` - Agent hierarchy and spawn protocol.
- `overseer_profile.md` - Overseer responsibilities and coordination profile.
- `v2_greenfield_bootstrap.md` - Greenfield v2 bootstrap architecture and execution plan.
- `v2_repo_rules_and_skills.md` - v2 repo rules and core skills governance for maintainability.
- `mailbox_blackboard.md` - Mailbox/blackboard model.
- `openapi_skill.md` - `http/openapi` behavior spec.
- `semantic_file_index.md` - Semantic file indexing spec.
- `code_symbol_index_and_swe_grep.md` - Symbol index + snippet extraction behavior.

## Status Guidance

- `docs/spec/v1/*` should be treated as stable contracts unless explicitly versioned.
- Non-`v1` docs may be evolving implementation specs.
- Historical or superseded specs should move to `docs/archive/specs/`.
