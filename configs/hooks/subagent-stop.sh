#!/usr/bin/env bash
# Thin SubagentStop wrapper: delegate ACA capture to Go.

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

printf '%s' "$payload" | "$AGENTCTL_BIN" hooks subagent-stop --workspace "$workspace" >/dev/null 2>&1 || true
echo '{}'
