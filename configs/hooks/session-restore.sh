#!/usr/bin/env bash
# session-restore.sh - Claude Code SessionStart hook wrapper for session/restore skill
#
# This script restores session state after compaction or when resuming a session,
# enabling session continuity.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "SessionStart": [
#         {
#           "matcher": "compact|resume",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-restore.sh"
#             }
#           ]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN - Path to agentctl binary (default: searches PATH, then project bin/)

set -euo pipefail

# Find agentctl binary
if [[ -n "${AGENTCTL_BIN:-}" ]]; then
  : # Use provided path
elif command -v agentctl &>/dev/null; then
  AGENTCTL_BIN="agentctl"
elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
  AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
else
  # Can't find agentctl - fail silently
  echo '{}'
  exit 0
fi

# Read hook input from stdin
payload="$(cat)"

# Extract trigger source from the hook input
# SessionStart provides: { source: "startup"|"resume"|"clear"|"compact" }
trigger=$(printf '%s' "$payload" | jq -r '.source // "compact"')

# Workspace from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Build skill input
skill_input=$(jq -nc \
  --arg trigger "$trigger" \
  --arg workspace "$workspace" \
  '{
    trigger: $trigger,
    workspace: $workspace
  }'
)

# Call session/restore skill with --ephemeral for faster execution
result="$(printf '%s' "$skill_input" | "$AGENTCTL_BIN" run session/restore --ephemeral --input-file - 2>/dev/null)" || {
  # On error, proceed without restored context
  echo '{}'
  exit 0
}

# Extract hook_output from envelope
hook_output="$(printf '%s' "$result" | jq -c '.data.hook_output // {}')"

# Check if we have context to inject
context=$(printf '%s' "$hook_output" | jq -r '.context // ""')

anchor_input=$(jq -nc --arg ws "$workspace" '{operation: "get", workspace: $ws}')
if anchor_result=$(printf '%s' "$anchor_input" | "$AGENTCTL_BIN" run session/anchor --ephemeral --workspace "$workspace" --input-file - 2>/dev/null); then
  anchor_main=$(printf '%s' "$anchor_result" | jq -r '.data.anchor.main_prompt // ""' 2>/dev/null || echo "")
  anchor_q=$(printf '%s' "$anchor_result" | jq -r '.data.anchor.pending_question // ""' 2>/dev/null || echo "")
else
  anchor_main=""
  anchor_q=""
fi

if [[ -n "$anchor_main" && "$anchor_main" != "null" ]]; then
  anchor_block=$'## Session Anchor\n\n**Goal:** '
  anchor_block+="${anchor_main}"
  if [[ -n "$anchor_q" && "$anchor_q" != "null" ]]; then
    anchor_block+=$'\n\n**Pending:** '
    anchor_block+="${anchor_q}"
  fi
  context="${anchor_block}"$'\n\n'"${context}"
fi

if [[ -n "$context" && "$context" != "null" ]]; then
  # Return the context for injection
  jq -nc --arg context "$context" '{context: $context}'
else
  # No context to inject
  echo '{}'
fi
