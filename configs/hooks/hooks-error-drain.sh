#!/usr/bin/env bash
# hooks-error-drain.sh - Surface queued hook errors on PostToolUse
#
# Hooks fail silently to avoid blocking Claude Code. This drain hook runs on
# PostToolUse and surfaces any queued errors so they can be addressed.
#
# Environment:
#   FOXCTL_HOOK_ERROR_DRAIN_DISABLED - Set to "1" to disable
#
# Hook type: PostToolUse (runs after any tool completes)

set -euo pipefail

# Check if disabled
if [[ "${FOXCTL_HOOK_ERROR_DRAIN_DISABLED:-}" == "1" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Source the error queue library
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/error-queue.sh"

# Check for pending errors
if ! hook_error_has_pending; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Drain and format errors
errors=$(hook_error_drain)
if [[ -z "$errors" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Format as markdown
formatted=$(hook_error_format_markdown "$errors")

# Return with context injection via hookSpecificOutput.additionalContext
if [[ -n "$formatted" ]]; then
  jq -nc --arg ctx "$formatted" '{
    hookSpecificOutput: {
      hookEventName: "PostToolUse",
      additionalContext: $ctx
    }
  }'
else
  echo '{}'
fi
