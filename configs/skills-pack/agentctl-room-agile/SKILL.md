---
name: agentctl-room-agile
description: "Run an agile-style epic/milestone/story workflow inside an existing agentctl room using durable epic intake, milestone proposals, story proposals, review summaries, and delivery logs."
---

# agentctl-room-agile

Use this skill when a room needs a durable agile workflow instead of only free-form chat, tasks, or interviews.

This skill assumes:

- the room already exists
- `agentctl-room` remains the base room protocol
- `agentctl-room-operator` still governs day-to-day participant behavior

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
- the room should externalize long-running work into stable work-pack files under `~/.agentctl/epics`

## Mental model

- `epic` is the long-running objective
- `epic` starts in discovery, not execution
- `epic ask` / `epic answer` / `epic finalize` make the intake interview durable
- `epic shape` turns the finalized brief into durable milestone proposals
- `epic resume` reconstructs the current operational state after a gap
- `epic next` extracts the next concrete actions without rereading the whole room
- `milestone start --proposal` promotes one proposal into a real milestone
- `story propose` / `story accept` do the same one level down
- `milestone review` records the pass/block verdict
- `milestone summary` records the synthesis after review
- `log append` is the cross-session delivery journal for the epic
- `story validate` attaches proof at the story level
- `~/.agentctl/epics/<epic-id>/...` is the rich markdown/artifact mirror; room state stays canonical

## Operating sequence

### 1. Start the epic

```bash
agentctl room epic start <room-id> "Room agile protocol" \
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
agentctl room epic ask <room-id> <epic-id> \
  "What constraints must the first tranche respect?" \
  --kind constraint \
  --to human-a

agentctl room epic ask <room-id> <epic-id> \
  "What must be true before milestones can open?" \
  --kind success \
  --to human-a

agentctl room epic answer <room-id> <question-id> \
  "The first tranche must stay transport-agnostic and room-native."
```

Do not open milestones while the epic is still:

- `discovery`
- `intake_in_progress`
- `ready_to_finalize`

### 3. Finalize the epic brief

```bash
agentctl room epic finalize <room-id> <epic-id> \
  "Clarified brief: ship the room agile layer first, then surface it in the GUI."
```

### 4. Shape milestones from the finalized brief

```bash
agentctl room epic shape <room-id> <epic-id>
agentctl room epic show <room-id> <epic-id>
agentctl room epic resume <room-id> <epic-id>
agentctl room epic next <room-id> <epic-id>
```

This writes `milestone proposal` messages into the room. Review them before execution starts.

When resuming after a pause, prefer:

```bash
agentctl room epic resume <room-id> <epic-id>
agentctl room epic next <room-id> <epic-id> --actor <you>
```

### 5. Promote one milestone proposal into a real milestone

```bash
agentctl room milestone start <room-id> <epic-id> --proposal <proposal-id>
```

Then add explicit acceptance criteria:

```bash
agentctl room milestone criteria <room-id> <milestone-id> \
  "Epic and milestone hierarchy is visible via show commands"
```

### 6. Propose and accept stories under the milestone

```bash
agentctl room story propose <room-id> <milestone-id> \
  "Implement story proposal flow" \
  "Add story propose and accept commands." \
  --owner gemini-a \
  --rationale "Needed before agents can refine milestone internals."

agentctl room story show <room-id>
agentctl room story accept <room-id> <milestone-id> <story-proposal-id>
```

Use `story add` only when a proposal step would add no value.

### 7. Review and synthesize the milestone

```bash
agentctl room milestone review <room-id> <milestone-id> pass \
  "Foundation slice met the milestone criteria"

agentctl room milestone summary <room-id> <milestone-id> \
  "Review synthesis: the milestone passed, accepted stories are clear, and the next tranche can start."
```

`milestone review` is the verdict.

`milestone summary` is the durable synthesis.

### 8. Attach validation to accepted stories

Use story-level validation as the primary proof unit:

```bash
agentctl room story validate <room-id> <story-id> review pass \
  "Code review passed with no blocking findings." \
  --sender human-a \
  --artifact-path docs/reviews/story.md \
  --command "go test ./cmd/agentctl/cmd" \
  --notes "Validated after the second pass."
```

Rules:

- validation belongs to the story, not only to the milestone
- `artifact-digest` requires `artifact-path`
- `waived` requires explicit waiver notes and owner/coordinator authority
- related stories must resolve inside the same milestone for the current slice

### 9. Let the work-pack mirror stay in sync

After epic, milestone, story, review, summary, and validation writes, agentctl materializes:

- `~/.agentctl/epics/<epic-id>/epic.md`
- `~/.agentctl/epics/<epic-id>/meta.json`
- `~/.agentctl/epics/<epic-id>/milestones/<milestone-id>/milestone.md`
- `~/.agentctl/epics/<epic-id>/milestones/<milestone-id>/meta.json`
- `~/.agentctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/story.md`
- `~/.agentctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/meta.json`
- `~/.agentctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/validation/<validation-id>.md`
- `~/.agentctl/epics/<epic-id>/milestones/<milestone-id>/stories/<story-id>/validation/<validation-id>.json`

Use the explicit backend surface when you need to inspect or refresh that mirror:

```bash
agentctl room workpack show <room-id> <epic-id>
agentctl room workpack sync <room-id> <epic-id> --sender human-a
```

### 10. Keep the epic delivery log current

```bash
agentctl room log append <room-id> <epic-id> "Foundation landed" \
  --completed "CLI hierarchy shipped" \
  --in-flight "GUI surfacing" \
  --next "Wire status and planning views"
```

## Default coordinator behavior

- own epic intake and finalization
- use `epic shape` before improvising milestones
- prefer `milestone start --proposal`
- prefer `story propose` then `story accept` for non-trivial work
- treat `story validate` as required closure evidence, not an optional afterthought
- write `milestone summary` after reviews so the next session does not need to reconstruct conclusions
- keep `log append` current at tranche boundaries

## Default participant behavior

- read `epic show`, `milestone show`, and `story show` before starting
- after a long gap, read `epic resume` and `epic next` before reconstructing state manually
- if intake is still open, answer epic questions instead of starting implementation
- if a story is only proposed, wait for acceptance before treating it as committed execution scope
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

- `configs/skills-pack/agentctl-room/SKILL.md`
- `configs/skills-pack/agentctl-room-operator/SKILL.md`
