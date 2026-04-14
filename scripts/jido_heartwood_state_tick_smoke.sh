#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEFAULT_HEARTWOOD_ROOT="$(cd "${REPO_ROOT}/../heartwood" 2>/dev/null && pwd || true)"

JIDO_SOCKET="${AGENTCTL_JIDO_SOCKET:-/tmp/foxctl-jido.sock}"
HEARTWOOD_ROOT="${HEARTWOOD_ROOT:-${DEFAULT_HEARTWOOD_ROOT}}"
if [ -z "${HEARTWOOD_ROOT}" ] || [ ! -d "${HEARTWOOD_ROOT}" ]; then
  echo "HEARTWOOD_ROOT is not set and no sibling heartwood repo was found" >&2
  exit 1
fi
HEARTWOOD_HOST="${HEARTWOOD_HOST:-ws://127.0.0.1:3001}"
HEARTWOOD_DB_NAME="${HEARTWOOD_DB_NAME:-heartwood}"
TOKEN_PATH="${TOKEN_PATH:-/tmp/hw-jido-tick-tool.token}"
AGENT_ID="${AGENT_ID:-jido-heartwood-state-tick-smoke}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-60}"

rpc() {
  local method="$1"
  local params="$2"
  curl -sf --unix-socket "${JIDO_SOCKET}" \
    -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"$(date +%s%N)\",\"method\":\"${method}\",\"params\":${params}}" \
    http://localhost/
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

echo "Running Jido Heartwood state tick smoke"
echo "Jido socket: ${JIDO_SOCKET}"
echo "Heartwood root: ${HEARTWOOD_ROOT}"
echo "Heartwood host: ${HEARTWOOD_HOST}"
echo "Heartwood db: ${HEARTWOOD_DB_NAME}"

rpc "runtime.health" '{}' >/dev/null

start_params="$(jq -n \
  --arg agent_id "${AGENT_ID}" \
  --arg exec_mode "tick" \
  --arg prompt "Run one deterministic Heartwood state probe." \
  --arg heartwood_root "${HEARTWOOD_ROOT}" \
  --arg host "${HEARTWOOD_HOST}" \
  --arg db_name "${HEARTWOOD_DB_NAME}" \
  --arg token_path "${TOKEN_PATH}" \
  '{
    agent_id: $agent_id,
    exec_mode: $exec_mode,
    think_interval: 3,
    initial_state: {
      exec_mode: $exec_mode,
      prompt: $prompt,
      tick_prompt: $prompt,
      think_interval: 3,
      tick_tool: "heartwood/state",
      tick_tool_input: {
        heartwood_root: $heartwood_root,
        host: $host,
        db_name: $db_name,
        token_path: $token_path,
        wait_timeout_ms: 8000,
        message_limit: 5
      }
    }
  }')"

start_response="$(rpc "runtime.start_agent" "${start_params}")"
echo "runtime.start_agent => ${start_response}"

poll_until "Heartwood tick result" \
  "state=\$(rpc 'runtime.state' '{\"agent_id\":\"${AGENT_ID}\"}' 2>/dev/null || true); \
   status=\$(printf '%s' \"\$state\" | jq -r '.result.state.foxctl.status // empty' 2>/dev/null || true); \
   [ \"\$status\" = \"completed\" ]" \
  "${TIMEOUT_SECONDS}"

final_state="$(rpc "runtime.state" "{\"agent_id\":\"${AGENT_ID}\"}")"
echo "${final_state}" | jq '.result.state.foxctl.last_result'

echo
echo "Jido Heartwood state tick smoke complete"
