#!/usr/bin/env bash
# Thin Stop hook plan sync wrapper: delegate to Go.

set -euo pipefail

if [[ "${AGENTCTL_PLAN_SYNC_DISABLED:-}" == "1" ]]; then
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-foxctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"
printf '%s' "$payload" | "$AGENTCTL_BIN" hooks plan-sync --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
