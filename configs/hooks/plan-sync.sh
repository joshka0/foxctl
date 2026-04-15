#!/usr/bin/env bash
# Thin Stop hook plan sync wrapper: delegate to Go.

set -euo pipefail

if [[ "${FOXCTL_PLAN_SYNC_DISABLED:-}" == "1" ]]; then
  exit 0
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"
printf '%s' "$payload" | "$FOXCTL_BIN" hooks plan-sync --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
