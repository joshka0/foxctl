#!/usr/bin/env bash
# Thin PreToolUse memory recall wrapper: delegate file-memory lookup to Go.

set -euo pipefail

if [[ "${AGENTCTL_FILE_RECALL_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

if ! output="$(printf '%s' "$payload" | "$AGENTCTL_BIN" hooks memory-recall --workspace "$workspace" 2>/dev/null)"; then
  echo '{}'
  exit 0
fi

context="$(printf '%s' "$output" | jq -r '.data.response.context // ""' 2>/dev/null || echo "")"
if [[ -n "$context" && "$context" != "null" ]]; then
  jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
else
  echo '{}'
fi
