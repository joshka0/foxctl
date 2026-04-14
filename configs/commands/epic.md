# Epic Planner

Transform a user request into a structured epic with goal and actionable todos.

## Request

$ARGUMENTS

## Instructions

You are an expert at breaking down complex requests into actionable plans. Given the user's request above:

### Step 1: Analyze the Request

Understand the scope and intent:
- What is the user trying to achieve?
- What are the key deliverables?
- What are the implicit requirements?

### Step 2: Create the Epic

Formulate a clear, concise goal statement that captures the essence of the request.

**Goal format:** A single sentence describing the desired end state.

Example: "Implement user authentication with OAuth2 support and session management"

### Step 3: Break Down into Todos

Create 3-8 actionable todos that, when completed, achieve the goal. Each todo should be:
- **Specific**: Clear what needs to be done
- **Actionable**: Can be started immediately
- **Ordered**: Dependencies considered (earlier todos don't depend on later ones)
- **Appropriately sized**: Not too granular, not too broad

### Step 4: Execute

1. **Set the epic anchor** using sqlite3 (requires workspace + session ID):
```bash
WORKSPACE="${CLAUDE_PROJECT_DIR:-$(pwd)}"
SESSION_ID="${CLAUDE_SESSION_ID:-$(cat ~/.foxctl/sessions/active/*.json 2>/dev/null | jq -r '.session_id' | head -1)}"
EPIC_ID="$(python3 -c 'import ulid; print(ulid.new().str)' 2>/dev/null || uuidgen | tr -d '-' | tr '[:upper:]' '[:lower:]' | head -c 26)"

sqlite3 ~/.foxctl/storage/tasks.db "
INSERT INTO epics (id, workspace_id, title, goal, status, created_at, session_id)
VALUES ('$EPIC_ID', '$WORKSPACE', 'Epic Title Here', 'Goal statement here', 'active', datetime('now'), '$SESSION_ID');

INSERT INTO active_epics (workspace_id, session_id, epic_id) VALUES ('$WORKSPACE', '$SESSION_ID', '$EPIC_ID')
ON CONFLICT(workspace_id, session_id) DO UPDATE SET epic_id = excluded.epic_id;
"
```

2. **Create the todos** using TodoWrite tool with all the planned tasks.

3. **Confirm to the user** with a summary:
```
Epic: "<title>"
Goal: <goal statement>
Session: <session_id>

Todos:
1. [ ] First task
2. [ ] Second task
...

Ready to begin with task 1.
```

### Guidelines

- If the request is vague, make reasonable assumptions and note them
- If the request is too large (would need >8 todos), suggest breaking into multiple epics
- Consider research/exploration tasks first if the domain is unfamiliar
- Include verification tasks (tests, reviews) where appropriate
- Don't include documentation tasks unless explicitly requested

### Example

**Request:** "Add dark mode to the app"

**Epic:**
- Title: "Dark Mode Support"
- Goal: "Enable users to switch between light and dark themes with persistent preference"

**Todos:**
1. Research existing theme/styling patterns in the codebase
2. Create theme context with light/dark color tokens
3. Add theme toggle component to settings
4. Update core UI components to use theme tokens
5. Persist theme preference to local storage
6. Test theme switching across all major views

---

Now analyze the request and create the epic with todos. After creating them, immediately start working on the first todo.
