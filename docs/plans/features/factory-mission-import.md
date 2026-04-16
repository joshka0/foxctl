# Factory Mission Greenfield Import

| Field | Value |
|-------|--------|
| Status | Proposal |
| Scope | Deterministic greenfield import from `~/.factory/missions/<mission-dir>` into the room-agile epic/milestone/story ledger and work-pack mirror |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md), [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-epic-resume-and-next.md](./room-epic-resume-and-next.md) |

## Conclusion

Yes. Deterministic import is feasible, and the canonical target is the
**room-agile epic system**.

Import should now be understood as one **intake adapter** into the broader
[canonical room epic pipeline](./room-epic-pipeline.md), not as a special
one-off lifecycle. The Factory-specific work ends when the source mission has
been deterministically converted into canonical room-agile board state.

Why this is feasible:

1. Factory mission state is already structured and durable:
   - `mission.md`
   - `features.json`
   - `validation-state.json`
   - `progress_log.jsonl`
   - `state.json`
2. The room-agile ledger accepts caller-supplied `BoardMessage.ID` values and
   caller-supplied `CreatedAt` timestamps.
3. Room work-packs are already a derived mirror of canonical board messages, so
   imported missions can immediately project into `~/.foxctl/epics/<epic-id>/...`
   without inventing a second storage model.

Why this is a hard cut:

- there is no `tasks.db` anchor-epic projection
- there is no dual-write path
- there is no compatibility import into flat tasks first
- Factory missions import directly into the canonical room-agile graph or not at all

## Recommended target

- `foxctl room` agile ledger
- imported as `epic`, `epic_finalize`, `milestone`, `story`,
  `story_state`, `story_validation`, and `delivery_log` messages

## Category boundary

Factory import belongs to `epic creation`, not `epic implementation`.

That distinction is intentional:

- `epic creation` means import the Factory mission, normalize it into canonical
  room-agile structure, preserve validation and evidence provenance, and close
  planning gaps until the epic is ready to serve as the implementation plan
- `epic implementation` means execute the stories in that already-normalized
  foxctl epic

Import success alone does not mean the mission is ready for implementation.
The imported epic may still need foxctl-native refinement:

- split oversized stories
- add validator lanes or validator stories
- tighten milestone contracts and criteria
- preserve source evidence more explicitly
- reconcile source contradictions before execution begins

The recommended operator surface for that loop is the
`foxctl-factory-epic-creation` skill, which treats Factory import as the input
to a foxctl epic-creation rubric rather than as the implementation plan by
default.

Within the canonical pipeline, Factory import occupies only the intake stage:

1. `factory_import` adapter establishes the first canonical epic graph
2. generic status, scout, advisor, grading, and frontier stages shape it
3. implementation begins only after that generic pipeline says the epic is dispatchable

## Deterministic source mapping

### Source -> target shape

| Factory source | Room-agile target | Deterministic rule |
|---|---|---|
| mission directory | epic | one mission directory becomes one room epic |
| `features[].milestone` | milestone | group features by exact milestone key |
| `features[]` | story | one feature becomes one story |
| `validation-state.assertions` filtered by `features[].fulfills` | story validation | derive one imported validation summary per story |
| `progress_log.jsonl` high-signal events | delivery log | import only durable mission events, in timestamp order |
| `state.json.state` | epic continuity note | map to imported checkpoint or delivery-log continuity only |

### Stable IDs

Do not mint fresh ULIDs during import. Supply stable IDs directly to
`BoardMessage` and treat those ids as the only canonical outward identifiers for
the imported graph.

Recommended id scheme:

- epic: `factory-mission-<mission-dir>`
- epic finalize: `factory-mission-<mission-dir>-finalize`
- milestone: `factory-mission-<mission-dir>-milestone-<milestone-key>`
- story: `factory-mission-<mission-dir>-story-<feature-id>`
- story validation: `factory-mission-<mission-dir>-story-<feature-id>-validation`
- delivery log: `factory-mission-<mission-dir>-log-<progress-index>`

These ids are:

- deterministic across re-imports
- path-safe for work-pack directories
- independent of wall-clock replay timing

### Stable timestamps

Preserve source timestamps when available:

- epic `CreatedAt`: earliest known mission timestamp
  - prefer `state.json.createdAt`
  - else first `progress_log.jsonl.timestamp`
- delivery-log timestamps: exact `progress_log.jsonl.timestamp`
- story validation timestamps: derived import timestamp from the best source
  - prefer latest related `worker_completed.timestamp`
  - else milestone-level validation marker when present
  - else mission `updatedAt`

## Field mapping

### Epic

Populate epic body deterministically from `mission.md` and `state.json`:

- title: first H1 from `mission.md`
- goal: first paragraph under `## Plan Overview` when available
- outcome: first paragraph under `## Expected Functionality` when available
- owner: importer-supplied coordinator identity
- scope: milestone keys from `features.json`
- success: compact summary of validation contract coverage when available

Then write an `epic_finalize` message immediately after the epic message so the
imported epic is born directly into the canonical executable room-agile shape.

### Milestones

Create one milestone per distinct `features[].milestone`.

Deterministic milestone fields:

- title: deterministic titleization of the milestone key
- goal: `"Imported Factory milestone <key>"`
- objective: summary built from the milestone's feature descriptions
- scope: feature ids in that milestone
- validators expected: leave empty unless the source contains an explicit typed
  validator contract

Do not infer validator lanes from keywords. Factory assertions do not currently
carry typed evidence-lane semantics, so the import leaves lane typing unset.

### Stories

Create one story per feature.

Story title:

- derived from `feature.id`, titleized, unless the importer can deterministically
  extract a clearer title from the first sentence of `description`

Story body should include:

- feature id
- raw description
- skill name
- preconditions
- expected behavior
- verification steps
- fulfills assertion ids
- worker session ids

### Story state

Map Factory feature status to room story lifecycle conservatively:

- `pending` -> `accepted`
- `in_progress` -> `in_progress`
- `completed` with imported validation `pass` or `waived` -> `done`
- `completed` with imported validation `fail` -> `blocked`
- `completed` with imported validation `blocked` -> `in_review`

This keeps room lifecycle and validation ledger consistent without adding
transitional state.

### Story validation

Import one synthetic validation record per feature using only structured source
signals, not keyword heuristics.

Recommended rule:

- validator type: `audit`
- status:
  - `pass` if all linked assertions are `passed`
  - `fail` if any linked assertion is `failed`
  - `blocked` if no linked assertion failed and at least one is still `pending`
  - omit validation if the feature links no assertions and there is no
    `worker_completed` evidence

Validation summary should include:

- assertion counts by status
- linked assertion ids
- latest worker completion outcome when present
- latest commit id when present

Optional fields:

- `command`: join deterministic verification steps if they are command-shaped
- `artifact_path`: leave empty in v1 unless we copy source artifacts into the
  story artifact directory
- `notes`: include concise import provenance such as source mission dir and
  source worker session ids

## Progress log import

Do not replay every Factory event as a room artifact. Import only the durable,
mission-relevant subset:

- `mission_accepted` -> epic provenance note or first delivery log entry
- `mission_run_started` -> delivery log entry
- `worker_completed` -> delivery log entry and input into story validation
- `handoff_items_dismissed` -> delivery log entry
- `mission_paused` / `mission_resumed` -> delivery log entries
- `milestone_validation_triggered` -> delivery log entry or milestone summary hint

Skip low-signal orchestration chatter such as:

- `worker_selected_feature`
- `worker_started`
- `worker_paused`

That keeps the room import readable while preserving the mission story.

## Import mode

Recommended first slice: **deterministic greenfield import with selected history**.

That means:

1. read current mission files
2. create deterministic board messages directly
3. preserve stable ids and timestamps
4. import only high-signal progress events into delivery logs
5. run one work-pack sync at the end

Do not shell out to user-facing `foxctl room ...` commands for the import path.
Those commands are correct for interactive use, but they:

- generate fresh ids by default
- enforce sequencing intended for humans
- make idempotent re-import harder

Instead, implement a dedicated importer that writes canonical board messages via
the blackboard store, then calls the existing work-pack sync path.

## Idempotency contract

Importer should support rerun without duplication.

Rules:

1. Reuse the same deterministic message ids on every run.
2. On re-import, compare the existing message body with the newly derived body.
3. If the message already exists with identical content, skip it.
4. If the message already exists with different content:
   - default to fail with a drift error
   - allow `--replace-imported` only for importer-owned ids
5. Never rewrite non-imported room messages.

This prevents the importer from trampling manually evolved room state.

There is no compatibility mode that also mirrors imported state into older epic
surfaces.

## Proposed CLI surface

```bash
foxctl room epic import-factory <room-id> \
  --mission-dir ~/.factory/missions/<mission-dir> \
  --sender actor:system:importer
```

Recommended flags:

- `--dry-run`
- `--replace-imported`
- `--include-progress-history`

## Acceptance criteria

1. The same mission directory imports to the same epic/milestone/story ids on
   every run.
2. Re-import is idempotent when source files are unchanged.
3. Work-pack output under `~/.foxctl/epics/<epic-id>/...` is stable.
4. No keyword heuristics are used for routing or evidence typing.
5. Imported room state can drive `epic show`, `epic resume`, `epic next`, and
   `room pulse` without special cases.
6. The importer does not write `tasks.db` anchor epics, legacy todo epics, or
   any secondary compatibility projection.

## Immediate implementation order

1. Add a `factory mission` reader that parses:
   - `mission.md`
   - `features.json`
   - `validation-state.json`
   - `progress_log.jsonl`
   - `state.json`
2. Add a deterministic mapper from Factory mission data to room-agile message
   specs.
3. Add an importer that writes board messages with explicit ids and timestamps.
4. Call existing room work-pack sync after the import batch.
5. Add golden tests covering:
   - id stability
   - status mapping
   - assertion-to-validation mapping
   - idempotent re-import
