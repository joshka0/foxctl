#!/usr/bin/env bash
set -euo pipefail

API_URL="${FOXCTL_GUI_API_URL:-http://localhost:8090}"
LMSTUDIO_URL="${LMSTUDIO_BASE_URL:-http://127.0.0.1:1234/v1}"
WORKSPACE_PATH="${1:-$PWD}"
ROOM_ID="${ROOM_ID:-room-board-e2e}"
ROOM_TITLE="${ROOM_TITLE:-Room Board E2E}"
ISSUE_ID="${ISSUE_ID:-room-board-e2e-issue-1}"
ISSUE_IDENTIFIER="${ISSUE_IDENTIFIER:-ROOM-BOARD-E2E-1}"
LEAD_ROLE="${LEAD_ROLE:-researcher}"
WORKER_ROLE="${WORKER_ROLE:-coder}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

poll_until() {
  local description="$1"
  local command="$2"
  local timeout="${3:-$TIMEOUT_SECONDS}"
  local start
  start="$(date +%s)"
  while true; do
    if eval "$command"; then
      return 0
    fi
    if [ "$(( $(date +%s) - start ))" -ge "$timeout" ]; then
      echo "timed out waiting for: ${description}" >&2
      return 1
    fi
    sleep 2
  done
}

require curl
require jq

echo "Running room-board live e2e"
echo "API: ${API_URL}"
echo "LM Studio: ${LMSTUDIO_URL}"
echo "Workspace: ${WORKSPACE_PATH}"
echo "Room: ${ROOM_ID}"
echo "Issue: ${ISSUE_ID}"

curl -sf "${API_URL}/api/health" >/dev/null

models_json="$(curl -sf "${LMSTUDIO_URL}/models")"
LEAD_MODEL="${LEAD_MODEL:-$(printf '%s' "${models_json}" | jq -r '.data[0].id // empty')}"
WORKER_MODEL="${WORKER_MODEL:-$(printf '%s' "${models_json}" | jq -r '.data[1].id // .data[0].id // empty')}"

if [ -z "${LEAD_MODEL}" ] || [ -z "${WORKER_MODEL}" ]; then
  echo "unable to determine LM Studio models from ${LMSTUDIO_URL}/models" >&2
  exit 1
fi

echo "Lead model: ${LEAD_MODEL}"
echo "Worker model: ${WORKER_MODEL}"

room_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg id "${ROOM_ID}" \
  --arg title "${ROOM_TITLE}" \
  '{
    workspace_id: $workspace_id,
    id: $id,
    title: $title,
    description: "Live e2e room for orchestration-board completion"
  }')"

curl -sf -X POST "${API_URL}/api/rooms" \
  -H 'Content-Type: application/json' \
  -d "${room_payload}" >/dev/null || true

seed_payload="$(jq -n \
  --arg request_id "req-room-board-live-seed-$(date +%s)" \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg issue_id "${ISSUE_ID}" \
  --arg issue_identifier "${ISSUE_IDENTIFIER}" \
  '{
    request_id: $request_id,
    workspace_id: $workspace_id,
    cards: [
      {
        issue_id: $issue_id,
        issue_identifier: $issue_identifier,
        title: "Complete room-board live e2e"
      }
    ]
  }')"

curl -sf -X POST "${API_URL}/api/orchestration/seed-cards" \
  -H 'Content-Type: application/json' \
  -d "${seed_payload}" >/dev/null

lead_prompt="$(cat <<EOF
You are the lead agent for issue ${ISSUE_IDENTIFIER} in room ${ROOM_ID}.
When you receive a room-dispatch message:
1. reply with one concise coordination update;
2. if the message explicitly asks for a final conclusion, reply with exactly this prefix:
ROOM-BOARD-DONE ${ISSUE_ID}:
Keep the rest of the reply short.
EOF
)"

worker_prompt="$(cat <<EOF
You are the worker agent for issue ${ISSUE_IDENTIFIER} in room ${ROOM_ID}.
When you receive a room-dispatch message, reply with one concise implementation-style update.
Do not use the ROOM-BOARD-DONE prefix.
EOF
)"

spawn_agent() {
  local role="$1"
  local prompt="$2"
  local model="$3"
  local room_role="$4"
  local payload
  payload="$(jq -n \
    --arg role "${role}" \
    --arg prompt "${prompt}" \
    --arg workspace_id "${WORKSPACE_PATH}" \
    --arg room_id "${ROOM_ID}" \
    --arg room_role "${room_role}" \
    --arg llm_provider "lmstudio" \
    --arg llm_model "${model}" \
    '{
      role: $role,
      prompt: $prompt,
      workspace_id: $workspace_id,
      room_id: $room_id,
      room_role: $room_role,
      llm_provider: $llm_provider,
      llm_model: $llm_model,
      exec_mode: "reactive"
    }')"
  curl -sf -X POST "${API_URL}/api/agents/spawn" \
    -H 'Content-Type: application/json' \
    -d "${payload}"
}

lead_spawn_json="$(spawn_agent "${LEAD_ROLE}" "${lead_prompt}" "${LEAD_MODEL}" "lead")"
worker_spawn_json="$(spawn_agent "${WORKER_ROLE}" "${worker_prompt}" "${WORKER_MODEL}" "worker")"

LEAD_AGENT_ID="$(printf '%s' "${lead_spawn_json}" | jq -r '.actor_id')"
WORKER_AGENT_ID="$(printf '%s' "${worker_spawn_json}" | jq -r '.actor_id')"

if [ -z "${LEAD_AGENT_ID}" ] || [ "${LEAD_AGENT_ID}" = "null" ] || [ -z "${WORKER_AGENT_ID}" ] || [ "${WORKER_AGENT_ID}" = "null" ]; then
  echo "failed to spawn lead/worker agents" >&2
  echo "lead: ${lead_spawn_json}" >&2
  echo "worker: ${worker_spawn_json}" >&2
  exit 1
fi

echo "Lead agent: ${LEAD_AGENT_ID}"
echo "Worker agent: ${WORKER_AGENT_ID}"

poll_until "lead agent running" \
  "test \"\$(curl -sf \"${API_URL}/api/agents/${LEAD_AGENT_ID}\" | jq -r '.state')\" = \"running\"" \
  "${TIMEOUT_SECONDS}"
poll_until "worker agent running" \
  "test \"\$(curl -sf \"${API_URL}/api/agents/${WORKER_AGENT_ID}\" | jq -r '.state')\" = \"running\"" \
  "${TIMEOUT_SECONDS}"

members_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg lead "${LEAD_AGENT_ID}" \
  --arg worker "${WORKER_AGENT_ID}" \
  '{
    workspace_id: $workspace_id,
    members: [
      { actor_id: $lead, role: "lead" },
      { actor_id: $worker, role: "worker" }
    ]
  }')"

curl -sf -X PATCH "${API_URL}/api/rooms/${ROOM_ID}/members?workspace_id=${WORKSPACE_PATH}" \
  -H 'Content-Type: application/json' \
  -d "${members_payload}" >/dev/null

first_message="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg issue_id "${ISSUE_ID}" \
  --arg lead "${LEAD_AGENT_ID}" \
  --arg worker "${WORKER_AGENT_ID}" \
  '{
    workspace_id: $workspace_id,
    sender: "human:gui",
    task_id: $issue_id,
    subject: "Start work",
    body: "Coordinate on the issue and report status in the room.",
    dispatch_agents: true,
    dispatch_agent_ids: [$lead, $worker],
    context: {
      issue_id: $issue_id,
      issue_identifier: "'${ISSUE_IDENTIFIER}'"
    }
  }')"

curl -sf -X POST "${API_URL}/api/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -d "${first_message}" >/dev/null

poll_until "both room agent replies" \
  "msgs=\$(curl -sf \"${API_URL}/api/rooms/${ROOM_ID}/messages?workspace_id=${WORKSPACE_PATH}&limit=50\"); \
   lead=\$(printf '%s' \"\$msgs\" | jq --arg id \"${LEAD_AGENT_ID}\" '[.messages[] | select(.sender == \$id)] | length'); \
   worker=\$(printf '%s' \"\$msgs\" | jq --arg id \"${WORKER_AGENT_ID}\" '[.messages[] | select(.sender == \$id)] | length'); \
   test \"\$lead\" -ge 1 && test \"\$worker\" -ge 1" \
  "${TIMEOUT_SECONDS}"

worker_summary="$(curl -sf "${API_URL}/api/rooms/${ROOM_ID}/messages?workspace_id=${WORKSPACE_PATH}&limit=50" | jq -r --arg id "${WORKER_AGENT_ID}" '[.messages[] | select(.sender == $id) | .body] | last // ""')"

second_message="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg issue_id "${ISSUE_ID}" \
  --arg lead "${LEAD_AGENT_ID}" \
  --arg worker_summary "${worker_summary}" \
  '{
    workspace_id: $workspace_id,
    sender: "human:gui",
    task_id: $issue_id,
    subject: "Finalize",
    body: ("Worker summary:\\n" + $worker_summary + "\\n\\nIf this is enough to close the issue, reply with the required ROOM-BOARD-DONE prefix."),
    dispatch_agents: true,
    dispatch_agent_ids: [$lead],
    context: {
      issue_id: $issue_id,
      final_conclusion: true
    }
  }')"

curl -sf -X POST "${API_URL}/api/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -d "${second_message}" >/dev/null

poll_until "lead completion reply" \
  "curl -sf \"${API_URL}/api/rooms/${ROOM_ID}/messages?workspace_id=${WORKSPACE_PATH}&limit=50\" | jq -r --arg id \"${LEAD_AGENT_ID}\" --arg issue \"ROOM-BOARD-DONE ${ISSUE_ID}:\" '[.messages[] | select(.sender == \$id) | .body] | any(startswith(\$issue))' | grep -qx true" \
  "${TIMEOUT_SECONDS}"

poll_until "board card marked done from room bridge" \
  "test \"\$(curl -sf \"${API_URL}/api/orchestration/board-card-get?workspace_id=${WORKSPACE_PATH}&issue_id=${ISSUE_ID}\" | jq -r '.data.card.tracker_state // \"\"')\" = \"Done\"" \
  "${TIMEOUT_SECONDS}"

echo
echo "Room-board live e2e complete"
echo "Room:"
echo "  ${API_URL}/api/rooms/${ROOM_ID}?workspace_id=${WORKSPACE_PATH}"
echo "Transcript:"
echo "  ${API_URL}/api/rooms/${ROOM_ID}/messages?workspace_id=${WORKSPACE_PATH}&limit=50"
echo "Board card:"
echo "  ${API_URL}/api/orchestration/board-card-get?workspace_id=${WORKSPACE_PATH}&issue_id=${ISSUE_ID}"
echo "Lead agent: ${LEAD_AGENT_ID}"
echo "Worker agent: ${WORKER_AGENT_ID}"
