
---
title: Reactive Actor Runtime Migration
status: draft
owners: []
last_updated: 2026-01-08
---

## 0. Purpose

Migrate agentctl’s current poll/daemon + mixed hook ecosystem into a single, always-on, **reactive actor runtime** with:

- **Supervisor-managed actors** (one process, many actors)
- **SQLite mailbox as durable queue** with leased delivery (at-least-once)
- **MailboxWatcher for wakeups only** (no semantic changes to Poll/Ack/Nack)
- **Hooks v1**: deterministic dispatcher + standard `hook.Input`/`hook.Output`
- **Unified session lineage propagation** (session_id/agent_id/workspace everywhere)
- **Progressive memory for actors** (L0/L1/L2 cursor-based; crash safe; redacted)

This plan is intentionally incremental and reversible.

---

## 1. Guiding Contracts (non-negotiable)

### 1.1 Delivery & Queue
- At-least-once delivery; handlers must be idempotent
- Poll() claims via lease (visibility timeout); Ack deletes; Nack reschedules
- Watcher is **wake-up only**; all claiming is still done via Poll()

### 1.2 Concurrency
- Per-actor concurrency = 1 (MVP)
- Supervisor only claims when actor is idle

### 1.3 Hooks v1
- All hooks receive `hook.Input` and return `hook.Output` in `data.hook_output`
- Deterministic aggregation:
  - any `block` wins
  - tool arg updates are **last-wins** in hook order
  - assistant output rewrite is **last-wins**
  - actions are appended in hook order

### 1.4 Session lineage
- Every message, hook input, and persisted artifact is tagged with:
  - `session_id`, `agent_id`, `workspace_id`
- Sessions can be UUID (legacy Claude) or ULID (new runtime). Both are TEXT.

### 1.5 Progressive memory
- Raw turns durable before summarization
- Cursor-based summarization, crash safe
- Summaries redacted before persistence

---

## 2. What we are reshaping

### Current (practical)
- `internal/agent/daemon/*`: poll loop + per-agent process; dspy-go runtime embedded
- `configs/hooks/*.sh`: shells act as hooks with inconsistent output shapes
- `internal/agent/tools/*`: tools implemented as dspy-go FuncTool wrappers
- `sessions.db`: primarily Claude Code session capture + recall; not guaranteed lineage-complete
- mailbox: durable SQLite, but wakeups are polling-based

### Target (runtime spine)
- `internal/actor/*`: Supervisor + Watcher + EventBus + Actor implementations
- Hooks are first-class and Go-native (skills remain the executable unit)
- dspy-go becomes just an engine implementation behind an interface (optional)
- Unified propagation of session_id/agent_id/workspace across everything

---

## 3. Feature flags (for safe rollout)

Environment flags:
- `AGENTCTL_HOOKS_V1=0|1` (default 0 initially)
- `AGENTCTL_ACTOR_SUPERVISOR=0|1` (default 0 initially)
- `AGENTCTL_MAILBOX_WATCHER=0|1` (default 0 initially)
- `AGENTCTL_SESSION_LINEAGE=0|1` (default 0 initially)
- `AGENTCTL_PROGRESSIVE_MEMORY=0|1` (default 0 initially)

---

## 4. Repo layout changes (target steady state)

Add/extend packages (names can be adjusted, but keep responsibilities stable):

- `internal/hooks/dispatcher/`
  - load hooks config
  - match events/tool names
  - execute hook skills
  - merge outputs deterministically

- `internal/actor/`
  - `supervisor/` (lifecycle + routing)
  - `watcher/` (mailbox_notify polling)
  - `eventbus/` (ephemeral pub/sub + selective persister)
  - `actors/` (runtime actor types)
  - `memory/` (progressive memory implementation)

- `internal/agent/engine/` (engine interface + adapters)
  - `Engine` interface
  - `dspy_engine` adapter (keeps current behavior)
  - future: `llmchat_engine` (OpenAI-compatible tool calling)

- `internal/storage/*` migrations
  - mailbox.db: notify + metadata columns
  - sessions.db: lineage columns + actor tables

---

## 5. PR-sized migration sequence (ordered)

Each PR has:
- Scope (what changes)
- Files to touch (suggested)
- Acceptance checks
- Rollback plan

### PR0 — Contracts: Hook v1 types + minimal dispatcher skeleton
**Scope**
- Define/extend canonical hook types
- Implement dispatcher that runs “hook skills” and merges outputs deterministically
- No runtime behavior changes yet

**Files**
- `internal/domain/hook/` (extend types)
- `internal/hooks/dispatcher/*` (new)
- `configs/hooks.yaml` example(s)
- `cmd/agentctl/cmd/hooks.go` (optional: `agentctl hooks dry-run`)

**Acceptance**
- Can run dispatcher in a unit test with fake hook skills and get deterministic merged output
- Block overrides approve; last-wins updates behave

**Rollback**
- Keep `AGENTCTL_HOOKS_V1=0` default; dispatcher unused

---

### PR1 — Hook adapters: legacy shell output + legacy skill output compatibility
**Scope**
- Allow existing shell hooks and non-v1 hook skills to be executed via dispatcher
- Map old JSON to `hook.Output`

**Files**
- `internal/hooks/dispatcher/adapters/*.go`
- `internal/domain/hook/compat.go` (mapping helpers)

**Acceptance**
- Shell hook producing `{decision:"approve", context:"..."}` becomes `hook.Output{Decision: approve, Context: ...}`
- Nonconforming hook skills don’t crash dispatcher; they fail-open as NONE/APPROVE with reason

**Rollback**
- No production path change; still behind flags

---

### PR2 — Mailbox notify wakeups (no semantic changes to queue)
**Scope**
- Add mailbox_notify + trigger
- Implement MailboxWatcher that emits WakeUp{to_ns}
- Supervisor not required yet

**Files**
- `internal/storage/mailbox/migrations.go` (or equivalent)
- `internal/actor/watcher/mailbox_watcher.go` (new)

**Acceptance**
- Send message → notify row appears → watcher wakes within ~50–100ms
- Poll/Ack/Nack behavior unchanged

**Rollback**
- Disable watcher flag; Poll loop still works

---

### PR3 — Supervisor skeleton + one minimal actor (“runner actor”)
**Scope**
- Implement Supervisor state machine
- Implement minimal Actor that can process a message type deterministically (e.g., just acknowledges or runs a simple skill)
- Route mailbox via watcher wakeups

**Files**
- `internal/actor/supervisor/*.go`
- `internal/actor/actors/runner_actor.go`
- `cmd/agentctl/cmd/actor_supervisor.go` (start/stop/status)

**Acceptance**
- Supervisor starts, registers actor, processes a mailbox message, ack/nack works
- Per-actor concurrency = 1 enforced

**Rollback**
- Turn off `AGENTCTL_ACTOR_SUPERVISOR`

---

### PR4 — Engine interface + DSPy adapter (do NOT drop DSPy yet)
**Scope**
- Create `internal/agent/engine.Engine` abstraction
- Wrap current dspy-go execution behind this interface
- Actors call the engine; mailbox ack/nack stays outside engine

**Files**
- `internal/agent/engine/engine.go` (new)
- `internal/agent/engine/dspy_engine.go` (new)
- Minimal refactor of existing `internal/agent/daemon` handlers to use Engine (or keep daemon separate; actor runtime uses Engine)

**Acceptance**
- Engine can run the same task prompt + tools as before
- Tool telemetry and CAS offload still works

**Rollback**
- Actor runtime can still be disabled; daemon keeps running

---

### PR5 — Hooks v1 in the actor runtime loop (PreToolUse/PostToolUse/StopRequested)
**Scope**
- Actor loop calls dispatcher at canonical points:
  - MessageReceived
  - PreToolUse / PostToolUse
  - StopRequested
  - PostAgentTurn (if needed)
- For now, dispatcher can be configured to do nothing

**Files**
- `internal/actor/actors/*` (where loop lives)
- `internal/hooks/dispatcher/*` integration

**Acceptance**
- A hook can block a tool call (PreToolUse)
- A hook can inject context to be used next turn (Action or Inbox write)
- StopRequested hook can block stopping and force another iteration (for engines that support iterative loop)

**Rollback**
- Keep dispatcher enabled but with empty config; or gate with `AGENTCTL_HOOKS_V1`

---

### PR6 — Unified session lineage plumbing (env + schema + message tagging)
**Scope**
- sessions.db: add lineage columns + session_edges
- mailbox: add session_id/agent_id/workspace_id columns; populate on send
- runner: export `AGENTCTL_SESSION_ID/WORKSPACE/AGENT_ID` env vars to skills
- hooks: dispatcher always includes these in hook.Input

**Files**
- `internal/storage/sessions/*` migrations
- `internal/storage/mailbox/*` migrations and Send/Poll structs
- `internal/adapters/skillslib/runner/*` env propagation (or wherever runner lives)
- `configs/hooks/session-identity.sh` can become simpler later, but keep it for compatibility

**Acceptance**
- Every processed message can be traced to session_id/agent_id/workspace_id
- Resume/fork edges can be created and queried

**Rollback**
- Feature-flag lineage writes; keep old columns nullable

---

### PR7 — Progressive memory for actors (L0/L1/L2) behind a flag
**Scope**
- Add actor_turns + actor_memory_state tables to sessions.db
- Implement cursor-based compaction and redaction
- Actor prompt builder uses:
  - injected context inbox (highest priority)
  - L2/L1/L0 tiers
  - retrieved context

**Files**
- `internal/actor/memory/*` (implementation)
- `internal/storage/sessions/*` (tables/migrations)
- `internal/actor/actors/*` (wire into context builder)

**Acceptance**
- Crash safety: kill during summarize → restart → cursor resumes deterministically
- Redaction applied to summaries
- Token estimator enforces budget threshold & triggers ContextBudgetExceeded hook

**Rollback**
- Keep `AGENTCTL_PROGRESSIVE_MEMORY=0` default; actor still works without it

---

### PR8 — Cutover to Supervisor for one real role (e.g., overseer or coder)
**Scope**
- Choose one actor role and route its mailbox namespace through supervisor in production mode
- Keep old daemon path as fallback

**Files**
- `internal/actor/actors/dspy_actor.go` (or equivalent)
- `cmd/agentctl/cmd/daemon.go` adjust startup instructions

**Acceptance**
- End-to-end message flow works with real tool calls
- Observability events emitted; lease semantics preserved

**Rollback**
- Flip flags off and run old daemon

---

### PR9+ — Post-spine alignment (search/graph/summarization)
After runtime spine is stable:
- refactor `code/semantic_search` to `internal/retrieval`
- enable unified graph ingestion and pagerank
- unify summarization for tasks/memories/digests

---

## 6. Decision: drop DSPy now vs later

**Recommendation (migration-safe):**
- Keep dspy-go as `Engine` implementation **during the runtime migration**.
- Move all queue/event/hook control into the actor runtime.
- Once stable, implement a second engine (`llmchat` tool-calling loop) and switch per-role.

**Why**
- You get immediate event-bus/queue control without a risky rewrite.
- DSPy becomes replaceable; deletion becomes a small PR, not a flag day.

---

## 7. Rollback strategy (must be possible at every phase)

- Supervisor can be disabled at startup; mailbox remains durable
- Hooks v1 can be disabled; hooks fall back to current behavior
- Session lineage columns are additive; nullable; no destructive migration
- Progressive memory is gated by flag; raw turns remain durable regardless

Rollback trigger examples:
- Ack/Nack semantics regressions
- Increased duplicate processing beyond expected at-least-once behavior
- Hooks begin blocking unexpectedly (misconfig) → disable hooks v1 flag

---

## 8. Definition of Done (migration complete)

- Supervisor is default path for actor execution
- Hooks v1 is default and covers the canonical events
- Session lineage is propagated end-to-end and queryable
- Progressive memory is enabled for long-running actors and proven crash-safe
- Legacy poll daemons and legacy hook scripts are deprecated and removed or kept as “unsupported compatibility”

