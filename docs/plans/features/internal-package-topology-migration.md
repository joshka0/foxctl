# Internal Package Topology Migration Plan

| Field | Value |
|-------|--------|
| Status | Active plan |
| Canonical architecture | [docs/architecture/package-topology.md](../../architecture/package-topology.md) |
| Related runtime docs | [docs/architecture/system-architecture.md](../../architecture/system-architecture.md), [docs/general/runtime-orchestration.md](../../general/runtime-orchestration.md) |

## Goal

Reshape `internal/*` into a small number of explicit package families so new code has
obvious placement rules and existing packages stop reading like a flat dumping ground.

This plan is intentionally **incremental**:

- no repo-wide rename in one PR
- no attempt to merge package-topology cleanup with the broader runtime migration
- no requirement that the final target tree exist immediately before the package
  family boundaries become useful

## Core constraints

1. `internal/v2/*` stays explicitly scoped to the newer **agent/runtime/orchestration**
   lane.
2. `internal/v2/*` is **not** the replacement namespace for context, retrieval,
   storage, or interfaces.
3. `internal/storage/*`, `internal/domain/*`, `internal/platform/*`, and
   `internal/protocol` remain stable foundation families.
4. Moves should happen when touching a package family for real work, not as pure
   churn unless the package boundary itself is the problem being fixed.

## The family model

The current repo already wants to be organized around these families:

| Family | Current anchors | Meaning |
|--------|------------------|---------|
| runtime | `agent/*`, `execution/*`, `runservice`, `daemon`, `v2/*` | agent execution, orchestration, lifecycle, runtime state |
| context | `companion`, `context/*`, `contextplane/*`, `sessionkit`, `transcriptpipeline`, `knowledge/*` | ACA/control-plane, context assembly, transcript history, durable knowledge |
| intelligence | `indexing/*`, `retrieval*`, `repoquery`, `search*`, `codecontext/*`, `codemap/*`, `refactor/*`, `analysis/*`, `verification/*` | ingestion, retrieval, evidence extraction, synthesis, refactor planning, verification |
| interfaces | `web/*`, `gateway/*`, `chatadapter/*`, `openapi/*`, `console`, `consoleapp` | server, UI, transport, platform-facing entrypoints |
| tooling | `tooling`, `tools/*`, `skillrun`, `adapters/skillslib/*` | generic tool registry, skill execution, reusable tooling adapters |
| foundations | `storage/*`, `domain/*`, `platform/*`, `protocol` | shared persistence, contracts, platform helpers, wire helpers |

## What `v2` is actually replacing

`internal/v2` is the newer runtime lane, not a repo-wide v2 of everything.

### Direct replacement targets

| Legacy/current package(s) | V2 replacement target | Notes |
|---------------------------|-----------------------|-------|
| `internal/agent/runtime` | `internal/v2/runtime/*` plus `internal/v2/services/*` | core agent session/runtime replacement seam |
| `internal/agent/daemon` | `internal/v2/runtime/{runner,orchestration,supervisor}` | partial replacement today |
| `internal/execution/agentmanager` | `internal/v2/services/{spawn,kill,list,run}` | fallback path remains in places |
| agent-management paths in `internal/daemon` | prefer `internal/v2/services/*` semantics | keep `internal/daemon` as hosting shell where needed |
| some runtime-facing management in `internal/agent/tools` | should increasingly delegate to v2 services or Go-owned runtime state | transitional |
| Jido-backed live runtime inspection in `internal/v2/adapters/jido` | Go-owned runtime state with Jido optional | adapter should shrink in default path |

### Explicit non-targets

These families are peers, not “legacy runtime”:

- `internal/storage/*`
- `internal/companion`, `internal/contextplane`, `internal/transcriptpipeline`, `internal/knowledge`
- `internal/indexing/*`, `internal/retrieval*`, `internal/codecontext`, `internal/codemap`, `internal/refactor`
- `internal/web`, `internal/gateway`, `internal/chatadapter`, `internal/openapi`
- `internal/domain`, `internal/platform`, `internal/protocol`

## As-built `v2` usage today

The codebase already uses `internal/v2/*` in a focused way:

| Surface | Current usage |
|--------|----------------|
| CLI ask flow | `cmd/agentctl/cmd/agent.go` builds `v2services.NewAskService(...)` |
| orchestration/spawn/list/kill | `cmd/agentctl/cmd/orchestration.go`, `cmd/agentctl/cmd/overseer_v2_orchestration.go`, and `internal/web/api/orchestration.go` construct v2 services |
| companion context assembly | `internal/companion/service.go` and `internal/companion/v2_context_adapter.go` wire `internal/v2/runtime/contextbuilder` |
| optional runtime backend bridge | `internal/agent/runtime/runtime.go` gates Eino through `internal/v2/adapters/eino` |
| newer retrieval lane | `internal/retrieval/doc.go` already points new work to `internal/retrieval/v2` |

This is why the topology doc should keep saying “`v2` is mostly the newer
agent/runtime/orchestration lane.”

## Package family matrix

Use this matrix when deciding what to do with an existing package.

### Runtime family

| Action | Packages | Reason |
|--------|----------|--------|
| keep | `internal/v2/*` | already coherent as the newer runtime lane |
| keep | `internal/agent/*` | still needed while hybrid runtime remains live |
| bridge | `internal/daemon`, `internal/execution/agentmanager`, agent-management pieces in `internal/agent/tools` | transitional shells between legacy and v2 |
| move later | `internal/agentpane`, `internal/tmuxbridge`, `internal/zellijbridge`, terminal-facing parts of `internal/gateway/*` | should become one explicit runtime-terminal family |
| retire later | legacy-only runtime surfaces after v2 parity exists | only after control-plane migration is complete |

### Context family

| Action | Packages | Reason |
|--------|----------|--------|
| keep | `internal/contextplane/*` | ACA control plane is a real family |
| keep | `internal/companion/*` | live assembly/runtime memory family |
| keep | `internal/transcriptpipeline/*` | transcript-history pipeline is a real family |
| bridge | `internal/contextplane/taskhistory/*` | straddles control plane and transcript history |
| move later | `internal/sessionkit/*` into context assembly helpers | too small and generic to stay a top-level family |
| move later | `internal/context/updater/*` with `storage/context*` helpers | reads more like context runtime than observability |
| move later | `internal/knowledge/*` plus `internal/storage/knowledge/*` into one knowledge slice | registry + seed split is currently opaque |

### Intelligence family

| Action | Packages | Reason |
|--------|----------|--------|
| keep | `internal/indexing/*` | ingestion/derived-index family is coherent |
| keep | `internal/refactor/*` | planning/evidence/snapshot family is coherent |
| keep | `internal/codemap/*` | synthesis family is coherent enough |
| bridge | `internal/retrieval` | already documented as legacy/transitional |
| bridge | `internal/retrieval/v2`, `searchquery`, `searchindex`, `searchrank`, `repoquery` | one retrieval/search story split across too many roots |
| move later | `internal/codecontext/*` into clearer subpackages | currently query/evidence/render/adapters are bundled too tightly |
| move later | `internal/analysis/*` and `internal/verification/*` into explicit oversight/governance grouping | adjacent to intelligence but not retrieval core |

### Interfaces family

| Action | Packages | Reason |
|--------|----------|--------|
| keep | `internal/web/*`, `internal/chatadapter/*`, `internal/openapi/*` | clean interface-facing families |
| bridge | `internal/gateway/*` | mixes terminal/runtime transport with user-facing interface concerns |
| move later | `internal/console`, `internal/consoleapp` into one console family | should be one interface slice, not two near-duplicate roots |
| move later | `gateway/webterm` and `gateway/sshterm` into runtime terminal support | they are terminal/session transport plumbing more than API-facing gateway logic |

### Tooling family

| Action | Packages | Reason |
|--------|----------|--------|
| keep | `internal/tooling` | generic tool registry/core |
| keep | `internal/agent/tools` | runtime-specific tooling, not generic tooling |
| bridge | `internal/tools/*`, `internal/skillrun`, `internal/adapters/skillslib/*` | all part of one generic tooling story, but split |
| move later | `internal/tools/*` under a `tooling` family | avoid semantic collision with `internal/agent/tools` |
| move later | `internal/skillrun`, `internal/adapters/skillslib/*` under tooling | these are execution/tooling adapters, not interface code |

## Target subfamily map

This is the recommended conceptual map for future moves.

### Runtime

```text
runtime/
  legacy/
  v2/
  terminal/
  orchestration/
  hooks/
  observability/
```

### Context

```text
context/
  controlplane/
  assembly/
  history/
  runtime/
  knowledge/
```

### Intelligence

```text
intelligence/
  ingest/
  search/
  context/
  synthesis/
  planning/
  oversight/
  verification/
```

### Interfaces

```text
interfaces/
  web/
  gateway/
  chat/
  openapi/
  console/
```

### Tooling

```text
tooling/
  registry/
  skills/
  adapters/
  tools/
```

## Migration order

The move order matters more than the final names.

### Phase 1. Freeze the topology

Goal:
- stop introducing new top-level `internal/*` roots without a family justification
- use `docs/architecture/package-topology.md` as the placement policy

Changes:
- update docs only
- align contributor guidance and review comments

Exit criteria:
- package placement discussions use the family model consistently

### Phase 2. Clarify runtime and `v2`

Goal:
- keep `internal/v2/*` explicitly scoped to agent/runtime/orchestration
- mark legacy runtime packages as compatibility paths, not expansion points

Changes:
- docs and package comments
- `doc.go` additions where missing
- small code comments at key legacy/v2 bridges

Exit criteria:
- new runtime work defaults to `internal/v2/*`
- new non-runtime work does not default to `internal/v2/*`

### Phase 3. Consolidate runtime terminal support

Goal:
- make terminal/session plumbing read as one family

Primary candidates:
- `internal/agentpane`
- `internal/tmuxbridge`
- `internal/zellijbridge`
- `internal/gateway/webterm`
- `internal/gateway/sshterm`

Why first:
- small, concrete seam
- low ambiguity
- high readability payoff

Exit criteria:
- terminal/runtime transport is clearly not mixed with generic gateway/API code

### Phase 4. Consolidate context

Goal:
- make the context story explicit instead of spread across unrelated names

Primary candidates:
- `internal/contextplane/taskhistory`
- `internal/transcriptpipeline`
- `internal/storage/transcriptcache`
- `internal/context/updater`
- `internal/storage/contextbuffer`
- `internal/storage/contextvar`
- `internal/storage/conversationsettings`
- `internal/sessionkit`
- `internal/knowledge/*`
- `internal/storage/knowledge/*`

Exit criteria:
- “control plane”, “live assembly”, “history”, and “knowledge” are visibly separate concerns

### Phase 5. Consolidate intelligence

Goal:
- make ingestion, search, evidence extraction, synthesis, and planning read as one
  pipeline with explicit ownership boundaries

Primary candidates:
- `internal/retrieval` and `internal/retrieval/v2`
- `internal/searchquery`
- `internal/searchindex`
- `internal/searchrank`
- `internal/repoquery`
- `internal/codecontext`

Exit criteria:
- one obvious place for new retrieval/query work
- one obvious place for code-evidence extraction

### Phase 6. Consolidate tooling and console

Goal:
- make generic tooling separate from runtime-specific tooling
- make console surfaces one family

Primary candidates:
- `internal/tools/*`
- `internal/skillrun`
- `internal/adapters/skillslib/*`
- `internal/console`
- `internal/consoleapp`

Exit criteria:
- `tooling` and `agent/tools` no longer look like competing top-level namespaces
- `console` and `consoleapp` stop reading like accidental duplication

## First-pass move batches

These are the smallest useful batches to execute first.

### Batch A: docs + package comments only

- add or tighten `doc.go` comments for:
  - `internal/v2`
  - `internal/retrieval`
  - `internal/contextplane`
  - `internal/companion`
  - `internal/tooling`
- make “legacy vs v2” explicit where hybrid bridges exist

### Batch B: runtime terminal support

- create a stable target family name
- move terminal transport packages together
- leave import shims or compatibility aliases only if needed

### Batch C: context history

- treat `taskhistory + transcriptpipeline + transcriptcache` as one migration batch
- do not mix with broader companion or ACA control-plane changes in the same PR

### Batch D: search/retrieval

- collapse the retrieval/search roots behind one family boundary
- keep `indexing/*` as the ingestion builder side

## Non-goals

- Rewriting imports across the entire repo in one pass
- Moving `internal/storage/*` under `v2`
- Moving `internal/contextplane` under `v2`
- Moving `internal/indexing/*` under `v2`
- Renaming packages just to satisfy aesthetics with no family-boundary benefit

## Risks

| Risk | Why it matters | Mitigation |
|------|----------------|------------|
| package churn with little behavioral value | creates merge pain and weakens history | move only one family at a time |
| mixing topology cleanup with runtime migration | makes rollback and review harder | keep runtime parity work separate from naming/placement work |
| overusing `v2` as a generic “new stuff” bucket | recreates the same problem later | keep the `v2` scope explicit in docs and package comments |
| breaking downstream imports too early | slows all active workstreams | do moves behind a documented family plan and with narrow PRs |

## Success criteria

- New packages almost always fit into one of the documented families without debate.
- Reviewers can explain why a package belongs in `runtime`, `context`,
  `intelligence`, `interfaces`, `tooling`, or `foundations`.
- `internal/v2/*` stays readable as the newer agent/runtime/orchestration lane.
- The number of ambiguous top-level roots stops growing.

## Related docs

- [docs/architecture/package-topology.md](../../architecture/package-topology.md)
- [docs/architecture/system-architecture.md](../../architecture/system-architecture.md)
- [docs/general/runtime-orchestration.md](../../general/runtime-orchestration.md)
- [docs/plans/features/eino-go-native-runtime-plan.md](./eino-go-native-runtime-plan.md)
- [docs/plans/retrieval-search-refactor.md](../../plans/retrieval-search-refactor.md)
