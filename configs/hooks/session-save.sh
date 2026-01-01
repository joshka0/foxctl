#!/usr/bin/env bash
# session-save.sh - Claude Code PreCompact hook wrapper for session/save skill
#
# This script captures session state before context compaction, enabling
# session continuity across compactions.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "PreCompact": [
#         {
#           "matcher": "auto|manual",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-save.sh"
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
  # Can't find agentctl - fail silently to not block compaction
  echo '{}'
  exit 0
fi

# Read hook input from stdin
payload="$(cat)"

# Extract trigger type from the hook input
# PreCompact provides: { trigger: "auto"|"manual", custom_instructions?: string }
trigger=$(printf '%s' "$payload" | jq -r '.trigger // "auto"')

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Session ID if available
session_id="${CLAUDE_SESSION_ID:-}"

# Build skill input
skill_input=$(jq -nc \
  --arg trigger "$trigger" \
  --arg workspace "$workspace" \
  --arg session_id "$session_id" \
  '{
    trigger: ("pre_compact"),
    workspace: $workspace,
    session_id: $session_id
  }'
)

# Call session/save skill with --ephemeral for faster execution
result="$(printf '%s' "$skill_input" | "$AGENTCTL_BIN" run session/save --ephemeral --input-file - 2>/dev/null)" || {
  # On error, don't block compaction
  echo '{}'
  exit 0
}

# PreCompact hook doesn't need to return anything special
# Just acknowledge success
echo '{}'
