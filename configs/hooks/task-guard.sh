#!/usr/bin/env bash
# task-guard.sh - Claude Code PreToolUse hook wrapper for hooks/task_guard skill
#
# This script wraps the agentctl hooks/task_guard skill for use as a Claude Code hook.
# It reads the hook event from stdin, calls agentctl, and extracts the hook_output.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "PreToolUse": [
#         {
#           "matcher": "Edit|Write|MultiEdit|NotebookEdit",
#           "hooks": ["$CLAUDE_PROJECT_DIR/.claude/hooks/task-guard.sh"]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN           - Path to agentctl binary (default: agentctl)
#   AGENTCTL_TASK_GUARD_MODE - Mode: 'auto' (default) or 'strict'

set -euo pipefail

# Configuration
AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"

# Read hook input from stdin
payload="$(cat)"

# Extract workspace root from CLAUDE_PROJECT_DIR or derive from tool input
workspace_root="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Transform Claude hook input to agentctl hook.Input format
# Claude provides: tool_name, tool_input, session_id, etc.
hook_input=$(printf '%s' "$payload" | jq -c --arg ws "$workspace_root" '{
  event: "PreToolUse",
  workspace_root: $ws,
  session_id: (.session_id // ""),
  tool_name: (.tool_name // ""),
  tool_input: (.tool_input // {})
}')

# Call agentctl skill
result="$(printf '%s' "$hook_input" | "$AGENTCTL_BIN" run hooks/task_guard --input-file - 2>/dev/null)" || {
  # On error, allow the operation to proceed (fail-open)
  echo '{}' 
  exit 0
}

# Extract hook_output from envelope data
hook_output="$(printf '%s' "$result" | jq -c '.data.hook_output // {}')"

# Check decision and render JSON via jq to ensure proper escaping
decision="$(printf '%s' "$hook_output" | jq -r '.decision // "approve"')"

if [[ "$decision" == "block" ]]; then
	printf '%s\n' "$hook_output" | jq -c '{
	  decision: "block",
	  reason: (.reason // "Operation blocked by task guard")
	}'
else
	printf '%s\n' "$hook_output" | jq -c '
	  if (.context // "") != "" then
	    { decision: "approve", context: .context }
	  else
	    {}
	  end
	'
fi
