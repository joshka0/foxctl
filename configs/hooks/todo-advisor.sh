#!/usr/bin/env bash
# todo-advisor.sh - PreToolUse advisory for TodoWrite
#
# Provides context about agentctl todo integration when Claude uses TodoWrite.

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

# Read hook input (discard - we just need to provide context)
cat >/dev/null

# Get workspace
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Get current agentctl task count
task_info=$("$AGENTCTL_BIN" run todo/manage --input "{\"operation\":\"list\",\"workspace_id\":\"$workspace\"}" 2>/dev/null) || task_info="{}"
pending=$(printf '%s' "$task_info" | jq -r '.data.pending_tasks // 0')
active_task=$(printf '%s' "$task_info" | jq -r '.data.tasks[] | select(.status == "in_progress") | .title' | head -1)

# Build advisory context
context="**agentctl tasks:** $pending pending"
if [[ -n "$active_task" ]]; then
  context="$context | Active: \"$active_task\""
fi
context="$context
Use \`/agentctl-todo\` to sync with agentctl or \`agentctl todo list\` to see all tasks."

jq -nc --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
