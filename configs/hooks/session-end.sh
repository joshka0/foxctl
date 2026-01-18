#!/usr/bin/env bash
# session-capture.sh - Claude Code Stop hook for capturing conversation sessions (ASYNC)
#
# ASYNC: This hook returns immediately and captures session in background.
# Stop hooks don't need to block - the session is ending anyway.
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_SESSION_CAPTURE_DISABLED - Set to "1" to disable capture
#   AGENTCTL_SESSION_CAPTURE_SYNC - Set to "1" for synchronous execution
#   CEREBRAS_API_KEY - Required for summarization (optional)

set -euo pipefail

init_user_prefs_file() {
  local prefs_file="$1"
  if [[ -f "$prefs_file" ]]; then
    return 0
  fi
  cat <<'EOF' >"$prefs_file"
# User Preferences
#
# Append-only log of explicit user preferences discovered from session summaries.
# Format: - YYYY-MM-DD: preference
EOF
}

init_recent_gotchas_file() {
  local gotchas_file="$1"
  if [[ -f "$gotchas_file" ]]; then
    return 0
  fi
  cat <<'EOF' >"$gotchas_file"
# Recent Errors, Gotchas, and Time Sinks
#
# Append-only log of recent errors/gotchas and slow-to-resolve issues.
# Format: - YYYY-MM-DD [gotcha|time]: note
EOF
}

append_summary_notes() {
  local summary_json="$1"
  local workspace_path="$2"

  if [[ -z "$summary_json" ]]; then
    return 0
  fi

  if ! printf '%s' "$summary_json" | jq -e '.data' >/dev/null 2>&1; then
    return 0
  fi

  local today config_dir prefs_file gotchas_file prefs gotchas time_sinks
  today="$(date +%Y-%m-%d)"
  config_dir="${workspace_path}/configs"
  prefs_file="${config_dir}/USER_PREFS.md"
  gotchas_file="${config_dir}/RECENT_GOTCHAS.md"

  mkdir -p "$config_dir" 2>/dev/null || true

  prefs="$(printf '%s' "$summary_json" | jq -r '.data.user_preferences[]? | select(type == "string" and length > 0)' 2>/dev/null || true)"
  if [[ -n "$prefs" ]]; then
    init_user_prefs_file "$prefs_file"
    while IFS= read -r item; do
      [[ -z "$item" ]] && continue
      # Use flock for atomic append to prevent interleaved writes from concurrent processes
      (
        flock -x 200 2>/dev/null || true
        printf -- "- %s: %s\n" "$today" "$item" >>"$prefs_file"
      ) 200>"${prefs_file}.lock"
    done <<<"$prefs"
  fi

  gotchas="$(printf '%s' "$summary_json" | jq -r '.data.gotchas[]? | select(type == "string" and length > 0)' 2>/dev/null || true)"
  time_sinks="$(printf '%s' "$summary_json" | jq -r '.data.time_sinks[]? | select(type == "string" and length > 0)' 2>/dev/null || true)"
  if [[ -n "$gotchas" || -n "$time_sinks" ]]; then
    init_recent_gotchas_file "$gotchas_file"
    if [[ -n "$gotchas" ]]; then
      while IFS= read -r item; do
        [[ -z "$item" ]] && continue
        # Use flock for atomic append
        (
          flock -x 200 2>/dev/null || true
          printf -- "- %s [gotcha]: %s\n" "$today" "$item" >>"$gotchas_file"
        ) 200>"${gotchas_file}.lock"
      done <<<"$gotchas"
    fi
    if [[ -n "$time_sinks" ]]; then
      while IFS= read -r item; do
        [[ -z "$item" ]] && continue
        # Use flock for atomic append
        (
          flock -x 200 2>/dev/null || true
          printf -- "- %s [time]: %s\n" "$today" "$item" >>"$gotchas_file"
        ) 200>"${gotchas_file}.lock"
      done <<<"$time_sinks"
    fi
  fi
}

# Check if capture is disabled
if [[ "${AGENTCTL_SESSION_CAPTURE_DISABLED:-}" == "1" ]]; then
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
  exit 0
fi

# Read and discard hook input from stdin
cat >/dev/null

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
session_id="${CLAUDE_SESSION_ID:-}"

# Build skill input for capture
capture_input=$(jq -nc \
  --arg workspace "$workspace" \
  --arg session_id "$session_id" \
  '{
    workspace: $workspace,
    session_id: (if $session_id != "" then $session_id else null end)
  }'
)

# ASYNC: Run in background unless SYNC mode requested
if [[ "${AGENTCTL_SESSION_CAPTURE_SYNC:-}" != "1" ]]; then
  LOG_DIR="${HOME}/.agentctl/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/session-capture-$(date +%Y%m%d-%H%M%S).log"

  # Spawn in background and exit immediately
  (
    capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run session/capture --input-file - 2>&1)" || true
    echo "$capture_result" >> "$LOG_FILE"

    # If CEREBRAS_API_KEY is set, also summarize
    if [[ -n "${CEREBRAS_API_KEY:-}" ]]; then
      captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""' 2>/dev/null)
      if [[ -n "$captured_session_id" ]]; then
        summarize_input=$(jq -nc --arg session_id "$captured_session_id" '{session_id: $session_id}')
        summarize_result="$(printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run session/summarize --no-cas --input-file - 2>>"$LOG_FILE")" || true
        echo "$summarize_result" >> "$LOG_FILE"
        append_summary_notes "$summarize_result" "$workspace"
      fi
    fi
  ) &
  disown
  exit 0
fi

# SYNC mode: Original blocking behavior
capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run session/capture --input-file - 2>/dev/null)" || exit 0

capture_status=$(printf '%s' "$capture_result" | jq -r '.data.status // "error"')
captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""')

if [[ "$capture_status" != "captured" && "$capture_status" != "exists" ]]; then
  exit 0
fi

# If CEREBRAS_API_KEY is set, also summarize
if [[ -n "${CEREBRAS_API_KEY:-}" && -n "$captured_session_id" ]]; then
  summarize_input=$(jq -nc --arg session_id "$captured_session_id" '{session_id: $session_id}')
  summarize_result="$(printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run session/summarize --no-cas --input-file - 2>/dev/null)" || true
  append_summary_notes "$summarize_result" "$workspace"
fi

exit 0
