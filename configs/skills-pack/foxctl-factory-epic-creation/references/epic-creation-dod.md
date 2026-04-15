# Epic Creation DoD

This Definition of Done is for `epic creation` only.

It is complete when foxctl has a canonical room-agile epic that is ready to be used as the implementation plan.

It is not complete merely because:

- a Factory mission was imported
- some source features were already executed
- validations already exist
- delivery-log history exists

## Category boundary

`Epic creation` and `epic implementation` are different categories.

Epic creation is done when the plan is structurally ready.

Epic implementation starts after that point and is out of scope for this DoD.

## Required outcomes

### 1. Canonical epic shape exists

- one foxctl epic exists for the mission
- the epic brief is coherent in foxctl terms
- milestone and story hierarchy is visible and usable in room-agile
- imported provenance remains traceable back to Factory artifacts

### 2. Stories are execution-shaped

- each story represents one clear unit of implementation or one clear validator step
- oversized imported stories have been split or explicitly queued for split
- milestone grouping is coherent
- dependencies and preconditions are visible enough to execute from the room-agile surface

### 3. Validation model is explicit

- evidence lanes are typed where the source clearly implies them
- validator stories are preserved or reconstructed when the source used them
- story validations carry useful evidence pointers when available
- milestones have enough validation structure to support later review and summary

### 4. Evidence expectations are visible

- verification commands or equivalent evidence expectations are preserved
- artifact paths or provenance notes are attached when source artifacts exist
- source contradictions are surfaced explicitly rather than hidden by optimistic state mapping

### 5. Ready-for-implementation boundary is clear

- planning gaps are separated from implementation tasks
- open questions are recorded if they block plan quality
- the epic can be handed to an implementation agent without asking it to redesign the mission first

## Typical blockers

Mark `NOT READY` if any of these remain:

- stories are still too coarse to execute safely
- validator lanes are missing even though the source mission clearly distinguishes them
- imported state overstates progress or hides source contradictions
- evidence expectations are missing or too vague
- milestone contracts or criteria are too weak to support later review
- important source artifacts exist but are not represented in foxctl validation/provenance

## Typical ready signal

Mark `READY` only when the imported and normalized foxctl epic can reasonably function as the implementation plan without another planning pass.
