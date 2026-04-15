#!/usr/bin/env bash
# smart-find.sh - Claude Code PreToolUse hook for enhanced file discovery
# When Glob is called, also runs fs/find to show rich metadata like
# modification times, sizes, and provides smarter relevance ranking.
#
# Environment:
#   FOXCTL_BIN - Path to foxctl binary (default: foxctl)
#   FOXCTL_SMART_FIND_MAX_RESULTS - Max results to show (default: 10)

set -euo pipefail

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
MAX_RESULTS="${FOXCTL_SMART_FIND_MAX_RESULTS:-10}"

# Read hook input from stdin
INPUT=$(cat)

# Extract pattern and path from tool_input
pattern=$(echo "$INPUT" | jq -r '.tool_input.pattern // ""')
search_path=$(echo "$INPUT" | jq -r '.tool_input.path // "."')

# Skip if no pattern
if [[ -z "$pattern" || "$pattern" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Skip very simple patterns that don't need enhancement
# Note: specific patterns (package.json) must come before generic globs (*.json)
case "$pattern" in
  package.json|go.mod|Makefile)
    echo '{}'
    exit 0
    ;;
  *.md|*.txt|*.json|*.yaml|*.yml)
    echo '{}'
    exit 0
    ;;
esac

# Strip **/ prefix - fs/find searches recursively by default
pattern="${pattern#\*\*/}"

# Run fs/find with the glob pattern
result=$("$FOXCTL_BIN" run --daemon fs/find --input "{\"pattern\":\"$pattern\",\"path\":\"$search_path\",\"max_results\":$MAX_RESULTS,\"sort_by\":\"modified\"}" 2>/dev/null) || {
  echo '{}'
  exit 0
}

# Extract results
result_count=$(echo "$result" | jq -r '.data.result_count // 0')

if [[ "$result_count" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Build context with file metadata
context="## File Discovery: \`$pattern\`

Found **$result_count** files (sorted by recently modified):

"

# Add file details with metadata
context+=$(echo "$result" | jq -r --argjson max "$MAX_RESULTS" '
  .data.preview[:$max] |
  map(
    "| `" + .path + "` | " +
    (if .size < 1024 then (.size | tostring) + " B"
     elif .size < 1048576 then ((.size / 1024 | floor | tostring) + " KB")
     else ((.size / 1048576 * 10 | floor / 10 | tostring) + " MB") end) +
    " | " + (.modified | split("T")[0]) + " |"
  ) |
  ["| File | Size | Modified |", "|------|------|----------|"] + . |
  join("\n")
')

# Check for recently modified files (within 24h)
recent_count=$(echo "$result" | jq '[.data.preview[] | select(.modified_unix > (now - 86400))] | length')
if [[ "$recent_count" -gt 0 ]]; then
  context+="

*$recent_count file(s) modified in the last 24 hours.*"
fi

context+="

---
*Use specific paths above for targeted reads.*"

# Return approve with context
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
