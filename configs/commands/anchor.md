# Epic Anchor

Set or manage the current epic (high-level goal) for this session. Epics persist and group related todos together. Each session can have its own active epic within a workspace.

## Command

$ARGUMENTS

## Instructions

Parse the command to determine the operation:

### Operations

1. **Set epic** (default - when arguments provided):
   - `/anchor <goal description>` - Create new epic and set as active for this session
   - Example: `/anchor Implement user authentication system`

2. **Show current** (no arguments):
   - `/anchor` - Show the active epic for this session and its linked tasks

3. **List epics**:
   - `/anchor list` - List all epics for this workspace

4. **Complete epic**:
   - `/anchor complete` - Mark the active epic as completed

5. **Clear epic**:
   - `/anchor clear` - Clear the active epic for this session without completing it

### Implementation

Use sqlite3 on `~/.agentctl/storage/tasks.db`. You need both workspace and session ID:

```bash
# Get workspace and session
WORKSPACE="${CLAUDE_PROJECT_DIR:-$(pwd)}"
SESSION_ID="${CLAUDE_SESSION_ID:-$(cat ~/.agentctl/sessions/active/*.json 2>/dev/null | jq -r '.session_id' | head -1)}"

# Create new epic (also stores the creating session)
EPIC_ID="$(python3 -c 'import ulid; print(ulid.new().str)' 2>/dev/null || uuidgen | tr -d '-' | tr '[:upper:]' '[:lower:]' | head -c 26)"
sqlite3 ~/.agentctl/storage/tasks.db "
INSERT INTO epics (id, workspace_id, title, goal, status, created_at, session_id)
VALUES ('$EPIC_ID', '$WORKSPACE', 'Epic Title', 'Goal description', 'active', datetime('now'), '$SESSION_ID');
"

# Set active epic for this session
sqlite3 ~/.agentctl/storage/tasks.db "
INSERT INTO active_epics (workspace_id, session_id, epic_id) VALUES ('$WORKSPACE', '$SESSION_ID', '$EPIC_ID')
ON CONFLICT(workspace_id, session_id) DO UPDATE SET epic_id = excluded.epic_id;
"

# Get active epic for this session
sqlite3 ~/.agentctl/storage/tasks.db "
SELECT e.id, e.title, e.goal, e.status, e.created_at
FROM epics e JOIN active_epics a ON e.id = a.epic_id
WHERE a.workspace_id = '$WORKSPACE' AND a.session_id = '$SESSION_ID';
"

# List all epics in workspace
sqlite3 ~/.agentctl/storage/tasks.db "
SELECT id, title, status, created_at FROM epics
WHERE workspace_id = '$WORKSPACE' ORDER BY created_at DESC;
"

# List tasks for epic
sqlite3 ~/.agentctl/storage/tasks.db "
SELECT id, title, status FROM tasks WHERE epic_id = '<epic_id>';
"

# Clear active epic for this session
sqlite3 ~/.agentctl/storage/tasks.db "
DELETE FROM active_epics WHERE workspace_id = '$WORKSPACE' AND session_id = '$SESSION_ID';
"
```

### Response Format

After executing:

1. **For set**: Confirm the epic was created and set as active
   ```
   Anchored to: "<epic title>"
   Goal: <goal description>
   Session: <session_id>
   ```

2. **For show**: Display the active epic with its tasks
   ```
   Current Epic: "<epic title>"
   Goal: <goal description>
   Status: active
   Tasks: X pending, Y in_progress, Z completed
   ```

3. **For list**: Show all epics in a table format

4. **For complete/clear**: Confirm the action

### Important Notes

- Each session has its own active epic (multiple sessions can work on different epics in the same workspace)
- When creating a new epic with an existing active epic, ask if user wants to complete or archive the current one
- All new todos created while an epic is active should be automatically linked to it (handled by todo-sync hook)
- Use ULID format for IDs (26 characters, sortable)
- The workspace_id should match how tasks are scoped (typically the project path)
- Session ID comes from CLAUDE_SESSION_ID env var or the active session file
