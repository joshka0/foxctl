## Spec
Link: docs/spec/<YYYY-MM-DD_name>.md (status: **Approved**)

## Phase (if part of implementation plan)
→ Phase X of Y — <short description>

## Gold Standard Checklist (all must be checked)
- [ ] Zero lint/type warnings
- [ ] 100 % test coverage on new lines
- [ ] All inputs validated + errors wrapped
- [ ] Cancellation + timeouts handled
- [ ] Idempotent or justified
- [ ] Golden tests updated
- [ ] Left every file better

## Completeness Matrix
| Tests | Golden | Docs | --dry-run | Idempotent | Cancellation | Observability |
|-------|--------|------|-----------|------------|--------------|---------------|
|   Yes   |   Yes    | Yes  |     Yes     |     Yes      |      Yes       |      Yes        |

## Rollback plan
One-line instant revert (e.g. "re-deploy previous image", "delete feature flag and redeploy")

## Root cause (only for bug fixes)
> [Explain the real root cause here — not the symptom]