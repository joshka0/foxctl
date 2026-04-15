#!/usr/bin/env bash
# counsel-suggest.sh - PostToolUse hook that suggests /counsel after reading multiple code files
#
# Tracks code file reads in a session-specific counter file.
# After 3+ code files read, suggests running /counsel for analysis.
#
# Environment:
#   FOXCTL_COUNSEL_SUGGEST_DISABLED - Set to 1 to disable
#   FOXCTL_COUNSEL_SUGGEST_THRESHOLD - Number of reads before suggesting (default: 3)

set -euo pipefail

if [[ "${FOXCTL_COUNSEL_SUGGEST_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

THRESHOLD="${FOXCTL_COUNSEL_SUGGEST_THRESHOLD:-3}"
COUNTER_DIR="${FOXCTL_HOME:-$HOME/.foxctl}/cache/counsel-counter"

INPUT=$(cat)
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Only track code files
case "$file_path" in
  *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.java|*.c|*.cpp|*.h|*.hpp|*.rs|*.rb|*.php)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Get session ID for scoping counter
session_id="${FOXCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-}}"
if [[ -z "$session_id" || "$session_id" == "null" ]]; then
  session_id="default"
fi

# Create counter directory
mkdir -p "$COUNTER_DIR" 2>/dev/null || true

# Counter file per session (hash for safety)
counter_hash=$(printf '%s' "$session_id" | shasum -a 256 | cut -c1-16)
counter_file="$COUNTER_DIR/count-$counter_hash"
suggested_file="$COUNTER_DIR/suggested-$counter_hash"

# Read current count
current_count=0
if [[ -f "$counter_file" ]]; then
  current_count=$(cat "$counter_file" 2>/dev/null || echo "0")
fi

# Increment count
new_count=$((current_count + 1))
printf '%d' "$new_count" > "$counter_file"

# Check if we've already suggested this session
if [[ -f "$suggested_file" ]]; then
  echo '{}'
  exit 0
fi

# Check if we hit threshold
if [[ "$new_count" -lt "$THRESHOLD" ]]; then
  echo '{}'
  exit 0
fi

# Mark as suggested so we don't repeat
touch "$suggested_file"

# Build suggestion context
context="---
**Tip:** You've read $new_count code files this session. Consider running:

\`\`\`
/counsel <question>
\`\`\`

For multi-perspective analysis (security, correctness, performance).

Example: \`/counsel review this code for potential issues\`"

jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
