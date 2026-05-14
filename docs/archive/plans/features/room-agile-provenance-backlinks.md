# Room Agile Provenance and Backlinks

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Make room state, work-packs, and ACA drafts cross-reference each other with stable provenance and backlinks |
| Related | [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-agile-aca-promotion.md](./room-agile-aca-promotion.md), [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md), [../architecture/context-architecture.md](../../../architecture/context-architecture.md) |

## Why this slice

The room-agile backend now has three useful layers:

1. `room`
   - canonical operational ledger
2. `~/.foxctl/epics/...`
   - rich work-pack mirror
3. ACA drafts / memory proposals
   - review-first semantic memory projection

What is still weak is the linkage between those layers.

Right now:

- room objects know some work-pack paths
- work-packs know their own local shape
- ACA drafts carry frontmatter provenance

But those links are not yet systematic enough to answer:

- what room message created this file?
- what work-pack file corresponds to this ACA note?
- what validation json and markdown belong to this promoted validation draft?
- what room summary or retro update produced this memory proposal?

That gap makes long-running continuity weaker than it should be.

## Goals

1. Make backlinks explicit and stable across room, work-pack, and ACA draft layers.
2. Make every promoted ACA draft traceable back to one or more room messages and work-pack artifacts.
3. Make work-pack files traceable back to canonical room ids without filename heuristics.
4. Keep stable ids authoritative; do not rely on prose titles or slugs for identity.
5. Keep the first slice additive and review-safe.

## Non-goals

1. Replacing room ids with filesystem ids.
2. Auto-merging ACA drafts into canonical vault notes.
3. Adding semantic retrieval logic in this slice.
4. Reworking the room protocol or message kinds.
5. Requiring every raw room chat message to have a work-pack file.

## Current gaps

### Room -> work-pack

Current room read models already expose some path hints:

- `workpack_root`
- `workpack_dir`
- `validation_dir`

But they do not consistently expose:

- the exact markdown path for the mirrored artifact
- the exact `meta.json` path
- the source room message ids that rendered that file

### Work-pack -> room

Current work-pack `meta.json` files carry the rendered object payloads, but not a strong backlink contract such as:

- `room_id`
- `room_message_ids`
- `source_kind`
- `source_id`
- `parent_ids`

### ACA draft -> work-pack / room

The thin ACA promotion slice already carries good frontmatter, but the next parity step is to make sure:

- work-pack metadata and ACA frontmatter use the same stable provenance keys
- note bodies reference the exact room/work-pack source ids consistently
- proposal payloads carry enough linkage to re-open the source from the CLI/UI

## Required provenance contract

Every mirrored or promoted artifact should carry these canonical fields where applicable:

- `workspace`
- `workspace_id`
- `room_id`
- `source_kind`
- `source_id`
- `epic_id`
- `milestone_id`
- `story_id`
- `validation_id`
- `guidance_update_id`
- `room_message_ids`
- `workpack_root`
- `workpack_path`
- `meta_json_path`
- `schema_version`
- `generated_at`

The canonical identity is still the room-side id.

## Source kinds

Use an explicit enum-like set for provenance:

- `epic`
- `milestone`
- `story`
- `story_validation`
- `guidance_update`
- `milestone_summary`
- `delivery_log`

Do not infer kinds from filenames.

`source_id` should always be the primary root object id for that artifact class:

- epic root id for `epic`
- milestone root id for `milestone`
- story root id for `story`
- validation message id for `story_validation`
- latest summary message id for `milestone_summary` when one exists, otherwise the milestone id
- epic id for aggregated `delivery_log` files

`room_message_ids` should carry the ordered set of room messages used to render or
sync that artifact.

## First implementation slice

### 1. Strengthen work-pack `meta.json`

Add explicit provenance blocks to:

- `~/.foxctl/epics/<epic-id>/meta.json`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/meta.json`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/meta.json`
- story validation `*.json`

Suggested shape:

```json
{
  "schema_version": 1,
  "generated_at": "2026-04-07T18:00:00Z",
  "provenance": {
    "workspace": "/Users/joshka/repos/personal/foxctl",
    "workspace_id": "/Users/joshka/repos/personal/foxctl",
    "room_id": "triad-20260404-202656",
    "source_kind": "story_validation",
    "source_id": "01ABC...",
    "epic_id": "01EPIC...",
    "milestone_id": "01MILE...",
    "story_id": "01STORY...",
    "validation_id": "01VAL...",
    "room_message_ids": ["01ABC..."],
    "workpack_root": "/Users/.../.foxctl/epics/01EPIC...",
    "workpack_path": "/Users/.../.foxctl/epics/01EPIC.../milestones/01MILE.../stories/01STORY.../validation/01VAL....md",
    "meta_json_path": "/Users/.../.foxctl/epics/01EPIC.../milestones/01MILE.../stories/01STORY.../validation/01VAL....json"
  }
}
```

### 2. Strengthen markdown headers

Add a short provenance block near the top of work-pack markdown files, for example:

- Room id
- Source id
- Parent ids
- Canonical message id

This should stay compact and human-readable.

### 3. Align ACA draft provenance with work-pack provenance

ACA draft frontmatter should keep the same field names as work-pack provenance where possible.

That means:

- do not invent alternative key names between the two layers
- use the same ids
- expose both `workpack_path` and `meta_json_path`

### 4. Expose provenance in room read models

`epic show`, `milestone show`, `story show`, and maybe `retro show` should expose:

- markdown path
- meta json path
- source/root message ids
- for summaries and validations, the current artifact pair paths

That gives the CLI/UI enough information to deep-link without recomputing paths.

## Link conventions

### Work-pack markdown -> ACA

Use lightweight human-visible references only where useful, for example:

- `ACA draft candidates live under .foxctl/templates/obsidian-vault/inbox/drafted-from-foxctl/...`

Do not try to hardcode final canonical vault destinations in work-pack markdown.

### ACA -> work-pack

ACA drafts should keep:

- `workpack_root`
- `workpack_path`
- `meta_json_path`

and may include short body links such as:

- `Work-pack: /Users/.../.foxctl/epics/...`

### Room -> artifact

Room read models should expose exact paths, not just roots.

Examples:

- epic markdown path
- milestone markdown + summary path
- story markdown path
- validation markdown + json path

## Idempotency expectations

This slice should be purely additive.

Re-syncing a work-pack should:

- overwrite generated markdown/json deterministically
- keep provenance stable for the same source object
- not create duplicate room objects

Re-promoting an ACA draft should still dedupe by source object id + note class.

## Risks

1. Over-linking
   - too much boilerplate in markdown makes the work-pack less readable
2. Drift between work-pack and ACA provenance keys
   - avoid by using one stable field contract
3. Source-id ambiguity for synthesized artifacts
   - summaries and logs may need both `source_id` and `room_message_ids`
4. Path brittleness
   - ids remain canonical; paths are projections

## Definition of done

This slice is done when:

1. work-pack `meta.json` files include explicit provenance blocks
2. work-pack markdown has compact backlink headers
3. ACA draft provenance aligns with the same key names
4. room read models expose artifact and meta paths consistently
5. focused tests verify at least one epic, one milestone, one story validation, and one ACA draft path carry the expected provenance fields
