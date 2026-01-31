#!/usr/bin/env bash
# overseer-inbox-post.sh - Claude Code PostToolUse hook for human-in-the-loop messaging
#
# This hook checks the mailbox for messages addressed to "overseer" (or configured
# recipient) and surfaces them into Claude's context on every tool call.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "PostToolUse": [
#         {
#           "matcher": ".*",
#           "hooks": ["$CLAUDE_PROJECT_DIR/configs/hooks/overseer-inbox-post.sh"]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_BIN                 - Path to agentctl binary (default: agentctl)
#   AGENTCTL_OVERSEER_RECIPIENT  - Recipient to monitor (default: "overseer", use "*" for broadcast)
#   AGENTCTL_OVERSEER_AUTOACK    - Set to "0" to disable auto-ack (default: "1")
#
# Sending messages to the overseer (from terminal or script):
#   agentctl run mailbox/manage --input '{
#     "operation": "send",
#     "workspace_id": "/path/to/project",
#     "send": {
#       "sender": "human",
#       "recipient": "overseer",
#       "subject": "Priority change",
#       "body": "Please focus on the auth bug first",
#       "priority": 1
#     }
#   }'

set -euo pipefail

# Ensure child processes are killed when this script is terminated
trap 'kill $(jobs -p) 2>/dev/null || true' SIGTERM SIGINT EXIT

# Configuration
AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"

# Read hook input from stdin
payload="$(cat)"

# Extract workspace root
workspace_root="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Transform Claude hook input to agentctl hook.Input format
hook_input=$(printf '%s' "$payload" | jq -c --arg ws "$workspace_root" '{
  event: "PostToolUse",
  workspace_root: $ws,
  session_id: (.session_id // ""),
  tool_name: (.tool_name // ""),
  tool_input: (.tool_input // {})
}')

# Call agentctl skill with --ephemeral for faster execution
# Pass --workspace so AGENTCTL_WORKSPACE env is set for the skill
result="$(printf '%s' "$hook_input" | "$AGENTCTL_BIN" run --daemon hooks/overseer_inbox --ephemeral --workspace "$workspace_root" --input-file - 2>/dev/null)" || {
  # On error, emit empty (no-op) - never block on mailbox failures
  echo '{}'
  exit 0
}

# Extract hook_output from envelope data
hook_output="$(printf '%s' "$result" | jq -c '.data.hook_output // {}')"

# Check if there's context to inject
context="$(printf '%s' "$hook_output" | jq -r '.context // ""')"

if [[ -n "$context" && "$context" != "null" ]]; then
  opencode_session_id="${OPENCODE_SESSION_ID:-}"
  if [[ -z "$opencode_session_id" || "$opencode_session_id" == "null" ]]; then
    opencode_session_id="$(printf '%s' "$payload" | jq -r '.sessionID // .session_id // ""' 2>/dev/null || true)"
  fi

  if [[ -n "$opencode_session_id" && "$opencode_session_id" != "null" ]]; then
    # Sanitize session ID to prevent path traversal attacks
    opencode_session_id="${opencode_session_id//[^a-zA-Z0-9_-]/}"
    if [[ -z "$opencode_session_id" ]]; then
      echo '{}'
      exit 0
    fi

    # Use Context Buffer API to enqueue context for injection
    enqueue_input=$(jq -nc \
      --arg sid "$opencode_session_id" \
      --arg ws "$workspace_root" \
      --arg src "Overseer Messages" \
      --arg ctx "$context" \
      '{
        session_id: $sid,
        workspace: $ws,
        source: $src,
        context: $ctx,
        priority: 1
      }')

    # Call context enqueue skill (best-effort, don't block on failure)
    printf '%s' "$enqueue_input" | "$AGENTCTL_BIN" run --daemon hooks/context_enqueue --ephemeral --input-file - >/dev/null 2>&1 || true

    echo '{}'
    exit 0
  fi

  jq -n --arg ctx "$context" '{
    hookSpecificOutput: {
      hookEventName: "PostToolUse",
      additionalContext: $ctx
    }
  }'
else
  echo '{}'
fi
