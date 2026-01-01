#!/usr/bin/env bash
# smart-grep.sh - Claude Code PreToolUse hook for context-aware code search
# When Grep is called, also runs context_ripgrep to show full code blocks
# containing matches (functions, methods, classes) for better understanding.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_SMART_GREP_MAX_BLOCKS - Max blocks to show (default: 5)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
MAX_BLOCKS="${AGENTCTL_SMART_GREP_MAX_BLOCKS:-5}"

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

# Skip very short patterns (likely too noisy)
if [[ ${#pattern} -lt 3 ]]; then
  echo '{}'
  exit 0
fi

# Only enhance for code-like patterns (skip simple strings)
# Skip patterns that are clearly just file path searches
case "$pattern" in
  */*|*.md|*.txt|*.json|*.yaml|*.yml)
    echo '{}'
    exit 0
    ;;
esac

# Skip documentation-only searches
case "$search_path" in
  docs/*|*.md|README*)
    echo '{}'
    exit 0
    ;;
esac

# Run context_ripgrep to get code blocks
input_json=$(jq -nc --arg pattern "$pattern" --arg path "$search_path" --argjson max_results "$MAX_BLOCKS" '{
  pattern: $pattern,
  path: $path,
  max_results: $max_results
}')
result=$("$AGENTCTL_BIN" run code/context_ripgrep --ephemeral --input "$input_json" 2>/dev/null) || {
  echo '{}'
  exit 0
}

# Extract results
block_count=$(echo "$result" | jq -r '.data.block_count // 0')
match_count=$(echo "$result" | jq -r '.data.match_count // 0')

if [[ "$block_count" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Build context with code blocks
context="## Code Context: \`$pattern\`

Found **$match_count** matches in **$block_count** code blocks:

"

# Add preview of blocks
context+=$(echo "$result" | jq -r --argjson max "$MAX_BLOCKS" '
  .data.preview[:$max] |
  to_entries |
  map("### " + (.value.symbol_name // "unknown") + " (" + (.value.symbol_kind // "block") + ")
**File:** `" + .value.file + "` (lines " + (.value.start_line | tostring) + "-" + (.value.end_line | tostring) + ")
```" + (.value.language // "") + "
" + (.value.header_line // "") + "
```
") |
  join("\n")
')

# Add top files summary
top_files=$(echo "$result" | jq -r '.data.top_files[:3] | map("- `" + .[0] + "` (" + (.[1] | tostring) + " matches)") | join("\n")')
if [[ -n "$top_files" ]]; then
  context+="
### Top Files
$top_files
"
fi

context+="
---
*Use line numbers above to read specific functions.*"

# Return approve with context
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
