---
title: Documentation map
description: Where current, planned, archived, and generated foxctl docs live.
---

The foxctl documentation spans the production docs site, the repository `docs/` directory, and generated artifacts. This page maps the structure, explains lifecycle categories, and shows how to find what you need.

## Repository docs structure

The canonical docs live in `docs/` at the repository root:

### Current behavior

| Directory | Contents |
|---|---|
| `docs/general/` | Subsystem guides and operational references (skills, hooks, memory, storage, sessions, events) |
| `docs/architecture/` | Current architecture boundaries (runtime, storage, context, auth, package topology) |
| `docs/spec/` | Protocol and behavior contracts |
| `docs/spec/v1/` | Stable v1 protocol docs; non-v1 specs may still be evolving |
| `docs/guides/` | How-to guides (Kubernetes deployment, feature design) |
| `docs/start/` | Fast orientation for common workflows |

### Planning

| Directory | Contents |
|---|---|
| `docs/plans/` | Active implementation plans and roadmaps |
| `docs/plans/features/` | Individual feature plans and design documents |

Plan content is not production behavior until promoted into current docs.

### Historical and generated material

| Directory | Contents |
|---|---|
| `docs/archive/` | Superseded or historical material |
| `docs/codemaps/` | Generated analysis artifacts |
| `docs/changelogs/` | Historical change notes |
| `docs/designs/` | Design proposals (mixed status) |
| `docs/research/` | Exploratory research notes |
| `docs/notes/` | Working notes (non-canonical) |
| `docs/examples/` | Example workflows and configuration |
| `docs/ci/` | CI-specific documentation |

## Lifecycle categories

Docs are organized by lifecycle class, defined in `docs/DOC_LIFECYCLE.md`:

| Class | Location | Meaning | Maintenance expectation |
|------|----------|---------|-------------------------|
| **Current** | `docs/general/`, `docs/architecture/`, `docs/spec/` | Represents current behavior and contracts | Must be kept accurate with code changes |
| **Active Plan** | `docs/plans/` | Forward-looking implementation work | Update as plans evolve; archive when complete or superseded |
| **Legacy Plan** | `docs/impl_plan/` | Historical phased plans | Keep for provenance; avoid using as canonical guidance |
| **Historical** | `docs/archive/` | Superseded or legacy material | Read-only except link hygiene and provenance notes |
| **Generated** | `docs/codemaps/` | Generated analysis artifacts | Regenerate when needed; not canonical |
| **Working Notes** | `docs/notes/`, `docs/research/`, `docs/designs/` | Exploratory or non-final material | Promote stable decisions into current docs |

### Rules

1. New canonical behavior docs go in `docs/general/`, `docs/architecture/`, or `docs/spec/`.
2. New planning docs go in `docs/plans/` (not `docs/impl_plan/`).
3. Completed or superseded plans and designs move to `docs/archive/`.
4. Historical docs may keep legacy content, but links must remain navigable.
5. When behavior changes, update the matching canonical doc in the same PR.
6. Markdown links in repo docs must pass `make check-doc-links`.

## Docs-site sections

This Starlight site organizes content into browsable sections:

| Site area | Purpose | Example pages |
|---|---|---|
| **Start Here** | First-run path and docs taxonomy | [Overview](/start/overview/), [Install](/start/install-first-run/), [Docs map](/start/docs-map/) |
| **Guides** | Feature design, skill authoring, workflow walkthroughs | [Design a feature](/guides/designing-foxctl-features/), [Add a skill](/guides/add-a-skill/) |
| **Core Workflows** | Skills, retrieval, repoindex, ACA, context engine, memory | [Skills runtime](/skills/runtime-and-install/), [Search and index](/retrieval/search-and-index/) |
| **Agents and Rooms** | Agent daemon, overseer, rooms, collaboration | [Agent lifecycle](/agents/lifecycle/), [Orchestration](/agents/orchestration/), [Rooms](/collaboration/rooms/) |
| **Integrations** | MCP, OpenAPI, hooks, chat platforms, Obsidian, and integration maturity | [Integration status](/integrations/status/), [Providers and MCP](/integrations/providers-and-mcp/), [Hooks](/integrations/hooks/) |
| **Reference** | Command map, CLI, Protocol v1, storage | [CLI](/reference/cli/), [Command map](/reference/command-map/), [Protocol v1](/reference/protocol-v1/) |
| **Architecture** | System, design principles, runtime, API, auth | [System](/architecture/system/), [Design principles](/architecture/design-principles/), [Runtime](/architecture/runtime/) |
| **Operations** | Gotchas, observability, CI/evals, benchmarks, Kubernetes | [Gotchas](/operations/gotchas/), [Troubleshooting](/operations/troubleshooting/), [Benchmarks](/quality/benchmarks/) |
| **Roadmap and Archive** | Progress, in-progress work, planned, experimental, generated, and historical material | [Progress](/roadmap/progress/), [Planned and archive](/roadmap/planned-and-archive/) |

## Reading order for new contributors

1. [`AGENTS.md`](https://github.com/joshka0/foxctl/blob/main/AGENTS.md) — Contributor and AI assistant operating rules
2. [`README.md`](https://github.com/joshka0/foxctl/blob/main/README.md) — Product overview and quick start
3. [`docs/start/README.md`](https://github.com/joshka0/foxctl/blob/main/docs/start/README.md) — Fast orientation for common workflows
4. [`docs/architecture/package-topology.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/package-topology.md) — Read before introducing a new `internal/*` root
5. [`docs/DOC_LIFECYCLE.md`](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md) — Documentation lifecycle and maintenance policy

## Current runtime reading order

For understanding the full system architecture:

1. [`docs/architecture/system-architecture.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/system-architecture.md)
2. [`docs/architecture/package-topology.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/package-topology.md)
3. [`docs/architecture/context-architecture.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
4. [`docs/architecture/rlm-gather-context.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/rlm-gather-context.md)
5. [`docs/architecture/jido-hybrid-runtime.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/jido-hybrid-runtime.md)
6. [`docs/architecture/go-native-runtime-and-optional-jido.md`](https://github.com/joshka0/foxctl/blob/main/docs/architecture/go-native-runtime-and-optional-jido.md)
7. [`docs/general/runtime-orchestration.md`](https://github.com/joshka0/foxctl/blob/main/docs/general/runtime-orchestration.md)
8. [`docs/general/agent-daemon.md`](https://github.com/joshka0/foxctl/blob/main/docs/general/agent-daemon.md)
9. [`docs/spec/agent_hierarchy.md`](https://github.com/joshka0/foxctl/blob/main/docs/spec/agent_hierarchy.md)
10. [`docs/spec/overseer_profile.md`](https://github.com/joshka0/foxctl/blob/main/docs/spec/overseer_profile.md)

## Automated checks

- Local: `make check-doc-links`
- CI: `.github/workflows/docs.yml`

## Canonical sources

- [`docs/README.md`](https://github.com/joshka0/foxctl/blob/main/docs/README.md)
- [`docs/DOC_LIFECYCLE.md`](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md)
