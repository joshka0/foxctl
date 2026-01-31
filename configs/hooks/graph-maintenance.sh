#!/usr/bin/env bash
# graph-maintenance.sh - Consolidated Stop hook for graph maintenance (ASYNC)
#
# Combines functionality from:
#   - graph-cleanup.sh: Remove expired/dangling edges
#   - graph-pagerank.sh: Recalculate degrees and PageRank scores
#
# ASYNC: This hook immediately returns and runs maintenance in background.
# Stop hooks don't need to block - the session is ending anyway.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_GRAPH_MAINTENANCE_DISABLED - Set to "1" to disable all
#   AGENTCTL_GRAPH_CLEANUP_DISABLED - Set to "1" to skip cleanup
#   AGENTCTL_GRAPH_PAGERANK_DISABLED - Set to "1" to skip pagerank
#   AGENTCTL_GRAPH_MAINTENANCE_SYNC - Set to "1" for synchronous execution

set -euo pipefail

# Check if all maintenance is disabled
if [[ "${AGENTCTL_GRAPH_MAINTENANCE_DISABLED:-}" == "1" ]]; then
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

# Determine which operations to run
do_cleanup=true
do_pagerank=true

[[ "${AGENTCTL_GRAPH_CLEANUP_DISABLED:-}" == "1" ]] && do_cleanup=false
[[ "${AGENTCTL_GRAPH_PAGERANK_DISABLED:-}" == "1" ]] && do_pagerank=false

# If both are disabled, exit
if [[ "$do_cleanup" == false && "$do_pagerank" == false ]]; then
  exit 0
fi

# Build cleanup input (expired + dangling edges)
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

# Build degree recalc input
degree_input=$(jq -nc \
  --arg ws "$workspace" \
  '{
    operation: "cleanup",
    workspace: $ws,
    cleanup: {
      expired_edges: false,
      dangling_edges: false,
      recalc_degrees: true
    }
  }')

# Build PageRank input
pagerank_input=$(jq -nc --arg ws "$workspace" '{ workspace: $ws }')

# ASYNC: Run in background unless SYNC mode requested
if [[ "${AGENTCTL_GRAPH_MAINTENANCE_SYNC:-}" != "1" ]]; then
  LOG_DIR="${HOME}/.agentctl/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/graph-maintenance-$(date +%Y%m%d-%H%M%S).log"

  # Spawn in background and exit immediately
  (
    echo "=== Graph Maintenance $(date -u +%Y-%m-%dT%H:%M:%SZ) ===" >> "$LOG_FILE"
    echo "Workspace: $workspace" >> "$LOG_FILE"

    # 1. Cleanup expired and dangling edges
    if [[ "$do_cleanup" == true ]]; then
      echo "--- Cleanup ---" >> "$LOG_FILE"
      printf '%s' "$cleanup_input" | "$AGENTCTL_BIN" run --daemon graph/manage --input-file - >> "$LOG_FILE" 2>&1 || true
    fi

    # 2. Recalculate degrees and PageRank
    if [[ "$do_pagerank" == true ]]; then
      echo "--- Degrees ---" >> "$LOG_FILE"
      printf '%s' "$degree_input" | "$AGENTCTL_BIN" run --daemon graph/manage --input-file - >> "$LOG_FILE" 2>&1 || true

      echo "--- PageRank ---" >> "$LOG_FILE"
      printf '%s' "$pagerank_input" | "$AGENTCTL_BIN" run --daemon graph/pagerank --input-file - >> "$LOG_FILE" 2>&1 || true
    fi

    echo "=== Done ===" >> "$LOG_FILE"
  ) &
  disown
  exit 0
fi

# SYNC mode (for debugging)
if [[ "$do_cleanup" == true ]]; then
  printf '%s' "$cleanup_input" | "$AGENTCTL_BIN" run --daemon graph/manage --input-file - 2>/dev/null || true
fi

if [[ "$do_pagerank" == true ]]; then
  printf '%s' "$degree_input" | "$AGENTCTL_BIN" run --daemon graph/manage --input-file - 2>/dev/null || true
  printf '%s' "$pagerank_input" | "$AGENTCTL_BIN" run --daemon graph/pagerank --input-file - 2>/dev/null || true
fi

exit 0
