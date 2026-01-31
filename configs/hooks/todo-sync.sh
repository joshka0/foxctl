#!/usr/bin/env bash
# todo-sync.sh - Sync Claude Code's TodoWrite with agentctl todo skill
# Last tested: 2026-01-14
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

# Session ID detection - PRIORITIZE payload over cached files
# Claude Code sends sessionID in PostToolUse payload
SESSION_ID="$(printf '%s' "$payload" | jq -r '.sessionID // .session_id // ""' 2>/dev/null || true)"

# Fall back to env vars
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  SESSION_ID="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-${OPENCODE_SESSION_ID:-}}}"
fi

# Last resort: check identity files (may be stale)
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

# Update identity file if we got session from payload (keeps cache fresh)
if [[ -n "$SESSION_ID" && "$SESSION_ID" != "null" ]]; then
  agentctl_home="${AGENTCTL_HOME:-$HOME/.agentctl}"
  workspace_hash="$(printf '%s' "$workspace" | shasum -a 256 | cut -c1-16)"
  identity_dir="$agentctl_home/sessions/active"
  identity_file="$identity_dir/${workspace_hash}-claude.json"
  mkdir -p "$identity_dir"
  # Update if file doesn't exist or has different session_id
  current_sid="$(jq -r '.session_id // ""' "$identity_file" 2>/dev/null || echo '')"
  if [[ "$current_sid" != "$SESSION_ID" ]]; then
    jq -nc \
      --arg sid "$SESSION_ID" \
      --arg ws "$workspace" \
      --arg wsh "$workspace_hash" \
      '{session_id: $sid, agent_id: "claude", provider: "claude", workspace: $ws, workspace_hash: $wsh, started_at: (now | todate), last_activity: (now | todate), detected_from: "hook_payload"}' \
      > "$identity_file"
    [[ -n "$DEBUG" ]] && echo "DEBUG: updated identity file with session $SESSION_ID" >&2
  fi
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
result=$(printf '%s' "$sync_input" | "$AGENTCTL_BIN" run --daemon todo/sync_from_provider --input-file - 2>"$sync_stderr") || {
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
removed=$(printf '%s' "$result" | jq -r '.data.removed // 0')
mapped=$(printf '%s' "$result" | jq -r '.data.mapped // 0')
deps_added=$(printf '%s' "$result" | jq -r '.data.deps_added // 0')
warnings=$(printf '%s' "$result" | jq -r '.data.warnings // [] | join("; ")')

# Queue task embeddings in background if any tasks were created/updated
if ((created > 0 || updated > 0)); then
  embed_input=$(jq -nc --arg ws "$workspace" '{scope: "workspace", workspace_id: $ws}')
  ("$AGENTCTL_BIN" run --daemon embedding/tasks --input "$embed_input" 2>/dev/null &) || true
  [[ -n "$DEBUG" ]] && echo "DEBUG: queued task embeddings for workspace" >&2
fi

# Build response
context_parts=()

total=$((created + updated + completed + removed))
if ((total > 0 || mapped > 0)); then
  summary="Synced $((mapped + created)) todos"
  [[ $created -gt 0 ]] && summary="$summary, created $created"
  [[ $updated -gt 0 ]] && summary="$summary, updated $updated"
  [[ $completed -gt 0 ]] && summary="$summary, completed $completed"
  [[ $removed -gt 0 ]] && summary="$summary, removed $removed"
  [[ $deps_added -gt 0 ]] && summary="$summary, $deps_added deps inferred"
  context_parts+=("**Todo Sync:** $summary")
fi

# Add warnings if any
if [[ -n "$warnings" && "$warnings" != "null" ]]; then
  context_parts+=("**Warnings:** $warnings")
fi

# Link newly created tasks to active epic (if any)
# Validate created is a positive integer (defense in depth)
if [[ "$created" =~ ^[0-9]+$ ]] && ((created > 0)); then
  db_path="${AGENTCTL_HOME:-$HOME/.agentctl}/storage/tasks.db"
  if [[ -f "$db_path" ]] && [[ -n "$SESSION_ID" && "$SESSION_ID" != "null" ]]; then
    # Escape single quotes for SQL safety
    workspace_safe="${workspace//\'/\'\'}"
    session_id_safe="${SESSION_ID//\'/\'\'}"
    # Check for active epic in this workspace+session
    active_epic=$(sqlite3 "$db_path" "SELECT epic_id FROM active_epics WHERE workspace_id = '$workspace_safe' AND session_id = '$session_id_safe'" 2>/dev/null || true)
    if [[ -n "$active_epic" && "$active_epic" != "" ]]; then
      # Escape epic_id for SQL safety
      active_epic_safe="${active_epic//\'/\'\'}"
      # Link all tasks without an epic_id in this workspace to the active epic
      linked=$(sqlite3 "$db_path" "
        UPDATE tasks SET epic_id = '$active_epic_safe'
        WHERE workspace_id = '$workspace_safe'
          AND (epic_id IS NULL OR epic_id = '')
          AND id IN (
            SELECT id FROM tasks
            WHERE workspace_id = '$workspace_safe'
            ORDER BY created_at DESC
            LIMIT $created
          );
        SELECT changes();
      " 2>/dev/null || echo "0")
      if [[ "$linked" -gt 0 ]]; then
        context_parts+=("**Epic:** Linked $linked tasks to active epic")
        [[ -n "$DEBUG" ]] && echo "DEBUG: linked $linked tasks to epic $active_epic" >&2
      fi
    fi
  fi
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
  outbound_result=$(printf '%s' "$outbound_input" | "$AGENTCTL_BIN" run --daemon todo/sync_to_provider --input-file - 2>/dev/null) || true

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
