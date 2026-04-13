# ACA Retrieval Guidance For Code Search Ensemble

Status: proposed active plan
Owner: contextplane / obsidian / rlm
Last Updated: 2026-03-23

## Goal

Use ACA *(the control plane plus Obsidian wiki-link knowledge plane)* as a
durable retrieval-guidance layer for `code_search_ensemble`, so models need
less search effort without replacing grounded repo truth.

The target outcome is:

1. repo/code retrieval remains the exact current source of truth
2. ACA stores reusable retrieval guidance and bridge abstractions
3. `code_search_ensemble` consults ACA only when ambiguity remains
4. small models reason over compact grounded evidence plus ACA hints, rather
   than rediscovering the repo from scratch

This plan is explicitly **not** about adding repo-specific hacks such as
"if `skills/*/main.go` then add sibling `skill.yaml`".

## Core Decision

### ACA is a guidance plane, not a truth replacement

ACA should store:

- durable bridge patterns
- canonical skill/runtime/package notes
- repeated retrieval corrections
- typed wiki-link relationships
- recommended companion artifacts

ACA should not replace:

- grounded repo paths
- current symbol locations
- current call/reference structure
- exact snippets used in the final answer

The correct architecture is:

```text
repo/code plane
  -> current exact truth

ACA / Obsidian knowledge plane
  -> durable abstractions and retrieval guidance

smart ensemble
  -> chooses when ACA should bias retrieval or pruning
```

### The model should be injected at ambiguity points

The small reasoning model should not act as a broad repo searcher.

Use it only when:

- multiple candidates have similar support
- bridge candidates need disambiguation
- the system must decide what to keep vs prune
- ACA hints conflict with current grounded repo evidence

That means:

- deterministic retrieval first
- ACA guidance second
- small model last, and only on compact evidence

## Problem Statement

The current deterministic `code_search_ensemble` is now strong on direct code
and bridge-style traces, but harder cases still expose a structural gap:

- current code retrieval can find the local truth
- repeated task shapes still require too much rediscovery
- models still have to infer recurring bridge/companion patterns from scratch

Examples:

- a runtime dispatch chain vs a registration chain
- a skill implementation note vs its manifest note
- `RunAgentctlSkillDecode(..., "<skill>", ...)` style bridges
- implementation files vs exposure files vs structural support files

These are exactly the kinds of patterns ACA should remember and expose.

## Non-Goals

- Do not store the full current repo graph inside ACA.
- Do not let ACA override grounded repo evidence.
- Do not hardcode repo-specific promotion rules into `code_search_ensemble`.
- Do not require semantic note search for correctness.
- Do not make wiki-links the only reasoning surface for code questions.

## Design Principles

### 1. Durable guidance must be typed

ACA guidance should be represented as canonical note types plus frontmatter,
not as vague prose only.

### 2. Repo evidence still wins

If ACA hints disagree with grounded repo evidence, the repo wins.

### 3. Promotion must come from repeated evidence

Guidance notes should be created from:

- repeated retrieval misses
- repeated adjudicated traces
- bridge correction runs
- manual review and merge

not from one-off guesses.

### 4. Guidance should reduce effort, not increase scope

ACA hints should narrow search and reduce context volume, not widen it.

### 5. Ambiguity handling belongs in the ensemble

ACA should not independently decide answers.
`code_search_ensemble` decides when to consult ACA and how much weight to give
it.

## ACA Note Types

Add canonical note families such as:

### `pattern/dispatch/<name>`

Purpose:

- explain recurring dispatch/bridge patterns

Examples:

- runtime tool exposure vs direct dispatch
- adapter dispatch
- `RunAgentctlSkillDecode(...)`

Suggested frontmatter:

```yaml
type: pattern
pattern_kind: dispatch
trust: canonical
paths:
  - internal/rlm/env/adapter.go
symbols:
  - internal/rlm/env/adapter.go::ReadOnlyAdapter.Execute
bridge_queries:
  - ReadOnlyAdapter Execute code_search_ensemble
retrieval_kinds:
  - direct_dispatch
  - exposure
companion_paths:
  - internal/rlm/env/code_search_ensemble.go
repo_docs:
  - docs/plans/features/code-search-ensemble.md
```

### `skill/<name>`

Purpose:

- tie together skill implementation, manifest, and canonical usage note

Suggested frontmatter:

```yaml
type: skill
skill_name: code/semantic_search
trust: canonical
paths:
  - skills/code_semantic_search/main.go
  - skills/code_semantic_search/skill.yaml
symbols:
  - skills/code_semantic_search/main.go::main
  - skills/code_semantic_search/main.go::Input
retrieval_kinds:
  - implementation
  - manifest
companion_paths:
  - skills/code_semantic_search/skill.yaml
```

### `runtime/tool/<tool>`

Purpose:

- tie a tool name to direct dispatch, exposure, and structural support notes

Suggested frontmatter:

```yaml
type: runtime_tool
tool_name: code_search_ensemble
trust: canonical
paths:
  - internal/rlm/env/adapter.go
  - internal/rlm/env/code_search_ensemble.go
symbols:
  - internal/rlm/env/adapter.go::ReadOnlyAdapter.Execute
  - internal/rlm/env/code_search_ensemble.go::ReadOnlyAdapter.codeSearchEnsemble
retrieval_kinds:
  - direct_dispatch
  - implementation
companion_paths:
  - cmd/agentctl/cmd/eval_code_search_ensemble.go
bridge_queries:
  - ReadOnlyAdapter Execute code_search_ensemble
```

### `bridge/<query-shape>`

Purpose:

- hold proven reusable bridge patterns from correction runs

Suggested frontmatter:

```yaml
type: bridge
bridge_kind: code_trace
trust: reviewed
seed_query: Which files connect runSingleCodeSearchEnsembleEval to code_search_ensemble execution?
bridge_queries:
  - ReadOnlyAdapter Execute code_search_ensemble
paths:
  - cmd/agentctl/cmd/eval_code_search_ensemble.go
  - internal/rlm/env/adapter.go
  - internal/rlm/env/code_search_ensemble.go
retrieval_kinds:
  - direct_dispatch
  - implementation
```

## Wiki-Link Model

Use wiki-links for durable semantic relations such as:

- `[[pattern/dispatch/adapter-dispatch]]`
- `[[runtime/tool/code-search-ensemble]]`
- `[[skill/code-semantic-search]]`
- `[[bridge/run-agentctl-skill-decode]]`

The note text should explain the pattern briefly, but the retrieval-critical
signals must remain in frontmatter:

- `paths`
- `symbols`
- `bridge_queries`
- `retrieval_kinds`
- `companion_paths`

That allows ACA retrieval to use the note graph while keeping ranking
deterministic and inspectable.

## Promotion Sources

These ACA notes should be promoted from existing signal lanes already present in
the system:

### 1. Retrieval correction runs

Use:

- `RetrievalCorrectionRun`
- `GraphCorrectionRun`

from [types.go](../../../internal/context/contextplane/types.go).

Promotion trigger:

- repeated miss or ranking mismatch for the same bridge/task family

### 2. `code_search_ensemble` candidate traces

Use:

- `candidate_trace`
- `bridge_queries`
- selected vs pruned candidates

Promotion trigger:

- same bridge pattern is repeatedly adjudicated the same way

### 3. Reviewed docs/graph notes

Use:

- repo graph draft bundle
- bridge reconcile/apply
- canonical note review flow

Promotion trigger:

- pattern is stable enough to deserve a canonical note

## Ensemble Integration Points

`code_search_ensemble` should consult ACA at three points.

### A. Trace planning

After first-pass deterministic retrieval, if the task is:

- `execution_trace`
- `change_impact`
- `registration_trace`

and ambiguity remains, query ACA for:

- matching `bridge_queries`
- matching `paths`
- matching `symbols`
- related canonical pattern notes

ACA output should bias:

- which bridge query to run next
- which relation class to follow

### B. Companion expansion

When a grounded file is selected, ACA may suggest:

- companion manifest file
- canonical implementation note
- related bridge note

This should be treated as a candidate expansion, not an automatic keep.

### C. Prune/keep disambiguation

When `candidate_trace` contains multiple similar candidates, ACA note guidance
may bias the keep/drop decision.

Examples:

- prefer `direct_dispatch` over `exposure`
- prefer `implementation` over `registration`
- prefer canonical skill implementation + manifest companions

## Smart Retrieval Flow

The planned smart flow becomes:

1. deterministic retrieval
2. candidate trace + bridge query extraction
3. ACA lookup seeded by `paths`, `symbols`, and `bridge_queries`
4. optional small-model decision over:
   - grounded snippets
   - ACA hint notes
   - candidate trace
5. final grounded evidence pack

The small model should never receive the whole repo.
It should receive only the compact evidence pack plus ACA hints.

## Minimal First Slice

### Slice 1: ACA guidance note schema

Add canonical note conventions and draft generation for:

- `pattern/dispatch/*`
- `skill/*`
- `runtime/tool/*`

No ensemble behavior change yet.

### Slice 2: Promotion from correction runs

When retrieval correction runs repeat the same bridge/miss shape:

- draft an ACA guidance note into the inbox
- include `paths`, `symbols`, `bridge_queries`, `retrieval_kinds`

No automatic promotion.

### Slice 3: Ensemble ACA hook

Add an ACA lookup step inside `code_search_ensemble` for ambiguous
`execution_trace` queries.

Input seeds:

- selected grounded `paths`
- selected `symbols`
- `bridge_queries`

### Slice 4: Smart prune with ACA hints

Only after slices 1-3:

- add a small-model prune/keep gate
- it sees deterministic evidence plus ACA hints

## Evaluation Plan

Use three comparison modes:

1. deterministic ensemble only
2. deterministic ensemble + ACA guidance
3. deterministic ensemble + ACA guidance + small-model prune

Measure:

- path recall
- wrong-scope rate
- direct-dispatch vs exposure classification quality
- token use
- whether ACA guidance reduces needed widening steps

Good benchmark families:

- bridge traces
- skill implementation + manifest pairs
- runtime exposure vs direct dispatch
- repeated retrieval corrections promoted into notes

## Risks

### Over-promotion

Risk:

- ACA fills with brittle or low-value bridge notes

Mitigation:

- require repeated evidence
- keep promotion inbox-first
- review-gate canonical notes

### ACA overriding repo truth

Risk:

- stale ACA guidance biases selection away from current code

Mitigation:

- grounded repo evidence always wins
- ACA can bias, not override

### Context bloat

Risk:

- ACA hints widen the evidence pack too much

Mitigation:

- expose only the top 1-3 ACA guidance notes
- feed summaries + frontmatter, not full note bodies, into the small model by
  default

## Concrete Implementation Targets

Likely code touch points:

- [retrieval.go](../../../internal/context/contextplane/retrieval.go)
- [store.go](../../../internal/context/contextplane/store.go)
- [store.go](../../../internal/storage/obsidianindex/store.go)
- [graph.go](../../../internal/tools/obsidian/graph.go)
- [bridge.go](../../../internal/tools/obsidian/bridge.go)
- [code_search_ensemble.go](../../../internal/rlm/env/code_search_ensemble.go)

Likely docs touch points:

- [context-architecture.md](../../architecture/context-architecture.md)
- [embedding-rebuilds.md](../../general/embedding-rebuilds.md)

## Success Criteria

This plan is working when:

1. repeated bridge patterns produce ACA guidance notes instead of repeated ad
   hoc ranking tweaks
2. `code_search_ensemble` needs fewer repo searches on repeated trace families
3. smaller models can choose the correct keep/drop set more often from compact
   evidence
4. answers remain grounded in current repo files and snippets

