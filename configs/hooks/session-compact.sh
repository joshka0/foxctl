#!/usr/bin/env bash
# session-compact.sh - Consolidated PreCompact hook for session state preservation
#
# Combines functionality from:
#   - session-save.sh: Save session state before compaction
#   - session-summarize.sh: Summarize session via LLM for future recall
#
# Usage in ~/.claude/settings.json:
#   "PreCompact": [
#     {
#       "matcher": "auto|manual",
#       "hooks": ["$HOME/.claude/hooks/foxctl/session-compact.sh"]
#     }
#   ]
#
# Environment:
#   AGENTCTL_SUMMARIZE_DISABLED=1 - Skip LLM summarization
#   AGENTCTL_SUMMARIZE_MODE - "windows" (default) or "summary"
#   AGENTCTL_SUMMARIZE_BATCH_SIZE - Windows per batch (default: 5)
#   AGENTCTL_BIN - Path to foxctl binary

set -euo pipefail

# Find foxctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  if command -v foxctl &>/dev/null; then
    AGENTCTL_BIN="foxctl"
  elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/foxctl" ]]; then
    AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/foxctl"
  else
    echo '{}'
    exit 0
  fi
fi

# Read hook input from stdin
payload="$(cat)"

# Extract trigger info
trigger=$(printf '%s' "$payload" | jq -r '.trigger // "auto"')
custom_instructions=$(printf '%s' "$payload" | jq -r '.custom_instructions // ""' 2>/dev/null || true)

# Workspace and session
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
session_id="${CLAUDE_SESSION_ID:-}"

# Try to get session ID from active file if not in env
if [[ -z "$session_id" ]]; then
  if command -v sha256sum &>/dev/null; then
    ws_hash=$(echo -n "$workspace" | sha256sum | cut -c1-16)
  elif command -v shasum &>/dev/null; then
    ws_hash=$(echo -n "$workspace" | shasum -a 256 | cut -c1-16)
  else
    ws_hash=""
  fi
  if [[ -n "$ws_hash" ]]; then
    active_file="$HOME/.foxctl/sessions/active/${ws_hash}-claude.json"
    if [[ -f "$active_file" ]]; then
      session_id=$(jq -r '.session_id // ""' "$active_file" 2>/dev/null || true)
    fi
  fi
fi

context_parts=()

# =============================================================================
# 0. CREATE PENDING-RESTORE MARKER (for post-compact hook)
# =============================================================================

marker_dir="$HOME/.foxctl/sessions/pending-restore"
mkdir -p "$marker_dir"

# Reuse ws_hash if already computed, otherwise compute it
if [[ -z "${ws_hash:-}" ]]; then
  if command -v sha256sum &>/dev/null; then
    ws_hash=$(echo -n "$workspace" | sha256sum | cut -c1-16)
  elif command -v shasum &>/dev/null; then
    ws_hash=$(echo -n "$workspace" | shasum -a 256 | cut -c1-16)
  else
    ws_hash=""
  fi
fi

if [[ -n "$ws_hash" ]]; then
  marker_file="$marker_dir/${ws_hash}.json"
  jq -nc \
    --arg workspace "$workspace" \
    --arg session_id "$session_id" \
    --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg trigger "$trigger" \
    '{workspace: $workspace, session_id: $session_id, created_at: $created_at, trigger: $trigger}' \
    > "$marker_file"
fi

# =============================================================================
# 1. SAVE: Capture session state
# =============================================================================

save_input=$(jq -nc \
  --arg trigger "pre_compact" \
  --arg workspace "$workspace" \
  --arg session_id "$session_id" \
  '{trigger: $trigger, workspace: $workspace, session_id: $session_id}')

printf '%s' "$save_input" | "$AGENTCTL_BIN" run --daemon session/save --ephemeral --input-file - >/dev/null 2>&1 || true

# Bump anchor compaction count
anchor_bump_input=$(jq -nc --arg ws "$workspace" '{operation: "bump_compaction", workspace: $ws, trigger: "pre_compact"}')
printf '%s' "$anchor_bump_input" | "$AGENTCTL_BIN" run --daemon session/anchor --ephemeral --workspace "$workspace" --input-file - >/dev/null 2>/dev/null || true

# Append custom instructions to anchor learnings
if [[ -n "${custom_instructions:-}" && "${custom_instructions:-}" != "null" ]]; then
  clipped="${custom_instructions:0:500}"
  anchor_append_input=$(jq -nc --arg ws "$workspace" --arg sum "$clipped" '{operation: "append_learnings", workspace: $ws, trigger: "pre_compact", summary: $sum}')
  printf '%s' "$anchor_append_input" | "$AGENTCTL_BIN" run --daemon session/anchor --ephemeral --workspace "$workspace" --input-file - >/dev/null 2>/dev/null || true
fi

# =============================================================================
# 2. SUMMARIZE: Extract learnings via LLM (optional)
# =============================================================================

if [[ "${AGENTCTL_SUMMARIZE_DISABLED:-0}" != "1" && -n "$session_id" ]]; then
  mode="${AGENTCTL_SUMMARIZE_MODE:-windows}"
  batch_size="${AGENTCTL_SUMMARIZE_BATCH_SIZE:-5}"

  summarize_input=$(jq -nc \
    --arg session_id "$session_id" \
    --arg mode "$mode" \
    --argjson batch_size "$batch_size" \
    '{session_id: $session_id, mode: $mode, batch_size: $batch_size, force: false}')

  result=$("$AGENTCTL_BIN" run --daemon session/summarize --ephemeral --input "$summarize_input" 2>/dev/null) || true

  if [[ -n "$result" ]]; then
    if [[ "$mode" == "windows" ]]; then
      summarized=$(echo "$result" | jq -r '.data.windows_summarized // 0' 2>/dev/null || echo "0")
      remaining=$(echo "$result" | jq -r '.data.windows_remaining // 0' 2>/dev/null || echo "0")
      embedded=$(echo "$result" | jq -r '.data.windows_reembedded // 0' 2>/dev/null || echo "0")

      if [[ "$summarized" -gt 0 ]] || [[ "$remaining" -gt 0 ]]; then
        msg="Windows: $summarized summarized"
        [[ "$embedded" -gt 0 ]] && msg="$msg, $embedded embedded"
        [[ "$remaining" -gt 0 ]] && msg="$msg, $remaining remaining"
        context_parts+=("$msg")
      fi
    else
      gotchas_count=$(echo "$result" | jq -r '.data.gotchas | length // 0' 2>/dev/null || echo "0")
      decisions_count=$(echo "$result" | jq -r '.data.decisions | length // 0' 2>/dev/null || echo "0")

      if [[ "$gotchas_count" -gt 0 ]] || [[ "$decisions_count" -gt 0 ]]; then
        context_parts+=("Session summarized: $gotchas_count gotchas, $decisions_count decisions")
      fi
    fi
  fi
fi

# =============================================================================
# OUTPUT
# =============================================================================

if [[ ${#context_parts[@]} -gt 0 ]]; then
  context=$(printf '%s; ' "${context_parts[@]}")
  jq -nc --arg ctx "${context%%; }" '{context: $ctx}'
else
  echo '{}'
fi
