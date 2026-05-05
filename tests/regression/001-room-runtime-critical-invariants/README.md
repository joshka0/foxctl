# Regression: room runtime critical invariants

**Issue:** Room runtime hardening left several critical behaviors spread across multiple packages and easy to regress independently.
**Bug:** Delivery ownership, fallback eligibility, loop-memory durability, scoped SSE, room authorization, and observability could drift without one stable proof bundle.
**Expected:** A single regression case should prove the hardened room-runtime invariants still hold across storage, loop runtime, API surfaces, SSE, and relay merge behavior.

## Why this exists
- The room system now depends on singular live-delivery ownership and durable loop state; regressions here create duplicate relays or stale runtime truth.
- Authorization and reminder semantics were recently tightened; a later refactor could silently reopen those mutation or scheduler bugs.
- Operators need the status surface and last-delivery trace to remain trustworthy as delivery logic evolves.

## How to run
```bash
bash run.sh
```

## Notes
- This case uses the repo's native Go test framework with a narrow selector across the touched packages.
- It runs on the canonical non-CGO storage path. Turso-backed SQLite-family
  storage should be tested through the default build, not a libsqlite3 tag.
