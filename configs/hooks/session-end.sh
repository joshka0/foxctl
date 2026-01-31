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

# Portable atomic file append with stale lock detection and cleanup.
# Usage: atomic_append <file> <content>
# Lock timeout: 30 seconds (stale locks older than this are removed)
# atomic_append appends CONTENT to FILE atomically using a per-file lock directory and a PID marker to detect and clear stale locks; returns 0 on success and 1 if it cannot obtain the lock after retries.
atomic_append() {
  local file="$1"
  local content="$2"
  local lock_dir="${file}.lock.d"
  local lock_marker="${lock_dir}/pid"
  local max_retries=50
  local lock_timeout=30
  local retries=0

  while ! mkdir "$lock_dir" 2>/dev/null; do
    retries=$((retries + 1))
    if [[ $retries -ge $max_retries ]]; then
      # Check for stale lock before giving up
      if [[ -f "$lock_marker" ]]; then
        local lock_age
        lock_age=$(( $(date +%s) - $(stat -f %m "$lock_marker" 2>/dev/null || stat -c %Y "$lock_marker" 2>/dev/null || echo 0) ))
        if [[ $lock_age -gt $lock_timeout ]]; then
          # Stale lock detected, remove it
          rm -f "$lock_marker" 2>/dev/null || true
          rmdir "$lock_dir" 2>/dev/null || true
          retries=0
          continue
        fi
      fi
      # Max retries exceeded, skip this append (non-critical)
      return 1
    fi
    sleep 0.1
  done

  # Write PID marker for stale detection
  echo $$ > "$lock_marker" 2>/dev/null || true

  # Perform the append
  printf '%s\n' "$content" >> "$file"

  # Cleanup
  rm -f "$lock_marker" 2>/dev/null || true
  rmdir "$lock_dir" 2>/dev/null || true
  return 0
}

# init_user_prefs_file initializes a persistent append-only user preferences file at the given path with a header describing the expected format if the file does not already exist.
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

# append_summary_notes parses a summarization JSON and appends extracted user preferences, gotchas, and time-sink notes into workspace configs/USER_PREFS.md and configs/RECENT_GOTCHAS.md, creating the config directory and files when missing.
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
      atomic_append "$prefs_file" "- $today: $item" || true
    done <<<"$prefs"
  fi

  gotchas="$(printf '%s' "$summary_json" | jq -r '.data.gotchas[]? | select(type == "string" and length > 0)' 2>/dev/null || true)"
  time_sinks="$(printf '%s' "$summary_json" | jq -r '.data.time_sinks[]? | select(type == "string" and length > 0)' 2>/dev/null || true)"
  if [[ -n "$gotchas" || -n "$time_sinks" ]]; then
    init_recent_gotchas_file "$gotchas_file"
    if [[ -n "$gotchas" ]]; then
      while IFS= read -r item; do
        [[ -z "$item" ]] && continue
        atomic_append "$gotchas_file" "- $today [gotcha]: $item" || true
      done <<<"$gotchas"
    fi
    if [[ -n "$time_sinks" ]]; then
      while IFS= read -r item; do
        [[ -z "$item" ]] && continue
        atomic_append "$gotchas_file" "- $today [time]: $item" || true
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
  # Respect AGENTCTL_HOME if set, otherwise use default
  AGENTCTL_HOME="${AGENTCTL_HOME:-${HOME}/.agentctl}"
  LOG_DIR="${AGENTCTL_HOME}/logs/hooks"
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  # Include milliseconds and PID to avoid log file collisions
  LOG_FILE="$LOG_DIR/session-capture-$(date +%Y%m%d-%H%M%S)-$$.log"

  # Maximum time for background capture (5 minutes)
  CAPTURE_TIMEOUT=300

  # Spawn in background with timeout and exit immediately
  (
    # Use timeout if available (GNU coreutils), otherwise proceed without it
    if command -v timeout &>/dev/null; then
      capture_result="$(printf '%s' "$capture_input" | timeout "$CAPTURE_TIMEOUT" "$AGENTCTL_BIN" run --daemon session/capture --input-file - 2>&1)" || true
    else
      capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run --daemon session/capture --input-file - 2>&1)" || true
    fi
    echo "$capture_result" >> "$LOG_FILE"

    # If CEREBRAS_API_KEY is set, also summarize
    if [[ -n "${CEREBRAS_API_KEY:-}" ]]; then
      captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""' 2>/dev/null)
      if [[ -n "$captured_session_id" ]]; then
        summarize_input=$(jq -nc --arg session_id "$captured_session_id" '{session_id: $session_id}')
        if command -v timeout &>/dev/null; then
          summarize_result="$(printf '%s' "$summarize_input" | timeout "$CAPTURE_TIMEOUT" "$AGENTCTL_BIN" run --daemon session/summarize --no-cas --input-file - 2>>"$LOG_FILE")" || true
        else
          summarize_result="$(printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run --daemon session/summarize --no-cas --input-file - 2>>"$LOG_FILE")" || true
        fi
        echo "$summarize_result" >> "$LOG_FILE"
        append_summary_notes "$summarize_result" "$workspace"
      fi
    fi
  ) &
  disown
  exit 0
fi

# SYNC mode: Original blocking behavior
capture_result="$(printf '%s' "$capture_input" | "$AGENTCTL_BIN" run --daemon session/capture --input-file - 2>/dev/null)" || true

# Handle capture failure gracefully
if [[ -z "$capture_result" ]]; then
  exit 0
fi

capture_status=$(printf '%s' "$capture_result" | jq -r '.data.status // "error"' 2>/dev/null) || true
captured_session_id=$(printf '%s' "$capture_result" | jq -r '.data.session_id // ""' 2>/dev/null) || true

if [[ "$capture_status" != "captured" && "$capture_status" != "exists" ]]; then
  exit 0
fi

# If CEREBRAS_API_KEY is set, also summarize
if [[ -n "${CEREBRAS_API_KEY:-}" && -n "$captured_session_id" ]]; then
  summarize_input=$(jq -nc --arg session_id "$captured_session_id" '{session_id: $session_id}')
  summarize_result="$(printf '%s' "$summarize_input" | "$AGENTCTL_BIN" run --daemon session/summarize --no-cas --input-file - 2>/dev/null)" || true
  append_summary_notes "$summarize_result" "$workspace"
fi

exit 0