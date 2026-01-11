Awesome — here’s the **exact `hooks/dispatch` contract** (input/output structs + merge rules + failure semantics) that works for **Claude Code**, **OpenCode**, and your **future in-process actor engine**.

I’m writing this as a “spec you can implement” (and it’s compatible with your existing hook skills that already emit `data.hook_output`).

---

# `hooks/dispatch` skill

## Purpose

A single skill that:

1. Loads `hooks.yaml` configs
2. Matches hooks for a given event + tool/prompt context
3. Runs hook skills in deterministic order
4. Merges their `hook.Output`s into one final `hook_output` the adapter/runtime can consume

Adapters (CC/OC) become thin: they just normalize payload → call `hooks/dispatch` → translate response.

---

# 1) Input Contract (JSON)

Use one **union input shape** for all events.

### `hook.Input` (v1)

```jsonc
{
  "event": "PreToolUse|PostToolUse|UserPromptSubmit|SessionStart|StopRequested|SessionEnd|PostAgentTurn|ContextBudgetExceeded|LLMRequest|LLMResponse|MessageReceived",

  "workspace_root": "/abs/path/to/workspace",
  "workspace_id": "optional-stable-id",        // optional, if you have it
  "session_id": "optional",
  "actor_id": "optional",                     // actor namespace/id
  "turn_id": "optional",                      // stable turn id if available
  "trace_id": "optional",
  "correlation_id": "optional",               // ask_id/cmd_id/tool_call_id

  "tool_name": "Edit",                        // platform tool name (CC/OC)
  "tool_canonical": "edit.apply_patch",       // optional canonical name (agentctl runtime)
  "tool_kind": "read|write|search|exec|any",  // optional but recommended

  "tool_input": { /* any */ },                // JSON object (or {}); raw tool args
  "tool_observation": { /* any */ },          // JSON object (or null); raw tool observation
  "tool_error": "string",                     // optional error string
  "tool_duration_ms": 123,                    // optional duration in ms

  "mailbox_message": { /* MessageReceived payload */ },

  "prompt": "user prompt text",               // for UserPromptSubmit / LLMRequest, etc.
  "assistant_text": "assistant response text", // for PostAgentTurn / LLMResponse

  "token_estimate": 12345,                    // optional; used by ContextBudget hooks
  "token_budget": 32000,                      // optional; total budget if known
  "budget_pressure": "ok|warning|exceeded"    // optional
}
```

### Notes

* **Adapters should always send `tool_name`** when the event is tool-related (CC/OC).
* Your **in-process engine** should set `tool_canonical` for canonical tool calls (and can set `tool_name` equal to canonical).
* `tool_kind` can be computed by dispatcher if missing (recommended default behavior).

---

# 2) Output Contract (JSON in envelope)

`hooks/dispatch` returns an envelope with `data.hook_output` plus diagnostics.

### `data` payload

```jsonc
{
  "hook_output": {
    "decision": "approve|block|none",
    "reason": "string (optional)",
    "context": "string (optional)",

    "updated_tool_input": { /* any */ },        // optional; only meaningful for PreToolUse in engine/OC
    "updated_assistant_text": "string",         // optional; only meaningful for PostAgentTurn

    "actions": [
      // optional; used by in-process engine
      { "type": "inject_context", "text": "...", "priority": 50 },
      { "type": "run_skill", "skill": "graph/pagerank", "args": { "workspace": "..." } }
    ],

    "meta": { /* optional merged meta */ }
  },

  "matched_hooks": [
    { "id": "pre-write", "event": "PreToolUse", "priority": 10, "match": { /* resolved match */ }, "run": [/* run entries */] }
  ],

  "steps": [
    {
      "idx": 1,
      "hook_id": "pre-write",
      "skill": "hooks/task_guard",
      "decision": "approve|block|none",
      "duration_ms": 12,
      "error": "",
      "fail_open": true,
      "hook_output": { /* raw hook output for debugging */ }
    }
  ],

  "config_files": [
    "/workspace/.agentctl/hooks.yaml",
    "/home/user/.agentctl/hooks.yaml"
  ],
  "blocked": false,
  "blocked_by": "",
  "hooks_run": ["pre-write"],
  "duration_ms": 37
}
```

---

# 3) `hook.Output` and `hook.Action` (v1)

### `hook.Output` (merged output)

```go
type Output struct {
  Decision Decision `json:"decision"` // approve|block|none
  Reason   string   `json:"reason,omitempty"`
  Context  string   `json:"context,omitempty"`

  UpdatedToolInput      json.RawMessage `json:"updated_tool_input,omitempty"`
  UpdatedAssistantText  string          `json:"updated_assistant_text,omitempty"`
  Actions               []Action        `json:"actions,omitempty"`

  Meta map[string]any `json:"meta,omitempty"`
}
```

### `hook.Action` (engine-only; adapters may ignore)

Keep v1 action set **exactly** this (matches your earlier target architecture):

```go
type Action struct {
  Type string `json:"type"` // run_skill|inject_context|send_mailbox|bb_post|bb_claim

  // run_skill
  Skill string          `json:"skill,omitempty"`
  Args  json.RawMessage `json:"args,omitempty"`

  // inject_context
  Text     string `json:"text,omitempty"`
  Priority int    `json:"priority,omitempty"`

  // send_mailbox
  ToNS        string          `json:"to_ns,omitempty"`
  MessageType string          `json:"message_type,omitempty"`
  Payload     json.RawMessage `json:"payload,omitempty"`
  Headers     map[string]string `json:"headers,omitempty"`
  TTLMS       int64           `json:"ttl_ms,omitempty"`
  DeliveryHint string         `json:"delivery_hint,omitempty"`

  // bb_post / bb_claim
  Topic    string          `json:"topic,omitempty"`
  RecordID string          `json:"record_id,omitempty"`
  BBPayload json.RawMessage `json:"payload_bb,omitempty"`
}
```

---

# 4) Deterministic Merge Rules (this is the key bit)

Given hook steps executed in order, merge into a single accumulator:

## Decision

1. If **any** step returns `decision=block` ⇒ final `block`.
2. Else if **any** step returns `decision=approve` ⇒ final `approve`.
3. Else final `none`.

## Reason

* If final is `block`: take the **first non-empty reason** among blocking steps.
* If final is `approve`: take the **first non-empty reason** among approve steps (optional).
* Otherwise empty.

## Context (string)

* Collect all non-empty `context` strings **in execution order**.
* Final `context` is `join(contexts, "\n\n")` (simple, portable).
* For `StopRequested`, this `context` is treated as the **inject prompt** by the CC adapter (your current behavior).

## UpdatedToolInput

* **Last-wins** (the last step with `updated_tool_input` overwrites prior).
* If final decision is `block`, you can still keep it in the output for debugging, but runtime should ignore it.

## UpdatedAssistantText

* **Last-wins** (used by PostAgentTurn).

## Actions

* Append in order.
* (Optional but recommended) Dedupe identical actions by `type + stable_hash(payload)` to prevent spam.

## Meta

* Don’t try to “deep merge” arbitrary maps in v1.
* Instead:

  * Put per-step meta into `steps[].hook_output.meta`
  * Only keep a small top-level `hook_output.meta` with dispatcher diagnostics, e.g. `{ "blocked_by": "hooks/task_guard" }`

---

# 5) Failure Semantics (important)

Each `hooks.yaml` `run` item should support:

* `required: false` (default)
* `fail_mode: open|closed` (default `open`)
* `fail_open: true|false` (alias of `fail_mode`)

Behavior if a hook skill fails (non-zero exit, invalid envelope, timeout):

* **fail_mode=open**: treat as `decision=none`, record error in `steps[]`.
* **fail_mode=closed**: treat as `decision=block` with reason:

  * `"hook_failed:<skill>:<error>"`

This keeps your current “fail open” behavior for CC hooks, while letting you make guards strict when you want.

---

# 6) Hook config resolution order

Dispatcher loads configs in this order (later overrides earlier by `id`, execution order remains deterministic by `priority`):

1. `~/.agentctl/hooks.yaml` (if exists)
2. `<workspace>/.agentctl/hooks.yaml` (if exists)

Merge rules:
- Entries are merged by `id` (required).
- If the workspace config defines an existing `id`, it **replaces** that entry.
- Otherwise entries are appended.

---

# 7) Matching rules (exact)

A hook matches if:

* `event` equals input `event`, AND
* every specified match field passes:

Supported match fields (v1):

* `tool_name`: regex matched against `input.tool_name`
* `tool_canonical`: regex matched against `input.tool_canonical` (or `input.tool_name` if canonical missing and it contains a dot)
* `tool_kind`: equals `input.tool_kind`
* `prompt_regex`: regex over `input.prompt` (optional)
* `path_regex`: regex over extracted `file_path` from `tool_input` when present (optional)

Order:

* Sort matched hooks by `(priority asc, file_order asc, list_order asc)`.
* Execute `run[]` in order.

---

# 8) What the CC adapter does with this

For Claude Code:

* PreToolUse / PostToolUse:

  * If `block` ⇒ return `{decision:"block", reason}`
  * If `context` non-empty ⇒ return `{decision:"approve", context}`
  * Else `{}`

For Stop:

* Call `StopRequested`:

  * If `block` ⇒ return `{decision:"block", reason, inject_prompt: context, stop_hook_active:true}`
* If approved, call `SessionEnd` (cleanup) and return `{}`.

OpenCode:

* You can call dispatcher from tool hooks too, but since OC can’t inject context there, you’ll typically convert `actions.inject_context` into your pending-context file.

---
