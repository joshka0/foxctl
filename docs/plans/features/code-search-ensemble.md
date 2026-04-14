# Code Search Ensemble

Status: proposed active plan
Owner: rlm / agent runtime / evals
Last Updated: 2026-03-23

## Goal

Add a staged `code_search_ensemble` retrieval controller to `foxctl` that
gets the right code evidence into model context with the least possible context
rot.

This is intentionally **not** just "more scouts".

The intended outcome is:

1. a standalone code-search ensemble call that can be used by RLM and evals
2. a staged retrieval flow that uses the minimum useful lanes before widening
3. compact, grounded evidence packs instead of raw tool-output dumps
4. optional scout escalation, rather than scout fanout by default

## Core Decision

### `code_search_ensemble` is a controller, not a scout

Do **not** model this as a fifth scout or as a synonym for
`semantic_scout` / `dag_scout` / `symbol_scout`.

The ensemble is:

- a retrieval planner
- a lane selector
- a grounding enforcer
- a compactor

Scouts remain useful, but as optional worker modes inside the ensemble.

This keeps the layers clean:

```text
tools
  -> primitive retrieval and verification operations

scouts
  -> narrow worker roles specialized for one lane

code_search_ensemble
  -> staged controller that chooses lanes, grounds evidence, and compacts output

RLM / agent controller
  -> decides when to call the ensemble at all
```

### The ensemble is a standalone call

Do **not** overload:

- `context_search`
- `smart_search`
- existing scout roles
- `memory_ensemble_retrieve`

Instead:

- add `code_search_ensemble` to the experimental typed RLM tool surface in
  `internal/rlm/env/*`
- keep individual search tools and scout roles directly callable
- let the ensemble call direct tools first, then optionally escalate to scouts

This preserves:

- debuggability of each primitive lane
- stable single-purpose scout roles
- eval visibility into lane choice and grounding quality

## Problem Statement

The current retrieval surfaces are useful but not coordinated well enough for
high-signal code search.

Observed issues:

- semantic discovery can still retrieve semantically related but wrong-scope
  files when the query itself is ambiguous
- `symbol_scout` is useful as a precision lane, but weak as a first-pass
  discovery lane
- broad agents over-search and spend tokens on irrelevant lanes
- some questions need structural validation (`repo_index_*`) before a result is
  trustworthy
- some questions need exact chunk extraction (`code_symbols`, `context_grep`,
  `fs_read_file`) before the answer should be promoted into context

The problem is not "missing tools". It is unstructured retrieval fanout.

## Non-Goals

- Do not replace `semantic_search_code`, `repo_index_*`, `code_symbols`, or
  other primitives.
- Do not collapse all search into one hidden heuristic tool.
- Do not make scouts mandatory for every code search.
- Do not answer from ungrounded semantic hits alone for exact location tasks.
- Do not mix ACA or memory lanes into code search unless the task explicitly
  needs historical or continuity context.

## Design Principles

### 1. Direct tools first, scouts second

The ensemble should prefer direct tool use for cheap, deterministic retrieval.

Escalate to scout roles only when:

- the question remains ambiguous after first-pass retrieval
- a specialized lane needs deeper bounded reasoning
- the budget allows a higher-cost second pass

### 2. Ground before answering

No exact file-change, trace, or snippet answer is considered complete until at
least one grounding step has happened.

Allowed grounding steps:

- `repo_index_open`
- `fs_read_file`
- `context_grep`
- `code_symbols`

### 3. Stage widening instead of parallel fanout

Default behavior should be:

1. generate candidates cheaply
2. validate the candidate set
3. extract only the exact evidence needed
4. widen only if uncertainty remains

### 4. Return compact evidence, not raw logs

The output must be a normalized evidence pack, not concatenated tool outputs.

### 5. No keyword heuristics

Do not route lanes by ad hoc substring matching.

Task selection should be:

- explicit from the caller when available
- otherwise derived by a typed classifier or deterministic task-shape logic
- later replaceable by a cheap learned router

## Retrieval Plan Model

`code_search_ensemble` should operate in four internal stages.

### Stage 1: Query Analysis

Determine:

- `task_type`
- whether historical context is allowed
- whether ACA context is allowed
- whether exact grounding is required
- whether scout escalation is allowed

Supported task types:

- `file_locate`
- `execution_trace`
- `symbol_inspect`
- `change_impact`
- `historical_decision`
- `continuity_context`

### Stage 2: Candidate Generation

Use the minimum useful lane set for the task type.

Default first-pass tools:

- `semantic_search_code`
- `repo_index_search`

Optional first-pass scouts:

- `semantic_scout`
- `dag_scout`

### Stage 3: Grounding

Verify the candidate set against real repo anchors.

Grounding tools:

- `repo_index_open`
- `repo_index_dag_grep`
- `code_symbols`
- `context_grep`
- `fs_read_file`

Optional grounding scout:

- `symbol_scout`

### Stage 4: Optional Historical/Continuity Augmentation

Only run this stage when the task type or caller explicitly requires it.

Context lanes:

- `semantic_search_sessions`
- `semantic_search_memories`
- `semantic_search_context`
- `session_recall`
- `session_timeline`
- `agent_memory_search`
- `agent_memory_context`
- `context_show`
- `context_retrieve`

Optional scouts:

- `memory_fact_scout`
- `memory_timeline_scout`
- `aca_context_scout`

## Task-Type to Lane Policy

### `file_locate`

Question shape:

- "Which files matter for X?"
- "Where would I change Y?"

Default lanes:

1. `semantic_search_code`
2. `repo_index_search`
3. `repo_index_open`

Optional escalation:

- `dag_scout` when structural path is still unclear
- `symbol_scout` only after candidate files exist

Do not include:

- ACA
- session recall
- memory recall

### `execution_trace`

Question shape:

- "How does X flow through the system?"
- "Where does this call path execute?"

Default lanes:

1. `repo_index_search`
2. `repo_index_dag_grep`
3. `repo_index_open`
4. `context_grep` or `fs_read_file`

Optional escalation:

- `dag_scout`
- `symbol_scout` for exact call sites or signatures

### `symbol_inspect`

Question shape:

- "What is in this file/type/function?"
- "Show the exact chunk that handles X."

Default lanes:

1. `semantic_search_code` or caller-supplied candidate files
2. `code_symbols`
3. `context_grep`
4. `fs_read_file`

Optional escalation:

- `symbol_scout`

### `change_impact`

Question shape:

- "What else would break if I change X?"
- "What files and call paths are impacted?"

Default lanes:

1. `repo_index_search`
2. `repo_index_dag_grep`
3. `repo_index_open`
4. `code_symbols`

Optional escalation:

- `dag_scout`
- `symbol_scout`

### `historical_decision`

Question shape:

- "Why was this done this way?"
- "What did we decide before about X?"

Default lanes:

1. `semantic_search_sessions`
2. `semantic_search_memories`
3. `session_recall`
4. `agent_memory_search`

Optional augmentation:

- `memory_fact_scout`
- `memory_timeline_scout`

Do not let historical context override current code evidence for exact
file-location answers.

### `continuity_context`

Question shape:

- "What were we doing around this area?"
- "What is the current durable context or handoff state?"

Default lanes:

1. `semantic_search_context`
2. `context_show`
3. `context_retrieve`

Optional augmentation:

- `aca_context_scout`

## Direct Tools vs Scouts

Use this decision table inside the ensemble.

| Condition | Preferred execution |
|-----------|---------------------|
| Cheap file discovery | direct tools |
| Need exact repo anchor | direct tools |
| Need exact symbol/chunk extraction | direct tools first, then `symbol_scout` if needed |
| Need structural path reasoning across several candidates | `dag_scout` or `repo_index_dag_grep` |
| Need ambiguous historical interpretation | memory scouts |
| Need durable continuity context | ACA tools or `aca_context_scout` |

Scouts should be invoked only when their specialization materially reduces
uncertainty or token cost compared with additional direct-tool passes.

## Proposed Tool Contract

Expose a new read-only experimental RLM tool:

```json
{
  "name": "code_search_ensemble",
  "query": "Which files must change to add a new scout role in the legacy agent runtime?",
  "task_type": "file_locate",
  "candidate_paths": ["internal/agent/runtime/runtime.go"],
  "constraints": {
    "exclude_paths": ["internal/rlm/env/**"],
    "include_history": false,
    "include_aca": false,
    "require_grounding": true
  },
  "budget": {
    "max_steps": 6,
    "max_candidates": 8,
    "max_files": 6,
    "max_snippets": 4,
    "max_tokens_out": 3000,
    "allow_scouts": true
  }
}
```

### Input Schema

Required:

- `query`

Optional:

- `task_type`
- `candidate_paths`
- `constraints.exclude_paths`
- `constraints.include_history`
- `constraints.include_aca`
- `constraints.require_grounding`
- `budget.max_steps`
- `budget.max_candidates`
- `budget.max_files`
- `budget.max_snippets`
- `budget.max_tokens_out`
- `budget.allow_scouts`

Notes:

- `task_type` should be caller-supplied when known
- if omitted, the ensemble should classify using a typed planner path
- `candidate_paths` lets callers skip first-pass discovery

## Proposed Output Contract

Return one compact evidence pack.

```json
{
  "summary": "Add a new classic runtime scout role by updating role definitions, prompt/signature wiring, runtime tool bundles, tests, and daemon research enforcement.",
  "task_type": "file_locate",
  "answer_basis": "semantic_search_code + repo_index_search + repo_index_open",
  "confidence": 0.84,
  "files": [
    {
      "path": "internal/agent/types/types.go",
      "why": "declares role constants",
      "support_score": 0.93,
      "confirmed_by": ["semantic_search_code", "repo_index_open"]
    }
  ],
  "symbols": [
    {
      "path": "internal/agent/runtime/runtime.go",
      "symbol": "BuildToolDefsForRole",
      "line": 2142,
      "why": "role-specific runtime tool wiring"
    }
  ],
  "snippets": [
    {
      "path": "internal/runtime/agentprompt/signature.go",
      "start_line": 160,
      "end_line": 220,
      "reason": "runtime instructions for scout roles"
    }
  ],
  "call_paths": [],
  "supporting_context": [],
  "gaps": [],
  "metadata": {
    "lanes_used": ["semantic_code", "repo_index"],
    "grounded": true,
    "scouts_used": []
  }
}
```

### Output Requirements

- `summary` must be compact and grounded
- `answer_basis` must name the winning lane combination
- `files` must contain repo-relative paths only
- `symbols` and `snippets` must reference real files
- `metadata.grounded` must be false if no grounding step occurred
- `supporting_context` is for historical/ACA augmentation only

## Compaction Rules

The ensemble exists to reduce context rot, so its compaction rules are part of
the contract.

### Keep

- verified repo-relative file paths
- small exact symbol lists
- short snippet ranges with line numbers
- one-sentence explanations of why each item matters
- explicit uncertainty and gaps

### Drop

- raw full tool outputs
- duplicate paths across lanes
- unverified semantic-only guesses for exact tasks
- ACA or memory context on purely structural code-location tasks
- more than one snippet per file unless clearly necessary

### Cross-lane promotion

Prefer promoting evidence when at least two signals agree, for example:

- semantic candidate + repo-index verification
- repo DAG path + file open confirmation
- symbol extraction + grep/read confirmation

## Where It Fits

### Experimental RLM surface first

Start in `internal/rlm/env/*`, parallel to `memory_ensemble_retrieve`.

Recommended files:

- `internal/rlm/env/tools.go`
- `internal/rlm/env/adapter.go`
- `internal/rlm/env/code_search_ensemble.go` (new)

Do **not** start by wiring this directly into the broad legacy researcher
prompting path.

### Scouts remain reusable

If the ensemble escalates to scouts, it should use the existing first-class
roles rather than duplicating their prompts internally.

Relevant roles:

- `semantic_scout`
- `dag_scout`
- `symbol_scout`
- `memory_fact_scout`
- `memory_timeline_scout`
- `aca_context_scout`

## Eval Changes

The ensemble needs different evals from isolated scout benchmarking.

### Extend the eval case shape

Add fields for ensemble evaluation:

- `task_type`
- `expected_paths`
- `excluded_paths`
- `expected_symbols`
- `expected_patterns`
- `requires_grounding`
- `allow_history`
- `allow_aca`

### New ensemble metrics

Track:

- `path_recall`
- `path_precision`
- `excluded_path_hits`
- `grounded_before_answer`
- `snippet_match`
- `wrong_scope_penalty`
- `lane_efficiency`
- `context_rot_penalty`
- `tool_cost`
- `duration_ms`

### Lane-quality scoring

Evals should judge the sequence, not just the prose.

Examples:

- `file_locate` should not include ACA or memory lanes by default
- `symbol_inspect` should not use `code_symbols` before a real file exists
- `historical_decision` may use sessions/memory, but should not skip current
  code evidence when answering file-location questions

### Compare three layers

The new eval harness should compare:

1. direct-tool ensemble
2. scout-assisted ensemble
3. broad-agent baseline

This is more meaningful than comparing isolated scouts alone.

## Rollout Plan

### Phase 1: Add the standalone ensemble tool

Implement:

- `code_search_ensemble` tool definition in `internal/rlm/env/tools.go`
- adapter entrypoint in `internal/rlm/env/adapter.go`
- first implementation in `internal/rlm/env/code_search_ensemble.go`

First slice behavior:

- direct tools only
- no scout escalation yet
- support `file_locate`, `execution_trace`, and `symbol_inspect`
- enforce grounding
- return normalized evidence packs

### Phase 2: Add scout escalation

Add optional escalation when:

- direct lanes cannot ground enough evidence
- the planner marks uncertainty high
- the budget allows it

Start with:

- `dag_scout`
- `symbol_scout`

### Phase 3: Add historical and continuity augmentation

Support:

- `historical_decision`
- `continuity_context`

Use:

- semantic session/memory/context lanes
- memory/ACA scouts only when explicitly allowed

### Phase 4: Upgrade evals

Extend `eval agents` or add a sibling eval mode so ensemble outputs are scored
on:

- exact path quality
- wrong-scope retrieval
- grounding
- context compactness
- cost and latency

### Phase 5: Controller integration

Let higher-level retrieval/controller logic choose when to call
`code_search_ensemble` instead of raw search tools.

This should remain optional and gated until evals show real lift over the
current direct-tool path.

## Open Questions

### Should `code_search_ensemble` live under `internal/rlm/env` or a reusable retrieval package?

Short answer:

- start in `internal/rlm/env` for speed and symmetry with
  `memory_ensemble_retrieve`
- extract later if it proves useful beyond RLM

Possible extraction targets later:

- `internal/intelligence/retrieval/codesearchensemble/`
- `internal/rlm/codesearchensemble/`

### Should the ensemble call tools directly or always subcall scouts?

Default:

- direct tools first
- scouts only as escalation

Reason:

- cheaper
- easier to evaluate
- easier to debug

### Should code search ever include ACA by default?

No.

ACA should be opt-in for `continuity_context` or explicitly continuity-shaped
questions.

## Success Criteria

The ensemble is successful if it improves all of the following at once on
repo-grounded coding tasks:

- higher exact path recall than the broad-agent baseline
- lower wrong-scope retrieval rate
- lower token cost than broad-agent fanout
- lower context-rot penalty in the returned evidence pack
- explicit grounding on exact tasks

## Initial Implementation Slice

Build the smallest useful slice first:

1. standalone `code_search_ensemble` tool in `internal/rlm/env/*`
2. support `file_locate`, `execution_trace`, `symbol_inspect`
3. direct-tool stages only
4. grounded evidence-pack output
5. ensemble-aware eval cases with `task_type`, `expected_paths`, and
   `excluded_paths`

Do **not** start with scout fanout, historical context, or ACA augmentation in
the first slice.
