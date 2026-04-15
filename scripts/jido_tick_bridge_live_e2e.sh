#!/usr/bin/env bash
set -euo pipefail

API_URL="${FOXCTL_GUI_API_URL:-http://localhost:8090}"
LMSTUDIO_URL="${LMSTUDIO_BASE_URL:-http://127.0.0.1:1234/v1}"
WORKSPACE_PATH="${1:-$PWD}"
JIDO_REPO="${JIDO_REPO:-$HOME/repos/githubs/jido}"
JIDO_SOCKET="${FOXCTL_JIDO_SOCKET:-/tmp/foxctl-jido.sock}"
ROOM_ID="${ROOM_ID:-jido-tick-room}"
ROOM_TITLE="${ROOM_TITLE:-Jido Tick Bridge E2E}"
ISSUE_ID="${ISSUE_ID:-jido-tick-issue-1}"
ISSUE_IDENTIFIER="${ISSUE_IDENTIFIER:-JIDO-TICK-1}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"
TICK_INTERVAL_SECONDS="${TICK_INTERVAL_SECONDS:-3}"
BRIDGE_LOG="${BRIDGE_LOG:-/tmp/jido-foxctl-bridge.log}"

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

rpc_call() {
  local method="$1"
  local params="$2"
  curl -sf --unix-socket "${JIDO_SOCKET}" \
    -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"$(date +%s%N)\",\"method\":\"${method}\",\"params\":${params}}" \
    http://localhost/
}

require curl
require jq

echo "Running Jido tick bridge live e2e"
echo "API: ${API_URL}"
echo "LM Studio: ${LMSTUDIO_URL}"
echo "Workspace: ${WORKSPACE_PATH}"
echo "Jido repo: ${JIDO_REPO}"
echo "Jido socket: ${JIDO_SOCKET}"

curl -sf "${API_URL}/api/health" >/dev/null

models_json="$(curl -sf "${LMSTUDIO_URL}/models")"
TICK_MODEL="${TICK_MODEL:-$(printf '%s' "${models_json}" | jq -r '.data[1].id // .data[0].id // empty')}"
if [ -z "${TICK_MODEL}" ]; then
  echo "unable to determine LM Studio model from ${LMSTUDIO_URL}/models" >&2
  exit 1
fi

echo "Tick worker model: ${TICK_MODEL}"

bridge_started=0
bridge_pid=""
cleanup() {
  if [ "${bridge_started}" = "1" ] && [ -n "${bridge_pid}" ]; then
    kill "${bridge_pid}" >/dev/null 2>&1 || true
    wait "${bridge_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [ ! -S "${JIDO_SOCKET}" ]; then
  if ! command -v mix >/dev/null 2>&1; then
    echo "Jido bridge socket not found and mix is unavailable to start it" >&2
    exit 1
  fi
  if [ ! -d "${JIDO_REPO}" ]; then
    echo "Jido repo not found: ${JIDO_REPO}" >&2
    exit 1
  fi
  FOXCTL_BIN="${FOXCTL_BIN:-$(cd "$(dirname "$0")/.." && pwd)/bin/foxctl}"
  echo "Starting Jido bridge via mix in ${JIDO_REPO}"
  (
    cd "${JIDO_REPO}"
    FOXCTL_JIDO_SOCKET="${JIDO_SOCKET}" \
    FOXCTL_WORKSPACE="${WORKSPACE_PATH}" \
    FOXCTL_BIN="${FOXCTL_BIN}" \
    mix jido.foxctl.bridge --quiet
  ) >"${BRIDGE_LOG}" 2>&1 &
  bridge_pid=$!
  bridge_started=1
  poll_until "jido bridge socket" "test -S \"${JIDO_SOCKET}\"" "${TIMEOUT_SECONDS}"
fi

room_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg id "${ROOM_ID}" \
  --arg title "${ROOM_TITLE}" \
  '{
    workspace_id: $workspace_id,
    id: $id,
    title: $title,
    description: "Live Jido tick bridge room"
  }')"

curl -sf -X POST "${API_URL}/api/rooms" \
  -H 'Content-Type: application/json' \
  -d "${room_payload}" >/dev/null || true

seed_payload="$(jq -n \
  --arg request_id "req-jido-tick-seed-$(date +%s)" \
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
        title: "Complete Jido tick bridge e2e"
      }
    ]
  }')"

curl -sf -X POST "${API_URL}/api/orchestration/seed-cards" \
  -H 'Content-Type: application/json' \
  -d "${seed_payload}" >/dev/null

TICK_AGENT_ID="${TICK_AGENT_ID:-jido-tick-bridge-root}"

tick_prompt="$(cat <<EOF
Reply with exactly:
ROOM-BOARD-DONE ${ISSUE_ID}: smoke ok
EOF
)"

tick_agent_prompt="$(cat <<EOF
You are a deterministic smoke-test worker.
When the user message contains "Reply with exactly:", output exactly the text that follows, with no code fences, commentary, or suffixes.
If no such phrase exists, reply with exactly: NO_WORK_NEEDED
EOF
)"

start_params="$(jq -n \
  --arg agent_id "${TICK_AGENT_ID}" \
  --arg exec_mode "tick" \
  --arg prompt "${tick_prompt}" \
  --arg tick_agent_prompt "${tick_agent_prompt}" \
  --arg room_id "${ROOM_ID}" \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg issue_id "${ISSUE_ID}" \
  --arg issue_identifier "${ISSUE_IDENTIFIER}" \
  --arg llm_model "${TICK_MODEL}" \
  --argjson think_interval "${TICK_INTERVAL_SECONDS}" \
  '{
    agent_id: $agent_id,
    exec_mode: $exec_mode,
    think_interval: $think_interval,
    initial_state: {
      exec_mode: $exec_mode,
      prompt: $prompt,
      tick_prompt: $prompt,
      tick_agent_prompt: $tick_agent_prompt,
      think_interval: $think_interval,
      tick_agent_skills: ["think"],
      room_id: $room_id,
      tick_context: {
        workspace_id: $workspace_id,
        room_id: $room_id,
        issue_id: $issue_id,
        issue_identifier: $issue_identifier
      },
      tick_agent_model: $llm_model
    }
  }')"

start_response="$(rpc_call "runtime.start_agent" "${start_params}")"
echo "runtime.start_agent => ${start_response}"

poll_until "jido tick bridge state" \
  "state=\$(rpc_call \"runtime.state\" '{\"agent_id\":\"${TICK_AGENT_ID}\"}'); \
   count=\$(printf '%s' \"\$state\" | jq -r '.result.state.tick.count // 0'); \
   test \"\$count\" -ge 1" \
  "${TIMEOUT_SECONDS}"

poll_until "backing tick agent appears in foxctl" \
  "agent_json=\$(curl -sf \"${API_URL}/api/agents?limit=200\"); \
   backing=\$(printf '%s' \"\$agent_json\" | jq -r '.agents | map(select(.name == \"tick-agent\" and .llm_provider == \"lmstudio\" and .state == \"running\")) | first | .id // empty'); \
   test -n \"\$backing\"" \
  "${TIMEOUT_SECONDS}"

BACKING_AGENT_ID="$(curl -sf "${API_URL}/api/agents?limit=200" | jq -r '.agents | map(select(.name == "tick-agent" and .llm_provider == "lmstudio" and .state == "running")) | first | .id // empty')"
if [ -z "${BACKING_AGENT_ID}" ]; then
  echo "backing agent id not found in foxctl" >&2
  exit 1
fi

echo "Backing agent: ${BACKING_AGENT_ID}"

poll_until "ROOM-BOARD-DONE reply from backing agent through Jido tick bridge" \
  "state=\$(rpc_call \"runtime.state\" '{\"agent_id\":\"${TICK_AGENT_ID}\"}' 2>/dev/null || true); \
   reply=\$(printf '%s' \"\$state\" | jq -r '.result.state.foxctl.last_result.reply // \"\"' 2>/dev/null || true); \
   printf '%s' \"\$reply\" | grep -q '^ROOM-BOARD-DONE ${ISSUE_ID}:'" \
  "${TIMEOUT_SECONDS}"

final_state_json="$(rpc_call "runtime.state" "{\"agent_id\":\"${TICK_AGENT_ID}\"}" 2>/dev/null || true)"
final_reply="$(printf '%s' "${final_state_json}" | jq -r '.result.state.foxctl.last_result.reply // ""' 2>/dev/null || true)"
tick_count="$(printf '%s' "${final_state_json}" | jq -r '.result.state.tick.count // 0' 2>/dev/null || true)"

if [ -z "${final_reply}" ]; then
  echo "final reply missing from Jido tick bridge state" >&2
  echo "${final_state_json}" >&2
  exit 1
fi

room_message_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg sender "jido:${TICK_AGENT_ID}" \
  --arg task_id "${ISSUE_ID}" \
  --arg body "${final_reply}" \
  '{
    workspace_id: $workspace_id,
    sender: $sender,
    task_id: $task_id,
    body: $body
  }')"

curl -sf -X POST "${API_URL}/api/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -d "${room_message_payload}" >/dev/null

poll_until "board card marked done from Jido bridge room post" \
  "test \"\$(curl -sf \"${API_URL}/api/orchestration/board-card-get?workspace_id=${WORKSPACE_PATH}&issue_id=${ISSUE_ID}\" | jq -r '.data.card.tracker_state // \"\"')\" = \"Done\"" \
  "${TIMEOUT_SECONDS}"

echo
echo "Jido tick bridge live e2e complete"
echo "Jido tick bridge agent: ${TICK_AGENT_ID}"
echo "Jido backing agent: ${BACKING_AGENT_ID}"
echo "Tick count: ${tick_count}"
echo "Final reply: ${final_reply}"
echo "Room:"
echo "  ${API_URL}/api/rooms/${ROOM_ID}?workspace_id=${WORKSPACE_PATH}"
echo "Transcript:"
echo "  ${API_URL}/api/rooms/${ROOM_ID}/messages?workspace_id=${WORKSPACE_PATH}&limit=50"
echo "Board card:"
echo "  ${API_URL}/api/orchestration/board-card-get?workspace_id=${WORKSPACE_PATH}&issue_id=${ISSUE_ID}"
if [ "${bridge_started}" = "1" ]; then
  echo "Bridge log:"
  echo "  ${BRIDGE_LOG}"
fi
