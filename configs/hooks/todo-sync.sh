#!/usr/bin/env bash
# todo-sync.sh - Sync Claude Code's TodoWrite with agentctl todo skill
#
# This hook triggers on PostToolUse for TodoWrite and syncs the todo list
# with agentctl's task management system.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "PostToolUse": [
#         {
#           "matcher": "TodoWrite",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/todo-sync.sh",
#               "timeout": 5
#             }
#           ]
#         }
#       ]
#     }
#   }

set -euo pipefail

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

# Get current agentctl tasks for mapping
current_tasks=$("$AGENTCTL_BIN" run todo/manage --input "{\"operation\":\"list\",\"workspace_id\":\"$workspace\"}" 2>/dev/null | jq -c '.data.tasks // []') || current_tasks="[]"

# Build a map of existing task titles to IDs
declare -A task_map
while IFS= read -r task; do
  title=$(printf '%s' "$task" | jq -r '.title')
  id=$(printf '%s' "$task" | jq -r '.id')
  task_map["$title"]="$id"
done < <(printf '%s' "$current_tasks" | jq -c '.[]')

# Process each Claude todo
synced=0
created=0
updated=0
completed=0

while IFS= read -r todo; do
  content=$(printf '%s' "$todo" | jq -r '.content // ""')
  status=$(printf '%s' "$todo" | jq -r '.status // "pending"')

  [[ -z "$content" ]] && continue

  # Check if task exists
  existing_id="${task_map[$content]:-}"

  if [[ -z "$existing_id" ]]; then
    # Create new task
    add_input=$(jq -nc --arg title "$content" --arg ws "$workspace" '{
      operation: "add",
      workspace_id: $ws,
      add: { title: $title, description: "Synced from Claude Code TodoWrite" }
    }')

    result=$(printf '%s' "$add_input" | "$AGENTCTL_BIN" run todo/manage --input-file - 2>/dev/null) || continue
    new_id=$(printf '%s' "$result" | jq -r '.data.task.id // ""')

    if [[ -n "$new_id" ]]; then
      task_map["$content"]="$new_id"
      existing_id="$new_id"
      ((created++)) || true
    fi
  fi

  # Update status if task exists
  if [[ -n "$existing_id" ]]; then
    cmd_success=false
    case "$status" in
      in_progress)
        # Set as active task
        active_input=$(jq -nc --arg id "$existing_id" --arg ws "$workspace" '{
          operation: "set_active",
          workspace_id: $ws,
          set_active: { task_id: $id }
        }')
        if printf '%s' "$active_input" | "$AGENTCTL_BIN" run todo/manage --input-file - &>/dev/null; then
          ((updated++)) || true
          cmd_success=true
        fi
        ;;
      completed)
        # Complete the task
        complete_input=$(jq -nc --arg id "$existing_id" --arg ws "$workspace" '{
          operation: "complete",
          workspace_id: $ws,
          complete: { id: $id, notes: "Completed via Claude Code" }
        }')
        if printf '%s' "$complete_input" | "$AGENTCTL_BIN" run todo/manage --input-file - &>/dev/null; then
          ((completed++)) || true
          cmd_success=true
        fi
        ;;
      *)
        # pending status - no update needed, still counts as synced
        cmd_success=true
        ;;
    esac
    if [[ "$cmd_success" == "true" ]]; then
      ((synced++)) || true
    fi
  fi
done < <(printf '%s' "$todos" | jq -c '.[]')

# Return approval with sync summary
if ((created > 0 || updated > 0 || completed > 0)); then
  summary="Synced $synced todos"
  [[ $created -gt 0 ]] && summary="$summary, created $created"
  [[ $updated -gt 0 ]] && summary="$summary, updated $updated"
  [[ $completed -gt 0 ]] && summary="$summary, completed $completed"

  jq -nc --arg summary "$summary" '{
    decision: "approve",
    context: ("**Todo Sync:** " + $summary)
  }'
else
  echo '{"decision":"approve"}'
fi
