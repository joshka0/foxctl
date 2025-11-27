#!/usr/bin/env bash
# knowledge-router.sh - Claude Code hook wrapper for hooks/knowledge_router skill
# This hook surfaces relevant knowledge packs based on prompt content and file paths.
# It is advisory only (never blocks) and injects context hints when matches exceed threshold.
#
# Environment:
#   AGENTCTL_BIN           - Path to agentctl binary (default: agentctl)
#   AGENTCTL_KNOWLEDGE_THRESHOLD - Minimum score threshold (default: 0.5)
#   CLAUDE_PROJECT_DIR     - Workspace root (set by Claude Code)

set -euo pipefail

# Resolve agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"

# Read hook input from stdin
INPUT=$(cat)

# Run the knowledge_router skill
# The skill reads JSON from stdin and emits a JSON envelope to stdout
echo "$INPUT" | "$AGENTCTL_BIN" run hooks/knowledge_router --input -
