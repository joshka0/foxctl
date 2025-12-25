#!/usr/bin/env bash
# session-restore.sh - Claude Code SessionStart hook wrapper for session/restore skill
#
# This script restores session state after compaction or when resuming a session,
# enabling session continuity.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "SessionStart": [
#         {
#           "matcher": "compact|resume",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-restore.sh"
#             }
#           ]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary (default: searches PATH, then project bin/)

set -euo pipefail

# Find agentctl binary
if [[ -n "${AGENTCTL_BIN:-}" ]]; then
  : # Use provided path
elif command -v agentctl &>/dev/null; then
  AGENTCTL_BIN="agentctl"
elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
  AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
else
  # Can't find agentctl - fail silently
  echo '{}'
  exit 0
fi

# Read hook input from stdin
payload="$(cat)"

# Extract trigger source from the hook input
# SessionStart provides: { source: "startup"|"resume"|"clear"|"compact" }
trigger=$(printf '%s' "$payload" | jq -r '.source // "compact"')

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Build skill input
skill_input=$(jq -nc \
  --arg trigger "$trigger" \
  --arg workspace "$workspace" \
  '{
    trigger: $trigger,
    workspace: $workspace
  }'
)

# Call session/restore skill
result="$(printf '%s' "$skill_input" | "$AGENTCTL_BIN" run session/restore --input-file - 2>/dev/null)" || {
  # On error, proceed without restored context
  echo '{}'
  exit 0
}

# Extract hook_output from envelope
hook_output="$(printf '%s' "$result" | jq -c '.data.hook_output // {}')"

# Check if we have context to inject
context=$(printf '%s' "$hook_output" | jq -r '.context // ""')

if [[ -n "$context" && "$context" != "null" ]]; then
  # Return the context for injection
  printf '%s' "$hook_output" | jq -c '{
    context: .context
  }'
else
  # No context to inject
  echo '{}'
fi
