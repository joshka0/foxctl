# Slop Function Detection Plan

Status: active plan

Owner: agentctl

Last updated: 2026-03-30

## Goal

Make `slop` a user-facing label for structural cleanup hotspots while keeping the
detector itself deterministic, typed, and testable.

In this plan, `slop` means incidental complexity *(more precisely: duplicated
control-flow recovery, repeated error remapping, retry ladders, and
policy-transport mixing inside a single function)*. It does **not** mean
"anything that looks ugly" and it must not be implemented with ad hoc keyword
matching.

The intended outcome is:

- local deterministic detection via `refactor scout`
- optional second-stage explanation or reranking via `refactor advisor`
- findings that explain *why* a function was flagged, not just that it "feels
  sloppy"

## Decision

Do **not** add a brand-new primary `code/slop` detection stack in the first
pass.

Instead:

- extend `code/refactor_scout` with a `slop` finding family
- preserve `code/refactor_advisor` as the optional model-backed second stage
- optionally add a convenience CLI wrapper such as `agentctl refactor slop`
  later if the UX proves useful

This keeps one deterministic hotspot engine as the source of discovery truth and
avoids building a second overlapping analysis pipeline beside the existing
refactor scout/advisor workflow.

## Current State

Existing adjacent capabilities already cover part of the problem:

- `code/refactor_scout` already emits deterministic structural findings such as:
  - `duplicate_orchestration_fingerprint`
  - `same_file_extraction_candidate`
  - `function_hotspot`
- `code/refactor_advisor` already acts as a second-stage model-backed shortlist
  ranker over scout findings.
- `workflows/code-review.yaml` currently surfaces complexity and TODO-style
  checks, but not a dedicated structural slop pass.
- Go has AST-backed structural metrics in current analysis paths.
- TypeScript/JavaScript support is useful for call-family and extraction-style
  seams, but current complexity analysis elsewhere in the repo is still weaker
  than Go and should not be treated as AST-precise slop detection yet.

This means the repo already has the right architectural seam:

- deterministic scout first
- optional model second
- no keyword heuristics

## Why This Needs a Separate Family

The current scout is good at generic refactor seams, but `slop` needs a more
specific vocabulary around intra-function cleanup patterns:

- duplicated recovery branches
- repeated `status == sentinel` guard ladders
- repeated error translation blocks
- retry/fallback state reassignment ladders
- functions that mix transport, auth policy, fallback policy, and terminal error
  shaping in one body

Those are related to current duplicate-orchestration findings, but not identical
to them. The new family should therefore:

- reuse existing scout infrastructure
- reuse existing hotspot synthesis when possible
- add more specific typed rules where the evidence is meaningfully different

## Design Invariants

1. `refactor scout` remains the deterministic discovery source of truth.
2. `slop` is a presentation label over typed structural findings, not a fuzzy
   keyword category.
3. No rule may rely on substring triggers such as `retry`, `auth`, `slop`,
   `workaround`, or `hack`.
4. Every finding must include concrete evidence fields that support the score.
5. Small-model classification is optional and advisory only; it must not replace
   local discovery.
6. Language precision must be explicit. Go can ship first; TypeScript/JavaScript
   must not claim AST-grade confidence until the parser path is real.
7. Existing envelope and artifact behavior stays unchanged.

## Recommended User-Facing Shape

Keep the current entrypoints and add a narrow selector for this family.

Recommended first-pass command surface:

```bash
agentctl refactor scout --path ./internal --language go --focus slop
agentctl refactor advisor --path ./packages/gui-agent --language typescript --focus slop
```

If `--focus` feels too narrow for the long term, it can be generalized later to
`--finding-family` or `--rule-group`. The first pass should optimize for
clarity, not taxonomy completeness.

Recommended skill-level addition:

- add optional scout/advisor input field:
  - `focus: "all" | "slop"`

Behavior:

- `focus=all` preserves current behavior
- `focus=slop` filters or boosts slop-family findings while keeping the same
  output envelope shape

## Rule Model

### Reuse Existing Rules

The following existing rules should count toward `slop` when they match:

- `duplicate_orchestration_fingerprint`
- `same_file_extraction_candidate`
- `function_hotspot`

This keeps backward compatibility and avoids re-encoding the same signal under
different names.

### Add New Slop-Specific Rules

Add the following typed rules where current findings are too coarse:

1. `duplicate_recovery_block`
   - Detect repeated normalized recovery/remap subtrees in the same function.
   - Primary evidence:
     - normalized subtree hash
     - duplicate count
     - repeated span starts

2. `repeated_guard_ladder`
   - Detect repeated guards against the same subject/sentinel pair, such as
     repeated `response.status == 401` checks across sequential branches.
   - Primary evidence:
     - guard subject fingerprint
     - sentinel value
     - occurrence count

3. `retry_ladder`
   - Detect repeated reassignment of the same result variables from equivalent
     call shapes across fallback/retry branches.
   - Primary evidence:
     - assigned identifiers
     - call fingerprint
     - branch count

4. `duplicated_error_remap`
   - Detect repeated translation from the same external error predicate to the
     same domain error constructor.
   - Primary evidence:
     - predicate fingerprint
     - constructor fingerprint
     - duplicate count

5. `policy_transport_mixing`
   - Detect functions that combine transport execution, auth or permission
     policy, fallback policy, and terminal error shaping in one body.
   - Primary evidence:
     - phase vector
     - transport call count
     - branch density
     - remap count

6. `slop_hotspot`
   - Composite finding emitted when multiple slop-family rules accumulate on the
     same symbol.
   - Primary evidence:
     - contributing rules
     - rule scores
     - structural/supportive mix

## Evidence Model

Findings should remain scout findings, but they need more explicit evidence so
they can be trusted in review and tested with golden fixtures.

Recommended additional evidence keys:

- `normalized_subtree_hash`
- `duplicate_span_lines`
- `guard_subject`
- `guard_operator`
- `guard_literal`
- `call_fingerprint`
- `assigned_result_vars`
- `predicate_fingerprint`
- `constructor_fingerprint`
- `phase_vector`
- `contributing_rules`

Example shape:

```json
{
  "rule_id": "duplicate_recovery_block",
  "category": "function",
  "severity": "high",
  "score": 84,
  "title": "Function repeats the same recovery block",
  "detail": "fetchFoo repeats a structurally equivalent recovery/remap block twice, which suggests one local helper should own the timeout and retry remapping path.",
  "file": "packages/gui-agent/src/api/client.ts",
  "line": 143,
  "symbol": "fetchFoo",
  "language": "typescript",
  "signals": ["normalized_subtree", "recovery_duplication"],
  "evidence": {
    "normalized_subtree_hash": "abc123",
    "duplicate_span_lines": [148, 166],
    "duplicate_count": 2
  }
}
```

## Scoring

Scoring should stay deterministic and compositional.

Recommended weighting:

- duplicate recovery or remap evidence should dominate
- repeated guard and retry ladder evidence should be medium-weight
- policy/transport mixing should raise the score when paired with other signals
- raw complexity alone should not create a slop finding, but it should raise the
  score of an existing slop candidate

High-level scoring shape:

```text
slop_score =
  structural_duplication
  + guard_repetition
  + retry_ladder
  + error_remap_duplication
  + policy_transport_mix
  + complexity_boost
```

Where:

- the first five terms come from typed rule evidence
- `complexity_boost` reuses existing scout structural metrics rather than
  inventing a second complexity system

## Language Strategy

### Phase 1

Ship Go first.

Reason:

- Go already has reliable AST-backed structural analysis in adjacent code paths
- the refactor scout is already strongest in Go
- the initial heuristics can be precise enough to golden-test

### Phase 2

Add TypeScript/JavaScript with a real parser-backed path.

Requirements:

- do not implement slop detection with regex-only JS/TS scanning
- prefer existing parser infrastructure already used by the scout where
  possible
- emit explicit confidence differences until the TS/JS rule family reaches the
  same evidence depth as Go

### Phase 3

Evaluate whether Python and Elixir meaningfully benefit from the same family or
whether the term should stay scoped to Go and TS/JS.

## Advisor Role

The small-model path is still useful, but only after local narrowing.

Recommended advisor flow:

1. run `code/refactor_scout` locally with `focus=slop`
2. take the top `N` findings
3. ask the advisor to:
   - rank which slop findings are worth fixing first
   - explain the likely extraction boundary
   - suggest whether the cleanup is local helper extraction, function splitting,
     or policy separation

The advisor should not be asked "is this sloppy?" in the abstract. It should be
asked to reason over already-typed scout findings.

Recommended advisor prompt additions:

- explain the smallest safe extraction boundary
- distinguish local duplication from framework-imposed ceremony
- call out false-positive risk when duplication is intentional for clarity

## Workflow Integration

After the scout family is stable, add a non-blocking code-review workflow step.

Target:

- extend `workflows/code-review.yaml`

Behavior:

- run the scout with `focus=slop`
- continue on error
- summarize count and top findings
- keep advisory-only in the first rollout

Do not make `slop` a hard fail until:

- fixture coverage exists
- threshold tuning has stabilized
- false positive rates are acceptable in at least Go

## Implementation Phases

## Phase 0: semantics and fixtures

Objective:

Define what counts as `slop` in repo terms before adding new rules.

Deliverables:

- small labeled fixture set:
  - positive examples
  - near misses
  - explicit non-examples
- crosswalk from existing scout rules to the new slop family
- a written suppression policy for intentional duplication

Acceptance criteria:

- at least 15-20 labeled functions across Go and TypeScript examples
- every proposed rule has one positive and one negative fixture

## Phase 1: scout `focus=slop`

Objective:

Add slop-family routing to `code/refactor_scout` without changing the output
envelope contract.

Deliverables:

- new `focus` input in scout
- slop-family filtering/boosting
- labels or metadata marking slop-related findings

Acceptance criteria:

- existing non-slop scout behavior is unchanged when `focus=all`
- `focus=slop` produces deterministic filtered results

## Phase 2: Go rule implementation

Objective:

Add the first precise slop-family rules in Go.

Primary files:

- `skills/code_refactor_scout/main.go`
- related scout tests under `skills/code_refactor_scout/`

Deliverables:

- `duplicate_recovery_block`
- `repeated_guard_ladder`
- `retry_ladder`
- `duplicated_error_remap`
- `policy_transport_mixing`

Acceptance criteria:

- golden tests for each rule
- composite hotspot synthesis works for multi-rule functions
- no keyword-triggered implementation paths exist

## Phase 3: composite hotspot and UX

Objective:

Add a coherent top-level slop experience without splitting the engine.

Deliverables:

- `slop_hotspot` composite finding
- CLI docs and examples
- optional convenience wrapper:
  - `agentctl refactor slop`

Acceptance criteria:

- one obvious command path for humans
- no duplicate engine logic outside scout

## Phase 4: advisor specialization

Objective:

Let the small model explain or rerank slop findings without replacing local
discovery.

Deliverables:

- `focus=slop` support in advisor
- slop-specific prompt tuning
- stable JSON schema for advisor rationale

Acceptance criteria:

- advisor output is useful even when the remote model is unavailable
- fallback remains the local deterministic shortlist

## Phase 5: TypeScript/JavaScript

Objective:

Bring the original motivating UI/API cleanup cases into scope with a parser-
backed implementation.

Deliverables:

- TS/JS parser-backed slop rules
- explicit confidence or evidence parity notes
- TS fixture corpus based on real API-client and retry/fallback shapes

Acceptance criteria:

- no regex-only slop detector for TS/JS
- at least one real-world duplicated recovery example is detected in fixtures

## Phase 6: review workflow and docs

Objective:

Surface the feature in normal review flows.

Deliverables:

- workflow integration in `workflows/code-review.yaml`
- docs update in `docs/general/refactor-scout.md`
- examples in `docs/README.md` and `docs/plans/README.md`

Acceptance criteria:

- review workflow shows slop findings as advisory output
- docs explain the scout/advisor split clearly

## Testing Strategy

- table-driven unit tests for each rule detector
- fixture-based golden tests for full scout outputs
- deterministic ordering tests for mixed slop and non-slop findings
- regression tests for known false positives
- advisor parser tests for slop-specific shortlist JSON

The test bar should explicitly include:

- intentional duplication that should **not** be flagged
- framework wrappers that are repetitive but acceptable
- straight-line transport helpers that are complex but not slop-heavy

## Success Criteria

- users can ask "show me slop in this repo" and get typed structural findings
- the output explains *which* slop pattern was detected
- the deterministic scout finds the candidates first
- the advisor improves prioritization, not discovery
- Go support is trustworthy before TS/JS claims parity
- review workflow can surface the findings without breaking CI stability

## Open Questions

1. Should `focus=slop` only filter results, or should it also lower thresholds
   for slop-family rules?
2. Should `slop_hotspot` suppress its constituent rules the way
   `function_hotspot` currently suppresses some constituents, or should both be
   returned together?
3. Is `agentctl refactor slop` worth adding early, or should the first pass stay
   behind `refactor scout --focus slop`?
4. For TypeScript, should the parser path live inside the scout directly or in a
   shared structural-analysis package first?

## First Slice

Start with `focus=slop` in `code/refactor_scout` plus one precise Go rule:
`duplicate_recovery_block`.

Reason:

- it lands in the existing architecture
- it is easy to explain to users
- it aligns closely with the motivating example
- it proves the evidence model before broader rule expansion
