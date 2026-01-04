#!/usr/bin/env bash
# graph-cleanup.sh - Stop hook to clean up the dependency graph (ASYNC)
#
# Runs at session end to perform graph maintenance:
#   - Remove expired edges (past TTL)
#   - Remove dangling edges (pointing to non-existent nodes)
#
# ASYNC: This hook immediately returns and runs cleanup in background.
# Stop hooks don't need to block - the session is ending anyway.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_GRAPH_CLEANUP_DISABLED - Set to "1" to disable
#   AGENTCTL_GRAPH_CLEANUP_SYNC - Set to "1" to force synchronous execution

set -euo pipefail

# Check if disabled
if [[ "${AGENTCTL_GRAPH_CLEANUP_DISABLED:-}" == "1" ]]; then
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

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Build cleanup input
cleanup_input=$(jq -nc \
  --arg ws "$workspace" \
  '{
    operation: "cleanup",
    workspace: $ws,
    cleanup: {
      expired_edges: true,
      dangling_edges: true,
      recalc_degrees: false
    }
  }')

# ASYNC: Run in background unless SYNC mode requested
if [[ "${AGENTCTL_GRAPH_CLEANUP_SYNC:-}" != "1" ]]; then
  LOG_DIR="${HOME}/.agentctl/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/graph-cleanup-$(date +%Y%m%d-%H%M%S).log"

  # Spawn in background and exit immediately
  (
    printf '%s' "$cleanup_input" | "$AGENTCTL_BIN" run graph/manage --input-file - >> "$LOG_FILE" 2>&1
  ) &
  disown
  exit 0
fi

# SYNC mode (for debugging)
printf '%s' "$cleanup_input" | "$AGENTCTL_BIN" run graph/manage --input-file - 2>/dev/null || true
exit 0
