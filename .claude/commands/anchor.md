# Epic Anchor

Set or manage the current epic (high-level goal) for this session. Epics persist and group related todos together. Each session can have its own active epic within a workspace.

## Command

$ARGUMENTS

## Instructions

Parse the command to determine the operation:

### Operations

1. **Set epic** (default - when arguments provided):
   - `/anchor <goal description>` - Create new epic and set as active for this session
   - The goal can be a simple statement or a detailed specification

### Goal Formatting

When the user provides a goal, help them reformat it into a **comprehensive anchor** that maintains context throughout the session. A well-crafted anchor should include:

```
<Objective>: One clear sentence stating the end goal

<Tasks>:
A) First deliverable - brief description
B) Second deliverable - brief description
C) Third deliverable - brief description

<Success Criteria>:
- How we know each task is complete
- Tests to run, checks to perform

<Constraints>: (optional)
- Key patterns to follow
- Things to avoid
```

**Example transformation:**

User says: "optimize the todo skill"

Reformat to:
```
Optimize todo/manage skill performance:
A) SQLite: WAL + busy timeout + connection pool settings
B) PageRank: gate synchronous recomputation for >500 tasks
C) Graph store: reuse within batch operations
D) HTTP clients: ensure timeouts in planner + embedder

Success: skill builds, tests pass, no hanging operations
```

This format works because:
1. **Enumerated tasks (A, B, C...)** become the todo list automatically
2. **Success criteria** define when to stop
3. **Constraints** prevent scope drift

If the user provides a vague goal, ask clarifying questions to build a proper anchor. If they provide a detailed specification, extract the key tasks and format appropriately.

2. **Show current** (no arguments):
   - `/anchor` - Show the active epic for this session and its linked tasks

3. **List epics**:
   - `/anchor list` - List all epics for this workspace

4. **Complete epic**:
   - `/anchor complete` - Mark the active epic as completed

5. **Clear epic**:
   - `/anchor clear` - Clear the active epic for this session without completing it

6. **Enable anchor mode** (stop enforcement):
   - `/anchor on` - Enable anchor mode (Stop hook will block until work is complete)
   - This is automatically enabled when setting a new anchor

7. **Disable anchor mode**:
   - `/anchor off` - Disable anchor mode (allows stopping without completing anchor)

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

### Session Persistence

After creating/updating an epic in the database, **also call the `session/anchor` skill** to persist the goal across compactions:

```bash
# Persist via session/anchor skill (survives compactions)
agentctl run session/anchor --input '{
  "operation": "set",
  "workspace": "'"$WORKSPACE"'",
  "session_id": "'"$SESSION_ID"'",
  "main_prompt": "<the formatted epic goal>"
}'
```

This ensures the anchor is:
1. Stored in `tasks.db` as an epic (for task grouping)
2. Persisted via `session/anchor` (survives compactions, injected on restore)

### Anchor Mode Flag (Stop Enforcement)

The anchor mode flag enables the Stop hook to block until anchor work is complete. Set this flag when creating an anchor:

```bash
# Set anchor mode flag (enables stop enforcement)
MODE_DIR="$HOME/.agentctl/cache/session-modes"
mkdir -p "$MODE_DIR"
ANCHOR_HASH=$(echo -n "anchor:${SESSION_ID}" | shasum -a 256 | cut -c1-16)
NOW_MS=$(( $(date +%s) * 1000 ))

# Create/update flag file
jq -n \
  --argjson updated_at "$NOW_MS" \
  --arg goal "<short summary of the goal>" \
  '{updated_at: $updated_at, goal: $goal}' \
  > "$MODE_DIR/anchor-${ANCHOR_HASH}.json"

# Clear anchor mode flag (for /anchor off or /anchor complete)
rm -f "$MODE_DIR/anchor-${ANCHOR_HASH}.json"
```

**When to set the flag:**
- `/anchor <goal>` - Set flag automatically (anchor mode ON by default)
- `/anchor on` - Set flag explicitly (re-enable after `/anchor off`)

**When to clear the flag:**
- `/anchor off` - Clear flag (disable stop enforcement)
- `/anchor complete` - Clear flag (work is done)
- `/anchor clear` - Clear flag (abandoning anchor)

The Stop hook (`todo-continuation.sh`) checks this flag and blocks if:
1. Anchor mode is enabled (`anchor-<hash>.json` exists and is fresh)
2. There are incomplete tasks in the session

### Response Format

After executing:

1. **For set**: Confirm the epic was created and set as active
   ```
   Anchored to: "<epic title>"
   Goal: <goal description>
   Session: <session_id>
   Persisted: via session/anchor skill
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
