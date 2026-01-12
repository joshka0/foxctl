# Lint & Architecture Cleanup Spec

**Status:** Draft

## 1. Context & Motivation

`agentctl` now targets **Go 1.24** with toolchain **go1.24.7** and uses **golangci-lint v2.1.5** via the v2 module path.

- `go.mod`:
  - `go 1.24`
  - `toolchain go1.24.7`
  - `github.com/golangci/golangci-lint/v2 v2.1.5`
- `Makefile`:
  - `GOLANGCI ?= $(GO_CMD_CGO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.5`
- `.golangci.yml` (v2 config):
  - `linters.enable`: `govet`, `staticcheck`, `revive`, `errcheck`, `depguard`.
  - `revive` comment rules (`package-comments`, `exported`) explicitly disabled to respect the current rule of not changing code comments unless explicitly requested.

With this setup, `make lint` runs successfully but reports ~100+ issues across:

- `revive` (unused parameters, naming, builtin redefinitions, context usage, etc.).
- `staticcheck` (QF10xx quickfixes, minor simplifications).
- `errcheck` (unchecked errors).
- `depguard` (layering violations between `internal/domain`, `internal/execution`, `internal/platform`, `internal/adapters`, `internal/storage`, etc.).

The original skill test typecheck issues (`ctx`/`cfg` undefined, unused imports) are already fixed and no longer reported. The remaining lints are larger-scope cleanliness and architecture items that should be handled deliberately rather than ad‑hoc.

This spec describes how to drive `make lint` to green under Go 1.24 + golangci-lint v2 **without weakening meaningful checks**, and how to treat architecture-related findings (especially `depguard`).

## 2. Goals

- **G1 – Clean lint signal:**
  - Achieve `make lint` passing under Go 1.24.x and golangci-lint v2.1.5.
  - Keep meaningful analyzers (`govet`, `staticcheck`, `revive`, `errcheck`, `depguard`) enabled.
- **G2 – Preserve behavior and contracts:**
  - No changes to the JSON envelope wire contract, error codes, or Core Profile semantics.
  - No changes to network policy or WASI/exec runner rules without a separate spec and human approval (per `AGENTS.md`).
- **G3 – Respect layering:**
  - Treat `depguard` violations as signals about architectural issues.
  - Where possible, fix imports and dependencies to match the intended layering rather than disabling rules.
- **G4 – Minimize noisy style changes:**
  - Avoid mass reformatting or stylistic churn beyond what is already enforced by `make fmt` and existing linters.

## 3. Non‑Goals

- Changing envelope fields, error schema, or Core Profile v1 behavior.
- Modifying WASI or exec network policies.
- Introducing new dependencies that require CGO or non‑portable tooling.
- Relaxing `depguard` globally just to get a green `make lint`.

Any changes in these areas require a separate spec and explicit human review.

## 4. Current Lint Landscape (High Level)

This section is descriptive; exact counts will drift, but the main categories are stable.

### 4.1 Revive

Representative issues:

- **Unused parameters** in helpers and tests, e.g.:
  - `internal/platform/errors/errors_test.go`: unused `t` in subtest closures.
  - `internal/storage/jobs/store_unit_test.go`: unused `ctx` parameters in fake implementations.
  - Skills helpers: `in`, `workspace`, `fset` parameters not used in some analysis/extraction functions.
- **Builtin redefinitions**:
  - Local variables or functions named `min` / `max` shadowing built‑in identifiers.
- **Context usage**:
  - `context-as-argument` in some test helpers where `context.Context` is not the first parameter.
- **Naming**:
  - Stuttered exported names like `memory.MemorySearchResult`.
- **Empty blocks**:
  - An empty `if` block in `skills/data_jq/main_test.go` where all logic is in comments.

### 4.2 Staticcheck

Mostly quickfix / style suggestions, for example:

- QF1008/1003/1011 style improvements (embedded selector simplifications, switch refactors, type inference) that do not change behavior.

### 4.3 Errcheck

- A handful of unchecked errors in non‑critical paths, often tests or cleanup paths.

### 4.4 Depguard

Rules currently capture intended layering (e.g., domain not depending on storage/adapters/platform, limited dependencies for execution). There are ~50 violations spread across:

- Domain packages importing storage or adapters.
- Execution packages importing adapters.

These require case‑by‑case design decisions rather than blind import surgery.

## 5. Constraints & Guardrails

- Follow `AGENTS.md` **Do/Ask/Act** rules:
  - **Do**: refactors, lint fixes, layered architecture improvements.
  - **Ask**: any change that affects envelope semantics, network, on‑disk layout, or DB schema.
- No storing secrets in code, testdata, or CAS.
- Keep `make check` (fmt, lint, vet, test, coverage, build) green at each checkpoint.
- Code comments and docs should only be added/changed when explicitly in scope (as in this spec).

## 6. Work Plan

### Phase 0 – Tooling Alignment (**Done, documented for context**)

- Target Go 1.24 in `go.mod` with toolchain `go1.24.7`.
- Run `golangci-lint v2.1.5` via `go run` from the `Makefile`.
- Migrate `.golangci.yml` to v2 format (`version: 2`) and:
  - Remove formatter linters (gofmt, gofumpt) from `linters.enable`.
  - Explicitly disable `revive` comment rules (`package-comments`, `exported`).

### Phase 1 – Local/Mechanical Cleanups

**Objective:** Remove easy, low‑risk issues so that remaining lints mostly reflect architectural questions.

Scope:

- **Unused parameters (revive `unused-parameter`):**
  - Tests and fake/mock implementations: rename unused parameters to `_` or remove them where signature is internal and not part of a public contract.
  - Internal helpers where the extra parameter is truly unused and not reserved for future use.
- **Builtin redefinitions (revive `redefines-builtin-id`):**
  - Rename local `min`/`max` variables and functions to names like `minInt`, `maxInt`, or `minVal`, `maxVal`.
  - Prefer introducing a small, file‑local helper where appropriate.
- **Empty block in `skills/data_jq/main_test.go`:**
  - Replace the empty `if` body with a comment‑only block or restructure the test to avoid the empty block entirely (keeping intent the same: exercising error path when `jq` is missing).
- **Context ordering (`context-as-argument` in tests/helpers):**
  - For internal helpers and fakes, reorder parameters so `context.Context` is first, provided this doesn’t change any public API.
- **Staticcheck QF10xx suggestions:**
  - Apply safe quickfixes that:
    - Simplify selectors for embedded fields.
    - Simplify type declarations where inference is obvious.
    - Improve switch style where it’s purely structural.

Process:

- Work in a dedicated branch (e.g., `codex/lint-phase1`), with small, logically grouped commits, each titled `checkpoint(lint): <message>`.
- After each logical group:
  - `make fmt`
  - `make test`
  - `make lint`

Exit criteria for Phase 1:

- No remaining `revive` unused‑parameter or builtin‑redefinition issues.
- No trivial staticcheck QF10xx items left.
- No simple `errcheck` omissions in tests/helpers.

### Phase 2 – Depguard & Layering

**Objective:** Resolve or intentionally document each depguard violation.

Tasks:

1. **Inventory depguard violations:**
   - Capture the current output of `golangci-lint run` filtered to depguard.
   - Group by rule (`domain-no-deps`, `execution-limited-deps`, etc.) and by package pair (e.g., `internal/domain -> internal/storage`).

2. **Classification per violation:**
   - **Type A – Legitimate architecture violation:**
     - Example: domain reaching directly into storage implementation.
     - Plan: introduce interfaces or service abstractions; move logic so dependencies flow from outer layers inward.
   - **Type B – Rule too strict or mis‑scoped:**
     - Example: a thin helper import that doesn’t actually break intended layering.
     - Plan: refine depguard patterns (e.g., narrow `files:` globs) rather than disable rules broadly.

3. **Design changes:**
   - For each Type A violation, propose:
     - New interface or service boundary.
     - Where the implementation should live (e.g., adapters or storage).
     - Migration steps (move code, update imports, tests).
   - For Type B, propose a depguard config adjustment with rationale.

4. **Implementation:**
   - Use one or more feature branches (e.g., `codex/depguard-cleanup-1`, `codex/depguard-cleanup-2`).
   - Maintain green `make check` at each checkpoint.

Exit criteria for Phase 2:

- No depguard violations under the agreed rules.
- Updated `depguard` config still reflects the intended architecture from `AGENTS.md`.

### Phase 3 – Final Polishing & CI Enforcement

- Re‑run `make check` (fmt, lint, vet, tests, coverage, build) on main branch after merges.
- Ensure CI uses the same Go and golangci-lint versions (Go 1.24.x, golangci-lint v2.1.5 or newer compatible release).
- Document any intentionally disabled rules (beyond comment rules) in this spec and/or `.golangci.yml` comments for future maintainers.

## 7. Acceptance Criteria

`make check` passes on a clean checkout with the agreed toolchain, specifically:

- `make fmt` produces no diffs on re‑run.
- `make test` + `make test-race` pass.
- `make lint` passes with:
  - `govet`, `staticcheck`, `revive`, `errcheck`, and `depguard` enabled.
  - Comment‑related revive rules (`package-comments`, `exported`) disabled as per this spec.
- No depguard violations under the finalized rules.
- No changes to envelope wire contracts, network policy, or on‑disk layouts, unless separately specified and approved.

## 8. Open Questions / To‑Decide

- For stuttered exported names (e.g., `MemorySearchResult` in `internal/storage/memory`):
  - Do we want to rename types now (with corresponding test updates), or leave them and relax the specific revive rule?
- Are there any depguard rules that should be split by subdirectory (e.g., allowing certain utility imports) rather than applying to an entire layer?
- Should we introduce a dedicated `internal/lint` or `internal/shared` package for utilities that currently cause depguard noise?

These should be answered during implementation of Phase 2 and captured either as updates to this spec or as comments in `.golangci.yml`.
