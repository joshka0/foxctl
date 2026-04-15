#!/usr/bin/env bash
# Thin SessionStart wrapper: delegate lifecycle orchestration to Go.

set -euo pipefail

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"
source_name="$(printf '%s' "$payload" | jq -r '.source // "startup"' 2>/dev/null || echo "startup")"

if ! output="$("$FOXCTL_BIN" hooks session-start --workspace "$workspace" --source "$source_name" 2>/dev/null)"; then
  echo '{}'
  exit 0
fi

response="$(printf '%s' "$output" | jq -c '.data.response // {}' 2>/dev/null || echo '{}')"
printf '%s\n' "$response"
