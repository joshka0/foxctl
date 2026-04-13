
Below is a **migration outline** that treats your “target architecture” as the union of:

* **Reactive Actor System** (Supervisor + MailboxWatcher + leased mailbox queue + ephemeral EventBus + persisted trajectory events)
* **Hook v1 contract** (`hook.Input` / `hook.Output` + deterministic dispatcher semantics)
* **Unified Session Lineage** (session IDs, parent edges, agent identity, env propagation)
* **Actor Progressive Memory** (L0/L1/L2, cursor-based compaction, redaction)
* (Then, once the runtime spine is stable) **Unified search/graph/summarization** enhancements

I’m assuming the repo state is “mixed”: you already have a lot of skills and storage, but the **runtime spine** (hooks + actor supervisor + queue semantics + session lineage propagation) needs to be made canonical and then everything else is “pulled into alignment”.

---

## Migration principles

These keep you safe while reshaping the system:

1. **Durability-first**: never break the mailbox lease/ack semantics and never lose raw session turns.
2. **Additive migrations**: schema changes should be additive + backfilled; keep old readers/writers working until cutover.
3. **Compatibility shims**: keep shell hooks and existing skills working while you introduce Go-native hook skills.
4. **Feature-flag cutovers**: turn on new runtime pieces per-workspace/per-agent to avoid “flag day”.
5. **One canonical contract**: hook dispatcher speaks only `hook.Input`/`hook.Output` (v1), everything else adapts to that.

---

## Phase map at a glance

### Phase 0 — Inventory + “contract freeze”

* Freeze v1 runtime contracts:

  * `hook.Input` / `hook.Output`
  * mailbox claim/lease/ack behavior
  * event schema (persisted vs ephemeral)
* Add feature flags:

  * `AGENTCTL_ACTOR_SUPERVISOR=0/1`
  * `AGENTCTL_HOOKS_V1=0/1`
  * `AGENTCTL_SESSION_LINEAGE=0/1`
  * `AGENTCTL_PROGRESSIVE_MEMORY=0/1`

### Phase 1 — Hooks v1: dispatcher + compatibility layer

* Implement Go-native dispatcher semantics (block wins, last-wins overrides, ordered actions).
* Wrap existing hooks (shell + skills) behind the v1 contract.

### Phase 2 — Mailbox → reactive wakeups (no behavior change)

* Add `mailbox_notify` + trigger + watcher.
* Keep `Poll()` + lease semantics as the source of truth; watcher only wakes.

### Phase 3 — Supervisor + Actor runtime spine (minimal actors first)

* Start/stop supervisor process; register actors; route mailbox messages to actors.
* First actor can be a “passthrough” that runs *existing* skill handlers.
* Keep dspy-go agents **as an implementation detail** behind an `AgentEngine` interface.

### Phase 4 — Session lineage propagation (env + schema + mailbox tagging)

* Migrate sessions schema to include parent/edges/agent_id/status.
* Ensure every mailbox message and every hook invocation carries `session_id`, `agent_id`, `workspace_id`.

### Phase 5 — Progressive memory inside actors

* Add L0/L1/L2 state + cursor-based summarization (crash safe).
* Wire it into `Actor.OnMailReceived` context building.

### Phase 6 — Pull the “higher layers” into alignment

* Unified semantic search refactor (`internal/intelligence/retrieval`)
* Unified dependency graph + pagerank integration
* Unified summarization & compaction (sessions/tasks/memories/digests)

### Phase 7 — Cutover + deprecation

* Default-enable supervisor + hooks v1.
* Deprecate old poll daemons and ad-hoc hook paths.
* Optional: decide whether to keep or remove DSPy once runtime spine is stable.

---

## What needs to move/reshape (component-by-component)

### 1) Hooks: from ad-hoc scripts → v1 contract + deterministic dispatcher

**Current (likely):**

* Shell scripts in `.claude/hooks/*` and/or skills that return minimal `{"decision": ...}`
* Inconsistent payload shapes per hook

**Target:**

* Hook dispatcher always sends `hook.Input`
* Hook skill always returns `hook.Output` in `data.hook_output`
* Dispatcher runs actions deterministically

**Migration steps**

1. **Introduce a Go dispatcher package**

   * `internal/hook/dispatcher`:

     * execute configured hooks for an event
     * collect outputs
     * apply merge rules:

       * `block` wins
       * `updated_tool_input` last-wins
       * `updated_assistant_text` last-wins
       * actions concatenated in hook order

2. **Compatibility adapters**

   * Adapter A: **shell hook output adapter**

     * If a hook prints `{"decision":"approve"}`, wrap it into `hook.Output{Decision: approve}`
   * Adapter B: **legacy skill output adapter**

     * If an existing Go skill returns an envelope but not `hook_output`, interpret:

       * `data.decision` → map to `hook.Output.Decision`
       * (or default `approve`)

3. **Reshape hook skills**

   * Move “hook logic” into skills under `skills/hooks_*`
   * Each hook skill should:

     * parse `hook.Input`
     * return `hook.Output` only
     * emit actions rather than doing side effects inline (when possible)

4. **Cutover**

   * Flip `AGENTCTL_HOOKS_V1=1` per workspace
   * Keep old hooks runnable through the adapter until removed

**Deliverable checkpoints**

* You can run: `agentctl hooks run --event PreToolUse --input <json>` and get deterministic results.

---

### 2) Mailbox: keep SQLite queue, add reactive wakeups, then add richer routing

**Current:**

* Poll-based consumption with lease/ack (per design)
* Maybe multiple daemons poll independently

**Target:**

* SQLite remains the queue
* Watcher is wake-only
* Supervisor claims messages via `Poll()` and dispatches to actors

**Migration steps**

1. **Schema (additive)**

   * Add `mailbox_notify` table + insert trigger (as in reactive-actor-system.md).
2. **MailboxWatcher**

   * A small loop that polls `mailbox_notify` every ~50ms and emits `WakeUp{namespace}`.
3. **No behavior change yet**

   * Still process via existing poll loop (just faster wakeups).
4. **Add routing metadata (Phase 4 tie-in)**

   * Ensure messages store:

     * `session_id`
     * `agent_id`
     * `workspace_id`
     * optional `correlation_id` / `trace_id`

**Deliverable checkpoints**

* P95 “message-to-start-processing” latency drops without changing queue semantics.

---

### 3) Supervisor + Actors: introduce the runtime spine without rewriting agents

**Key idea:** You don’t need to drop DSPy to gain control of queues/events. You need an **agent engine interface** and make DSPy one implementation.

**Migration steps**

1. **Define the actor runtime interface (thin)**

   * `Actor` already in your design.
   * Add a small `AgentEngine` interface used by `DspyActor`:

     * `Run(ctx, llmContext, payload) -> response`
2. **Build Supervisor**

   * Registry of actors by namespace
   * On wakeup: if actor idle → `Poll()` → `OnMailReceived()`
3. **Start with one “safe actor”**

   * e.g. `actor:runner` that just runs a skill based on message payload
   * This proves the supervisor lifecycle & mailbox semantics.
4. **Wrap DSPy**

   * Put dspy-go behind `AgentEngine`
   * All event bus publishing and mailbox acks happen **outside** DSPy
5. **Gradual adoption**

   * Migrate one agent/role at a time to supervisor-managed execution

**Deliverable checkpoints**

* You can run one supervisor process that manages N actors reliably with at-least-once semantics.

---

### 4) Session lineage: schema + environment propagation + mailbox tagging

This is the most important “reshape” because it becomes the glue for:

* hooks
* memory
* events
* retrieval scoping

**Migration steps**

#### 4.1 sessions.db schema migrations (additive)

Apply `unified-session-lineage.md`:

* Add columns to sessions table:

  * `parent_session_id`, `agent_id`, `status`, `started_at`, `updated_at`
* Create `session_edges` table + indexes

**Backfill**

* Existing rows:

  * `agent_id='agentctl'`
  * `status='ok'` (or infer)
  * timestamps inferred from existing fields if present

#### 4.2 Identity propagation

* Runner must export env:

  * `AGENTCTL_SESSION_ID`
  * `AGENTCTL_WORKSPACE`
  * `AGENTCTL_AGENT_ID`
* Every skill execution inherits these env vars.

#### 4.3 Mailbox schema update

* Add columns to mailbox messages:

  * `session_id`, `agent_id`, `workspace_id`
* Backfill:

  * for existing messages: set `workspace_id` if derivable; otherwise NULL and tolerate

#### 4.4 Hook input population

* Dispatcher populates `hook.Input.SessionID`, `WorkspaceID`, `ActorID`, `CorrelationID`
* Hooks stop scraping these from random places

**Deliverable checkpoints**

* You can trace: mailbox message → hook events → trajectory events → session chain.

---

### 5) Progressive memory in actors (L0/L1/L2) with crash-safe cursors

**Current:**

* You already have session capture/summarize pipelines for Claude Code sessions.
* Actor runtime memory (short-term) may not exist yet.

**Target:**

* Each actor run accumulates L0 turns durably and compacts to L1/L2 with cursor-based compaction, redaction, monotonic indexing.

**Migration steps**

1. **Add `internal/actor/memory/` implementation**

   * As per `actor-progressive-memory.md`
2. **Add `actor_memory_state` table to sessions.db**

   * plus CAS refs to L1/L2 artifacts
3. **Wire into actor loop**

   * On every message:

     * persist raw turns first
     * trigger async summarization pipeline using durable cursor
4. **Redaction**

   * Ensure summaries/learnings are redacted before persistence
5. **Context assembly**

   * Actor context = system + retrieved + L2 + L1 + last N L0 + current

**Deliverable checkpoints**

* You can kill -9 the supervisor mid-summarization and restart; cursor resumes safely with no corruption.

---

### 6) “Higher layers” migrations: search, graph, summarization

Once the runtime spine is stable, pull the rest into alignment.

#### 6.1 Unified semantic search (Phase 3 plan)

* Refactor `code/semantic_search` to use `internal/intelligence/retrieval.Generator`
* Keep sessions search separate but fuse with RRF
* Introduce canonical IDs (`symbol:...`, `session:...`, etc.)

#### 6.2 Unified dependency graph

* Add `graph_nodes`, `graph_edges` tables (likely in memory.db)
* Create `graph/add_edge`, `graph/query`, `graph/pagerank`
* Start ingesting edges from hooks:

  * PostToolUse(Edit/Write) → task→symbol, session→symbol
  * session end → session→task, session→symbol
* Integrate pagerank into semantic_search scoring

#### 6.3 Unified summarization & compaction

* Add task summaries + struct fields to tasks.db
* Add memory compaction threshold and CAS pinning
* Add daily digest entries (named_memory)

**Deliverable checkpoints**

* Semantic search results become graph-aware and session-aware while staying <500ms p95 (with fallbacks).

---

## Concrete migration checklist (ordered, “do this then that”)

### Step 1 — Introduce v1 hook contract everywhere (no runtime change)

* [ ] Add `hook.Input` / `hook.Output` types to `internal/domain/hook` (or wherever canonical)
* [ ] Add dispatcher that can run hooks and merge outputs deterministically
* [ ] Add adapters for legacy hook outputs
* [ ] Update at least one hook skill to return `data.hook_output`

### Step 2 — Add reactive wakeups (no actor rewrite)

* [ ] Create `mailbox_notify` table + trigger
* [ ] Add `MailboxWatcher` that wakes on namespaces
* [ ] Replace long poll sleeps with watcher wakes

### Step 3 — Add supervisor skeleton + one minimal actor

* [ ] Implement supervisor lifecycle + state machine
* [ ] Implement `actor:runner` (or similar) that handles one message type
* [ ] Prove ack/nack + lease expiry correctness under crashes

### Step 4 — Session lineage propagation (schema + env)

* [ ] sessions.db: add lineage columns + `session_edges`
* [ ] runner: export `AGENTCTL_*` env vars to all skills
* [ ] mailbox.db: add `session_id`, `agent_id`, `workspace_id` columns
* [ ] hooks: dispatcher populates these into `hook.Input`

### Step 5 — Convert one DSPy agent into a supervisor-managed actor

* [ ] Wrap dspy-go in `AgentEngine`
* [ ] Actor uses mailbox message payload + context build
* [ ] Event bus emits `mail.received` / `agent.started` / `mail.acked`

### Step 6 — Add progressive memory to that actor

* [ ] `actor_memory_state` schema
* [ ] L0 durable turn writes
* [ ] async L0→L1 cursor compaction
* [ ] L1→L2 distillation
* [ ] redaction before persistence

### Step 7 — Expand to all roles + cut over

* [ ] Migrate remaining agents to supervisor
* [ ] Turn on `AGENTCTL_ACTOR_SUPERVISOR=1` by default
* [ ] Deprecate old poll loops/daemons

### Step 8 — Align search + graph + summaries

* [ ] Refactor semantic_search to `internal/intelligence/retrieval`
* [ ] Introduce graph tables + pagerank
* [ ] Add task/memory compaction summaries + daily digest

---

## DSPy: drop now or later?

If your motivation is “better control over event bus and queues”: **don’t drop DSPy as a prerequisite.** You get control by:

* making queue/leases/acks/event publishing happen in the **actor runtime** (supervisor), not inside the agent framework
* isolating DSPy behind an interface (`AgentEngine`) so replacing it later is easy

A good “migration-safe” stance is:

* **Phase 3–6:** keep DSPy, but treat it as *just one engine*
* **Phase 7+:** once the runtime spine is stable and observable, you can decide to:

  * keep DSPy for optimization + structured prompting
  * or replace it with a smaller in-house loop if you want absolute determinism

This keeps the migration blast radius low.

