#!/usr/bin/env bash
# semantic-search.sh - Claude Code PreToolUse hook for proactive semantic search
# When Grep or Glob is called, also runs code/semantic_search to provide
# semantic context from embeddings across symbols, sessions, memories, and tasks.
#
# This complements smart-grep.sh (context_ripgrep) and smart-find.sh (fs/find)
# by adding vector search results when the query looks like a conceptual search.
#
# Environment:
#   FOXCTL_BIN - Path to foxctl binary (default: foxctl)
#   FOXCTL_SEMANTIC_DISABLED - Set to 1 to disable
#   FOXCTL_SEMANTIC_MAX_RESULTS - Max results per scope (default: 3)
#   FOXCTL_SEMANTIC_RERANK - Set to 1 to enable local Qwen/OpenAI-compatible rerank

set -euo pipefail

# Allow disabling
if [[ "${FOXCTL_SEMANTIC_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
MAX_RESULTS="${FOXCTL_SEMANTIC_MAX_RESULTS:-3}"

# Read hook input from stdin
INPUT=$(cat)

# Extract tool name and pattern
tool_name=$(echo "$INPUT" | jq -r '.tool_name // ""')
pattern=""

case "$tool_name" in
  Grep)
    pattern=$(echo "$INPUT" | jq -r '.tool_input.pattern // ""')
    ;;
  Glob)
    pattern=$(echo "$INPUT" | jq -r '.tool_input.pattern // ""')
    # Strip glob syntax for semantic query
    pattern="${pattern//\*\*/}"
    pattern="${pattern//\*/}"
    pattern="${pattern//\//}"
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip if no pattern or too short
if [[ -z "$pattern" || "$pattern" == "null" || ${#pattern} -lt 4 ]]; then
  echo '{}'
  exit 0
fi

# Skip patterns that are clearly not conceptual queries
# (exact file extensions, simple globs, etc.)
# Note: specific patterns (package.json) must come before generic globs (*.json)
case "$pattern" in
  package.json|go.mod|Makefile|README*)
    echo '{}'
    exit 0
    ;;
  *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.md|*.json|*.yaml|*.yml)
    echo '{}'
    exit 0
    ;;
esac

# Check if reranking is enabled
RERANK_ENABLED="${FOXCTL_SEMANTIC_RERANK:-0}"
if [[ "$RERANK_ENABLED" == "1" ]]; then
  rerank_flag="true"
else
  rerank_flag="false"
fi

# Run semantic search with all scopes
input_json=$(jq -nc \
  --arg query "$pattern" \
  --argjson limit "$MAX_RESULTS" \
  --argjson rerank "$rerank_flag" \
  '{
    query: $query,
    scope: ["symbols", "sessions", "memories", "tasks"],
    limit: $limit,
    summarize: false,
    rerank_enabled: $rerank
  }')

result=$("$FOXCTL_BIN" run --daemon code/semantic_search --ephemeral --input "$input_json" 2>/dev/null) || {
  echo '{}'
  exit 0
}

# Check if we got results
total=$(echo "$result" | jq -r '.data.stats.total_results // 0')

if [[ "$total" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Build context with semantic results
context="## Semantic Search: \`$pattern\`

"

# Add results by source
sources=$(echo "$result" | jq -r '.data.stats.source_counts | keys[]' 2>/dev/null) || sources=""

for source in $sources; do
  count=$(echo "$result" | jq -r ".data.stats.source_counts.\"$source\" // 0")
  if [[ "$count" -gt 0 ]]; then
    context+="### ${source^} ($count)
"
    # Get top results for this source
    context+=$(echo "$result" | jq -r --arg src "$source" --argjson max "$MAX_RESULTS" '
      .data.results | map(select(.source == $src)) | .[:$max] |
      map("- `" + .path + "` (" + (.similarity * 100 | floor | tostring) + "% match)") |
      join("\n")
    ')
    context+="
"
  fi
done

# Add quick insight if summary available
summary=$(echo "$result" | jq -r '.data.summary.answer // ""')
if [[ -n "$summary" && "$summary" != "null" ]]; then
  context+="
**Insight:** $summary"
fi

# Add rerank indicator if enabled
if [[ "$RERANK_ENABLED" == "1" ]]; then
  rerank_note=" (reranked)"
else
  rerank_note=""
fi

context+="
---
*Vector search across code, sessions, memories & tasks${rerank_note}.*"

# Return approve with context
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
