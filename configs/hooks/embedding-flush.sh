#!/usr/bin/env bash
# Thin Stop hook embedding flush wrapper: delegate to Go.

set -euo pipefail

if [[ "${AGENTCTL_EMBED_QUEUE:-1}" == "0" ]]; then
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-foxctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
"$AGENTCTL_BIN" hooks embedding-flush --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
