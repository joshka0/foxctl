```md
---
title: Agentctl Actor Architecture Spec v1
status: draft
owners: []
last_updated: 2026-01-08
---

# Agentctl Actor Architecture Spec v1

This document is the **canonical** top-level spec for `agentctl`’s always-on, multi-actor, event-driven agent runtime. It defines what the system **is**, what it **guarantees**, and the **non-negotiable contracts** that all implementations and refactors must preserve.

It intentionally avoids an implementation plan. This spec is the target that future PRs should be judged against.

---

## Goals

1. **Always-on, multi-actor runtime**: one Supervisor process manages many Actors (per workspace, per role).
2. **Event-driven correctness**: durable queues drive work; no “best-effort” in-memory buffering.
3. **Hook-governed execution**: hooks can block/mutate/enqueue/inject/rewrite at canonical events.
4. **Deterministic, bounded context**: prompt assembly is deterministic, budgeted, and includes injected context first.
5. **Crash-safe progressive memory**: turns are durable; summarization is cursor-based and resumable.
6. **Multi-agent safety**: edits are safe by construction (reservations enforced by tools).
7. **Stable identity + lineage**: session/workspace/agent identity is explicit and propagated through tools/hooks.
8. **Provider-agnostic LLM loop**: swap LLM providers without changing actor semantics.

---

## Non-goals (v1)

- Distributed supervisors or external brokers (Redis/Kafka/etc.).
- Per-actor concurrency > 1 (v1 is sequential per actor).
- Background “magic” work without a durable trigger (unless explicitly scheduled).
- Replacing all existing skills at once (migration must be incremental).

---

## Glossary

- **Supervisor**: long-running process managing actor lifecycle, routing, preemption, and “one in-flight turn per actor”.
- **Actor**: single-threaded executor bound to a workspace + identity, consuming durable messages.
- **Engine**: the LLM + tool-calling loop inside an actor, governed by hooks.
- **Mailbox**: durable queue (SQLite) with lease/ack/nack semantics.
- **Blackboard**: durable coordination store (records, leases, reservations, inbox surfacing).
- **Hook**: a skill invoked at a canonical event; may block/mutate/inject/act/rewrite.
- **CAS**: content-addressable storage for large payloads and artifacts.
- **Context Inbox**: durable “inject next turn” channel for high-priority context.

---

## System Model (topology)

```

```
             ┌──────────────────────────────┐
             │           Supervisor          │
             │  - actor lifecycle            │
             │  - routing + preemption       │
             │  - one in-flight turn/actor   │
             └───────────┬──────────────────┘
                         │
        wakeup/poll/claim│
                         ▼
```

┌───────────────────────────────────────────────────────────┐
│                       Durable Stores                        │
│  mailbox.db  sessions.db  tasks.db  memory.db  graph.db  cas │
└───────────────────────────────────────────────────────────┘
▲
│ claim msg
│
┌───────────┴──────────────────┐
│             Actor             │  (single-threaded)
│  - Engine (LLM loop)          │
│  - HookDispatcher             │
│  - ToolRegistry              │
│  - ContextManager            │
└──────────────────────────────┘

````

---

## Hard Contracts (MUST)

### 1) Delivery + Queue Semantics (Mailbox)
- **At-least-once delivery**: messages may be delivered multiple times.
- **Leased claims**: messages are *claimed* with a visibility timeout; lease expiry is the retry mechanism.
- **Ack/Nack**:
  - `Ack` removes message from queue.
  - `Nack` requeues with visibility delay (backoff).
- **No in-memory buffering as source of truth**: SQLite queue is authoritative.

### 2) Concurrency
- **Per-actor concurrency = 1**: Supervisor MUST NOT run more than one in-flight turn per actor.
- Supervisor MAY manage many actors concurrently.

### 3) Hook Governance
- Hooks MUST be invokable at canonical events (see Events section).
- Hook outputs MUST be aggregated deterministically (see Hook Contract).

### 4) Context + Memory
- **Raw turns are durable**: turns are persisted before summarization decisions.
- **Summarization is cursor-based and crash-safe** (resumable, monotonic indexing).
- **Injected context appears first** in the next LLM request.

### 5) Multi-agent editing safety
- **All write/edit tools MUST enforce reservations**, not merely recommend them.
- A PreToolUse hook MAY auto-reserve, but the tool layer MUST enforce the rule regardless of hook config.

### 6) Identity propagation
- `workspace_id`, `agent_id`, and `session_id` MUST be available to hooks/tools (env + structured inputs).
- Session lineage edges MUST be recorded for continues/forks.

---

## Canonical Events (v1)

These are the only canonical hook attach points in v1. Names are stable.

1. `SessionStart`
2. `MessageReceived`
3. `UserPromptSubmit`
4. `LLMRequest`
5. `LLMResponse`
6. `PreToolUse`
7. `PostToolUse`
8. `StopRequested`
9. `PostAgentTurn`
10. `ContextBudgetExceeded`
11. `SessionEnd`

### Event semantics (high-level)
- **MessageReceived**: a mailbox message was claimed for an actor turn.
- **LLMRequest/LLMResponse**: boundaries around calling the LLM.
- **PreToolUse/PostToolUse**: per tool call inside the engine loop.
- **StopRequested**: engine is about to stop; hooks may block and force another iteration.
- **PostAgentTurn**: final chance to rewrite/redact/truncate the assistant output.
- **ContextBudgetExceeded**: prompt builder exceeded budget threshold; hooks can prune/inject/trigger distillation.

---

## Hook Contract (skills-as-hooks)

### Hook configuration
Hooks are configured from (in this precedence order):
1. `<workspace>/.agentctl/hooks.yaml`
2. `~/.agentctl/hooks.yaml`

Each mapping is: **event + optional matcher → ordered list of hook skills**.

### Hook input (minimum required fields)
Hooks receive a `hook.Input` JSON object. v1 MUST include at least:

- `event` (canonical event name)
- `workspace_root` / `workspace_id`
- `session_id`
- `actor_id`
- `tool_name` + `tool_input` (for Pre/PostToolUse)
- `assistant_text` (for PostAgentTurn / LLMResponse)
- `prompt` (for LLMRequest / UserPromptSubmit)
- `token_estimate` (for budget events)
- `correlation_id` / `turn_id` (for traceability)

### Hook output
Hooks return an envelope containing `data.hook_output`:

```json
{
  "decision": "approve|block|none",
  "reason": "...",
  "context": "text to inject (optional)",
  "updated_tool_input": { /* optional */ },
  "updated_assistant_text": "optional",
  "actions": [ /* optional */ ],
  "meta": { /* optional */ }
}
````

#### Action types (v1 only)

Hooks may emit actions. The set is closed in v1:

* `run_skill`: `{ "skill": "...", "args": {...} }`
* `inject_context`: `{ "text": "...", "priority": 0-100 }`
* `send_mailbox`: `{ "to_ns": "...", "message_type": "...", "payload": {...}, "headers": {...} }`
* `bb_post`: `{ "topic": "...", "payload": {...} }`
* `bb_claim`: `{ "topic": "...", "record_id": "..." }`

### Deterministic aggregation rules (MUST)

Given multiple hooks for the same event:

* If any hook returns `decision=block`, the event is blocked (tool not run / stop denied).
* `updated_tool_input`: last-wins (order in config).
* `updated_assistant_text`: last-wins (order in config).
* `context`: join non-empty strings with `\n\n` (order in config).
* `actions`: concatenated in order (including any `inject_context` actions).
* `actions` are executed in the order hooks ran (stable).

---

## Tool System (canonical + enforced)

### Tool naming (v1 canonical)

Tool names are **lowercase dot-separated** and stable:

* `fs.read_file`, `fs.list_dir`
* `code.search`, `code.symbol_search`, `code.swe_grep`, `code.semantic_search`
* `edit.create_file`, `edit.apply_patch`, `edit.apply_structured_diff`
* `tests.run`
* `todo.*`
* `mail.*`
* `bb.*`
* `graph.*`
* `artifact.read` (required)

### Tool result size rule (MUST)

If a tool result exceeds `MaxToolResultBytes`, the runtime MUST:

1. Store the full output in CAS
2. Replace the observation with a small summary payload referencing `artifact.read`

Example observation shape:

```json
{
  "summary": "Tool output was large and stored as artifact",
  "artifact": { "digest": "sha256:...", "hint": "use artifact.read" },
  "preview": "first N bytes..."
}
```

Hooks may further rewrite/summarize the observation in `PostToolUse`.

### Reservations (MUST enforce at tool layer)

All `edit.*` tools MUST fail unless:

* the actor holds a valid reservation for the target path, OR
* the tool is explicitly configured for a safe “no-reservation” mode in tests only (not default).

Hooks (e.g., file_guard) may auto-reserve in `PreToolUse`, but enforcement is not optional.

### Path safety (MUST)

All filesystem access MUST go through a path validator anchored to workspace + allowed roots.

---

## Preemption Policy (interruptive messages)

### Interruptive by default

Messages from:

* the actor’s parent, or
* human namespaces (`actor:human:*`, `cli:*`)

are **interruptive by default**, unless message header includes:

* `delivery=next_turn` (exact string)

### Behavior

If actor is busy and an interruptive message arrives, Supervisor MUST:

1. cancel the in-flight engine context
2. enqueue the interruptive message at the front of the actor’s queue
3. ensure only one in-flight turn continues

---

## Context Management (prompt builder)

### Deterministic prompt order (MUST)

Before each LLM request, the prompt is assembled in this exact order:

1. System prompt + role
2. **Injected Context Inbox** (newest first)
3. L2 distilled summary (if exists)
4. L1 recent summaries (if exists)
5. L0 recent raw turns (bounded)
6. Retrieved context (semantic search + graph neighbors; bounded)
7. Current message + scratchpad

### Budgeting

* Target budget is configurable (e.g., 50k tokens).
* Estimator (v1): `len(text)/4` with safety margin.
* If estimated prompt > `0.8 * budget`, emit `ContextBudgetExceeded` hooks and rebuild.

### Progressive memory tiers (from actor-progressive-memory.md)

* L0 raw turns (durable)
* L1 summaries (cursor-based)
* L2 distilled summary (cursor-based)
* Summaries are redacted; raw may contain secrets (handled via CAS policies / TTL).

---

## Session Identity + Lineage

### Required identity fields

All runtime actions MUST be scoped by:

* `workspace_id`
* `agent_id`
* `session_id`

### Environment propagation (minimum)

Runners and sub-process tool invocations MUST propagate:

* `AGENTCTL_WORKSPACE`
* `AGENTCTL_SESSION_ID`
* `AGENTCTL_AGENT_ID`

### Lineage edges

Sessions MUST support:

* `continues`
* `forked_from`
* `relates_to` (optional/weak)

Identity file fallback MAY exist (e.g., `~/.agentctl/sessions/active/<workspace_hash>-<agent_id>.json`) for environments that can’t pass env vars reliably.

---

## Observability

### Event bus

* In-memory pub/sub is **ephemeral** and may drop events.
* Important events MUST be persisted (trajectory/audit), including:

  * `mail.received`, `mail.acked`, `mail.expired`
  * `agent.started`, `agent.stopped`, `agent.error`
  * `task.completed` (and other high-value lifecycle transitions)

### Correlation

All events/messages SHOULD include a `correlation_id` / `trace_id` to connect:

* mailbox message → actor turn → tool calls → hooks → artifacts

---

## Security & Safety

* **Secret scanning**: write/edit operations MUST block obvious secrets by default (configurable patterns).
* **Redaction before persistence**: summaries/L1/L2 artifacts MUST be redacted.
* **Allowlists**: agents MAY be constrained to a tool/skill allowlist; enforcement must happen in tool registry.
* **Least privilege**: default filesystem policy is workspace-only unless explicitly expanded.

---

## Failure Handling

* Tool failures:

  * surface as structured errors
  * PostToolUse hooks may add remediation context
* Message processing failures:

  * Nack with backoff
  * retry is expected under at-least-once delivery
* Dead-letter / escalation:

  * repeated failures SHOULD escalate to overseer/human via mailbox

---

## Compatibility stance on DSPy (v1)

DSPy MAY exist as:

* an offline trainer/export format (`session/export-dspy`)
* a bounded skill helper for specific tasks

DSPy MUST NOT be the owner of:

* mailbox lease semantics
* hook event boundaries
* stop gating
* preemption
* deterministic prompt ordering

The actor runtime loop is owned by the Supervisor + Engine as defined here.

---

## Acceptance Criteria (v1)

The system meets this spec when:

1. Supervisor runs multiple actors with **one in-flight turn per actor**.
2. Message processing obeys lease/ack/nack semantics under crash/restart.
3. Hooks can deterministically block/mutate/inject/rewrite at canonical events.
4. StopRequested hooks can deny stop and force continued iterations.
5. Injected context appears first in the next LLM request.
6. Progressive memory is cursor-based, crash-safe, and keeps raw turns durable.
7. Edit tools enforce reservations regardless of hook config.
8. Session/workspace/agent identity is consistently propagated across tools/hooks.
9. Large tool outputs are CAS-offloaded with `artifact.read` access.
