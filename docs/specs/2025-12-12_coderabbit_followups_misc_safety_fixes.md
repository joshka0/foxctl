---
description: CodeRabbit Follow-ups – Misc Safety & Guidelines Fixes
status: Approved
owner: jkatigbak
---

# CodeRabbit Follow-ups – Misc Safety & Guidelines Fixes

## Goal

Address a small set of CodeRabbit review items focused on correctness and
safety:

- Make the `skills/code_swe_grep/testdata/config.go` fixture follow our
  guidelines:
  - Context propagation (`Load(ctx)` and `Validate(ctx)`).
  - No ignored parse errors (PORT parsing).
  - Error codes + actionable hints (`EARG`, etc.).
- Make diff application stricter so malformed diff lines fail fast:
  - Reject unknown hunk line prefixes in `internal/agent/tools/edit_tools.go`.
- Fix test assertions to use wrapped-error compatible checks:
  - Replace direct equality with `errors.Is` for `context.Canceled` and sentinel
    errors.

## Non-goals

- No envelope or `meta.*` contract changes.
- No functional change to the `code/swe_grep` skill behavior (this is testdata +
  tests only).
- No changes to CLI behavior.

## Proposed Changes

### 1) `skills/code_swe_grep/testdata/config.go`

- Change:
  - `Load() *Config`
  - to `Load(ctx context.Context) (*Config, error)`
- Early cancellation:
  - If `ctx.Done()` is closed, return `ctx.Err()`.
- PORT parsing:
  - If `PORT` is set and `strconv.Atoi` fails, return an error using code `EARG`
    and an actionable hint.
- Validation:
  - Change `Validate() error` to `Validate(ctx context.Context) error`.
  - Keep logic the same (port range check), but accept `ctx` for guideline
    compliance.
- Error type:
  - Update `ConfigError` to include `Code` and `Hint` fields.
  - Update `ErrInvalidPort` to use `Code: "EARG"` and set `Hint`.

### 2) `internal/agent/tools/edit_tools.go`

- In the hunk line parse loop, treat any line prefix other than:
  - `' '` (context)
  - `'-'` (remove)
  - `'+'` (add) as an error.
- Error message must include:
  - The unexpected prefix.
  - The hunk line index (1-based).

### 3) `skills/code_swe_grep/main_test.go`

- Replace `err != context.Canceled` with `errors.Is(err, context.Canceled)`.
- Add the standard `errors` import.

## Design Diagram

```mermaid
graph TD
  A[Test code / fixtures] --> B[config.Load(ctx)]
  B -->|invalid PORT| C[EARG ConfigError + Hint]

  D[edit_tools apply hunk] --> E[parse hunk lines]
  E -->|unknown prefix| F[return error]
```

## Rollout Plan

| Step | Action                               | Validation                   |
| ---- | ------------------------------------ | ---------------------------- |
| 1    | Update config testdata + any callers | `go test`                    |
| 2    | Update edit_tools hunk parsing       | unit tests + `go test ./...` |
| 3    | Update cancellation test assertion   | `go test`                    |
| 4    | Run `make lint`                      | zero lint issues             |

## Rollback Plan

- Revert the commit(s).
- No persistent data migrations are involved.

## Test Plan

- `CGO_ENABLED=0 go test ./...`
- `make lint`

## Approval

To proceed with implementation, change `status:` above to `Approved`.
