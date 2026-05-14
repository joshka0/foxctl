# Integration & E2E Test Quality Review Report

**Scope:** `tests/integration/`, `tests/e2e/`, `cmd/foxctl/cmd/*_test.go`, and all `//go:build integration` tests across the foxctl Go codebase.
**Date:** 2026-05-05
**Reviewer:** Worker Droid (subagent)

---

## Summary

| Category | Count | Notes |
|----------|-------|-------|
| KEEP | 28 | Valuable integration tests that exercise real cross-component paths |
| MARGINAL | 8 | Have some value but are fragile, slow, or partially duplicative |
| VANITY | 12 | Look like integration tests but are expensive unit tests |
| FLAKY | 5 | Known or likely to be unreliable due to external deps, timing, or LLM calls |

---

## 1. Integration Tests Worth Keeping (KEEP)

### `tests/e2e/multiagent_workflow_test.go` (7 tests)
- **TestMultiAgentWorkflow_FullCycle** — Exercises the full stack: task store → graph analysis → mailbox → file reservations → overseer scoring. Uses real SQLite-backed stores (`t.TempDir()`). This is a genuine integration test that validates cross-component coordination.
- **TestMultiAgentWorkflow_CyclicDependencies** — Tests graph analyzer behavior with cycles.
- **TestMultiAgentWorkflow_MessagePrioritization** — Validates overseer scoring with admin messages.
- **TestMultiAgentWorkflow_FileGuardConflictResolution** — Tests file reservation conflict detection and release.
- **TestMultiAgentWorkflow_SharedReservations** — Tests shared vs exclusive reservation modes.
- **TestMultiAgentWorkflow_BroadcastMessages** — Tests broadcast message delivery.
- **TestMultiAgentWorkflow_PlanOperation** — Tests plan event emission via mailbox.

**Why KEEP:** These tests wire together real storage (tasks, blackboard) with real analyzers and scorers. They verify emergent behavior (e.g., Task C gets top recommendation due to admin message + critical path) that no unit test can catch.

### `tests/integration/agent_helpers_test.go` (6 tests)
- **TestSender_SendAsk**, **TestSender_SendReply**, **TestSender_SendCmd**, **TestSender_SendEvent**, **TestReceiver**, **TestMessageMethods**, **TestFilterFunctions**, **TestReplyToBuilder**

**Why KEEP:** Tests the agent sender/receiver helpers against a real `mailbox.Open()` store. Validates message serialization, polling, ack/nack/retry semantics. These are core protocol behaviors that must work across process boundaries.

### `tests/integration/agent_message_integration_test.go` (2 tests)
- **TestMessagePassingIntegration** — Full request-response, fire-and-forget command, event broadcasting, session lineage, retry with Nack, workspace filtering.
- **TestMessageBuilderFluent** — Fluent builder API with context preservation.

**Why KEEP:** End-to-end message passing workflows through real mailbox store. Tests message types (Ask, Reply, Cmd, Event, Console) that are the backbone of the multi-agent system.

### `tests/integration/cache_test.go` (5 tests)
- **TestCacheHitSameInput**, **TestCacheMissDifferentInput**, **TestCacheModeOff**, **TestCacheModeOnlyMiss**, **TestCacheKeyDeterminism**

**Why KEEP:** Tests the cache subsystem with real `cache.Open()` stores. Verifies cache key determinism (JSON key reordering, digest reordering), mode behavior, and the fact that cache is currently disabled but still stores entries. Important for cache correctness when re-enabled.

### `cmd/foxctl/cmd/run_integration_test.go` (3 tests)
- **TestRunCommandEmitsCompleteMeta** — Validates envelope meta fields (TS, Source, CASDigest) from real skill execution.
- **TestSkillsRunProducesInlineEnvelope** — Validates inline vs CAS artifact behavior.
- **TestRunCommandRememberSavesMemory** — Tests `--remember` flag writes to memory store.

**Why KEEP:** These exercise the full CLI → skill → envelope → CAS pipeline with real skill binaries. They catch envelope contract breaks.

### `cmd/foxctl/cmd/skills_chain_integration_test.go` (1 test)
- **TestFsReadSkillChainsThroughBash** — Bash pipeline: `fs/ls` → `fs/read` via stdin/stdout chaining.

**Why KEEP:** Tests real skill binary chaining through bash pipes. Validates envelope I/O contract between skills.

### `cmd/foxctl/cmd/room_relay_tmux_integration_test.go` (3 tests)
- **TestIntegrationRelayRoomMessageTmuxRealTmux** — Real tmux session, text injection, capture-pane verification.
- **TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux** — Proves relay line is consumed by target process (cat >> file).
- **TestIntegrationRelayRoomMessageTmuxDispatchesQueuedDraftRealTmux** — Queued-draft state detection with bounded retry.

**Why KEEP:** These are the only tests that verify the tmux relay actually works against a live tmux server. Gated behind `FOXCTL_INTEGRATION_TMUX=1`. They catch real transport bugs.

### `cmd/foxctl/cmd/room_sandbox_integration_test.go` (many tests)
- Tests for VAL-RS-012 through VAL-RS-019: sandbox room spawn CWD, scope path resolution, relay delivery, room loop, status/inbox sandbox context, red/green pattern, agile commands on sandbox rooms.

**Why KEEP:** These test the sandbox room integration with real git worktrees and tmux sessions (where available). They validate the room-sandbox boundary which is a critical operational surface.

### `internal/interfaces/tui/group_a_test.go` (many tests)
- **TestBootManager_TransitionsToReadyWithinTimeout**, **TestBootManager_LoadingPhaseBeforeTransition**, **TestBootManager_NoSyncHTTPBeforeFirstPaint**
- **TestInventory_SixFieldsPerRow**, **TestInventory_DeterministicSort**, **TestInventory_ColumnsAt80Cols**
- **TestEmptyState_NoAgentsShowsReadyWithZeroRows**, **TestEmptyState_RenderContainsEmptyHint**
- **TestErrorState_UnreachableAPITransitionsToError**, **TestErrorState_ErrorRenderContainsURL**, **TestErrorState_ErrorRenderContainsRetryHint**, **TestErrorState_EscKeyBindingPresent**, **TestErrorState_ESCExitsCleanly**, **TestErrorState_RetryKeyTriggersRetry**
- **TestStatusFooter_ReadyStateContainsAllThreeElements**, **TestStatusFooter_ErrorStateContainsAllThreeElements**, **TestStatusFooter_LoadingStateContainsAllThreeElements**, **TestStatusFooter_MinimumSize**

**Why KEEP:** TUI walking skeleton integration tests using `testfixture.BootDaemon()` and `gotui.MockTerminal`. They verify rendering, boot phases, and keybindings. Well-structured with clear VAL-SKEL traceability.

### `cmd/foxctl/cmd/agent_wait_jido_test.go` (11 tests)
- **TestResolveJidoTerminalCallback_ReturnsLatestTerminalPayload**, **TestResolveJidoTerminalCallback_NotFoundWhenNoTerminalEvent**, **TestResolveJidoTerminalCallback_FailedFallsBackFromEventType**
- **TestWaitForJidoRunStateWithPoll_CompletesAfterRunning**, **TestWaitForJidoRunStateWithPoll_ReturnsFailedTerminalState**, **TestWaitForJidoRunStateWithPoll_TimesOutWhenStateNeverAppears**, **TestWaitForJidoRunStateWithPoll_ReturnsReaderError**
- **TestAskWaitFailureHint_IncludesStructuredCallbackDetails**, **TestBuildAskRunResponseData_IncludesCallbackDetails**
- **TestRunAgentAskStatus_ReportsNotFound**, **TestRunAgentAskStatus_ReturnsCallbackEnrichedStatus**

**Why KEEP:** Tests Jido ask/wait callback resolution with fake readers AND a real libsql event/projection store. The last test (`ReturnsCallbackEnrichedStatus`) is a genuine integration test through the full event store → projection → CLI path.

### `cmd/foxctl/cmd/sandbox_smolvm_test.go` (8 tests)
- All tests exercise the smolvm sandbox command plan generation and execution with injected runners.

**Why KEEP:** Tests sandbox command plan generation (argv, env, limitations) and execution with a fake runner. The fake runner tests are fast and validate the plan structure without needing real smolvm.

---

## 2. Flaky Tests That Need Fixing or Removal (FLAKY)

### `tests/integration/hierarchy_spawn_test.go` (2 tests)
- **TestHierarchySpawn**, **TestOverseerConcurrencyLimit**

**Why FLAKY:** Requires `GEMINI_API_KEY` or `FOXCTL_LLM_API_KEY` and calls real LLM APIs (Gemini). Spawns actual overseer and child agents with 2-minute timeouts. LLM API latency, rate limits, and model availability make these inherently flaky. They also leave sessions that need cleanup (`rt.Kill`).

**Recommendation:** Replace with tests that use a fake LLM client or mock the runtime's LLM calls. The spawn logic and depth limit enforcement can be tested without a real LLM.

### `tests/integration/embedding_gemini_test.go` (2 tests)
- **TestGeminiProvider_Integration**, **TestGeminiProvider_Integration_GeminiEmbedding001**

**Why FLAKY:** Hits live Gemini API. Requires `GEMINI_API_KEY`. Network latency, API changes, and rate limiting make these flaky. The similarity test (`sim12 <= sim13`) could fail with model updates.

**Recommendation:** Keep as manual/smoke tests. Move to a separate `smoke/` directory or gate behind an additional `smoke` build tag. Do not run in CI.

### `internal/intelligence/verification/cove_integration_test.go` (2 tests)
- **TestCoVeIntegration_FullPipeline**, **TestCoVeIntegration_VerifyOnly**

**Why FLAKY:** Requires live LLM API key (Cerebras, Groq, OpenRouter, or OpenAI). 5-minute timeout. LLM responses are non-deterministic — the claim extraction and verification results may vary. The `c2` claim (Eiffel Tower in London) should be False but LLM hallucination could make it uncertain.

**Recommendation:** Convert to tests with a fake LLM client that returns deterministic responses. The CoVe pipeline logic (claim extraction → verification → refinement) can be tested without real LLM calls.

### `internal/context/companion/turnlock_pg_integration_test.go` (2 tests)
- **TestPgTurnLock_LockUnlockCycle**, **TestPgTurnLock_Lock_RespectsContextCancellation**

**Why MARGINAL/FLAKY:** Requires `FOXCTL_TEST_POSTGRES_DSN`. These are genuine integration tests but depend on an external PostgreSQL instance. If the DSN is not available, they skip. The lock semantics are important but could be tested with an in-memory Postgres (e.g., embedded PostgreSQL) for CI reliability.

**Recommendation:** Keep but add embedded PostgreSQL support for CI. Mark as `FLAKY` if CI doesn't have Postgres.

### `tests/integration/symbol_index_test.go` (3 tests)
- **TestSymbolIndex_PostReviewFlow**, **TestSymbolIndex_IncrementalUpdate**, **TestSymbolIndex_CallGraph**

**Why MARGINAL:** These test the symbol indexer with real file I/O and memory store. They are not flaky per se, but they test a heuristic indexer (symbol extraction is best-effort). The expected symbol counts ("at least 4") are loose. They are slow due to file writes and indexing.

**Recommendation:** KEEP but tighten assertions. The loose `>= 4` checks could mask regressions.

---

## 3. Integration Tests That Are Just Expensive Unit Tests (VANITY)

### `tests/integration/tool_integration_test.go` (3 tests)
- **TestToolIntegration_RetrievalFunnelWorkflow**, **TestToolIntegration_StructuredDiffWorkflow**, **TestToolIntegration_DryRunMode**

**Why VANITY:** These test individual tools in a registry against a temp workspace. They don't test integration between components — they test each tool in isolation. The "workflow" is just sequential tool calls in the same test. The `code.search` step even notes it may fail if `rg` is not available. The telemetry recorder is a mock. These are expensive unit tests (file I/O + registry setup) dressed as integration tests.

**Recommendation:** Move to `internal/agent/tools/` as unit tests. They don't need the `integration` build tag.

### `tests/integration/swe_grep_test.go` (1 test + subtests)
- **TestSWEGrep_CandidatesToSnippets** with 5 subtests

**Why VANITY:** This tests the `code/snippet_extract` skill by building it and running it as a subprocess. While it exercises a real skill binary, the test is essentially verifying skill behavior through CLI invocation. The assertions are on envelope structure and summary fields, not on cross-component interaction. It requires `make skills-build` first.

**Recommendation:** Keep as a skill-level test, but it's more of a skill acceptance test than an integration test. Consider moving to `skills/code_snippet_extract/`.

### `cmd/foxctl/cmd/e2e_test.go` (1 test + subtests)
- **TestEndToEndCacheMemoryWorkflow** with 2 subtests

**Why VANITY:** Tests cache + memory workflow end-to-end, but cache is disabled (`expected cache to be disabled`). The test verifies that cache does NOT work, which is odd for an "end-to-end" test. It exercises real skills and memory stores, but the cache path is a no-op.

**Recommendation:** Re-enable cache in the test or rename to reflect it's testing the disabled-cache path. Currently it's an expensive test that doesn't verify the integration it's named for.

### `cmd/foxctl/cmd/agent_spawn_jido_test.go` (3 tests)
- **TestV2ProcessProfileForAgentRole**, **TestBuildJidoPluginConfig_UsesSharedDefaultToolSpec**, **TestBuildJidoInitialState_IncludesTaskContinuity**

**Why VANITY:** Pure unit tests — no I/O, no external deps, no integration. They test function outputs against expected values.

**Recommendation:** Move to `internal/v2/adapters/jido/` or `internal/v2/core/` as unit tests.

### `cmd/foxctl/cmd/agent_ask_dispatcher_test.go` (2 tests)
- **TestMailboxAskDispatcher_Send**, **TestMailboxAskDispatcher_SendRequiresStore**

**Why VANITY:** Tests a dispatcher with a real mailbox store, but the test is just verifying that `Send()` writes to the store and `SendRequiresStore` returns an error. This is a thin wrapper test — the real integration is in `agent_message_integration_test.go`.

**Recommendation:** Move to `internal/v2/core/ask/` as unit tests.

### `cmd/foxctl/cmd/agent_ask_runtime_test.go` (4 tests)
- **TestResolvedAskDispatcherMode**, **TestParseDurationMillisEnv**, **TestResolvedSpawnExecutionLayer**, **TestResolvedAskDispatcherMode_IgnoresAgentExecutionLayer**

**Why VANITY:** Pure unit tests for helper functions. No integration involved.

**Recommendation:** Move to a `_test.go` file in the same package but without `integration` tag, or to `internal/v2/core/ask/`.

### `cmd/foxctl/cmd/agent_spawn_workspace_test.go` (1 test)
- **TestCurrentSpawnWorkspaceRootAndID**

**Why VANITY:** Tests `os.Getwd()` and `ws.ID()` — pure unit test with no integration.

**Recommendation:** Move to `internal/platform/workspace/` as a unit test.

### `cmd/foxctl/cmd/overseer_v2_orchestration_test.go` (2 tests)
- **TestResolveOverseerDispatchParentAgentID_PrefersExplicitEnv**, **TestResolveOverseerDispatchParentAgentID_FallsBackToFirstParent**

**Why VANITY:** Pure unit tests for env var parsing.

**Recommendation:** Move to `internal/v2/adapters/jido/` as unit tests.

### `cmd/foxctl/cmd/sandbox_smolvm_test.go` — Plan-only tests
- **TestSandboxSmolVMProbeLMStudioWritesPlanEnvelope**, **TestSandboxSmolVMPackagePlanWritesPackCreatePlan**, **TestSandboxSmolVMRunAgentPlanWritesLimitations**, **TestSandboxSmolVMFoxctlPackagePlanWritesOrderedSteps**

**Why VANITY (subset):** These 4 tests only verify that the CLI command generates the correct plan envelope. They don't execute anything. The execution tests (with injected runner) are the real integration tests.

**Recommendation:** The plan-only tests are valuable as unit tests. Move them to a non-integration test file. Keep the execution tests as integration tests.

---

## 4. Missing Integration Coverage

### Critical Gaps

1. **Agent Daemon Lifecycle Integration**
   - No integration tests for `foxctl agent spawn` → daemon → engine Run() → reply.
   - The hierarchy_spawn_test.go tests runtime spawning but requires a real LLM.
   - Missing: spawn → run one turn → kill, with a fake LLM provider.

2. **Skill Execution with Real WASI Runtime**
   - Tests build and run skills as exec binaries, but WASI skills are not tested end-to-end.
   - Missing: compile a WASI skill, run it through the WASI runtime, verify output envelope.

3. **Room Coordination with Multiple Real Agents**
   - The e2e tests test task graph + mailbox + overseer, but not the full room protocol (epic → milestone → story → task assignment → agent execution).
   - Missing: room creation → epic start → milestone → story → agent spawn → task execution → status update.

4. **Storage Backend Integration (Turso/Postgres)**
   - Most tests use SQLite (libsql) via `t.TempDir()`.
   - Missing: integration tests against Turso remote database.
   - Missing: integration tests for CAS with remote storage.

5. **Context/Retrieval Pipeline Integration**
   - Tests exist for individual tools (semantic search, snippet extract) but not the full retrieval pipeline: semantic search → repo index search → snippet extract → context assembly.
   - Missing: end-to-end context retrieval for a real query.

6. **Session Memory and Trajectory Integration**
   - Tests for memory put/get exist, but not for the full session → trajectory → memory promotion pipeline.
   - Missing: run a skill → save job as memory → search memory → verify trajectory linking.

7. **Gateway/HTTP Integration**
   - `gateway_test.go` has some tests but they use `httptest.Server`.
   - Missing: integration test for gateway registration/deregistration with a real (or more realistic) gateway.

8. **Hook System Integration**
   - `hooks_runtime_test.go` and `hooks_proposal_*_test.go` test individual hook commands.
   - Missing: end-to-end hook execution: trigger → hook script → context injection → skill run.

9. **Obsidian/ContextWiki Bridge Integration**
   - No integration tests for `foxctl obsidian graph build → promote → bridge reconcile → index build`.
   - This is a critical knowledge-layer pipeline with no automated integration coverage.

10. **Multi-Backend Room Relay (Zellij)**
    - Tmux relay has thorough integration tests (gated).
    - Missing: equivalent zellij relay integration tests.

---

## 5. Recommendations by Priority

### P0 — Fix Flaky Tests
1. Replace `hierarchy_spawn_test.go` LLM-dependent tests with fake LLM client tests.
2. Gate `embedding_gemini_test.go` and `cove_integration_test.go` behind a `smoke` tag, not `integration`.
3. Add embedded PostgreSQL for `turnlock_pg_integration_test.go`.

### P1 — Remove Vanity Tests from Integration Suite
1. Move `tool_integration_test.go` tests to `internal/agent/tools/` as unit tests.
2. Move `agent_spawn_jido_test.go`, `agent_ask_runtime_test.go`, `agent_spawn_workspace_test.go`, `overseer_v2_orchestration_test.go` tests to appropriate unit test locations.
3. Split `sandbox_smolvm_test.go` — move plan-only tests to unit tests, keep execution tests.

### P2 — Add Missing Coverage
1. Agent daemon lifecycle with fake LLM.
2. Full room coordination workflow (epic → story → execution).
3. WASI skill end-to-end execution.
4. Context retrieval pipeline integration.
5. Obsidian bridge pipeline integration.

### P3 — Improve Existing Tests
1. Tighten assertions in `symbol_index_test.go` (exact symbol counts instead of `>= 4`).
2. Re-enable or fix cache in `e2e_test.go` so it tests actual caching behavior.
3. Add zellij relay integration tests (gated like tmux).

---

## Appendix: Test Inventory

### `tests/integration/` (9 files, 22 tests)
| File | Tests | Tag | Rating |
|------|-------|-----|--------|
| tool_integration_test.go | 3 | integration | VANITY |
| agent_helpers_test.go | 8 | integration | KEEP |
| symbol_index_test.go | 3 | integration | KEEP (tighten) |
| embedding_gemini_test.go | 2 | integration | FLAKY |
| agent_message_integration_test.go | 2 | integration | KEEP |
| hierarchy_spawn_test.go | 2 | integration | FLAKY |
| swe_grep_test.go | 1 | integration | MARGINAL |
| cache_test.go | 5 | integration | KEEP |

### `tests/e2e/` (1 file, 7 tests)
| File | Tests | Tag | Rating |
|------|-------|-----|--------|
| multiagent_workflow_test.go | 7 | (none) | KEEP |

### `cmd/foxctl/cmd/` — Integration-tagged files
| File | Tests | Tag | Rating |
|------|-------|-----|--------|
| run_integration_test.go | 3 | integration | KEEP |
| room_relay_tmux_integration_test.go | 3 | integration | KEEP |
| skills_chain_integration_test.go | 1 | integration | KEEP |
| room_sandbox_integration_test.go | ~20 | (none) | KEEP |
| e2e_test.go | 2 | (none) | VANITY |
| agent_wait_jido_test.go | 11 | (none) | KEEP |
| sandbox_smolvm_test.go | 8 | (none) | KEEP (subset VANITY) |
| agent_spawn_jido_test.go | 3 | (none) | VANITY |
| agent_ask_dispatcher_test.go | 2 | (none) | VANITY |
| agent_ask_runtime_test.go | 4 | (none) | VANITY |
| agent_spawn_workspace_test.go | 1 | (none) | VANITY |
| overseer_v2_orchestration_test.go | 2 | (none) | VANITY |

### Other integration-tagged files
| File | Tests | Tag | Rating |
|------|-------|-----|--------|
| internal/interfaces/tui/group_a_test.go | ~15 | integration | KEEP |
| internal/context/companion/turnlock_pg_integration_test.go | 2 | integration | FLAKY |
| internal/intelligence/verification/cove_integration_test.go | 2 | integration | FLAKY |
