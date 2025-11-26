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

# Run the test_feedback skill
# The skill reads JSON from stdin and emits a JSON envelope to stdout
echo "$INPUT" | "$AGENTCTL_BIN" run hooks/test_feedback --input -
