---
name: foxctl-room-agile
description: "Run agile workflow inside a foxctl room with durable epic intake, milestone and story proposals, reviews, and delivery logs."
---

# foxctl-room-agile

Use this skill when a room needs a durable agile workflow instead of only free-form chat, tasks, or interviews.

This skill assumes:

- the room already exists
- `foxctl-room` remains the base room protocol
- `foxctl-room-operator` still governs day-to-day participant behavior

This skill adds the explicit agile layer:

- `epic`
- `milestone`
- `milestone proposal`
- `story`
- `story proposal`
- `acceptance criteria`
- `milestone review`
- `milestone summary`
- `delivery log`
- `story validation`
- `work-pack mirror`

## When to use me

- a long-running objective needs durable scoping across many sessions
- the coordinator wants milestone proposals before committing to execution
- stories should be proposed and accepted explicitly instead of appearing ad hoc
- review synthesis should live in the room, not only in transient chat
- stories need durable validation evidence, not just “done” chat
- the room should externalize long-running work into stable work-pack files under `~/.foxctl/epics`

## Mental model

- `epic` is the long-running objective
- one mission should normally stay one `epic` across many milestones
- do not start a second `epic` just because the next tranche or milestone begins
- start a new `epic` only when the mission/outcome is materially different, not when the same mission continues
- `epic` starts in discovery, not execution
- `epic ask` / `epic answer` / `epic finalize` make the intake interview durable
- `epic shape` turns the finalized brief into durable milestone proposals
- `epic resume` reconstructs the current operational state after a gap
- `epic next` extracts the next concrete actions without rereading the whole room
- `epic checkpoint` captures a durable resumability snapshot from the current `resume` and `next` state
- `room pulse` is the room-wide mission-control surface across multiple epics
- `milestone start --proposal` promotes one proposal into a real milestone
- `story propose` / `story accept` do the same one level down
- `milestone review` records the pass/block verdict
- `milestone summary` records the synthesis after review
- `log append` is the cross-session delivery journal for the epic
- `story validate` attaches proof at the story level
- `~/.foxctl/epics/<epic-id>/...` is the rich markdown/artifact mirror; room state stays canonical
- `room aca promote` projects high-signal completed agile artifacts into ACA draft notes for later semantic retrieval

## Operating sequence

### 1. Start the epic

Use `epic start` once per mission. After that, keep extending the same mission with more milestone proposals and milestone starts instead of opening a sibling epic for the next tranche.

```bash
foxctl room epic start <room-id> "Room agile protocol" \
  --goal "Give the room a durable epic/milestone/story hierarchy" \
  --owner human-a \
  --scope room \
  --scope gui-agent \
  --success "agents can orient from room state"
```

### 2. Run epic intake before opening milestones

Use typed questions:

- `product`
- `technical`
- `constraint`
- `success`

```bash
foxctl room epic ask <room-id> <epic-id> \
  "What constraints must the first tranche respect?" \
  --kind constraint \
  --to human-a

foxctl room epic ask <room-id> <epic-id> \
  "What must be true before milestones can open?" \
  --kind success \
  --to human-a

foxctl room epic answer <room-id> <question-id> \
  "The first tranche must stay transport-agnostic and room-native."
```

Do not open milestones while the epic is still:

- `discovery`
- `intake_in_progress`
- `ready_to_finalize`

### 3. Finalize the epic brief

```bash
foxctl room epic finalize <room-id> <epic-id> \
  "Clarified brief: ship the room agile layer first, then surface it in the GUI."
```

### 4. Shape milestones from the finalized brief

```bash
foxctl room epic shape <room-id> <epic-id>
foxctl room epic show <room-id> <epic-id>
foxctl room epic resume <room-id> <epic-id>
foxctl room epic next <room-id> <epic-id>
foxctl room epic checkpoint <room-id> <epic-id> --note "Coordinator handoff note"
foxctl room pulse <room-id>
```

This writes `milestone proposal` messages into the room. Review them before execution starts.

When resuming after a pause, prefer:

```bash
foxctl room epic resume <room-id> <epic-id>
foxctl room epic next <room-id> <epic-id> --actor <you>
foxctl room epic checkpoint <room-id> <epic-id> --actor <you> --note "Where to resume from next"
```

### 5. Promote one milestone proposal into a real milestone

```bash
foxctl room milestone start <room-id> <epic-id> --proposal <proposal-id>
```

Prefer attaching the milestone contract at creation time for non-trivial work:

```bash
foxctl room milestone start <room-id> <epic-id> --proposal <proposal-id> \
  --objective "Make story validation and work-pack sync operational." \
  --risk "Multi-epic rooms may overcount unrelated interview sessions." \
  --exclude "GUI changes" \
  --dependency "epic finalized" \
  --validator review \
  --validator test \
  --required-lane review \
  --required-lane integration \
  --optional-lane user_test \
  --exit "accepted stories are validated"
```

If the contract changes later, update it explicitly instead of smuggling those changes into summary text:

```bash
foxctl room milestone contract <room-id> <milestone-id> \
  --objective "..." \
  --risk "..." \
  --dependency "..." \
  --validator review \
  --required-lane integration \
  --optional-lane user_test \
  --exit "..."
```

Behavior:

- `--objective` replaces the objective narrative when provided
- repeated list flags on `milestone contract` are cumulative for the fields you pass
- cumulative list fields are unioned, deduped, and stable-sorted
- `required-lane` and `optional-lane` use the same lane keys as evidence lanes (`review`, `test`, `integration`, `user_test`, `manual_check`, `audit`)
- optional lanes stay disjoint from required lanes after merge
- omitted fields remain unchanged

Milestone evidence policy is guidance-only in v1:

- `milestone show` exposes `required_lane_status`, `required_lane_missing`, and `required_lane_covered`
- `milestone show` also exposes `exit_policy.status`, `exit_policy.reasons`, and `exit_policy.checks`
- `epic next` may emit `cover_required_lane`
- `epic health` may warn with `milestone_missing_required_lane`
- `epic next` and `epic health` now align on the same derived milestone exit-policy helper
- `room pulse` aggregates those epic-level signals across the whole room for the coordinator
- milestone review writes are not auto-rejected on policy gaps in this slice

If a room wants strict pass-review discipline, milestone contract can opt into enforcement:

- `--enforce-exit-policy`
- `--no-enforce-exit-policy`
- when enabled, `milestone review ... pass ...` is rejected unless the milestone exit policy is `ready_for_review` or `ready_to_exit`
- `block` verdicts remain allowed
- milestone contract updates can explicitly turn enforcement back off again with `--no-enforce-exit-policy`

Then add explicit acceptance criteria:

```bash
foxctl room milestone criteria <room-id> <milestone-id> \
  "Epic and milestone hierarchy is visible via show commands"
```

### 6. Propose and accept stories under the milestone

```bash
foxctl room story propose <room-id> <milestone-id> \
  "Implement story proposal flow" \
  "Add story propose and accept commands." \
  --owner gemini-a \
  --rationale "Needed before agents can refine milestone internals."

foxctl room story show <room-id>
foxctl room story accept <room-id> <milestone-id> <story-proposal-id>
```

Use `story add` only when a proposal step would add no value.

### 7. Track execution state explicitly

Use `story state` for execution lifecycle, not proof:

```bash
foxctl room story state <room-id> <story-id> in_progress \
  --reason "Started implementation"

foxctl room story state <room-id> <story-id> in_review \
  --reason "Ready for review" \
  --reviewer human-a

foxctl room story state <room-id> <story-id> blocked \
  --reason "Waiting on cross-story decision" \
  --blocked-by milestone-summary
```

Rules:

- `story state` is append-only
- use it for execution state, not proof
- `blocked` and `deferred` require `--reason`
- `done` requires the latest story validation status to be `pass` or `waived`
- `validated` and `waived` story states must not contradict the latest story validation
- milestone `validated_story_count` remains proof/coverage-based, not strictly `state == validated`
### 8. Review and synthesize the milestone

```bash
foxctl room milestone review <room-id> <milestone-id> pass \
  "Foundation slice met the milestone criteria"

foxctl room milestone summary <room-id> <milestone-id> \
  --summary "Review synthesis: the milestone passed, accepted stories are clear, and the next tranche can start." \
  --passed-criterion "Accepted stories are validated" \
  --decision "Keep summary separate from proof" \
  --finding "Room follow-ups should be acked when no reply is needed" \
  --next "Start the story lifecycle slice" \
  --guidance "Use milestone summary for synthesis, not proof"
```

### 9. Promote durable work into ACA drafts when the signal is worth keeping

Use ACA promotion for completed/high-signal artifacts, not routine chat churn:

```bash
foxctl room aca promote epic <room-id> <epic-id>
foxctl room aca promote milestone <room-id> <milestone-id>
foxctl room aca promote retro <room-id> <guidance-update-id>
foxctl room aca promote validation <room-id> <validation-id>
```

Rules:

- the room/work-pack remain canonical
- ACA promotion is review-first and draft-only in v1
- routine green validations should usually stay out of ACA
- failed, blocked, and waived validations are the safest first promotion targets

`milestone review` is the verdict.

`milestone summary` is the durable synthesis.

Rules:

- positional `milestone summary <room> <milestone> "<notes>"` still works as shorthand
- if both positional notes and `--summary` are provided, `--summary` wins
- `blocking_validation_ids` and `waived_validation_ids` must reference existing story validations in the same milestone
- summary should summarize or reference proof, not paste large validation bodies or artifacts

### 10. Attach validation to accepted stories

Use story-level validation as the primary proof unit:

```bash
foxctl room story validate <room-id> <story-id> review pass \
  "Code review passed with no blocking findings." \
  --sender human-a \
  --artifact-path docs/reviews/story.md \
  --command "go test ./cmd/foxctl/cmd" \
  --notes "Validated after the second pass."
```

Rules:

- validation belongs to the story, not only to the milestone
- `artifact-digest` requires `artifact-path`
- `waived` requires explicit waiver notes and owner/coordinator authority
- related stories must resolve inside the same milestone for the current slice

### 11. Capture retro guidance separately from milestone synthesis

Use retro guidance for what should change next time, not for restating milestone proof:

```bash
foxctl room retro add <room-id> <epic-id> \
  --milestone <milestone-id> \
  --kind coordination \
  --summary "Ack no-blocker follow-ups." \
  --impact "Prevents stale reply-expected inbox items." \
  --change "Ack no-blocker follow-ups instead of waiting for another reply." \
  --scope room \
  --scope review-loop \
  --follow-up "Document this in the room-agile skill"

foxctl room retro show <room-id> <epic-id>
```

Rules:

- retro guidance is append-only
- the coordinator owns retro writes in this slice
- `kind` must be one of:
  - `process`
  - `tooling`
  - `coordination`
  - `quality`
  - `delivery`
- `summary`, `impact`, and `--change` are required
- if `--milestone` is set, it must belong to the same epic
- use `milestone summary` for what happened
- use `retro add` for what should change next time

### 11. Let the work-pack mirror stay in sync

After epic, milestone, story, review, summary, and validation writes, foxctl materializes:

- `~/.foxctl/epics/<epic-id>/epic.md`
- `~/.foxctl/epics/<epic-id>/meta.json`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/milestone.md`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/meta.json`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/story.md`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/meta.json`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/validation/<validation-id>.md`
- `~/.foxctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/validation/<validation-id>.json`

Use the explicit backend surface when you need to inspect or refresh that mirror:

```bash
foxctl room workpack show <room-id> <epic-id>
foxctl room workpack sync <room-id> <epic-id> --sender human-a
```

### 12. Keep the epic delivery log current

```bash
foxctl room log append <room-id> <epic-id> "Foundation landed" \
  --completed "CLI hierarchy shipped" \
  --in-flight "GUI surfacing" \
  --next "Wire status and planning views"
```

## Default coordinator behavior

- own epic intake and finalization
- use `epic shape` before improvising milestones
- prefer `milestone start --proposal`
- prefer `story propose` then `story accept` for non-trivial work
- use `story state` to make in-progress, review, blocked, and deferred work durable
- treat `story validate` as required closure evidence, not an optional afterthought
- write `milestone summary` after reviews so the next session does not need to reconstruct conclusions
- keep `log append` current at tranche boundaries

## Default participant behavior

- read `epic show`, `milestone show`, and `story show` before starting
- after a long gap, read `epic resume` and `epic next` before reconstructing state manually
- if intake is still open, answer epic questions instead of starting implementation
- if a story is only proposed, wait for acceptance before treating it as committed execution scope
- when work starts or stalls, update `story state` instead of relying on chat context alone
- once a story is implemented, attach validation before calling it done
- keep detailed work notes in room tasks; keep scope/acceptance decisions in the agile layer

## Anti-patterns

Do not:

- skip epic intake and jump straight to milestones
- open a milestone before `epic finalize`
- treat shaped proposals as implicit acceptance
- call a story done without `story validate` or an explicit waiver
- bury milestone acceptance criteria or synthesis in normal chat
- let the delivery log drift behind the actual tranche state

## Related

- `configs/skills-pack/foxctl-room/SKILL.md`
- `configs/skills-pack/foxctl-room-operator/SKILL.md`
