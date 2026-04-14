#!/usr/bin/env bash
# Thin SessionEnd wrapper: delegate lifecycle orchestration to Go.

set -euo pipefail

if [[ "${AGENTCTL_SESSION_CAPTURE_DISABLED:-}" == "1" ]]; then
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-foxctl}"
if ! command -v "$AGENTCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${AGENTCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

if [[ "${AGENTCTL_SESSION_CAPTURE_SYNC:-}" != "1" ]]; then
  AGENTCTL_HOME="${AGENTCTL_HOME:-${HOME}/.foxctl}"
  LOG_DIR="${AGENTCTL_HOME}/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/session-end-$(date +%Y%m%d-%H%M%S)-$$.log"

  (
    printf '%s' "$payload" | "$AGENTCTL_BIN" hooks session-end --workspace "$workspace" >>"$LOG_FILE" 2>&1 || true
  ) &
  disown
  exit 0
fi

printf '%s' "$payload" | "$AGENTCTL_BIN" hooks session-end --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
