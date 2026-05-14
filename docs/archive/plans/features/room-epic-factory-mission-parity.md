# Room Epic vs Factory Mission Parity

| Field | Value |
|-------|--------|
| Status | Updated |
| Scope | `foxctl room` epic/milestone/story backend, work-pack model, and mission-style coordinator surfaces |
| Related | [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-epic-resume-and-next.md](./room-epic-resume-and-next.md), [room-epic-health-pulse.md](./room-epic-health-pulse.md), [room-milestone-contract.md](./room-milestone-contract.md), [room-story-lifecycle.md](./room-story-lifecycle.md), [room-milestone-synthesis.md](./room-milestone-synthesis.md), [room-retro-guidance.md](./room-retro-guidance.md), [room-evidence-lanes.md](./room-evidence-lanes.md), [room-workpack-templates.md](./room-workpack-templates.md), [room-agile-provenance-backlinks.md](./room-agile-provenance-backlinks.md), [room-agile-aca-promotion.md](./room-agile-aca-promotion.md), [room-epic-checkpoint.md](./room-epic-checkpoint.md), [go-native-runtime-and-optional-jido.md](../../../architecture/go-native-runtime-and-optional-jido.md) |

## Why this note exists

The room agile backend is no longer only a basic ledger. It now has most of the
core long-running mission primitives that Factory-style work benefited from.

This note exists to keep planning honest:

- call out what is already shipped
- identify what is still missing for stronger Factory-style mission parity
- prevent future planning from re-scoping already-completed slices

## Shipped baseline

The room agile backend already has these mission-quality primitives in tree:

1. epic intake and clarification
   - `epic start`
   - `epic ask`
   - `epic answer`
   - `epic finalize`
   - `epic shape`
2. epic continuity and health
   - `epic resume`
   - `epic next`
   - `epic health`
   - `epic checkpoint`
3. richer milestone contracts
   - objective
   - risks
   - exclusions
   - dependencies
   - validators expected
   - required and optional evidence lanes
   - exit criteria
   - `enforce_exit_policy`
   - reversible enforcement toggle
4. explicit story lifecycle state
   - `proposed`
   - `accepted`
   - `in_progress`
   - `in_review`
   - `blocked`
   - `validated`
   - `waived`
   - `done`
   - `deferred`
5. story-owned validation evidence
6. milestone review plus structured milestone synthesis
7. retro / guidance feedback loop
8. typed evidence lanes in read models and work-pack rendering
9. explicit work-pack templates and stable artifact structure
10. provenance and backlinks between room state, work-pack artifacts, and ACA drafts
11. ACA draft promotion for high-signal agile artifacts
12. deterministic delivery improvements for mux send confirmation
13. room-wide coordinator pulse (`room pulse`) with epic health aggregation, stale detection, interview nudge, and task heartbeat monitoring
14. Go-native runtime parity for CLI runtime tree reads (`coreworker.StateReader` seam, goruntime/libsql backend)

So the parity question is no longer “how do we get resume, contracts, synthesis,
work-packs, or mission pulse?” Those are all implemented.

## What Factory missions still do better

The remaining gap is less about more nouns and more about stronger mission-wide
operator surfaces.

The best remaining Factory-style ideas are:

1. one coordinator view across all active missions
2. cleaner mission scoping when multiple objectives share a room
3. richer evidence semantics than a single validation stream, even with lanes
4. deeper continuity into the semantic memory layer after work is done

## Remaining gaps

### 1. Room-wide coordinator pulse (SHIPPED)

Implemented as `room pulse` with `detectRoomCoordinatorPulseMessages()`.

Surfaces: epics in intake, blocked milestones, missing reviews/summaries, stale checkpoints/logs, safe-to-handoff signals, interview nudge, task heartbeat monitoring.

This gap is now closed.

### 2. Multi-epic scoping cleanup

The current system behaves well for the active single-epic-per-room workflow,
but a few derivations still assume the room is effectively centered on one epic.

Examples:

- unresolved interview items are still counted room-wide in some epic surfaces
- future coordinator pulse logic will need stronger epic scoping
- more room-level summaries will need to avoid cross-epic leakage

This is a correctness cleanup rather than a new capability, but it matters once
rooms host multiple real epics.

### 3. Richer evidence semantics

Evidence lanes are already shipped, but Factory-style mission parity may still
want stronger semantic distinction between:

- scrutiny/code review
- automated test
- integration validation
- user testing
- manual audit
- synthesis-level evidence

The current lane model is useful, but it is still derived from
`story_validation`. If future retrieval, synthesis, or exit policy needs more
than lane typing, this is the next layer.

### 4. Deeper ACA / Obsidian follow-through

ACA draft promotion is already shipped, but the semantic-memory side is still a
drafting layer rather than a fuller reviewed knowledge loop.

Possible future depth:

- richer note classes beyond the current draft path
- clearer reviewed/accepted promotion flow
- stronger wiki-link conventions across room artifacts
- better memory retrieval from completed mission slices

This is valuable, but it is less urgent than the room-wide coordinator pulse.

## Recommended next backend order

From the current shipped baseline, the highest-leverage next slices are:

1. multi-epic scoping cleanup
2. richer evidence semantics
3. deeper ACA / Obsidian follow-through

## Proposed immediate next slice

The next backend slice should be:

1. multi-epic scoping cleanup (correctness)
2. then richer evidence semantics if retrieval needs it

Why this next:

- coordinator pulse is already shipped
- single-epic-per-room workflow is the current default
- multi-epic correctness matters before adding more semantic depth

## Non-goals

This parity effort should not:

1. move canonical state out of `room`
2. reintroduce tmux as the semantic home
3. duplicate already-shipped slices just because older docs still list them
4. force every artifact into ACA automatically
5. turn checkpoints or summaries into a second source of truth

## Working principle

The target remains:

- Factory discipline
- room-native canonical state
- transport-independent execution
- semantic continuity as a projection, not the ledger
