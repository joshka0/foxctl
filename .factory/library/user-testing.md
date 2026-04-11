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

## Flow Validator Guidance: go test (cmd/agentctl/cmd - room-sandbox)

**Surface:** Go unit tests in `cmd/agentctl/cmd/` package for room sandbox features
**Tool:** `env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 go test ./cmd/agentctl/cmd/ -run "<pattern>" -v -count=1`
**Isolation:** Each test function creates its own temp directory and git repo via `t.TempDir()`. No shared state between tests. Tests use mock/fake implementations for gateway and worktree where needed.
**Concurrency:** Safe to run multiple test subsets concurrently — they operate on independent temp directories.
**Assertions covered:** VAL-RS-001 through VAL-RS-021
**Max concurrent validators:** 3 (each runs go test subset, ~50 MB per process)
**Constraints:**
- Do NOT modify any test files. Only run tests and report results.
- Some tests require `tmux` — those tests skip gracefully if missing.
- Must use CGO_ENABLED=0 to avoid SQLite symbol duplication.
- Tests run in ~3 seconds total for the full suite.

### Test-to-Assertion Mapping (room-sandbox)

**Sandbox Provisioning (room-sandbox-create):**
- VAL-RS-001 → TestProvisionSandbox_CreatesWorktreeAndTmuxSession
- VAL-RS-002 → TestProvisionSandbox_CustomWorktreeRoot
- VAL-RS-003 → TestProvisionSandbox_BaseRef
- VAL-RS-004 → TestProvisionSandbox_RollbackOnTmuxFailure
- VAL-RS-009 → TestProvisionSandbox_IdempotentOnExistingSandbox
- VAL-RS-010 → TestProvisionSandbox_UpgradesNonSandboxRoom
- VAL-RS-011 → TestBuildRoomSandboxInfo_SandboxRoom, TestRoomCreateWithSandboxFlag_Integration

**Sandbox Lifecycle (room-sandbox-lifecycle):**
- VAL-RS-005 → TestRoomListSandbox_IncludesSandboxStatus
- VAL-RS-006 → TestRoomShowSandbox_IncludesSandboxMetadata
- VAL-RS-007 → TestRoomDestroySandbox_CleansUpResources
- VAL-RS-008 → TestRoomDestroySandbox_NonSandboxRoomIsNoop
- VAL-RS-020 → TestRoomDestroySandbox_PartialCleanupOnMissingWorktree (active agent check)
- VAL-RS-021 → TestRoomJoinSandbox_TerminalBinding, TestRoomLeaveSandbox_TerminalBinding

**Sandbox Integration (room-sandbox-integration):**
- VAL-RS-012 → TestRoomSandboxAgent_SpawnUsesWorktreeCWD
- VAL-RS-013 → TestRoomSandboxTasks_ScopePathResolvedToWorktree, TestRoomSandboxTasks_TaskAddUsesSandboxScopePath
- VAL-RS-014 → TestRoomSandboxRelay_DeliversToSandboxSession
- VAL-RS-015 → TestRoomSandboxLoop_RelayIncludesSandboxDelivery
- VAL-RS-016 → TestRoomSandboxStatus_IncludesSandboxInfo
- VAL-RS-017 → TestRoomSandboxInbox_IncludesSandboxInfo
- VAL-RS-018 → TestRoomSandboxRedgreen_InitOnSandboxRoom
- VAL-RS-019 → TestRoomSandboxAgile_EpicStartOnSandboxRoom, TestRoomSandboxAgile_MilestoneStartOnSandboxRoom, TestRoomSandboxAgile_StoryAddOnSandboxRoom

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

## Flow Validator Guidance: go test (internal/gateway)

**Surface:** Go unit tests in `internal/gateway/` package and sub-packages
**Tool:** `go test ./internal/gateway/... -v -count=1`
**Isolation:** Each test function creates its own server instance on a random port via `findFreePort()`. No shared state between tests. tmux sessions are created with unique timestamps and cleaned up in deferred cleanup.
**Concurrency:** Safe to run multiple test subsets concurrently — they operate on independent ports and tmux sessions.
**Assertions covered:** VAL-GW-001 through VAL-GW-030
**Max concurrent validators:** 3 (each runs go test subset, ~50 MB per process)
**Constraints:**
- Do NOT modify any test files. Only run tests and report results.
- Some tests require `tmux` to be available — those tests skip gracefully if missing.
- Tests with `os.Getenv("CI") != ""` skip in CI environments.
- SSH tests start their own server instances on random ports — no port conflicts.
- Tailscale-specific assertions (VAL-GW-022, VAL-GW-023, VAL-GW-024) require `TS_AUTHKEY` and actual tsnet connectivity — mark as **blocked** if not available.

### Test-to-Assertion Mapping (gateway-terminal)

**Gateway Core (server.go tests):**
- VAL-GW-001 → TestStartDevMode (dev mode healthz returns 200)
- VAL-GW-002 → TestStateDirResolution (state dir stored; AuthKeyError_NoStateNoKey for restarts)
- VAL-GW-003 → TestStartDevMode_GracefulShutdown, TestShutdown_SetsStoppedHealth
- VAL-GW-004 → TestAuthKeyError, TestRun_AuthKeyError, TestAuthKeyError_NoStateNoKey
- VAL-GW-027 → TestStartDevMode (dev mode on localhost HTTP)
- VAL-GW-028 → TestHandleHealthz_OK, TestHandleHealthz_Degraded, TestHandleHealthz_Starting, TestHandleHealthz_Disabled

**Web Terminal - Handler/Routing (webterm/handler_test.go):**
- VAL-GW-005 → TestHandler_TerminalPage_ContainsHTML (HTML with xterm.js, /ws/terminal/ URL)
- VAL-GW-025 → TestHandler_TerminalPage_RoomNotFound, TestHandler_ErrorJSON (ENOTFOUND with hint)

**Web Terminal - PTY/Hub (webterm/pty_test.go, hub_test.go):**
- VAL-GW-006 → TestStartTmuxAttach_CreatesSession (session created, PTY running)
- VAL-GW-007 → TestPTY_WriteInput (echo hello-webterm visible)
- VAL-GW-008 → TestPTY_Resize (132 columns verified via tmux list-panes)
- VAL-GW-009 → TestPTY_OutputBroadcast (reconnect behavior tested via subscriber model)
- VAL-GW-014 → TestPTY_OutputBroadcast (two subscribers both receive output)
- VAL-GW-017 → TestHub_RemoveClient, TestPTY_Close (one client removed, hub still works)
- VAL-GW-018 → TestHub_RoomIDs (multiple rooms registered independently)
- VAL-GW-019 → Tests use separate tmux sessions per room (inherent isolation)
- VAL-GW-020 → TestStartTmuxAttach_AttachExisting (pre-created session visible)
- VAL-GW-021 → TestStartTmuxAttach_CreatesSession (creates new if none exists)
- VAL-GW-026 → TestPTY_Close (abrupt close, no panic, idempotent)
- VAL-GW-029 → TestHub_AddClient_ConnectionLimit (max connections enforced)

**SSH Terminal (sshterm/server_test.go):**
- VAL-GW-010 → TestServer_ServeAndConnect, TestServer_WhoIsIdentityLogged (WhoIs verified)
- VAL-GW-011 → TestServer_PTYSession (routes by room-<id> username)
- VAL-GW-012 → TestServer_WindowResize (resize propagates), signal tests via parseSignal
- VAL-GW-013 → TestServer_DetachOnDisconnect (session survives disconnect)
- VAL-GW-015 → TestServer_MultipleSessions (3 concurrent SSH sessions)
- VAL-GW-016 → Covered by TestServer_MultipleSessions (shared tmux session)
- VAL-GW-030 → TestServer_WhoIsIdentityLogged (identity captured and verified)

**Tailscale-only (requires tsnet):**
- VAL-GW-022 → auto-TLS via ListenTLS — blocked without Tailscale
- VAL-GW-023 → MagicDNS resolution — blocked without Tailscale
- VAL-GW-024 → localhost access fails in tsnet mode — blocked without Tailscale
