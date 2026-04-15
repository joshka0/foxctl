#!/usr/bin/env bash
# Thin stop-time todo continuation wrapper: delegate gating logic to Go.

set -euo pipefail

if [[ "${FOXCTL_TODO_CONTINUATION_DISABLED:-}" == "1" ]]; then
  exit 0
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  echo '{"decision":"approve"}'
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

if ! output="$(printf '%s' "$payload" | "$FOXCTL_BIN" hooks todo-continuation --workspace "$workspace" 2>/dev/null)"; then
  echo '{"decision":"approve"}'
  exit 0
fi

response="$(printf '%s' "$output" | jq -c '.data.response // {}' 2>/dev/null || echo '{}')"
decision="$(printf '%s' "$response" | jq -r '.decision // "approve"' 2>/dev/null || echo 'approve')"

if [[ "$decision" == "block" ]]; then
  printf '%s\n' "$response" | jq -c '{
    decision: "block",
    reason: (.reason // "Incomplete tasks remain"),
    inject_prompt: (.inject_prompt // null)
  }'
else
  printf '%s\n' "$response" | jq -c '
    if (.warning // "") != "" then
      { decision: "approve", warning: .warning }
    else
      { decision: "approve" }
    end
  '
fi
