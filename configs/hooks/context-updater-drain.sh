#!/usr/bin/env bash
# Thin UserPromptSubmit context updater drain wrapper: delegate to Go.

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-foxctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  echo '{"decision":"approve"}'
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

if ! output="$(printf '%s' "$payload" | "$AGENTCTL_BIN" hooks context-updater-drain --workspace "$workspace" 2>/dev/null)"; then
  echo '{"decision":"approve"}'
  exit 0
fi

context="$(printf '%s' "$output" | jq -r '.data.response.context // ""' 2>/dev/null || echo "")"
if [[ -n "$context" && "$context" != "null" ]]; then
  jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
else
  echo '{"decision":"approve"}'
fi
