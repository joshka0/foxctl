# Companion RLM Continuity Harness

Status: draft  
Owner: companion / contextplane / rlm  
Scope: `internal/companion`, `internal/rlm`, `internal/context/contextplane`, `internal/web/api`, `packages/gui-agent`

## Goal

Turn the current companion continuity controller into a real RLM-style continuity harness that:

1. treats OpenViking-style `L0/L1/L2` as the core progressive context model, with higher continuity lanes layered explicitly on top
2. uses `TopOfMind` and task continuity as first-class harness inputs
3. supports bounded recursive subcalls for smaller subproblems
4. adopts a Jupyter-like exploration model where the controller can inspect, transform, and reuse state across bounded steps
5. exposes enough observability in the API and GUI to understand which layer or subcall produced an answer

## Non-Goals

- Replace the existing companion chat engine with a general-purpose sandboxed code runner in one step
- Allow unrestricted tool execution or unconstrained recursion
- Hide all harness mechanics from operators; the first versions should bias toward debuggability
- Make every companion turn pay the cost of deep recursive retrieval

## Terminology

OpenViking’s formal layering is `L0/L1/L2`:

- `L0`: abstract / quick relevance
- `L1`: overview / planning context
- `L2`: full detail / on-demand load

In this plan, earlier shorthand like `L3-L5` should be read as `agentctl` continuity extensions above the OpenViking core, not as something OpenViking itself defines.

For this harness, the effective lane model is:

- `L0` visible turn abstraction / quick continuity signal
- `L1` recent recap / overview
- `L2` detailed companion/session memory when needed
- `L3` `TopOfMind`
- `L4` task continuity
- `L5` recalled sessions / ACA durable retrieval / recursive subcalls

## Current State

Implemented:

- `L0` visible recent turns
- `L1` deterministic recent recap
- ACA-backed `TopOfMind` injection
- ACA task continuity injection
- an RLM-style controller that can bias toward visible history
- structured continuity metadata in the companion response and GUI debug surface

Still missing:

- explicit per-lane source accounting in the runtime contract
- a true recursive subcall lane for continuity and retrieval
- a Jupyter-style scratch state / variable model
- bounded handle-based subagent result composition
- controller routing across all lanes instead of mostly `visible_history` vs `durable_memory`

## Design Principles

### 1. Data First, Prompt Second

The harness should improve the data contract before adding more wording.

That means:

- structured machine-generated state over prose summaries when possible
- explicit layer sections and metadata
- explicit controller decisions and subcall traces
- bounded state representations instead of free-form tool transcripts

### 2. Jupyter-Style Exploration

The article’s useful idea is not “use Python everywhere”, but:

- the model sees a bounded external state, not the entire universe inline
- it can inspect slices of that state incrementally
- intermediate results persist across bounded steps
- final answers may be generated text or structured values composed from intermediate bindings

For `agentctl`, the analogous runtime is:

- structured harness state
- bounded read-only tools
- optional subcalls
- scratch bindings kept in turn-local controller state
- truncated inspection outputs

### 3. Recursive Subcalls Must Return Handles, Not Full Context Dumps

Subagent/subcall outputs should not be naively pasted back into the parent prompt.

Instead they should return:

- a compact summary
- typed result metadata
- evidence refs / CAS artifact refs
- optionally a small inline value when the payload is tiny

This preserves the “symbol/variable” property from the article.

### 4. Layered Routing Must Be Explicit

The controller should choose among:

- `L0 visible_history`
- `L1 recap`
- `L2 companion layered memory`
- `L3 top_of_mind`
- `L4 task_continuity`
- `L5 recalled_sessions / durable_memory / ACA retrieval`
- `subcall`

The answer path should record which lanes were used.

## Proposed Runtime Shape

### Harness Inputs

Each turn should assemble a machine-readable harness state object roughly like:

```json
{
  "l0_visible_history": {...},
  "l1_recent_recap": {...},
  "l2_companion_memory": {...},
  "l3_top_of_mind": {...},
  "l4_task_continuity": {...},
  "l5_durable_context": {...},
  "workspace": {...},
  "limits": {
    "max_steps": 4,
    "max_subcalls": 2,
    "max_chars_per_inspect": 1200
  }
}
```

### Controller Step

Before the answer turn, run a bounded controller step that returns:

```json
{
  "source": "visible_history|top_of_mind|task_continuity|durable_memory|combined|subcall",
  "reason": "...",
  "visible_summary": "...",
  "memory_query": "...",
  "subcalls": [
    {
      "kind": "continuity|session|aca|repo|vault",
      "prompt": "...",
      "budget": {"max_steps": 2, "max_subcalls": 0}
    }
  ]
}
```

The main answer turn should then run with only the lanes/tools the controller selected.

## Jupyter / REPL Mapping

The article’s Jupyter framing maps well to a future `agentctl` harness:

### Needed Semantics

- a turn-local scratch state
- inspect commands that show truncated output
- persistent bindings across controller steps in one turn
- explicit `FINAL(value)` semantics for structured results

### `agentctl`-native Mapping

- scratch bindings live in a bounded in-memory turn state, not a real notebook kernel at first
- inspect actions are explicit helper tools or structured lane reads
- large values become CAS artifacts with handles
- `FINAL(value)` maps to a typed structured controller result handed to the answer phase

### Why This Matters

Without this, the controller is still mostly:

- prompt → tool result → prompt → final prose

With it, the controller becomes:

- inspect lane
- bind intermediate result
- optionally subcall
- compose answer inputs from bindings
- finalize

That is much closer to the article’s “variables in a notebook” mental model.

## Recursive Subagent / Subcall Design

### Existing Substrate

The repo already has a recursive substrate:

- `internal/rlm/env/tools.go` defines `subcall`
- `internal/rlm/env/adapter.go` already supports bounded recursive callback wiring
- `internal/rlm/inspect_runner.go` already demonstrates subcall usage

### What To Add

For companion continuity and hard retrieval questions, add a `llm_query`-style alias over `subcall` with stricter shaping:

- one prompt string
- optional lane hint
- bounded step budget
- returns typed result, summary, evidence refs, and optional artifact digest

Example parent-visible result:

```json
{
  "summary": "Found prior session about Japan travel planning and hotel comparison.",
  "value_ref": "cas:sha256:...",
  "evidence_refs": ["session:abc", "artifact:def"],
  "lane": "l5_durable_context"
}
```

### Subcall Policy

- only allow subcalls from the controller phase, not the final answer phase
- keep recursion shallow at first: `max_depth <= 2`
- prefer parallel subcalls only for obviously independent branches
- do not inline large child outputs into the parent context

## Phased Implementation

### Phase 1 — Explicit Layer Contract

Goal: make the OpenViking-style `L0/L1/L2` core plus `agentctl` continuity extensions explicit in the companion/runtime contract.

Changes:

- add a structured `HarnessState` model in `internal/companion`
- replace ad hoc string sections with typed lane payloads internally
- add response/debug metadata:
  - `layer_hits`
  - `controller_source`
  - `subcall_count`
  - `used_top_of_mind`
  - `used_task_continuity`

Files:

- `internal/companion/service.go`
- `internal/companion/personality.go`
- `packages/gui-agent/src/api/client.ts`
- `packages/gui-agent/src/components/conversations/*`

### Phase 2 — ACA / Task Continuity First-Class Lanes

Goal: treat `TopOfMind` and task continuity as actual harness lanes, not just extra prompt text.

Changes:

- normalize `TopOfMind` into a typed lane payload
- normalize task continuity pack into a typed lane payload
- expose lane-specific summaries plus refs/artifact digests
- include lane provenance in `ChatResponse`

Files:

- `internal/context/contextplane/store.go`
- `internal/context/contextplane/taskhistory/*`
- `internal/web/api/companion.go`
- `internal/companion/service.go`

### Phase 3 — Bounded RLM Controller State

Goal: give one turn a tiny notebook-like scratch state.

Changes:

- add turn-local bindings for controller outputs
- add bounded inspect/result truncation rules
- add typed structured finalize path

Likely shape:

- `controller_state.bindings["visible_summary"]`
- `controller_state.bindings["task_pack_ref"]`
- `controller_state.bindings["session_ref"]`

Files:

- `internal/companion/service.go`
- new `internal/companion/controller_state.go`

### Phase 4 — Recursive Subcall Lane

Goal: allow the controller to delegate harder continuity/retrieval problems.

Changes:

- add companion-facing `llm_query` or `subcall_continuity` tool alias
- wire to existing `internal/rlm/env/adapter.go` subcall substrate
- support child result handles and summaries
- enforce depth and budget limits

Files:

- `internal/rlm/env/adapter.go`
- `internal/rlm/env/tools.go`
- `internal/companion/service.go`
- potentially `cmd/agentctl/cmd/rlm.go`

### Phase 5 — Parallel Branches for Independent Retrieval

Goal: support multiple orthogonal subproblems without context rot.

Examples:

- session recall
- ACA note recall
- repo anchor search
- vault note lookup

Changes:

- allow controller to request multiple subcalls
- run them concurrently with bounded fanout
- return compact per-branch summaries and refs

### Phase 6 — Final Composition Semantics

Goal: support both prose answers and structured-value outputs.

Changes:

- answer mode: plain conversational synthesis
- value mode: structured payload / JSON / artifact handle
- keep “construct answer into value, not tokens” as an explicit future lane for non-conversational tasks

## API / GUI Additions

### Response Metadata

Extend `ChatResponse` continuity/debug payload with:

```json
{
  "continuity": {
    "source": "visible_history",
    "layer_hits": ["L0", "L1", "L3"],
    "subcalls": 0,
    "visible_summary": "...",
    "memory_query": "...",
    "artifact_refs": ["cas:sha256:..."]
  }
}
```

### Inspector / Debug View

Add:

- layer hit badges
- controller decision
- subcall count
- artifact refs
- selected-message continuity metadata

## Tests

### Unit

- layer assembly with and without `TopOfMind`
- task continuity provider success / failure
- controller source selection parsing
- subcall result shaping
- truncation behavior for inspect outputs

### Integration

- visible-history continuation
- workspace-only `TopOfMind` question
- task continuity question with active task
- recursive subcall over session recall / ACA

### Regression

- local LMStudio continuation should not regress to `rlm_context_query` for visible-history-only follow-ups

## Immediate Next Steps

1. Add `layer_hits` / `subcall_count` / `artifact_refs` to the continuity metadata.
2. Convert current prompt sections into typed harness lanes internally.
3. Introduce a bounded turn-local scratch state for controller bindings.
4. Add a companion-facing `llm_query` / `subcall` lane over the existing RLM substrate.
5. Add one end-to-end test where the controller delegates a smaller retrieval problem and composes the result without dumping the full child trace into the parent.

## Repo Touchpoints

- `internal/companion/service.go`
- `internal/companion/personality.go`
- `internal/context/contextplane/store.go`
- `internal/context/contextplane/orienter.go`
- `internal/context/contextplane/taskhistory/taskhistory.go`
- `internal/context/contextplane/taskhistory/render.go`
- `internal/rlm/llm_runner.go`
- `internal/rlm/env/adapter.go`
- `internal/rlm/env/tools.go`
- `internal/web/api/companion.go`
- `packages/gui-agent/src/api/client.ts`
- `packages/gui-agent/src/components/conversations/ConversationsList.tsx`
- `packages/gui-agent/src/components/conversations/ConversationInspector.tsx`

## Success Criteria

We should consider the harness “real” when:

1. a continuation question can answer from `L0/L1` without any tool calls
2. a workspace-state question can answer from `TopOfMind` / task continuity without fake “no context keys” fallback
3. a hard continuity question can trigger a bounded recursive subcall
4. the parent answer uses child summaries/refs instead of absorbing the whole child trace
5. the API and GUI show which layers and subcalls were involved
