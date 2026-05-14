# Room Work-Pack Templates

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Make room-agile work-pack generation explicitly template-backed so epic, milestone, summary, story, validation, delivery-log, and retro artifacts have stable sections and are easier for agents to update and ACA to promote |
| Related | [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-agile-provenance-backlinks.md](./room-agile-provenance-backlinks.md), [room-agile-aca-promotion.md](./room-agile-aca-promotion.md), [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

The room-agile system already materializes a useful mirror under `~/.foxctl/epics/...`, but the current markdown files are still effectively produced by hand-written render functions.

That is workable, but it leaves a few gaps:

- there is no explicit template contract for each artifact class
- section ordering can drift as fields are added over time
- agents do not have a stable expectation for where to look or write within a work-pack file
- ACA promotion is richer when source notes have predictable headings and compact empty-state behavior

Factory-style mission packages were strong partly because each artifact class had a recognizable, repeatable shape.

## Goals

1. Make each work-pack artifact class have a stable template contract.
2. Keep room state canonical and keep work-pack markdown as a projection, not the source of truth.
3. Preserve current work-pack paths and provenance blocks.
4. Give agents predictable section layouts for epic, milestone, summary, story, validation, delivery-log, and retro files.
5. Keep the first slice additive and low-risk: template-backed rendering, not user-edit merge.

## Non-goals

1. Treating markdown edits as canonical protocol updates.
2. Adding a general-purpose templating engine or user-editable template files in v1.
3. Changing work-pack paths or provenance semantics.
4. Reworking milestone review/synthesis semantics.
5. Introducing partial-file patch sync back into room state.

## Proposed model

Treat the renderer layer as a set of explicit artifact templates.

### Artifact classes

The first slice should make these classes explicit:

- `epic.md`
- `delivery-log.md`
- `retro.md`
- `milestone.md`
- `criteria.md`
- `summary.md`
- `story.md`
- `validation/<validation-id>.md`

Each class should have:

- fixed title format
- fixed top summary block
- fixed section ordering
- stable empty-state behavior
- provenance appended after the body (preserving the current backlink approach)

## Template contract

### Epic

Expected sections:

1. title + compact stats
2. goal / owner / status
3. final brief
4. milestones

### Delivery log

Expected sections:

1. title
2. one subsection per log entry
3. stable ordering from newest to oldest
4. explicit empty-state message when there are no entries

### Retro

Expected sections:

1. title
2. empty-state message when no updates exist
3. one subsection per guidance update with:
   - id
   - kind
   - milestone reference when present
   - impact
   - recommended change
   - scope
   - follow-up

### Milestone

Expected sections:

1. title + compact stats
2. goal / objective / owner / status
3. contract sections:
   - scope
   - risks
   - exclusions
   - dependencies
   - validators expected
   - required evidence lanes
   - optional evidence lanes
   - exit criteria
4. required lane status, when configured

### Criteria

Expected sections:

1. title
2. bullet list of criteria
3. explicit empty-state message if none exist

### Summary

Expected sections:

1. title + compact stats
2. required lane status when configured
3. summary body
4. passed criteria
5. failed criteria
6. waived validations
7. blocking validations
8. notable decisions
9. systemic findings
10. recommended next
11. guidance updates
12. evidence-lane rollup

### Story

Expected sections:

1. title + compact stats
2. owner / status / state
3. description
4. state reason / blocker / reviewer when present
5. evidence lanes
6. state history
7. validation history summary

### Validation

Expected sections:

1. title + validator type / status
2. summary
3. command / artifact refs
4. related stories
5. waiver reason when present
6. notes when present

## First implementation slice

### 1. Centralize render shape

Refactor the current markdown builders into explicit template functions with a shared pattern:

- compact top matter
- append sections only when relevant
- render a stable empty-state message when the artifact class has no entries

This can still be plain Go code in v1; “template-backed” here means explicit artifact templates, not user-configurable text templates.

### 2. Standardize empty states

Add explicit empty-state text for:

- delivery log with no entries
- criteria with no criteria
- summary with no summary yet
- retro with no guidance updates
- stories or validations when their optional sections are absent

### 3. Stabilize ordering

Document and enforce ordering for repeated sections:

- logs
- guidance updates
- validations
- state history
- milestones listed in epic notes

### 4. Keep provenance unchanged

Preserve the current provenance append behavior and `meta.json` contract.

## Behavior boundaries

### What changes now

- work-pack markdown shape becomes more intentionally structured
- empty-state output becomes explicit and consistent
- agents get a clearer contract for where information will appear

### What does not change yet

- no markdown-to-room sync
- no custom template registry
- no runtime user overrides for template text
- no path changes

## Risks

1. Over-engineering a templating layer too early
   - keep v1 as explicit render contracts in Go
2. Changing too many artifacts at once without focused tests
   - cover each class touched by this slice
3. Breaking ACA promotion assumptions about section names
   - preserve current provenance and major headings where possible

## Definition of done

This slice is done when:

1. work-pack artifact classes have explicit template contracts
2. renderers use stable section ordering and explicit empty-state behavior
3. provenance append behavior remains unchanged
4. focused tests cover at least milestone/story/summary rendering and one empty-state artifact
5. Cursor review confirms the slice stays additive and does not imply markdown-as-source-of-truth
