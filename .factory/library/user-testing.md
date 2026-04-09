# User Testing — Room Sandbox Mission

Testing surface, required tools, and resource cost classification for validation.

**What belongs here:** Validation surface discoveries, testing tool requirements, resource constraints. Updated by user-testing validators with runtime findings.

---

## Validation Surface

### Surface 1: CLI (agentctl commands)
- **Tools:** `tuistory`, shell assertions
- **Entry points:** `agentctl room create --sandbox`, `agentctl room list`, `agentctl room show`, `agentctl room destroy`, `agentctl gateway`
- **Setup:** Build agentctl binary, ensure git/tmux/zellij on PATH

### Surface 2: Web Terminal (xterm.js)
- **Tools:** `agent-browser`
- **Entry points:** `http://localhost:8765/terminal/{room-id}` (dev mode) or `https://<hostname>/terminal/{room-id}` (Tailscale)
- **Setup:** Start gateway in --dev mode, create sandbox room, open browser

### Surface 3: SSH Terminal
- **Tools:** Shell-based SSH commands
- **Entry points:** `ssh room-<id>@<hostname>` (Tailscale) or local SSH to gateway
- **Setup:** Gateway running, sandbox room with tmux session

### Surface 4: Gateway API
- **Tools:** `curl`
- **Entry points:** `GET /healthz`, `GET /terminal/{room-id}`, WebSocket upgrade
- **Setup:** Gateway running in any mode

## Validation Concurrency

**Machine:** 64 GB RAM, 16 cores (Apple Silicon)

| Surface | Per-instance Cost | Max Concurrent | Rationale |
|---------|------------------|----------------|-----------|
| CLI (tuistory) | ~50 MB | 5 | Lightweight, process spawn only |
| Web (agent-browser) | ~300 MB | 5 | Browser instance per validator |
| SSH | ~10 MB | 5 | Just SSH processes |
| Gateway API (curl) | ~5 MB | 5 | HTTP requests only |

**Gateway overhead:** ~100 MB (single process, shared across all validators)

**Total worst case:** 5 × 300 MB + 100 MB = 1.6 GB — well within 64 GB headroom.

## Notes

- Gateway --dev mode (localhost:8765) should be used for CI/local validation
- Tailscale integration (tsnet) requires TS_AUTHKEY for full validation
- For validation without Tailscale: use --dev mode for web terminal tests
- SSH validation in --dev mode: SSH server listens on localhost in dev mode
- tmux must be available for all terminal tests; skip gracefully if missing

## Flow Validator Guidance: go test (internal/worktree)

**Surface:** Go unit tests in `internal/worktree/` package
**Tool:** `go test ./internal/worktree/... -v -count=1`
**Isolation:** Each test function creates its own temp git repo via `t.TempDir()`. No shared state between tests.
**Concurrency:** Safe to run multiple test subets concurrently — they operate on independent temp directories.
**Assertions covered:** VAL-WT-001 through VAL-WT-040
**Test-to-assertion mapping:**
- VAL-WT-001 → TestCreate_NewBranch
- VAL-WT-002 → TestCreate_ExistingBranch
- VAL-WT-003 → TestCreate_FromRef
- VAL-WT-004 → TestSanitizeBranchName_UnsafeChars (spaces_and_tilde_replaced, consecutive_unsafe_chars_collapse)
- VAL-WT-005 → TestSanitizeBranchName_EmptyResult (only_dots, only_unsafe_chars, empty_string)
- VAL-WT-006 → TestSanitizeBranchName_SafePreserved (typical_feature_branch, etc.)
- VAL-WT-007 → TestCreate_CustomBaseDir
- VAL-WT-008 → TestCreate_DefaultBaseDir
- VAL-WT-009 → TestCreate_NonRepoDirectory
- VAL-WT-010 → TestCreate_DuplicateBranch
- VAL-WT-011 → TestCreate_ContextCancellation
- VAL-WT-012 → TestList_AllWorktrees
- VAL-WT-013 → TestParsePorcelain_* (normal, bare, detached, locked, prunable, empty, mixed, trailing)
- VAL-WT-014 → TestStatus_OK, TestStatus_Locked, TestStatus_Prunable
- VAL-WT-015 → TestRemove_CleanWorktree
- VAL-WT-016 → TestRemove_DirtyWithoutForce
- VAL-WT-017 → TestRemove_DirtyWithForce
- VAL-WT-018 → TestRemove_WithDeleteBranch
- VAL-WT-019 → TestRemove_PreservesBranchByDefault
- VAL-WT-020 → TestRemove_OrphanedAdminData
- VAL-WT-021 → TestResolve_ByBranchName
- VAL-WT-022 → TestResolve_ByPartialBranchName
- VAL-WT-023 → TestResolve_SpecialOneReturnsMain
- VAL-WT-024 → TestResolve_AmbiguousPartialReturnsError
- VAL-WT-025 → TestCopyFiles_IncludePatterns
- VAL-WT-026 → TestCopyFiles_ExcludePatterns
- VAL-WT-027 → TestCopyFiles_CombinedIncludeExclude
- VAL-WT-028 → TestCopyFiles_Dotfiles
- VAL-WT-029 → TestCopyFiles_PreservesNestedDirectoryStructure
- VAL-WT-030 → TestHooks_PostCreateEnvVars
- VAL-WT-031 → TestHooks_PostCreateFailureDoesNotRollback
- VAL-WT-032 → TestHooks_PostRemoveExecutesAfterRemoval
- VAL-WT-033 → TestHooks_TimeoutEnforcement
- VAL-WT-034 → TestStatus_OK
- VAL-WT-035 → TestStatus_Locked
- VAL-WT-036 → TestStatus_Prunable
- VAL-WT-037 → TestPrune_RemovesStaleEntries
- VAL-WT-038 → TestCreate_ConcurrentDifferentBranches (with -race flag)
- VAL-WT-039 → TestRemove_RejectsMainCheckout
- VAL-WT-040 → TestCreate_PathValidation
**Max concurrent validators:** 3 (each runs go test subset, ~50 MB per process)
**Constraints:** Do NOT modify any test files. Only run tests and report results.
