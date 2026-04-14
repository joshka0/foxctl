#!/usr/bin/env bash
# subagent-bash-guard.sh - Thin wrapper for hooks/bash_guard skill
#
# This script is called by Claude Code as a PreToolUse hook on Bash commands
# when running subagents with profile restrictions.
#
# It forwards the hook payload to foxctl run hooks/bash_guard and returns
# the result to Claude Code.
#
# Usage:
#   In .claude/agents/explorer.md:
#   hooks:
#     PreToolUse:
#       - matcher: "Bash"
#         hooks:
#           - type: command
#             command: "$CLAUDE_PROJECT_DIR/configs/hooks/subagent-bash-guard.sh"

set -euo pipefail

# Read hook payload from stdin
payload="$(cat)"

# Forward to hooks/bash_guard skill
echo "$payload" | foxctl run hooks/bash_guard --ephemeral --input-file -
