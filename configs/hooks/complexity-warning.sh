#!/usr/bin/env bash
# complexity-warning.sh - Claude Code PostToolUse hook for complexity analysis
# Analyzes edited file for complexity hotspots and suggests refactoring.
# Non-blocking (advisory only) - always approves but may inject context.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_COMPLEXITY_THRESHOLD - Complexity threshold (default: 15)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
THRESHOLD="${AGENTCTL_COMPLEXITY_THRESHOLD:-15}"

# Read hook input from stdin
INPUT=$(cat)

# Extract file path from tool_input
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Only analyze code files
case "$file_path" in
  *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.java|*.c|*.cpp|*.rs)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Run complexity analysis on the specific file
input_json=$(jq -nc --arg path "$file_path" --argjson threshold "$THRESHOLD" '{
  path: $path,
  threshold: $threshold,
  analysis_mode: "hotspots",
  max_results: 5
}')
result=$("$AGENTCTL_BIN" run code/complexity --input "$input_json" 2>/dev/null) || {
  # On error, fail open
  echo '{}'
  exit 0
}

# Extract results (functions above threshold)
results=$(echo "$result" | jq -r '.data.results // []')

# Filter to only high/medium risk
high_risk=$(echo "$results" | jq '[.[] | select(.risk_level == "high" or .risk_level == "medium")]')
risk_count=$(echo "$high_risk" | jq 'length')

if [[ "$risk_count" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Build context message with complexity warnings
context="## Complexity Warning

This file has **$risk_count** function(s) with elevated complexity:

"

# Add each high-risk function
context+=$(echo "$high_risk" | jq -r '
  .[:3] |
  to_entries |
  map("- **\(.value.function)** (line \(.value.line)): cyclomatic=\(.value.cyclomatic_complexity), cognitive=\(.value.cognitive_complexity)\n  - \(.value.recommendations // ["Consider refactoring"] | .[0])") |
  join("\n")
')

context+="

*High complexity makes code harder to test and maintain. Consider refactoring.*"

# Return approve with context (non-blocking warning)
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
