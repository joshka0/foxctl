# Room Agile ACA Promotion Layer

| Field | Value |
|-------|--------|
| Status | Reviewed, refining for implementation |
| Scope | Promotion of completed room agile work into ACA and Obsidian-backed semantic memory |
| Related | [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md), [../architecture/context-architecture.md](../../architecture/context-architecture.md) |

## Why this slice

The room agile backend now gives us a durable execution ledger:

- epics
- milestones
- stories
- story validation
- milestone synthesis
- retro guidance
- epic health
- work-pack mirrors under `~/.foxctl/epics`

What it still does not give us is a strong semantic memory layer over completed
work.

Without that, the system can coordinate current work well, but it still has to
re-discover too much about:

- similar past epics
- recurring milestone failure patterns
- validation lanes that usually block a story
- process lessons from retro guidance
- previously completed work in the same subsystem

That is exactly the gap ACA and the Obsidian-backed knowledge plane should fill.

## Core layering

This slice should keep the layers explicit:

1. `room`
   - canonical operational ledger
   - source of truth for workflow state
2. `~/.foxctl/epics/...`
   - rich work-pack mirror
   - human/agent-editable artifacts
3. ACA / Obsidian
   - semantic memory and durable learned knowledge
   - not the live execution source of truth

This is a projection model, not a replacement model.

## Goals

1. Make completed room-agile work promotable into ACA.
2. Make promoted artifacts semantically searchable and linkable in Obsidian.
3. Keep stable ids and provenance so promoted notes can be traced back to room/work-pack state.
4. Promote only high-signal artifacts by default, not every raw task or chat message.
5. Make room/ACA integration explicit enough that future retrieval can answer “have we done this before?”

## Non-goals

1. Replacing room state with notes.
2. Promoting every room task automatically.
3. Turning every chat/reply into an Obsidian note.
4. Building the full ACA retrieval or reranking logic in this slice.
5. Auto-merging promoted notes into canonical evergreen notes without review.

## Promotion candidates

Default promotion candidates:

- completed epics
- completed milestones
- milestone synthesis summaries
- retro/guidance updates
- high-signal story validations
  - especially blocked, failed, waived, or otherwise instructive validations

Not promoted by default:

- ordinary in-progress task churn
- raw room chat
- low-signal task updates
- every accepted story
- every successful trivial validation

This keeps the ACA lane useful instead of noisy.

## Proposed note classes

Suggested Obsidian note classes:

- `room_epic`
- `room_milestone`
- `room_summary`
- `room_retro`
- `room_validation`

These are note classes, not necessarily separate command nouns.

## Note identity and naming

Do not make human-readable filenames the canonical identity.

Canonical ids should remain:

- `room_id`
- `epic_id`
- `milestone_id`
- `story_id`
- `validation_id`
- `guidance_update_id`
- `room_message_id`

Human-readable note paths are still fine, for example:

- `projects/foxctl/room-epics/<epic-id>-room-agile.md`
- `projects/foxctl/room-milestones/<milestone-id>-foundation.md`
- `projects/foxctl/room-retro/<guidance-update-id>-ack-no-blocker-followups.md`

But the frontmatter must carry the stable ids.

Workspace normalization:

- `workspace` should be the absolute canonical workspace path used by room/work-pack state
- `workspace_id` should mirror the canonical room workspace id when the system uses that same absolute workspace identity
- do not invent a second workspace slug in the first slice

## Proposed frontmatter

All promoted notes should include bounded provenance-oriented frontmatter.

Example:

```yaml
---
note_type: room_epic
schema_version: 1
generated_at: 2026-04-07T18:00:00Z
workspace: /Users/joshka/repos/personal/foxctl
workspace_id: /Users/joshka/repos/personal/foxctl
project: foxctl
room_id: triad-20260404-202656
epic_id: 01ABC...
milestone_id:
story_id:
validation_id:
guidance_update_id:
room_message_ids:
  - 01XYZ...
workpack_root: ~/.foxctl/epics/01ABC...
workpack_path: ~/.foxctl/epics/01ABC.../epic.md
status: completed
promotion_source: room_agile
promotion_review_state: drafted
tags:
  - room/agile
  - epic
---
```

Required fields should vary by note type, but the provenance shape should stay
stable.

### Required fields by note type

`room_epic`

- `schema_version`
- `generated_at`
- `workspace`
- `workspace_id`
- `project`
- `room_id`
- `epic_id`
- `room_message_ids`
- `promotion_source`
- `promotion_review_state`

`room_milestone`

- all epic-shared provenance fields above
- `milestone_id`

`room_summary`

- all milestone provenance fields above
- at least one summary-producing `room_message_id`

`room_retro`

- all epic-shared provenance fields above
- `guidance_update_id`
- if milestone-scoped, `milestone_id`

`room_validation`

- epic/milestone/story provenance fields as applicable
- `validation_id`
- if promoted from one story, `story_id`

The first slice should fail validation if a note class is missing its required
identity fields.

## Wikilink conventions

The Obsidian layer should prefer explicit wiki-link conventions so promoted
artifacts connect into a graph.

Examples:

- `[[room-epics/<epic-id>-room-agile]]`
- `[[room-milestones/<milestone-id>-foundation]]`
- `[[room-retro/<guidance-id>-ack-no-blocker-followups]]`
- `[[room-validations/<validation-id>-story-review]]`

Within note bodies, prefer links to:

- parent epic
- current or parent milestone
- related retro notes
- related validation notes
- canonical repo notes when relevant

The title can be readable; the frontmatter remains the stable contract.

## Promotion model

Recommended promotion path:

1. room/work-pack artifact reaches a promotion-worthy terminal state
2. promotion payload is prepared with:
   - stable ids
   - work-pack paths
   - summary content
   - bounded evidence/provenance refs
3. ACA proposal or inbox draft note is created
4. review merges it into the vault through the existing ACA promotion path

So the first slice should be:

- room/work-pack -> ACA proposal/draft

Not:

- room/work-pack -> direct canonical note mutation

Thin first-slice implementation target:

- prepare a promotion-ready draft payload
- write or queue it through the existing ACA proposal/draft lane
- stop there

This slice should not auto-merge reviewed notes into canonical evergreen notes.

## What should be promoted from each artifact

### Epic

Promote:

- title
- goal
- final brief
- current outcome if completed
- key milestones
- latest delivery-log checkpoints
- key retro guidance

Do not promote:

- every intake question/answer in raw form

### Milestone

Promote:

- contract summary
- acceptance criteria summary
- final review verdict
- structured milestone summary
- notable decisions
- recommended next

### Retro guidance

Promote:

- summary
- impact
- recommended change
- scope
- follow-up

This is especially valuable for future retrieval.

### Validation

Promote selectively.

Promote when:

- validation failed
- validation blocked
- validation was waived
- validation exposed a durable gotcha
- validation meaningfully changed future guidance

Usually do not promote routine green validations.

Implementation note:

- “high-signal validation” must be decided from typed validation state and metadata, not prose heuristics
- safe first-slice signals are:
  - `status in {fail, blocked, waived}`
  - presence of explicit waiver reason
  - presence of related-story linkage
  - explicit promotion flag added later

## ACA retrieval fit

Promoted room-agile notes should be searchable so future tasks can answer:

- have we run a similar epic before?
- what kinds of validations usually block this subsystem?
- what retro guidance came out of similar delivery loops?
- what milestone contracts tended to be too weak?
- what recurring process mistakes show up in room retros?

ACA should bias retrieval with those notes, but not override current room state
or current repo truth.

## Minimum first slice

The first ACA integration slice should probably do only this:

1. define the promotion-ready note schema
2. define promotion candidate rules
3. define provenance/frontmatter requirements
4. define wikilink/body conventions
5. add a draft-promotion path from room agile artifacts into ACA proposal notes

That is enough to start accumulating durable semantic memory without
over-automating.

## Suggested command direction

Possible command surface:

```bash
foxctl room aca promote epic <room-id> <epic-id>
foxctl room aca promote milestone <room-id> <milestone-id>
foxctl room aca promote retro <room-id> <guidance-update-id>
foxctl room aca promote validation <room-id> <validation-id>
```

These commands should likely produce:

- ACA proposal payloads
- draft note paths
- work-packet metadata

And not direct canonical-note mutation in v1.

Idempotency and re-promote semantics:

- promotion should be idempotent by source object id + target note class
- re-promoting the same source should refresh or supersede the existing draft/proposal packet rather than creating silent duplicates
- the first slice should return enough metadata to distinguish:
  - first promotion
  - refreshed promotion
  - already-current promotion

## Gaps to watch

Likely gaps or risks in this slice:

1. Over-promotion
   - too many low-value notes make ACA noisy
2. Weak provenance
   - promoted notes become detached from the room/work-pack source
3. Title churn
   - human-friendly note names drift, but stable ids solve this only if enforced
4. Duplicate semantic notes
   - multiple similar milestone or retro notes need a proposal/review path, not blind creation
5. Retrieval confusion
   - ACA notes must bias or enrich retrieval, not replace current room/repo truth
6. Draft-path ambiguity
   - this slice needs a concrete ACA proposal/draft mechanism instead of only describing a future handoff
7. Large payload sprawl
   - long milestone summaries or evidence-heavy drafts should use bounded summaries plus artifact/CAS pointers when needed

## Definition of done

This slice is done when:

1. the room/work-pack/ACA layering is explicitly defined
2. promotion candidates and exclusions are explicit
3. note classes and frontmatter are explicit
4. wikilink conventions are explicit
5. provenance/backlink requirements are explicit
6. draft-path, idempotency, and required-field rules are explicit
7. Cursor review confirms whether any critical gaps remain before implementation

## Open questions

1. Should epic and milestone promotion be manual-only at first, or suggested automatically when completion occurs?
2. Should retro and validation promotions go into the ACA proposal lane automatically when they match high-signal criteria?
3. Should promoted room notes live in a dedicated vault folder such as `projects/foxctl/room-*`, or merge directly into existing repo-note hierarchies after review?
4. Should story-level notes ever be promoted directly, or should stories mostly surface through milestone/validation promotion?
