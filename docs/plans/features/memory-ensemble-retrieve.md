# Memory Ensemble Retrieve

Status: proposed active plan
Owner: companion / rlm / agent runtime
Last Updated: 2026-03-22

## Goal

Add a memory-focused scout ensemble to `agentctl` with two properties:

1. each scout is a first-class, individually callable bounded agent role
2. the ensemble is exposed as its own retrieval call, layered above those scouts

The intended outcome is:

- narrow, debuggable memory scouts similar to existing `semantic_scout`,
  `dag_scout`, `symbol_scout`, and `annotation_scout`
- a standalone `memory_ensemble_retrieve` tool on the experimental RLM surface
- later companion integration without overloading the older
  `rlm_context_query` lane

## Core Decision

### `memory_ensemble_retrieve` is its own call, under RLM

Do **not** fold the ensemble into `rlm_context_query`.

Reason:

- `internal/engine/rlm_tools.go` is the older conversation-scoped contextvar
  and companion-memory lane
- it mixes read and write concerns
- it is not the best home for a typed, read-only, multi-scout retrieval flow

Instead:

- keep `rlm_context_query` cheap and always-available
- add `memory_ensemble_retrieve` to the experimental typed RLM tool surface in
  `internal/rlm/env/*`
- let the ensemble orchestrate real scout roles through bounded subcalls

This keeps the boundaries clean:

```text
companion memory / ACA / sessions / annotations
  -> what exists

scouts
  -> specialized bounded inspection over one lane

memory_ensemble_retrieve
  -> coordinated multi-scout retrieval result

RLM / companion controller
  -> decides when to pay for the ensemble
```

### Scouts are first-class roles, not hidden implementation details

Each memory scout must be directly callable as a role, for example:

- `memory_fact_scout`
- `memory_timeline_scout`
- `aca_context_scout`

The ensemble should call those same roles, not duplicate their reasoning in a
second internal-only path.

This gives:

- direct eval and debugging of one lane at a time
- reusable scout roles outside the ensemble
- a clear mapping between scout identity and tool budget

## Non-Goals

- Do not replace `rlm_context_query`.
- Do not add a new continuity controller source enum in the first slice.
- Do not make every companion turn spawn child agents.
- Do not grant memory scouts broad repo-edit or shell power.
- Do not make memory scouts write to memory in the first slice.

## Current State

### Existing scout substrate

The legacy agent runtime already supports scout-style roles:

- role constants in `internal/agent/types/types.go`
- default spawn prompts in `internal/agent/prompts/prompts.go`
- role-specific tool bundles in `internal/agent/runtime/runtime.go`
- allow/deny tests in `internal/agent/runtime/runtime_test.go`

Important current constraint:

- runtime system instructions do **not** come from
  `internal/agent/prompts/prompts.go`
- they come from `internal/agentprompt/signature.go`
- existing scout roles are not explicitly represented there today

That means a new scout role is not fully wired until both layers are updated.

### Existing memory and continuity substrate

Available read surfaces already exist for:

- companion layered memory context
- companion memory search
- named memory search
- session recall and timeline
- annotation recall
- ACA top-of-mind and blended retrieval
- Obsidian note search and reads

Relevant existing paths:

- `cmd/agentctl/cmd/agent_memory.go`
- `internal/context/companion/service.go`
- `internal/agent/runtime/runtime.go`
- `internal/rlm/env/*`

### Existing subcall substrate

Companion and experimental RLM already have bounded subcall concepts, but the
experimental `subcall` tool is not role-aware yet.

Today it can bound prompt and handles, but not choose a specialized child role.

This is the key missing piece for having `memory_ensemble_retrieve` call real
memory scouts instead of duplicating their logic.

## Proposed Scout Roles

Start with three roles.

### 1. `memory_fact_scout`

Purpose:

- recover explicit facts, preferences, decisions, goals, and technical context
- answer “what do we know right now?”

Allowed tools:

- `think`
- `end_tick`
- `agent_memory_search`
- `agent_memory_context`
- `memory_query`
- `session_recall`
- `annotation_recall`
- `context_filter`

Expected output shape:

```json
{
  "scout": "memory_fact_scout",
  "query": "string",
  "facts": [
    {
      "key": "string",
      "value": "string",
      "status": "current|candidate",
      "source": "string",
      "evidence": ["string"],
      "confidence": 0.0
    }
  ],
  "summary": "string",
  "gaps": ["string"]
}
```

### 2. `memory_timeline_scout`

Purpose:

- reconstruct updates, retractions, and current-vs-old state
- answer “what changed, and in what order?”

Allowed tools:

- `think`
- `end_tick`
- `session_timeline`
- `session_recall`
- `annotation_recall`
- `annotation_category_stats`
- `agent_memory_search`
- `context_filter`

Expected output shape:

```json
{
  "scout": "memory_timeline_scout",
  "query": "string",
  "timeline": [
    {
      "ts": "string",
      "kind": "statement|update|retraction|decision",
      "value": "string",
      "source": "string",
      "supersedes": "string",
      "confidence": 0.0
    }
  ],
  "current_best_view": "string",
  "summary": "string",
  "gaps": ["string"]
}
```

### 3. `aca_context_scout`

Purpose:

- recover broader durable task and workspace continuity
- answer “what is the surrounding durable context for this?”

Allowed tools:

- `think`
- `end_tick`
- `context_show`
- `context_retrieve`
- `obsidian_index_search`
- `obsidian_read`
- `obsidian_related`
- `context_filter`

Expected output shape:

```json
{
  "scout": "aca_context_scout",
  "query": "string",
  "context_blocks": [
    {
      "lane": "top_of_mind|task_continuity|vault|related_note",
      "summary": "string",
      "refs": ["string"]
    }
  ],
  "recommended_context": "string",
  "summary": "string",
  "gaps": ["string"]
}
```

### Deferred role: `memory_conflict_scout`

Do not build this in the first slice.

Initially, contradiction and stale-vs-current resolution should be handled by
the ensemble merger over fact and timeline outputs.

Promote conflict resolution into a dedicated scout only after:

- fact scout outputs are stable
- timeline outputs are stable
- evals show a clear benefit from isolating contradiction work

## Ensemble Tool Contract

Expose a new typed RLM tool:

- `memory_ensemble_retrieve`

It should live on the experimental RLM tool surface, not in the older
`rlm_context_*` executor.

### Input

```json
{
  "query": "What is the current codename and when did it change?",
  "lanes": ["facts", "timeline", "aca"],
  "max_scouts": 3,
  "max_iterations_per_scout": 4,
  "max_subcalls_per_scout": 0,
  "limit_per_lane": 5
}
```

### Output

```json
{
  "summary": "string",
  "facts": [],
  "timeline": [],
  "aca_context": [],
  "conflicts": [
    {
      "field": "string",
      "old_value": "string",
      "new_value": "string",
      "resolution": "prefer_newer|ambiguous"
    }
  ],
  "recommended_answer_basis": "facts|timeline|aca|combined",
  "evidence_refs": ["string"],
  "retrieved_paths": ["string"],
  "metadata": {
    "scouts_run": [
      "memory_fact_scout",
      "memory_timeline_scout",
      "aca_context_scout"
    ]
  }
}
```

### Routing defaults

- direct fact query:
  - run `memory_fact_scout`
- update/latest/current query:
  - run `memory_timeline_scout` then `memory_fact_scout`
- continuity/task/workspace query:
  - run `aca_context_scout`
- ambiguous memory query:
  - run all three

## Design Patterns

### Narrow scout pattern

Each scout owns:

- one primary problem type
- one narrow read-only tool family
- one small structured output schema

This mirrors the existing scout model better than a single general memory
researcher.

### Ensemble-over-scouts

The ensemble is an orchestrator, not a fourth generalist scout.

Its job is to:

- choose scouts
- run them
- merge them
- surface conflicts and answer-basis recommendations

### Role-aware subcall

The experimental RLM `subcall` path should gain an optional child role.

That lets:

- `memory_ensemble_retrieve`
  call
- `memory_fact_scout`
  or
- `memory_timeline_scout`
  directly

without inventing a separate child-execution substrate.

## Implementation Plan

### Phase 1: Add individually callable memory scout roles

Add three new roles:

- `memory_fact_scout`
- `memory_timeline_scout`
- `aca_context_scout`

Files:

- `internal/agent/types/types.go`
- `internal/agent/prompts/prompts.go`
- `internal/agentprompt/signature.go`

Key work:

- add role constants
- add default spawn prompts
- add runtime system instructions

### Phase 2: Add scout-specific tool bundles

Wire each new role into the legacy agent runtime tool surface.

Files:

- `internal/agent/runtime/runtime.go`
- `internal/agent/runtime/runtime_test.go`
- `internal/agent/daemon/handlers.go`

Key work:

- add per-role tool allowlists
- extend research-execution contract enforcement to memory scouts
- add role tests similar to current scout tests

### Phase 3: Expose companion-memory inspection tools to scouts

The current runtime already exposes ACA, sessions, annotations, and named
memory, but not dedicated companion-memory inspection tools.

Add read-only runtime tools:

- `agent_memory_context`
- `agent_memory_search`

Files:

- `internal/agent/runtime/runtime.go`
- `cmd/agentctl/cmd/agent_memory.go`
- `internal/agent/toolnames/registry.go`
- `internal/agent/runtime/runtime_test.go`

Implementation note:

- reuse the existing `agent memory context` and `agent memory search` CLI
  commands instead of adding direct DB access to the runtime executor

### Phase 4: Make experimental RLM subcalls role-aware

Extend the experimental RLM `subcall` tool so a parent can request a child role.

Files:

- `internal/rlm/types.go`
- `internal/rlm/env/tools.go`
- `internal/rlm/env/adapter.go`
- `internal/rlm/inspect_runner.go`
- related RLM tests

Key work:

- add optional `role` to subcall schema
- thread role through task/subcall callback
- preserve bounded limits and current defaults when role is omitted

### Phase 5: Add `memory_ensemble_retrieve` to the experimental RLM surface

Expose the ensemble as a standalone typed retrieval call.

Files:

- `internal/rlm/env/tools.go`
- `internal/rlm/env/adapter.go`
- new package:
  - `internal/rlm/memoryensemble/`
  or
  - `internal/intelligence/retrieval/memoryensemble/`

Preferred implementation split:

- reusable orchestration logic in a dedicated package
- thin tool wrapper in `internal/rlm/env/adapter.go`

Key work:

- choose scouts based on query and lane hints
- run scouts via role-aware bounded subcalls
- merge structured results
- return one compact typed payload

### Phase 6: Add retrieval eval and observability hooks

Files:

- `cmd/agentctl/cmd/eval.go`
- `internal/evals/retrievaleval/eval.go`
- `internal/rlm/llm_runner.go`
- optional observability wiring in `internal/observability/*`

Key work:

- add one or more eval modes such as:
  - `rlm_llm_memory_ensemble`
- capture:
  - scouts run
  - evidence refs
  - retrieved paths
  - conflict count
- keep current eval metrics comparable to other RLM routes

### Phase 7: Companion integration

Do this only after the standalone tool and scout roles are stable.

First slice:

- do **not** add a new continuity source enum
- do **not** modify `rlm_context_query`

Preferred integration:

- expose the ensemble through the companion tooling layer as an optional extra
  read-only tool
- let the companion controller or tool-using model call it when durable memory
  is needed

Potential future files:

- `internal/context/companion/service.go`
- `internal/interfaces/web/api/companion.go`
- `internal/agent/daemon/daemon.go`

## File Changes

### `internal/agent/types/types.go` (modified)

Add the new role constants.

### `internal/agent/prompts/prompts.go` (modified)

Add spawn-time default prompts for each memory scout role.

### `internal/agentprompt/signature.go` (modified)

Add runtime system instructions for the new roles.

Important:

- do not rely on `internal/agent/prompts/prompts.go` alone
- the runtime currently uses `internal/agentprompt/signature.go`

### `internal/agent/runtime/runtime.go` (modified)

Add:

- new role-specific tool bundles
- new executor dispatch cases for:
  - `agent_memory_context`
  - `agent_memory_search`

### `internal/agent/runtime/runtime_test.go` (modified)

Add allow/deny tests for each new scout role and the new memory tools.

### `internal/agent/toolnames/registry.go` (modified)

Register any newly exposed runtime tool names.

### `internal/agent/daemon/handlers.go` (modified)

Treat memory scouts like the existing research-oriented roles for execution
contract injection.

### `internal/rlm/types.go` (modified)

Add optional child role support if needed for subcalls.

### `internal/rlm/env/tools.go` (modified)

Add:

- optional `role` to `subcall`
- new `memory_ensemble_retrieve` tool definition

### `internal/rlm/env/adapter.go` (modified)

Implement:

- role-aware `subcallTool`
- `memory_ensemble_retrieve` adapter entrypoint

### `internal/rlm/memoryensemble/*` or `internal/intelligence/retrieval/memoryensemble/*` (new)

Implement:

- scout selection
- scout execution
- structured merge
- conflict synthesis

### `cmd/agentctl/cmd/eval.go` (modified)

Add retrieval eval mode(s) for the ensemble.

## Testing Strategy

### Unit tests

- new role/tool bundle tests in `internal/agent/runtime/runtime_test.go`
- prompt/signature coverage for new scout roles
- `subcall` role passthrough tests in `internal/rlm/env/adapter_test.go`
- merge logic tests for the ensemble package

### Integration tests

- spawn each scout role and verify it receives the intended tool budget
- run `memory_ensemble_retrieve` with mocked subcalls and verify:
  - lane selection
  - merged output schema
  - conflict reporting
- run end-to-end experimental RLM retrieval with the ensemble enabled

### Retrieval evals

Track:

- whether the ensemble improves temporal memory questions
- whether it reduces stale-fact errors
- whether it improves evidence quality over baseline `rlm_context_query`

## Rollout Order

1. add memory scout roles
2. add scout tool bundles
3. add `agent_memory_context` and `agent_memory_search`
4. add role-aware experimental `subcall`
5. add standalone `memory_ensemble_retrieve`
6. add eval and observability
7. integrate into companion as an optional advanced lane

## Open Questions

### Should `memory_ensemble_retrieve` live in `internal/rlm/env` or `internal/engine/rlm_tools.go`?

Decision for this plan:

- tool definition lives in `internal/rlm/env`
- reusable orchestration logic lives in a dedicated package
- do not use `internal/engine/rlm_tools.go` as the primary home

### Should the first slice use real child agents or inline scout emulation?

Decision for this plan:

- real scout roles are the target behavior
- role-aware bounded subcalls are part of the initial architecture
- do not build a duplicate inline-only scout path if avoidable

### Should the companion continuity controller grow a new source enum?

Decision for this plan:

- not in the first slice
- use the ensemble as a callable retrieval tool first
- revisit controller routing after evals prove value

### Should scouts be allowed to write memory?

Decision for this plan:

- no
- first slice is read-only
- memory writeback, if added later, should be a separate explicit path
