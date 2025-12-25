#!/usr/bin/env bash
# Stop hook: Sync Claude Code plans before session ends
# This hook runs on the Stop event to detect plan changes and optionally import as tasks

set -euo pipefail

# agentctl binary location
AGENTCTL="${AGENTCTL_BIN:-$(dirname "$0")/../../bin/agentctl}"
if [[ ! -x "$AGENTCTL" ]]; then
  AGENTCTL="agentctl"
fi

# Read hook input from stdin (JSON with event, session_id, etc.)
INPUT=$(cat)

# Extract workspace from hook input (session_cwd or current directory)
WORKSPACE=$(echo "$INPUT" | jq -r '.session_cwd // empty' 2>/dev/null)
if [[ -z "$WORKSPACE" ]]; then
  WORKSPACE="$(pwd)"
fi

# Run plan/sync skill
# - Detects plans in ~/.claude/plans/
# - Tracks content hashes to detect changes
# - Does NOT import as tasks by default (set import_tasks: true to enable)
exec "$AGENTCTL" run plan/sync --input "$(jq -n \
  --arg workspace "$WORKSPACE" \
  '{workspace: $workspace, import_tasks: false}'
)"
