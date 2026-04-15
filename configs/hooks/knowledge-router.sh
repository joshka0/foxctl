#!/usr/bin/env bash
# knowledge-router.sh - Claude Code hook wrapper for hooks/knowledge_router skill
# This hook surfaces relevant knowledge packs based on prompt content and file paths.
# It is advisory only (never blocks) and injects context hints when matches exceed threshold.
#
# Environment:
#   FOXCTL_BIN           - Path to foxctl binary (default: foxctl)
#   FOXCTL_KNOWLEDGE_THRESHOLD - Minimum score threshold (default: 0.5)
#   CLAUDE_PROJECT_DIR     - Workspace root (set by Claude Code)

set -euo pipefail

# Resolve foxctl binary
FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"

# Read hook input from stdin
INPUT=$(cat)

# Run the knowledge_router skill and extract hook_output from envelope
# Use --ephemeral for faster execution (skip job persistence)
# Redirect stderr to /dev/null to suppress status messages
result="$(echo "$INPUT" | "$FOXCTL_BIN" run --daemon hooks/knowledge_router --ephemeral --input-file - 2>/dev/null)" || {
  # On error, return empty (fail-open)
  echo '{}'
  exit 0
}

# Extract hook_output from envelope data
hook_output="$(printf '%s' "$result" | jq -c '.data.hook_output // {}')"

# Check decision and format output for Claude Code
decision="$(printf '%s' "$hook_output" | jq -r '.decision // "none"')"

if [[ "$decision" == "approve" ]]; then
  # If there's context to inject, include it
  printf '%s\n' "$hook_output" | jq -c '
    if (.context // "") != "" then
      { decision: "approve", context: .context }
    else
      {}
    end
  '
else
  # For "none" or other decisions, return empty
  echo '{}'
fi
