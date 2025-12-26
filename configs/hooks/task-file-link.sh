#!/usr/bin/env bash
# task-file-link.sh - PostToolUse hook to link edited files to active task
#
# Creates a "modifies" edge in the dependency graph connecting the active task
# to any file edited via Edit/Write tools. This enables:
#   - Tracking which files were touched for each task
#   - Showing file context when viewing task history
#   - PageRank propagation through task→file relationships
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_TASK_FILE_LINK_DISABLED - Set to "1" to disable
#   AGENTCTL_TASK_FILE_LINK_DEBUG - Set to "1" for debug output

set -euo pipefail

DEBUG="${AGENTCTL_TASK_FILE_LINK_DEBUG:-}"

# Check if disabled
if [[ "${AGENTCTL_TASK_FILE_LINK_DISABLED:-}" == "1" ]]; then
  echo '{}'
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
  echo '{}'
  exit 0
fi

# Read hook input from stdin
payload="$(cat)"

# Extract file_path from tool_input
file_path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // ""')

if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Normalize to absolute path
if [[ "$file_path" != /* ]]; then
  file_path="${CLAUDE_PROJECT_DIR:-$(pwd)}/$file_path"
fi

# Make path canonical (resolve symlinks, ..)
if [[ -e "$file_path" ]]; then
  file_path=$(cd "$(dirname "$file_path")" && pwd)/$(basename "$file_path")
fi

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Get relative path from workspace
rel_path="${file_path#$workspace/}"

# Get active task
active_result=$("$AGENTCTL_BIN" todo active 2>/dev/null) || active_result=""

if [[ -z "$active_result" ]]; then
  [[ -n "$DEBUG" ]] && echo "DEBUG: No active task, skipping file link" >&2
  echo '{}'
  exit 0
fi

task_id=$(printf '%s' "$active_result" | jq -r '.data.task.id // ""')
task_title=$(printf '%s' "$active_result" | jq -r '.data.task.title // ""')

if [[ -z "$task_id" || "$task_id" == "null" ]]; then
  [[ -n "$DEBUG" ]] && echo "DEBUG: Could not extract task ID" >&2
  echo '{}'
  exit 0
fi

[[ -n "$DEBUG" ]] && echo "DEBUG: Linking file '$rel_path' to task '$task_title' ($task_id)" >&2

# Create file node (upsert)
file_node_input=$(jq -nc \
  --arg path "$rel_path" \
  --arg ws "$workspace" \
  '{
    operation: "add_node",
    workspace: $ws,
    add_node: {
      node_id: $path,
      node_type: "file",
      title: $path,
      current_path: $path,
      metadata: {}
    }
  }')

printf '%s' "$file_node_input" | "$AGENTCTL_BIN" run graph/manage --input-file - &>/dev/null || true

# Create task node (upsert) - ensures task exists in graph
task_node_input=$(jq -nc \
  --arg id "$task_id" \
  --arg title "$task_title" \
  --arg ws "$workspace" \
  '{
    operation: "add_node",
    workspace: $ws,
    add_node: {
      node_id: $id,
      node_type: "task",
      title: $title,
      metadata: {}
    }
  }')

printf '%s' "$task_node_input" | "$AGENTCTL_BIN" run graph/manage --input-file - &>/dev/null || true

# Create "modifies" edge from task to file
edge_input=$(jq -nc \
  --arg task_id "$task_id" \
  --arg file_path "$rel_path" \
  --arg ws "$workspace" \
  '{
    operation: "add_edge",
    workspace: $ws,
    add_edge: {
      from_id: $task_id,
      from_type: "task",
      to_id: $file_path,
      to_type: "file",
      edge_type: "modifies",
      weight: 1.0,
      ttl_days: 90,
      metadata: {}
    }
  }')

if printf '%s' "$edge_input" | "$AGENTCTL_BIN" run graph/manage --input-file - &>/dev/null; then
  [[ -n "$DEBUG" ]] && echo "DEBUG: Created modifies edge" >&2
  # Return context so Claude knows the link was created
  jq -nc --arg file "$rel_path" --arg task "$task_title" '{
    decision: "approve",
    context: ("**Graph:** Linked `" + $file + "` → task \"" + $task + "\"")
  }'
else
  [[ -n "$DEBUG" ]] && echo "DEBUG: Failed to create edge" >&2
  echo '{}'
fi
