#!/usr/bin/env bash
# todo-sync.sh - Sync Claude Code's TodoWrite with agentctl todo skill
#
# This hook triggers on PostToolUse for TodoWrite and syncs the todo list
# with agentctl's task management system using the todo/sync_from_provider skill.
#
# Features:
#   - Syncs todos to agentctl task store with stable ID tags
#   - Auto-infers dependencies based on task order
#   - Maps todos by task ID tag (〔T:<id>〕) or title
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_TODO_SYNC_DISABLED - Set to "1" to disable
#   AGENTCTL_TODO_SYNC_DEBUG - Set to "1" for debug output
#   AGENTCTL_TODO_BIDIRECTIONAL - Set to "1" to enable outbound sync (Phase 2)

set -euo pipefail

# Source error queue library for surfacing failures
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SCRIPT_DIR/lib/error-queue.sh" ]]; then
  source "$SCRIPT_DIR/lib/error-queue.sh"
  HAS_ERROR_QUEUE=1
else
  HAS_ERROR_QUEUE=0
fi

DEBUG="${AGENTCTL_TODO_SYNC_DEBUG:-}"

# Check if disabled
if [[ "${AGENTCTL_TODO_SYNC_DISABLED:-}" == "1" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Find agentctl binary
if [[ -n "${AGENTCTL_BIN:-}" ]]; then
  : # Use provided path
elif command -v agentctl &>/dev/null; then
  AGENTCTL_BIN="agentctl"
elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
  AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
else
  echo '{"decision":"approve"}'
  exit 0
fi

# Read hook input from stdin
payload="$(cat)"

# Extract todos from tool_input
todos=$(printf '%s' "$payload" | jq -c '.tool_input.todos // []')

# Exit early if no todos
if [[ "$todos" == "[]" || "$todos" == "null" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Session ID detection
SESSION_ID="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-${OPENCODE_SESSION_ID:-}}}"
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  SESSION_ID="$(printf '%s' "$payload" | jq -r '.sessionID // .session_id // ""' 2>/dev/null || true)"
fi
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  agentctl_home="${AGENTCTL_HOME:-$HOME/.agentctl}"
  workspace_hash="$(printf '%s' "$workspace" | shasum -a 256 | cut -c1-16)"
  identity_dir="$agentctl_home/sessions/active"
  for f in "$identity_dir/${workspace_hash}-"*.json "$identity_dir/${workspace_hash}.json"; do
    [[ -f "$f" ]] || continue
    SESSION_ID="$(jq -r '.session_id // ""' "$f" 2>/dev/null || true)"
    if [[ -n "$SESSION_ID" && "$SESSION_ID" != "null" ]]; then
      break
    fi
  done
fi

[[ -n "$DEBUG" ]] && echo "DEBUG: session_id=$SESSION_ID, workspace=$workspace" >&2

# Build skill input
sync_input=$(jq -nc \
  --arg provider "claude" \
  --arg ws "$workspace" \
  --arg sid "$SESSION_ID" \
  --argjson todos "$todos" \
  '{
    provider: $provider,
    workspace_id: $ws,
    session_id: (if $sid == "" or $sid == "null" then null else $sid end),
    todos: $todos
  }')

[[ -n "$DEBUG" ]] && echo "DEBUG: sync_input=$sync_input" >&2

# Run inbound sync skill
sync_stderr=$(mktemp)
result=$(printf '%s' "$sync_input" | "$AGENTCTL_BIN" run todo/sync_from_provider --input-file - 2>"$sync_stderr") || {
  error_detail=$(cat "$sync_stderr" 2>/dev/null || echo "unknown error")
  rm -f "$sync_stderr"
  [[ -n "$DEBUG" ]] && echo "DEBUG: sync skill failed: $error_detail" >&2

  # Block with exit 2 to surface error to Claude (will retry on next TodoWrite)
  if [[ "$HAS_ERROR_QUEUE" == "1" ]]; then
    hook_error_block "todo-sync" "inbound sync failed" "$error_detail"
  fi

  # Fallback if error-queue.sh not available
  echo '{"decision":"approve"}'
  exit 0
}
rm -f "$sync_stderr"

[[ -n "$DEBUG" ]] && echo "DEBUG: result=$result" >&2

# Extract counts from result
created=$(printf '%s' "$result" | jq -r '.data.created // 0')
updated=$(printf '%s' "$result" | jq -r '.data.updated // 0')
completed=$(printf '%s' "$result" | jq -r '.data.completed // 0')
mapped=$(printf '%s' "$result" | jq -r '.data.mapped // 0')
deps_added=$(printf '%s' "$result" | jq -r '.data.deps_added // 0')
warnings=$(printf '%s' "$result" | jq -r '.data.warnings // [] | join("; ")')

# Build response
context_parts=()

total=$((created + updated + completed))
if ((total > 0 || mapped > 0)); then
  summary="Synced $((mapped + created)) todos"
  [[ $created -gt 0 ]] && summary="$summary, created $created"
  [[ $updated -gt 0 ]] && summary="$summary, updated $updated"
  [[ $completed -gt 0 ]] && summary="$summary, completed $completed"
  [[ $deps_added -gt 0 ]] && summary="$summary, $deps_added deps inferred"
  context_parts+=("**Todo Sync:** $summary")
fi

# Add warnings if any
if [[ -n "$warnings" && "$warnings" != "null" ]]; then
  context_parts+=("**Warnings:** $warnings")
fi

# Phase 2: Outbound sync (when enabled)
if [[ "${AGENTCTL_TODO_BIDIRECTIONAL:-}" == "1" ]]; then
  # Run outbound sync to update Claude's todo file with task ID tags
  outbound_input=$(jq -nc \
    --arg provider "claude" \
    --arg ws "$workspace" \
    --arg sid "$SESSION_ID" \
    '{
      provider: $provider,
      workspace_id: $ws,
      session_id: (if $sid == "" or $sid == "null" then null else $sid end)
    }')

  # Enable provider state writes for outbound sync
  export AGENTCTL_ALLOW_PROVIDER_STATE=1
  outbound_result=$(printf '%s' "$outbound_input" | "$AGENTCTL_BIN" run todo/sync_to_provider --input-file - 2>/dev/null) || true

  if [[ -n "$outbound_result" ]]; then
    written=$(printf '%s' "$outbound_result" | jq -r '.data.written // 0')
    if ((written > 0)); then
      context_parts+=("**Outbound:** Updated Claude todo file with $written tasks")
    fi
  fi
fi

if [[ ${#context_parts[@]} -gt 0 ]]; then
  context=$(printf '%s\n' "${context_parts[@]}" | tr '\n' ' ')
  jq -nc --arg ctx "$context" '{
    hookSpecificOutput: {
      hookEventName: "PostToolUse",
      additionalContext: $ctx
    }
  }'
else
  echo '{}'
fi
