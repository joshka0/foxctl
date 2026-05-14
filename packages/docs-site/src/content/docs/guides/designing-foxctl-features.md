---
title: Design a foxctl feature
description: The smallest safe path from idea to command, skill, agent surface, and docs.
---

Status: Current guide.

foxctl features usually cross several layers: CLI, skill runtime, storage,
context, retrieval, agents, or UI. The safe design path is to choose the owner
family first, keep the first slice narrow, and document which surfaces are
current versus planned.

## Start with the user path

Write the first sentence as a user action:

```text
As an agent or operator, I need to ...
```

Then decide the smallest public surface:

| User need | First surface |
|---|---|
| Repeatable task with JSON input/output | Skill |
| Long-running or stateful local behavior | CLI command plus storage |
| Agent coordination | Agent or room command |
| Retrieval or graph navigation | Repoindex, semantic search, or context engine |
| External service call | Provider, MCP, or OpenAPI adapter |
| Dashboard workflow | API plus GUI only after the CLI path is clear |

Avoid adding a runtime abstraction before a command or proof path exists.

## Choose the package family

Use the package topology map before adding or moving code.

| Concern | Package family |
|---|---|
| Domain value types | `internal/domain` |
| Wire contracts and envelopes | `internal/protocol` |
| Config, workspace, clocks, shared platform utilities | `internal/platform` |
| Durable state, CAS, local DBs, projections | `internal/storage` |
| Context, memory, transcript continuity | `internal/context/*` |
| Repo graph, semantic search, codemaps, retrieval | `internal/intelligence/*` |
| Skill execution, jobs, hooks, daemon hosting | `internal/runtime/*` |
| Web, gateway, chat, OpenAPI surfaces | `internal/interfaces/*` |
| Newer agent/runtime/orchestration lane only | `internal/v2/*` |

`internal/v2` is not a general destination for new context, retrieval, storage,
or interface code.

## Keep the first slice reversible

Prefer this order:

1. Document the behavior and status label.
2. Add a small pure core or storage shape.
3. Add a CLI or skill proof.
4. Add tests and golden outputs.
5. Add agent, hook, room, or GUI surfaces only after the command path works.

For experimental dependencies, add a gate first. The LadybugDB planning posture
is the model: external projection first, no dual-write, no core Go dependency
until the proof succeeds.

## Design contracts

Strong foxctl features usually define:

| Contract | Question to answer |
|---|---|
| Input schema | What does the caller provide? |
| Output envelope | What stable data can scripts consume? |
| Source of truth | Which store or source owns canonical state? |
| Derived artifacts | What can be rebuilt or deleted safely? |
| Failure mode | What does fallback look like? |
| Observability | Which event, report, or artifact proves what happened? |
| Verification | Which command proves the feature works? |

If the output can exceed the inline threshold, design the CAS path before
shipping the command.

## Retrieval feature design

For retrieval and context features, decide which plane owns each job:

| Plane | Owns |
|---|---|
| Semantic search | Meaning-based candidate discovery |
| Repoindex | Graph-shaped source relationships and explanation subgraphs |
| ContextWiki | Workspace continuity, vault notes, proposal lanes, and retrieval policy |
| Context engine | Typed evidence packs, lane fusion, retrieval episodes, and feedback |
| App code | Ranking policy, query planning, permissions, and LLM summarization |

Keep ranking and policy explicit. Do not route behavior through ad hoc keyword
matching.

## Documentation status

Every new feature doc should say one of:

- `Current`
- `Experimental`
- `Planned`
- `Archive`

If a plan has not shipped, keep it in `docs/plans/` and link it as planned
context. Do not present it as production behavior.

## Verification checklist

Before calling a feature production-ready:

- CLI or skill path works from a clean checkout.
- Tests cover the behavior contract.
- Generated output is deterministic or stored as a CAS artifact.
- Markdown links pass.
- Security-sensitive dependency changes were vetted before install.
- Agent-facing docs mention fallback and boundaries.

## Canonical sources

- [docs/architecture/package-topology.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/package-topology.md)
- [docs/general/skills.md](https://github.com/joshka0/foxctl/blob/main/docs/general/skills.md)
- [docs/general/repoindex.md](https://github.com/joshka0/foxctl/blob/main/docs/general/repoindex.md)
- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)

