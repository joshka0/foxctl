#!/usr/bin/env bash
# agentctl-hook.sh - Unified Claude Code hook adapter
#
# This script handles all CC hook events by normalizing payloads and calling
# the hooks/dispatch skill. Adapters become thin wrappers around this central
# dispatcher.
#
# Usage:
#   agentctl-hook.sh --event PreToolUse
#   agentctl-hook.sh --event PostToolUse
#   agentctl-hook.sh --event UserPromptSubmit
#   agentctl-hook.sh --event SessionStart
#   agentctl-hook.sh --event Stop
#
# Reads Claude hook payload JSON on stdin.
# Emits Claude-compatible hook output JSON on stdout.
#
# Environment:
#   AGENTCTL_BIN           - Path to agentctl binary (default: agentctl)
#   CLAUDE_PROJECT_DIR     - Workspace root (set by CC)
#   CLAUDE_SESSION_ID      - Session ID (set by CC)
#   AGENTCTL_SESSION_ID    - Alternate session ID

set -euo pipefail

# Ensure child processes are killed on termination
trap 'kill $(jobs -p) 2>/dev/null || true' SIGTERM SIGINT EXIT

# Source error handling library
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$SCRIPT_DIR/lib/error-queue.sh" ]]; then
  source "$SCRIPT_DIR/lib/error-queue.sh"
  HAS_ERROR_LIB=1
else
  HAS_ERROR_LIB=0
fi

# Parse arguments
EVENT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --event) EVENT="${2:-}"; shift 2;;
    *) shift;;
  esac
done

if [[ -z "${EVENT}" ]]; then
  echo '{}' # fail open - unknown event
  exit 0
fi

# Configuration
AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
WORKSPACE_ROOT="${CLAUDE_PROJECT_DIR:-$(pwd)}"
PAYLOAD="$(cat)"

# Session ID resolution (prefer env, then payload)
SESSION_ID="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-}}"
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  SESSION_ID="$(printf '%s' "$PAYLOAD" | jq -r '.session_id // .sessionID // ""' 2>/dev/null || true)"
fi

# Extract common fields from CC payload
TOOL_NAME="$(printf '%s' "$PAYLOAD" | jq -r '.tool_name // ""' 2>/dev/null || true)"
TOOL_INPUT="$(printf '%s' "$PAYLOAD" | jq -c '.tool_input // {}' 2>/dev/null || echo '{}')"
TOOL_RESULT="$(printf '%s' "$PAYLOAD" | jq -c '.tool_result // null' 2>/dev/null || echo 'null')"
PROMPT_TEXT="$(printf '%s' "$PAYLOAD" | jq -r '.prompt // ""' 2>/dev/null || true)"
SOURCE="$(printf '%s' "$PAYLOAD" | jq -r '.source // ""' 2>/dev/null || true)"

# Map Claude event -> canonical event(s)
# Special handling for Stop: run StopRequested first; if approved, then SessionEnd.

dispatch_once() {
  local canonical_event="$1"

  # Determine provider capabilities based on event
  # Context injection is possible on: SessionStart, PostToolUse, UserPromptSubmit
  # Blocking is possible on: PreToolUse, StopRequested
  local can_inject="false"
  local can_block="false"
  case "$canonical_event" in
    SessionStart|PostToolUse|UserPromptSubmit)
      can_inject="true"
      ;;
    PreToolUse|StopRequested)
      can_block="true"
      ;;
  esac

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
    --argjson can_inject "$can_inject" \
    --argjson can_block "$can_block" \
    '{
      event: $event,
      workspace_root: $ws,
      session_id: $sid,
      tool_name: $tool,
      tool_input: $tool_input,
      tool_observation: (if $tool_result == null then null else $tool_result end),
      prompt: $prompt,
      provider: {
        name: "claude-code",
        event: $event,
        can_inject_context: $can_inject,
        can_block: $can_block
      },
      meta: {
        platform: "claude_code",
        source: $source
      }
    }'
  )"

  # Call dispatcher - capture errors for propagation
  local resp stderr_file exit_code
  stderr_file=$(mktemp)

  resp="$(
    printf '%s' "$hook_input" | \
      "$AGENTCTL_BIN" run hooks/dispatch --workspace "$WORKSPACE_ROOT" --ephemeral --input-file - 2>"$stderr_file"
  )" && exit_code=0 || exit_code=$?

  local stderr_content
  stderr_content=$(cat "$stderr_file" 2>/dev/null || true)
  rm -f "$stderr_file"

  # If dispatcher failed, propagate error via exit 2
  if [[ $exit_code -ne 0 || -z "$resp" ]]; then
    if [[ "$HAS_ERROR_LIB" == "1" && -n "$stderr_content" ]]; then
      hook_error_block "agentctl-hook" "hooks/dispatch failed for $canonical_event" "$stderr_content"
    fi
    echo '{}' # fail open if error lib not available
    return 0
  fi

  # Check for skill-level errors in response
  local skill_error
  skill_error=$(printf '%s' "$resp" | jq -r '.error.message // ""' 2>/dev/null || true)
  if [[ -n "$skill_error" && "$skill_error" != "null" ]]; then
    if [[ "$HAS_ERROR_LIB" == "1" ]]; then
      hook_error_block "agentctl-hook" "hooks/dispatch error for $canonical_event" "$skill_error"
    fi
    echo '{}'
    return 0
  fi

  # Extract hook_output
  printf '%s' "$resp" | jq -c '.data.hook_output // {}' 2>/dev/null || echo '{}'
}

# Convert hook.Output -> Claude hook response
emit_claude_response() {
  local canonical_event="$1"
  local hook_output_json="$2"

  local decision reason context
  decision="$(printf '%s' "$hook_output_json" | jq -r '.decision // "none"' 2>/dev/null || echo "none")"
  reason="$(printf '%s' "$hook_output_json" | jq -r '.reason // ""' 2>/dev/null || echo "")"
  context="$(printf '%s' "$hook_output_json" | jq -r '.context // ""' 2>/dev/null || echo "")"

  # Extract context from inject_context actions if not set directly
  if [[ -z "$context" || "$context" == "null" ]]; then
    context="$(printf '%s' "$hook_output_json" | jq -r '
      [.actions // [] | .[] | select(.type == "inject_context") | .text] | join("\n\n")
    ' 2>/dev/null || echo "")"
  fi

  # If blocked, return CC block format
  if [[ "$decision" == "block" ]]; then
    if [[ "$canonical_event" == "StopRequested" ]]; then
      # Stop hooks can inject a continuation prompt
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

  # Approve / none with optional context
  if [[ -n "$context" && "$context" != "null" ]]; then
    # Use hookSpecificOutput.additionalContext for PostToolUse
    if [[ "$canonical_event" == "PostToolUse" ]]; then
      jq -n --arg c "$context" '{
        hookSpecificOutput: {
          hookEventName: "PostToolUse",
          additionalContext: $c
        }
      }'
    else
      # Other events use stdout context injection
      printf '%s' "$context"
    fi
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
    emit_claude_response "UserPromptSubmit" "$out"
    ;;

  SessionStart)
    out="$(dispatch_once "SessionStart")"
    emit_claude_response "SessionStart" "$out"
    ;;

  PreCompact)
    # PreCompact maps to ContextBudgetExceeded
    out="$(dispatch_once "ContextBudgetExceeded")"
    emit_claude_response "ContextBudgetExceeded" "$out"
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
