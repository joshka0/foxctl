---
name: go-backend
description: Go backend implementation worker for foxctl features — writes Go code, tests, and CLI commands following the project's functional core / imperative shell architecture.
---

# Go Backend Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

All features that involve writing Go code: new packages, CLI commands, tests, integration code. This covers the worktree manager, gateway, terminal bridge, room integration, and OpenSandbox adapter features.

## Required Skills

None — this worker uses native Go tooling directly.

## Work Procedure

### 1. Read Context (MANDATORY FIRST STEP)

Before writing any code, read these files in order:

1. **Mission context:** `{missionDir}/AGENTS.md` — boundaries, architecture, conventions
2. **Architecture:** `.factory/library/architecture.md` — how the system works
3. **Feature description:** Your assigned feature from `{missionDir}/features.json`
4. **Related code:** Read the files mentioned in AGENTS.md "Existing Code to Reference" section that are relevant to your feature
5. **Project conventions:** `AGENTS.md` at repo root (coding guidelines, testing requirements, envelope format)

### 2. Write Tests First (TDD — RED)

For every piece of functionality:

1. Write table-driven tests in `*_test.go` files in the same package
2. Tests must FAIL before implementation begins
3. Cover: happy path, error cases, edge cases, boundary conditions
4. Use `testify/assert` and `testify/require` (project convention)
5. Use `t.Setenv()` for environment isolation
6. Golden files in `testdata/` for deterministic output verification
7. For tests needing git repos: create temp git repos via `exec.Command("git", "init", tmpDir)`
8. For tests needing tmux: verify tmux is available, skip if not (`t.SkipIf`)

Run tests to confirm they fail:
```bash
go test ./internal/<package>/ -run TestXxx -v
```

### 3. Implement (GREEN)

1. Write the minimum implementation to make tests pass
2. Follow the plan/apply pattern: pure functions for business logic, thin shell for IO
3. Domain types in `internal/platform/worktree/` or `internal/gateway/` — no IO imports in core
4. Use `context.Context` through all calls
5. Return structured errors — never leak raw git/tmux stderr
6. All CLI output as JSON envelopes (version, status, command, data, meta, error)
7. `meta.ts` must be present in every envelope (RFC3339 UTC)
8. No `time.Now()` in core logic — inject clock if needed

### 4. Verify

After implementation passes tests:

1. `go test ./internal/<package>/ -v` — all tests pass
2. `go test -race ./internal/<package>/...` — no race conditions
3. `go vet ./internal/<package>/...` — no vet issues
4. `make lint` (or `golangci-lint run --timeout 10m ./internal/<package>/...`)
5. `gofumpt -l ./internal/<package>/` — no formatting issues

### 5. Manual Verification

For features that involve CLI commands:

1. Build: `make build` or `go build ./cmd/foxctl/...`
2. Run the new command manually and verify output is valid JSON envelope
3. For gateway features: test with `--dev` mode on localhost
4. For worktree features: verify with real git operations
5. Check that existing tests still pass: `go test ./... -count=1` (no regressions)

### 6. Commit

1. `git add` only files related to your feature
2. Commit message: descriptive, referencing the feature ID
3. Do NOT commit changes to files outside your feature's scope

## Example Handoff

```json
{
  "salientSummary": "Implemented worktree Create, List, Remove, and Prune in internal/platform/worktree/ with full TDD. 28 tests passing, no races. go vet and lint clean.",
  "whatWasImplemented": "internal/platform/worktree/manager.go (Manager struct with Create/List/Remove/Prune), internal/platform/worktree/sanitize.go (SanitizeBranchName), internal/platform/worktree/porcelain.go (porcelain parser), internal/platform/worktree/manager_test.go (28 test cases), internal/platform/worktree/sanitize_test.go (12 test cases), internal/platform/worktree/porcelain_test.go (8 test cases including golden files)",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {"command": "go test ./internal/platform/worktree/ -v", "exitCode": 0, "observation": "28 tests passing"},
      {"command": "go test -race ./internal/platform/worktree/...", "exitCode": 0, "observation": "No races detected"},
      {"command": "go vet ./internal/platform/worktree/...", "exitCode": 0, "observation": "Clean"},
      {"command": "golangci-lint run --timeout 10m ./internal/platform/worktree/...", "exitCode": 0, "observation": "No issues"}
    ],
    "interactiveChecks": []
  },
  "tests": {
    "added": [
      {"file": "internal/platform/worktree/manager_test.go", "cases": [
        {"name": "TestCreate_NewBranch", "verifies": "VAL-WT-001"},
        {"name": "TestCreate_ExistingBranch", "verifies": "VAL-WT-002"},
        {"name": "TestCreate_FromRef", "verifies": "VAL-WT-003"},
        {"name": "TestList_AllWorktrees", "verifies": "VAL-WT-012"},
        {"name": "TestRemove_CleanWorktree", "verifies": "VAL-WT-015"},
        {"name": "TestRemove_DirtyWithoutForce", "verifies": "VAL-WT-016"}
      ]},
      {"file": "internal/platform/worktree/sanitize_test.go", "cases": [
        {"name": "TestSanitizeBranchName_UnsafeChars", "verifies": "VAL-WT-004"},
        {"name": "TestSanitizeBranchName_EmptyResult", "verifies": "VAL-WT-005"},
        {"name": "TestSanitizeBranchName_SafePreserved", "verifies": "VAL-WT-006"}
      ]}
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- Feature depends on an API endpoint or data model that doesn't exist yet
- Requirements are ambiguous or contradictory
- Existing bugs in unrelated code affect this feature
- A dependency (tsnet, pty) has unexpected behavior that blocks progress
- The feature scope is significantly larger than described
- Need to modify files listed as "off-limits" in AGENTS.md
