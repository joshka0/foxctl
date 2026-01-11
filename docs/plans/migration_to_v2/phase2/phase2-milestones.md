Absolutely — let’s plan the “not now” items in the **same PR-by-PR, unambiguous** style.

Below is a **forward plan** that builds on Milestone 1 (engine/supervisor/LLMActor/hooks/toolkit/actor_turns/context_inbox) and adds:

* Interactive consoles (streaming/cancel)
* Blackboard updates + routing
* Multi-agent coordination via tasks + shared dialogue
* Unified graph + PageRank (overseer scheduling + context injection)
* Progressive memory (L0/L1/L2) for actors
* “Smarter tools” (compound tools that reduce tool spam)
* “Brain-like” learning: preferences + pattern mining + auto-improvement loops

No option A/B; each PR is a concrete decision.

---

# Milestone 2 — Interactive Actor Console

## PR2.1 — Console session registry + mailbox protocol (durable)

**Goal:** Consoles are first-class, attachable, and use mailbox transport only.

### Implement

1. `sessions.db` migration:

* `console_sessions`

  * `console_id TEXT PK`
  * `workspace_id TEXT`
  * `actor_id TEXT`
  * `session_id TEXT` (optional, can match actor run lineage)
  * `created_at TEXT`
  * `last_attached_at TEXT`
  * `meta_json TEXT`

2. Mailbox message payload schema (canonical v1), stored in mailbox `body`:

```jsonc
{
  "type": "ask" | "reply" | "event" | "cmd",
  "actor_id": "...",
  "console_id": "...",
  "correlation_id": "...",
  "content": "...",
  "metadata": { "partial": true, "mime": "text/markdown", "progress": {...} },
  "cmd": { "name": "cancel" }
}
```

3. Add toolkit helpers:

* `console.send_event(console_id, correlation_id, text, partial=true)`
* `console.send_reply(console_id, correlation_id, text)`

### Done when

* `agentctl actor console --actor <id>` creates/attaches a `console_sessions` row.
* A console “ask” message can be delivered to the actor and a “reply” comes back through mailbox.

---

## PR2.2 — agentctl_viewer: tabs + streaming renderer + cancel

**Goal:** You can interact with multiple actors live, and cancel mid-turn.

### Implement

1. `cmd/agentctl_viewer/`:

* Tabs by `console_id`
* Render `event` chunks streaming
* Enter sends `ask`
* Ctrl+C sends `cmd.cancel` for active `correlation_id`

2. Backpressure rule (hard):

* Default `max_inflight_correlations_per_console = 1`
* Viewer will not send a new ask while one is running (unless `--force`)

### Done when

* You can watch “event” streaming updates (partial chunks) and receive final reply.
* Cancel interrupts an in-progress turn and the actor stops work.

---

## PR2.3 — Actor streaming integration (engine → console)

**Goal:** While an actor is working, it can emit progress without waiting for stop.

### Implement

1. New engine hook event: `AgentProgress`

* Emitted after each tool call and after each iteration
* Hook dispatcher can route these to console events (or ignore)

2. LLMActor behavior:

* If the inbound mailbox message has `console_id`, stream:

  * `event`: “thinking/progress summary” after each tool
  * `reply`: final assistant output on stop

### Done when

* Long running tasks show progress in viewer without spamming the LLM prompt.

---

## PR2.4 — Trajectory logging for console sessions

**Goal:** Full observability of console asks/events/replies/cancels.

### Implement

* Persist these event types into `trajectory.db`:

  * `console.ask`, `console.event`, `console.reply`, `console.cancel`
* Include: `actor_id`, `console_id`, `correlation_id`, `run_id`

### Done when

* `agentctl trajectory tail --actor <id>` shows console interactions.

---

# Milestone 3 — Blackboard Updates + Task-Linked Multi-Agent Interaction

This milestone makes your “big brain” coordination real: **tasks become the shared substrate** and agents see each other through task-linked dialogue and artifacts.

## PR3.1 — Task binding to turns (task_id on actor_turns)

**Goal:** Every turn can be attributed to an active task (or explicitly none).

### Implement

1. `sessions.db` migration:

* Add `task_id TEXT` nullable on `actor_turns`
* Add index `(workspace_id, task_id, created_at)`

2. Toolkit rule (hard):

* `todo.ensure_active` is called at the start of every `RunUntilStop` **unless** message is marked `no_task=true`

3. Hook event: `TaskBinding`

* Emitted at turn start with `{actor_id, run_id, task_id}`

### Done when

* You can query “show me all dialogue for task X” and it is complete and ordered.

---

## PR3.2 — Blackboard event stream + persistence

**Goal:** Blackboard posts become durable and routable events.

### Implement

1. Define blackboard events (canonical v1):

* `bb.posted`
* `bb.updated`
* `bb.claimed`
* `bb.released`

2. When `bb.post` tool is called:

* Persist post (existing blackboard store)
* Emit EventBus event `bb.posted`
* Append a compact entry into `actor_turns` (role=`tool`, tool=`bb.post`) with `task_id` if present

### Done when

* Posting to bb always creates an event and a durable trace.

---

## PR3.3 — Context router hook: bb/task → actor_context_inbox

**Goal:** Context injection becomes the mechanism for multi-agent awareness.

### Implement

1. New built-in hook skill: `hooks_context_router`

* Trigger on: `bb.posted`, `task.updated`, `agent.turn.start`
* Input: event payload + workspace graph hints (if available)
* Output actions:

  * `inject_context` to specific actors
  * `send_mailbox` for interrupt messages

2. Routing rules (hard, deterministic):

* If bb post has `task_id`, deliver to:

  * actors currently working on that task (or its dependencies once graph exists in Milestone 4)
* If bb post is from human/parent and marked `interrupt=true`, deliver as mailbox interrupt
* Otherwise deliver as inbox injection (appears at top next prompt)

### Done when

* If actor A posts “I’m stuck on task X because …”, actor B working on X sees it at the top of its next prompt automatically.

---

## PR3.4 — Task dialogue fetch tool (LLM-friendly)

**Goal:** Agents can deliberately read other agents’ dialogue for a task.

### Implement

Add tool: `task.dialogue`

* Input: `{task_id, limit_turns, since, include_tools}`
* Output: compact transcript (bounded), with paging token

**Important:** This tool returns **text**, not raw DB rows, and never returns megabytes.

### Done when

* An agent can call `task.dialogue` to see what upstream/downstream agents said about the same task.

---

## PR3.5 — Overseer “Task Board” actor (no PageRank yet)

**Goal:** A single overseer can coordinate “who should do what” using deterministic rules before PageRank exists.

### Implement

1. New LLMActor instance type: `overseer`
2. Deterministic scheduling rule (hard):

* Sort runnable tasks by:

  1. explicit priority field (if exists)
  2. due date (if exists)
  3. created_at (FIFO)

3. Overseer writes assignments to blackboard:

* `bb.post(kind="task.assignment", task_id, actor_id, rationale)`

### Done when

* You can run overseer and it assigns tasks + actors begin to coordinate via bb/inbox.

---

# Milestone 4 — Unified Graph + PageRank (Big Brain Ordering + Better Context Injection)

This is where your “shared brain” becomes *structured*: **tasks/sessions/symbols/memories become nodes**, edges are added via hooks, and PageRank helps both scheduling and context selection.

## PR4.1 — Graph tables in memory.db + graph API

**Goal:** Durable graph store for the workspace.

### Implement

1. Add `graph_nodes`, `graph_edges` to `memory.db` exactly as in your design
2. Implement skills:

* `graph.add_edge`
* `graph.query_neighbors`
* `graph.stats`

### Done when

* You can add edges and query neighbors deterministically.

---

## PR4.2 — Edge ingestion hooks (tasks/sessions/turns/bb)

**Goal:** Graph is populated automatically from real work.

### Implement hook skills (trigger → edge):

* `PostToolUse(Edit/Write)` → `task -> symbol (modified)` (requires active task)
* `PostAgentTurn` → `task -> actor (worked_by)` and `task -> bb_post (discussed)` (represented as node IDs)
* `bb.posted` → `task -> memory-ish-node (mentions)` (bb posts become nodes with stable IDs)
* `task.depends_on` updates → `task -> task (depends_on)`

### Done when

* After normal work, the graph has enough edges to rank tasks and associate dialogue.

---

## PR4.3 — PageRank job + persistence on nodes

**Goal:** Compute authority/importance for tasks/symbols and store it.

### Implement

* Skill: `graph.pagerank`
* Writes `pagerank`, `in_degree`, `out_degree` into `graph_nodes`
* Trigger:

  * run after every N new edges (N fixed: 200)
  * and once on supervisor startup

### Done when

* `agentctl graph top --type task --limit 20` is stable and reproducible.

---

## PR4.4 — Overseer scheduling uses PageRank

**Goal:** Overseer ordering is no longer naive; it uses graph importance.

### Implement (hard formula)

Overseer runnable-task score:

* `score = 0.60*pagerank + 0.25*explicit_priority + 0.15*recency`

(No tuning yet; fixed constants.)

### Done when

* Overseer consistently pushes high-importance tasks earlier and explains why in bb.assignment.

---

## PR4.5 — Context injection uses graph neighbors (multi-agent “brain”)

**Goal:** Agents automatically see upstream/downstream dialogue that matters.

### Implement

At `agent.turn.start`, router injects:

1. Active task summary (from tasks db)
2. Top 3 neighbor tasks by PageRank (dependencies + dependents)
3. For each injected task: 10-turn compact dialogue slice via `task.dialogue`
4. Any new bb posts for those tasks since last turn

### Done when

* Agents working on related tasks “notice” each other without direct messaging.

---

# Milestone 5 — Progressive Memory for Actors (L0/L1/L2)

This brings your existing progressive-memory design into the new engine.

## PR5.1 — Actor memory cursors + artifacts

**Goal:** Cursor-based, crash-safe summarization state per actor.

### Implement

* `actor_memory_state` already exists from Milestone 1; extend with:

  * `next_turn_to_summarize`
  * `next_summary_to_distill`
  * `l1_artifact_ref`, `l2_artifact_ref` (these refs are *internal*, not dumped into prompts)
* New table `actor_summaries`:

  * `actor_id`, `summary_index`, `level (L1|L2)`, `turn_start`, `turn_end`, `text`, `created_at`

### Done when

* You can produce and store L1 summaries without affecting runtime.

---

## PR5.2 — Background summarizer worker (supervisor-managed)

**Goal:** Summarization runs async but is crash-safe and deterministic.

### Implement

* `internal/actor/memory/worker.go` started by supervisor
* It:

  * reads turns beyond cursor
  * summarizes batches into L1
  * distills L1→L2 when thresholds hit
  * advances cursor only after successful transaction

### Done when

* Killing the process mid-summarize does not corrupt state and resumes cleanly.

---

## PR5.3 — Prompt builder uses L2 + recent L1 + bounded L0

**Goal:** Context stays within 50k without losing important history.

### Implement (hard budget allocation)

* System + policies: 2k
* Injected inbox context: up to 6k
* Retrieved (semantic/task/graph): up to 8k
* L2: 6k
* L1: 8k
* L0 raw turns: remaining up to 20k
* Current message + scratch: rest

### Done when

* Long-running actors stay stable and don’t require manual compaction.

---

## PR5.4 — “ContextBudgetExceeded” hook actually prunes

**Goal:** Your hook-driven “smart filtering” becomes real.

### Implement

Built-in hook skill `hooks_context_pruner`:

* On `ContextBudgetExceeded` it:

  * drops lowest-ranked injected items (by task PageRank + recency)
  * reduces L0 turn window first
  * keeps decisions + errors + todo state always

### Done when

* Context overflow triggers pruning deterministically and the agent continues.

---

# Milestone 6 — Smarter Tools (Compound Tools to Reduce Tool Spam)

This addresses your point: agents shouldn’t need 10 reads + 5 edits when a single tool can do it safely.

## PR6.1 — `fs.apply_patchset` (atomic multi-edit)

**Goal:** Replace “edit 1 by 1” with one validated patchset.

### Implement

Tool: `fs.apply_patchset`

* Input: `{patches:[{file_path, unified_diff}], require_reservation:true}`
* Applies all-or-nothing
* Rejects if any patch fails or lock missing

### Done when

* LLM can implement multi-file changes in one call safely.

---

## PR6.2 — `code.context_bundle` (one call replaces many reads/searches)

**Goal:** A single “gather relevant context” tool.

### Implement

Tool: `code.context_bundle`

* Input: `{query, workspace_scope, max_files, include_symbols, include_related_sessions, include_related_tasks}`
* Internally uses your existing:

  * ripgrep blocks
  * symbols index
  * session recall
  * task embeddings (once available)
* Output is a bounded “bundle” meant to be pasted into prompt

### Done when

* A coding agent can start with 1 tool call instead of 5–15.

---

## PR6.3 — `code.search_and_edit` (safe refactor tool)

**Goal:** Replace repeated small edits with one verified transformation.

### Implement

Tool: `code.search_and_edit`

* Input: `{pattern, replacement, paths, dry_run:false}`
* Returns:

  * diff preview
  * applied digest internally
* Requires reservations for touched files

### Done when

* Common refactors become one tool call with safety.

---

## PR6.4 — Tool availability policy (deterministic, enforced)

**Goal:** Make tool selection “smarter” by restricting what the model sees.

### Implement

Hard rule set:

* Default tools exposed: read/search/query tools only
* Write tools exposed only if:

  * active task exists
  * reservations acquired
  * hooks allow it (`hooks_file_guard`)

### Done when

* Tool spam drops and destructive tools never appear in the tool list unless conditions are met.

---

# Milestone 7 — “Brain-like” Adaptation (Preferences + Pattern Mining + Improvement Loop)

This is the self-improving layer that stays compatible with your architecture.

## PR7.1 — Preferences as first-class memory entries

**Goal:** Store user/actor preferences and inject them automatically.

### Implement

* `memory.db` named entry type: `preference`
* Example names:

  * `pref://workspace/<id>/formatting/goimports`
  * `pref://user/<id>/tooling/always_run_tests`
* Hook `AgentTurnStart` injects top matching prefs (bounded)

### Done when

* If you set a preference once, future runs follow it without re-teaching.

---

## PR7.2 — Pattern miner job (from trajectories + actor_turns)

**Goal:** Learn “what worked” from real traces.

### Implement

* Background job `analysis.pattern_miner`:

  * Reads successful runs (stop reason ok, no errors)
  * Extracts:

    * tool sequences that correlate with success
    * common file clusters per task type
    * frequent “gotchas → fix” pairs
  * Writes compact “pattern memories” into memory.db

### Done when

* New “pattern” memories appear and get injected for relevant tasks.

---

## PR7.3 — Automatic context improvements (no model training required)

**Goal:** Improve prompt building *without* doing model fine-tuning.

### Implement

* Hook `PostAgentTurn` writes:

  * “what was useful context” (explicit list)
  * “what was useless context” (explicit list)
* The context router uses this feedback to rank future injections.

### Done when

* The system becomes less noisy over time in a measurable way (fewer injected tokens, same or better success rate).

---

## PR7.4 — Evaluation harness (offline, deterministic)

**Goal:** You can prove improvements are improvements.

### Implement

* Command: `agentctl eval replay --from trajectory.db`
* Replays tool traces + prompt builds (no live edits)
* Metrics:

  * avg tool calls per task
  * avg injected tokens
  * stop-block rate
  * time-to-first-success

### Done when

* You can compare baseline vs improved router/tooling policies.

---

# Summary: “What gets implemented now vs later”

You already have Milestone 1 (runtime). The plan above is the concrete roadmap for:

* **Milestone 2:** interactive console
* **Milestone 3:** bb + task dialogue + basic overseer coordination
* **Milestone 4:** graph + pagerank + coordination becomes “big brain”
* **Milestone 5:** progressive memory (L0/L1/L2) for long-running actors
* **Milestone 6:** compound tools to reduce tool spam
* **Milestone 7:** adaptation (preferences + pattern mining + evaluation)

If you want, next I can convert this into a single **implementation checklist doc** that’s ready to paste into `docs/impl_plan/actor_runtime_v2.md` with:

* exact tables + indexes
* exact hook event names
* exact tool schemas (JSON)
* exact default `hooks.yaml` that wires the whole thing together end-to-end
