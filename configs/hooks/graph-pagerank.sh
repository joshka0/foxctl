#!/usr/bin/env bash
# graph-pagerank.sh - Stop hook to recalculate graph metrics
#
# Runs at session end to recalculate node degrees and (when available)
# PageRank scores for the workspace's dependency graph.
#
# Currently performs:
#   - Recalculate in_degree/out_degree for all nodes
#   - Report graph stats
#
# TODO: Add actual PageRank computation when graph/pagerank skill is added
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_GRAPH_PAGERANK_DISABLED - Set to "1" to disable
#   AGENTCTL_GRAPH_PAGERANK_DEBUG - Set to "1" for debug output

set -euo pipefail

DEBUG="${AGENTCTL_GRAPH_PAGERANK_DEBUG:-}"

# Check if disabled
if [[ "${AGENTCTL_GRAPH_PAGERANK_DISABLED:-}" == "1" ]]; then
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

[[ -n "$DEBUG" ]] && echo "DEBUG: Recalculating graph metrics for workspace: $workspace" >&2

# Recalculate degrees via cleanup operation
cleanup_input=$(jq -nc \
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

if result=$(printf '%s' "$cleanup_input" | "$AGENTCTL_BIN" run graph/manage --input-file - 2>/dev/null); then
  [[ -n "$DEBUG" ]] && echo "DEBUG: Degree recalculation complete" >&2

  # Get updated stats
  stats_input=$(jq -nc --arg ws "$workspace" '{operation: "stats", workspace: $ws}')
  stats=$(printf '%s' "$stats_input" | "$AGENTCTL_BIN" run graph/manage --input-file - 2>/dev/null) || stats=""

  if [[ -n "$stats" ]]; then
    node_count=$(printf '%s' "$stats" | jq -r '.data.total_nodes // 0')
    edge_count=$(printf '%s' "$stats" | jq -r '.data.total_edges // 0')
    avg_pagerank=$(printf '%s' "$stats" | jq -r '.data.avg_pagerank // 0')

    [[ -n "$DEBUG" ]] && echo "DEBUG: Graph stats - nodes: $node_count, edges: $edge_count, avg_pr: $avg_pagerank" >&2
  fi
else
  [[ -n "$DEBUG" ]] && echo "DEBUG: Graph cleanup failed" >&2
fi

# Stop hooks don't return JSON, just exit cleanly
exit 0
