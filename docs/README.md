# agentctl Documentation

This is the canonical map for docs in this repository.

## Start Here

- [AGENTS.md](../AGENTS.md) - Contributor + AI assistant operating rules.
- [README.md](../README.md) - Product overview and quick start.
- [docs/start/README.md](start/README.md) - Fast orientation for common workflows.
- [docs/DOC_LIFECYCLE.md](DOC_LIFECYCLE.md) - Documentation lifecycle and maintenance policy.

## Current Reference Docs

- [docs/general/](general/) - Core subsystem guides (skills, hooks, memory, storage, sessions).
- [docs/general/core-package-coverage.md](general/core-package-coverage.md) - Machine-friendly core package coverage matrix.
- [docs/architecture/](architecture/) - Current runtime architecture docs.
- [docs/architecture/jido-hybrid-runtime.md](architecture/jido-hybrid-runtime.md) - Canonical hybrid-runtime split for Jido orchestration + `agentctl` semantics.
- [docs/architecture/auth-identity.md](architecture/auth-identity.md) - Auth, identity, and verification architecture map.
- [docs/kubernetes.md](kubernetes.md) - Kubernetes deployment guide tied to current overlays.
- [docs/observability/README.md](observability/README.md) - Event schema and persistence docs.

## Specifications

- [docs/spec/README.md](spec/README.md) - Canonical protocol and behavior specs.
- [docs/spec/v1/README.md](spec/v1/README.md) - Foundational v1 contracts.
- [docs/spec/agent_hierarchy.md](spec/agent_hierarchy.md) - Current hierarchy and spawn protocol.
- [docs/spec/overseer_profile.md](spec/overseer_profile.md) - Overseer coordination profile.
- [docs/spec/v2_greenfield_bootstrap.md](spec/v2_greenfield_bootstrap.md) - Evolving target-state v2 design spec, not the canonical as-built runtime map.
- [docs/spec/v2_repo_rules_and_skills.md](spec/v2_repo_rules_and_skills.md) - v2 repo rules and core skills governance.

## Current Runtime Reading Order

1. [docs/architecture/system-architecture.md](architecture/system-architecture.md)
2. [docs/architecture/jido-hybrid-runtime.md](architecture/jido-hybrid-runtime.md)
3. [docs/general/runtime-orchestration.md](general/runtime-orchestration.md)
4. [docs/general/agent-daemon.md](general/agent-daemon.md)
5. [docs/spec/agent_hierarchy.md](spec/agent_hierarchy.md)
6. [docs/spec/overseer_profile.md](spec/overseer_profile.md)

## Planning Docs

- [docs/plans/README.md](plans/README.md) - Active and current planning docs.
- [docs/impl_plan/README.md](impl_plan/README.md) - Legacy phased plan stream kept for history.

## Historical and Generated Material

- [docs/archive/README.md](archive/README.md) - Archived/superseded docs.
- [docs/changelogs/README.md](changelogs/README.md) - Historical change notes.
- [docs/designs/README.md](designs/README.md) - Design proposals (mixed status).
- [docs/codemaps/README.md](codemaps/README.md) - Generated codemap artifacts.

## Additional Topic Areas

- [docs/examples/README.md](examples/README.md) - Example workflows.
- [docs/ci/README.md](ci/README.md) - CI-specific docs.
- [docs/guides/README.md](guides/README.md) - How-to guides.
- [docs/research/README.md](research/README.md) - Exploratory research notes.
- [docs/workflows/README.md](workflows/README.md) - Workflow-specific docs.
- [docs/godot/README.md](godot/README.md) - Godot integration docs.
- [docs/notes/README.md](notes/README.md) - Working notes (non-canonical).

## Automated Checks

- Local: `make check-doc-links`
- CI: `.github/workflows/docs.yml`
