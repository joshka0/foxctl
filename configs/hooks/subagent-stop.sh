#!/usr/bin/env bash
# Thin SubagentStop wrapper: delegate ACA capture to Go.

set -euo pipefail

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

printf '%s' "$payload" | "$FOXCTL_BIN" hooks subagent-stop --workspace "$workspace" >/dev/null 2>&1 || true
echo '{}'
