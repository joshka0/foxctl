# Documentation Lifecycle Policy

This policy defines how docs should be organized, maintained, and retired.

## Lifecycle Classes

| Class | Location | Meaning | Maintenance expectation |
|------|----------|---------|-------------------------|
| Current | `docs/general/`, `docs/architecture/`, `docs/spec/` | Represents current behavior/contracts | Must be kept accurate with code changes |
| Active Plan | `docs/plans/` | Forward-looking implementation work | Update as plans evolve; archive when complete/superseded |
| Legacy Plan | `docs/impl_plan/` | Historical phased plans | Keep for provenance; avoid using as canonical guidance |
| Historical | `docs/archive/` | Superseded/legacy material | Read-only except link hygiene and provenance notes |
| Generated | `docs/codemaps/` | Generated analysis artifacts | Regenerate when needed; not canonical |
| Working Notes | `docs/notes/`, `docs/research/`, `docs/designs/` | Exploratory/non-final material | Promote stable decisions into current docs |

## Canonical Sources

- Docs navigation index: [`docs/README.md`](README.md)
- Contributor/assistant operating rules: [`AGENTS.md`](../AGENTS.md)
- Foundational stable contracts: [`docs/spec/v1/`](spec/v1/)

## Rules

1. New canonical behavior docs go in `docs/general/`, `docs/architecture/`, or `docs/spec/`.
2. New planning docs go in `docs/plans/` (not `docs/impl_plan/`).
3. Completed or superseded plans/designs move to `docs/archive/`.
4. Historical docs may keep legacy content, but links must remain navigable.
5. When behavior changes, update the matching canonical doc in the same PR.
6. Markdown links in repo docs must pass `make check-doc-links`.

## Review Cadence

- Current docs: review on every behavior-impacting PR.
- Active plans: review when milestones/status changes.
- Archive docs: no content refresh requirement; link hygiene only.
