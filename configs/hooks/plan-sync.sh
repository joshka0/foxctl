#!/usr/bin/env bash
# Stop hook: Sync Claude Code plans before session ends (ASYNC)
#
# ASYNC: This hook returns immediately and syncs plans in background.
# Stop hooks don't need to block - the session is ending anyway.
#
# Environment:
#   AGENTCTL_PLAN_SYNC_DISABLED - Set to "1" to disable
#   AGENTCTL_PLAN_SYNC_SYNC - Set to "1" for synchronous execution

set -euo pipefail

# Check if disabled
if [[ "${AGENTCTL_PLAN_SYNC_DISABLED:-}" == "1" ]]; then
  exit 0
fi

# agentctl binary location
AGENTCTL="${AGENTCTL_BIN:-$(dirname "$0")/../../bin/agentctl}"
if [[ ! -x "$AGENTCTL" ]]; then
  AGENTCTL="agentctl"
fi

# Read hook input from stdin (JSON with event, session_id, etc.)
INPUT=$(cat)

# Extract workspace from hook input (session_cwd or current directory)
WORKSPACE=$(echo "$INPUT" | jq -r '.session_cwd // empty' 2>/dev/null)
if [[ -z "$WORKSPACE" ]]; then
  WORKSPACE="$(pwd)"
fi

SYNC_INPUT=$(jq -n \
  --arg workspace "$WORKSPACE" \
  '{workspace: $workspace, import_tasks: false}'
)

# ASYNC: Run in background unless SYNC mode requested
if [[ "${AGENTCTL_PLAN_SYNC_SYNC:-}" != "1" ]]; then
  LOG_DIR="${HOME}/.agentctl/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/plan-sync-$(date +%Y%m%d-%H%M%S).log"

  # Spawn in background and exit immediately
  (
    "$AGENTCTL" run plan/sync --input "$SYNC_INPUT" >> "$LOG_FILE" 2>&1
  ) &
  disown
  exit 0
fi

# SYNC mode
exec "$AGENTCTL" run plan/sync --input "$SYNC_INPUT"
