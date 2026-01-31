#!/usr/bin/env bash
# smart-grep.sh - Claude Code PreToolUse hook for context-aware code search
# When Grep is called, also runs context_ripgrep to show full code blocks
# containing matches (functions, methods, classes) for better understanding.
#
# For conceptual queries (multi-word, natural language), chains:
#   semantic_search (find candidates) → snippet_extract (extract snippets)
#
# For literal patterns (identifiers, symbols), uses context_ripgrep directly.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_SMART_GREP_MAX_BLOCKS - Max blocks to show (default: 5)
#   AGENTCTL_SMART_GREP_SEMANTIC - Enable semantic chaining (default: 1)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
MAX_BLOCKS="${AGENTCTL_SMART_GREP_MAX_BLOCKS:-5}"
SEMANTIC_ENABLED="${AGENTCTL_SMART_GREP_SEMANTIC:-1}"

# Ensure MAX_BLOCKS is numeric to avoid jq --argjson failures under set -e
if ! [[ "$MAX_BLOCKS" =~ ^[0-9]+$ ]]; then
  MAX_BLOCKS=5
fi

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

# Detect if pattern is conceptual (natural language) vs literal (identifier)
# Conceptual: >6 chars, contains spaces or not a simple identifier
is_conceptual=false
if [[ "$SEMANTIC_ENABLED" == "1" ]]; then
  if [[ "$pattern" =~ [[:space:]] ]]; then
    # Contains spaces - definitely conceptual
    is_conceptual=true
  elif [[ ${#pattern} -gt 6 ]] && [[ ! "$pattern" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
    # Long and not a simple identifier - likely conceptual
    is_conceptual=true
  fi
fi

result=""
search_method="ripgrep"

if [[ "$is_conceptual" == "true" ]]; then
  # Chain: semantic_search (candidates) → snippet_extract (snippets)
  search_method="semantic+snippet_extract"

  # Get candidates from semantic search
  candidates=$("$AGENTCTL_BIN" run --daemon code/semantic_search --ephemeral --input "$(jq -nc --arg q "$pattern" '{
    query: $q,
    scope: ["symbols"],
    limit: 10
  }')" 2>/dev/null | jq -c '[.data.results[]? | {path: .path, symbol_id: .symbol_id}] | unique_by(.path)[:8]') || candidates="[]"

  # If we got candidates, use snippet_extract
  if [[ "$candidates" != "[]" && "$candidates" != "null" && -n "$candidates" ]]; then
    result=$("$AGENTCTL_BIN" run --daemon code/snippet_extract --ephemeral --input "$(jq -nc --arg q "$pattern" --argjson cands "$candidates" '{
      question: $q,
      candidates: $cands,
      max_files: 8,
      max_snippets: 10
    }')" 2>/dev/null) || result=""
  fi
fi

# Fallback to context_ripgrep if semantic chain failed or not conceptual
if [[ -z "$result" ]]; then
  search_method="ripgrep"
  input_json=$(jq -nc --arg pattern "$pattern" --arg path "$search_path" --argjson max_blocks "$MAX_BLOCKS" '{
    pattern: $pattern,
    path: $path,
    max_blocks: $max_blocks
  }')
  result=$("$AGENTCTL_BIN" run --daemon code/context_ripgrep --ephemeral --input "$input_json" 2>/dev/null) || {
    echo '{}'
    exit 0
  }
fi

# Handle different output formats based on search method
if [[ "$search_method" == "semantic+snippet_extract" ]]; then
  # snippet_extract output format
  snippet_count=$(echo "$result" | jq -r '.data.stats.snippets_extracted // .data.snippet_count // 0')
  file_count=$(echo "$result" | jq -r '.data.stats.files_processed // .data.file_count // 0')

  # Validate snippet_count is numeric to avoid bash comparison errors
  if ! [[ "$snippet_count" =~ ^[0-9]+$ ]]; then
    snippet_count=0
  fi

  if [[ "$snippet_count" -eq 0 ]]; then
    # Fall through to ripgrep
    search_method="ripgrep"
    input_json=$(jq -nc --arg pattern "$pattern" --arg path "$search_path" --argjson max_blocks "$MAX_BLOCKS" '{
      pattern: $pattern,
      path: $path,
      max_blocks: $max_blocks
    }')
    result=$("$AGENTCTL_BIN" run --daemon code/context_ripgrep --ephemeral --input "$input_json" 2>/dev/null) || {
      echo '{}'
      exit 0
    }
  fi
fi

if [[ "$search_method" == "semantic+snippet_extract" ]]; then
  # Format snippet_extract results
  snippet_count=$(echo "$result" | jq -r '.data.stats.snippets_extracted // 0')
  file_count=$(echo "$result" | jq -r '.data.stats.files_processed // 0')

  context="## Semantic Code Search: \`$pattern\`

Found **$snippet_count** snippets in **$file_count** files (via semantic search):

"

  # Add snippet previews
  context+=$(echo "$result" | jq -r --argjson max "$MAX_BLOCKS" '
    (.data.preview // .data.snippets_inline // [])[:$max] |
    map("### " + .file + ":" + (.start_line | tostring) + "-" + (.end_line | tostring) + "
```" + (.language // "") + "
" + (.preview // .text // "[snippet]") + "
```
") |
    join("\n")
  ')

  context+="
---
*Semantic search found conceptually related code. Use line numbers to read more.*"

else
  # context_ripgrep output format
  block_count=$(echo "$result" | jq -r '.data.block_count // 0')
  match_count=$(echo "$result" | jq -r '.data.match_count // 0')

  # Validate block_count is numeric to avoid bash comparison errors
  if ! [[ "$block_count" =~ ^[0-9]+$ ]]; then
    block_count=0
  fi

  if [[ "$block_count" -eq 0 ]]; then
    echo '{}'
    exit 0
  fi

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
fi

# Return approve with context
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
