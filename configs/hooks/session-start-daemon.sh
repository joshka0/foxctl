#!/usr/bin/env bash
# session-start-daemon.sh - Claude Code SessionStart hook for daemon auto-start
#
# This script starts the agentctl daemon on session start if not already running.
# The daemon provides faster hook execution by maintaining pre-loaded resources.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "SessionStart": [
#         {
#           "matcher": "",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-start-daemon.sh"
#             }
#           ]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary (default: searches PATH, then project bin/)
#   AGENTCTL_DAEMON_DISABLED - Set to 1 to disable daemon auto-start

set -euo pipefail

# Check if daemon is disabled
if [[ "${AGENTCTL_DAEMON_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

# Find agentctl binary
if [[ -n "${AGENTCTL_BIN:-}" ]]; then
  : # Use provided path
elif command -v agentctl &>/dev/null; then
  AGENTCTL_BIN="agentctl"
elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
  AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
else
  # Can't find agentctl - fail silently
  echo '{}'
  exit 0
fi

# Consume stdin (hook input) even if unused
# Guard against blocking: skip if TTY, use timeout otherwise
if [[ -t 0 ]]; then
  : # stdin is a TTY - don't try to drain
elif command -v timeout &>/dev/null; then
  timeout 1 cat > /dev/null 2>/dev/null || true
else
  # Fallback: use read with timeout (portable)
  while IFS= read -r -t 1 _line; do :; done 2>/dev/null || true
fi

# Check if daemon is already running
if "$AGENTCTL_BIN" daemon status --quiet 2>/dev/null; then
  # Daemon already running - warm the workspace
  workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

  # Send warm request (fire and forget)
  echo "{\"method\":\"warm\",\"params\":{\"workspace\":\"$workspace\"}}" | \
    timeout 1 nc -U "$(dirname "$("$AGENTCTL_BIN" daemon status 2>/dev/null | jq -r '.data.socket // ""')" 2>/dev/null || echo "/tmp/agentctl-$(id -u).sock")" 2>/dev/null || true

  echo '{}'
  exit 0
fi

# Start daemon in background
"$AGENTCTL_BIN" daemon start --background --workspace "${CLAUDE_PROJECT_DIR:-$(pwd)}" 2>/dev/null || true

# Return success (don't block session start)
echo '{}'
