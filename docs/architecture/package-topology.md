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
| `internal/context/companion`, `internal/context/contextplane`, `internal/context/transcriptpipeline` | Context/memory/history plane | current context family |
| `internal/intelligence/indexing`, `internal/intelligence/retrieval`, `internal/intelligence/codecontext`, `internal/intelligence/codemap`, `internal/intelligence/refactor` | Retrieval and code intelligence | current intelligence family |
| `internal/web`, `internal/interfaces/gateway`, `internal/interfaces/chatadapter`, `internal/openapi` | Interface and transport layers | current interface family |

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
| `internal/context/companion`, `internal/context/contextplane`, `internal/context/transcriptpipeline` | Context/memory/history plane, not old runtime scaffolding |
| `internal/intelligence/indexing/*`, `internal/intelligence/retrieval`, `internal/intelligence/codecontext`, `internal/intelligence/codemap`, `internal/intelligence/refactor` | Intelligence and retrieval plane |
| `internal/web`, `internal/interfaces/gateway`, `internal/interfaces/chatadapter`, `internal/openapi` | Interface and transport layers |
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

## Current Execution Order

The active migration epic is tracked in
[`docs/plans/features/internal-package-topology-migration.md`](../plans/features/internal-package-topology-migration.md).

Its current execution order is:

1. Freeze topology and clarify the legacy runtime versus `v2` boundary.
2. Consolidate runtime terminal support.
3. Consolidate context family boundaries.
4. Consolidate intelligence and retrieval families.
5. Consolidate tooling and console surfaces.

The first execution slice is intentionally documentation-first:

- align this topology doc with the migration epic
- keep `internal/v2` scoped to the newer agent/runtime/orchestration lane
- only then tighten package comments and contributor guidance for legacy/v2 bridges

## Recommended First Consolidations

These are the lowest-ambiguity cleanups:

| Priority | Current packages | Better family framing |
|------|-------------------|-----------------------|
| 1 | `agentpane`, `tmuxbridge`, `zellijbridge`, terminal-facing parts of `gateway` | runtime/terminal with gateway terminal entrypoints kept under interfaces/gateway |
| 2 | `companion`, `context`, `contextplane`, `sessionkit`, `transcriptpipeline`, `knowledge` | one explicit context family |
| 3 | `indexing`, `retrieval`, `codecontext`, `codemap`, `repoquery`, `refactor`, `search*`, `analysis`, `verification` | one explicit intelligence family |
| 4 | `tools`, `tooling`, `skillrun`, `adapters/skillslib` | one explicit tooling family |

## Runtime-Terminal Inventory

The current runtime-terminal slice already behaves like one family, but the
responsibilities are split across mux backends, pane ownership, and gateway
entrypoints:

| Package | Current responsibility | Most important coupling points |
|------|-------------------------|--------------------------------|
| `internal/runtime/terminal/agentpane` | Owns pane-scoped PTY wrappers, submit-mode rendering, unix-socket delivery, and mux-agnostic watch-pane allocation | Calls `tmuxbridge.CreatePane` and `zellijbridge.CreatePane`; persists transport metadata and submit behavior for room delivery |
| `internal/runtime/terminal/tmuxbridge` | Implements tmux pane/session creation, pane labeling, pane send/submit behavior, and tmux-specific presentation/runtime helpers | Used by `agentpane`; also remains the tmux execution surface that gateway terminal flows ultimately attach to |
| `internal/runtime/terminal/zellijbridge` | Implements zellij pane creation and submit behavior for named panes | Used by `agentpane` as the zellij backend for the same pane-allocation contract |
| `internal/interfaces/gateway/webterm` | Exposes browser terminal access as one shared WebSocket-to-PTY room, backed by `tmux new -A -s <session>` | Registered by `gateway.Server`; starts tmux-backed PTYs per room and multiplexes multiple web clients onto one room session |
| `internal/interfaces/gateway/sshterm` | Exposes SSH terminal access for the same room model, authenticated through Tailscale WhoIs | Registered by `gateway.Server`; resolves room IDs to tmux session names and opens interactive SSH sessions against those tmux rooms |

Shared coupling points in the current slice:

- the room-to-terminal identity is still the tmux session name
- `gateway.Server` registers both `webterm` and `sshterm` from the same room
  metadata and treats them as parallel access paths to the same terminal room
- `webterm` and `sshterm` are interface entrypoints, but both depend on tmux
  session semantics today rather than a mux-neutral runtime-terminal contract
- `agentpane` is already the closest thing to a runtime-terminal owner because
  it normalizes submit behavior and transport delivery across mux backends, but
  it is still separate from the gateway-facing room terminal surfaces

That split is why the next story in this milestone is boundary selection rather
than code movement: the repo needs one explicit runtime-terminal contract before
it can decide what remains interface-only in `gateway` and what should move
toward a shared runtime terminal family.

## Target Runtime-Terminal Contract

The target boundary for this slice is:

- one explicit **runtime-terminal family owner** for terminal session lifecycle,
  pane allocation, submit semantics, and room-to-terminal identity
- gateway terminal packages remain **interface entrypoints**, not terminal
  runtime owners
- mux-specific packages remain backend adapters behind the same runtime-terminal
  contract

That means the target logical split is:

| Concern | Target family owner | Current anchor |
|------|----------------------|----------------|
| pane/session lifecycle, submit behavior, room terminal binding, mux-neutral terminal contract | runtime/terminal | `internal/runtime/terminal/agentpane` plus `internal/runtime/terminal` |
| tmux backend implementation | runtime/terminal backend adapter | `internal/runtime/terminal/tmuxbridge` |
| zellij backend implementation | runtime/terminal backend adapter | `internal/runtime/terminal/zellijbridge` |
| browser-facing terminal transport, websocket framing, HTTP integration | interfaces/gateway | `internal/interfaces/gateway/webterm` |
| SSH-facing terminal transport, Tailscale auth, SSH server integration | interfaces/gateway | `internal/interfaces/gateway/sshterm` |

Explicit ownership rules for the next move batch:

- `agentpane` is the current best anchor for the runtime-terminal contract
  because it already owns pane-socket delivery, submit-mode normalization, and
  mux-agnostic pane allocation
- `tmuxbridge` and `zellijbridge` should be treated as runtime-terminal backend
  adapters, not peer top-level families with their own package-placement rules
- `webterm` and `sshterm` stay in `interfaces/gateway` because they are access
  surfaces into the terminal runtime, not the owner of terminal lifecycle or
  room execution state
- gateway code may keep temporary tmux-specific wiring while the first move
  batch is planned, but that tmux session naming is a compatibility seam, not
  the long-term contract

The compatibility seam for the current repo state is therefore explicit:

- room identity still resolves to a tmux session name in gateway terminal flows
- gateway entrypoints may continue to depend on that mapping while broader
  lifecycle ownership still lives in `agentpane`
- new package moves in this slice should reduce direct gateway ownership of tmux
  semantics rather than expand it

## Recommended First Move Batch

The first low-risk move batch for this family should be **semantic extraction
before package relocation**:

- use the shared runtime-terminal room/session identity seam in
  `internal/runtime/terminal` as the owner of room-to-terminal session naming
  and resolution
- update `internal/interfaces/gateway/webterm` and `internal/interfaces/gateway/sshterm` to depend on
  that shared seam instead of each gateway surface owning tmux session mapping
- keep `webterm` and `sshterm` in `interfaces/gateway` for this batch
- keep `agentpane`, `tmuxbridge`, and `zellijbridge` at their current import
  paths for this batch

This is the lowest-risk first move because it:

- reduces direct gateway ownership of terminal runtime semantics immediately
- avoids a broad import-path rewrite across runtime and API callers
- creates one explicit runtime-owned contract that later package moves can
  follow

For the detailed batch plan, compatibility notes, and review checks, use the
Milestone 2 Story 3 section in
[`docs/plans/features/internal-package-topology-migration.md`](../plans/features/internal-package-topology-migration.md).

## Context Subfamily Map

The current context family is overloaded because it mixes at least five
different concerns:

- control-plane state and promotion logic
- live context assembly and injection
- transcript-derived history extraction
- runtime helpers for session/context plumbing
- durable knowledge registries and sync

The target split for this family is:

| Context concern | Current anchor packages | Current decision |
|------|--------------------------|------------------|
| controlplane | `internal/context/contextplane` | keep as the control-plane anchor |
| assembly | `internal/context/companion` | keep as the live assembly anchor |
| history | `internal/context/transcriptpipeline`, `internal/context/contextplane/taskhistory`, `internal/storage/transcriptcache` | keep as one explicit first migration tranche |
| runtime-helper | `internal/context/sessionkit`, `internal/context/updater`, `internal/storage/contextbuffer`, `internal/storage/contextvar` | keep as helper/bridge packages until history and assembly seams are narrower |
| knowledge | `internal/context/knowledge`, `internal/storage/knowledge` | keep as the durable knowledge slice |

That yields these routing decisions for the current top-level roots:

| Package/root | Subfamily | Keep / bridge / move-later | Why |
|------|-----------|-----------------------------|-----|
| `internal/context/contextplane` | controlplane | keep | Owns ACA-style orientation, proposals, retrieval inspection, promotion helpers, and `taskhistory`; it is already the control-plane home rather than a generic helper bucket |
| `internal/context/companion` | assembly | keep | Owns live prompt/context assembly, conversation memory coordination, and layered context building for active sessions |
| `internal/context/transcriptpipeline` | history | keep | Owns transcript import, preprocessing, claim derivation, and history extraction; it is the main history-processing engine |
| `internal/context/contextplane/taskhistory` | history | bridge inside controlplane today | Lives under `contextplane` today but belongs to the same history tranche as `transcriptpipeline`; treat it as coupled history work, not stray control-plane cleanup |
| `internal/storage/transcriptcache` | history | keep in storage and pair with the history tranche | It is durable transcript-processing cache/state, so it stays under storage while being planned together with history packages |
| `internal/context/sessionkit` | runtime-helper | keep as helper slice | Provides session-oriented utilities, archival, snapshotting, and JSONL helpers that support multiple context/history flows without defining the family boundary on their own |
| `internal/context/updater` | runtime-helper | bridge | Proactively surfaces relevant context at runtime; it depends on context retrieval/assembly concerns but should not define the control-plane or history boundary |
| `internal/storage/contextbuffer` | runtime-helper | keep in storage and classify with helper slice | It is a local queue/store for context injection, so it stays storage-owned while routing with runtime helpers |
| `internal/storage/contextvar` | runtime-helper | keep in storage and classify with helper slice | It is the durable RLM context-variable store, not a separate top-level context family |
| `internal/context/knowledge` | knowledge | keep | Represents the durable knowledge-plane logic rather than runtime assembly or history processing |
| `internal/storage/knowledge` | knowledge | keep in storage and pair with knowledge slice | It is the persistence layer for knowledge packs/registry state and should move only with explicit knowledge work |

Explicit placement rules for this family:

- do not route new context work into `internal/v2`; the context family is a
  peer family, not legacy runtime scaffolding
- treat `transcriptpipeline`, `taskhistory`, and `storage/transcriptcache` as
  one planned history tranche even though they cross package roots today
- keep storage-backed context helpers in `internal/storage/*`; classify them
  with the context subfamily they serve rather than promoting them into new
  top-level roots
- use `internal/context/companion` for live assembly concerns and `internal/context/contextplane`
  for control-plane concerns unless a narrower subfamily has already been
  carved out

The next story in this milestone should therefore focus on the **history**
tranche, because it is the cleanest context slice that already has clear
runtime-processing and storage boundaries.

## History Tranche Boundary

Within the context family, the first explicit migration tranche should be the
**history** slice:

| Package/root | History role | Current boundary decision |
|------|--------------|---------------------------|
| `internal/context/transcriptpipeline` | transcript import, preprocessing, claim derivation, objective extraction, grouped history runs, and `HistoryPack` production | treat as the history-processing owner |
| `internal/context/contextplane/taskhistory` | control-plane consumer that assembles task-oriented history views, family overviews, repo anchors, and ACA-facing summaries from transcript/history inputs | keep under `contextplane` for now, but treat as a history consumer rather than a second history-processing engine |
| `internal/storage/transcriptcache` | durable cache for prederived transcript artifacts | keep in `internal/storage/*` and plan with the history tranche |

That means the practical boundary for new work is:

- transcript parsing, normalization, objective extraction, claim derivation,
  grouped history runs, and cache semantics belong to `internal/context/transcriptpipeline`
  plus `internal/storage/transcriptcache`
- task-oriented history packaging, control-plane summaries, and ACA/task-facing
  presentation belong to `internal/context/contextplane/taskhistory`
- new history work should not drift into `internal/context/sessionkit` or generic
  `internal/context/contextplane` helpers unless it is clearly a helper or consumer of
  the existing history outputs

## Recommended First History Batch

The first low-risk history batch should be **boundary tightening without path
renames**:

- preserve `internal/context/transcriptpipeline` as the producer of transcript-derived
  history artifacts and packs
- preserve `internal/context/contextplane/taskhistory` as the control-plane consumer of
  those history outputs
- preserve `internal/storage/transcriptcache` as the durable transcript artifact
  cache
- remove any future ambiguity about ownership by routing new transcript-derived
  logic into `transcriptpipeline` and new control-plane presentation logic into
  `taskhistory`

The intent of this first batch is to stop history logic from spreading across
nearby context roots before any package relocation is attempted.

Explicit non-goals for the first history batch:

- do not rename `taskhistory`, `transcriptpipeline`, or `transcriptcache`
  packages yet
- do not rework ACA promotion semantics or knowledge-plane behavior
- do not fold session helper packages into the history tranche prematurely
- do not change transcript artifact formats or cache semantics as part of the
  boundary clarification step

## Runtime-Helper And Knowledge Placement

The remaining context ambiguity is mostly in two small groups:

- runtime-helper packages that support session/context flows
- durable knowledge packages that own registries, sync, and embedded knowledge

The routing rule is:

| Package/root | Slice | Placement rule |
|------|-------|----------------|
| `internal/context/sessionkit` | runtime-helper | keep as the shared helper root for session store-opening, path resolution, archive/snapshot helpers, and transcript JSONL utilities |
| `internal/context/updater` | runtime-helper | keep as a runtime helper that analyzes active conversations and injects relevant context; it is not the control-plane anchor or the durable knowledge plane |
| `internal/storage/contextbuffer` | runtime-helper | keep in `internal/storage/*` as helper-owned injection buffering |
| `internal/storage/contextvar` | runtime-helper | keep in `internal/storage/*` as helper-owned context-variable persistence |
| `internal/context/knowledge` | knowledge | keep as the durable knowledge-plane logic and embedded knowledge root |
| `internal/storage/knowledge` | knowledge | keep in `internal/storage/*` as durable knowledge persistence and sync state |

Practical placement rules:

- if the package exists to open stores, resolve session paths, snapshot/archive
  sessions, inject context, or hold short-lived context state, it belongs in
  the runtime-helper slice
- if the package exists to register, sync, trigger, or persist reusable
  knowledge packs and documents, it belongs in the knowledge slice
- runtime-helper packages may support the context family, but they should not
  become the default home for history or control-plane logic
- knowledge packages are runtime-neutral and should not absorb session/runtime
  helpers just because they are context-adjacent

This keeps the remaining helper roots useful without turning them into
catch-all family anchors.

## Intelligence Subfamily Map

The intelligence family is currently spread across builders, query planners,
retrieval engines, code-evidence gatherers, synthesis agents, oversight logic,
and verification passes. Treating all of that as one undifferentiated
`retrieval` bucket is what makes placement drift likely.

The target split for this family is:

| Intelligence concern | Current anchors | Current decision |
|------|-----------------|------------------|
| ingest/builders | `internal/intelligence/indexing`, `internal/intelligence/searchindex` | keep as the builder and persisted-index slice |
| search/query/recall | `internal/intelligence/retrieval`, `internal/intelligence/retrieval/v2`, `internal/intelligence/repoquery`, `internal/intelligence/searchquery`, `internal/intelligence/searchrank` | keep as the query and recall slice |
| evidence gathering | `internal/intelligence/codecontext`, `internal/intelligence/codemap/context` | keep as the code-evidence extraction slice |
| synthesis and refactor planning | `internal/intelligence/codemap`, `internal/intelligence/refactor`, `internal/intelligence/analysis/tasksgraph` | keep as the synthesis/planning slice |
| oversight | `internal/intelligence/analysis/overseer` | keep as the review and prioritization oversight slice |
| verification | `internal/intelligence/verification` | keep as the verification slice |

That yields these routing decisions:

| Package/root | Subfamily | Keep / bridge / move-later | Why |
|------|-----------|-----------------------------|-----|
| `internal/intelligence/indexing` | ingest/builders | keep | Coordinates post-review indexing pipelines and embedding/index maintenance rather than end-user retrieval |
| `internal/intelligence/searchindex` | ingest/builders | keep | Defines the persisted retrieval-document model and recall store contract that retrieval engines consume |
| `internal/intelligence/retrieval` | search/query/recall | bridge | Holds remaining non-v2 retrieval helpers; new code-search entrypoints should favor `internal/intelligence/retrieval/v2` |
| `internal/intelligence/retrieval/v2` | search/query/recall | keep | It is the main fused search and recall engine |
| `internal/intelligence/repoquery` | search/query/recall | keep | Provides typed repo-index query, expand, open, and DAG-grep requests for structural recall |
| `internal/intelligence/searchquery` | search/query/recall | keep | Owns parsed lexical query plans and path/identifier extraction |
| `internal/intelligence/searchrank` | search/query/recall | keep | Owns cross-source ranking and fusion logic |
| `internal/intelligence/codecontext` | evidence gathering | keep | Owns snippet extraction and the shared code-context funnel used by semantic/code search skills |
| `internal/intelligence/codemap/context` | evidence gathering | bridge inside codemap today | Gathers rich code evidence for codemap generation and should be treated as evidence collection, not as a second synthesis owner |
| `internal/intelligence/codemap` | synthesis and refactor planning | keep | Produces semantic codemaps and LLM-backed synthesized traces from gathered evidence |
| `internal/intelligence/refactor` | synthesis and refactor planning | keep | Owns change analysis, hotspot evidence, dependency analysis, and refactor-oriented planning artifacts |
| `internal/intelligence/analysis/tasksgraph` | synthesis and refactor planning | keep | Computes graph structure and critical-path style signals that support planning decisions |
| `internal/intelligence/analysis/overseer` | oversight | keep | Scores tasks, handles post-review coordination, and fans out indexing work |
| `internal/intelligence/verification` | verification | keep | Implements claim-checking and verification-specific pipelines |

Practical placement rules for this family:

- new indexing or persisted recall-document work belongs with
  `internal/intelligence/indexing` or `internal/intelligence/searchindex`, not under generic retrieval
- new query parsing, repo-index recall, lexical/vector fusion, and grouped
  search behavior belongs in the search/query/recall slice
- new code-evidence extraction belongs in `internal/intelligence/codecontext` or other
  evidence gatherers, not directly in synthesis packages
- synthesized codemaps, refactor plans, and planning artifacts belong in the
  synthesis/planning slice after evidence has already been gathered
- review fanout and task-scoring coordination belong in oversight, and claim
  checking belongs in verification

This is the stable vocabulary the next two intelligence stories should use.

## Retrieval-Search Consolidation Tranche

Inside the intelligence family, the highest-ambiguity consolidation is the
retrieval/query slice. The durable boundary should be:

| Package/root | Retrieval-search role | Current boundary decision |
|------|------------------------|---------------------------|
| `internal/intelligence/retrieval/v2` | main retrieval engine, source orchestration, grouped search, lexical/vector/repo-index fusion | treat as the main retrieval-search owner |
| `internal/intelligence/retrieval` | legacy helpers such as semantic tree building and file summary generation | keep as a bridge until those helpers are either retired or rehomed intentionally |
| `internal/intelligence/repoquery` | typed repo-index search, expand, open, and DAG requests | keep in the retrieval-search slice as structural recall support |
| `internal/intelligence/searchquery` | parsed lexical query plans, identifiers, phrases, and path hints | keep in the retrieval-search slice as query planning |
| `internal/intelligence/searchrank` | cross-source fusion and ranking | keep in the retrieval-search slice as ranking/fusion |
| `internal/intelligence/searchindex` | persisted retrieval-document model and recall store contract | keep on the builder side as shared index infrastructure, not as the retrieval owner |
| `internal/intelligence/indexing` | post-review indexing pipelines and embedding/index maintenance | keep on the builder side |

That yields one explicit rule:

- **retrieval-search owns queries and recall**
- **indexing/searchindex own building and storing the recall substrate**

## Recommended First Retrieval-Search Batch

The first low-risk consolidation batch should be **boundary clarification before
package relocation**:

- treat `internal/intelligence/retrieval/v2` as the default home for new retrieval/query
  entrypoints
- treat `internal/intelligence/retrieval` as transitional bridge code for legacy tree and
  file-summary helpers only
- keep `searchquery`, `searchrank`, and `repoquery` grouped with retrieval
  rather than treating them as unrelated parallel families
- keep `searchindex` and `indexing` on the builder side so recall-substrate work
  does not get mixed into query behavior changes

Explicit non-goals for this first batch:

- do not merge indexing pipelines into retrieval-query packages
- do not rename `searchindex` into retrieval just because retrieval uses it
- do not pull code-evidence extraction (`codecontext`, `codemap/context`) into
  the same tranche
- do not remove `internal/intelligence/retrieval` helpers until the replacement boundary is
  explicitly complete

This keeps the first retrieval-search consolidation narrow enough to guide new
work without forcing a broad intelligence rewrite.

## Code-Evidence Extraction Boundary

The remaining intelligence ambiguity is mostly between:

- packages that gather code evidence
- packages that synthesize or plan from that evidence
- packages that coordinate or verify downstream decisions

The durable split should be:

| Package/root | Role | Current boundary decision |
|------|------|---------------------------|
| `internal/intelligence/codecontext` | shared code-evidence extraction and snippet collection | treat as the main evidence-gathering owner |
| `internal/intelligence/codemap/context` | codemap-specific evidence gathering from graph, symbols, and search | keep as evidence-gathering support inside codemap |
| `internal/intelligence/codemap` | synthesized semantic maps and trace generation | keep in the synthesis slice |
| `internal/intelligence/refactor` | refactor-oriented evidence consumers, hotspot analysis, and change planning | keep in the planning/synthesis slice |
| `internal/intelligence/analysis/tasksgraph` | graph-derived task analysis that supports prioritization and planning | keep in the planning/oversight support slice |
| `internal/intelligence/analysis/overseer` | post-review coordination, task scoring, and indexer fanout | keep in oversight |
| `internal/intelligence/verification` | claim extraction and claim verification | keep in verification |

Practical placement rules:

- new snippet extraction, file evidence collection, and query-to-snippet logic
  belong in evidence-gathering packages
- synthesized codemaps, hotspot packs, and planning artifacts belong in
  synthesis/planning packages after evidence has already been gathered
- task scoring, post-review fanout, and coordination logic belong in oversight
- claim checking stays in verification even when it consumes the same evidence

Explicit non-goals for this boundary:

- do not treat `codemap` itself as the default home for raw snippet extraction
- do not fold verification into oversight just because both inspect outputs
- do not move task-scoring analysis into retrieval or evidence-gathering
  packages

This gives future code-intelligence work one obvious distinction:

- **evidence gathering** answers "what code and signals should we look at?"
- **synthesis/planning** answers "what does that evidence mean?"
- **oversight/verification** answers "what should we coordinate or check next?"

## Generic Tooling Family

The tooling family should distinguish between:

- generic tooling substrate that any skill or workflow can reuse
- runtime-facing agent tools that exist specifically to serve the agent runtime

The durable split should be:

| Package/root | Tooling role | Current boundary decision |
|------|--------------|---------------------------|
| `internal/tooling` | generic in-memory tool registry and callable tool substrate | keep as generic tooling |
| `internal/skillrun` | generic skill resolution, execution, and envelope decoding | keep as generic tooling |
| `internal/tools/*` | standalone generic tools such as obsidian and ripgrep integrations | keep as generic tooling |
| `internal/adapters/skillslib/*` | reusable helpers for building and testing skills | keep as generic tooling support |
| `internal/agent/tools` | runtime-facing tool implementations that wrap agentctl skills for agent sessions | keep separate as agent-runtime tooling |

That yields one explicit placement rule:

- if the package provides reusable tooling substrate, standalone tools, or
  helper libraries for many skills/workflows, it belongs in the generic tooling
  family
- if the package exists to expose tools specifically inside the agent runtime,
  it belongs with runtime-facing agent tooling rather than the generic tooling
  family

Practical non-goals for this split:

- do not collapse `internal/agent/tools` into generic tooling just because it
  wraps skills
- do not treat every package with “tool” in the name as the same family
- do not mix console-family decisions into the generic tooling contract yet

This gives the tooling milestone its first stable distinction before the
console boundary is handled separately.

## Console Family Boundary

The console-related packages split into two different concerns:

- low-level console session utilities
- the application/runtime surface that runs and streams console sessions

The durable split should be:

| Package/root | Console role | Current boundary decision |
|------|--------------|---------------------------|
| `internal/console` | console session contract, lifecycle, and persistence primitives such as correlation tracking for interactive actor console sessions | keep as the canonical console ownership slice |
| `internal/consoleapp` | console application runtime, LLM runner, stream parsing, and session-turn execution | keep as the console application slice |
| `internal/web/consolews` | websocket transport surface for console sessions | treat as the web-facing console transport entrypoint, not as the utility owner |

That yields one explicit rule:

- `internal/console` owns reusable console-session primitives and persistence
  contracts
- `internal/consoleapp` owns application/runtime behavior for console sessions
- `internal/web/consolews` remains the interface/transport layer that hosts the
  console app over web sockets

Current implemented seam:

- `internal/console` now owns the console session transcript/config model and
  internal subscriber event model, in addition to correlation tracking
- `internal/console` also owns the console session lifecycle and persistence
  contract instead of leaving sessions-store wiring under `consolews`
- `internal/consoleapp` consumes the narrow session handle/runtime contract and
  remains the owner of turn execution, tool-event emission, and stream parsing
- `internal/web/consolews` now acts as the websocket host and wire adapter,
  rather than the owner of the session/transcript model
- `internal/domain/console` is the canonical console payload contract; the
  websocket layer no longer carries a separate payload schema

## Implemented First Console Batch

The first low-risk console batch is now an **implemented boundary cut without
renames**:

- move console session transcript/config primitives into `internal/console`
- keep `internal/consoleapp` as the runner/streaming application layer
- keep `internal/web/consolews` as the websocket transport host
- collapse the duplicate websocket payload wrapper onto the canonical
  `internal/domain/console` payload contract

Explicit non-goals for this first batch:

- do not merge `console` into `consoleapp`
- do not move websocket hosting into `internal/console`
- do not redesign console UX or LLM streaming behavior as part of topology work

This is enough to make future console work land in one obvious place without
starting a repo-wide rename, while still leaving any deeper websocket/runtime
rework for later.

## Recommended First Tooling Batch

The first low-risk tooling batch should be **package-comment and
documentation-only guardrails**:

- clarify generic tooling ownership in `internal/tooling`,
  `internal/skillrun`, `internal/tools/*`, and `internal/adapters/skillslib/*`
- clarify that `internal/agent/tools` stays runtime-facing rather than generic
  tooling
- clarify that `internal/console` is the narrow utility layer and
  `internal/consoleapp` is the application/runtime layer

Why this is the right first batch:

- it proves the tooling-family split without forcing a package rename
- it reduces future placement drift immediately in the code itself
- it keeps tool execution semantics and console UX out of scope

This batch is intentionally small: package comments and topology guidance first,
then any later package moves can follow the now-explicit family contract.

## Related Docs

- [system-architecture.md](./system-architecture.md)
- [go-native-runtime-and-optional-jido.md](./go-native-runtime-and-optional-jido.md)
- [jido-hybrid-runtime.md](./jido-hybrid-runtime.md)
- [../general/runtime-orchestration.md](../general/runtime-orchestration.md)
- [../plans/features/internal-package-topology-migration.md](../plans/features/internal-package-topology-migration.md)
