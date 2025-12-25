#!/usr/bin/env bash
# live-index.sh - Index edited files for symbol search
#
# PostToolUse hook for Edit|Write|MultiEdit|NotebookEdit
# Extracts symbols from edited files into the memory store
# for faster code search and universal SWE grep.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: bin/agentctl)
#   AGENTCTL_LIVE_INDEX_DISABLED - Set to "1" to disable
#   AGENTCTL_LIVE_INDEX_DEBUG - Set to "1" for debug output
#   AGENTCTL_EMBED_QUEUE - Set to "1" to queue symbols for embedding

set -euo pipefail

# Check if disabled
if [[ "${AGENTCTL_LIVE_INDEX_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

# Debug mode
DEBUG="${AGENTCTL_LIVE_INDEX_DEBUG:-}"

# Find agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  # Try common locations
  if [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
    AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
  elif command -v agentctl &>/dev/null; then
    AGENTCTL_BIN="agentctl"
  else
    # Can't find agentctl, skip silently
    echo '{}'
    exit 0
  fi
fi

# Read hook input
INPUT=$(cat)

# Extract tool name and file path
tool_name=$(echo "$INPUT" | jq -r '.tool_name // ""')
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Debug logging
if [[ "$DEBUG" == "1" ]]; then
  echo "[live-index] tool=$tool_name file=$file_path" >&2
fi

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Only index supported file types
case "$file_path" in
  *.go|*.py|*.ts|*.tsx|*.js|*.jsx|*.gd)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip vendor/node_modules/generated directories
case "$file_path" in
  */vendor/*|*/node_modules/*|*/.git/*|*/dist/*|*/build/*|*/__pycache__/*)
    echo '{}'
    exit 0
    ;;
esac

# Skip test fixture files
case "$file_path" in
  */testdata/*|*/fixtures/*|*_test.go)
    echo '{}'
    exit 0
    ;;
esac

# Determine if embedding queue is enabled (default: ON)
embed_queue="true"
if [[ "${AGENTCTL_EMBED_QUEUE:-1}" == "0" ]]; then
  embed_queue="false"
fi

# Run incremental index (symbols + optional embedding queue)
input_json=$(jq -nc --arg file "$file_path" --argjson embed_queue "$embed_queue" '{
  file: $file,
  symbols: true,
  embed: false,
  embed_queue: $embed_queue
}')
result=$("$AGENTCTL_BIN" run code/incremental_index \
  --input "$input_json" \
  2>/dev/null) || {
  # Don't block on indexing failures
  if [[ "$DEBUG" == "1" ]]; then
    echo "[live-index] indexing failed for $file_path" >&2
  fi
  echo '{}'
  exit 0
}

# Debug: show result
if [[ "$DEBUG" == "1" ]]; then
  echo "[live-index] result: $result" >&2
fi

# Check if successful
status=$(echo "$result" | jq -r '.status // "error"')
if [[ "$status" != "ok" ]]; then
  echo '{}'
  exit 0
fi

# Extract stats for context message
symbols_updated=$(echo "$result" | jq -r '.data.symbols_updated // 0')
symbols_deleted=$(echo "$result" | jq -r '.data.symbols_deleted // 0')
embedding_queued=$(echo "$result" | jq -r '.data.embedding_queued // 0')
duration_ms=$(echo "$result" | jq -r '.data.duration_ms // 0')
skipped=$(echo "$result" | jq -r '.data.skipped // false')

# Skip if file was skipped (unsupported type, too large, etc.)
if [[ "$skipped" == "true" ]]; then
  echo '{}'
  exit 0
fi

# Only show context if we indexed something meaningful
if [[ "$symbols_updated" -gt 0 || "$symbols_deleted" -gt 0 ]]; then
  filename=$(basename "$file_path")

  # Build context message
  if [[ "$symbols_deleted" -gt 0 ]]; then
    ctx="Indexed **${symbols_updated}** symbols (+${symbols_deleted} removed) from \`$filename\` (${duration_ms}ms)"
  else
    ctx="Indexed **${symbols_updated}** symbols from \`$filename\` (${duration_ms}ms)"
  fi

  # Add embedding queue info if any were queued
  if [[ "$embedding_queued" -gt 0 ]]; then
    ctx="$ctx | Queued **${embedding_queued}** for embedding"
  fi

  jq -n --arg ctx "$ctx" '{
    decision: "approve",
    context: $ctx
  }'
else
  echo '{}'
fi
