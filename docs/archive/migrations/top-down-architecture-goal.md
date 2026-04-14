Here’s the **top-down architecture goal** we’re converging on — independent of implementation details — framed as “what the system *is*” and “what it *guarantees*”.

---

## What we’re trying to achieve

Build `foxctl` into an **always-on, multi-actor, event-driven agent system** where:

* **Work is driven by durable queues** (SQLite mailbox/blackboard), not in-memory loops.
* A single **Supervisor** process runs many **Actors** (per workspace / per role).
* Each Actor is a **deterministic, single-threaded executor**:

  * consumes messages/events
  * builds a bounded prompt from durable state
  * runs tool loops under hook governance
  * persists turns + memory state
  * stops only when Stop is **accepted** (not when the LLM “feels done”)

This gives you **control over the event bus and queue semantics**, which is the real reason to drop DSPy from the runtime loop.

---

## The “north star” user experience

### For a human (or OpenCode/Claude Code)

* You can message an actor (`overseer`, `coder`, `reviewer`, etc.) and **it reacts quickly** (wake-up, claim, process).
* If the session compacts/restarts, the system **restores continuity** (session lineage + anchor + todos + gotchas).
* When you say “stop”, the system only stops if the **Stop guard** agrees the DoD is met.

### For multiple agents

* They can collaborate without stepping on each other:

  * edits are safe (reservations)
  * coordination is explicit (mail/blackboard)
  * interruptions/preemption behave predictably

---

## Core invariants (what the architecture must guarantee)

### 1) Delivery + concurrency correctness (from `reactive-actor-system.md`)

* **At-least-once delivery**: messages may repeat; handlers are idempotent.
* **Leased claims**: claim/ack/nack semantics are durable; lease expiry is the retry mechanism.
* **Per-actor concurrency = 1 (MVP)**: supervisor only claims when actor is idle.
* **SQLite is the queue**; memory is not the source of truth.

### 2) Hook governance is first-class

Hooks aren’t “advisory scripts”; they are a **policy/runtime layer** that can:

* block
* mutate tool args
* inject context (next turn, and injected context appears first)
* run actions (send mail, post bb, run skill, etc.)
* rewrite assistant output at end of turn

### 3) Memory is durable + progressive (from `actor-progressive-memory.md`)

* Every turn is persisted before summarization.
* Summarization is cursor-based and crash-safe (L0→L1→L2).
* Summaries are redacted; large artifacts go to CAS.
* Prompt assembly is deterministic and budgeted.

### 4) Identity and lineage are stable (from `unified-session-lineage.md`)

* Every action is scoped by:

  * `workspace_id`
  * `agent_id`
  * `session_id`
* Session chains exist (`continues`, `forked_from`, etc.) so continuity and scoping are reliable across compactions/tools/platforms.

### 5) Multi-agent safety is enforced, not optional

* `edit.*` must require valid reservations.
* Guards (task/file/security) are enforceable rules, not “best effort.”

---

## System shape (components and responsibilities)

### Control plane: Supervisor + event bus

**Supervisor** is the owner of:

* actor lifecycle (start/stop/restart)
* “one in-flight turn per actor”
* preemption rules for interruptive messages
* mailbox wakeups → poll/claim → dispatch

Event bus is:

* **ephemeral pub/sub** for low latency fanout
* **selective persistence** for important events (trajectory)

### Data plane: durable stores

* **mailbox.db**: durable queue with leases + notify table
* **blackboard**: coordination records + reservations + inbox
* **sessions.db**: session lineage + turns + summaries
* **tasks.db**: todos + task graph + status transitions
* **memory store + embeddings**: searchable knowledge
* **graph.db**: unified dependency graph + pagerank
* **CAS**: large payload storage with paging access

### Execution plane: Actor engine

Each Actor runs:

* a **prompt builder** (budgeted, deterministic order)
* an **LLM adapter** (provider-agnostic)
* a **tool registry** (canonical tool names; structured I/O)
* a **hook dispatcher** (canonical events)
* a **stop gate** (StopRequested must be accepted)

---

## The canonical turn lifecycle we’re aiming for

1. **MessageReceived** (durable mailbox claim)
2. Build context in fixed order:

   1. System
   2. **Injected Context inbox** (newest first)
   3. L2 distilled
   4. L1 summaries
   5. L0 recent turns
   6. Retrieval (semantic search + graph neighbors)
   7. Current message
3. **LLMRequest / LLMResponse**
4. For each tool call:

   * **PreToolUse hooks** (may block/mutate/auto-reserve)
   * run tool (enforce max output + CAS pointerization)
   * **PostToolUse hooks** (may inject context/actions)
5. When LLM returns “final”:

   * **StopRequested hooks** decide if stop is allowed
   * if blocked → inject context/actions and continue loop
6. **PostAgentTurn** hooks may rewrite output
7. Persist turns → trigger progressive memory compaction if needed
8. Ack mailbox message

---

## What “dropping DSPy” means at the architecture level

Not “no DSPy anywhere”, but:

* **DSPy is not the runtime loop owner** (no core agent execution inside DSPy)
* The runtime loop is owned by your Engine + events + hooks + queue semantics
* DSPy (if kept) becomes **a skill-level helper** for bounded workflows (optional)

This aligns with your need for:

* strict lease semantics
* deterministic hook points
* stop gating
* preemption
* uniform tool contracts

---

## What success looks like (high-level acceptance)

* One supervisor process can run many actors with **predictable concurrency**.
* Message delivery is correct under crashes/timeouts (lease + retry).
* Hooks can enforce real behavior (task guard, file guard, stop guard).
* Context continuity works across compactions and tool platforms.
* Edits are safe in multi-agent mode (reservations enforced).
* Search + graph + memory behave as a unified “relevance layer” for the actor.

---
