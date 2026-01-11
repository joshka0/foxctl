#!/bin/bash
# Post-compact session restore hook (UserPromptSubmit)
# Checks for pending-restore marker created by PreCompact hook
# Runs session/restore and injects context on first prompt after compaction

# Get workspace from env or git
WORKSPACE="${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"

# Check for pending-restore marker
MARKER_DIR="$HOME/.agentctl/sessions/pending-restore"
WORKSPACE_HASH=$(echo -n "$WORKSPACE" | shasum -a 256 | cut -c1-16)
MARKER_FILE="$MARKER_DIR/$WORKSPACE_HASH.json"

# Check marker age atomically to reduce TOCTOU race window
# If stat fails (file doesn't exist or is deleted), exit gracefully
if [ "$(uname)" = "Darwin" ]; then
  MARKER_MTIME=$(stat -f %m "$MARKER_FILE" 2>/dev/null) || exit 0
else
  MARKER_MTIME=$(stat -c %Y "$MARKER_FILE" 2>/dev/null) || exit 0
fi

MARKER_AGE=$(( $(date +%s) - MARKER_MTIME ))

if [ "$MARKER_AGE" -gt 600 ]; then
  # Marker too old, delete it and exit
  rm -f "$MARKER_FILE"
  exit 0
fi

# Read session ID from marker (if available)
SESSION_ID=$(jq -r '.session_id // empty' "$MARKER_FILE" 2>/dev/null)

# Delete marker BEFORE running restore to prevent re-triggering
rm -f "$MARKER_FILE"

# Build input for session/restore
INPUT=$(jq -n \
  --arg workspace "$WORKSPACE" \
  --arg session_id "$SESSION_ID" \
  '{
    workspace: $workspace,
    session_id: (if $session_id == "" then null else $session_id end)
  }')

# Run the session restore skill and capture output
RESULT=$(agentctl run session/restore --input "$INPUT" 2>/dev/null)

# Extract context from envelope
CONTEXT=$(echo "$RESULT" | jq -r '.data.hook_output.context // empty' 2>/dev/null)

if [ -n "$CONTEXT" ]; then
  # Output format for UserPromptSubmit hooks (must use hookSpecificOutput wrapper)
  jq -n --arg ctx "$CONTEXT" '{
    "hookSpecificOutput": {
      "hookEventName": "UserPromptSubmit",
      "additionalContext": $ctx
    }
  }'
fi

exit 0
