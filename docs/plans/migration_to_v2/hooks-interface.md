Great — below is a **minimal, portable `hooks.yaml` schema** + a **single Claude Code adapter script interface** that:

* works for **CC + OpenCode + agentctl runtime**
* matches on **platform tool names** (`Edit`, `Write`, …) *and/or* **canonical tool names** (`edit.apply_patch`, `fs.read_file`, …)
* centralizes behavior in **hook skills + dispatcher**
* keeps adapters *thin*

---

## 1) `hooks.yaml` minimal schema (portable across CC/OC/agentctl)

### Schema (v1, minimal)

```yaml
version: 1

defaults:
  # Optional defaults applied to each hook execution
  timeout_ms: 2500
  fail_mode: open
  ephemeral: true

hooks:
  - id: pre-write
    event: PreToolUse
    priority: 10
    match:
      # Match on platform tool names (Claude/OpenCode): Edit/Write/Read/Bash/Grep/Glob/TodoWrite…
      tool_name: "^(Edit|Write|MultiEdit|NotebookEdit)$"
      # Match on canonical tool names (agentctl runtime): edit.*, fs.*, code.*
      tool_canonical: "^edit\\."
      # Optional coarse class (recommended):
      tool_kind: "write"  # read|write|exec|search|any
    run:
      - skill: hooks/task_guard
        timeout_ms: 2000
      - skill: hooks/file_guard
        required: true
      - skill: hooks/security_scanner

  - id: post-write
    event: PostToolUse
    match:
      tool_name: "^(Edit|Write|MultiEdit|NotebookEdit)$"
    run:
      - skill: hooks/impact_analysis
      - skill: hooks/test_feedback

  - id: pre-search
    event: PreToolUse
    match:
      tool_name: "^(Grep|Glob)$"
      tool_kind: "search"
    run:
      - skill: hooks/knowledge_router
      - skill: hooks/semantic_search_hint

  - id: session-start
    event: SessionStart
    run:
      - skill: session/restore
      - skill: daemon/warmup

  - id: stop-guard
    event: StopRequested
    run:
      - skill: hooks/stop_guard   # e.g., todo continuation / DoD gate

  - id: session-end
    event: SessionEnd
    run:
      - skill: session/save
      - skill: session/capture
      - skill: embedding/flush
      - skill: graph/cleanup
```

### Notes on matching

* `match.tool_name` is for **CC/OC tool hooks**.
* `match.tool_canonical` is for your **internal actor runtime tools**.
* You can set either/both. If both are present, treat as **AND** (recommended).
  If you want OR, do it by splitting into two hook entries.

### “HookSpec” fields you should support (minimal)

* `id` (string; required for deterministic overrides/merges)
* `event` (string)
* `priority` (int; optional, default 0)
* `match.tool_name` (regex string; optional)
* `match.tool_canonical` (regex string; optional)
* `match.tool_kind` (`read|write|exec|search|any`; optional)
* `match.prompt_regex` (regex string; optional)
* `match.path_regex` (regex string; optional)
* `run[]` ordered list:

  * `skill` (string)
  * optional per-skill overrides: `timeout_ms`, `fail_open` | `fail_mode` | `required`, `ephemeral` (inherit defaults)

That’s enough to cover your current CC hooks + OC plugin behavior.

---

## 2) Claude Code single adapter script

### Intent

One script that Claude calls for all hook events. It:

1. **Normalizes Claude payload → `hook.Input`**
2. Calls `agentctl run hooks/dispatch`
3. **Translates `hook.Output` → Claude hook response**

### Interface

* File: `configs/hooks/claude/agentctl-hook.sh`
* Called with `--event <ClaudeEvent>` (e.g., `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `SessionStart`, `Stop`)
* Reads JSON payload from stdin
* Outputs JSON Claude understands

### Script (drop-in template)

```bash
#!/usr/bin/env bash
set -euo pipefail

# configs/hooks/claude/agentctl-hook.sh
#
# Usage:
#   agentctl-hook.sh --event PreToolUse
#   agentctl-hook.sh --event PostToolUse
#   agentctl-hook.sh --event UserPromptSubmit
#   agentctl-hook.sh --event SessionStart
#   agentctl-hook.sh --event Stop
#
# Reads Claude hook payload JSON on stdin.
# Calls: agentctl run hooks/dispatch
# Emits Claude-compatible hook output JSON.

EVENT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --event) EVENT="${2:-}"; shift 2;;
    *) shift;;
  esac
done

if [[ -z "${EVENT}" ]]; then
  echo '{}' # fail open
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
WORKSPACE_ROOT="${CLAUDE_PROJECT_DIR:-$(pwd)}"
PAYLOAD="$(cat)"

# Session ID resolution (prefer env, then payload)
SESSION_ID="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-}}"
if [[ -z "${SESSION_ID}" || "${SESSION_ID}" == "null" ]]; then
  SESSION_ID="$(printf '%s' "$PAYLOAD" | jq -r '.session_id // .sessionID // ""' 2>/dev/null || true)"
fi

TOOL_NAME="$(printf '%s' "$PAYLOAD" | jq -r '.tool_name // ""' 2>/dev/null || true)"
TOOL_INPUT="$(printf '%s' "$PAYLOAD" | jq -c '.tool_input // {}' 2>/dev/null || echo '{}')"
TOOL_RESULT="$(printf '%s' "$PAYLOAD" | jq -c '.tool_result // null' 2>/dev/null || echo 'null')"
PROMPT_TEXT="$(printf '%s' "$PAYLOAD" | jq -r '.prompt // ""' 2>/dev/null || true)"
SOURCE="$(printf '%s' "$PAYLOAD" | jq -r '.source // ""' 2>/dev/null || true)"

# Map Claude event → canonical event(s)
# Special handling for Stop: run StopRequested first; if approved, then SessionEnd.
dispatch_once () {
  local canonical_event="$1"

  local hook_input
  hook_input="$(jq -c -n \
    --arg event "$canonical_event" \
    --arg ws "$WORKSPACE_ROOT" \
    --arg sid "$SESSION_ID" \
    --arg tool "$TOOL_NAME" \
    --argjson tool_input "$TOOL_INPUT" \
    --argjson tool_result "$TOOL_RESULT" \
    --arg prompt "$PROMPT_TEXT" \
    --arg source "$SOURCE" \
    '{
      event: $event,
      workspace_root: $ws,
      session_id: $sid,
      tool_name: $tool,
      tool_input: $tool_input,
      tool_observation: (if $tool_result == null then null else $tool_result end),
      prompt: $prompt,
      meta: {
        platform: "claude_code",
        source: $source
      }
    }'
  )"

  # Call dispatcher (fail open unless dispatcher returns a block)
  local resp
  resp="$(
    printf '%s' "$hook_input" | \
      "$AGENTCTL_BIN" run hooks/dispatch --workspace "$WORKSPACE_ROOT" --ephemeral --input-file - 2>/dev/null || true
  )"

  if [[ -z "$resp" ]]; then
    echo '{}' # fail open
    return 0
  fi

  # Extract hook_output
  echo "$resp" | jq -c '.data.hook_output // {}' 2>/dev/null || echo '{}'
}

# Convert hook.Output → Claude hook response
emit_claude_response () {
  local canonical_event="$1"
  local hook_output_json="$2"

  local decision reason context
  decision="$(printf '%s' "$hook_output_json" | jq -r '.decision // "none"' 2>/dev/null || echo "none")"
  reason="$(printf '%s' "$hook_output_json" | jq -r '.reason // ""' 2>/dev/null || echo "")"
  context="$(printf '%s' "$hook_output_json" | jq -r '.context // ""' 2>/dev/null || echo "")"

  # If blocked, return CC block format.
  if [[ "$decision" == "block" ]]; then
    if [[ "$canonical_event" == "StopRequested" ]]; then
      # Preserve current behavior: Stop hooks can inject a continuation prompt.
      # Claude Code convention used in your existing hooks: inject_prompt.
      jq -n --arg r "${reason:-blocked}" --arg p "$context" '{
        decision: "block",
        reason: $r,
        inject_prompt: (if $p != "" then $p else null end),
        stop_hook_active: true
      }'
      return 0
    fi

    jq -n --arg r "${reason:-blocked}" '{
      decision: "block",
      reason: $r
    }'
    return 0
  fi

  # Approve / none
  if [[ -n "$context" && "$context" != "null" ]]; then
    # For most CC hooks, this works fine:
    jq -n --arg c "$context" '{
      decision: "approve",
      context: $c
    }'
    return 0
  fi

  echo '{}' # no-op
}

# Dispatch logic per Claude event
case "$EVENT" in
  PreToolUse)
    out="$(dispatch_once "PreToolUse")"
    emit_claude_response "PreToolUse" "$out"
    ;;

  PostToolUse)
    out="$(dispatch_once "PostToolUse")"
    emit_claude_response "PostToolUse" "$out"
    ;;

  UserPromptSubmit)
    out="$(dispatch_once "UserPromptSubmit")"
    # Many of your existing UserPromptSubmit hooks accept {decision,context}.
    # If you *prefer* hookSpecificOutput for this event, swap the emit function here.
    emit_claude_response "UserPromptSubmit" "$out"
    ;;

  SessionStart)
    out="$(dispatch_once "SessionStart")"
    emit_claude_response "SessionStart" "$out"
    ;;

  Stop)
    # 1) StopRequested gate
    stop_out="$(dispatch_once "StopRequested")"
    stop_decision="$(printf '%s' "$stop_out" | jq -r '.decision // "none"' 2>/dev/null || echo "none")"
    if [[ "$stop_decision" == "block" ]]; then
      emit_claude_response "StopRequested" "$stop_out"
      exit 0
    fi

    # 2) SessionEnd cleanup (only if stop is allowed)
    end_out="$(dispatch_once "SessionEnd")"
    emit_claude_response "SessionEnd" "$end_out"
    ;;

  *)
    echo '{}' # unknown hook event
    ;;
esac
```

### What `hooks/dispatch` should accept/return (minimal)

**Input**: the JSON we built (`event`, `workspace_root`, `session_id`, `tool_name`, `tool_input`/`tool_observation`, `prompt`, `tool_kind`, etc.)
**Output**: standard envelope with `.data.hook_output`:

```json
{
  "decision": "approve|block|none",
  "reason": "optional string",
  "context": "optional string (injected context / continuation prompt)",
  "updated_tool_input": { "optional": "json" },
  "actions": [ "optional actions array (ignored by CC adapter for now)" ]
}
```

---

## 3) Minimal `.claude/settings.json` using the adapter

The key is: **match everything**, let hooks.yaml decide what applies.

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|compact|clear",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/configs/hooks/claude/agentctl-hook.sh --event SessionStart",
            "timeout": 8
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/configs/hooks/claude/agentctl-hook.sh --event UserPromptSubmit",
            "timeout": 5
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/configs/hooks/claude/agentctl-hook.sh --event PreToolUse",
            "timeout": 5
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/configs/hooks/claude/agentctl-hook.sh --event PostToolUse",
            "timeout": 10
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/configs/hooks/claude/agentctl-hook.sh --event Stop",
            "timeout": 20
          }
        ]
      }
    ]
  }
}
```

---

## 4) (Optional but recommended) “tool_kind” normalization

To make matchers portable, your dispatcher should compute `tool_kind` like:

* CC tool names:

  * Read → `read`
  * Edit/Write/MultiEdit/NotebookEdit → `write`
  * Grep/Glob → `search`
  * Bash → `exec`
  * TodoWrite → `write` or `plan` (your call; I’d use `plan`)

* agentctl canonical tools:

  * `fs.*` → read
  * `edit.*` → write
  * `code.search|code.semantic_search|text.*` → search
  * `tests.*` → exec
  * `todo.*` → plan
  * `mail.*|bb.*` → comms

This lets `hooks.yaml` stay consistent across CC/OC/runtime.

---
