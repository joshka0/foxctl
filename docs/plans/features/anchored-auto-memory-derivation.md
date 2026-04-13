---
vault_refs:
  - notes/repo/agentctl/semantic-and-memory.md
  - notes/repo/agentctl/index.md
---
# Anchored Auto-Memory Derivation

Status: proposed active plan
Owner: companion / memory / continuity
Last Updated: 2026-03-25

## Goal

Add a first-class derivation layer for conversation memory that is:

1. anchored to prior state instead of evaluating turns in isolation
2. centered on interaction outcomes rather than raw message text
3. able to distinguish durable facts from provisional unresolved state
4. ready for local LMStudio inspection and later dream-style consolidation

The intended outcome is:

- better candidate memory quality than the current per-event heuristic pass
- explicit handling for corrections, unresolved issues, and user reactions
- a safer bridge from conversation memory into named memory and ACA

## Core Decision

### Evaluate anchored interaction frames, not isolated turns

The primary evaluation unit should be:

```text
anchor_state_t + user_t -> assistant_t -> user_t+1
```

Where:

- `anchor_state_t` is the state known before `user_t`
- `user_t -> assistant_t -> user_t+1` is the interaction triple
- `user_t+1` is optional; if absent, the interaction remains unresolved

This is better than evaluating `user_t -> assistant_t` alone because the next
user turn is where acceptance, correction, frustration, confusion, or relief
becomes visible.

### Unresolved facts should become provisional memory, not durable fact

If the interaction does not produce a clearly grounded durable update, emit one
of:

- `open_question`
- `active_assumption`
- `blocked_issue`
- `follow_up_needed`
- `user_pain_point`

Do **not** write an unresolved interaction directly as durable hard state.

## Why this is needed now

The current hybrid runtime already has strong primitives:

- ordered event storage in `companion_events`
- durable append-only hard state
- assumptions, evidence, and episodic summaries
- periodic maintenance through `autoCompress` and the daemon

What it does not yet have is a first-class:

```text
ordered interaction -> candidate memories -> consolidation
```

pipeline. Today the live path is mostly:

```text
single event -> heuristic extraction
sealed span -> LLM summary
```

That is not enough for robust auto-memory quality.

## Anchored Interaction Frame

### Shape

Each frame should be a typed record built from ordered conversation events:

```json
{
  "conversation_id": "conv-123",
  "anchor_state": {
    "before_event_id": 41,
    "hard_state": [],
    "active_assumptions": [],
    "open_questions": [],
    "goals": [],
    "recent_tool_receipts": []
  },
  "user_event": {
    "id": 41,
    "text": "..."
  },
  "assistant_event": {
    "id": 42,
    "text": "..."
  },
  "followup_user_event": {
    "id": 43,
    "text": "..."
  },
  "resolution": "resolved|corrected|unresolved|continues",
  "reaction": {
    "outcome": "accepted|corrected|frustrated|confused|neutral|unresolved",
    "observed_cues": ["..."],
    "inferred_affect": "frustration",
    "confidence": 0.78
  }
}
```

### `anchor_state_t`

`anchor_state_t` should represent what the system knew before the user spoke:

- active hard state as of `user_t`
- active assumptions as of `user_t`
- open questions as of `user_t`
- active goals as of `user_t`
- recent tool receipts before `user_t`

This avoids the main quality failure where later facts leak backward into the
evaluation of earlier turns.

## Evaluation rubric

Two distinct questions must be answered for every frame.

### 1. Was the derivation faithful?

The derivation must:

- be grounded in the actual interaction frame
- separate observation from inference
- respect the anchor state available at that time
- correctly classify the state delta as:
  - `new`
  - `update`
  - `contradiction`
  - `reinforcement`
  - `none`

### 2. Is it worth remembering?

The derivation should be scored on:

- `future_utility`
- `persistence`
- `impact`
- `recurrence`
- `novelty`
- `emotional_salience`

Important rule:

- remember the cause of the emotion, not the emotion alone

Good:

- user showed frustration because repo context was repeatedly lost

Bad:

- user is irritable

## Reaction and salience

### Observable vs inferred

Every reaction record should keep these separate:

- observable cue
- inferred reaction
- inferred cause

Example:

```json
{
  "observed_cues": [
    "still broken",
    "I already told you this"
  ],
  "inferred_affect": "frustration",
  "confidence": 0.82,
  "cause": "repeated context loss during setup"
}
```

### Surprise metric

The desired “surprise” score is better modeled as:

- novelty relative to prior state
- prediction error relative to prior assumptions

This is useful when:

- a previous assumption is disproven
- the user sharply corrects direction
- a repeated pain point keeps recurring
- a tool result invalidates the current plan

## Memory outputs

Frames should classify outputs into one of four buckets.

### 1. Drop

Not worth retaining beyond recent turns.

### 2. Session-only memory

Useful for current continuity but not durable enough for named memory or ACA.

Examples:

- temporary blockers
- local unresolved questions
- one-off tactical reactions

### 3. Durable conversation memory

Write into hybrid hard state and/or named memory when the interaction yields a
stable durable learning.

Examples:

- user preference
- stable technical constraint
- corrected workflow rule
- durable gotcha
- accepted decision

### 4. Promotion candidate

Escalate only higher-confidence workspace-relevant outcomes into ACA
observations, tensions, or handoffs.

ACA remains a second-stage promotion boundary, not the first sink.

## Consolidation lane

The derivation pass is only the first half.

The second half is a dream-style consolidation pass that:

- merges duplicate candidates
- converts relative time to absolute time
- resolves contradictions against current state
- updates the current best view
- preserves append-only/tombstone semantics

This should be proposal-driven:

```text
frame derivation
  -> candidate memory updates
  -> evaluation
  -> safe apply
  -> later consolidation
```

Not:

```text
rewrite memory in place
```

## First implementation slice

The first slice should be deliberately narrow.

### Deliverables

1. typed anchored interaction frame structs in companion memory
2. a builder that compiles ordered `user -> assistant -> optional next user`
   frames from `companion_events`
3. historical anchor-state snapshots keyed to `before_event_id`
4. simple deterministic follow-up reaction labeling:
   - `accepted`
   - `corrected`
   - `frustrated`
   - `confused`
   - `neutral`
   - `unresolved`
5. tests proving:
   - frames are built in order
   - future hard-state does not leak backward into prior anchor snapshots
   - follow-up correction and acceptance are recognized

### Explicit non-goals for slice 1

- no LMStudio derivation prompt yet
- no automatic writes from frames into named memory
- no ACA promotion from frames
- no dream consolidation worker yet

Those follow after the substrate exists.

## Proposed rollout

### Phase 1

Add anchored frame compilation as a read-only substrate in `internal/context/companion`.

### Phase 2

Add LMStudio-backed frame derivation:

- input: anchored interaction frame
- output: typed candidate memories with confidence and action

### Phase 3

Add a proposal/apply path for:

- hybrid hard-state updates
- session-only provisional state
- named memory writes

### Phase 4

Add a dream-style consolidation worker over candidates and existing state.

## File map

### New first-slice files

- `internal/context/companion/anchored_derivation.go`
- `internal/context/companion/anchored_derivation_test.go`

### Likely later files

- `internal/context/companion/anchored_derivation_llm.go`
- `internal/context/companion/anchored_consolidation.go`
- `internal/context/companion/anchored_consolidation_test.go`

## Related

- [docs/general/companion-memory.md](../../general/companion-memory.md)
- [docs/plans/companion-memory-salience-policy.md](../companion-memory-salience-policy.md)
- [docs/plans/features/memory-ensemble-retrieve.md](memory-ensemble-retrieve.md)
- [docs/plans/features/aca-self-evolving-memory-layer.md](aca-self-evolving-memory-layer.md)
