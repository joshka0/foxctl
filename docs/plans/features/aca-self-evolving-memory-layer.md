---
vault_refs:
  - notes/repo/agentctl/self-evolving-memory-layer.md
  - notes/repo/agentctl/semantic-and-memory.md
  - notes/repo/agentctl/aca-memory-stack-comparison.md
  - notes/repo/agentctl/index.md
---
# ACA Self-Evolving Memory Layer Plan

Status: proposed active plan
Owner: solo maintainer
Last Updated: 2026-03-19

## Goal

Turn ACA into a self-evolving memory layer *(in implementation terms: a proposal-driven, eval-gated memory control loop)*.

The target system should:

1. notice memory failures and gaps
2. classify them into bounded root causes
3. draft typed corrections
4. evaluate those corrections against control suites and invariants
5. auto-apply only low-risk changes
6. keep durable knowledge under review and provenance discipline

This plan extends the current ACA retrieval-correction slice documented in
[docs/plans/features/aca-self-corrective-loop.md](aca-self-corrective-loop.md)
and the dual-plane runtime in
[docs/architecture/context-architecture.md](../../architecture/context-architecture.md).

## Problem

ACA already has:

- a workspace-local control plane
- a durable Obsidian knowledge plane
- promotion drafts
- retrieval inspection and correction for some miss classes
- bridge reconciliation between repo docs and vault notes

ACA does not yet behave like a full compounding memory system because the
system is still stronger at:

- preserving and retrieving current internal state

than at:

- importing external evidence
- converting repeated friction into proposed structural improvements
- evaluating memory changes as a first-class operational loop
- unifying multiple memory vocabularies into one operator-facing model

The main gap is not "more memory." It is a closed loop that can safely evolve
memory structure over time.

## Non-Goals

- Do not let agents rewrite canonical notes or prompt doctrine silently.
- Do not collapse the control plane and knowledge plane into one store.
- Do not make embeddings or semantic search mandatory for core ACA correctness.
- Do not auto-promote arbitrary observations into canonical knowledge.
- Do not treat companion `L0/L1/L2` conversation memory as identical to ACA
  `L0-L5` without an explicit mapping layer.

## Design Principles

### Dual-plane stays intact

- control-plane truth remains in `.agentctl/runtime/`
- durable human-readable knowledge remains in the vault

### Typed proposals, not freeform mutation

Every proposed memory change should be expressed as a bounded change family,
not as an unconstrained "rewrite memory" action.

### Eval before apply

Any change that affects retrieval, doctrine, or durable note topology should be
tested before it becomes active.

### Provenance always survives promotion

External evidence and runtime observations must retain source identity as they
move toward durable notes.

### Low-risk auto-apply only

ACA may apply safe changes automatically, but high-impact changes remain
review-gated.

## Target Architecture

```text
signal sources
  runtime friction
  retrieval misses
  contradictions
  doc/vault drift
  user corrections
  external evidence
        |
        v
proposal builder
  classify
  dedupe
  score risk
  generate typed delta
        |
        v
evaluation lane
  retrieval suites
  contradiction checks
  docs link checks
  note graph health
  control-suite comparison
        |
        +--> reject / defer / escalate
        |
        v
apply lane
  safe policy patch
  bridge metadata patch
  inbox draft
  routing-note refresh
        |
        v
promotion + indexing
  reviewed durable merge
  obsidian index rebuild
  bridge reconcile
  memory health metrics
```

## Proposal Families

ACA should standardize the following proposal kinds:

1. `retrieval_policy_patch`
2. `bridge_metadata_patch`
3. `missing_note_draft`
4. `external_evidence_import`
5. `routing_note_refresh`
6. `methodology_draft`
7. `ontology_patch`
8. `prompt_doctrine_draft`

Each proposal row should include:

- `id`
- `kind`
- `workspace`
- `source_refs`
- `confidence`
- `blast_radius`
- `dedupe_key`
- `proposed_change`
- `evaluation_status`
- `apply_status`
- `review_required`
- `created_at`
- `updated_at`

## Signal Inputs

The self-evolving loop should ingest these signal classes:

### 1. Retrieval signals

Source:

- `context retrieve-inspect`
- `context retrieve-inspect-suite`
- retrieval eval suites

Useful outputs:

- miss class
- expected note/path set
- ranking mismatch
- repeated package-note coverage gaps

### 2. Runtime friction signals

Source:

- repeated handoff content
- repeated observations and tensions
- repeated user corrections
- repeated "where is X?" or "we already decided this" interactions

Useful outputs:

- knowledge exists but is not routed well
- knowledge is missing entirely
- doctrine is ambiguous or contradictory

### 3. Drift signals

Source:

- docs bridge reconcile
- vault graph health
- unresolved links
- stale `repo_docs` and `vault_refs`

Useful outputs:

- bridge metadata patch
- note rewrite draft
- canonical map update proposal

### 4. External evidence signals

Source:

- URL
- transcript
- audio/video
- local file imports

Useful outputs:

- extracted claims
- frameworks
- examples
- actions
- provenance-bound draft notes

## Phase 1: Proposal Store and Governance

### Goal

Create a first-class ACA proposal lane that can hold suggested memory changes
before they are applied or promoted.

### Changes

- add a proposal store under the ACA control plane
- define proposal kinds and risk levels
- add CLI read surfaces for listing, inspecting, and filtering proposals
- add dedupe/merge rules so the same memory problem does not create endless
  duplicate proposals

### Likely surfaces

- `agentctl context proposals`
- `agentctl context proposal <id>`
- `agentctl context proposal apply <id>`
- `agentctl context proposal reject <id>`

### Acceptance

- repeated equivalent misses collapse into one proposal record
- proposals have deterministic kind/risk metadata
- no proposal directly mutates durable notes on creation

## Phase 2: L5 External Evidence Intake

### Goal

Make `L5 External Sources` operational instead of conceptual.

### Changes

- add an intake command family for external evidence
- normalize source metadata and provenance
- extract typed evidence from URL/transcript/audio/video/local files
- create inbox drafts that can be reviewed and promoted into canonical notes

### Likely surfaces

- `agentctl context import-evidence --url ...`
- `agentctl context import-evidence --transcript ...`
- `agentctl context import-evidence --file ...`
- `agentctl obsidian promote-evidence-draft`

### Output contract

Each ingest run should produce:

- a bounded summary
- typed extracted items
- a provenance record
- a vault draft path
- optional CAS artifact for large payloads

### Acceptance

- external evidence lands in the inbox with provenance
- evidence imports also seed typed proposals, so repeated topic imports can converge on merge-oriented or methodology-oriented review work
- evidence proposals should suggest likely canonical landing notes when the vault already contains a strong match, so review starts from a proposed merge target instead of a raw draft only
- applying an evidence proposal should prepare a reviewed-merge job for that target, not directly rewrite the canonical note
- promotion preserves source references
- vault index rebuild makes imported notes retrievable through ACA

### Local summarizer lane

A lightweight local-model summarizer is useful here, but it should not be
treated as a new durable memory tier.

The better shape is:

- `L5` remains the external evidence layer
- an optional local `L6` acts only as a synthesis lane that cheaply extracts
  summaries, claims, frameworks, and routing hints before review/promotion

That keeps the architecture honest:

- `L5` stores evidence
- `L6` processes evidence
- `L3` keeps durable promoted knowledge

## Phase 3: Session Routing Note

### Goal

Add a bounded, always-loadable routing note that turns ACA state into a
human-readable session-start memory surface.

### Why

`top_of_mind.json` is good control-plane state, but it is not yet a durable,
attention-routing note shaped for agents and humans.

### Changes

Generate a note from:

- current objective
- phase
- hard constraints
- latest handoff
- open tensions
- a small set of durable note pointers
- methodology reminders

### Constraints

- generated note is bounded
- generated note is refreshable, not append-forever
- generated note routes to deeper notes instead of duplicating them

### Acceptance

- session-start context becomes more legible without replacing the control plane
- the note is explicitly marked as a routing artifact, not canonical doctrine

## Phase 4: Self-Correction Beyond Retrieval

### Goal

Extend the current self-corrective retrieval loop into a broader memory
maintenance loop.

### New correction classes

- `instruction_gap`
- `external_evidence_gap`
- `ontology_gap`
- `routing_gap`
- `bridge_drift`

### Changes

- broaden classifier logic beyond retrieval misses
- map each class to one safe proposal family
- add escalation rules for cases that need human review

### Acceptance

- ACA can explain why the memory system failed, not just that it failed
- the proposal family is deterministic for repeated cases

## Phase 5: Eval-Gated Apply Lane

### Goal

Make memory changes measurable and reversible.

### Eval gates

- retrieval hit-rate changes
- MRR / ranking improvement on affected suites
- contradiction count deltas
- unresolved link count deltas
- docs-link health
- control-suite regression check

### Auto-apply lane

Allowed:

- retrieval policy toggles
- low-risk bridge metadata patches
- routing-note refresh
- index rebuilds

### Review-required lane

- canonical note rewrites
- methodology changes
- ontology changes
- prompt/doctrine changes
- broad graph restructuring

### Acceptance

- every applied proposal has an audit trail
- harmful changes can be reverted
- no high-risk durable change bypasses review

## Phase 6: Unified Memory Vocabulary

### Goal

Stop presenting companion memory and ACA memory as two unrelated stacks.

### Problem

Today the repo uses both:

- companion `L0/L1/L2`
- ACA `L0-L5`

These are both valid, but the operator story is fragmented.

### Changes

Define one canonical mapping:

- companion `L0` -> ACA `L0/L1` runtime continuity
- companion `L1/L2` -> ACA operational/durable support layers
- ACA `L5` -> external evidence and import path

Then document:

- what each layer is for
- what storage owns it
- what promotion path crosses between them

### Acceptance

- one architecture note can explain memory without switching vocabularies midstream
- prompt/docs/tooling use the same layer names consistently

## Data Model Sketch

New control-plane entities:

- `memory_proposals`
- `evidence_import_runs`
- `routing_note_state`
- `memory_health_snapshots`

Suggested fields:

### `memory_proposals`

- `id`
- `workspace`
- `kind`
- `classification`
- `status`
- `review_required`
- `confidence`
- `blast_radius`
- `dedupe_key`
- `summary`
- `source_refs_json`
- `proposed_change_json`
- `eval_result_json`
- `created_at`
- `updated_at`

### `evidence_import_runs`

- `id`
- `workspace`
- `source_kind`
- `source_uri`
- `artifact_digest`
- `draft_path`
- `summary`
- `status`
- `created_at`

### `memory_health_snapshots`

- `id`
- `workspace`
- `retrieval_hit_rate`
- `retrieval_mrr`
- `contradiction_count`
- `open_proposal_count`
- `stale_note_count`
- `unresolved_link_count`
- `repeated_question_rate`
- `captured_at`

## Rollout Order

1. proposal store
2. external evidence ingest
3. routing note
4. broader classifier families
5. eval-gated apply lane
6. vocabulary unification

This order matters because:

- proposal storage is the control point for everything else
- `L5` ingest is the highest-value missing capability
- routing improves session usability quickly
- wider self-correction should not ship before proposals and eval gates exist

## Verification

### Repo checks

- `make check-doc-links`

### ACA retrieval checks

- `agentctl obsidian index build --vault-path <vault>`
- targeted `agentctl obsidian index search` queries for the new note set

### Evaluation checks

- retrieval control suite before/after proposal apply
- contradiction counts before/after note or bridge changes
- no regression on unrelated control suites

## Open Questions

1. Should `L5` ingest live under `context` or `obsidian` commands, or both?
2. Should proposal evaluation run inline, in daemon maintenance, or both?
3. What is the minimal safe auto-apply set for bridge metadata patches?
4. How much of routing-note refresh should be generated versus curated?
5. Should methodology and prompt doctrine proposals share one review queue or
   stay separate?

## Initial Success Criteria

- ACA can ingest outside evidence into reviewed draft memory
- ACA can produce typed proposals from repeated friction, not just retrieval
  misses
- ACA can evaluate proposed memory changes before apply
- ACA can auto-apply only low-risk changes with an audit trail
- ACA exposes one coherent explanation of its memory layers to operators and
  agents
