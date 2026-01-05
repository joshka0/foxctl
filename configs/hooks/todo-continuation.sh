#!/usr/bin/env bash
# todo-continuation.sh - Claude Code/OpenCode Stop hook that enforces task completion
#
# This hook prevents Claude from stopping when incomplete tasks remain, and provides
# intelligent task prioritization using PageRank, ready/blocked status, and cycle detection.
#
# Usage in .claude/settings.json or opencode hooks:
#   {
#     "hooks": {
#       "Stop": [
#         { "matcher": "", "hooks": ["$CLAUDE_PROJECT_DIR/.claude/hooks/todo-continuation.sh"] }
#       ]
#     }
#   }
#
# Environment variables:
#   AGENTCTL_TODO_CONTINUATION_DISABLED - Set to "1" to disable
#   AGENTCTL_TODO_CONTINUATION_MIN_PENDING - Minimum pending tasks to trigger (default: 1)
#   AGENTCTL_TODO_CONTINUATION_TOP_N - Number of top tasks to show (default: 5)

set -euo pipefail

# Check if disabled
if [[ "${AGENTCTL_TODO_CONTINUATION_DISABLED:-}" == "1" ]]; then
  exit 0
fi

AGENTCTL="${AGENTCTL_BIN:-agentctl}"
MIN_PENDING="${AGENTCTL_TODO_CONTINUATION_MIN_PENDING:-1}"
TOP_N="${AGENTCTL_TODO_CONTINUATION_TOP_N:-5}"

# Read hook input from stdin (JSON with session_id, cwd, etc.)
INPUT=$(cat)

# Extract workspace from hook input
WORKSPACE=$(echo "$INPUT" | jq -r '.cwd // empty' 2>/dev/null)
if [[ -z "$WORKSPACE" ]]; then
  WORKSPACE="${CLAUDE_PROJECT_DIR:-$(pwd)}"
fi

# Get ALL tasks (including completed) to determine ready status
# Note: empty status returns all tasks
ALL_TASKS_INPUT=$(jq -n \
  --arg ws "$WORKSPACE" \
  '{
    operation: "list",
    workspace_id: $ws,
    list: {
      ranked: true,
      include_metrics: true,
      sort_by: "pagerank"
    }
  }'
)

ALL_RESULT=$("$AGENTCTL" run todo/manage --input "$ALL_TASKS_INPUT" 2>/dev/null) || {
  # On error, allow the operation to proceed (fail-open)
  echo '{}'
  exit 0
}

# Build completed task ID set and extract pending/in_progress tasks
# Also calculate ready status (all dependencies completed)
# Build ID->Title map and enriched tasks
ENRICHED_TASKS=$(echo "$ALL_RESULT" | jq -c '
  (.data.tasks // []) as $all |
  # Build ID -> Title mapping
  ($all | map({key: .id, value: .title}) | from_entries) as $id_to_title |
  # Build set of completed task IDs
  ($all | map(select(.status == "completed")) | map(.id) | unique) as $completed_ids |
  # Process pending/in_progress tasks
  $all | map(select(.status == "pending" or .status == "in_progress")) | map(
    # Calculate blockers first (incomplete dependencies)
    ((.depends_on // []) | map(select(. as $dep | $completed_ids | index($dep) == null))) as $blocker_ids |
    # Map blocker IDs to titles
    ($blocker_ids | map($id_to_title[.] // .)) as $blocker_titles |
    . + {
      # A task is ready if it has no blockers
      ready: ($blocker_ids | length == 0),
      blockers: $blocker_titles,
      blocker_ids: $blocker_ids,
      # Unblocks count is in_degree (how many tasks depend on this one)
      unblocks_count: (.in_degree // 0)
    }
  )
')

# Extract ID->Title map for execution order
ID_TO_TITLE=$(echo "$ALL_RESULT" | jq -c '(.data.tasks // []) | map({key: .id, value: .title}) | from_entries')

# Separate into pending and in_progress
PENDING_TASKS=$(echo "$ENRICHED_TASKS" | jq -c '[.[] | select(.status == "pending")]')
IN_PROGRESS_TASKS=$(echo "$ENRICHED_TASKS" | jq -c '[.[] | select(.status == "in_progress")]')

PENDING_COUNT=$(echo "$PENDING_TASKS" | jq 'length')
IN_PROGRESS_COUNT=$(echo "$IN_PROGRESS_TASKS" | jq 'length')
TOTAL_INCOMPLETE=$((PENDING_COUNT + IN_PROGRESS_COUNT))

if [[ "$TOTAL_INCOMPLETE" -lt "$MIN_PENDING" ]]; then
  # All tasks complete or below threshold - allow stop
  echo '{"decision": "approve"}'
  exit 0
fi

# Get graph insights for cycle detection and topological order
INSIGHTS_INPUT=$(jq -n \
  --arg ws "$WORKSPACE" \
  '{
    operation: "graph_insights",
    workspace_id: $ws,
    graph_insights: {
      include_completed: false,
      limit: 20
    }
  }'
)

INSIGHTS_RESULT=$("$AGENTCTL" run todo/manage --input "$INSIGHTS_INPUT" 2>/dev/null) || true

# Extract cycles and topological order
CYCLES=$(echo "$INSIGHTS_RESULT" | jq -c '.data.insights.cycles // []' 2>/dev/null || echo "[]")
CYCLE_COUNT=$(echo "$CYCLES" | jq 'length')
# Map topological order IDs to titles
TOPO_ORDER=$(echo "$INSIGHTS_RESULT" | jq -r --argjson map "$ID_TO_TITLE" '
  .data.insights.topological_order // [] | map($map[.] // .) | join(" -> ")
' 2>/dev/null || echo "")

# Build the continuation prompt
CYCLE_WARNING=""
if [[ "$CYCLE_COUNT" -gt 0 ]]; then
  FIRST_CYCLE=$(echo "$CYCLES" | jq -r '.[0] | join(" -> ")')
  CYCLE_WARNING="
**CYCLE DETECTED**: $FIRST_CYCLE
This circular dependency must be resolved before continuing. Consider breaking one of the dependency links.
"
fi

# Format ready tasks (can be started immediately)
READY_TASKS=$(echo "$PENDING_TASKS" | jq -r --argjson n "$TOP_N" '
  [.[] | select(.ready == true)] | sort_by(-.pagerank) | .[:$n] | to_entries | map(
    "  \(.key + 1). \(.value.title // .value.id)\n     pagerank=\(.value.pagerank // 0 | tostring | .[0:6]) | unblocks=\(.value.unblocks_count // 0) tasks"
  ) | join("\n")
')

# Format blocked tasks (waiting on dependencies)
BLOCKED_TASKS=$(echo "$PENDING_TASKS" | jq -r '
  [.[] | select(.ready == false)] | sort_by(-.pagerank) | .[:3] | to_entries | map(
    "  - \(.value.title // .value.id)\n    blocked by: \(.value.blockers | join(", "))"
  ) | join("\n")
')

# Format in_progress tasks
IN_PROGRESS_LIST=""
if [[ "$IN_PROGRESS_COUNT" -gt 0 ]]; then
  IN_PROGRESS_LIST=$(echo "$IN_PROGRESS_TASKS" | jq -r '
    .[:3] | to_entries | map(
      "  - \(.value.title // .value.id)"
    ) | join("\n")
  ' 2>/dev/null || echo "")
fi

# Count ready vs blocked
READY_COUNT=$(echo "$PENDING_TASKS" | jq '[.[] | select(.ready == true)] | length')
BLOCKED_COUNT=$(echo "$PENDING_TASKS" | jq '[.[] | select(.ready == false)] | length')

# Build inject prompt with rich context
INJECT_PROMPT="[SYSTEM REMINDER - TODO CONTINUATION]

Incomplete tasks: $TOTAL_INCOMPLETE ($READY_COUNT ready, $BLOCKED_COUNT blocked, $IN_PROGRESS_COUNT in progress)
$CYCLE_WARNING"

if [[ -n "$READY_TASKS" ]]; then
  INJECT_PROMPT="$INJECT_PROMPT
**READY TO START** (sorted by impact - tasks that unblock the most work):
$READY_TASKS"
fi

if [[ -n "$BLOCKED_TASKS" ]]; then
  INJECT_PROMPT="$INJECT_PROMPT

**BLOCKED** (waiting on dependencies):
$BLOCKED_TASKS"
fi

if [[ -n "$IN_PROGRESS_LIST" ]]; then
  INJECT_PROMPT="$INJECT_PROMPT

**IN PROGRESS** (complete these first):
$IN_PROGRESS_LIST"
fi

if [[ -n "$TOPO_ORDER" && "$TOPO_ORDER" != "null" ]]; then
  INJECT_PROMPT="$INJECT_PROMPT

**Execution Order**: $TOPO_ORDER"
fi

INJECT_PROMPT="$INJECT_PROMPT

Continue with ready tasks first. Mark each complete when finished.
Do not stop until all tasks are done or explicitly told to stop by the user."

# Output block decision with inject prompt and rich metadata
jq -n \
  --arg reason "Incomplete tasks remain ($TOTAL_INCOMPLETE pending)" \
  --arg prompt "$INJECT_PROMPT" \
  --argjson cycles "$CYCLE_COUNT" \
  --argjson ready "$READY_COUNT" \
  --argjson blocked "$BLOCKED_COUNT" \
  --argjson in_progress "$IN_PROGRESS_COUNT" \
  '{
    decision: "block",
    reason: $reason,
    inject_prompt: $prompt,
    stop_hook_active: true,
    metadata: {
      incomplete_count: '"$TOTAL_INCOMPLETE"',
      ready_count: $ready,
      blocked_count: $blocked,
      in_progress_count: $in_progress,
      cycle_count: $cycles
    }
  }'
