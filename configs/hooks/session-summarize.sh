#!/usr/bin/env bash
# session-summarize.sh - Claude Code PreCompact hook for session summarization
#
# This script triggers LLM-based session summarization on context compaction,
# extracting gotchas, decisions, and learnings for future recall.
#
# Usage in ~/.claude/settings.json:
#   {
#     "hooks": {
#       "PreCompact": [
#         {
#           "matcher": "auto|manual",
#           "hooks": [
#             { "type": "command", "command": "session-save.sh", "timeout": 5 },
#             { "type": "command", "command": "session-summarize.sh", "timeout": 60 }
#           ]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary (default: searches PATH)
#   AGENTCTL_SUMMARIZE_DISABLED - Set to 1 to skip summarization
#   AGENTCTL_SUMMARIZE_MODE - "windows" (default) or "summary" for full session
#   AGENTCTL_SUMMARIZE_BATCH_SIZE - Windows per compaction (default: 5)

set -euo pipefail

# Check if disabled
if [[ "${AGENTCTL_SUMMARIZE_DISABLED:-0}" == "1" ]]; then
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

# Read hook input from stdin (consume it even if we don't use it)
payload="$(cat)"

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Session ID - try multiple sources
session_id="${CLAUDE_SESSION_ID:-}"
if [[ -z "$session_id" ]]; then
  # Try to get from active session file
  # Use portable hash command (sha256sum on Linux, shasum on macOS)
  if command -v sha256sum &>/dev/null; then
    ws_hash=$(echo -n "$workspace" | sha256sum | cut -c1-16)
  elif command -v shasum &>/dev/null; then
    ws_hash=$(echo -n "$workspace" | shasum -a 256 | cut -c1-16)
  else
    # Fallback: can't compute hash, skip session file lookup
    ws_hash=""
  fi
  if [[ -n "$ws_hash" ]]; then
    active_file="$HOME/.agentctl/sessions/active/${ws_hash}.json"
    if [[ -f "$active_file" ]]; then
      session_id=$(jq -r '.session_id // ""' "$active_file" 2>/dev/null || true)
    fi
  fi
fi

# If no session ID, skip summarization
if [[ -z "$session_id" ]]; then
  echo '{}'
  exit 0
fi

# Summarization mode and batch size
mode="${AGENTCTL_SUMMARIZE_MODE:-windows}"
batch_size="${AGENTCTL_SUMMARIZE_BATCH_SIZE:-5}"

# Build skill input
skill_input=$(jq -nc \
  --arg session_id "$session_id" \
  --arg mode "$mode" \
  --argjson batch_size "$batch_size" \
  '{
    session_id: $session_id,
    mode: $mode,
    batch_size: $batch_size,
    force: false
  }'
)

# Call session/summarize skill
# Use --ephemeral for faster startup, redirect stderr to avoid noise
result=$("$AGENTCTL_BIN" run session/summarize --ephemeral --input "$skill_input" 2>/dev/null) || {
  # On error, don't block compaction
  echo '{}'
  exit 0
}

# Extract results based on mode
if [[ "$mode" == "windows" ]]; then
  # Windows mode: report summarized/remaining counts
  summarized=$(echo "$result" | jq -r '.data.windows_summarized // 0' 2>/dev/null || echo "0")
  remaining=$(echo "$result" | jq -r '.data.windows_remaining // 0' 2>/dev/null || echo "0")
  embedded=$(echo "$result" | jq -r '.data.windows_reembedded // 0' 2>/dev/null || echo "0")

  if [[ "$summarized" -gt 0 ]] || [[ "$remaining" -gt 0 ]]; then
    msg="Windows: $summarized summarized"
    [[ "$embedded" -gt 0 ]] && msg="$msg, $embedded embedded"
    [[ "$remaining" -gt 0 ]] && msg="$msg, $remaining remaining"
    echo "{\"context\": \"$msg\"}"
  else
    echo '{}'
  fi
else
  # Summary mode: report gotchas/decisions counts
  gotchas_count=$(echo "$result" | jq -r '.data.gotchas | length // 0' 2>/dev/null || echo "0")
  decisions_count=$(echo "$result" | jq -r '.data.decisions | length // 0' 2>/dev/null || echo "0")

  if [[ "$gotchas_count" -gt 0 ]] || [[ "$decisions_count" -gt 0 ]]; then
    echo "{\"context\": \"Session summarized: $gotchas_count gotchas, $decisions_count decisions extracted.\"}"
  else
    echo '{}'
  fi
fi
