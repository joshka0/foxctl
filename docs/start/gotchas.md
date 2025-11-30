# Gotchas & Rules (agentctl)

This document works together with `AGENTS.md`'s **Gotchas Graveyard** section.

- `AGENTS.md` records **Gotchas** (`G1`, `G2`, …): real incidents and scars.
- This file records the corresponding **Rules** (`R1`, `R2`, …): how we
  permanently prevent each gotcha from recurring.

Each rule entry should be added in the **same PR** that adds/updates the
corresponding Gotcha row.

---

## Rules Index

| Rule ID | Gotcha ID | Short rule                                                     | Detection / enforcement hint                                      | Notes |
|--------|-----------|-----------------------------------------------------------------|-------------------------------------------------------------------|-------|
| R1     | G1        | Never treat Go/CGO toolchain crashes as "tests green"         | Look for `runtime/cgo: .* cgo: exit status 2` in local test runs. | If local `make test-race` fails due to toolchain/CGO, either fix the env or clearly document the exception and rely on CI's containerized Go image for race validation. |

---

## How to add a new rule

1. Add a new Gotcha row (`G*`) to **AGENTS.md** under *Gotchas Graveyard*.
2. In the same PR, add a matching rule row (`R*`) here that:
   - References the Gotcha ID.
   - States the rule in one short sentence.
   - Describes how tools/agents/humans can detect violations.
3. When practical, add or update a regression test that would have caught the
   incident.

This file is the canonical, growing list of **"never again" rules** for the
agentctl repo.
