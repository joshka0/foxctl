#!/usr/bin/env bash

set -euo pipefail

if [[ "${AGENTCTL_TODO_CONTINUATION_DISABLED:-}" == "1" ]]; then
  exit 0
fi

AGENTCTL="${AGENTCTL_BIN:-agentctl}"
MIN_PENDING="${AGENTCTL_TODO_CONTINUATION_MIN_PENDING:-1}"
TOP_N="${AGENTCTL_TODO_CONTINUATION_TOP_N:-5}"

INPUT=$(cat)

WORKSPACE=$(echo "$INPUT" | jq -r '.cwd // empty' 2>/dev/null)
if [[ -z "$WORKSPACE" ]]; then
  WORKSPACE="${CLAUDE_PROJECT_DIR:-$(pwd)}"
fi

# Session ID detection - PRIORITIZE payload over cached files
# Claude Code sends sessionID in Stop hook payload
SESSION_ID="$(echo "$INPUT" | jq -r '.sessionID // .session_id // ""' 2>/dev/null || true)"

# Fall back to env vars
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  SESSION_ID="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-${OPENCODE_SESSION_ID:-}}}"
fi

# Last resort: check identity files (may be stale)
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  agentctl_home="${AGENTCTL_HOME:-$HOME/.agentctl}"
  workspace_hash="$(printf '%s' "$WORKSPACE" | shasum -a 256 | cut -c1-16)"
  identity_dir="$agentctl_home/sessions/active"
  for f in "$identity_dir/${workspace_hash}-"*.json; do
    [[ -f "$f" ]] || continue
    SESSION_ID="$(jq -r '.session_id // ""' "$f" 2>/dev/null || true)"
    if [[ -n "$SESSION_ID" && "$SESSION_ID" != "null" ]]; then
      break
    fi
  done
fi

if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  jq -n --arg warning "todo continuation: no session_id detected; allowing stop" '{decision: "approve", warning: $warning}'
  exit 0
fi

# Check for anchor flag file (lightweight) or todo mode
FLAG_TTL_MS=$((6 * 60 * 60 * 1000))  # 6 hours
mode_dir="${AGENTCTL_HOME:-$HOME/.agentctl}/cache/session-modes"
now_ms="$(( $(date +%s) * 1000 ))"

# Check TODO_MODE flag
TODO_MODE="false"
mode_hash="$(printf '%s' "todo:${SESSION_ID}" | shasum -a 256 | cut -c1-16)"
mode_file="${mode_dir}/todo-${mode_hash}.json"
if [[ -f "$mode_file" ]]; then
  updated_at="$(jq -r '.updated_at // 0' "$mode_file" 2>/dev/null || echo 0)"
  if [[ "$updated_at" =~ ^[0-9]+$ ]] && (( now_ms - updated_at <= FLAG_TTL_MS )); then
    TODO_MODE="true"
  else
    rm -f "$mode_file" 2>/dev/null || true
  fi
fi

# Check ANCHOR flag (set by /anchor command)
ANCHOR_MODE="false"
ANCHOR_GOAL=""
anchor_hash="$(printf '%s' "anchor:${SESSION_ID}" | shasum -a 256 | cut -c1-16)"
anchor_file="${mode_dir}/anchor-${anchor_hash}.json"
if [[ -f "$anchor_file" ]]; then
  updated_at="$(jq -r '.updated_at // 0' "$anchor_file" 2>/dev/null || echo 0)"
  if [[ "$updated_at" =~ ^[0-9]+$ ]] && (( now_ms - updated_at <= FLAG_TTL_MS )); then
    ANCHOR_MODE="true"
    ANCHOR_GOAL="$(jq -r '.goal // ""' "$anchor_file" 2>/dev/null || echo '')"
  else
    rm -f "$anchor_file" 2>/dev/null || true
  fi
fi

# If neither anchor nor todo mode is active, allow stop
if [[ "$ANCHOR_MODE" != "true" && "$TODO_MODE" != "true" ]]; then
  echo '{"decision": "approve"}'
  exit 0
fi

# If anchor mode is active, fetch full anchor from session/anchor skill
FULL_ANCHOR=""
if [[ "$ANCHOR_MODE" == "true" && -n "$SESSION_ID" ]]; then
  ANCHOR_INPUT=$(jq -nc --arg ws "$WORKSPACE" --arg sid "$SESSION_ID" '{operation:"get", workspace:$ws, session_id:$sid}')
  ANCHOR_RESULT=$("$AGENTCTL" run session/anchor --input "$ANCHOR_INPUT" 2>/dev/null) || true
  if [[ -n "$ANCHOR_RESULT" ]]; then
    ANCHOR_FOUND=$(echo "$ANCHOR_RESULT" | jq -r '.data.found // false' 2>/dev/null || echo 'false')
    if [[ "$ANCHOR_FOUND" == "true" ]]; then
      FULL_ANCHOR=$(echo "$ANCHOR_RESULT" | jq -r '.data.anchor.main_prompt // ""' 2>/dev/null || echo '')
      # Use full anchor if available, otherwise fall back to flag file goal
      if [[ -n "$FULL_ANCHOR" ]]; then
        ANCHOR_GOAL="$FULL_ANCHOR"
      fi
    fi
  fi
fi

# Check tasks scoped to this session
LIST_INPUT=$(jq -n --arg ws "$WORKSPACE" --arg sid "$SESSION_ID" '{operation:"list", workspace_id:$ws, list:{session_id:$sid}}')
LIST_RESULT=$("$AGENTCTL" run todo/manage --input "$LIST_INPUT" 2>/dev/null) || {
  echo '{"decision": "approve"}'
  exit 0
}

TASKS=$(echo "$LIST_RESULT" | jq -c '.data.tasks // []' 2>/dev/null || echo '[]')
OPEN_TASKS=$(echo "$TASKS" | jq -c 'map(select(.status != "completed"))' 2>/dev/null || echo '[]')
OPEN_COUNT=$(echo "$OPEN_TASKS" | jq -r 'length' 2>/dev/null || echo '0')

if [[ "$OPEN_COUNT" == "0" ]]; then
  # All tasks done - clear anchor flag and allow stop
  rm -f "$anchor_file" 2>/dev/null || true
  echo '{"decision": "approve"}'
  exit 0
fi

PENDING_COUNT=$(echo "$OPEN_TASKS" | jq -r '[.[] | select(.status == "pending")] | length' 2>/dev/null || echo '0')
BLOCKED_COUNT=$(echo "$OPEN_TASKS" | jq -r '[.[] | select(.status == "blocked")] | length' 2>/dev/null || echo '0')
IN_PROGRESS_COUNT=$(echo "$OPEN_TASKS" | jq -r '[.[] | select(.status == "in_progress")] | length' 2>/dev/null || echo '0')
TASK_LINES=$(echo "$OPEN_TASKS" | jq -r --argjson n "$TOP_N" '(
    map(select(.status == "pending"))
    + map(select(.status == "in_progress"))
    + map(select(.status == "blocked"))
  )[:$n]
  | to_entries
  | map("  \(.key + 1). \(.value.title // .value.id)")
  | join("\n")' 2>/dev/null || echo '')

INJECT_PROMPT="[SYSTEM REMINDER - ANCHOR MODE ACTIVE]"

# Format anchor goal (truncate very long anchors)
if [[ -n "$ANCHOR_GOAL" ]]; then
  # Check if anchor is multi-line or long
  ANCHOR_LINES=$(echo "$ANCHOR_GOAL" | wc -l | tr -d ' ')
  ANCHOR_LEN=${#ANCHOR_GOAL}

  if [[ "$ANCHOR_LINES" -gt 5 || "$ANCHOR_LEN" -gt 500 ]]; then
    # Truncate to first 500 chars for display
    DISPLAY_GOAL="${ANCHOR_GOAL:0:500}"
    if [[ "$ANCHOR_LEN" -gt 500 ]]; then
      DISPLAY_GOAL="${DISPLAY_GOAL}...(truncated)"
    fi
    INJECT_PROMPT="${INJECT_PROMPT}\n\n**Session Anchor (Epic Goal)**:\n\`\`\`\n${DISPLAY_GOAL}\n\`\`\`"
  else
    INJECT_PROMPT="${INJECT_PROMPT}\n\n**Goal**: ${ANCHOR_GOAL}"
  fi
fi

INJECT_PROMPT="${INJECT_PROMPT}\n\nIncomplete tasks: ${OPEN_COUNT} (${PENDING_COUNT} pending, ${BLOCKED_COUNT} blocked, ${IN_PROGRESS_COUNT} in progress)"
if [[ -n "$TASK_LINES" ]]; then
  INJECT_PROMPT="${INJECT_PROMPT}\n\n**NEXT TASKS**:\n${TASK_LINES}"
fi
INJECT_PROMPT="${INJECT_PROMPT}\n\n**Instructions**:\n- Continue working on the anchor goal\n- Mark each task complete when finished\n- Do not stop until anchor work is complete\n- Use /anchor off to disable this check"

reason="Incomplete tasks remain (${OPEN_COUNT} incomplete)"
jq -n \
  --arg reason "$reason" \
  --arg prompt "$INJECT_PROMPT" \
  --argjson pending "$PENDING_COUNT" \
  --argjson blocked "$BLOCKED_COUNT" \
  --argjson in_progress "$IN_PROGRESS_COUNT" \
  --argjson incomplete "$OPEN_COUNT" \
  '{decision:"block", reason:$reason, inject_prompt:$prompt}'
exit 0
