#!/usr/bin/env bash
# context-updater-drain.sh - Drain context buffer on UserPromptSubmit
#
# This hook drains the context buffer that the background context updater
# populates with relevant context (memories, past session learnings, etc.).
#
# Usage in ~/.claude/settings.json:
#   {
#     "hooks": {
#       "UserPromptSubmit": [
#         {
#           "matcher": "",
#           "hooks": ["$HOME/.claude/hooks/agentctl/context-updater-drain.sh"]
#         }
#       ]
#     }
#   }
#
# The context updater daemon (agentctl daemon) continuously monitors sessions
# and enqueues relevant context to the contextbuffer store. This hook drains
# that buffer and injects it into Claude's context.
#
# Flow:
#   1. Context updater daemon analyzes conversation with cheap LLM
#   2. Finds relevant memories/learnings and enqueues to contextbuffer
#   3. This hook fires on UserPromptSubmit
#   4. Drains contextbuffer and returns context for injection

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"

# Read hook payload from stdin
payload="$(cat)"

# Extract workspace and session
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
session_id="${AGENTCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-}}"

# If no session ID, try to extract from payload or skip
if [[ -z "$session_id" ]]; then
  session_id="$(printf '%s' "$payload" | jq -r '.session_id // ""' 2>/dev/null || true)"
fi

# Skip if we don't have required info
if [[ -z "$workspace" || -z "$session_id" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Drain context buffer with source filter for context-updater
result=$("$AGENTCTL_BIN" run hooks/context_drain --input "$(jq -nc \
  --arg ws "$workspace" \
  --arg sid "$session_id" \
  '{
    workspace_id: $ws,
    session_id: $sid,
    sources: ["context-updater"],
    format: "markdown",
    limit: 10
  }')" 2>/dev/null) || {
  # On error, just approve without context
  echo '{"decision":"approve"}'
  exit 0
}

# Extract markdown content
markdown=$(printf '%s' "$result" | jq -r '.data.markdown // ""' 2>/dev/null || echo "")
count=$(printf '%s' "$result" | jq -r '.data.count // 0' 2>/dev/null || echo "0")

# Only inject if we have content
if [[ -n "$markdown" && "$count" -gt 0 ]]; then
  # Format with header
  context="<context-updater>
$markdown
</context-updater>"

  jq -nc --arg ctx "$context" '{
    decision: "approve",
    context: $ctx
  }'
else
  echo '{"decision":"approve"}'
fi
