#!/usr/bin/env bash
# graph-cleanup.sh - Stop hook to clean up the dependency graph
#
# Runs at session end to perform graph maintenance:
#   - Remove expired edges (past TTL)
#   - Remove dangling edges (pointing to non-existent nodes)
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_GRAPH_CLEANUP_DISABLED - Set to "1" to disable
#   AGENTCTL_GRAPH_CLEANUP_DEBUG - Set to "1" for debug output

set -euo pipefail

DEBUG="${AGENTCTL_GRAPH_CLEANUP_DEBUG:-}"

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

[[ -n "$DEBUG" ]] && echo "DEBUG: Running graph cleanup for workspace: $workspace" >&2

# Run cleanup: expired + dangling edges
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

if result=$(printf '%s' "$cleanup_input" | "$AGENTCTL_BIN" run graph/manage --input-file - 2>/dev/null); then
  expired=$(printf '%s' "$result" | jq -r '.data.expired_edges_removed // 0')
  dangling=$(printf '%s' "$result" | jq -r '.data.dangling_edges_removed // 0')

  total=$((expired + dangling))

  if [[ $total -gt 0 ]]; then
    [[ -n "$DEBUG" ]] && echo "DEBUG: Cleaned up $expired expired + $dangling dangling edges" >&2
  fi
else
  [[ -n "$DEBUG" ]] && echo "DEBUG: Graph cleanup failed" >&2
fi

# Stop hooks don't return JSON, just exit cleanly
exit 0
