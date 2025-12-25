#!/usr/bin/env bash
# session-capture.sh - Claude Code Stop hook for capturing conversation sessions
#
# This script captures the Claude Code conversation session and stores it in
# agentctl's sessions.db. It extracts high-signal content for later recall.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "Stop": [
#         {
#           "matcher": "",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-capture.sh",
#               "timeout": 10
#             }
#           ]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_SESSION_CAPTURE_DISABLED - Set to "1" to disable capture
#   CEREBRAS_API_KEY - Required for summarization (optional)

set -euo pipefail

# Check if capture is disabled
if [[ "${AGENTCTL_SESSION_CAPTURE_DISABLED:-}" == "1" ]]; then
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
  # Can't find agentctl - exit silently
  echo '{}'
  exit 0
fi

# Read and discard hook input from stdin (Stop hook provides minimal data)
cat >/dev/null

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Session ID if available
session_id="${CLAUDE_SESSION_ID:-}"

# Build skill input for capture
capture_input=$(jq -nc \
  --arg workspace "$workspace" \
  --arg session_id "$session_id" \
  '{
    workspace: $workspace,
    session_id: (if $session_id != "" then $session_id else null end)
  }'
)

# Call session/capture skill
capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run session/capture --input-file - 2>/dev/null)" || {
  # On error, don't block session stop
  echo '{}'
  exit 0
}

# Check if capture was successful
capture_status=$(printf '%s' "$capture_result" | jq -r '.data.status // "error"')
captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""')

if [[ "$capture_status" != "captured" && "$capture_status" != "exists" ]]; then
  # Capture failed, exit silently
  echo '{}'
  exit 0
fi

# If CEREBRAS_API_KEY is set, also summarize
if [[ -n "${CEREBRAS_API_KEY:-}" && -n "$captured_session_id" ]]; then
  summarize_input=$(jq -nc \
    --arg session_id "$captured_session_id" \
    '{
      session_id: $session_id
    }'
  )

  # Call session/summarize skill (async, don't wait)
  printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run session/summarize --input-file - &>/dev/null &
fi

# Stop hook doesn't need special output
echo '{}'
