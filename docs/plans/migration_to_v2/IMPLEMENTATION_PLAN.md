# V2 Migration Implementation Plan

> **Status**: Planning
> **Last Updated**: 2026-01-08
> **Scope**: Transform agentctl into an always-on, multi-actor, event-driven agent runtime

---

## Executive Summary

This plan organizes the v2 migration into **6 phases** spanning approximately 10 PRs. Each phase is independently valuable, feature-flagged for safe rollout, and has clear rollback paths.

**Key Constraints:**
- Additive-only DB migrations (no destructive changes)
- Feature flags control all new behavior
- Existing Claude Code/OpenCode integrations must continue working
- DSPy is wrapped, not dropped (migration path preserved)

---

## Phase Overview

| Phase | Name | PRs | Key Deliverable | Risk |
|-------|------|-----|-----------------|------|
| 0 | Foundation | PR0-PR1 | Hook v1 types + dispatcher | Low |
| 1 | Reactive Queue | PR2 | Mailbox wakeup notifications | Low |
| 2 | Supervisor Core | PR3-PR4 | Actor lifecycle + Engine interface | Medium |
| 3 | Hook Integration | PR5 | Hooks in actor loop | Medium |
| 4 | Identity & Memory | PR6-PR7 | Session lineage + Progressive memory | Medium |
| 5 | Cutover | PR8-PR9+ | Production rollout + higher layers | High |

---

## Phase 0: Foundation (Hooks v1 Types + Dispatcher)

**Goal**: Establish the canonical hook contract and dispatcher without changing runtime behavior.

### PR0: Hook v1 Types + Dispatcher Skeleton

**Files to create/modify:**
```
internal/runtime/hooks/
├── types.go           # hook.Input, hook.Output, hook.Action
├── dispatcher.go      # HookDispatcher interface + implementation
├── merge.go           # Deterministic merge rules
├── config.go          # hooks.yaml loader
└── dispatcher_test.go # Unit tests for merge semantics
```

**Deliverables:**
1. `hook.Input` struct with all canonical fields:
   - `Event`, `WorkspaceRoot`, `WorkspaceID`, `SessionID`, `ActorID`
   - `ToolName`, `ToolInput` (PreToolUse/PostToolUse)
   - `AssistantText` (PostAgentTurn/LLMResponse)
   - `Prompt` (LLMRequest/UserPromptSubmit)
   - `TokenEstimate` (ContextBudgetExceeded)
   - `CorrelationID`, `TurnID`

2. `hook.Output` struct:
   - `Decision` (approve/block/none)
   - `Reason`, `Context`
   - `UpdatedToolInput`, `UpdatedAssistantText`
   - `Actions` slice

3. `hook.Action` types (closed set for v1):
   - `run_skill`, `inject_context`, `send_mailbox`, `bb_post`, `bb_claim`

4. `HookDispatcher` with deterministic merge:
   - Any `block` wins
   - `UpdatedToolInput`: last-wins
   - `UpdatedAssistantText`: last-wins
   - `Actions`: appended in order
   - `Context`: collected for injection

**Tests:**
- `TestMerge_BlockWins`
- `TestMerge_LastWins_ToolInput`
- `TestMerge_LastWins_AssistantText`
- `TestMerge_ActionsAppended`
- `TestMerge_StableOrdering`

**Feature Flag:** `AGENTCTL_HOOKS_V1=0` (disabled by default)

**Acceptance Criteria:**
- [ ] Hook types compile and are importable
- [ ] Dispatcher can load hooks.yaml and resolve ordered hook lists
- [ ] Merge rules are deterministic under all input combinations
- [ ] All unit tests pass

---

### PR1: Hook Adapters (Legacy Compatibility)

**Goal**: Bridge existing shell/skill hooks to the new v1 contract.

**Files:**
```
internal/runtime/hooks/
├── adapters/
│   ├── shell.go       # JSON stdin/stdout shell adapter
│   ├── skill.go       # Skill envelope adapter
│   └── adapters_test.go
└── registry.go        # Hook registration by event
```

**Deliverables:**
1. Shell adapter:
   - Serialize `hook.Input` to JSON stdin
   - Parse JSON stdout to `hook.Output`
   - Handle malformed JSON gracefully (fail-open with reason)
   - Timeout handling (default 30s)

2. Skill adapter:
   - Route hook to skill via executor
   - Extract `data.hook_output` from envelope
   - Map skill errors to `decision=none` with error reason

3. Registry:
   - Load `hooks.yaml` from workspace then global
   - Resolve event + matcher → ordered hook list
   - Support both shell and skill hook types

**Tests:**
- `TestShellAdapter_ValidJSON`
- `TestShellAdapter_MalformedJSON_FailOpen`
- `TestShellAdapter_Timeout`
- `TestSkillAdapter_EnvelopeExtraction`
- `TestRegistry_PrecedenceOrder`

**Feature Flag:** Same as PR0 (`AGENTCTL_HOOKS_V1`)

**Acceptance Criteria:**
- [ ] Existing shell hooks work with new dispatcher
- [ ] Existing skill hooks work with new dispatcher
- [ ] Malformed hook output doesn't crash dispatcher
- [ ] Hook precedence (workspace > global) is correct

---

## Phase 1: Reactive Queue (Mailbox Wakeups)

### PR2: Mailbox Notify + Watcher

**Goal**: Add reactive wakeups to the mailbox without changing existing poll semantics.

**DB Migration (mailbox.db):**
```sql
CREATE TABLE IF NOT EXISTS mailbox_notify (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    to_ns TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mailbox_notify_to_ns_created
  ON mailbox_notify(to_ns, created_at);

CREATE TRIGGER IF NOT EXISTS mailbox_notify_trigger
AFTER INSERT ON mailbox
BEGIN
  INSERT INTO mailbox_notify (to_ns, created_at)
  VALUES (NEW.to_ns, strftime('%s','now'));
END;
```

**Files:**
```
internal/mailbox/
├── notify.go          # mailbox_notify operations
├── watcher.go         # MailboxWatcher (poll-based on notify table)
├── watcher_test.go
└── migrations.go      # Idempotent schema migrations
```

**Deliverables:**
1. Migration helpers:
   - `hasColumn(db, table, col)`
   - `addColumnIfMissing(db, table, ddl)`
   - `createIndexIfMissing(db, ddl)`

2. `MailboxWatcher`:
   - Polls `mailbox_notify` table (not mailbox itself)
   - Emits `WakeUp{Namespace}` events
   - Does NOT claim messages (Supervisor does that)
   - Configurable poll interval (default 500ms)
   - Prunes old notify rows after processing

3. Event emission:
   - In-memory pub/sub for wakeup events
   - Supervisor subscribes to relevant namespaces

**Tests:**
- `TestTrigger_CreatesNotifyRow`
- `TestWatcher_EmitsWakeup`
- `TestWatcher_DoesNotClaimMessages`
- `TestWatcher_PrunesOldNotifications`

**Feature Flag:** `AGENTCTL_MAILBOX_WATCHER=0`

**Acceptance Criteria:**
- [ ] INSERT into mailbox creates notify row (trigger)
- [ ] Watcher detects new messages within poll interval
- [ ] Watcher never mutates mailbox table
- [ ] Old notify rows are cleaned up

---

## Phase 2: Supervisor Core

### PR3: Supervisor Skeleton + Actor Lifecycle

**Goal**: Implement the core Supervisor that manages actor lifecycle and enforces one-in-flight-turn-per-actor.

**Files:**
```
internal/supervisor/
├── supervisor.go      # Supervisor struct + lifecycle
├── actor.go           # Actor struct + state machine
├── router.go          # Message routing logic
├── preemption.go      # Interruptive message handling
├── supervisor_test.go
└── actor_test.go
```

**Deliverables:**
1. `Supervisor`:
   - Manages actor registry
   - Subscribes to mailbox wakeups
   - Routes messages to correct actor
   - Enforces one in-flight turn per actor
   - Handles actor start/stop/restart

2. `Actor`:
   - State: `idle`, `processing`, `stopping`
   - Owns message claim lifecycle (Poll → Process → Ack/Nack)
   - Bound to workspace + agent_id

3. Preemption policy:
   - Messages from parent or human namespaces are interruptive
   - Interruptive messages cancel current context
   - Enqueued at front of actor's queue
   - Header `delivery=next_turn` disables preemption

4. Basic runner actor:
   - Stub that acks messages immediately
   - Used for testing Supervisor in isolation

**Tests:**
- `TestSupervisor_OneInFlightPerActor`
- `TestSupervisor_MultiActorConcurrency`
- `TestActor_StateTransitions`
- `TestPreemption_CancelsInFlight`
- `TestPreemption_NextTurnDelivery`

**Feature Flag:** `AGENTCTL_ACTOR_SUPERVISOR=0`

**Acceptance Criteria:**
- [ ] Supervisor starts and manages multiple actors
- [ ] Only one message processed per actor at a time
- [ ] Different actors can process concurrently
- [ ] Preemption cancels current turn and processes interruptive message

---

### PR4: Engine Interface + DSPy Adapter

**Goal**: Define the `AgentEngine` interface and wrap DSPy behind it.

**Files:**
```
internal/runtime/engine/
├── engine.go          # AgentEngine interface
├── context.go         # EngineContext (cancellable)
├── dspy_adapter.go    # DSPy wrapped as AgentEngine
├── tool_runner.go     # Canonical tool execution
└── engine_test.go
```

**Interface:**
```go
type AgentEngine interface {
    // Run executes a single turn, returning when:
    // - LLM returns final response (no tool calls)
    // - Context is cancelled (preemption)
    // - Error occurs
    Run(ctx context.Context, input EngineInput) (EngineOutput, error)
}

type EngineInput struct {
    Messages      []Message
    Tools         []ToolDef
    SystemPrompt  string
    Hooks         HookDispatcher
}

type EngineOutput struct {
    AssistantText string
    ToolCalls     []ToolCall
    ToolResults   []ToolResult
    StopReason    StopReason
}
```

**Deliverables:**
1. `AgentEngine` interface with cancellable context
2. DSPy adapter that:
   - Converts EngineInput to DSPy format
   - Runs DSPy agent loop
   - Converts output back to EngineOutput
   - Respects context cancellation
3. Tool runner with canonical names (`fs.read_file`, `edit.apply_patch`, etc.)
4. Tool result size enforcement (CAS offload for large results)

**Tests:**
- `TestDSPyAdapter_BasicTurn`
- `TestDSPyAdapter_ContextCancellation`
- `TestToolRunner_MaxResultBytes`
- `TestToolRunner_CASOffload`

**Feature Flag:** Part of `AGENTCTL_ACTOR_SUPERVISOR`

**Acceptance Criteria:**
- [ ] Engine interface is stable and documented
- [ ] DSPy adapter passes basic turn tests
- [ ] Context cancellation stops DSPy loop within reasonable time
- [ ] Large tool outputs are CAS-offloaded with `artifact.read` hint

---

## Phase 3: Hook Integration

### PR5: Hooks v1 in Actor Loop

**Goal**: Integrate the hook dispatcher into the actor's engine loop at all canonical events.

**Files to modify:**
```
internal/runtime/engine/
├── dspy_adapter.go    # Add hook dispatch points
├── hooks_integration.go
└── hooks_integration_test.go

internal/supervisor/
└── actor.go           # Wire hooks to actor
```

**Hook Events to Implement:**
1. `SessionStart` - When actor starts new session
2. `MessageReceived` - When mailbox message is claimed
3. `LLMRequest` - Before calling LLM
4. `LLMResponse` - After LLM returns
5. `PreToolUse` - Before each tool call
6. `PostToolUse` - After each tool call
7. `StopRequested` - When LLM wants to stop (can be blocked)
8. `PostAgentTurn` - Final rewrite opportunity
9. `ContextBudgetExceeded` - When prompt exceeds threshold
10. `SessionEnd` - When actor session ends

**Deliverables:**
1. Hook dispatch at each canonical event
2. `PreToolUse` can:
   - Block tool execution
   - Mutate tool input
   - Auto-reserve files
3. `StopRequested` can:
   - Block stop and inject continuation context
   - Force another iteration
4. `PostAgentTurn` can:
   - Rewrite assistant output
   - Redact content
5. Context injection:
   - Injected context stored in actor_context_inbox
   - Surfaces at top of next turn's prompt

**Tests:**
- `TestHook_PreToolUse_Block`
- `TestHook_PreToolUse_Mutate`
- `TestHook_StopRequested_Block`
- `TestHook_StopRequested_InjectContinuation`
- `TestHook_PostAgentTurn_Rewrite`
- `TestHook_ContextInjection_AppearsFirst`

**Feature Flag:** `AGENTCTL_HOOKS_V1=1` (enable to use new dispatcher)

**Acceptance Criteria:**
- [ ] All 10 canonical events fire hooks correctly
- [ ] PreToolUse block prevents tool execution
- [ ] StopRequested block continues iteration
- [ ] Injected context appears first in next prompt
- [ ] Existing hooks continue to work via adapters

---

## Phase 4: Identity & Memory

### PR6: Session Lineage Plumbing

**Goal**: Propagate session identity everywhere and record lineage edges.

**DB Migration (sessions.db):**
```sql
-- Add columns to sessions table
ALTER TABLE sessions ADD COLUMN workspace_id TEXT;
ALTER TABLE sessions ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'agentctl';
ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'ok';
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT;
ALTER TABLE sessions ADD COLUMN started_at TEXT;
ALTER TABLE sessions ADD COLUMN updated_at TEXT;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sessions_workspace_agent_started
  ON sessions(workspace_id, agent_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_workspace_agent_status
  ON sessions(workspace_id, agent_id, status);
CREATE INDEX IF NOT EXISTS idx_sessions_parent
  ON sessions(workspace_id, parent_session_id);

-- Session edges table
CREATE TABLE IF NOT EXISTS session_edges (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  from_session TEXT NOT NULL,
  to_session TEXT NOT NULL,
  edge_type TEXT NOT NULL,
  created_at TEXT NOT NULL,
  metadata TEXT,
  UNIQUE(workspace_id, from_session, to_session, edge_type)
);
```

**Files:**
```
internal/sessions/
├── lineage.go         # Session edge operations
├── identity.go        # Identity propagation helpers
├── migrations.go      # Idempotent migrations
└── lineage_test.go
```

**Deliverables:**
1. Session lineage columns populated on create/resume/fork
2. Edge recording:
   - `continues` - Resume creates edge from new → old
   - `forked_from` - Fork creates edge from new → source
   - `relates_to` - Weak references (optional)
3. Environment propagation:
   - `AGENTCTL_WORKSPACE`
   - `AGENTCTL_SESSION_ID`
   - `AGENTCTL_AGENT_ID`
4. Identity fallback file:
   - `~/.agentctl/sessions/active/<workspace_hash>-<agent_id>.json`
   - Contains session_id, agent_id, lineage for hook access
5. One active session enforcement:
   - Check for existing `running` session per (workspace, agent_id)
   - Require `--force` to override

**Tests:**
- `TestLineage_ContinuesEdge`
- `TestLineage_ForkedFromEdge`
- `TestLineage_ChainQuery`
- `TestIdentity_EnvPropagation`
- `TestIdentity_FallbackFile`
- `TestOneActiveSession_Enforcement`

**Feature Flag:** `AGENTCTL_SESSION_LINEAGE=0`

**Acceptance Criteria:**
- [ ] Resume creates `continues` edge
- [ ] Fork creates `forked_from` edge
- [ ] Chain query returns ancestors correctly
- [ ] Environment vars are set in skill/hook subprocesses
- [ ] Only one active session per (workspace, agent_id)

---

### PR7: Progressive Memory (L0/L1/L2)

**Goal**: Implement cursor-based, crash-safe progressive memory compaction.

**DB Migration (sessions.db):**
```sql
-- Actor turns (L0 raw)
CREATE TABLE IF NOT EXISTS actor_turns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  turn_index INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT,
  tool_name TEXT,
  tool_input TEXT,
  tool_output TEXT,
  artifact_digest TEXT,
  correlation_id TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(actor_id, session_id, turn_index)
);

-- Context inbox for hook-injected context
CREATE TABLE IF NOT EXISTS actor_context_inbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  kind TEXT NOT NULL DEFAULT 'context',
  text TEXT NOT NULL,
  created_at TEXT NOT NULL,
  surfaced_at TEXT
);

-- Memory state (cursors + artifact refs)
CREATE TABLE IF NOT EXISTS actor_memory_state (
  actor_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  task_context TEXT,
  next_turn_to_summarize INTEGER NOT NULL DEFAULT 0,
  next_summary_to_distill INTEGER NOT NULL DEFAULT 0,
  l1_artifact_id TEXT,
  l2_artifact_id TEXT,
  total_turns INTEGER NOT NULL DEFAULT 0,
  token_estimate INTEGER NOT NULL DEFAULT 0,
  last_summarize_at TEXT,
  last_distill_at TEXT,
  updated_at TEXT NOT NULL
);
```

**Files:**
```
internal/actor/memory/
├── turns.go           # L0 turn persistence
├── summarize.go       # L0→L1 summarization
├── distill.go         # L1→L2 distillation
├── cursors.go         # Cursor management
├── prompt_builder.go  # Budgeted prompt assembly
├── redaction.go       # Secret redaction
└── memory_test.go
```

**Deliverables:**
1. L0 turn persistence:
   - Every turn persisted before summarization
   - Large payloads CAS-offloaded
   - Turn index is monotonic per (actor, session)

2. Cursor-based summarization:
   - `next_turn_to_summarize` cursor
   - Batch N turns → generate L1 summary
   - Only advance cursor after CAS write succeeds
   - Crash recovery: re-summarize same batch (idempotent)

3. L1→L2 distillation:
   - `next_summary_to_distill` cursor
   - Batch M summaries → distill to L2
   - Same crash-safety guarantees

4. Prompt builder:
   - Deterministic order (system → injected → L2 → L1 → L0 → retrieval → message)
   - Budget enforcement (50k tokens default)
   - Emit `ContextBudgetExceeded` at 80% threshold

5. Redaction:
   - Apply patterns before L1/L2 writes
   - Raw L0 may contain secrets (TTL/access control separate)

**Tests:**
- `TestTurnPersistence_Monotonic`
- `TestSummarize_CursorAdvancesAfterWrite`
- `TestSummarize_CrashRecovery`
- `TestDistill_CursorAdvancesAfterWrite`
- `TestPromptBuilder_DeterministicOrder`
- `TestPromptBuilder_BudgetEnforcement`
- `TestRedaction_AppliedToSummaries`

**Feature Flag:** `AGENTCTL_PROGRESSIVE_MEMORY=0`

**Acceptance Criteria:**
- [ ] Turns are durable before any summarization
- [ ] Cursor only advances after successful write
- [ ] Crash during summarization is recoverable (idempotent retry)
- [ ] Prompt order matches spec exactly
- [ ] Budget exceeded triggers hook at 80%
- [ ] Summaries are redacted

---

## Phase 5: Cutover

### PR8: Cutover One Real Role

**Goal**: Enable full actor runtime for one role (e.g., `coder`) in production.

**Deliverables:**
1. Feature flag configuration:
   ```bash
   AGENTCTL_HOOKS_V1=1
   AGENTCTL_ACTOR_SUPERVISOR=1
   AGENTCTL_MAILBOX_WATCHER=1
   AGENTCTL_SESSION_LINEAGE=1
   AGENTCTL_PROGRESSIVE_MEMORY=1
   ```

2. Run full cutover test plan:
   - Hook dispatcher merge semantics
   - Hook adapters
   - Token estimator
   - Schema migration idempotency
   - Mailbox semantics (lease/ack/nack)
   - MailboxWatcher
   - Supervisor (one-in-flight, multi-actor)
   - Session lineage
   - Progressive memory

3. Monitoring:
   - Lease expiry rate
   - Duplicate message rate
   - Hook block rate
   - Supervisor stall detection

4. Rollback procedure documented and tested:
   - Set all flags to 0
   - Restart daemons
   - Verify mailbox processing continues
   - No schema rollback needed (additive only)

**Tests:**
- Full E2E cutover rehearsal scenario
- Rollback drill

**Acceptance Criteria:**
- [ ] One real role (coder/overseer) runs on new runtime
- [ ] All cutover tests pass
- [ ] Monitoring shows no regressions
- [ ] Rollback procedure verified

---

### PR9+: Higher-Layer Alignment

**Goal**: Align search, graph, memory, and summarization with new runtime.

**Areas:**
1. **Search integration**:
   - Semantic search results in retrieval slot
   - Graph neighbors in retrieval slot
   - Bounded by prompt budget

2. **Graph + PageRank**:
   - Task → file edges from hooks
   - PageRank for continuation prioritization
   - Critical path analysis

3. **Summarization pipeline**:
   - Hook-triggered vs background
   - Session restore from L1/L2 artifacts

4. **Multi-agent coordination**:
   - Reservation enforcement in edit tools
   - Blackboard coordination
   - Inter-actor messaging

**This is ongoing work beyond initial cutover.**

---

## Rollback Plan

If any phase causes issues in staging/production:

### Quick Rollback (< 5 min)
```bash
# Disable all v2 features
export AGENTCTL_HOOKS_V1=0
export AGENTCTL_ACTOR_SUPERVISOR=0
export AGENTCTL_MAILBOX_WATCHER=0
export AGENTCTL_SESSION_LINEAGE=0
export AGENTCTL_PROGRESSIVE_MEMORY=0

# Restart affected processes
```

### What Triggers Rollback
- Lease expiry behavior changes unexpectedly
- Duplicate message rate increases significantly
- Hooks block unexpectedly due to config errors
- Supervisor stalls (no progress with pending mailbox)
- Session identity not propagating correctly

### Post-Rollback
- DB migrations stay in place (additive only)
- Old code paths continue to work
- Investigate root cause before re-enabling

---

## Dependencies Graph

```
PR0 (Hook Types) ─────┬─────▶ PR1 (Adapters)
                      │
                      └─────▶ PR5 (Hook Integration)
                                    │
PR2 (Mailbox Watcher) ──────▶ PR3 (Supervisor) ──────▶ PR4 (Engine)
                                                           │
                                                           ▼
                                                      PR5 (Hooks)
                                                           │
PR6 (Session Lineage) ──────────────────────────────▶ PR8 (Cutover)
                                                           ▲
PR7 (Progressive Memory) ──────────────────────────────────┘
```

**Critical Path**: PR0 → PR1 → PR2 → PR3 → PR4 → PR5 → PR8

---

## Risk Assessment

| Phase | Risk | Mitigation |
|-------|------|------------|
| 0 | Low | Types only, no runtime changes |
| 1 | Low | Trigger + watcher are additive |
| 2 | Medium | Supervisor lifecycle is complex | Extensive tests + feature flag |
| 3 | Medium | Hook dispatch affects all tools | Adapter compatibility + fail-open |
| 4 | Medium | Identity changes touch many files | Env fallback + gradual rollout |
| 5 | High | Production traffic | Staged rollout + monitoring + rollback drills |

---

## Success Metrics

After full cutover:

1. **Correctness**
   - Zero data loss in sessions/turns
   - Lease semantics preserved (at-least-once)
   - Hook determinism verified

2. **Performance**
   - Wakeup latency < 1s (message → actor claim)
   - No increase in duplicate processing
   - Prompt assembly < 100ms

3. **Reliability**
   - Crash recovery works (cursor-based)
   - Rollback takes < 5 min
   - No manual intervention required for steady state

---

## Appendix: File Checklist

### New Files to Create
```
internal/runtime/hooks/
├── types.go
├── dispatcher.go
├── merge.go
├── config.go
├── registry.go
├── adapters/
│   ├── shell.go
│   └── skill.go

internal/mailbox/
├── notify.go
├── watcher.go
├── migrations.go

internal/supervisor/
├── supervisor.go
├── actor.go
├── router.go
├── preemption.go

internal/runtime/engine/
├── engine.go
├── context.go
├── dspy_adapter.go
├── tool_runner.go
├── hooks_integration.go

internal/sessions/
├── lineage.go
├── identity.go

internal/actor/memory/
├── turns.go
├── summarize.go
├── distill.go
├── cursors.go
├── prompt_builder.go
├── redaction.go
```

### Existing Files to Modify
- `internal/mailbox/store.go` - Add notify trigger
- `internal/storage/sessions/store.go` - Add lineage columns
- `internal/executor/*.go` - Add identity propagation
- `skills/*/main.go` - Use propagated identity
- `configs/hooks/*.sh` - Verify v1 compatibility

---

## Next Steps

1. **Start PR0**: Create `internal/runtime/hooks/types.go` with canonical types
2. **Review**: Get architecture review on hook types before implementation
3. **Parallel work**: DB migration helpers can be written alongside PR0
