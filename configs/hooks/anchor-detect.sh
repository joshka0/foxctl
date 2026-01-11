#!/usr/bin/env bash
# anchor-detect.sh - Set session anchor via /anchor and enable /todo mode
#
# Claude Code UserPromptSubmit hook:
# - When user includes "/anchor <goal>" (or "anchor this", "@anchor"),
#   persist the session goal via `session/anchor`.
# - When user includes "/todo", enable lightweight todo mode for the session.
# - Returns a context hint so the UI confirms the anchor.

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
MODE_DIR="${AGENTCTL_HOME:-$HOME/.agentctl}/cache/session-modes"

payload="$(cat)"
workspace_root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
prompt="$(printf '%s' "$payload" | jq -r '.prompt // ""' 2>/dev/null || true)"

if [[ -z "$prompt" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

has_anchor=0
has_todo=0
if printf '%s' "$prompt" | grep -qiE '(^|\b)(anchor this|anchor it|anchor prompt|@anchor|/anchor)(\b|$)'; then
  has_anchor=1
fi
if printf '%s' "$prompt" | grep -qiE '(^|\b)(/todo)(\b|$)'; then
  has_todo=1
fi

if [[ "$has_anchor" -ne 1 && "$has_todo" -ne 1 ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

session_id="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-}}"
if [[ -z "$session_id" || "$session_id" == "null" ]]; then
  session_id="$(printf '%s' "$payload" | jq -r '.sessionID // .session_id // ""' 2>/dev/null || true)"
fi

append_context() {
  local msg="$1"
  if [[ -z "${context:-}" ]]; then
    context="$msg"
  else
    context="${context}\n\n${msg}"
  fi
}

if [[ "$has_anchor" -eq 1 ]]; then
  clean_prompt="$(printf '%s' "$prompt" | sed -E 's/(^|\b)(anchor this|anchor it|anchor prompt|@anchor|\/anchor)(\b|$)//Ig' | sed -E 's/(^|\b)(\/todo)(\b|$)//Ig' | sed -E 's/^[:\-\s]+//; s/[\s]+$//')"

  if [[ -z "$clean_prompt" ]]; then
    append_context "Usage: /anchor <goal>"
  else
    # Persist anchor via session/anchor skill
    anchor_input="$(jq -nc --arg op "set" --arg ws "$workspace_root" --arg sid "$session_id" --arg mp "$clean_prompt" '{operation:$op, workspace:$ws, session_id:$sid, main_prompt:$mp, trigger:"user_prompt_submit"}')"
    "$AGENTCTL_BIN" run session/anchor --ephemeral --workspace "$workspace_root" --input "$anchor_input" >/dev/null 2>/dev/null || true
    
    # Write anchor flag file for stop hook (lightweight check)
    if [[ -n "$session_id" && "$session_id" != "null" ]]; then
      mkdir -p "$MODE_DIR" 2>/dev/null || true
      now_ms="$(( $(date +%s) * 1000 ))"
      anchor_hash="$(printf '%s' "anchor:${session_id}" | shasum -a 256 | cut -c1-16)"
      jq -nc --arg goal "$clean_prompt" --argjson ts "$now_ms" '{goal: $goal, updated_at: $ts}' >"${MODE_DIR}/anchor-${anchor_hash}.json" 2>/dev/null || true
    fi
    append_context "**Anchor set**: $clean_prompt\n**Stop hook**: will check for incomplete tasks before allowing stop"
  fi
fi

if [[ "$has_todo" -eq 1 ]]; then
  if [[ -n "$session_id" && "$session_id" != "null" ]]; then
    mkdir -p "$MODE_DIR" 2>/dev/null || true
    now_ms="$(( $(date +%s) * 1000 ))"
    mode_hash="$(printf '%s' "todo:${session_id}" | shasum -a 256 | cut -c1-16)"
    printf '{"updated_at": %s}\n' "$now_ms" >"${MODE_DIR}/todo-${mode_hash}.json" 2>/dev/null || true
    append_context "**Todo mode**: enabled"
  else
    append_context "Todo mode: missing session ID."
  fi
fi

if [[ -z "${context:-}" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
