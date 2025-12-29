#!/usr/bin/env bash
# session-identity.sh - Detect and persist session ID from TUI providers
#
# This hook detects the current session ID from various AI coding assistants
# (Claude Code, Cursor, OpenCode) and writes it to the identity file for
# subsequent hooks/skills to use.
#
# Multi-agent safe: Identity files are keyed by (workspace_hash, agent_id)
# to prevent conflicts when multiple agents run in the same workspace.
#
# Can run on:
# - SessionStart:startup - detect on fresh start (recommended)
# - PreToolUse (any) - lazy fallback if identity file missing
#
# Fast-path: If identity file exists and is recent (<1 hour), exits immediately.
#
# Usage in .claude/settings.json:
#   {
#     "hooks": {
#       "SessionStart": [
#         {
#           "matcher": "startup",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/configs/hooks/session-identity.sh",
#               "timeout": 2
#             }
#           ]
#         }
#       ],
#       "PreToolUse": [
#         {
#           "matcher": "Edit|Write|Bash",
#           "hooks": [
#             {
#               "type": "command",
#               "command": "$CLAUDE_PROJECT_DIR/configs/hooks/session-identity.sh",
#               "timeout": 2
#             }
#           ]
#         }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_SESSION_ID - If already set, skip detection
#   AGENTCTL_AGENT_ID   - Agent identifier for multi-agent (default: provider name)
#   AGENTCTL_BIN        - Path to agentctl binary

set -euo pipefail

# Workspace and agent ID from environment
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
agent_id="${AGENTCTL_AGENT_ID:-}"
workspace_hash="$(printf '%s' "$workspace" | shasum -a 256 | cut -c1-16)"
identity_dir="$HOME/.agentctl/sessions/active"

# Identity file keyed by (workspace, agent_id) for multi-agent safety
# If no agent_id yet, check the base file first (will be updated with agent_id later)
if [[ -n "$agent_id" ]]; then
  identity_file="$identity_dir/${workspace_hash}-${agent_id}.json"
else
  # Try to find any existing identity file for this workspace
  identity_file="$identity_dir/${workspace_hash}-claude.json"
  if [[ ! -f "$identity_file" ]]; then
    identity_file="$identity_dir/${workspace_hash}-agentctl.json"
  fi
fi

# Fast-path: If identity file exists and is recent (<1 hour), skip detection
if [[ -f "$identity_file" ]]; then
  # Check file age (cross-platform stat: try Linux, then macOS)
  file_mtime=0
  if file_mtime=$(stat -c %Y "$identity_file" 2>/dev/null); then
    :
  elif file_mtime=$(stat -f %m "$identity_file" 2>/dev/null); then
    :
  else
    file_mtime=0
  fi

  now=$(date +%s)
  age=$((now - file_mtime))

  if [[ $age -lt 3600 ]]; then
    # Identity file is recent - fast exit
    echo '{}'
    exit 0
  fi
fi

# Find agentctl binary
if [[ -n "${AGENTCTL_BIN:-}" ]]; then
  : # Use provided path
elif command -v agentctl &>/dev/null; then
  AGENTCTL_BIN="agentctl"
elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
  AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
else
  # Can't find agentctl - skip silently
  echo '{}'
  exit 0
fi

# Skip if session ID already available via env
if [[ -n "${AGENTCTL_SESSION_ID:-}" ]]; then
  echo '{}'
  exit 0
fi

# Also check other provider env vars
if [[ -n "${CLAUDE_SESSION_ID:-}" ]] || \
   [[ -n "${CURSOR_SESSION_ID:-}" ]] || \
   [[ -n "${OPENCODE_SESSION_ID:-}" ]]; then
  echo '{}'
  exit 0
fi

# Build session-id command args
cmd_args=(session-id --workspace "$workspace")
if [[ -n "$agent_id" ]]; then
  cmd_args+=(--agent-id "$agent_id")
fi

# Detect session ID from TUI providers
result="$("$AGENTCTL_BIN" "${cmd_args[@]}" 2>/dev/null)" || {
  # Detection failed - proceed without
  echo '{}'
  exit 0
}

# Extract fields from envelope
session_id="$(printf '%s' "$result" | jq -r '.data.session_id // ""')"
provider="$(printf '%s' "$result" | jq -r '.data.provider // ""')"
detected_agent_id="$(printf '%s' "$result" | jq -r '.data.agent_id // ""')"

if [[ -z "$session_id" || "$session_id" == "null" ]]; then
  # No session detected
  echo '{}'
  exit 0
fi

# Use detected agent_id if we didn't have one
if [[ -z "$agent_id" ]]; then
  agent_id="$detected_agent_id"
fi

# Update identity file path with resolved agent_id
identity_file="$identity_dir/${workspace_hash}-${agent_id}.json"

mkdir -p "$identity_dir"

# Create/update identity file
jq -nc \
  --arg session_id "$session_id" \
  --arg agent_id "$agent_id" \
  --arg provider "$provider" \
  --arg workspace "$workspace" \
  --arg workspace_hash "$workspace_hash" \
  --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{
    session_id: $session_id,
    agent_id: $agent_id,
    provider: $provider,
    workspace: $workspace,
    workspace_hash: $workspace_hash,
    started_at: $started_at,
    last_activity: $started_at,
    detected_from: "tui"
  }' > "$identity_file"

# Return minimal context - hooks can read identity file if needed
jq -nc \
  --arg session_id "$session_id" \
  --arg agent_id "$agent_id" \
  --arg provider "$provider" \
  '{
    context: ("Session detected: " + $session_id + " (agent: " + $agent_id + ", from " + $provider + ")")
  }'
