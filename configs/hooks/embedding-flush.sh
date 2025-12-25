#!/usr/bin/env bash
# Stop hook: Flush pending embedding jobs before session ends
# This hook processes queued symbol embeddings accumulated during the session

set -euo pipefail

# Check if embedding queue is disabled
if [[ "${AGENTCTL_EMBED_QUEUE:-1}" == "0" ]]; then
  exit 0
fi

# Check if GEMINI_API_KEY is set (required for embedding generation)
if [[ -z "${GEMINI_API_KEY:-}" ]]; then
  # No API key, skip silently
  exit 0
fi

# agentctl binary location
AGENTCTL="${AGENTCTL_BIN:-$(dirname "$0")/../../bin/agentctl}"
if [[ ! -x "$AGENTCTL" ]]; then
  AGENTCTL="agentctl"
fi

# First check if there are pending jobs (quick stats check)
STATS=$("$AGENTCTL" run embedding/queue --input '{"operation": "stats"}' 2>/dev/null) || exit 0

QUEUED=$(echo "$STATS" | jq -r '.data.stats.queued_count // 0' 2>/dev/null)
if [[ "$QUEUED" == "0" || "$QUEUED" == "null" ]]; then
  # No pending jobs, skip
  exit 0
fi

# Process pending embeddings with reasonable limits for session end
# - batch_size: 50 (process a good chunk)
# - max_duration: 60 (don't block session end for too long)
exec "$AGENTCTL" run embedding/worker --input "$(jq -n \
  '{batch_size: 50, max_duration: 60}'
)"
