#!/usr/bin/env bash
# todo-sync.sh - Sync Claude Code's TodoWrite with agentctl todo skill
#
# This hook triggers on PostToolUse for TodoWrite and syncs the todo list
# with agentctl's task management system.
#
# Features:
#   - Syncs todos to agentctl task store
#   - Auto-infers dependencies based on task order and context
#   - Creates dependency edges in the graph
#   - Warns about potential missing dependencies
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_TODO_SYNC_DISABLED - Set to "1" to disable
#   AGENTCTL_TODO_SYNC_DEBUG - Set to "1" for debug output

set -euo pipefail

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

# Get current agentctl tasks for mapping
current_tasks=$("$AGENTCTL_BIN" run todo/manage --input "{\"operation\":\"list\",\"workspace_id\":\"$workspace\"}" 2>/dev/null | jq -c '.data.tasks // []') || current_tasks="[]"

# Build maps of existing tasks
declare -A task_map      # title -> id
declare -A task_status   # id -> status
declare -A task_order    # title -> order in current list

order=0
while IFS= read -r task; do
  title=$(printf '%s' "$task" | jq -r '.title')
  id=$(printf '%s' "$task" | jq -r '.id')
  status=$(printf '%s' "$task" | jq -r '.status')
  task_map["$title"]="$id"
  task_status["$id"]="$status"
  task_order["$title"]=$order
  ((order++)) || true
done < <(printf '%s' "$current_tasks" | jq -c '.[]')

# Process each Claude todo - first pass to collect all titles and their order
declare -a todo_titles
declare -A todo_statuses
todo_index=0
while IFS= read -r todo; do
  content=$(printf '%s' "$todo" | jq -r '.content // ""')
  status=$(printf '%s' "$todo" | jq -r '.status // "pending"')
  [[ -z "$content" ]] && continue
  todo_titles+=("$content")
  todo_statuses["$content"]="$status"
  ((todo_index++)) || true
done < <(printf '%s' "$todos" | jq -c '.[]')

# Process each Claude todo - second pass to create/update with dependencies
synced=0
created=0
updated=0
completed=0
deps_created=0
dep_warnings=()

for i in "${!todo_titles[@]}"; do
  content="${todo_titles[$i]}"
  status="${todo_statuses[$content]}"

  # Check if task exists
  existing_id="${task_map[$content]:-}"

  if [[ -z "$existing_id" ]]; then
    # Infer dependencies: tasks that come before this one in the list and are pending
    depends_on=()
    for j in "${!todo_titles[@]}"; do
      if ((j < i)); then
        prev_title="${todo_titles[$j]}"
        prev_status="${todo_statuses[$prev_title]}"
        prev_id="${task_map[$prev_title]:-}"

        # If previous task exists and is not completed, it's a potential dependency
        if [[ -n "$prev_id" && "$prev_status" != "completed" ]]; then
          depends_on+=("$prev_id")
        fi
      fi
    done

    # Build depends_on JSON array
    deps_json="[]"
    if [[ ${#depends_on[@]} -gt 0 ]]; then
      deps_json=$(printf '%s\n' "${depends_on[@]}" | jq -R . | jq -s .)
    fi

    # Create new task with dependencies
    add_input=$(jq -nc \
      --arg title "$content" \
      --arg ws "$workspace" \
      --argjson deps "$deps_json" \
      '{
        operation: "add",
        workspace_id: $ws,
        add: {
          title: $title,
          description: "Synced from Claude Code TodoWrite",
          depends_on: $deps
        }
      }')

    [[ -n "$DEBUG" ]] && echo "DEBUG: Creating task: $add_input" >&2

    result=$(printf '%s' "$add_input" | "$AGENTCTL_BIN" run todo/manage --input-file - 2>/dev/null) || continue
    new_id=$(printf '%s' "$result" | jq -r '.data.task.id // ""')

    if [[ -n "$new_id" ]]; then
      task_map["$content"]="$new_id"
      existing_id="$new_id"
      ((created++)) || true

      if [[ ${#depends_on[@]} -gt 0 ]]; then
        ((deps_created += ${#depends_on[@]})) || true
      fi
    fi
  else
    # Task exists - check if it should have dependencies on newer tasks
    # (tasks added after it that are still pending)
    for j in "${!todo_titles[@]}"; do
      if ((j < i)); then
        prev_title="${todo_titles[$j]}"
        prev_id="${task_map[$prev_title]:-}"
        prev_stored_status="${task_status[$prev_id]:-}"

        # If previous task is incomplete, warn about potential missing dependency
        if [[ -n "$prev_id" && "$prev_stored_status" != "done" ]]; then
          # Check if dependency already exists by querying the task
          task_data=$("$AGENTCTL_BIN" run todo/manage --input "{\"operation\":\"get\",\"workspace_id\":\"$workspace\",\"get\":{\"id\":\"$existing_id\"}}" 2>/dev/null) || continue
          existing_deps=$(printf '%s' "$task_data" | jq -r '.data.task.depends_on // [] | .[]' 2>/dev/null) || existing_deps=""

          if ! echo "$existing_deps" | grep -q "^${prev_id}$"; then
            dep_warnings+=("Task '$content' may depend on incomplete '$prev_title'")
          fi
        fi
      fi
    done
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
done

# Build response
context_parts=()

if ((created > 0 || updated > 0 || completed > 0)); then
  summary="Synced $synced todos"
  [[ $created -gt 0 ]] && summary="$summary, created $created"
  [[ $updated -gt 0 ]] && summary="$summary, updated $updated"
  [[ $completed -gt 0 ]] && summary="$summary, completed $completed"
  [[ $deps_created -gt 0 ]] && summary="$summary, $deps_created deps inferred"
  context_parts+=("**Todo Sync:** $summary")
fi

# Add dependency warnings
if [[ ${#dep_warnings[@]} -gt 0 ]]; then
  warning_text="**Dependency hints:** "
  for w in "${dep_warnings[@]}"; do
    warning_text="$warning_text\n- $w"
  done
  context_parts+=("$warning_text")
fi

if [[ ${#context_parts[@]} -gt 0 ]]; then
  context=$(printf '%s\n' "${context_parts[@]}" | tr '\n' ' ')
  jq -nc --arg ctx "$context" '{decision: "approve", context: $ctx}'
else
  echo '{"decision":"approve"}'
fi
