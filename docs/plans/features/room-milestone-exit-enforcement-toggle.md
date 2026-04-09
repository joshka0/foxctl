# Room Milestone Exit Enforcement Toggle

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Make milestone exit enforcement explicitly reversible so coordinators can turn `enforce_exit_policy` on or off without rewriting the milestone contract out of band |
| Related | [room-milestone-exit-enforcement.md](./room-milestone-exit-enforcement.md), [room-milestone-contract.md](./room-milestone-contract.md), [room-milestone-exit-policy.md](./room-milestone-exit-policy.md) |

## Why this slice

The current `enforce_exit_policy` implementation is intentionally narrow and works, but it has one operational gap:

- milestone contract merges can only turn enforcement on
- there is no explicit contract path to turn it back off

That means the room has an asymmetric control:

- enable is durable and supported
- disable requires manual workaround or future code changes

For a coordinator-controlled policy flag, that is unnecessarily rigid.

## Goals

1. Make `enforce_exit_policy` explicitly toggleable.
2. Keep the change scoped to contract/start semantics only.
3. Preserve default-off behavior.
4. Keep enforcement logic itself unchanged.
5. Avoid ambiguous “unset” behavior on updates.

## Non-goals

1. Changing the exit-policy status ladder.
2. Expanding enforcement beyond pass-review writes.
3. Introducing another board-message kind.
4. Making enforcement inherit automatically from epic-level policy.
5. Adding general-purpose field-clearing semantics for every milestone contract field.

## Proposed model

Use an explicit tri-state contract input model:

- milestone start:
  - `--enforce-exit-policy`
  - `--no-enforce-exit-policy`
- milestone contract:
  - `--enforce-exit-policy`
  - `--no-enforce-exit-policy`

Rules:

- start:
  - absent flags => default `false`
  - `--enforce-exit-policy` => `true`
  - `--no-enforce-exit-policy` => `false`
- contract update:
  - absent flags => no change
  - `--enforce-exit-policy` => set `true`
  - `--no-enforce-exit-policy` => set `false`
  - both flags together => error

## First implementation slice

### 1. CLI flag model

Add explicit paired flags for milestone start and contract.

### 2. Body/meta encoding

Preserve the same stored field:

- `EnforceExitPolicy: true|false`

But only write it on updates when the caller explicitly set one of the flags.

### 3. Merge semantics

Contract merge should:

- set `true` when explicitly enabled
- set `false` when explicitly disabled
- leave unchanged when omitted

### 4. MCP surface

Expose:

- `enforce_exit_policy`
- `disable_exit_policy` or `no_enforce_exit_policy`

with one explicit conflict rule

## Behavior boundaries

### What changes now

- coordinators can explicitly disable enforcement again
- contract update semantics become symmetric for this flag

### What does not change yet

- enforcement target stays pass-review only
- no epic-level inheritance
- no broad contract field reset semantics

## Risks

1. Ambiguous omission vs explicit false
   - use separate enable/disable flags
2. MCP/CLI mismatch
   - document one conflict rule and mirror it in both surfaces
3. Silent flips on start
   - keep start default explicit as false when flags are omitted

## Definition of done

1. milestone start and contract expose symmetric enable/disable flags
2. merge semantics can turn enforcement off again
3. MCP mirrors the same toggle semantics
4. focused tests cover:
   - enable on start
   - disable on contract update
   - omit means unchanged
   - conflicting flags error
5. review confirms the slice stays narrow and does not generalize into full contract field clearing
