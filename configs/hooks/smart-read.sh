#!/usr/bin/env bash
# smart-read.sh - Claude Code PreToolUse:Read hook for structure-first reading
# Injects file structure (symbols, types, functions) before Claude reads a file.
# This helps Claude make smarter decisions about what sections to read.
#
# The idea: Instead of reading lines 1-50 blindly, Claude sees the file's "map"
# first and can target specific functions/types by line number.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_SMART_READ_INCLUDE_PRIVATE - Include private symbols (default: true)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
INCLUDE_PRIVATE="${AGENTCTL_SMART_READ_INCLUDE_PRIVATE:-true}"

# Read hook input from stdin
INPUT=$(cat)

# Extract file path from tool_input
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Only analyze code files (skip docs, configs, etc.)
case "$file_path" in
  *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.java|*.c|*.cpp|*.h|*.hpp|*.rs|*.rb|*.php)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip if file doesn't exist
if [[ ! -f "$file_path" ]]; then
  echo '{}'
  exit 0
fi

# Get file size - skip very large files (>100KB)
file_size=$(stat -f%z "$file_path" 2>/dev/null || stat -c%s "$file_path" 2>/dev/null || echo "0")
if [[ "$file_size" -gt 102400 ]]; then
  echo '{}'
  exit 0
fi

# Run symbols extraction
input_json=$(jq -nc --arg path "$file_path" --argjson include_private "$INCLUDE_PRIVATE" '{
  path: $path,
  include_private: $include_private,
  max_results: 50
}')
result=$("$AGENTCTL_BIN" run code/symbols --input "$input_json" 2>/dev/null) || {
  # On error, fail open
  echo '{}'
  exit 0
}

# Extract symbols
symbols=$(echo "$result" | jq -r '.data.preview // []')
symbol_count=$(echo "$symbols" | jq 'length')

if [[ "$symbol_count" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Get filename for display
filename=$(basename "$file_path")

# Build context message with file structure
context="## File Structure: $filename

"

# Group by type
structs=$(echo "$symbols" | jq '[.[] | select(.type == "struct" or .type == "class" or .type == "interface")]')
functions=$(echo "$symbols" | jq '[.[] | select(.type == "function" or .type == "method")]')
vars=$(echo "$symbols" | jq '[.[] | select(.type == "var" or .type == "const")]')

# Add types/structs section
struct_count=$(echo "$structs" | jq 'length')
if [[ "$struct_count" -gt 0 ]]; then
  context+="### Types & Structs
"
  context+=$(echo "$structs" | jq -r '
    .[:10] |
    map("- `" + .name + "` (" + .type + ", line " + (.line | tostring) + ")" + if .fields then ": " + (.fields[:5] | join(", ")) + (if (.fields | length) > 5 then "..." else "" end) else "" end) |
    join("\n")
  ')
  context+="

"
fi

# Add functions section
func_count=$(echo "$functions" | jq 'length')
if [[ "$func_count" -gt 0 ]]; then
  context+="### Functions
"
  context+=$(echo "$functions" | jq -r '
    .[:15] |
    map("- `" + (.signature // .name) + "` (line " + (.line | tostring) + ")" + if .receiver then " [" + .receiver + "]" else "" end) |
    join("\n")
  ')
  context+="

"
fi

# Add vars/consts if any
var_count=$(echo "$vars" | jq 'length')
if [[ "$var_count" -gt 0 ]]; then
  context+="### Variables & Constants
"
  context+=$(echo "$vars" | jq -r '
    .[:5] |
    map("- `" + .name + "` (line " + (.line | tostring) + ")") |
    join("\n")
  ')
  context+="

"
fi

# Add summary
context+="---
*$symbol_count symbols total. Use line numbers to target specific reads.*"

# Return approve with context
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
