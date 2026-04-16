---
name: foxctl-factory-epic-creation
description: "Import a Factory mission into foxctl and normalize it into a canonical room-agile epic creation artifact. Use when converting Factory mission structure, validation, and evidence into a foxctl epic, cross-checking epic-creation Definition of Done, filling missing planning structure, and explicitly stopping before implementation begins."
---

# foxctl-factory-epic-creation

Use this skill when the job is:

- import a Factory mission into foxctl
- review the imported epic as a planning artifact
- normalize missing structure in foxctl
- cross-check the imported epic against an epic-creation Definition of Done
- prepare the epic so it can later serve as the implementation plan

Do not use this skill for implementation work. This skill is about `epic creation`, not `epic implementation`.

## Mental model

- Factory mission import is an input, not the final plan
- Factory import is one intake adapter into the canonical room epic pipeline
- foxctl room-agile is the canonical planning surface after import
- `epic creation` ends when the epic is structurally ready for implementation
- `epic implementation` starts only after the creation DoD is met
- do not let import-time evidence or source progress trick you into treating the epic as already implemented

Read [references/epic-creation-dod.md](./references/epic-creation-dod.md) before making structural decisions.

## Operating sequence

### 1. Import the Factory mission

Use the deterministic importer:

```bash
foxctl room epic import-factory <room-id> \
  --mission-dir ~/.factory/missions/<mission-dir> \
  --sender <coordinator>
```

Treat the imported result as a draft executable epic, not as implementation truth.

### 2. Compare Factory structure against foxctl structure

Check:

- `mission.md` brief quality
- feature decomposition shape in `features.json`
- validation contract density in `validation-contract.md`
- validation state shape in `validation-state.json`
- handoff and transcript evidence under `handoffs/` and `worker-transcripts.jsonl`
- imported foxctl epic, milestone, story, validation, and delivery-log views

Use the imported room-agile epic as the comparison target:

```bash
foxctl room epic show <room-id> <epic-id>
foxctl room show <room-id>
```

### 3. Normalize the epic for creation readiness

Fill planning gaps in foxctl without starting implementation.

Typical creation-time upgrades:

- add or tighten milestone contracts
- add milestone criteria
- add validator lanes when the source clearly implies them
- split oversized imported stories into narrower accepted stories or proposals
- add missing validator stories when the source uses validator phases
- preserve source evidence by attaching or referencing artifacts in story validation
- add explicit blockers, dependencies, and open questions

If an imported story is too large or mixes multiple concerns, do not implement it. Refactor the plan:

- preserve the imported story for provenance
- add better-shaped stories under the milestone
- use milestone contract/criteria to make the new structure executable

### 4. Cross-check the epic-creation DoD

Use the DoD reference as a hard gate, not a suggestion.

Prefer running the `foxctl-epic-grader` rubric after normalization so the epic
gets a consistent score, blockers, and upgrade actions before anyone starts
implementation.

Expected output after review:

- `READY` when the epic is creation-complete
- `NOT READY` when structural planning gaps remain

When `NOT READY`, list only planning gaps:

- missing validator lanes
- missing evidence expectations
- oversized stories
- missing criteria or dependencies
- unresolved source contradictions
- missing provenance/evidence mapping

Do not list implementation tasks as if they were epic-creation blockers unless they reveal missing plan structure.

### 5. Stop before implementation

If the epic-creation DoD is met:

- checkpoint the epic
- summarize what makes it implementation-ready
- stop

Do not start coding, validating runtime behavior, or moving stories into active implementation as part of this skill.

## Commands to prefer

```bash
foxctl room epic show <room-id> <epic-id>
foxctl room milestone show <room-id> <milestone-id>
foxctl room milestone contract <room-id> <milestone-id> ...
foxctl room milestone criteria <room-id> <milestone-id> "..."
foxctl room story show <room-id>
foxctl room epic checkpoint <room-id> <epic-id> --note "epic creation review"
```

If the source mission implies validation phases, use foxctl evidence lanes and validator typing explicitly instead of leaving everything as a generic audit lane.

## Deliverable

Your deliverable is a foxctl epic that is ready to become the implementation plan.

That means:

- structurally complete
- validation-aware
- evidence-aware
- milestone- and story-shaped for execution
- clearly separated from implementation work

Do not claim success because the Factory mission was already detailed. Success means the imported epic is now a canonical foxctl planning artifact that passes the epic-creation DoD.
