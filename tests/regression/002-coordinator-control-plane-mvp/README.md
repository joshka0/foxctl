# Regression: coordinator control-plane MVP

**Issue:** The final coordinator loop depends on several small control-plane contracts that live in separate packages.
**Bug:** Hook proposal capture, contextplane decision/apply state, web API decision evidence, and coordinator command surfaces can drift independently.
**Expected:** A cheap regression wrapper should prove the landed coordinator control-plane behavior without live Pi, browser, tmux, or daemon dependencies.

## Why this exists
- Proposal mode must record reviewable task proposals instead of silently creating tasks.
- Coordinator decisions must keep evidence and harness metadata append-only and inspectable.
- Apply paths must require the current approving decision and stay idempotent.
- CLI coordinator coverage should be included once command-level tests land.

## How to run
```bash
bash run.sh
```

## Notes
- This case intentionally calls narrow Go test selectors only.
- The wrapper skips coordinator-command coverage when no coordinator command test target exists in the checkout.
- It runs on the default non-CGO storage path; do not add live Pi, browser, tmux, or daemon prerequisites.
