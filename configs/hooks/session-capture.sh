#!/usr/bin/env bash
# session-capture.sh - Claude Code Stop hook for capturing conversation sessions (ASYNC)
#
# ASYNC: This hook returns immediately and captures session in background.
# Stop hooks don't need to block - the session is ending anyway.
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_SESSION_CAPTURE_DISABLED - Set to "1" to disable capture
#   AGENTCTL_SESSION_CAPTURE_SYNC - Set to "1" for synchronous execution
#   CEREBRAS_API_KEY - Required for summarization (optional)

set -euo pipefail

# Check if capture is disabled
if [[ "${AGENTCTL_SESSION_CAPTURE_DISABLED:-}" == "1" ]]; then
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
  exit 0
fi

# Read and discard hook input from stdin
cat >/dev/null

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
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

# ASYNC: Run in background unless SYNC mode requested
if [[ "${AGENTCTL_SESSION_CAPTURE_SYNC:-}" != "1" ]]; then
  LOG_DIR="${HOME}/.agentctl/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/session-capture-$(date +%Y%m%d-%H%M%S).log"

  # Spawn in background and exit immediately
  (
    capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run session/capture --input-file - 2>&1)" || true
    echo "$capture_result" >> "$LOG_FILE"

    # If CEREBRAS_API_KEY is set, also summarize
    if [[ -n "${CEREBRAS_API_KEY:-}" ]]; then
      captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""' 2>/dev/null)
      if [[ -n "$captured_session_id" ]]; then
        summarize_input=$(jq -nc --arg session_id "$captured_session_id" '{session_id: $session_id}')
        printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run session/summarize --input-file - >> "$LOG_FILE" 2>&1 || true
      fi
    fi
  ) &
  disown
  exit 0
fi

# SYNC mode: Original blocking behavior
capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run session/capture --input-file - 2>/dev/null)" || exit 0

capture_status=$(printf '%s' "$capture_result" | jq -r '.data.status // "error"')
captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""')

if [[ "$capture_status" != "captured" && "$capture_status" != "exists" ]]; then
  exit 0
fi

# If CEREBRAS_API_KEY is set, also summarize
if [[ -n "${CEREBRAS_API_KEY:-}" && -n "$captured_session_id" ]]; then
  summarize_input=$(jq -nc --arg session_id "$captured_session_id" '{session_id: $session_id}')
  printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run session/summarize --input-file - &>/dev/null &
fi

exit 0
