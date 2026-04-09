# Room Agile Work-Packs and Story Validation

| Field | Value |
|-------|--------|
| Status | Draft for review |
| Depends on | Existing `agentctl room epic|milestone|story|log` agile protocol |
| Review requested from | `cursor-c-a` in `triad-20260404-202656` |

## Summary

Extend the room agile backend with two linked capabilities:

1. **Story-owned validation** as the primary durable proof of completion.
2. **Filesystem work-packs** under `~/.agentctl/epics` as a rich markdown/artifact mirror of the canonical room state.

The room database remains the source of truth. The filesystem mirror exists to
make briefs, summaries, reviews, and validation artifacts easy for humans and
agents to inspect and edit over long-running work.

This follows the strongest part of the Droid `.factory` pattern without
copying its exact filenames or moving source-of-truth semantics out of `agentctl room`.

## Goals

1. Make validation attach to the smallest durable unit of work: the **story**.
2. Make milestone summary a **rollup** of story evidence instead of a second copy.
3. Materialize a stable work-pack tree under `~/.agentctl/epics` keyed by room ids.
4. Keep room state canonical and machine-readable while allowing richer markdown artifacts.
5. Preserve transport independence: this belongs to `room`, not to tmux/zellij.

## Non-goals

1. Replacing room state with markdown files.
2. Requiring every artifact to be handwritten by users.
3. Copying Droid `.factory` filenames byte-for-byte.
4. Treating milestone summary as the only validation record.
5. Building the GUI first.

## Core policy

The policy is:

- `room` state is canonical
- work-pack files are derived mirrors plus editable narrative artifacts
- story validation is the primary proof of completion
- milestone summary is a synthesis layer
- cross-story validation is allowed, but must reference contributing story ids

In short:

- `story` owns evidence
- `milestone` owns synthesis
- `epic` owns continuity
- `~/.agentctl/epics` owns rich human-facing artifacts

## Proposed room model additions

### New story-level protocol

Add a new validation record under stories:

- `story_validation`

Each record should include:

- `validation_id`
- `created_at`
- `created_by`
- `story_id`
- `milestone_id`
- `epic_id`
- `validator_type`
- `status`
- `summary`
- `command`
- `artifact_path`
- `artifact_digest`
- `notes`
- `waiver_reason`
- `related_story_ids` *(optional, for cross-story checks)*

Supersession policy:

- validation records are append-only
- the latest record for a given `validator_type` supersedes earlier records of that same type for rollup purposes
- later non-waived validations may supersede earlier waivers

### Validation status values

Use explicit states:

- `pass`
- `fail`
- `blocked`
- `waived`

### Validator types

Start with a small explicit set:

- `review`
- `test`
- `integration`
- `user_test`
- `manual_check`
- `audit`

This avoids stringly typed ad hoc validation names.

## Work-pack layout

Each epic gets a stable root under `~/.agentctl/epics/<epic-id>/`.

Recommended layout:

```text
~/.agentctl/epics/<epic-id>/
  epic.md
  meta.json
  delivery-log.md
  milestones/
    <milestone-id>/
      milestone.md
      meta.json
      summary.md
      criteria.md
      stories/
        <story-id>/
          story.md
          meta.json
          validation/
            <validation-id>.md
            <validation-id>.json
          artifacts/
```

Rules:

- directory names use stable ids, not human titles
- markdown files are for narrative editing and review
- json/meta files are for deterministic machine reads when needed
- `artifacts/` can contain richer outputs or referenced copies
- every `meta.json` and validation json payload must include `schema_version`

## Canonical vs mirrored data

### Canonical in room state

Keep these canonical in the room ledger:

- epic identity and intake/finalization state
- milestone identity and criteria
- story identity, ownership, proposal/acceptance state
- story validation records
- milestone review verdict
- milestone summary metadata
- delivery log entries

### Mirrored in work-pack files

Mirror these into files:

- epic brief
- milestone brief
- criteria checklist
- story brief
- validation narrative
- milestone synthesis summary
- delivery log markdown

Filesystem artifacts should be regenerateable from room state where practical.

## Command surface

### New command

Add:

```bash
agentctl room story validate <room-id> <story-id> <validator-type> <pass|fail|blocked|waived> <summary>
```

Flags:

- `--workspace`
- `--sender`
- `--artifact-path`
- `--artifact-digest`
- `--command`
- `--notes`
- `--related-story <id>` repeatable

Command invariants:

- if `artifact_digest` is set, `artifact_path` is required
- `waived` requires non-empty waiver notes/reason
- waivers may be recorded only by the story owner or coordinator
- all `related_story_ids` must resolve to stories in the same epic
- for the current slice, `related_story_ids` must also be in the same milestone

### Follow-on command ideas

Add later if needed:

- `agentctl room story validation show <room-id> <story-id>`
- `agentctl room story validation list <room-id> <story-id>`
- `agentctl room workpack materialize <room-id> <epic-id>`
- `agentctl room workpack sync <room-id> <epic-id>`

For the first slice, `story validate` plus automatic work-pack materialization is enough.

## Milestone summary behavior

Milestone summary should not duplicate every story validation body.

It should include:

- milestone status
- accepted stories
- validation coverage counts
- blocking validations
- cross-story validation records
- artifact links
- short synthesized narrative

Recommended fields:

- `story_count`
- `accepted_story_count`
- `validated_story_count`
- `passed_story_count`
- `failed_story_count`
- `blocked_story_count`
- `waived_story_count`
- `coverage`
- `blocking_validation_ids`

`summary.md` can include richer prose, but its structured payload must be derivable from story validation state.

Coverage semantics:

- `accepted_story_count` = accepted stories under the milestone
- `validated_story_count` = accepted stories whose effective latest validation state is `pass` or `waived`
- `passed_story_count` = accepted stories with at least one effective `pass` and no effective `fail`/`blocked`
- `failed_story_count` = accepted stories with any effective `fail`
- `blocked_story_count` = accepted stories with any effective `blocked`
- `waived_story_count` = accepted stories with only effective `waived` validations and no effective `pass`/`fail`/`blocked`
- `blocking_validation_ids` must be stable-sorted

## Materialization rules

Materialization mode:

- room state remains canonical
- work-pack materialization runs synchronously after room writes in the first slice
- if materialization fails, the room write may already exist, and repair/resync remains possible
- a later repair/sync command can tighten this into an explicit reconciler if needed

### On epic start/finalize

Materialize or refresh:

- `epic.md`
- `meta.json`

### On milestone start/criteria/review/summary

Materialize or refresh:

- `milestones/<milestone-id>/milestone.md`
- `milestones/<milestone-id>/meta.json`
- `milestones/<milestone-id>/criteria.md`
- `milestones/<milestone-id>/summary.md`

### On story add/accept/validate

Materialize or refresh:

- `stories/<story-id>/story.md`
- `stories/<story-id>/meta.json`
- `stories/<story-id>/validation/<validation-id>.md`
- `stories/<story-id>/validation/<validation-id>.json`

## Why this is the right boundary

If validation lives only at the milestone level:

- the smallest durable completion unit becomes ambiguous
- ownership is blurred
- milestone summary turns into a second source of truth
- partial completion is harder to reason about

If validation lives at the story level:

- completion proof aligns with assignment scope
- milestone synthesis becomes clean aggregation
- traceability stays explicit
- cross-story gates can still be modeled deliberately

This mirrors the most useful part of `.factory`: feature-level evidence plus milestone-level synthesis.

## Definition of done

A story is not done unless it has:

1. a parent milestone
2. accepted scope
3. at least one validation record or explicit waiver
4. final state reflected in room ledger

A milestone is not review-pass unless:

1. its required stories have validation coverage or explicit waivers
2. its milestone summary references blocking or waived evidence
3. any cross-story integration validation references contributing story ids

Waiver policy:

- waivers are explicit exceptions, not silent success
- a waiver requires a non-empty waiver reason
- waivers are append-only records and may be superseded by later validation records

## Rollout plan

### Phase 1

- add `story_validation` room kind
- add `agentctl room story validate`
- teach `milestone show` and `milestone summary` to aggregate story validation

### Phase 2

- materialize work-pack directories under `~/.agentctl/epics`
- generate `epic.md`, `milestone.md`, `story.md`, and validation markdown

### Phase 3

- add sync/rebuild commands
- add GUI support for story validation coverage and work-pack links

## Open questions

1. Should `artifact_path` prefer absolute paths or work-pack-relative paths in the room payload?
2. Do we want milestone-level validation records as a separate protocol kind, or only milestone review plus cross-story `story_validation` records?
