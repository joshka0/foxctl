#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEFAULT_AGENTCTL_BIN="${REPO_ROOT}/bin/agentctl-cgo"
DEFAULT_HEARTWOOD_ROOT="$(cd "${REPO_ROOT}/../heartwood" 2>/dev/null && pwd || true)"

if [ -z "${AGENTCTL_BIN:-}" ]; then
  if [ -x "${DEFAULT_AGENTCTL_BIN}" ]; then
    AGENTCTL_BIN="${DEFAULT_AGENTCTL_BIN}"
  else
    AGENTCTL_BIN="$(command -v agentctl || true)"
  fi
fi
if [ -z "${AGENTCTL_BIN}" ]; then
  echo "AGENTCTL_BIN is not set and no agentctl binary was found on PATH" >&2
  exit 1
fi

HEARTWOOD_ROOT="${HEARTWOOD_ROOT:-${DEFAULT_HEARTWOOD_ROOT}}"
if [ -z "${HEARTWOOD_ROOT}" ] || [ ! -d "${HEARTWOOD_ROOT}" ]; then
  echo "HEARTWOOD_ROOT is not set and no sibling heartwood repo was found" >&2
  exit 1
fi

HEARTWOOD_HOST="${HEARTWOOD_HOST:-ws://127.0.0.1:3001}"
HEARTWOOD_DB_NAME="${HEARTWOOD_DB_NAME:-heartwood}"
TOKEN_PATH="${TOKEN_PATH:-/tmp/hw-classic-agent-smoke.token}"
MODEL="${MODEL:-qwen/qwen3.5-35b-a3b}"
TIMEOUT="${TIMEOUT:-90s}"
ALIAS="${ALIAS:-AgentMira}"
CITY="${CITY:-Tallinn}"
INTENT="${INTENT:-curious}"
RUN_SUFFIX="${RUN_SUFFIX:-$(date +%s)}"
AGENT_NAME="${AGENT_NAME:-hw-classic-sim-${RUN_SUFFIX}}"
AGENT_SLUG="${AGENT_SLUG:-hw-classic-sim-${RUN_SUFFIX}}"

echo "Running Heartwood agent action smoke"
echo "Heartwood root: ${HEARTWOOD_ROOT}"
echo "Heartwood host: ${HEARTWOOD_HOST}"
echo "Heartwood db: ${HEARTWOOD_DB_NAME}"
echo "Model: ${MODEL}"

SPAWN="$("${AGENTCTL_BIN}" agent spawn \
  --role coder \
  --name "${AGENT_NAME}" \
  --slug "${AGENT_SLUG}" \
  --llm-provider lmstudio \
  --llm-model "${MODEL}" \
  --skills-allow '["heartwood_state","heartwood_action","think"]' \
  --prompt 'You are a Heartwood simulator agent. Use heartwood_state and heartwood_action when asked.' )"

echo "${SPAWN}"
AGENT_ID="$(printf '%s' "${SPAWN}" | jq -r '.data.agent_id')"

for _ in $(seq 1 15); do
  if "${AGENTCTL_BIN}" agent info "${AGENT_ID}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

QUESTION="$(cat <<EOF
Call heartwood_action with:
- heartwood_root=${HEARTWOOD_ROOT}
- host=${HEARTWOOD_HOST}
- db_name=${HEARTWOOD_DB_NAME}
- token_path=${TOKEN_PATH}
- operation=upsert_profile
- args={alias:\"${ALIAS}\", city:\"${CITY}\", intent:\"${INTENT}\"}

Then call heartwood_state with the same heartwood_root, host, db_name, and token_path.

Reply with exactly:
ALIAS=<alias> CITY=<city> INTENT=<intent>
EOF
)"

ASK="$("${AGENTCTL_BIN}" agent ask "${AGENT_ID}" --question "${QUESTION}" --wait --timeout "${TIMEOUT}")"

echo
echo "${ASK}"

echo
echo "Heartwood agent action smoke complete"
