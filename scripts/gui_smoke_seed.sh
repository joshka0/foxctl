#!/usr/bin/env bash
set -euo pipefail

API_URL="${AGENTCTL_GUI_API_URL:-http://localhost:8090}"
WORKSPACE_PATH="${1:-$PWD}"
ROOM_ID="${ROOM_ID:-gui-smoke}"
ROOM_TITLE="${ROOM_TITLE:-GUI Smoke Room}"
ROOM_DESCRIPTION="${ROOM_DESCRIPTION:-Seeded smoke room visible in gui-agent}"
REQUEST_ID="req-gui-smoke-$(date +%s)"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require curl
require jq

echo "Seeding gui smoke data"
echo "API: ${API_URL}"
echo "Workspace: ${WORKSPACE_PATH}"
echo "Room: ${ROOM_ID}"

curl -sf "${API_URL}/api/health" >/dev/null

agents_json="$(curl -sf "${API_URL}/api/agents?limit=200")"
members_json="$(printf '%s' "${agents_json}" | jq --arg ws "${WORKSPACE_PATH}" '
  [
    .agents[]
    | select(.ns == $ws)
    | {actor_id: .id, role: (.role // "")}
  ]
')"

room_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg id "${ROOM_ID}" \
  --arg title "${ROOM_TITLE}" \
  --arg description "${ROOM_DESCRIPTION}" \
  --argjson members "${members_json}" \
  '{
    workspace_id: $workspace_id,
    id: $id,
    title: $title,
    description: $description,
    members: $members
  }')"

curl -sf -X POST "${API_URL}/api/rooms" \
  -H 'Content-Type: application/json' \
  -d "${room_payload}" >/dev/null

first_message_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  --arg body "Room seeded for GUI inspection at $(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{
    workspace_id: $workspace_id,
    sender: "human:gui",
    subject: "GUI smoke seed",
    body: $body
  }')"

curl -sf -X POST "${API_URL}/api/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -d "${first_message_payload}" >/dev/null

second_message_payload="$(jq -n \
  --arg workspace_id "${WORKSPACE_PATH}" \
  '{
    workspace_id: $workspace_id,
    sender: "human:gui",
    subject: "Next step",
    body: "Use Runtime -> Room Control or Rooms to inspect and update members."
  }')"

curl -sf -X POST "${API_URL}/api/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -d "${second_message_payload}" >/dev/null

cards_payload="$(jq -n \
  --arg request_id "${REQUEST_ID}" \
  --arg workspace_id "${WORKSPACE_PATH}" \
  '{
    request_id: $request_id,
    workspace_id: $workspace_id,
    cards: [
      {
        issue_id: "gui-smoke-card-1",
        issue_identifier: "GUI-SMOKE-1",
        title: "Inspect room transcript and member list"
      },
      {
        issue_id: "gui-smoke-card-2",
        issue_identifier: "GUI-SMOKE-2",
        title: "Open runtime room workflow and spawn into room"
      }
    ]
  }')"

curl -sf -X POST "${API_URL}/api/orchestration/seed-cards" \
  -H 'Content-Type: application/json' \
  -d "${cards_payload}" >/dev/null

echo
echo "Seed complete"
echo "Room endpoint:"
echo "  ${API_URL}/api/rooms/${ROOM_ID}?workspace_id=${WORKSPACE_PATH}"
echo "Room messages endpoint:"
echo "  ${API_URL}/api/rooms/${ROOM_ID}/messages?workspace_id=${WORKSPACE_PATH}&limit=20"
echo "Orchestration board endpoint:"
echo "  ${API_URL}/api/orchestration/board-get?workspace_id=${WORKSPACE_PATH}&limit=20"
