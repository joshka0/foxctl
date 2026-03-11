#!/usr/bin/env bash
# Thin Stop hook graph maintenance wrapper: delegate to Go.

set -euo pipefail

if [[ "${AGENTCTL_GRAPH_MAINTENANCE_DISABLED:-}" == "1" ]]; then
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
"$AGENTCTL_BIN" hooks graph-maintenance --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
