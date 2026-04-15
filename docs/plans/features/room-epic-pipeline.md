# Canonical Room Epic Pipeline

| Field | Value |
|-------|--------|
| Status | Proposal |
| Scope | One canonical pipeline for imported, authored, and hybrid room-agile epics from intake through dispatchable implementation plan |
| Related | [factory-mission-import.md](./factory-mission-import.md), [room-epic-resume-and-next.md](./room-epic-resume-and-next.md), [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [../general/refactor-scout.md](../../general/refactor-scout.md) |

## Conclusion

Room-agile should treat epic creation as a staged pipeline, not as a single
grade or a Factory-specific workflow.

The canonical pattern is the same one already used by refactor scout:

1. deterministic intake and structural discovery first
2. persisted artifacts and explicit status between stages
3. optional model-assisted ranking only after deterministic findings exist
4. lane-specific repair passes
5. dispatch only from the computed ready frontier

Import is one intake adapter. Greenfield authored epics are another. Hybrid
epics use the same pipeline after intake.

## Category boundary

This design keeps `epic creation` and `epic implementation` separate.

- `epic creation` means intake, shape, grade, repair, and prove that the epic
  is a dispatchable implementation plan
- `epic implementation` means dispatch execution work from that already-shaped
  epic, validate outcomes, and close work

An epic becomes implementation-eligible only after the creation pipeline says it
is structurally ready.

## Intake adapters

All intake modes must converge onto the same canonical room-agile graph:

- `factory_import`
  - structured mission import from `~/.factory/missions/<id>`
- `brief_draft`
  - create an epic from a human brief or room command
- `interview_draft`
  - create an epic by durable intake questions and answers
- `spec_parse`
  - derive the first draft from a repository doc, ADR, PRD, or implementation plan
- `hybrid_reconcile`
  - start from an imported or drafted epic and merge in additional foxctl-native shaping

The adapter is not the planning pipeline. It only establishes the first
canonical board state.

## Canonical pipeline

### 1. Intake

Goal:

- create or import the first canonical room-agile epic graph

Outputs:

- epic root
- finalized or draft brief
- milestones or milestone proposals
- stories or story proposals
- provenance metadata

Examples:

- `foxctl room epic import-factory ...`
- `foxctl room epic start ...`
- `foxctl room epic ask|answer|finalize ...`

### 2. Status

Goal:

- determine whether the epic has enough structure to enter shaping safely

This is the epic analogue of `refactor status`.

Outputs:

- intake mode: imported, authored, hybrid
- phase: draft, shaping, graded, dispatchable
- structural gaps:
  - missing outcome
  - missing owners
  - missing milestone contracts
  - missing criteria
  - missing dependency edges
  - missing validator lanes
  - open intake questions

### 3. Snapshot

Goal:

- persist a stable baseline so every shaping pass and repair pass has a durable
  reference point

Outputs:

- snapshot id
- artifact pointer
- compact summary of the current epic graph

### 4. Scout

Goal:

- deterministically surface planning defects and dispatchability defects

Recommended next implementation slice: **epic scout v1**.

This is the epic analogue of `refactor scout`.

#### Epic scout v1 (deterministic)

Command intent:

- `foxctl room epic scout <room-id> <epic-id>`
- output only deterministic defects and missing structure; no model ranking in v1
- consume `epic status` + `epic snapshot` outputs and emit a stable artifact for downstream ranking

Deterministic input surfaces for v1:

- canonical intake mode from status: `imported`, `brief_draft`, `interview_draft`, `spec_parse`, `hybrid_reconcile`
- status surface (or status cache) for phase, structural gaps, and canonical graph checksum
- snapshot record: `snapshot_id`, `snapshot_artifact`, and summary payload
- graph surfaces from room-agile state:
  - epic metadata and brief
  - milestones and milestone contracts
  - stories and story proposals (outcome, size, ownership, lane hints)
  - typed dependency edges (`depends_on`, `blocks`, `waiting_on_*`)
  - validator/evidence declarations and validation status
  - provenance breadcrumbs (source links and shaping amendments)
- no chat-log text classification and no unstructured LLM context

Initial rule families to emit:

- `brief_boundary`
  - missing or vague outcome, scope creep, or mixed audience/goal phrasing
- `milestone_contract`
  - milestone without contract, owner, acceptance criteria, or evidence hook
- `criteria_coverage`
  - missing acceptance criteria by milestone or story lane
- `story_scope`
  - oversized stories, story without clear single outcome, story mixes two concerns
- `dependency_topology`
  - missing/invalid `depends_on`, orphan dependencies, cycles
- `dependency_sequence`
  - implied milestone/story sequence from prose that is not represented in explicit edges
- `owner_dispatch`
  - missing owner, missing dispatch lane, or malformed assignment target
- `validation_lane`
  - no validator lane for required milestone/story outcomes
- `evidence_expectation`
  - missing evidence references where contract claims are high-confidence blockers
- `provenance_trace`
  - imported/authored/hybrid provenance not carried into canonical graph fields

Output shape at a high level:

- `data.scope`: room id, epic id, intake mode, status phase
- `data.snapshot_id`: snapshot id used by this run (or status/snapshot miss marker)
- `data.snapshot_artifact`: artifact digest for replayability
- `data.scout_artifact`: artifact digest for the findings bundle
- `data.findings`: stable list of objects with:
  - `id`, `rule_family`, `severity`, `entity_type`, `entity_id`, `title`, `summary`
  - `evidence` references (status snapshot, milestone/story/validation ids, optional dependency refs)
  - `recommended_repair_lanes` (one or more lane names)
- `data.signals`:
  - `total_findings`, `blocking_findings`, `family_counts`, `schema_version`
- optional `data.out_of_scope` list for deterministic conditions that skip checks

Explicitly out of scope for `epic scout v1`:

- model-assisted ranking (`epic advisor` stays optional and next)
- auto-remediation or automatic lane mutations
- frontier computation and dispatch decisions
- cross-epic comparisons
- rewriting or normalizing user-brief text
- keyword heuristics for routing or scoring

Relationship with adjacent stages:

- `epic status` is the required pre-read: if status says intake shape is not sufficient for safe shaping, scout still runs but marks `data.out_of_scope` entries for checks that cannot be computed.
- `epic grade` should consume `scout` severity and blocking counts as a major signal, but should not treat `epic scout` as proof completion.
- `epic frontier` should not read scout-only narrative findings; it should consume explicit dependency graph and deterministic blocker flags. Future frontier work can use scout `data.signals` to improve diagnostics, not to replace dependency math.

The scout is the discovery truth. Later advisory/model passes must consume the
scout output instead of making up a separate diagnosis from chat context.

### 5. Advisor

Goal:

- rank the deterministic scout findings into the highest-leverage shaping actions

This is optional and should follow the same contract as `refactor advisor`:

- scout findings remain authoritative
- the model ranks and sequences
- the output is a shortlist, not an alternate worldview

### 6. Repair Lanes

Goal:

- let multiple agents improve the epic in parallel without mixing planning work

Recommended lanes:

- brief lane
- milestone architecture lane
- story shaping lane
- dependency topology lane
- validation and evidence lane
- ownership and dispatch lane

Each lane should operate on scoped messages or fields, then push the epic back
through the pipeline.

### 7. Grade

Goal:

- score epic-creation quality only

The grade answers:

- is this a good implementation plan?
- what planning blockers remain?
- what upgrades most improve plan quality?

The grade should not decide what to execute next. That is the frontier stage.

### 8. Frontier

Goal:

- compute the current execution frontier from the dependency graph and state

Outputs:

- `ready_frontier`
  - stories whose dependencies are satisfied and are safe to dispatch now
- `blocked_frontier`
  - stories blocked by unmet upstream dependencies or validation gates
- `critical_path`
  - longest dependency chain
- `parallelism_width`
  - safe concurrency width from the current frontier
- `dependency_gaps`
  - missing or ambiguous dependency declarations that keep frontier computation weak

### 9. Dispatch

Goal:

- dispatch only the current ready frontier

Dispatch must be dependency-aware:

- do not dispatch blocked stories
- do not assume milestone order from prose alone
- re-run frontier after every material state change

### 10. Execution and Review

Goal:

- run implementation work against the dispatchable epic

Outputs:

- story state transitions
- validations
- milestone reviews
- milestone summaries
- delivery logs

This is intentionally outside epic creation.

## Dual outputs

The pipeline should expose two different result surfaces:

### 1. Plan grade

This answers:

- how good is this epic as an implementation plan?

This is the existing `READY` / `NOT READY` style score.

### 2. Dispatch state

This answers:

- what can run now?
- what is blocked?
- what upstream work unlocks the most downstream work?

These are different questions and should not be compressed into one number.

## Dependency model

To make frontier computation deterministic, room-agile should support explicit
dependency fields rather than inferring sequence from text.

Recommended explicit fields:

- milestone `depends_on`
- story `depends_on`
- story `blocks`
- typed blocker status:
  - `waiting_on_story`
  - `waiting_on_milestone`
  - `waiting_on_validation`
  - `external_dependency`

Validation gates should be allowed to satisfy dependencies when milestone
contracts say a downstream story requires a lane to be covered first.

## Grading by intake mode

Most rubric categories stay the same across all epics. The intake-specific part
should be generalized from pure import fidelity to intake traceability.

- imported epics
  - preserve provenance and source fidelity
- authored epics
  - preserve brief traceability and explicit assumptions
- hybrid epics
  - preserve both imported provenance and foxctl-native shaping decisions

The grader should score the canonical epic, but it should understand which
intake mode established that epic.

## Command surface

Recommended staged surface:

```bash
foxctl room epic status <room-id> <epic-id>
foxctl room epic snapshot <room-id> <epic-id>
foxctl room epic scout <room-id> <epic-id>
foxctl room epic advisor <room-id> <epic-id>
foxctl room epic grade <room-id> <epic-id>
foxctl room epic frontier <room-id> <epic-id>
foxctl room epic dispatch-frontier <room-id> <epic-id>
```

Intake adapters then feed the same pipeline:

```bash
foxctl room epic import-factory ...
foxctl room epic start ...
foxctl room epic ask ...
foxctl room epic finalize ...
```

## Definition of done

The pipeline succeeds when:

1. the epic has canonical room-agile structure
2. the grade says it is a strong implementation-plan candidate
3. dependency-aware frontier computation identifies what can run now
4. execution agents can start from the epic without first redesigning it

That is the definition of a dispatchable epic.
