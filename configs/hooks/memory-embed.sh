#!/usr/bin/env bash
# memory-embed.sh - Generate embeddings for memory updates
#
# PostToolUse hook that triggers embedding refresh when memories are updated.
# This hook chains with memory-capture.sh to embed captured memories.
#
# Triggers on:
# 1. MCP memory tools with set/append operations
# 2. Edit/Write when AGENTCTL_MEMORY_CAPTURE=1 (chains with memory-capture.sh)
#
# Auto-enabled when GEMINI_API_KEY is set. Disable with AGENTCTL_MEMORY_EMBED=0.
#
# Phase 4: Automatic Embedding Updates
# See: docs/designs/unified_semantic_search.md

set -euo pipefail

# Check for API key (required for embeddings) - auto-enable when set
if [[ -z "${GEMINI_API_KEY:-}" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Allow explicit disable via env var
if [[ "${AGENTCTL_MEMORY_EMBED:-1}" == "0" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Find agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  if command -v agentctl &>/dev/null; then
    AGENTCTL_BIN="agentctl"
  elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
    AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
  else
    echo '{"decision":"approve"}'
    exit 0
  fi
fi

# Read hook input
payload="$(cat)"

# Extract tool info
tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // ""')

# Handle MCP memory tools (if available)
if [[ "$tool_name" == *"memory"* ]] || [[ "$tool_name" == *"Memory"* ]]; then
  operation=$(printf '%s' "$payload" | jq -r '.tool_input.operation // ""')

  if [[ "$operation" == "set" || "$operation" == "append" ]]; then
    name=$(printf '%s' "$payload" | jq -r '.tool_input.name // ""')
    workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

    if [[ -n "$name" ]]; then
      # Fire and forget - don't block the tool
      "$AGENTCTL_BIN" run embedding/refresh --input "$(jq -nc \
        --arg scope "memory" \
        --arg name "$name" \
        --arg workspace "$workspace" \
        '{scope: $scope, name: $name, workspace: $workspace}'
      )" &>/dev/null &
    fi
  fi
fi

# Handle Edit/Write when memory capture is enabled
# This chains with memory-capture.sh to embed captured memories
if [[ "${AGENTCTL_MEMORY_CAPTURE:-0}" == "1" ]]; then
  case "$tool_name" in
    Edit|Write|MultiEdit|NotebookEdit)
      file_path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // .tool_input.path // ""')

      if [[ -n "$file_path" ]]; then
        workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
        rel_path="${file_path#$workspace/}"
        # Memory name matches what memory-capture.sh creates
        memory_name="edit:${rel_path}:$(date +%s)"

        # Small delay to ensure memory-capture.sh has stored the entry
        (
          sleep 1
          "$AGENTCTL_BIN" run embedding/refresh --input "$(jq -nc \
            --arg scope "memory" \
            --arg name "$memory_name" \
            --arg workspace "$workspace" \
            '{scope: $scope, name: $name, workspace: $workspace}'
          )" &>/dev/null
        ) &
      fi
      ;;
  esac
fi

echo '{"decision":"approve"}'
