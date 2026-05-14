Here’s what the **“Manus-style” GUI phase** looks like for *your* system: one place to **spawn/inspect agents, chat with them, watch tool/hook pipelines, see tasks/queues/locks**, and inject interrupts.

I’m going to describe it as a **single process** (because you want a single supervisor managing all actors), and a **web UI** that talks to it over HTTP + streaming.

---

# Runtime: how you actually run it

## One command starts everything

Add a command like:

```bash
foxctl gui --workspace /path/to/repo --addr 127.0.0.1:8080
```

This single process hosts:

1. **Supervisor** (the multi-actor runtime)
2. **Mailbox watcher** + **blackboard watcher** (wake-ups)
3. **HTTP API** for control/inspection
4. **SSE event stream** for live updates
5. **Static web UI** served from the same binary (embedded build)

So the UI is not a separate “app”—it’s a window into the supervisor.

---

# Architecture: how the GUI talks to the system

## Control plane = HTTP API

The UI uses normal HTTP endpoints to:

* spawn/register actors
* send messages (ask/cmd/event) with “interrupt now” vs “next turn”
* manage tasks / dependencies
* inspect state (actors, mailboxes, blackboard, reservations, turns)

## Observation plane = SSE stream

The UI opens one long-lived stream:

`GET /api/stream?workspace_id=…`

The server pushes events such as:

* `actor.state_changed`
* `mailbox.message_received`
* `engine.llm_request` / `engine.llm_response`
* `engine.tool_called` / `engine.tool_result`
* `hook.triggered` / `hook.blocked` / `hook.injected_context`
* `reservation.acquired` / `reservation.conflict`
* `task.updated` / `task.dependency_changed`
* `bb.posted` / `bb.claimed`

This is what gives you the “alive, observable” Manus feel.

**Important detail:** the SSE stream is *ephemeral* (like your EventBus), but the UI can always “catch up” by querying durable state (actor_turns, tasks.db, mailbox tables) on reconnect.

---

# The GUI experience (what you see)

## 1) Workspace dashboard

* Supervisor status (running, actors count)
* Queue depth overview:

  * mailbox pending per actor
  * blackboard items by topic
* “Active locks” panel (reservations)
* “Hot tasks” panel (ordered by PageRank / blockers)

## 2) Actor list + hierarchy

* Actors grouped by:

  * role (coder/planner/reviewer)
  * parent/child
  * state (idle / processing / blocked / error)
* Buttons:

  * **Spawn actor**
  * **Stop actor**
  * **Restart actor** (supervisor directive)
  * **Attach console/chat** (opens the actor view)

## 3) Actor “Console” view (Manus-style)

This is the primary interaction screen.

### Left: Chat + control

* A chat box to send:

  * `agent.ask` (normal question)
  * `agent.cmd` (structured command)
  * `agent.event` (context injection, notifications)
* Send buttons:

  * **Interrupt now** (preempt)
  * **Queue next turn** (non-preempt, sets header `delivery=next_turn`)
* “Stop run” / “Cancel current run”
* “Run until idle” / “Run continuously” toggle

### Center: Timeline (the gold)

A live timeline of the run:

* LLM request node
* tool call nodes (expandable)
* hook nodes (PreToolUse/PostToolUse/StopRequested/PostAgentTurn)
* stop accepted/blocked nodes

Clicking a node shows:

* inputs/outputs
* hook mutations
* injected context
* token estimate / budget warnings

This is where you “observe the process”.

### Right: Context inspector

Shows the **exact prompt blocks** that will be sent on the next request:

* Injected ContextInbox (always first)
* task brief + updates
* L2/L1/L0 memory blocks
* retrieved context (semantic search / graph neighbors)
* current user message

Each block shows:

* token estimate
* source (mailbox/blackboard/hook/task/sessions)
* pin/unpin
* “drop from working set” (does not delete durable storage)

This makes context management visible and debuggable.

## 4) Tasks view

* Task list ordered by:

  * PageRank score
  * blockers
  * “recently updated”
* Task detail:

  * brief
  * updates stream (append-only)
  * dependencies graph
  * assigned actor(s)
  * “notify downstream” actions

## 5) Blackboard view

* Topics list (task.update, work.queue, etc.)
* Claim/release actions (lease semantics)
* Filter to items relevant to a task/actor

## 6) Locks/reservations view

* Which files are reserved, by who, why, expiry
* Conflicts
* “Request release” button (sends a message to the holder)

---

# How agent-building fits in

“Agent building” in this UI isn’t training a model; it’s **configuring a running actor**:

* role
* provider/model (BYOK)
* allowed tools / skillpack
* policies (write requires reservation, etc.)
* hook chain config (per event)
* memory config thresholds

The GUI exposes this as an **Actor Template**:

* “Create template”
* “Spawn from template”
* “Apply template to actor”
* “Diff templates”

That’s the Manus-like “agent studio” part.

---

# What has to exist in code to support this GUI (concrete)

## A) A single Go HTTP server inside foxctl

Create `cmd/foxctl/cmd/gui.go`:

* starts supervisor + watcher + API + SSE
* serves embedded UI assets

## B) API endpoints (minimum set)

* `GET /api/state` (workspace + supervisor status)
* `GET /api/actors`
* `POST /api/actors` (spawn/register)
* `GET /api/actors/{id}`
* `POST /api/actors/{id}/send` (ask/cmd/event; interrupt flag)
* `POST /api/actors/{id}/cancel`
* `GET /api/actors/{id}/turns?since=...`
* `GET /api/actors/{id}/context` (next prompt blocks, token estimates)
* `GET /api/tasks` / `POST /api/tasks` / `PATCH /api/tasks/{id}`
* `GET /api/blackboard/topics` / `GET /api/blackboard/{topic}`
* `GET /api/reservations`
* `GET /api/artifacts/{digest}?page=…` (backs `artifact.read`)

## C) SSE events (minimum set)

* actor lifecycle/state
* mailbox receive/ack/nack
* engine llm request/response
* tool call/result
* hook triggered/blocked/injected/mutated
* reservation acquired/conflict/released
* task updated/deps changed
* blackboard posted/claimed/released

---

# Why this will feel like Manus (but for your system)

Because you’ll have:

* live “agent console”
* a visible execution trace (LLM ↔ tools ↔ hooks)
* inspector panels for context + memory + locks
* the ability to inject interrupts and watch behavior change immediately
* multi-agent state visible in one place

---

If you want, I can sketch the **UI layout** as a concrete component tree (React pages + panels) and the **SSE event payload schema** so the frontend can be implemented cleanly with minimal back-and-forth.
