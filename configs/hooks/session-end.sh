#!/usr/bin/env bash
# Thin SessionEnd wrapper: delegate lifecycle orchestration to Go.

set -euo pipefail

if [[ "${FOXCTL_SESSION_CAPTURE_DISABLED:-}" == "1" ]]; then
  exit 0
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

if [[ "${FOXCTL_SESSION_CAPTURE_SYNC:-}" != "1" ]]; then
  FOXCTL_HOME="${FOXCTL_HOME:-${HOME}/.foxctl}"
  LOG_DIR="${FOXCTL_HOME}/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/session-end-$(date +%Y%m%d-%H%M%S)-$$.log"

  (
    printf '%s' "$payload" | "$FOXCTL_BIN" hooks session-end --workspace "$workspace" >>"$LOG_FILE" 2>&1 || true
  ) &
  disown
  exit 0
fi

printf '%s' "$payload" | "$FOXCTL_BIN" hooks session-end --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
