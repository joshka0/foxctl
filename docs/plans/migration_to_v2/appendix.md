Awesome — here are the appendices to paste at the bottom of the spec.

````md
---

## Appendix A — `hooks.yaml` schema (v1)

This is the **canonical** config format for hook dispatch. It is intentionally small and deterministic.

### File locations (precedence)
1. `<workspace>/.agentctl/hooks.yaml`
2. `~/.agentctl/hooks.yaml`

### Merge rules (deterministic)
- Both files are loaded.
- Entries are merged by `id`:
  - If workspace config defines an `id` that exists in global config, it **replaces** that entry.
  - Otherwise, entries are **appended**.
- Execution order is sorted by `priority` (ascending) **within each event**; ties preserve merge order.

### Schema

```yaml
version: 1

defaults:
  # Optional defaults applied to each hook execution
  timeout_ms: 2500
  fail_mode: open
  ephemeral: true

hooks:
  - id: string                   # required; stable identifier for merge/override
    enabled: true                # optional; default true

    event: string                # required; one of canonical events
    # SessionStart | MessageReceived | UserPromptSubmit | LLMRequest | LLMResponse |
    # PreToolUse | PostToolUse | StopRequested | PostAgentTurn | ContextBudgetExceeded | SessionEnd

    priority: 0                  # optional; default 0 (lower runs earlier)

    match:                       # optional; all specified matchers must match
      actor_id: string           # regex (e.g. '^actor:system:overseer$')
      tool_name: string          # regex (e.g. '^(Edit|Write)$')
      tool_canonical: string     # regex (e.g. '^edit\\.')
      tool_kind: string          # read|write|exec|search|any
      message_type: string       # regex (e.g. '^(ask|cmd)$')
      namespace: string          # regex on to_ns/from_ns if present in input
      prompt_regex: string       # regex on input.prompt
      path_regex: string         # regex on file path extracted from tool_input

    run:                         # ordered list of hook skills
      - skill: string            # required; e.g. 'hooks/task_guard'
        timeout_ms: 2000         # optional; default 2000
        fail_mode: open          # optional; open|closed (default open)
        required: false          # optional; alias for fail_mode=closed
        fail_open: true          # optional; alias for fail_mode=open (legacy)
        ephemeral: true          # optional; hint for execution mode
        config:                  # optional; merged into hook.Input as hook_config
          any: value
````

### Notes

* Hook skills **always** receive `hook.Input`. `run[].config` is the only config channel; it is not a free-form CLI arg string.
* `fail_mode=closed` (or `required=true`) is reserved for security-critical hooks where “hook runner failed” should block.

---

## Appendix B — Canonical mailbox message shapes (v1)

There are **two messaging layers** in agentctl:

1. **Mailbox Queue (SQLite queue)**: durable, leased messages for actor scheduling. (Ask/Cmd/Event/Reply, Console.*)
2. **Board/Inbox (blackboard board store)**: human/agent messages, reservations, surfaced inbox context. (Handled by `mail.*` tools and `hooks/*_inbox` style hooks.)

This appendix defines the **Mailbox Queue** message format (the actor scheduler queue).

### Mailbox record (queue envelope)

```json
{
  "id": "01J...ULID",
  "from_ns": "actor:agent:coder-1",
  "to_ns": "actor:agent:reviewer-1",
  "type": "ask|cmd|event|reply|console.ask|console.cmd|console.reply|console.event",
  "headers": {
    "correlation": "01J... (ask_id/cmd_id)",
    "delivery": "next_turn"
  },
  "payload": { "version": 1, "status": "ok", "command": "...", "data": { }, "meta": { } },
  "ttl_ms": 300000,
  "visible_at": 1730000000,
  "timestamp": 1730000000
}
```

#### Headers (reserved)

* `correlation`: REQUIRED for `reply` and recommended for `cmd` — must equal `ask_id` or `cmd_id`.
* `delivery=next_turn`: marks a message **non-interruptive** for preemption policy.

#### TTL

* If `ttl_ms > 0`, the message expires at `timestamp*1000 + ttl_ms` and should be acked/dropped when claimed and found expired.

---

### Payload envelope (canonical)

All payloads in mailbox messages are agentctl envelopes:

```json
{
  "version": 1,
  "status": "ok|error",
  "command": "agent.ask|agent.cmd|agent.event|agent.reply|console.ask|console.cmd|console.reply|console.event",
  "data": { },
  "meta": {
    "ts": "2026-01-08T12:00:00Z",
    "correlation_id": "01J..."
  }
}
```

---

### `agent.ask` (request/response)

**Message type:** `ask`
**Command:** `agent.ask`

```json
{
  "data": {
    "ask_id": "01J...ULID",
    "question": "string",
    "context": { "any": "json" }
  }
}
```

**Reply**
**Message type:** `reply`
**Command:** `agent.reply`

```json
{
  "data": {
    "ask_id": "01J... (same as ask_id)",
    "answer": { "response": "string | json" }
  }
}
```

---

### `agent.cmd` (control)

**Message type:** `cmd`
**Command:** `agent.cmd`

```json
{
  "data": {
    "cmd_id": "01J...ULID",
    "action": "run_turn|run_skill|spawn|do_work|...",
    "args": { "any": "json" }
  }
}
```

**Reply (optional)**
**Message type:** `reply`
**Command:** `agent.reply`

```json
{
  "data": {
    "ask_id": "cmd_id (use correlation header)",
    "answer": { "status": "ok|error", "result": { } }
  }
}
```

---

### `agent.event` (fire-and-forget)

**Message type:** `event`
**Command:** `agent.event`

```json
{
  "data": {
    "event_id": "01J...ULID",
    "kind": "string",
    "job_count": 0,
    "payload": { "any": "json" }
  }
}
```

---

### Console messages (interactive sessions)

#### `console.ask`

**Message type:** `console.ask`
**Command:** `console.ask`

```json
{
  "data": {
    "ask_id": "01J...ULID",
    "console_id": "01J...ULID",
    "prompt": "string",
    "context": { "any": "json" }
  }
}
```

#### `console.reply`

**Message type:** `console.reply`
**Command:** `console.reply`

```json
{
  "data": {
    "ask_id": "01J... (same ask_id)",
    "console_id": "01J... (same console_id)",
    "response": "string",
    "status": "ok|error|cancelled",
    "metrics": { "duration_ms": 123 }
  }
}
```

#### `console.cmd` (cancel/pause/resume)

**Message type:** `console.cmd`
**Command:** `console.cmd`

```json
{
  "data": {
    "cmd_id": "01J...ULID",
    "action": "cancel|pause|resume",
    "ask_id": "01J... (target ask_id, required for cancel)"
  }
}
```

---

## Appendix C — Default “core hook pack” mapping (v1)

This is a pragmatic default mapping that:

* uses **existing** hook skills where possible
* includes **placeholders** for hooks we’ll want as first-class runtime hooks

> NOTE: any skill referenced here must be a **hook skill** (accepts `hook.Input`).
> If a non-hook skill is needed (e.g., `session/restore`), it should be invoked via a hook action (`run_skill`) from a hook skill wrapper.

### `~/.agentctl/hooks.yaml` (default)

```yaml
version: 1

hooks:
  # --- Context surfacing before each LLM request ---
  - id: core-llmrequest-mail-router
    event: LLMRequest
    match:
      actor_id: '^actor:agent:'
    run:
      - skill: hooks/mail_router
        timeout_ms: 2000
        fail_open: true

  - id: core-llmrequest-overseer-inbox
    event: LLMRequest
    match:
      actor_id: '^actor:system:overseer$'
    run:
      - skill: hooks/overseer_inbox
        timeout_ms: 2000
        fail_open: true

  - id: core-llmrequest-knowledge-router
    event: LLMRequest
    run:
      - skill: hooks/knowledge_router
        timeout_ms: 1500
        fail_open: true

  # --- Write safety + coordination (edit.*) ---
  - id: core-pretool-edit-task-guard
    event: PreToolUse
    match:
      tool_name: '^edit\.'
    run:
      - skill: hooks/task_guard
        timeout_ms: 2000
        fail_open: true
        config:
          mode: auto   # future: allow override vs env AGENTCTL_TASK_GUARD_MODE

  - id: core-pretool-edit-file-guard
    event: PreToolUse
    match:
      tool_name: '^edit\.'
    run:
      - skill: hooks/file_guard
        timeout_ms: 2000
        fail_open: true
        config:
          mode: advisory  # future: strict/advisory (env AGENTCTL_FILE_GUARD_MODE)

  # --- Post-edit analysis ---
  - id: core-posttool-impact-analysis
    event: PostToolUse
    match:
      tool_name: '^edit\.'
    run:
      - skill: hooks/impact_analysis
        timeout_ms: 45000
        fail_open: true

  - id: core-posttool-test-feedback
    event: PostToolUse
    match:
      tool_name: '^edit\.'
    run:
      - skill: hooks/test_feedback
        timeout_ms: 2000
        fail_open: true

  # --- Session end bookkeeping ---
  - id: core-session-end
    event: SessionEnd
    run:
      - skill: hooks/session_end
        timeout_ms: 5000
        fail_open: true

  # --- Placeholders (recommended additions to implement as hook skills) ---
  - id: core-stop-guard
    enabled: false
    event: StopRequested
    run:
      - skill: hooks/stop_guard
        timeout_ms: 3000
        fail_open: true

  - id: core-post-agent-turn
    enabled: false
    event: PostAgentTurn
    run:
      - skill: hooks/post_agent_turn
        timeout_ms: 1000
        fail_open: true

  - id: core-context-budget-guard
    enabled: false
    event: ContextBudgetExceeded
    run:
      - skill: hooks/context_budget_guard
        timeout_ms: 2000
        fail_open: true
```

### Why these defaults?

* `hooks/task_guard` + `hooks/file_guard` are the **core safety rails** (task-centric + reservation).
* `hooks/mail_router` / `hooks/overseer_inbox` handle **human-in-the-loop** messaging.
* `hooks/knowledge_router` is the lightest “retrieve/advise” layer to keep prompts relevant.
* `hooks/impact_analysis` + `hooks/test_feedback` are the highest ROI post-edit signals.

---

````md
---

## Appendix D — `hook.Input` / `hook.Output` Go types (v1)

These are the **canonical runtime contracts** between the hook dispatcher and hook skills.

### Envelope contract

Hook skills MUST write a normal agentctl envelope with:

- `status: "ok"` on success
- `data.hook_output` containing a `hook.Output`

Example shape:

```json
{
  "version": 1,
  "status": "ok",
  "command": "hooks/task_guard",
  "data": {
    "hook_output": { "...hook.Output..." }
  },
  "meta": { "ts": "2026-01-08T12:00:00Z" }
}
````

Only `data.hook_output` is interpreted by the dispatcher; other `data.*` fields are ignored unless explicitly added later.

---

### Go types (authoritative)

```go
package hooks

import "encoding/json"

// Event is the canonical event name.
// v1 supports ONLY these events.
type Event string

const (
	EventSessionStart          Event = "SessionStart"
	EventMessageReceived       Event = "MessageReceived"
	EventUserPromptSubmit      Event = "UserPromptSubmit"
	EventLLMRequest            Event = "LLMRequest"
	EventLLMResponse           Event = "LLMResponse"
	EventPreToolUse            Event = "PreToolUse"
	EventPostToolUse           Event = "PostToolUse"
	EventStopRequested         Event = "StopRequested"
	EventPostAgentTurn         Event = "PostAgentTurn"
	EventContextBudgetExceeded Event = "ContextBudgetExceeded"
	EventSessionEnd            Event = "SessionEnd"
)

// ToolKind categorizes tools for hook matching.
type ToolKind string

const (
	ToolKindRead   ToolKind = "read"
	ToolKindWrite  ToolKind = "write"
	ToolKindExec   ToolKind = "exec"
	ToolKindSearch ToolKind = "search"
	ToolKindAny    ToolKind = "any"
)

// Decision is the hook verdict.
type Decision string

const (
	DecisionNone    Decision = "none"    // Advisory/no-op; does not block.
	DecisionApprove Decision = "approve" // Allow the operation (default allow).
	DecisionBlock   Decision = "block"   // Block the operation (hard gate).
)

// Input is passed to hook skills on stdin (JSON).
// Fields are populated based on Event; unused fields may be omitted.
type Input struct {
	// Core routing
	Event         Event  `json:"event"`
	ActorID       string `json:"actor_id,omitempty"`       // e.g. actor:agent:coder-1
	WorkspaceID   string `json:"workspace_id,omitempty"`   // stable workspace key (e.g. hashed)
	WorkspaceRoot string `json:"workspace_root,omitempty"` // absolute path (when available)

	// Session/turn identity
	SessionID      string `json:"session_id,omitempty"`      // durable session identity
	TurnID         string `json:"turn_id,omitempty"`         // current turn correlation (engine-generated)
	TraceID        string `json:"trace_id,omitempty"`        // request trace
	CorrelationID  string `json:"correlation_id,omitempty"`  // e.g. ask_id/cmd_id/tool_call_id
	TokenEstimate  int    `json:"token_estimate,omitempty"`  // prompt token estimate (len/4 MVP)
	TokenBudget    int    `json:"token_budget,omitempty"`    // budget (when known)
	BudgetPressure string `json:"budget_pressure,omitempty"` // optional: "ok"|"warning"|"exceeded"

	// Message context (for MessageReceived)
	MailboxMessage *MailboxMessage `json:"mailbox_message,omitempty"`

	// Prompt context (for LLMRequest/LLMResponse/PostAgentTurn/StopRequested)
	Prompt        string `json:"prompt,omitempty"`         // full prompt text (bounded/assembled)
	AssistantText string `json:"assistant_text,omitempty"` // latest assistant text (if any)

	// Tool context (for PreToolUse/PostToolUse)
	ToolName      string          `json:"tool_name,omitempty"`      // platform tool name (Edit, Write, etc.)
	ToolCanonical string          `json:"tool_canonical,omitempty"` // canonical tool name (e.g. edit.apply_patch)
	ToolKind      ToolKind        `json:"tool_kind,omitempty"`      // read|write|exec|search|any
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`     // JSON args being executed

	// PostToolUse observation (what would be appended back to the LLM loop).
	// This should already be "safe": large outputs pointerized to artifact refs.
	ToolObservation json.RawMessage `json:"tool_observation,omitempty"`
	ToolError       string          `json:"tool_error,omitempty"`
	ToolDurationMS  int64           `json:"tool_duration_ms,omitempty"`

	// Hook-specific config from hooks.yaml
	HookConfig map[string]any `json:"hook_config,omitempty"`
}

// MailboxMessage is the durable queue message (leased mailbox store).
// This is the actor scheduler queue message, not the board/inbox message.
type MailboxMessage struct {
	ID      string            `json:"id"`
	FromNS  string            `json:"from_ns"`
	ToNS    string            `json:"to_ns"`
	Type    string            `json:"type"` // ask|cmd|event|reply|console.ask|...
	Headers map[string]string `json:"headers,omitempty"`
	Payload json.RawMessage   `json:"payload,omitempty"`

	TTLMS     int64 `json:"ttl_ms,omitempty"`
	VisibleAt int64 `json:"visible_at,omitempty"`
	Timestamp int64 `json:"timestamp,omitempty"`
}

// Output is returned by hook skills inside envelope.data.hook_output.
type Output struct {
	Decision Decision `json:"decision"`

	// Human-readable explanation. Used for block reasons and debugging.
	Reason string `json:"reason,omitempty"`

	// Optional context to inject immediately (dispatcher concatenates context strings in order).
	// Keep small (<4KB). For larger, use Actions with CAS-backed artifacts.
	Context string `json:"context,omitempty"`

	// PreToolUse: if set, replaces the tool input args passed to the tool.
	// Dispatcher uses "last-wins" across hooks.
	UpdatedToolInput json.RawMessage `json:"updated_tool_input,omitempty"`

	// PostAgentTurn: if set, replaces the assistant final response text.
	// Dispatcher uses "last-wins" across hooks.
	UpdatedAssistantText string `json:"updated_assistant_text,omitempty"`

	// Ordered action list emitted by this hook.
	// Dispatcher concatenates actions in hook execution order.
	Actions []Action `json:"actions,omitempty"`

	// Free-form metadata for debugging/observability.
	Meta map[string]any `json:"meta,omitempty"`
}

// ActionType enumerates allowed action kinds in v1.
type ActionType string

const (
	ActionRunSkill      ActionType = "run_skill"
	ActionInjectContext ActionType = "inject_context"
	ActionSendMailbox   ActionType = "send_mailbox"
	ActionBBPost        ActionType = "bb_post"
	ActionBBClaim       ActionType = "bb_claim"
)

// Action is a tagged union.
// v1 uses a single struct with a superset of fields; fields must match Type.
type Action struct {
	Type ActionType `json:"type"`

	// run_skill
	Skill string          `json:"skill,omitempty"` // e.g. "todo/manage"
	Args  json.RawMessage `json:"args,omitempty"`  // skill input

	// inject_context
	Text     string `json:"text,omitempty"`
	Priority int    `json:"priority,omitempty"` // higher = earlier in ContextInbox; default 0

	// send_mailbox
	ToNS         string          `json:"to_ns,omitempty"`
	MessageType  string          `json:"message_type,omitempty"` // ask|cmd|event|reply|console.ask|...
	Payload      json.RawMessage `json:"payload,omitempty"`      // agentctl envelope
	Headers      map[string]string `json:"headers,omitempty"`
	TTLMS        int64           `json:"ttl_ms,omitempty"`
	DeliveryHint string          `json:"delivery_hint,omitempty"` // optional: "interrupt"|"next_turn"

	// bb_post / bb_claim
	Topic    string          `json:"topic,omitempty"`
	RecordID string          `json:"record_id,omitempty"`
	BBPayload json.RawMessage `json:"payload_bb,omitempty"`
}

// Helper constructors (optional, for hook skills)
func NewNone() Output { return Output{Decision: DecisionNone} }

func NewApprove(reason string, meta map[string]any) Output {
	return Output{Decision: DecisionApprove, Reason: reason, Meta: meta}
}

func NewBlock(reason string) Output {
	return Output{Decision: DecisionBlock, Reason: reason}
}
```

---

### Dispatcher semantics (v1, deterministic)

When multiple hooks run for the same event:

* **Block wins:** if ANY output has `decision="block"`, the dispatcher blocks.
* `updated_tool_input`: **last-wins** (hook order).
* `updated_assistant_text`: **last-wins** (hook order).
* `context`: join non-empty strings with `\n\n` **in hook order**.
* `actions`: concatenated **in hook order**.

**Idempotency note:** hook skills must tolerate being re-run (at-least-once delivery + retries).

---
