#!/usr/bin/env bash
# memory-prompt.sh - Prompt to save memories when completing tasks
#
# PostToolUse hook for TodoWrite that reminds the agent to save
# gotchas, learnings, or decisions when tasks are completed.
#
# This helps build up the memory store with useful learnings that
# can be recalled later via file-memory-recall and semantic search.
#
# Environment:
#   AGENTCTL_MEMORY_PROMPT_DISABLED - Set to "1" to disable

set -euo pipefail

# Check if disabled
if [[ "${AGENTCTL_MEMORY_PROMPT_DISABLED:-}" == "1" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Read hook input from stdin
payload="$(cat)"

# Extract todos from tool_input
todos=$(printf '%s' "$payload" | jq -c '.tool_input.todos // []')

# Exit early if no todos
if [[ "$todos" == "[]" || "$todos" == "null" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Count completed tasks
completed_count=$(printf '%s' "$todos" | jq '[.[] | select(.status == "completed")] | length')

# If no tasks were completed, just approve
if [[ "$completed_count" -eq 0 ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Get the names of completed tasks
completed_tasks=$(printf '%s' "$todos" | jq -r '[.[] | select(.status == "completed") | .content] | join(", ")')

# Build the reminder message
if [[ "$completed_count" -eq 1 ]]; then
  hint="**Memory prompt:** You completed a task: \"$completed_tasks\"

If you learned something useful, encountered a gotcha, or made an important decision, save it:
\`\`\`bash
agentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<what you learned>\" --file - <<< '{}'
\`\`\`

Types: \`gotcha\` (tricky), \`decision\` (design choice), \`pattern\` (recurring solution), \`context\` (background)

**After saving a memory, respond with \`(memory saved)\` to confirm.**"
else
  hint="**Memory prompt:** You completed $completed_count tasks: $completed_tasks

If you learned something useful, encountered gotchas, or made important decisions, save them:
\`\`\`bash
agentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<what you learned>\" --file - <<< '{}'
\`\`\`

Types: \`gotcha\` (tricky), \`decision\` (design choice), \`pattern\` (recurring solution), \`context\` (background)

**After saving a memory, respond with \`(memory saved)\` to confirm.**"
fi

jq -nc --arg hint "$hint" '{
  decision: "approve",
  context: $hint
}'
