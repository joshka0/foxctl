#!/usr/bin/env bash
# daemon-warmup.sh - SessionStart hook to warm up agentctl for faster hooks
#
# Problem: First agentctl call in a session is slow (~2-4s) due to:
#   - Binary loading, Go runtime init
#   - SQLite database opening
#   - Config parsing
#
# Solution: Pre-warm agentctl with a cheap operation at session start.
# Subsequent calls reuse OS file cache and are much faster (~0.1-0.3s).
#
# Usage in .claude/settings.json:
#   "SessionStart": [
#     {
#       "matcher": "startup",
#       "hooks": [
#         {
#           "type": "command",
#           "command": "$CLAUDE_PROJECT_DIR/configs/hooks/daemon-warmup.sh",
#           "timeout": 5
#         }
#       ]
#     }
#   ]
#
# Environment:
#   AGENTCTL_WARMUP_DISABLED - Set to "1" to disable

set -euo pipefail

# Allow disabling
if [[ "${AGENTCTL_WARMUP_DISABLED:-}" == "1" ]]; then
  exit 0
fi

# Find agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  if command -v agentctl &>/dev/null; then
    AGENTCTL_BIN="agentctl"
  elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
    AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
  else
    exit 0
  fi
fi

# Warm up with a cheap version check (doesn't hit database)
# This loads the binary into OS cache for faster subsequent calls
"$AGENTCTL_BIN" version >/dev/null 2>&1 &

# Also warm up the databases by touching them
# This loads SQLite pages into OS file cache
DB_DIR="$HOME/.agentctl/storage"
if [[ -d "$DB_DIR" ]]; then
  for db in "$DB_DIR"/*.db; do
    if [[ -f "$db" ]]; then
      # Touch file to warm OS cache (non-blocking)
      head -c 4096 "$db" >/dev/null 2>&1 &
    fi
  done
fi

# Don't wait for background processes
exit 0
