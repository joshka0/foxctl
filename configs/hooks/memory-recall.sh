#!/usr/bin/env bash
# file-memory-recall.sh - Surface memories for files before editing
#
# PreToolUse hook that searches for memories related to files being edited.
# Surfaces gotchas, notes, and context before changes are made.
#
# Environment:
#   AGENTCTL_FILE_RECALL_DISABLED=1 - Disable this hook

set -euo pipefail

if [[ "${AGENTCTL_FILE_RECALL_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

INPUT=$(cat)
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Extract filename for search (without extension for broader matches)
filename=$(basename "$file_path")
filename_no_ext="${filename%.*}"

# Search with filename without extension for better recall
search_terms="$filename_no_ext"

# Find agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  if command -v agentctl &>/dev/null; then
    AGENTCTL_BIN="agentctl"
  elif [[ -x "${HOME}/.local/bin/agentctl" ]]; then
    AGENTCTL_BIN="${HOME}/.local/bin/agentctl"
  else
    echo '{}'
    exit 0
  fi
fi

# Search for memories related to this file
# Use timeout to prevent blocking if agentctl is slow
memories=""
memories=$(timeout 2s $AGENTCTL_BIN memory search --query "$search_terms" --limit 5 2>/dev/null) || true

# Parse results - check if we got any hits
if [[ -z "$memories" ]]; then
  echo '{}'
  exit 0
fi

# Extract entries from JSON envelope
entries=$(echo "$memories" | jq -r '.data.entries // [] | length' 2>/dev/null) || entries=0

if [[ "$entries" == "0" || "$entries" == "" ]]; then
  echo '{}'
  exit 0
fi

# Format memories concisely - strip filename prefix from summaries
formatted=$(echo "$memories" | jq -r --arg fn "$filename_no_ext" '
  .data.entries[:3] |
  map(
    "[" + (.type // "note")[0:4] + "] " +
    ((.summary // .name) | gsub("^" + $fn + "(\\.\\w+)?:\\s*"; ""))
  ) |
  join(" | ")
' 2>/dev/null) || formatted=""

if [[ -z "$formatted" ]]; then
  echo '{}'
  exit 0
fi

# Build concise context
context="\`$filename\`: $formatted"

jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
