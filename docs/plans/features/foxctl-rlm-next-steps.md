# AgentCTL RLM Next Steps

Status: in progress

## Why This Exists

The experimental RLM runtime is past basic scaffolding:

- the runtime loop exists
- tools are exposed with machine-usable schemas
- LM Studio can call them
- retrieval evals can score the retrieved paths

The next problem is no longer tool plumbing. It is planner architecture.

The current free-form loop is:

```text
query
  -> model sees full tool bag
  -> model chooses tools
  -> tool results come back
  -> loop continues
  -> final answer + retrieved paths
```

That is a good v0, but it leaves search strategy mostly implicit.

## Current Evidence

From the current evals:

- `rlm_llm` improves over the zero-tool baseline, but remains weak
- `rlm_llm_codeintel` collapses when precision tools are the only lane
- ACA-only currently outperforms both direct repoindex modes and RLM on the mixed suites
- the mixed ACA + code lane is still practically stronger than the current RLM controllers

For the current benchmark snapshot, see:

- [rlm-retrieval-findings.md](rlm-retrieval-findings.md)

Interpretation:

- discovery and narrowing are still necessary
- precision tools should not replace discovery
- the runtime needs route-aware phases instead of one flat tool bag

## Direction

Move the runtime from:

- free-form tool choice only

to:

- routed + staged execution for benchmarked query types

Keep free mode for experimentation, but add staged mode for stable retrieval work.

## Route Profiles

Planned route profiles:

- `code_retrieval`
- `memory_recall`
- `mixed`
- `evidence_audit`

Current implemented slice:

- `code_retrieval`

## Planning Modes

Supported or planned modes:

- `free`
  - model sees one full tool set
  - best for experimentation
- `guided`
  - same tools, but stronger prompt steering
- `staged`
  - tool availability changes by phase
  - primary target for stable evals
- `hard`
  - near-fixed skeleton for regression tests and control experiments

Current implemented slice:

- `free`
- `staged` for `code_retrieval`

## First Staged Route: `code_retrieval`

The first staged plan uses:

### Phase 1: Discovery

Goal:

- find likely repository files or canonical notes

Allowed tools:

- `semantic_search_code`
- `smart_search_code`
- `search_repo`
- `search_vault`

Required tool group:

- at least one of:
  - `semantic_search_code`
  - `smart_search_code`
  - `search_repo`

### Phase 2: Inspection

Goal:

- open and inspect the strongest candidates

Allowed tools:

- `load_file`
- `read_note`
- `ripgrep_code`
- `expand_repo_graph`

Required tool group:

- at least one of:
  - `load_file`
  - `read_note`

### Phase 3: Verification

Goal:

- cross-check the strongest candidate and confirm the best supporting paths

Allowed tools:

- `load_file`
- `read_note`
- `ripgrep_code`
- `expand_repo_graph`

Required tool group:

- at least one verification tool

### Final Synthesis

Goal:

- produce the final answer using only paths and notes gathered in prior phases

Allowed tools:

- none

## Why This Shape

The key lesson from the current evals is:

- discovery cannot be replaced by precision

So the runtime must explicitly support:

```text
discovery
  -> inspection
    -> verification
      -> synthesis
```

That is a better fit for both retrieval evals and future scene-thread reasoning.

## Tool Grouping Principles

### Discovery tools

Broad search tools:

- semantic or blended search
- repo graph search
- vault search

### Inspection tools

High-information tools:

- file loading
- note reading
- exact grep
- graph expansion

### Verification tools

Cross-check tools:

- exact file re-open
- graph expansion
- literal or structural grep

## File-Level Implementation Map

Current implementation lives in:

- [internal/rlm/plan.go](../../../internal/rlm/plan.go)
  - route profile and phase definitions
- [internal/rlm/llm_runner.go](../../../internal/rlm/llm_runner.go)
  - staged execution
  - per-phase prompt shaping
  - final synthesis
- [cmd/foxctl/cmd/rlm.go](../../../cmd/foxctl/cmd/rlm.go)
  - `--route-profile`
  - `--plan-mode`
- [cmd/foxctl/cmd/eval.go](../../../cmd/foxctl/cmd/eval.go)
  - `rlm_llm_code_staged` eval mode

Supporting tool surface:

- [internal/rlm/env/tools.go](../../../internal/rlm/env/tools.go)
- [internal/rlm/env/adapter.go](../../../internal/rlm/env/adapter.go)
- [internal/rlm/env/tool_profiles.go](../../../internal/rlm/env/tool_profiles.go)

## Evaluation Additions

The staged path should be compared against:

- `rlm_llm`
- `rlm_llm_codeintel`
- `rlm_llm_code_staged`

And later measured on:

- first useful tool chosen
- tool calls before first relevant path
- per-phase completion rate
- dead-end rate

## What Success Looks Like

For the first staged route, success is not “perfect general RLM.”

Success is:

- the runtime consistently enters discovery first
- inspection tools are actually used after candidate discovery
- retrieved path quality improves over free mode on at least one suite
- failures become phase-legible instead of opaque

## Likely Next Steps After This Slice

1. Add explicit route selection metrics to eval output.
2. Improve `code_retrieval` phase prompts based on actual tool traces.
3. Add a staged `memory_recall` route over scenes, threads, and artifacts.
4. Add a hard-routed benchmark mode for regression tests.
