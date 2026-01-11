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

SESSION_ID="${AGENTCTL_SESSION_ID:-${OPENCODE_SESSION_ID:-${CLAUDE_SESSION_ID:-}}}"
if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
  SESSION_ID="$(echo "$INPUT" | jq -r '.sessionID // .session_id // ""' 2>/dev/null || true)"
fi
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

TODO_MODE="false"
TODO_MODE_TTL_MS=$((6 * 60 * 60 * 1000))
mode_dir="${AGENTCTL_HOME:-$HOME/.agentctl}/cache/session-modes"
mode_hash="$(printf '%s' "todo:${SESSION_ID}" | shasum -a 256 | cut -c1-16)"
mode_file="${mode_dir}/todo-${mode_hash}.json"
if [[ -f "$mode_file" ]]; then
  updated_at="$(jq -r '.updated_at // 0' "$mode_file" 2>/dev/null || echo 0)"
  if [[ "$updated_at" =~ ^[0-9]+$ ]]; then
    now_ms="$(( $(date +%s) * 1000 ))"
    if (( now_ms - updated_at <= TODO_MODE_TTL_MS )); then
      TODO_MODE="true"
    else
      rm -f "$mode_file" 2>/dev/null || true
    fi
  fi
fi

ANCHOR_INPUT=$(jq -n --arg ws "$WORKSPACE" --arg sid "$SESSION_ID" '{operation: "get", workspace: $ws, session_id: $sid}')
ANCHOR_RESULT=$("$AGENTCTL" run session/anchor --input "$ANCHOR_INPUT" 2>/dev/null || echo '')
ANCHOR_MAIN=$(echo "$ANCHOR_RESULT" | jq -r '.data.anchor.main_prompt // ""' 2>/dev/null || echo '')
ANCHOR_Q=$(echo "$ANCHOR_RESULT" | jq -r '.data.anchor.pending_question // ""' 2>/dev/null || echo '')

if [[ -z "$ANCHOR_MAIN" || "$ANCHOR_MAIN" == "null" ]]; then
  if [[ "$TODO_MODE" != "true" ]]; then
    echo '{"decision": "approve"}'
    exit 0
  fi

  LIST_INPUT=$(jq -n --arg ws "$WORKSPACE" --arg sid "$SESSION_ID" '{operation:"list", workspace_id:$ws, list:{session_id:$sid}}')
  LIST_RESULT=$("$AGENTCTL" run todo/manage --input "$LIST_INPUT" 2>/dev/null) || {
    echo '{"decision": "approve"}'
    exit 0
  }

  TASKS=$(echo "$LIST_RESULT" | jq -c '.data.tasks // []' 2>/dev/null || echo '[]')
  OPEN_TASKS=$(echo "$TASKS" | jq -c 'map(select(.status != "completed" and .status != "canceled"))' 2>/dev/null || echo '[]')
  OPEN_COUNT=$(echo "$OPEN_TASKS" | jq -r 'length' 2>/dev/null || echo '0')
  if [[ "$OPEN_COUNT" == "0" ]]; then
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

  INJECT_PROMPT="[SYSTEM REMINDER - TODO CHECK-IN]\n\nIncomplete tasks: ${OPEN_COUNT} (${PENDING_COUNT} pending, ${BLOCKED_COUNT} blocked, ${IN_PROGRESS_COUNT} in progress)"
  if [[ -n "$TASK_LINES" ]]; then
    INJECT_PROMPT="${INJECT_PROMPT}\n\n**NEXT TASKS**:\n${TASK_LINES}"
  fi
  INJECT_PROMPT="${INJECT_PROMPT}\n\n- Proceed without asking for permission\n- Mark each task complete when finished\n- Do not stop until all tasks are done"

  reason="Incomplete tasks remain (${OPEN_COUNT} incomplete)"
  jq -n \
    --arg reason "$reason" \
    --arg prompt "$INJECT_PROMPT" \
    --argjson pending "$PENDING_COUNT" \
    --argjson blocked "$BLOCKED_COUNT" \
    --argjson in_progress "$IN_PROGRESS_COUNT" \
    --argjson incomplete "$OPEN_COUNT" \
    '{decision:"block", reason:$reason, inject_prompt:$prompt, stop_hook_active:true, metadata:{incomplete_count:$incomplete, pending_count:$pending, blocked_count:$blocked, in_progress_count:$in_progress, todo_mode:true}}'
  exit 0
fi

CONT_INPUT=$(jq -n \
  --arg ws "$WORKSPACE" \
  --arg sid "$SESSION_ID" \
  --arg goal "$ANCHOR_MAIN" \
  --arg pending "$ANCHOR_Q" \
  --argjson top_n "$TOP_N" \
  --argjson min_pending "$MIN_PENDING" \
  '{
    workspace_id: $ws,
    session_id: $sid,
    top_n: $top_n,
    min_pending: $min_pending,
    anchor_goal: $goal,
    anchor_pending: $pending
  }'
)

CONT_RESULT=$("$AGENTCTL" run todo/continuation --input "$CONT_INPUT" 2>/dev/null) || {
  echo '{}'
  exit 0
}

CONT_DATA=$(echo "$CONT_RESULT" | jq -c '.data // {}' 2>/dev/null || echo '{}')
SHOULD_CONTINUE=$(echo "$CONT_DATA" | jq -r '.should_continue // false' 2>/dev/null || echo 'false')
INCOMPLETE_COUNT=$(echo "$CONT_DATA" | jq -r '.incomplete_count // 0' 2>/dev/null || echo '0')
UNSCOPED_INCOMPLETE_COUNT=$(echo "$CONT_DATA" | jq -r '.unscoped_incomplete_count // 0' 2>/dev/null || echo '0')
READY_COUNT=$(echo "$CONT_DATA" | jq -r '.ready_count // 0' 2>/dev/null || echo '0')
BLOCKED_COUNT=$(echo "$CONT_DATA" | jq -r '.blocked_count // 0' 2>/dev/null || echo '0')
IN_PROGRESS_COUNT=$(echo "$CONT_DATA" | jq -r '.in_progress_count // 0' 2>/dev/null || echo '0')
CYCLE_COUNT=$(echo "$CONT_DATA" | jq -r '.cycle_count // 0' 2>/dev/null || echo '0')
INJECT_PROMPT=$(echo "$CONT_DATA" | jq -r '.prompt // ""' 2>/dev/null || echo '')

emit_approve() {
  if [[ "${UNSCOPED_INCOMPLETE_COUNT:-0}" != "0" && "${UNSCOPED_INCOMPLETE_COUNT:-0}" != "null" ]]; then
    jq -n --arg warning "WARNING: ${UNSCOPED_INCOMPLETE_COUNT} incomplete tasks in this workspace have no session_id (ignored for this session)." '{decision: "approve", warning: $warning}'
    return 0
  fi

  echo '{"decision": "approve"}'
}

if [[ "$SHOULD_CONTINUE" != "true" ]]; then
  # CoVe verification is OFF by default (opt-in via AGENTCTL_TODO_CONTINUATION_VERIFY=1)
  # When disabled, we approve if incomplete_count==0 and no pending question
  VERIFY="${AGENTCTL_TODO_CONTINUATION_VERIFY:-0}"
  if [[ "$VERIFY" != "1" ]]; then
    emit_approve
    exit 0
  fi

  if [[ -n "${ANCHOR_Q:-}" && "${ANCHOR_Q:-}" != "null" ]]; then
    jq -n --arg reason "Pending anchor question: ${ANCHOR_Q}" '{decision: "block", reason: $reason}'
    exit 0
  fi

  if [[ -z "${ANCHOR_MAIN:-}" || "${ANCHOR_MAIN:-}" == "null" ]]; then
    emit_approve
    exit 0
  fi

  baseline=$(cat <<EOF
Claims:
- Incomplete task count is 0.
- There is no pending anchor question.
- Anchor goal is: ${ANCHOR_MAIN}
EOF
)

  COVE_INPUT=$(jq -n \
    --arg q "Is the Definition of Done met to stop? DoD: incomplete_task_count==0 AND pending_question==empty." \
    --arg baseline "$baseline" \
    --arg goal "$ANCHOR_MAIN" \
    '{question: $q, baseline: $baseline, mode: "gate", context: {anchor_goal: $goal, incomplete_task_count: 0, pending_question: ""}}'
  )

  COVE_RESULT=$("$AGENTCTL" run verification/cove_verify --input "$COVE_INPUT" 2>/dev/null) || {
    # Graceful degradation: if verification fails, approve (tasks=0, no pending question)
    jq -n --arg warning "CoVe verification failed (API key missing?). Approving based on task count." '{decision: "approve", warning: $warning}'
    exit 0
  }

  FINAL=$(echo "$COVE_RESULT" | jq -r '.data.result.final_answer // ""' 2>/dev/null || echo "")

  if printf '%s' "$FINAL" | grep -q '^STATUS: DONE'; then
    emit_approve
    exit 0
  fi

  jq -n --arg reason "$FINAL" '{decision: "block", reason: $reason}'
  exit 0
fi

reason="Incomplete tasks remain (${INCOMPLETE_COUNT} incomplete)"
if [[ "$INCOMPLETE_COUNT" == "0" && -n "${ANCHOR_Q:-}" && "${ANCHOR_Q:-}" != "null" ]]; then
  reason="Pending anchor question: ${ANCHOR_Q}"
fi

jq -n \
  --arg reason "$reason" \
  --arg prompt "$INJECT_PROMPT" \
  --argjson cycles "$CYCLE_COUNT" \
  --argjson ready "$READY_COUNT" \
  --argjson blocked "$BLOCKED_COUNT" \
  --argjson in_progress "$IN_PROGRESS_COUNT" \
  '{
    decision: "block",
    reason: $reason,
    inject_prompt: $prompt,
    stop_hook_active: true,
    metadata: {
      incomplete_count: '"$INCOMPLETE_COUNT"',
      ready_count: $ready,
      blocked_count: $blocked,
      in_progress_count: $in_progress,
      cycle_count: $cycles
    }
  }'
