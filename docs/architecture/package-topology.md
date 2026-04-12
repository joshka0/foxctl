# Package Topology

This page is the canonical package-grouping map for `internal/*`.

It does two things:

1. Defines the **target grouping shape** for the repo so `internal/` stops growing as a flat namespace.
2. Makes explicit which packages are **legacy agent/runtime paths** and which `internal/v2/*` packages are intended to replace them.

## Metadata

| Field | Value |
|------|-------|
| Status | Current guidance |
| Canonical scope | Package grouping and migration boundaries for `internal/*` |
| Last reviewed | 2026-04-13 |

## Why This Exists

`internal/` is currently grouped by several competing ideas at once:

- runtime generation: `agent`, `v2`, `daemon`
- technical layer: `storage`, `platform`, `domain`, `protocol`
- feature area: `companion`, `contextplane`, `transcriptpipeline`
- interface/transport: `web`, `gateway`, `chatadapter`, `openapi`
- utilities and local tooling: `tools`, `tooling`, `skillrun`

That mix makes package placement feel inconsistent even when individual packages are reasonable.

## Current Rule

New top-level `internal/*` roots should be rare.

Prefer adding new packages under one of these stable families:

| Family | Purpose | Current anchors |
|------|---------|-----------------|
| `internal/domain` | Core domain contracts and value types | `domain/agent`, `domain/envelope`, `domain/policy` |
| `internal/platform` | Cross-cutting platform/config/runtime utilities | `platform/config`, `platform/workspace`, `platform/timeutil` |
| `internal/protocol` | Wire/envelope/protocol helpers | `protocol` |
| `internal/storage` | Durable state, CAS, local rebuildable stores, DB helpers | `storage/*` |
| `internal/v2` | Newer **agent/runtime/orchestration** stack only | `v2/core`, `v2/services`, `v2/runtime`, `v2/adapters` |
| `internal/companion`, `internal/contextplane`, `internal/transcriptpipeline` | Context/memory/history plane | current context family |
| `internal/indexing`, `internal/retrieval`, `internal/codecontext`, `internal/codemap`, `internal/refactor` | Retrieval and code intelligence | current intelligence family |
| `internal/web`, `internal/gateway`, `internal/chatadapter`, `internal/openapi` | Interface and transport layers | current interface family |

## Target Shape

This is the intended logical grouping. It is not a command to rename everything immediately.

```text
internal/
  core/
    domain/
    platform/
    protocol/

  runtime/
    legacy/
    orchestration/
    hooks/
    observability/
    terminal/

  context/
    memory/
    assembly/
    history/
    knowledge/

  intelligence/
    indexing/
    retrieval/
    codecontext/
    codemap/
    refactor/
    search/
    analysis/
    verification/

  interfaces/
    web/
    gateway/
    chat/
    openapi/
    console/

  tooling/
    skillrun/
    tools/
    adapters/

  storage/

  v2/
    core/
    services/
    runtime/
    adapters/
```

## Important Constraint

`internal/v2` is **not** the future home for all new code.

`v2` is specifically the newer **agent/runtime/orchestration** lane:

- typed commands
- run/ask/spawn/list/kill services
- event-sourced orchestration
- runner/supervisor/contextbuilder runtime components
- runtime adapters such as Jido and libsql-backed v2 stores

It should not become a generic replacement namespace for:

- context and memory packages
- retrieval/indexing packages
- general storage packages
- web or transport packages

## Legacy vs V2 Replacement Map

This is the explicit map for what `v2` is replacing.

| Legacy/current package(s) | Current role | V2 replacement or target | Status |
|------|-------------|--------------------------|--------|
| `internal/agent/runtime` | Mailbox-driven agent sessions, overseer hierarchy, tool wiring | `internal/v2/runtime/*` plus `internal/v2/services/*` | Active replacement in progress |
| `internal/agent/daemon` | Classic foreground daemon loop and mailbox polling runtime | `internal/v2/runtime/{runner,orchestration,supervisor}` and service entrypoints | Partial replacement |
| `internal/execution/agentmanager` | Legacy spawn/kill and runtime management fallback | `internal/v2/services/{spawn,kill,list,run}` | Partial replacement |
| `internal/daemon` agent-management paths | Mixed legacy runtime control plus newer helper wiring | Prefer `internal/v2/services/*` for command semantics; keep `internal/daemon` as hosting shell | Mixed transitional |
| `internal/agent/tools` agent-management tools such as spawn/list/status/wait | Runtime-facing tools for the classic agent stack | Should increasingly delegate to `internal/v2/services` or Go-owned runtime state | Partial replacement |
| `internal/v2/adapters/jido` dependency on live runtime state | Runtime transport and state bridge for some orchestration paths | Long-term target is Go-owned runtime state with Jido optional | Adapter retained, default role should shrink |

## What V2 Is Not Replacing

These package families are peer families, not “legacy” in the same sense:

| Family | Reason |
|------|--------|
| `internal/storage/*` | Shared persistence layer used by both legacy and v2 paths |
| `internal/companion`, `internal/contextplane`, `internal/transcriptpipeline` | Context/memory/history plane, not old runtime scaffolding |
| `internal/indexing/*`, `internal/retrieval`, `internal/codecontext`, `internal/codemap`, `internal/refactor` | Intelligence and retrieval plane |
| `internal/web`, `internal/gateway`, `internal/chatadapter`, `internal/openapi` | Interface and transport layers |
| `internal/domain`, `internal/platform`, `internal/protocol` | Foundations, not generation-specific runtime code |

## Practical Grouping Guidance

If a package is mostly about:

- agent execution, orchestration commands, worker lifecycle, runtime-tree state:
  it belongs either in legacy runtime packages or `internal/v2/*`
- memory, context assembly, transcript-derived continuity:
  it belongs in the context family
- semantic search, repo graph, code retrieval, codemaps:
  it belongs in the intelligence family
- APIs, sockets, chat platforms, browser or terminal surfaces:
  it belongs in the interface family
- durable records, CAS, queues, projections, local DBs:
  it belongs in `internal/storage/*`

## Migration Policy

Do not do a single large package rename across the whole repo.

Use this order:

1. Keep `internal/v2` explicitly scoped to agent/runtime/orchestration work.
2. Stop adding new top-level roots unless they clearly represent a stable family.
3. Move packages only when already touching that area for real work.
4. Consolidate one family at a time.
5. Prefer documenting the target family first, then moving code incrementally.

## Recommended First Consolidations

These are the lowest-ambiguity cleanups:

| Priority | Current packages | Better family framing |
|------|-------------------|-----------------------|
| 1 | `agentpane`, `tmuxbridge`, `zellijbridge`, terminal-facing parts of `gateway` | runtime/terminal or interfaces/gateway |
| 2 | `companion`, `context`, `contextplane`, `sessionkit`, `transcriptpipeline`, `knowledge` | one explicit context family |
| 3 | `indexing`, `retrieval`, `codecontext`, `codemap`, `repoquery`, `refactor`, `search*`, `analysis`, `verification` | one explicit intelligence family |
| 4 | `tools`, `tooling`, `skillrun`, `adapters/skillslib` | one explicit tooling family |

## Related Docs

- [system-architecture.md](./system-architecture.md)
- [go-native-runtime-and-optional-jido.md](./go-native-runtime-and-optional-jido.md)
- [jido-hybrid-runtime.md](./jido-hybrid-runtime.md)
- [../general/runtime-orchestration.md](../general/runtime-orchestration.md)
- [../plans/features/internal-package-topology-migration.md](../plans/features/internal-package-topology-migration.md)
