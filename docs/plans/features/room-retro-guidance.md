# Room Retro Guidance Loop

| Field | Value |
|-------|--------|
| Status | Reviewed, implementing |
| Scope | durable retrospective and guidance updates for `agentctl room` agile workflow |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

The current room agile layer now covers:

- epic intake and resumability
- milestone contracts
- structured milestone synthesis
- story lifecycle
- story validation
- delivery log

What is still missing from the stronger Factory-style mission model is the
feedback loop that turns lessons from one tranche into durable guidance for the
next one.

Today those lessons mostly live in:

- room chat
- review replies
- local memory of the coordinator

That is not durable enough.

## Goals

1. Capture process and delivery lessons as first-class room records.
2. Keep retrospective guidance separate from milestone synthesis.
3. Make guidance updates easy to surface in epic resume / next work later.
4. Keep the slice narrow and append-only.
5. Keep room state canonical; work-pack mirror stays derived.

## Non-goals

1. Replacing docs/general/gotchas.md in the same slice.
2. Automatically rewriting skills from retro notes.
3. Adding scoring, voting, or ceremony-heavy retrospective structure.
4. Building a full "decision memory" system in this slice.
5. Reworking delivery-log semantics.

## Proposed model

Add a new append-only room protocol kind:

- `guidance_update`

It should attach to either:

- an epic
- a milestone summary

Recommended fields:

- `kind`
  - `process`
  - `tooling`
  - `coordination`
  - `quality`
  - `delivery`
- `summary`
- `impact`
- `recommended_change`
  - CLI flag remains `--change`
- `scope`
  - optional repeated strings
- `follow_up`
  - optional repeated strings

This is intentionally lighter than milestone summary. The summary explains what
happened in the work; guidance update explains what should change next time.

## Command surface

Add:

```bash
agentctl room retro add <room-id> <epic-id> \
  --kind coordination \
  --summary "Follow-up review threads should be acked when no reply is needed." \
  --impact "Prevents reminder churn and stale reply-expected inbox items." \
  --change "Ack no-blocker follow-ups instead of waiting for an explicit reply." \
  --scope room \
  --scope review-loop \
  --follow-up "Document this in the room-agile skill"
```

Optional milestone-attached variant:

```bash
agentctl room retro add <room-id> <epic-id> \
  --milestone <milestone-id> \
  --kind tooling \
  --summary "Node-pane submit behavior needs deterministic room-state confirmation." \
  --impact "Transport success is not enough for long-running review loops." \
  --change "Use room-backed send confirmation rather than assuming Enter delivered the message."
```

Read surface:

```bash
agentctl room retro show <room-id> <epic-id>
```

## Read-model shape

`epic show` and later `epic resume` should expose:

- `guidance_updates`
- `guidance_update_count`
- latest few updates inline
  - newest first
  - explicitly bounded so epic read models stay compact as guidance grows

`retro show` should expose:

- all updates for the epic
- optional filtering by milestone
- grouping by `kind`

## Invariants

1. Guidance updates are append-only.
2. They are coordinator-owned in the first slice.
3. `kind` is normalized to a fixed enum:
   - `process`
   - `tooling`
   - `coordination`
   - `quality`
   - `delivery`
4. `summary`, `impact`, and `recommended_change` are required.
5. `milestone_id`, when provided, must belong to the same epic.
6. Guidance updates are advisory artifacts, not direct mutations of skills or docs.

## Work-pack integration

Mirror under the epic work-pack:

- `retro.md`
- optional inclusion in `meta.json`

Recommended markdown sections:

- Summary
- Impact
- Recommended Change
- Scope
- Follow-up

Ordering:

- newest guidance updates first
- within one update, keep the section order exactly as listed above

## MCP exposure

Extend `room_agile` with:

- `retro_add`
- `retro_show`

Arguments for `retro_add`:

- `room_id`
- `epic_id`
- `workspace`
- `sender`
- optional `milestone_id`
- `kind`
- `summary`
- `impact`
- `change`
  - maps to `recommended_change` in the stored body/read model
- repeatable `scope`
- repeatable `follow_up`

## Skill implications

Update `agentctl-room-agile` to teach:

- use `milestone summary` for what happened
- use `retro add` for what should change next time
- do not bury process lessons only in chat or one-off review replies

## Definition of done

This slice is done when:

1. `retro add` records durable guidance updates
2. `retro show` lists them by epic and milestone
3. `epic show` surfaces guidance update counts
4. work-pack mirror includes `retro.md`
5. MCP exposes `retro_add` and `retro_show`
6. focused tests cover:
   - enum normalization
   - coordinator-only mutation
   - milestone-to-epic consistency

## Open questions

1. Should retro updates later feed directly into `epic next` or `epic resume` as top-level guidance hints?
2. Should `guidance_update` later support promotion into repo docs or skills, or remain an in-room advisory artifact?
3. Should participant-authored retro notes be allowed later with coordinator review, or stay coordinator-only?
4. Do we eventually add an optional provenance pointer to related review/summary/task ids, or keep the v1 artifact intentionally minimal?
