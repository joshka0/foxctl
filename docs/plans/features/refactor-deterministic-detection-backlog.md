# Refactor Deterministic Detection Backlog

Status: active backlog

Owner: foxctl

Last updated: 2026-04-02

Related plans:

- [Refactor Intelligence Substrate Plan](./refactor-intelligence-substrate.md)
- [Refactor Phase 1 Spec: Status and Snapshot](./refactor-phase1-status-and-snapshot.md)
- [Slop Function Detection Plan](./slop-function-detection.md)

## Goal

Improve `foxctl refactor scout` primarily through deterministic detection and
scoring rather than by expanding the LLM layer.

This backlog is intentionally scoped to:

- stronger primary detection
- better deterministic prioritization
- cleaner evidence synthesis
- lower false-positive / false-negative rates

This backlog is intentionally **not** about making the advisor smarter first.
The advisor can remain a second-stage explainer and sequencer. Discovery should
keep getting better from typed signals, repoindex, snapshots, churn, and
reachability.

## Current State

The current refactor lane already has:

- `refactor status`
- `refactor snapshot`
- `refactor deps`
- `refactor changes`
- `refactor hot`
- `refactor evidence`
- `refactor scout`
- `refactor advisor`

The scout already supports:

- parser-only and index-backed modes
- slop detection
- conservative dead-code detection
- evidence artifacts
- repo-graph reranking for index-backed hotspots
- symbol-level hotness and file-level hotness
- opportunity scoring with visible score factors
- file-level co-change evidence
- extraction boundary classification
- file/package dead-code candidates
- explicit confidence scoring

The remaining work is now narrower than the original backlog:

- finish dead-code root coverage for more indirect wiring families
- generalize finding-family merge / suppression beyond the current hotspot and
  cluster-specific logic
- optionally deepen symbol-level co-change and boundary taxonomy after the
  suppression layer is in place
- validate that TypeScript and Elixir family-level findings reach the same
  quality bar as Go for the supported rule families, and explicitly track any
  gaps where TS/Elixir still lag Go

The next gains are no longer about "add one more smell rule." They are mostly
about turning existing primitives into better deterministic decisions.

## Design Rules

1. Keep `refactor scout` as the deterministic source of discovery truth.
2. Prefer typed signals, scored features, and graph/snapshot evidence over
   keyword heuristics.
3. Add model help only after deterministic ranking is strong enough that the
   model is mostly explaining and sequencing.
4. Every backlog item should improve one of:
   - precision
   - recall
   - prioritization
   - actionability
5. New findings should degrade gracefully when the scope is `parser_only`.

## Prioritized Backlog

### 1. Symbol-Level Hotness

Status: done

Problem:

`refactor hot` is file-level today. That is useful, but reranking still treats
every symbol in a hot file as equally active.

Opportunity:

Add symbol-level hotness using:

- current snapshot symbol hashes
- file-level git churn
- symbol spans / body hashes from snapshots
- snapshot-to-snapshot symbol diffs when available

Expected value:

- better hotspot reranking
- better tie-breaking inside large files
- stronger "touch this function now" signal

Suggested output additions:

- `symbol_hot_score`
- `symbol_recent_change_count`
- `symbol_last_touched_at`

Acceptance:

- `refactor hot` can optionally emit symbol rows
- scout hotspots can attach symbol-level hot evidence when available
- reranking uses symbol hotness before file-level hotness when both exist

Current implementation:

- scout evidence attaches `symbol_hot_score`, `symbol_recent_change_count`, and
  changed-line evidence
- hotspot reranking prefers symbol hotness over file-only hotness when both are
  available
- symbol-level output is currently evidence-backed inside scout rather than
  exposed as a separate primary `refactor hot --symbols` surface

### 2. Opportunity Score

Status: done

Problem:

Current hotspot ranking is still partly additive but not explicitly framed as
"best action opportunity."

Opportunity:

Add a bounded composite score that combines:

- structural score
- reverse dependency pressure
- forward dependency pressure
- symbol/file hotness
- recent change count
- finding confidence

Suggested formula family:

```text
opportunity_score =
  structural_score * 0.45 +
  dependency_pressure * 0.20 +
  change_pressure * 0.20 +
  extraction_strength * 0.10 +
  confidence_bonus * 0.05
```

This should remain deterministic and testable. The exact weights can be tuned
with golden fixtures rather than guessed ad hoc.

Expected value:

- better "what should I do first" ordering
- less over-prioritization of noisy but isolated slop
- clearer distinction between cleanup seams and maintenance-critical seams

Acceptance:

- `function_hotspot` emits both `base_score` and `opportunity_score`
- ordering is stable and test-backed
- score contributions are visible in finding evidence

Current implementation:

- `function_hotspot` evidence includes `base_score`, `opportunity_score`,
  `opportunity_bonus`, and `opportunity_factors`
- scout reranking uses dependency, hotness, co-change, and confidence signals
  deterministically

### 3. Co-Change Clusters

Status: partial

Problem:

The scout can see structural similarity and file churn, but it cannot yet say
"these symbols/files repeatedly move together."

Opportunity:

Add deterministic co-change clustering from git history:

- file-level co-change matrix first
- symbol-level co-change second
- bounded commit window and minimum support threshold

New signals:

- `cochange_cluster`
- `shared_refactor_boundary`
- `cochange_strength`

Expected value:

- better extraction-boundary suggestions
- better prioritization of seams that already evolve together
- fewer false "split these apart" suggestions for code that is structurally
  similar but operationally independent

Acceptance:

- `refactor hot` or a sibling primitive can emit co-change groups
- scout can attach `cochange_cluster_id` and `cochange_strength`
- evidence artifacts include related paths/symbols from the same cluster

Current implementation:

- scout evidence attaches file-level `cochange_strength`, `cochange_count`, and
  related-path evidence
- co-change currently feeds `opportunity_score` and boundary classification
- missing pieces are symbol-level co-change and a first-class co-change command
  surface

### 4. Extraction Boundary Scoring

Status: partial

Problem:

The scout can say "same-file extraction candidate," but it is still weak at
naming the actual seam type.

Opportunity:

Turn extraction-style findings into more explicit boundaries:

- `extract_workflow_step`
- `extract_policy_layer`
- `extract_transport_wrapper`
- `extract_handler_core`
- `extract_error_normalizer`

This should be driven by structural cues such as:

- repeated guarded blocks
- repeated remap clauses
- repeated orchestration skeletons
- same-input/same-output sibling overlaps
- dep/call asymmetry across sub-blocks

Expected value:

- more actionable scout output
- better handoff to refactor advisor
- easier human reading of hotspot families

Acceptance:

- hotspots can include `suggested_boundary_kind`
- same-file extraction candidates include a seam classification
- tests cover at least one example per seam kind

Current implementation:

- hotspots already include `suggested_boundary_kind`
- the current seam taxonomy covers the strongest repeated recovery /
  orchestration shapes
- missing work is broader seam coverage for policy, transport, and handler-core
  families

### 5. Dead-Code Root Modeling

Status: partial

Problem:

Dead-code detection is conservative now, which is correct, but it still uses a
limited root model.

Opportunity:

Expand reachability roots deterministically for:

- interface implementation dispatch
- Cobra command registration
- HTTP/router registration
- actor registration and factory wiring
- skill/plugin entrypoints
- known framework lifecycle hooks

Expected value:

- fewer stale-export false positives
- better support for "unused but wired indirectly" code
- safer `focus=dead` output

Acceptance:

- dead-code fixtures cover at least one indirect-dispatch case per root family
- `focus=dead` false positives decrease in known wired subsystems
- finding evidence can state which root family kept a symbol live

Current implementation:

- dead-code now handles interface / embedding-backed methods
- Go package-scope registration roots now flow through file-level graph edges,
  which covers Cobra-style `RunE: runFoo` and similar top-level registration
  tables
- missing families include richer router/HTTP registration, actor/factory
  wiring summaries, skill/plugin entrypoints, and framework lifecycle hooks

### 6. Orphan File and Package Candidates

Status: done

Problem:

Dead-code detection is symbol-first only. Whole-file and package-level stale
surfaces are still mostly invisible.

Opportunity:

Add:

- `orphan_file`
- `test_only_file`
- `stale_package_candidate`

Using:

- repoindex file/package nodes
- inbound `IMPORTS`, `TESTS`, and symbol references
- snapshot file metadata

Expected value:

- better cleanup detection for old subsystems
- easier repo hygiene passes
- better package-level refactor opportunities

Acceptance:

- new rules are only emitted on index-backed scopes
- file/package findings include inbound edge evidence
- platform/generated/test exemptions are explicit and tested

### 7. Confidence Scoring

Status: done

Problem:

Not all findings should be trusted equally, but confidence is still mostly
implicit.

Opportunity:

Emit an explicit confidence score or confidence factors from:

- index coverage
- parser support quality for the language
- rule multiplicity
- graph support availability
- snapshot/evidence completeness
- dead-code root coverage

Expected value:

- better user trust
- easier filtering in downstream review flows
- cleaner distinction between "strong candidate" and "useful hint"

Acceptance:

- findings expose `confidence_score`
- evidence lists the contributing confidence factors
- UI/CLI consumers can sort or filter by confidence without recomputing it

### 8. Finding Family Merge / Suppression

Status: next

Problem:

Some outputs still feel repetitive because multiple deterministic rules are
reporting one underlying seam.

Opportunity:

Add a deterministic family merge layer that can:

- keep constituent findings for `focus=slop`
- suppress redundant children in broad runs
- promote one parent finding with a stable explanation bundle

Expected value:

- cleaner top-N results
- less output noise
- better hotspot summaries

Acceptance:

- merged hotspots retain constituent evidence
- broad runs are less repetitive without losing detail in focused modes

Current implementation:

- broad runs already suppress hotspot constituents and redundant cluster rows
- focused slop runs keep constituent findings visible
- the remaining gap is broad suppression for dead/file/package families and a
  more explicit family-merge contract in evidence

## Implementation Order

Recommended order:

1. finding family merge / suppression
2. finish dead-code root modeling
3. broaden extraction boundary taxonomy
4. deepen co-change from file-level to symbol-level

Reasoning:

- the score/evidence layers are already strong enough that output cleanup now
  matters more than adding another independent score
- dead-code is now accurate enough that the remaining gains are mainly in
  broader indirect-root coverage
- deeper co-change and seam taxonomy are refinement work after suppression is
  in place

## Why Not Lead With a Small Model

A small model can still help later, but it should not be the next primary
detection layer.

Why:

- AST/graph/churn/snapshot evidence is already rich enough to improve ranking
  deterministically
- model-first primary detection would be harder to test and less stable in CI
- dead-code classification especially benefits more from better root modeling
  than from model judgment

Recommended future model role:

- rerank top deterministic candidates
- explain why a seam matters
- propose extraction names and safe sequencing

Recommended non-role:

- scanning every function as the first detector

## Suggested Acceptance Strategy

For each backlog item:

1. add at least one fixture in `skills/code_refactor_scout/*_test.go`
2. add one live smoke example from `foxctl` or `praze`
3. keep all new scoring factors visible in finding evidence
4. ensure parser-only fallback remains valid
5. avoid keyword-trigger routing or suppression
6. when the rule family is intended to be cross-language, verify Go,
   TypeScript, and Elixir support explicitly and record whether TS/Elixir are at
   parity with Go or still behind it

## Immediate Next Slice

If work starts now, the best next deterministic slice is:

1. extend family merge / suppression beyond hotspot-only broad-run behavior
2. finish the remaining dead-code root families
3. then broaden seam taxonomy and symbol-level co-change

That is now a better next slice than adding another standalone smell rule.
