# ACA Dual-Plane Completion Plan

Status: active plan

Owner: agentctl

Last updated: 2026-03-09

## Goal

Complete the remaining work required to turn ACA from a strong control-plane foundation into the intended dual-plane architecture:

- a local operational control plane for continuity
- an external Obsidian-backed knowledge plane for durable reuse

The current implementation already covers orientation, capture, inference, clustering, reporting, maintenance generation, promotion drafts, reviewed promotion merges, hook wiring, bootstrap installation, daemonized ACA refresh, and first-class MCP reads/actions over the ACA control plane. The remaining work is primarily:

- knowledge-plane adapter implementation
- contradiction and health loops
- richer retrieval and ranking
- explicit task packet / dispatch flow
- externalization of durable notes beyond local vault templates

## Current State

Implemented now:

- `agentctl orient`
- `agentctl capture`
- `agentctl observe`
- `agentctl tension`
- `agentctl context show|report|retrieve|contradictions|next|dispatch|rethink|handoffs|observations|tensions|infer|promote|merge-promotion|hooks install`
- clustered observations and tensions
- maintenance queue generation
- promotion drafts from handoffs and repeated observations
- reviewed merge path from promotion drafts into canonical notes
- Claude lifecycle hook wiring for `SessionStart`, `Stop`, and `SubagentStop`
- first-class ACA MCP tools for read/report/retrieve/contradictions/rethink/promote/merge
- workspace-local mutable ACA store in `.agentctl/runtime/contextplane.db`
- Obsidian adapter (`search`, `read`, `related`, controlled writes)
- vault indexing and graph-backed related lookup
- daemonized ACA maintenance refresh

Not implemented yet:

- repo-path and symbol note backlinks
- richer promotion review/merge workflow and trust-state transitions beyond explicit reviewed merges
- repo-graph-driven symbol backlinks from the code index
- semantic retrieval/reranking beyond lexical + weighted blending
- a fuller external `contextd` service beyond the current daemon ACA loop

## Architecture Target

### Control plane

Keep local, deterministic, and write-optimized:

```text
.agentctl/
  runtime/
    state.db
    top_of_mind.json
    current_run.json
    contextplane.db
    queue/
    handoffs/
    sessions/
    events.ndjson
  policy/
  exports/
```

### Knowledge plane

Keep human-readable, link-dense, and durable:

```text
vault/
  00-home/
  atlas/
  self/
  notes/
    adr/
    patterns/
    incidents/
    investigations/
    claims/
    sources/
    moc/
  inbox/
    drafted-from-agentctl/
  ops/
```

### Integration contract

- control plane remains authoritative for active execution
- knowledge plane remains authoritative for reviewed durable notes
- promotion is the only normal bridge from operational artifacts to canonical notes

## Phases

## Phase 1: Obsidian Adapter

Objective:

Implement the first real knowledge-plane adapter under `internal/tools/obsidian` or equivalent package boundary.

Deliverables:

- `obsidian.search`
- `obsidian.read`
- `obsidian.related`
- `obsidian.create_note`
- `obsidian.append_under_heading`
- `obsidian.capture_session`
- `obsidian.promote_to_evergreen`

Requirements:

- filesystem-first interface, no plugin lock-in
- official Obsidian CLI syntax when CLI is the transport
- clear write policy boundaries
- inbox-first durable writes
- support for vault name or path targeting

Primary files:

- `internal/tools/obsidian/search.go`
- `internal/tools/obsidian/read.go`
- `internal/tools/obsidian/write.go`
- `internal/tools/obsidian/links.go`
- `internal/tools/obsidian/policy.go`
- `cmd/agentctl/cmd/obsidian.go`

Verification:

- create/read/search/append smoke tests against a temp vault
- guardrails preventing canonical-note overwrite outside allowed sections

## Phase 2: Vault Index And Graph

Objective:

Index vault markdown into a queryable local graph/cache so retrieval is not limited to raw shelling out.

Deliverables:

- note table
- heading table
- wikilink/backlink table
- tag table
- alias table
- chunk table
- index build and refresh commands

Suggested schema:

- `notes(id, path, title, type, project, status, trust, updated_at, hash)`
- `headings(id, note_id, heading, level, anchor)`
- `links(src_note_id, dst_note_id, link_text)`
- `tags(note_id, tag)`
- `aliases(note_id, alias)`
- `chunks(id, note_id, heading_id, text, token_count, embedding)`

Primary files:

- `internal/storage/obsidianindex/`
- `cmd/agentctl/cmd/obsidian_index.go`

Verification:

- indexing test vault yields stable note/link counts
- backlink and related-note queries return deterministic results

## Phase 3: Retrieval Planner

Objective:

Move from control-plane-only lookup to real dual-plane retrieval.

Deliverables:

- lexical vault retrieval
- semantic retrieval as optional enhancement
- graph-neighbor expansion
- reranking by:
  - project match
  - note type
  - trust
  - link proximity
  - recency
  - reuse frequency

Required read order:

1. `top_of_mind`
2. active task packet
3. latest handoff
4. procedural rules
5. vault retrieval
6. optional external docs

Primary files:

- `internal/context/contextplane/retrieval.go`
- `internal/tools/obsidian/search.go`
- `internal/storage/obsidianindex/*`

Verification:

- golden retrieval tests for implementation, research, and continuation modes
- no semantic dependency for baseline recall

## Phase 4: Task Packet And Dispatch Flow

Objective:

Implement the spec’s missing `select -> dispatch` path and fresh worker packet semantics.

Deliverables:

- explicit ACA task model
- `agentctl next`
- `agentctl dispatch <task-id>`
- packet builder with bounded context
- packet sources:
  - top-of-mind
  - latest handoff
  - procedural rules
  - selected evidence

Requirements:

- workers should not inherit the full transcript by default
- packet budgets should remain explicit
- maintenance tasks should compete in the same queue

Primary files:

- `internal/context/contextplane/tasks.go`
- `internal/context/contextplane/dispatch.go`
- `cmd/agentctl/cmd/context_next.go`
- `cmd/agentctl/cmd/context_dispatch.go`

Verification:

- queue ordering tests
- packet-size tests
- dispatch output contains only bounded inputs

## Phase 5: Promotion Workflow And Trust States

Objective:

Turn draft promotion into a durable review workflow.

Deliverables:

- trust levels on durable notes:
  - `raw`
  - `reviewed`
  - `canonical`
- promotion review command(s)
- controlled append to canonical notes
- policy enforcement against silent ADR/methodology rewrites
- merge suggestions instead of unbounded direct edits

Primary files:

- `internal/context/contextplane/promotion.go`
- `internal/tools/obsidian/policy.go`
- `cmd/agentctl/cmd/context_promote_review.go`

Verification:

- promotion from repeated observation
- promotion from contradiction-driven rule change
- canonical-write denial outside allowed sections

## Phase 6: Contradiction And Health Loops

Objective:

Add the mechanical daemon work that keeps the system clean.

Deliverables:

- contradiction detector over vault notes and ACA tensions
- stale-note detector
- orphan-note detector
- oversized MOC detector
- retrieval-quality metrics
- maintenance queue production from those scans

Daemon loops:

- fast loop: 2-5 minutes
- medium loop: 30-60 minutes
- slow loop: 12-24 hours

Primary files:

- `internal/context/contextplane/daemon.go`
- `internal/context/contextplane/health.go`
- `internal/context/contextplane/contradictions.go`

Verification:

- synthetic vault health fixtures
- repeated tension -> maintenance task
- unresolved high-impact tension blocks related promotion

## Phase 7: Repo-Aware Note Linking

Objective:

Connect code and durable notes directly.

Deliverables:

- note frontmatter support for:
  - `repo`
  - `paths`
  - `symbols`
- repo index -> note refs
- note lookup by path or symbol
- retrieval boost for repo-linked notes

Primary files:

- `internal/tools/obsidian/links.go`
- `internal/intelligence/indexing/repoindex/*`
- `internal/context/contextplane/retrieval.go`

Verification:

- file/symbol lookup returns linked notes
- implementation-mode retrieval favors repo-linked notes

## Phase 8: External Context Daemon

Objective:

Unify ACA maintenance and knowledge access behind one long-lived service.

Deliverables:

- `agentctl-contextd` or equivalent embedded daemon mode
- single state service for:
  - orientation
  - compaction
  - curation
  - contextualization
  - health
  - scheduling
- MCP served from that same state service

Primary files:

- `internal/context/contextplane/daemon.go`
- `cmd/agentctl/cmd/contextd.go`
- `cmd/agentctl/cmd/mcp.go`

Verification:

- daemon restart preserves ACA mutable state
- MCP queries reflect daemon-updated state

## Cross-Cutting Constraints

- top-of-mind remains computed, not hand-authored
- no canonical note without provenance refs
- vault must remain useful without embeddings
- semantic retrieval must remain optional
- active execution must not depend on Obsidian plugins
- control-plane mutations should remain rollback-safe and idempotent where possible

## Milestones

### Milestone A

Knowledge-plane adapter exists and can read/search/write inbox drafts safely.

Exit criteria:

- `obsidian.read`
- `obsidian.search`
- `obsidian.related`
- `obsidian.create_note`
- tests against temp vaults

### Milestone B

Vault indexing and retrieval planner exist.

Exit criteria:

- note graph index
- graph-first retrieval
- ranked blended results

### Milestone C

ACA dispatch flow exists.

Exit criteria:

- `context next`
- `context dispatch`
- bounded worker packets

### Milestone D

Promotion and trust-state workflow exists.

Exit criteria:

- reviewable promotion path
- canonical write enforcement

### Milestone E

Health loops and repo-aware linking exist.

Exit criteria:

- contradiction detection
- stale/orphan/MOC health
- repo path/symbol backlinks

### Milestone F

Single daemon + MCP knowledge layer exists.

Exit criteria:

- daemon-managed ACA maintenance
- stable MCP context API
- dual-plane architecture operational end to end

## Recommended Build Order

1. Phase 1
2. Phase 2
3. Phase 3
4. Phase 5
5. Phase 4
6. Phase 6
7. Phase 7
8. Phase 8

Reason:

- knowledge-plane reads unlock the rest of the dual-plane work
- promotion and retrieval are more foundational than scheduler polish
- dispatch should consume the retrieval/knowledge work instead of being built twice

## Success Criteria

- resume latency falls because ACA provides an immediate current-state report
- repeated questions fall because top-of-mind + latest handoff + vault retrieval are enough
- canonical notes are reused, not just accumulated
- maintenance work is visible and queued rather than hidden in drift
- vault notes remain navigable through links, folders, tags, and MOCs without embeddings

## Out Of Scope For This Plan

- redesigning the broader agentctl task system outside ACA needs
- replacing existing memory/session/task stores wholesale
- forcing a specific Obsidian plugin dependency
- automatic editing of canonical vault content without review gates
