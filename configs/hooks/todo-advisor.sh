#!/usr/bin/env bash
# todo-advisor.sh - PreToolUse advisory for TodoWrite
#
# Provides context about agentctl todo integration when Claude uses TodoWrite.
# FAST: Uses direct sqlite3 query instead of spawning agentctl process.
#
# Environment:
#   AGENTCTL_TODO_ADVISOR_DISABLED - Set to "1" to disable

set -euo pipefail

# Allow disabling
if [[ "${AGENTCTL_TODO_ADVISOR_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

# IDEMPOTENCY: Skip if we've run recently (within 60 seconds)
# This prevents repeated slow queries when Claude uses TodoWrite multiple times
CACHE_FILE="/tmp/.agentctl-todo-advisor-cache"
CACHE_TTL=60

if [[ -f "$CACHE_FILE" ]]; then
  cache_mtime=$(stat -f %m "$CACHE_FILE" 2>/dev/null || stat -c %Y "$CACHE_FILE" 2>/dev/null || echo 0)
  now=$(date +%s)
  age=$((now - cache_mtime))

  if [[ $age -lt $CACHE_TTL ]]; then
    # Return cached response
    cat "$CACHE_FILE"
    exit 0
  fi
fi

# Read hook input (discard - we just need to provide context)
cat >/dev/null

# Get workspace
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# SQL escape helper - escapes single quotes for SQLite
sql_escape() {
  printf '%s' "$1" | sed "s/'/''/g"
}

# FAST PATH: Direct sqlite3 query instead of spawning agentctl
DB_PATH="$HOME/.agentctl/storage/tasks.db"

if [[ -f "$DB_PATH" ]] && command -v sqlite3 &>/dev/null; then
  # Quick count query - use sql_escape to prevent SQL injection
  escaped_ws=$(sql_escape "$workspace")
  pending=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM tasks WHERE workspace='$escaped_ws' AND status IN ('pending','in_progress');" 2>/dev/null || echo "0")
  active_task=$(sqlite3 "$DB_PATH" "SELECT title FROM tasks WHERE workspace='$escaped_ws' AND status='in_progress' LIMIT 1;" 2>/dev/null || echo "")
else
  # Fallback: No database, just provide generic advice
  pending="?"
  active_task=""
fi

# Build advisory context
context="**agentctl tasks:** $pending pending"
if [[ -n "$active_task" ]]; then
  context="$context | Active: \"$active_task\""
fi
context="$context
Use \`/agentctl-todo\` to sync or \`agentctl todo list\` to see tasks."

# Build and cache response
response=$(jq -nc --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}')

echo "$response" > "$CACHE_FILE"
echo "$response"
