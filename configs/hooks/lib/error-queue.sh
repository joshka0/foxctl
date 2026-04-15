#!/usr/bin/env bash
# error-queue.sh - Hook error handling utilities
#
# Two strategies for surfacing hook errors:
#
# 1. BLOCKING (preferred): Exit 2 to immediately show error to Claude
#    Use hook_error_block() - exits with code 2, stderr shown to Claude
#
# 2. QUEUED (deferred): Enqueue for later display
#    Use hook_error_enqueue() - continues, surfaces on next PostToolUse
#
# Usage (blocking - recommended):
#   source "$CLAUDE_PROJECT_DIR/configs/hooks/lib/error-queue.sh"
#
#   result=$(...) || {
#     hook_error_block "my-hook" "operation failed" "$error_detail"
#     # Never reached - hook_error_block exits with code 2
#   }
#
# Usage (queued - for non-critical errors):
#   result=$(...) || {
#     hook_error_enqueue "my-hook" "operation failed"
#     echo '{"decision":"approve"}'
#     exit 0
#   }

HOOK_ERROR_FILE="${FOXCTL_HOME:-$HOME/.foxctl}/hooks/error_queue.ndjson"

# =============================================================================
# BLOCKING ERROR (exit 2) - Preferred approach
# =============================================================================

# Block with exit 2 to immediately surface error to Claude
# This outputs to stderr and exits - Claude sees it as a system alert
# Args: hook_name error_message [details]
hook_error_block() {
  local hook_name="$1"
  local error_msg="$2"
  local details="${3:-}"

  # Format as clear system alert with actionable instructions
  cat >&2 <<EOF
[SYSTEM ALERT] Hook '${hook_name}' failed: ${error_msg}
${details:+Details: ${details}}

This is a non-critical hook error. Choose one:
1. Add to TODO: Use TodoWrite to track "Fix ${hook_name} hook: ${error_msg}"
2. Investigate now if this affects your current task
3. Ignore if unrelated to current work

Note: This hook will retry on subsequent tool calls.
EOF

  exit 2
}

# =============================================================================
# QUEUED ERROR (deferred) - For non-critical errors
# =============================================================================

# Ensure directory exists
hook_error_init() {
  local dir
  dir=$(dirname "$HOOK_ERROR_FILE")
  [[ -d "$dir" ]] || mkdir -p "$dir"
}

# Enqueue an error for later display
# Args: hook_name error_message [details]
hook_error_enqueue() {
  local hook_name="$1"
  local error_msg="$2"
  local details="${3:-}"
  local ts
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)

  hook_error_init

  # Use jq for proper JSON escaping
  jq -nc \
    --arg hook "$hook_name" \
    --arg error "$error_msg" \
    --arg details "$details" \
    --arg ts "$ts" \
    '{hook: $hook, error: $error, details: (if $details == "" then null else $details end), ts: $ts}' \
    >> "$HOOK_ERROR_FILE"
}

# Drain all queued errors (returns NDJSON, clears queue)
# Returns empty string if no errors
hook_error_drain() {
  [[ -f "$HOOK_ERROR_FILE" ]] || return 0
  [[ -s "$HOOK_ERROR_FILE" ]] || return 0

  # Atomic read and clear
  local errors
  errors=$(cat "$HOOK_ERROR_FILE")
  : > "$HOOK_ERROR_FILE"

  printf '%s' "$errors"
}

# Check if there are queued errors (for conditional logic)
hook_error_has_pending() {
  [[ -f "$HOOK_ERROR_FILE" ]] && [[ -s "$HOOK_ERROR_FILE" ]]
}

# Count pending errors
hook_error_count() {
  [[ -f "$HOOK_ERROR_FILE" ]] || { echo 0; return; }
  wc -l < "$HOOK_ERROR_FILE" | tr -d ' '
}

# Format errors as markdown for context injection
# Args: ndjson_errors
hook_error_format_markdown() {
  local errors="$1"
  [[ -z "$errors" ]] && return

  echo "$errors" | jq -rs '
    if length == 0 then ""
    else
      "**Hook Errors** (fix now or `/todo add`):\n" +
      (map("- **\(.hook)**: \(.error)" + (if .details then " - `\(.details)`" else "" end)) | join("\n"))
    end
  '
}
