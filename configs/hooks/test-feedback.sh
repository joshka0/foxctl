#!/usr/bin/env bash
# test-feedback.sh - Claude Code hook wrapper for hooks/test_feedback skill
# This hook surfaces failing test results to Claude after code edits.
# It is advisory only (never blocks) and provides test failure context via PostToolUse.
#
# Environment:
#   AGENTCTL_BIN                          - Path to agentctl binary (default: agentctl)
#   AGENTCTL_TEST_FEEDBACK_MAX_FAILURES   - Max failures to show per watcher (default: 3)
#   CLAUDE_PROJECT_DIR                    - Workspace root (set by Claude Code)

set -euo pipefail

# Resolve agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"

# Read hook input from stdin
INPUT=$(cat)

# Run the test_feedback skill and extract hook_output from envelope
# Redirect stderr to /dev/null to suppress "job ... state ok" line
result="$(echo "$INPUT" | "$AGENTCTL_BIN" run hooks/test_feedback --input-file - 2>/dev/null)" || {
  # On error, return empty (fail-open)
  echo '{}'
  exit 0
}

# Extract hook_output from envelope data
hook_output="$(printf '%s' "$result" | jq -c '.data.hook_output // {}')"

# Check decision and format output for Claude Code
decision="$(printf '%s' "$hook_output" | jq -r '.decision // "none"')"

if [[ "$decision" == "approve" ]]; then
  # If there's context to inject (test failures), include it
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
