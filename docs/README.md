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
- [docs/architecture/auth-identity.md](architecture/auth-identity.md) - Auth, identity, and verification architecture map.
- [docs/kubernetes.md](kubernetes.md) - Kubernetes deployment guide tied to current overlays.
- [docs/observability/README.md](observability/README.md) - Event schema and persistence docs.

## Specifications

- [docs/spec/README.md](spec/README.md) - Canonical protocol and behavior specs.
- [docs/spec/v1/README.md](spec/v1/README.md) - Foundational v1 contracts.
- [docs/spec/v2_greenfield_bootstrap.md](spec/v2_greenfield_bootstrap.md) - Greenfield v2 bootstrap plan.
- [docs/spec/v2_repo_rules_and_skills.md](spec/v2_repo_rules_and_skills.md) - v2 repo rules and core skills set.

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
