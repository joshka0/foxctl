# WINDSURF AGENTS — GLOBAL RULES (PRAGMATIC v2.1)

**Goal:** To make the best possible code.

## 1. HARD STOPS (Non-Negotiable)

_Violating these is a failed change._

### Security

- No dynamic code execution (`eval`, `unsafe`, runtime codegen).
- No secrets in code, logs, fixtures, or error messages.
- Validate all untrusted input at boundaries (schema/type/size/path traversal).

### Build & Release Integrity

- Repo must stay releasable: run the project’s standard verification
  (CI-equivalent: build + tests + lint/static checks).
- Never bypass verification (`--no-verify`, disabling checks) without an
  explicit emergency override.

### State & Data Safety

- Every mutating action in code must have an idempotent strategy.
- No partial writes on failure: design for atomicity (or clear resumability).

### Knowledge Integrity

- Docs are the source of truth. If behavior/contracts change, update `docs/` in
  the same change.
- PR descriptions are not documentation.
- **If ever you see a better decision can be made but the rules seem to
  contradict or prevent you from that just ask if it's possible to ignore the
  rule for that change**, best code is the ultimate goal.

## 2. QUALITY GATES (High leverage)

### Testing

- Test **behavior** and invariants, not line coverage.
- Required: new logic, bug fixes, concurrency, parsers, boundary validation.
- Exempt: trivial boilerplate, generated code (only if behavior is unchanged).

### Code Health

- Long-running ops must support cancellation/timeouts (language-idiomatic
  mechanism).
- Errors must be contextual and actionable (not raw “failed”).
- Public APIs must have clear doc comments covering edge cases.
- Prefer small, composable units. Avoid hidden side effects / “magic”. Think
  functional.

### Dependencies

- Prefer stdlib. Add deps only when **strictly necessary**.
- New deps require a brief justification (why needed, risk/maintenance note).

### Debt Policy (Hardline)

- No `TODO`, `FIXME`, or `XXX` comments in code. Fix now or keep in docs todos.

## 3. WORKFLOW (Plan ↔ Execute)

### Phase 1: Planning (Verbose)

- State assumptions, edge cases, and blast radius.
- Identify contract touchpoints (protocols, storage, auth).
- Use TODOs judiciously to document remaining work, either in docs or tools.
  Prefer the former for more verbose tasks with extra context and the latter for
  quick tasks.

### Phase 2: Confirmation (Ask Gate)

Stop and ask ONLY if:

- Changes affect public API/protocols.
- Data models require migration.
- Refactors cross module boundaries.
- A new dependency is introduced.
- A security boundary is touched. Otherwise: proceed.

### Phase 3: Coding (Precise)

- Try to make the code strictly better, less does not equal better nor does more
  complexity make it better. Execute the change with the **best possible code**,
  be ruthless in improving it.

### Phase 4: Self-Review (Critical)

- Re-check contracts, boundary validation, error semantics.
- Verify no accidental behavior changes elsewhere.

## 4. AUTO-REJECT HEURISTICS (Stop if you’re about to…)

1. Bypass verification instead of fixing root cause.
2. Leak secrets (even in debug logs).
3. Hallucinate APIs (check source/docs first).
4. Ship fragile complexity without refactor + tests.
5. Change behavior without updating docs/tests.

## 5. REGRESSION PROTOCOL (“Never Again”)

If you trigger a regression or repeat a bug class:

1. Fix root cause.
2. Add a permanent regression test.
3. Record the failure mode as a “never again” rule in memory.

## 6. REFACTORING RULE (“Boy Scout”)

- If you touch a file, leave it strictly better within scope.
- Allowed: clarity renames, typing, dead-code removal, comments.
- Forbidden: unrelated rewrites / stylistic overhauls without permission.
