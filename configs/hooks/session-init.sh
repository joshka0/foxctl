#!/usr/bin/env bash
# session-init.sh - Consolidated SessionStart hook for session initialization
#
# Combines functionality from:
#   - daemon-warmup.sh: Pre-warm agentctl binary and DB caches
#   - session-start-daemon.sh: Auto-start daemon if not running
#   - session-identity.sh: Detect and persist session ID
#   - session-restore.sh: Restore session state on start/resume
#
# Usage in ~/.claude/settings.json:
#   "SessionStart": [
#     {
#       "matcher": "startup|resume|compact",
#       "hooks": ["$HOME/.claude/hooks/agentctl/session-init.sh"]
#     }
#   ]
#
# Environment:
#   AGENTCTL_WARMUP_DISABLED=1 - Skip warmup
#   AGENTCTL_DAEMON_DISABLED=1 - Skip daemon start
#   AGENTCTL_SESSION_ID - Skip identity detection if set
#   AGENTCTL_BIN - Path to agentctl binary

set -euo pipefail

# Find agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  if command -v agentctl &>/dev/null; then
    AGENTCTL_BIN="agentctl"
  elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
    AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
  else
    echo '{}'
    exit 0
  fi
fi

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
workspace_hash="$(printf '%s' "$workspace" | shasum -a 256 | cut -c1-16)"

# Read hook input from stdin
payload="$(cat)"
trigger=$(printf '%s' "$payload" | jq -r '.source // "startup"')

# =============================================================================
# 1. WARMUP: Pre-warm binary and DB caches (fast, non-blocking)
# =============================================================================

if [[ "${AGENTCTL_WARMUP_DISABLED:-}" != "1" ]]; then
  # Warm up binary with cheap version check (background)
  "$AGENTCTL_BIN" version >/dev/null 2>&1 &

  # Warm up databases (background)
  DB_DIR="$HOME/.agentctl/storage"
  if [[ -d "$DB_DIR" ]]; then
    for db in "$DB_DIR"/*.db; do
      if [[ -f "$db" ]]; then
        head -c 4096 "$db" >/dev/null 2>&1 &
      fi
    done
  fi
fi

# =============================================================================
# 2. DAEMON: Start daemon if not running (non-blocking)
# =============================================================================

if [[ "${AGENTCTL_DAEMON_DISABLED:-}" != "1" ]]; then
  if ! "$AGENTCTL_BIN" daemon status --quiet 2>/dev/null; then
    "$AGENTCTL_BIN" daemon start --background --workspace "$workspace" 2>/dev/null || true
  fi
fi

# =============================================================================
# 3. IDENTITY: Detect and persist session ID
# =============================================================================

identity_dir="$HOME/.agentctl/sessions/active"
identity_file="$identity_dir/${workspace_hash}-claude.json"

# Skip identity detection if already set or recent file exists
identity_needed=true
if [[ -n "${AGENTCTL_SESSION_ID:-}" || -n "${CLAUDE_SESSION_ID:-}" ]]; then
  identity_needed=false
elif [[ -f "$identity_file" ]]; then
  # Check file age
  file_mtime=0
  if file_mtime=$(stat -c %Y "$identity_file" 2>/dev/null) || \
     file_mtime=$(stat -f %m "$identity_file" 2>/dev/null); then
    now=$(date +%s)
    age=$((now - file_mtime))
    if [[ $age -lt 3600 ]]; then
      identity_needed=false
    fi
  fi
fi

session_id=""
if [[ "$identity_needed" == true ]]; then
  result="$("$AGENTCTL_BIN" session-id --workspace "$workspace" 2>/dev/null)" || true
  session_id="$(printf '%s' "$result" | jq -r '.data.session_id // ""')"
  provider="$(printf '%s' "$result" | jq -r '.data.provider // "claude"')"
  agent_id="$(printf '%s' "$result" | jq -r '.data.agent_id // "claude"')"

  if [[ -n "$session_id" && "$session_id" != "null" ]]; then
    mkdir -p "$identity_dir"
    jq -nc \
      --arg session_id "$session_id" \
      --arg agent_id "$agent_id" \
      --arg provider "$provider" \
      --arg workspace "$workspace" \
      --arg workspace_hash "$workspace_hash" \
      --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      '{
        session_id: $session_id,
        agent_id: $agent_id,
        provider: $provider,
        workspace: $workspace,
        workspace_hash: $workspace_hash,
        started_at: $started_at,
        last_activity: $started_at,
        detected_from: "tui"
      }' > "$identity_file"
  fi
else
  # Read existing session ID from identity file
  if [[ -f "$identity_file" ]]; then
    session_id="$(jq -r '.session_id // ""' "$identity_file" 2>/dev/null || true)"
  fi
fi

# =============================================================================
# 4. RESTORE: Restore session state (on resume/compact)
# =============================================================================

context=""
if [[ "$trigger" == "resume" || "$trigger" == "compact" ]]; then
  skill_input=$(jq -nc \
    --arg trigger "$trigger" \
    --arg workspace "$workspace" \
    '{trigger: $trigger, workspace: $workspace}')

  result="$(printf '%s' "$skill_input" | "$AGENTCTL_BIN" run session/restore --ephemeral --input-file - 2>/dev/null)" || true
  context=$(printf '%s' "$result" | jq -r '.data.hook_output.context // ""')

  # Get anchor if set
  anchor_input=$(jq -nc --arg ws "$workspace" '{operation: "get", workspace: $ws}')
  if anchor_result=$(printf '%s' "$anchor_input" | "$AGENTCTL_BIN" run session/anchor --ephemeral --workspace "$workspace" --input-file - 2>/dev/null); then
    anchor_main=$(printf '%s' "$anchor_result" | jq -r '.data.anchor.main_prompt // ""' 2>/dev/null || echo "")
    anchor_q=$(printf '%s' "$anchor_result" | jq -r '.data.anchor.pending_question // ""' 2>/dev/null || echo "")

    if [[ -n "$anchor_main" && "$anchor_main" != "null" ]]; then
      anchor_block=$'## Session Anchor\n\n**Goal:** '
      anchor_block+="${anchor_main}"
      if [[ -n "$anchor_q" && "$anchor_q" != "null" ]]; then
        anchor_block+=$'\n\n**Pending:** '
        anchor_block+="${anchor_q}"
      fi
      context="${anchor_block}"$'\n\n'"${context}"
    fi
  fi
fi

# =============================================================================
# OUTPUT
# =============================================================================

if [[ -n "$context" && "$context" != "null" ]]; then
  jq -nc --arg context "$context" '{context: $context}'
elif [[ -n "$session_id" && "$session_id" != "null" ]]; then
  jq -nc --arg sid "$session_id" '{context: ("Session: " + $sid)}'
else
  echo '{}'
fi
