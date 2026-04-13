# Internal Package Topology Migration Epic

| Field | Value |
|-------|--------|
| Status | Active epic |
| Canonical architecture | [docs/architecture/package-topology.md](../../architecture/package-topology.md) |
| Agile room | `internal-topology` |
| Agile epic id | `01KP1YZYJBE8ACH9DEQFD7R1C6` |
| Work-pack mirror | `~/.agentctl/epics/01KP1YZYJBE8ACH9DEQFD7R1C6/` |
| Related runtime docs | [docs/architecture/system-architecture.md](../../architecture/system-architecture.md), [docs/general/runtime-orchestration.md](../../general/runtime-orchestration.md) |

## Goal

Reshape `internal/*` into explicit package families so new code has obvious
placement rules, `internal/v2/*` stays narrowly scoped to the newer
agent/runtime/orchestration lane, and family consolidation proceeds through
small migration tranches instead of a repo-wide rename.

## Desired Outcome

By the end of this epic:

- package placement discussions start from a stable family model instead of local
  preference
- `internal/v2/*` remains a runtime-generation boundary rather than a generic
  "future code" bucket
- the repo has a documented and reviewable migration order for runtime terminal,
  context, intelligence, and tooling families
- the first execution slices for each family are small enough to land in narrow
  follow-up PRs

## Core Rules

1. `internal/v2/*` stays explicitly scoped to the newer
   agent/runtime/orchestration lane.
2. `internal/v2/*` is not the replacement namespace for context, retrieval,
   storage, or interface code.
3. `internal/storage/*`, `internal/domain/*`, `internal/platform/*`, and
   `internal/protocol` remain stable foundation families.
4. Moves happen when touching a family for real work, not as repo-wide churn.
5. This epic is about package boundaries and migration sequencing, not hidden
   runtime rewrites.

## Family Model

The current repo already wants to be organized around these families:

| Family | Current anchors | Meaning |
|--------|------------------|---------|
| runtime | `agent/*`, `execution/*`, `runservice`, `daemon`, `v2/*` | agent execution, orchestration, lifecycle, runtime state |
| context | `companion`, `context/*`, `contextplane/*`, `sessionkit`, `transcriptpipeline`, `knowledge/*` | ACA/control-plane, context assembly, transcript history, durable knowledge |
| intelligence | `indexing/*`, `retrieval*`, `repoquery`, `search*`, `codecontext/*`, `codemap/*`, `refactor/*`, `analysis/*`, `verification/*` | ingestion, retrieval, evidence extraction, synthesis, refactor planning, verification |
| interfaces | `web/*`, `gateway/*`, `chatadapter/*`, `openapi/*`, `console`, `consoleapp` | server, UI, transport, platform-facing entrypoints |
| tooling | `tooling`, `tools/*`, `skillrun`, `adapters/skillslib/*` | generic tool registry, skill execution, reusable tooling adapters |
| foundations | `storage/*`, `domain/*`, `platform/*`, `protocol` | shared persistence, contracts, platform helpers, wire helpers |

## What `v2` Is Replacing

`internal/v2/*` is the newer runtime lane, not a repo-wide v2 of everything.

| Legacy/current package(s) | V2 replacement target | Notes |
|---------------------------|-----------------------|-------|
| `internal/agent/runtime` | `internal/v2/runtime/*` plus `internal/v2/services/*` | core agent session/runtime replacement seam |
| `internal/agent/daemon` | `internal/v2/runtime/{runner,orchestration,supervisor}` | partial replacement today |
| `internal/execution/agentmanager` | `internal/v2/services/{spawn,kill,list,run}` | fallback path remains in places |
| agent-management paths in `internal/daemon` | prefer `internal/v2/services/*` semantics | keep `internal/daemon` as hosting shell where needed |
| some runtime-facing management in `internal/agent/tools` | should increasingly delegate to v2 services or Go-owned runtime state | transitional |
| `internal/v2/adapters/jido` live runtime dependence | Go-owned runtime state with Jido optional | adapter should shrink in the default path |

These families are explicit non-targets for the `v2` migration:

- `internal/storage/*`
- `internal/companion`, `internal/contextplane`, `internal/transcriptpipeline`,
  `internal/knowledge`
- `internal/indexing/*`, `internal/retrieval*`, `internal/codecontext`,
  `internal/codemap`, `internal/refactor`
- `internal/web`, `internal/gateway`, `internal/chatadapter`, `internal/openapi`
- `internal/domain`, `internal/platform`, `internal/protocol`

## As-Built `v2` Usage Today

The repo already uses `internal/v2/*` in a focused way:

| Surface | Current usage |
|--------|----------------|
| CLI ask flow | `cmd/agentctl/cmd/agent.go` builds `v2services.NewAskService(...)` |
| orchestration/spawn/list/kill | `cmd/agentctl/cmd/orchestration.go`, `cmd/agentctl/cmd/overseer_v2_orchestration.go`, and `internal/web/api/orchestration.go` construct v2 services |
| companion context assembly | `internal/companion/service.go` and `internal/companion/v2_context_adapter.go` wire `internal/v2/runtime/contextbuilder` |
| optional runtime backend bridge | `internal/agent/runtime/runtime.go` gates Eino through `internal/v2/adapters/eino` |
| newer retrieval lane | `internal/retrieval/doc.go` already points new work to `internal/retrieval/v2` |

This is why the topology doc must keep saying "`v2` is the newer
agent/runtime/orchestration lane" instead of implying repo-wide succession.

## Milestone Plan

The epic is intentionally decomposed into five active milestones plus one final
verification proposal. The milestones below match the durable room/work-pack
state.

### Milestone 1. Freeze topology and clarify the legacy runtime versus v2 boundary

Objective:

- freeze the package-family vocabulary
- keep `internal/v2` narrowly scoped to the agent/runtime/orchestration lane
- make the first tranche's sequencing explicit before package moves accelerate

In scope:

- align the topology architecture doc and this migration epic
- refresh package comments on key legacy/v2 bridge packages
- wire contributor-facing guidance to the package-family policy

Proposed stories:

1. Align the canonical topology doc and migration plan.
2. Add package-comment guardrails for legacy and v2 bridges.
3. Wire contributor guidance to the topology policy.

Exit criteria:

- `docs/architecture/package-topology.md` explicitly scopes `internal/v2` to
  agent/runtime/orchestration rather than a generic future namespace
- this migration epic names the first execution tranche as milestone/story work
  instead of ad hoc cleanup
- contributors have one explicit placement rule for new `internal/*` work

### Milestone 2. Consolidate runtime terminal support

Objective:

- pull terminal/session transport packages into one explicit runtime-terminal
  story so `agentpane`, `tmuxbridge`, `zellijbridge`, and gateway terminal
  plumbing stop looking like unrelated roots

Primary packages:

- `internal/agentpane`
- `internal/tmuxbridge`
- `internal/zellijbridge`
- `internal/gateway/webterm`
- `internal/gateway/sshterm`

Proposed stories:

1. Inventory the runtime-terminal slice.
2. Define the target runtime-terminal family contract.
3. Plan the first narrow terminal move batch.

Exit criteria:

- the runtime-terminal slice is documented as one family with explicit coupling
  points
- one target family boundary is chosen for terminal/session support
- one low-risk first move batch exists with compatibility and review notes

Current inventory:

| Package | Current role in the slice | Notes |
|--------|----------------------------|-------|
| `internal/agentpane` | pane wrapper and transport owner | normalizes submit modes, owns pane socket delivery, allocates tmux/zellij watch panes |
| `internal/tmuxbridge` | tmux backend | creates panes/sessions and provides tmux-specific send/submit behavior |
| `internal/zellijbridge` | zellij backend | provides the parallel pane creation and submit contract for zellij |
| `internal/gateway/webterm` | browser terminal entrypoint | one shared tmux-backed PTY per room, fanout to multiple WebSocket clients |
| `internal/gateway/sshterm` | SSH terminal entrypoint | Tailscale-authenticated SSH access, resolves room IDs to tmux sessions |

Coupling points captured by Story 1:

- `agentpane` already depends on `tmuxbridge` and `zellijbridge` as backend
  implementations of one pane-allocation contract
- `gateway.Server` registers `webterm` and `sshterm` from the same room/tmux
  metadata, so they are two interfaces onto one terminal-room concept
- both gateway surfaces still anchor on tmux session naming today, which is why
  the next story must choose the runtime-terminal family boundary before any
  move batch is planned

Chosen target contract for Story 2:

- the repo will treat **runtime-terminal support** as one explicit family whose
  owner is terminal session lifecycle, pane allocation, submit behavior, and
  room-to-terminal identity
- `internal/agentpane` is the current anchor for that contract because it
  already normalizes pane delivery and mux-agnostic submit behavior
- `internal/tmuxbridge` and `internal/zellijbridge` are backend adapters within
  the same family boundary
- `internal/gateway/webterm` and `internal/gateway/sshterm` remain
  `interfaces/gateway` entrypoints because they expose terminal access over web
  and SSH, but they do not own the terminal runtime contract

What stays in gateway versus what moves under runtime-terminal support:

| Concern | Stays in gateway | Belongs to runtime-terminal support |
|--------|-------------------|-------------------------------------|
| HTTP/WebSocket/SSH protocol handling | yes | no |
| Tailscale identity checks and browser transport framing | yes | no |
| room terminal lifecycle and pane/session ownership | no | yes |
| submit semantics and pane-socket delivery behavior | no | yes |
| mux backend implementation details | no | yes |

Temporary compatibility seam:

- gateway terminal entrypoints may continue to resolve room IDs to tmux session
  names during the first move batch
- that tmux mapping is treated as transitional compatibility, not as proof that
  gateway owns the long-term terminal runtime boundary
- the next move batch should reduce direct gateway dependence on tmux semantics
  instead of spreading them further

Planned first narrow move batch for Story 3:

- create one shared runtime-terminal room/session identity seam under the
  runtime-terminal family boundary
- move room-to-tmux session naming and resolution behind that seam first
- keep `internal/gateway/webterm` and `internal/gateway/sshterm` as interface
  entrypoints that call the shared seam
- defer any package-path relocation of `agentpane`, `tmuxbridge`, and
  `zellijbridge` until after the shared runtime-owned seam exists

Why this is the lowest-risk batch:

- `webterm` and `sshterm` currently encode the same terminal-room identity idea
  from two interface entrypoints, so extracting that seam reduces duplicated
  runtime ownership without changing transport behavior
- `agentpane` already imports `tmuxbridge` and `zellijbridge`, and other runtime
  callers such as `internal/agent/runtime/*` and `internal/web/api/mux.go`
  already depend on `agentpane`; a package move there would create wider import
  fallout than the seam-extraction batch
- gateway terminal behavior can stay stable while the ownership boundary becomes
  explicit

Batch scope:

| In batch 1 | Deferred after batch 1 |
|-----------|-------------------------|
| shared room/session identity helper owned by runtime-terminal support | package renames for `agentpane`, `tmuxbridge`, or `zellijbridge` |
| `gateway/webterm` switched to use the shared identity seam | gateway package relocation |
| `gateway/sshterm` switched to use the shared identity seam | mux backend relocation |
| documentation and package comments updated to reflect the new seam | broader API or console cleanup |

Expected import fallout:

- `internal/gateway/webterm` and `internal/gateway/sshterm` gain one import on
  the shared runtime-terminal seam
- direct tmux session-name construction should be removed from gateway-local
  code paths
- no import-path changes should be required for `internal/agentpane`,
  `internal/tmuxbridge`, `internal/zellijbridge`, `internal/agent/runtime/*`, or
  `internal/web/api/mux.go` in the first batch

Compatibility notes:

- preserve the current tmux session naming behavior so existing room IDs
  continue to resolve to the same tmux sessions
- preserve browser and SSH entrypoint behavior; this batch is ownership cleanup,
  not terminal UX or protocol change
- keep the shared seam narrow: room/session identity and resolution only, not a
  wholesale gateway rewrite

Review checks for batch 1:

- browser terminal access still attaches to the same tmux session for an
  existing room ID
- SSH terminal access still resolves `room-<id>` to the same tmux session as
  before
- no new direct tmux session-name construction is introduced in gateway code
- docs and package comments still tell the same runtime-terminal ownership story
- `make check-doc-links` passes and targeted tests cover the shared identity
  seam from both gateway entrypoints

### Milestone 3. Consolidate context family boundaries

Objective:

- make the context plane read as explicit controlplane, assembly, history,
  runtime-helper, and knowledge slices instead of a cluster of nearly-adjacent
  roots

Primary packages:

- `internal/contextplane/*`
- `internal/companion/*`
- `internal/transcriptpipeline/*`
- `internal/contextplane/taskhistory/*`
- `internal/sessionkit/*`
- `internal/context/updater/*`
- `internal/storage/context*`
- `internal/knowledge/*`
- `internal/storage/knowledge/*`

Proposed stories:

1. Define the context subfamily map.
2. Scope the history tranche.
3. Clarify knowledge and runtime-helper placement.

Story 1 outcome:

- classify the current context roots into five explicit slices:
  - controlplane
  - assembly
  - history
  - runtime-helper
  - knowledge
- record keep/bridge/move-later decisions for the current roots instead of
  jumping directly to package renames

Current mapping to make durable in Story 1:

| Current package/root | Context slice | Decision |
|------|---------------|----------|
| `internal/contextplane` | controlplane | keep as the control-plane anchor |
| `internal/companion` | assembly | keep as the live assembly anchor |
| `internal/transcriptpipeline` | history | keep as the history-processing anchor |
| `internal/contextplane/taskhistory` | history | treat as part of the same history tranche even though it currently lives under `contextplane` |
| `internal/storage/transcriptcache` | history | keep in storage but plan together with the history tranche |
| `internal/sessionkit` | runtime-helper | keep as a shared helper slice |
| `internal/context/updater` | runtime-helper | treat as a helper/bridge package, not the family anchor |
| `internal/storage/contextbuffer` | runtime-helper | keep in storage and classify with helper work |
| `internal/storage/contextvar` | runtime-helper | keep in storage and classify with helper work |
| `internal/knowledge` | knowledge | keep as the knowledge slice |
| `internal/storage/knowledge` | knowledge | keep in storage and plan with knowledge work |

Why this sequencing:

- the context family is too overloaded to rename safely without first separating
  control-plane, assembly, history, helper, and knowledge concerns
- the history tranche is the cleanest next slice because
  `transcriptpipeline`, `taskhistory`, and `storage/transcriptcache` already
  describe one bounded workflow
- helper/storage roots should not create new top-level placement rules on their
  own; they should stay storage-owned or helper-owned until a narrower tranche
  is ready

Story 2 target:

- scope the **history** tranche as one bounded workflow:
  - `internal/transcriptpipeline` produces transcript-derived history artifacts
    and packs
  - `internal/contextplane/taskhistory` consumes those outputs for
    control-plane and task-oriented views
  - `internal/storage/transcriptcache` persists the prederived transcript
    artifacts

Recommended first batch for Story 2:

- tighten the boundary without renaming packages:
  - route new transcript parsing, normalization, derivation, and cache work
    into `internal/transcriptpipeline` plus `internal/storage/transcriptcache`
  - route new task-facing history presentation and control-plane summaries into
    `internal/contextplane/taskhistory`
  - avoid adding new transcript-history logic to generic `contextplane`,
    `sessionkit`, or other nearby helper roots unless it is clearly a helper

Non-goals for Story 2:

- do not rename `taskhistory`, `transcriptpipeline`, or `transcriptcache`
  packages yet
- do not change ACA promotion semantics or knowledge-plane behavior
- do not fold `sessionkit` or other runtime-helper packages into the history
  tranche during this batch
- do not change transcript artifact formats or cache behavior as part of the
  tranche definition

Story 3 target:

- make the remaining helper roots and knowledge roots unambiguous:
  - `internal/sessionkit`
  - `internal/context/updater`
  - `internal/storage/contextbuffer`
  - `internal/storage/contextvar`
  - `internal/knowledge`
  - `internal/storage/knowledge`

Required placement rule from Story 3:

- runtime-helper slice:
  - shared session store-opening and path helpers
  - archive/snapshot helpers
  - live context injection and short-lived context state
- knowledge slice:
  - durable knowledge packs
  - embedded knowledge assets
  - knowledge sync and registry persistence

Story 3 should end with these durable decisions:

| Package/root | Slice | Decision |
|------|-------|----------|
| `internal/sessionkit` | runtime-helper | keep as shared helper root |
| `internal/context/updater` | runtime-helper | keep as helper/bridge package |
| `internal/storage/contextbuffer` | runtime-helper | keep in storage with helper ownership |
| `internal/storage/contextvar` | runtime-helper | keep in storage with helper ownership |
| `internal/knowledge` | knowledge | keep as durable knowledge-plane logic |
| `internal/storage/knowledge` | knowledge | keep in storage with knowledge ownership |

Non-goals for Story 3:

- do not collapse runtime-helper packages into the control-plane or history
  slices
- do not move knowledge packages under `internal/v2` or any runtime-specific
  family
- do not turn helper/storage packages into new top-level roots

Exit criteria:

- control plane, live assembly, history, runtime-helper, and knowledge concerns
  are visibly separated
- `taskhistory`, `transcriptpipeline`, and transcript-cache/storage helpers are
  treated as one first migration tranche
- future context work has a stable placement rule that does not default to
  `v2`

### Milestone 4. Consolidate intelligence and retrieval families

Objective:

- make retrieval, search, evidence extraction, synthesis, and planning read as
  one intelligence family with explicit subfamily seams

Primary packages:

- `internal/retrieval`
- `internal/retrieval/v2`
- `internal/searchquery`
- `internal/searchindex`
- `internal/searchrank`
- `internal/repoquery`
- `internal/codecontext`
- `internal/codemap`
- `internal/refactor`
- `internal/analysis`
- `internal/verification`

Proposed stories:

1. Map the intelligence subfamilies.
2. Define the retrieval-search consolidation tranche.
3. Carve code-evidence extraction boundaries.

Story 1 outcome:

- separate the intelligence family into explicit subfamilies:
  - ingest/builders
  - search/query/recall
  - evidence gathering
  - synthesis and refactor planning
  - oversight
  - verification

Current mapping to make durable in Story 1:

| Current package/root | Intelligence slice | Decision |
|------|--------------------|----------|
| `internal/indexing` | ingest/builders | keep as builder/index-maintenance slice |
| `internal/searchindex` | ingest/builders | keep as persisted retrieval-document/index slice |
| `internal/retrieval` | search/query/recall | treat as transitional bridge for legacy helpers |
| `internal/retrieval/v2` | search/query/recall | keep as the main retrieval engine |
| `internal/repoquery` | search/query/recall | keep as structural recall/query slice |
| `internal/searchquery` | search/query/recall | keep as query-planning slice |
| `internal/searchrank` | search/query/recall | keep as ranking/fusion slice |
| `internal/codecontext` | evidence gathering | keep as shared code-evidence extraction |
| `internal/codemap/context` | evidence gathering | treat as evidence-gathering support inside codemap |
| `internal/codemap` | synthesis and refactor planning | keep as codemap synthesis |
| `internal/refactor` | synthesis and refactor planning | keep as refactor planning/evidence consumer |
| `internal/analysis/tasksgraph` | synthesis and refactor planning | keep as planning support |
| `internal/analysis/overseer` | oversight | keep as review/task coordination oversight |
| `internal/verification` | verification | keep as verification-specific slice |

Why this sequencing:

- the intelligence family currently mixes builder-side index work, retrieval
  engines, evidence gathering, and post-processing into one broad label
- retrieval path cleanup is not safe until builder-side index ownership and
  evidence/synthesis boundaries are explicit
- code-evidence extraction should be separated from synthesized outputs before
  any consolidation batch is proposed

Story 2 target:

- make the retrieval-search tranche explicit:
  - `internal/retrieval/v2` is the default retrieval/search owner
  - `internal/retrieval` remains a transitional bridge for legacy helpers
  - `internal/repoquery`, `internal/searchquery`, and `internal/searchrank`
    belong with retrieval/search behavior
  - `internal/searchindex` and `internal/indexing` remain the builder/index
    substrate

Required placement rule from Story 2:

- query planning, lexical/vector/structural recall, and cross-source fusion
  belong in the retrieval-search slice
- persisted retrieval-document schemas and index maintenance belong on the
  builder side

Story 2 should end with these durable decisions:

| Package/root | Slice | Decision |
|------|-------|----------|
| `internal/retrieval/v2` | retrieval-search | keep as default owner |
| `internal/retrieval` | retrieval-search | keep as transitional bridge |
| `internal/repoquery` | retrieval-search | keep with structural recall |
| `internal/searchquery` | retrieval-search | keep with query planning |
| `internal/searchrank` | retrieval-search | keep with ranking/fusion |
| `internal/searchindex` | builder substrate | keep on builder side |
| `internal/indexing` | builder substrate | keep on builder side |

Non-goals for Story 2:

- do not merge indexing or persisted index schema work into retrieval packages
- do not pull `codecontext` or `codemap/context` into the same tranche
- do not retire legacy `internal/retrieval` helpers until the replacement
  boundary is explicitly complete

Story 3 target:

- separate code-evidence extraction from synthesis, planning, oversight, and
  verification:
  - `internal/codecontext` and `internal/codemap/context` gather evidence
  - `internal/codemap` and `internal/refactor` synthesize or plan from evidence
  - `internal/analysis/overseer` coordinates and scores
  - `internal/verification` verifies claims

Required placement rule from Story 3:

- new raw code-evidence collection belongs in evidence-gathering packages
- synthesized maps, hotspot packs, and planning artifacts belong in
  synthesis/planning packages
- post-review coordination and task scoring belong in oversight
- claim extraction and verification belong in verification

Story 3 should end with these durable decisions:

| Package/root | Slice | Decision |
|------|-------|----------|
| `internal/codecontext` | evidence gathering | keep as the main evidence extractor |
| `internal/codemap/context` | evidence gathering | keep as codemap-specific evidence support |
| `internal/codemap` | synthesis | keep as codemap synthesis |
| `internal/refactor` | planning/synthesis | keep as refactor planning and evidence consumer |
| `internal/analysis/tasksgraph` | planning support | keep as planning/priority support |
| `internal/analysis/overseer` | oversight | keep as coordination oversight |
| `internal/verification` | verification | keep as verification-specific slice |

Non-goals for Story 3:

- do not move raw snippet extraction into `internal/codemap`
- do not merge verification into oversight
- do not pull planning and graph-analysis packages into retrieval just because
  they consume search outputs

Exit criteria:

- there is one obvious place for new retrieval/query work
- there is one obvious place for code-evidence extraction work
- the first retrieval/search consolidation batch is documented without dragging
  indexing builders into the same rename

### Milestone 5. Consolidate tooling and console surfaces

Objective:

- separate generic tooling from runtime-specific agent tooling
- collapse console/interface duplication so tooling and console surfaces stop
  reading like accidental parallel roots

Primary packages:

- `internal/tooling`
- `internal/tools/*`
- `internal/skillrun`
- `internal/adapters/skillslib/*`
- `internal/agent/tools`
- `internal/console`
- `internal/consoleapp`

Proposed stories:

1. Define the generic tooling family.
2. Decide the console family boundary.
3. Plan the first tooling consolidation batch.

Story 1 outcome:

- separate generic tooling substrate from runtime-facing agent tooling

Current mapping to make durable in Story 1:

| Current package/root | Tooling slice | Decision |
|------|---------------|----------|
| `internal/tooling` | generic tooling | keep as the tool-registry substrate |
| `internal/skillrun` | generic tooling | keep as generic skill execution/runtime-neutral wrapper |
| `internal/tools/*` | generic tooling | keep as standalone generic tools |
| `internal/adapters/skillslib/*` | generic tooling support | keep as reusable skill-helper libraries |
| `internal/agent/tools` | agent-runtime tooling | keep separate from the generic tooling family |

Why this sequencing:

- the repo currently uses “tools” for both reusable tooling substrate and
  runtime-facing agent tools
- console duplication can be handled later, but the generic tooling contract
  needs to be stable first
- future tooling moves will be noisy unless generic tooling and agent-runtime
  tooling stop sharing one ambiguous label

Story 2 target:

- separate console-session utilities from the console application/runtime
  surface and from the web transport host

Required placement rule from Story 2:

- `internal/console` owns reusable console-session primitives such as
  correlation tracking
- `internal/consoleapp` owns runner, stream parsing, and application/runtime
  behavior for console sessions
- `internal/web/consolews` remains the transport host for console sessions

Story 2 should end with these durable decisions:

| Package/root | Slice | Decision |
|------|-------|----------|
| `internal/console` | console utilities | keep as narrow utility slice |
| `internal/consoleapp` | console application | keep as the runtime/app slice |
| `internal/web/consolews` | interface transport | keep as the web transport entrypoint |

Non-goals for Story 2:

- do not merge `console` and `consoleapp`
- do not move websocket hosting into `internal/console`
- do not redesign console UX or streaming behavior as part of the topology pass

Story 3 target:

- choose the smallest safe slice that proves the tooling family without a
  package rename

Recommended first batch:

- package-comment and documentation guardrails only:
  - clarify generic tooling ownership in `internal/tooling`
  - clarify runtime-facing ownership in `internal/agent/tools`
  - clarify the console utility versus console application split in
    `internal/console` and `internal/consoleapp`

Why this batch:

- it proves the tooling family contract in code immediately
- it avoids changing tool execution semantics or console runtime behavior
- it keeps later package moves optional and incremental

Non-goals for Story 3:

- do not rename tooling or console packages yet
- do not rework tool execution semantics
- do not redesign console UX or streaming behavior

Exit criteria:

- generic tooling packages are clearly separated from `internal/agent/tools`
- `console` and `consoleapp` stop reading like accidental duplication in the
  target topology
- the first tooling move batch is small enough to land without a repo-wide
  rename

### Final proposal. Verification and adoption

This milestone is still a proposal in the room rather than an active milestone.
It exists to close the loop after the five family milestones above.

Goal:

- prove the epic meets its acceptance bar and is ready for broad use

Expected scope:

- verify package placement decisions use the documented family model
- verify `internal/v2` remains explicitly scoped to the newer
  agent/runtime/orchestration lane
- verify the first migration batches are expressed as milestones and stories
  rather than ad hoc cleanup

## First Execution Slice

Start with Milestone 1, Story 1:

- align `docs/architecture/package-topology.md`
- align this epic doc
- then refresh the key package comments that need to tell the same story in
  code

That first slice is the highest leverage because every later milestone depends
on a stable vocabulary before package moves begin.

## Non-Goals

- repo-wide import rewrites in one pass
- moving `internal/storage/*`, `internal/contextplane`, or intelligence families
  under `internal/v2`
- hiding behavioral runtime changes inside topology cleanups
- renaming packages only to satisfy aesthetics with no family-boundary payoff

## Risks

| Risk | Why it matters | Mitigation |
|------|----------------|------------|
| package churn with little behavioral value | creates merge pain and weakens history | land one family slice at a time |
| mixing topology cleanup with runtime migration | makes rollback and review harder | keep package-boundary changes separate from runtime parity work |
| `v2` becoming a generic "new code" bucket again | recreates the same ambiguity later | keep the `v2` scope explicit in docs, comments, and review guidance |
| ambiguous bridge packages continuing to grow | erodes the family model before the migration lands | route new work through the freeze milestone guidance first |

## Success Criteria

- new packages almost always fit into one documented family without debate
- reviewers can explain why a package belongs in runtime, context,
  intelligence, interfaces, tooling, or foundations
- `internal/v2/*` stays readable as the newer agent/runtime/orchestration lane
- the number of ambiguous top-level roots stops growing
- each major family has at least one documented first move batch that can land
  independently

## Related Docs

- [docs/architecture/package-topology.md](../../architecture/package-topology.md)
- [docs/architecture/system-architecture.md](../../architecture/system-architecture.md)
- [docs/general/runtime-orchestration.md](../../general/runtime-orchestration.md)
- [docs/plans/features/eino-go-native-runtime-plan.md](./eino-go-native-runtime-plan.md)
- [docs/plans/retrieval-search-refactor.md](../../plans/retrieval-search-refactor.md)
