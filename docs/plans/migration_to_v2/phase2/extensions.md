Cool — here’s **exactly what we implement now**, in the order we implement it,
and **how** (packages/files/tables, what changes, and what “done” means). This
is the “MVP that replaces dspy-go for the actor system” and gives you:
**always-on supervisor + tool-calling loop-until-stop + hooks + guaranteed
context injection + preemption + write-safety**.

---

# What we implement now (Milestone 1)

## PR1 — LLM client (OpenRouter-first, BYOK-ready)

**Goal:** A single internal interface the engine uses, with OpenRouter
tool-calling working end-to-end.

### Implement

1. `internal/llmchat/`

- `type Client interface { Chat(ctx, req) (resp, error) }`
- `ChatRequest` contains:

  - `Messages []Message` (system/user/assistant/tool)
  - `Tools []ToolSpec` (JSON schema)
  - `MaxTokens`, `Temperature`, `Stop`, `Metadata`
- `ChatResponse` contains:

  - `AssistantText string`
  - `ToolCalls []ToolCall {ID, Name, Args json.RawMessage}`
  - `FinishReason string`

2. `internal/llmchat/openrouter/`

- Implements OpenAI-compatible tool calling against OpenRouter
- BYOK config resolution:

  - Actor config overrides env
  - Env fallback: `OPENROUTER_API_KEY`
  - Model id from actor config (`anthropic/claude-…`, etc.)

### Done when

- A unit test hits the OpenRouter mock transport and parses **tool calls**
  correctly.
- `ChatResponse.ToolCalls` works for a synthetic tool-call response.

---

## PR2 — ToolKit (standard tools) + hard write-safety

**Goal:** The engine has tools the LLM can call with **standardized names +
schemas**, and edits are **blocked unless reserved**.

### Implement

1. `internal/agent/toolkit/`

- `ToolSpec{Name, Description, JSONSchema}`
- `Invoke(ctx, name, args) -> ToolResult{JSON any, IsError bool}`

2. Standard tool names (v1 canonical) Keep your dot naming (no rename churn):

- `fs.read_file`, `fs.list_dir`
- `code.search`
- `tests.run`
- `todo.query`, `todo.add`, `todo.set_active`, `todo.ensure_active`
- `mail.send`, `mail.inbox`, `mail.ack`, `mail.reserve`, `mail.release`
- `bb.post`, `bb.search`, `bb.claim`, `bb.release`

3. Standard tool arg shape (v1 canonical)

- Any file tool takes `file_path` (not `path`). The toolkit adapter accepts
  both, but **emits `file_path` into hook inputs** so your existing hook skills
  work.

4. Reservations enforcement (hard rule)

- In every `edit.*` tool (we’ll implement in PR4/5), require:

  - reservation exists for `file_path`
  - held by this actor_id
  - not expired If not: return tool error `{code:"ELOCKED", conflicts:[...]}`.

5. Add `artifact.read` tool now

- Reads a CAS digest in pages/bytes (so the LLM never needs “agentctl cas …”)
- Output: `{digest, page, text, has_more}`

### Done when

- `toolkit.ListSpecs()` returns valid JSON schemas for all tools above.
- `mail.reserve` + `mail.release` work against your board store.
- `artifact.read` works against CAS store.

---

## PR3 — Hook dispatcher (hooks are skills) + extended hook outputs

**Goal:** Any pipeline stage can run hooks, and hooks can
block/mutate/enqueue/inject/rewrite.

### Implement

1. Extend `internal/domain/hook/types.go` Add optional fields to `hook.Output`:

- `UpdatedToolInput json.RawMessage`
- `UpdatedAssistantText string`
- `Actions []Action`

Add `hook.Action` type with **exact** v1 action set:

- `run_skill`
- `inject_context`
- `send_mailbox`
- `bb_post`

2. `internal/runtime/hooks/dispatcher/`

- Loads hook config from:

  - `<workspace>/.agentctl/hooks.yaml`
  - `~/.agentctl/hooks.yaml`
- Matches: `event` + optional regex on `tool_name`
- Executes hooks as skills using your existing skill runner
- Aggregates deterministically:

  - any block → block
  - `UpdatedToolInput`: last-wins
  - `UpdatedAssistantText`: last-wins
  - `Actions`: append in order
  - `Context`: concatenate (and becomes injected context if configured)

3. Hook input enrichment (v1) Extend `hook.Input` to include (optional fields):

- `actor_id`, `turn_id`
- `prompt` (for LLMRequest)
- `assistant_text` (for PostAgentTurn)
- `token_estimate`
- `tool_name`, `tool_input`, `tool_response` (already exist)

### Done when

- A test hook skill can:

  - mutate `tool_input`
  - inject context via `Actions: inject_context`
  - rewrite assistant output via `UpdatedAssistantText`

---

## PR4 — ActorStore tables (sessions DB) for turns + context inbox

**Goal:** Durable turns and guaranteed injection, keyed by `actor_id` (you asked
to prefer actor identity over session lineage).

### Implement

In `~/.agentctl/storage/sessions.db` migrate these tables:

1. `actor_turns`

- `id (ulid) PK`
- `workspace_id`
- `actor_id`
- `run_id` (correlation id for a “turn-until-stop” episode)
- `role` (`user|assistant|tool|system`)
- `content_preview` (bounded)
- `content_digest` (optional CAS pointer)
- `tool_name`, `tool_args_digest`, `tool_result_digest` (optional)
- `created_at`

2. `actor_context_inbox`

- `id (ulid) PK`
- `workspace_id`
- `actor_id`
- `priority` (int)
- `kind` (mailbox|bb|hook|system|human)
- `text` (bounded)
- `created_at`
- `surfaced_at` (nullable)

3. `actor_memory_state`

- cursors + token estimates (even if we only use L0 initially)
- `token_estimate`, `updated_at`, etc.

Create `internal/storage/actorstore/` with:

- `AppendTurn(...)`
- `AddInboxItem(...)`
- `DrainInbox(...)` (returns items, marks surfaced)
- `ListRecentTurns(...)` (bounded)

### Done when

- Turns and inbox items are persisted and retrievable.
- Drain semantics are correct (once drained, not returned again).

---

## PR5 — Engine: loop-until-stop + prompt builder + context budget trigger

**Goal:** Replace dspy-go execution with your own deterministic loop, and make
context injection + stop gating real.

### Implement

1. `internal/agent/engine/`

- `RunUntilStop(ctx, actorCtx, incomingMessage) -> finalAssistantText, toolTrace, error`
- Loop:

  1. Build prompt:

     - System+role
     - **Drain ContextInbox and include first**
     - recent actor turns (bounded)
     - current user message
  2. If token estimate > 0.8 * 50k:

     - emit `ContextBudgetExceeded` hooks
     - apply actions (usually inject “drop/retrieve less” guidance) and rebuild
  3. Call LLM with tool specs
  4. If tool calls:

     - emit `PreToolUse` hooks (can mutate args / block)
     - run tool
     - emit `PostToolUse` hooks (inject context / enqueue actions)
     - append tool result as a turn
     - continue
  5. If no tool calls:

     - emit `StopRequested` hooks
     - if blocked: inject context (inbox) + continue loop
     - else: accept stop
  6. emit `PostAgentTurn` hooks (can rewrite assistant output)
  7. persist assistant turn
  8. return

2. Hard caps (non-optional, to avoid runaway)

- `MaxIterationsPerRun = 50`
- `MaxToolCallsPerIteration = 10` If exceeded → engine returns error, and
  `PostAgentTurn` hook runs with an error summary.

3. “Large tool output” handling (LLM-friendly)

- If tool output JSON exceeds `MaxToolResultBytes`:

  - store full JSON in CAS
  - replace tool result with:

    - `{"summary":"output stored","artifact":{"digest":...,"tool":"artifact.read"},"preview":"..."}`

### Done when

- Engine can complete a mailbox ask by calling tools multiple times and then
  stopping.
- StopRequested hook can block stop (“remaining todos”) and force another
  iteration.
- ContextInbox items always appear as the first prompt block on the next LLM
  request.

---

## PR6 — Supervisor integration: new LLMActor replaces DspyActor

**Goal:** The actor system runs without dspy-go, under the single supervisor.

### Implement

1. `internal/runtime/actor/llm_actor.go`

- Implements your Actor interface (`OnMailReceived`, etc.)
- Uses the engine in PR5
- Persists turns/inbox items via actorstore
- Uses hook dispatcher in PR3

2. Update `internal/runtime/actor/system` to construct LLMActor

- `actor.NewLLMActor(config, deps...)`

3. Update `cmd/agentctl/cmd/actorsys.go`

- `respawnRegisteredActors` uses `NewLLMActor` (not dspy)
- `actorsys spawn` stores provider/model/api-key references in actor config

4. Preemption

- Supervisor logic:

  - if actor is processing and a message arrives from parent/human and not
    `delivery=next_turn`
  - cancel the actor’s context and enqueue this message at front

### Done when

- `agentctl actorsys supervisor start` runs, spawns multiple LLMActors, and they
  process mailbox events.
- Parent/human interrupt preempts a running actor.

---

## PR7 — Remove dspy-go wiring from the actor path

**Goal:** dspy-go is no longer required for the actor runtime.

### Implement

- Stop importing dspy-go in actor system paths
- Leave legacy CLI commands (`dspy-agent`) alone for now, but they’re no longer
  used by actorsys.

### Done when

- `actorsys` runs without dspy-go dependencies in its code path.

---

# What is _not_ implemented yet (Milestone 2, after the above is stable)

This is explicitly _not now_:

- interactive actor console UI wiring (you said phase 2 ok)
- “big brain task update routing” (task_updates + bb task.update + overseer
  pagerank scheduling)
- compound tools (`code.gather`, `edit.apply_plan`, `tests.run_smart`)
- progressive L0→L1→L2 summarization (your memory design); we’ll do it after we
  see real engine traces so the summarizer prompts are grounded

---

# What you’ll have after Milestone 1

- Single supervisor managing many actors
- OpenRouter tool-calling loop-until-stop
- Hook pipeline at all key stages (including PostAgentTurn)
- Guaranteed context injection (inbox drains first)
- Preemption by parent/human
- Write-safety enforced (reservations required for edits)
- Durable actor turns in sessions DB

---

If you’re good with this, I’ll next turn this into:

- a PR-by-PR checklist (files + functions to create/modify)
- a minimal default `hooks.yaml` we ship so the system works out of the box
  (file_guard, task_guard, stop_guard, post_agent_turn, context_budget_guard)
