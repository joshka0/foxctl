# Claude Code Integration with agentctl

This project uses **agentctl** as the unified runtime for Claude Code hooks and
skills. All automation flows through agentctl's JSON envelope protocol,
providing:

- **Single binary** - No Python dependencies, ~5ms startup
- **Unified storage** - SQLite-backed tasks, CAS for artifacts, structured
  memory
- **Task-centric model** - All work is tracked via tasks with traceability

## Architecture

```text
Claude Code → Hooks (bash wrappers) → agentctl skills → SQLite/CAS
                                           ↓
                                    JSON envelope output
```

## Active Hooks

### PreToolUse: Task Guard

**File:** `.claude/hooks/task-guard.sh`\
**Skill:** `hooks/task_guard`

Enforces task-centric model for write operations (Edit, Write, MultiEdit,
NotebookEdit):

- **Auto mode** (default): Auto-creates a task if none exists
- **Strict mode**: Blocks writes without an active task

Set mode via: `AGENTCTL_TASK_GUARD_MODE=strict`

## Available Skills

### Task Management (`todo/manage`)

```bash
# Add a task
agentctl todo add --title "Implement feature X" --description "Details..."

# List tasks
agentctl todo list

# Show active task
agentctl todo active

# Complete a task
agentctl todo complete --id <task-id> --notes "What was done"
```

### Skill Operations

```bash
# Run any skill directly
agentctl run <skill-name> --input '{"key": "value"}'

# List installed skills
agentctl skills list

# Describe a skill
agentctl skills describe <skill-name>
```

## Task Workflow

1. **Start work**: Create a task with `agentctl todo add --title "..."`
2. **Task guard activates**: PreToolUse hook ensures task exists before writes
3. **Work proceeds**: All edits are associated with the active task
4. **Complete task**: `agentctl todo complete --id <id> --notes "..."`

## Getting Started Workflows

### 1. Start a Task-Centric Coding Session

- **Step 1:** From a shell in the project root, create a task:
  - `agentctl todo add --title "<feature or bug>" --description "context"`
- **Step 2:** Optionally inspect tasks:
  - `agentctl todo list`
  - `agentctl todo active`
- **Step 3:** Work in Claude Code as usual; the PreToolUse task guard will
  auto-ensure or enforce an active task for write operations.

### 2. Use a Knowledge Pack While Editing

- When working in a specific domain, explicitly ask Claude to consult a
  knowledge pack, e.g.:
  - Frontend: "Consult the `frontend-dev-guidelines` knowledge pack while we
    update this component."
  - Backend: "Use the `backend-dev-guidelines` knowledge pack to review this
    handler."
- Claude should then pull from `docs/knowledge/<pack-name>/` as a reference
  while proposing changes.
- **Future:** `agentctl knowledge sync` will index these packs into SQLite;
  `hooks/knowledge_router` will auto-surface relevant packs when embedding
  similarity exceeds a threshold.

### 3. Run an Agent + Command for Planning

- **Step 1:** Use a command to generate a plan, for example via
  `.claude/commands/dev-docs.md` (from the command palette or by pasting the
  template and filling `$ARGUMENTS`).
- **Step 2:** Optionally register the plan as an `agentctl` task for tracking:
  - `agentctl todo add --title "<plan name>" --description "see dev-docs"`
- **Step 3:** For deep review of the implementation, ask Claude to use an agent
  such as `code-architecture-reviewer` on the relevant changes.

## Agents

Agents are specialized prompt profiles stored in `.claude/agents/`. They run as
standalone Claude sessions for deeper, multi-step work.

- **Location:** `.claude/agents/`
- **Examples:**
  - `code-architecture-reviewer`
  - `code-refactor-master`
  - `documentation-architect`
  - `plan-reviewer`
  - `refactor-planner`
  - `web-research-specialist`

**How to use:** Ask Claude explicitly, for example:

- "Use the `code-architecture-reviewer` agent to review the changes in this
  branch."
- "Use the `plan-reviewer` agent to sanity-check this implementation plan."

Stack-specific agents (e.g., `frontend-error-fixer`, `auth-route-tester`) are
also available but may reference technologies not used in this repo; treat them
as optional.

## Knowledge Packs

Knowledge packs are **Claude prompt/documentation bundles** (markdown +
resources) that provide domain-specific guidance. They are distinct from
executable **agentctl skills** (Go/WASI/exec plugins managed via
`agentctl skills ...`).

- **Location:** `docs/knowledge/`
- **Examples:**
  - `backend-dev-guidelines` — backend architecture and patterns
  - `frontend-dev-guidelines` — React/MUI/TanStack Router patterns
  - `error-tracking` — Sentry and error monitoring patterns
  - `route-tester` — authenticated route testing patterns
  - `skill-developer` — how to design and manage knowledge packs

`docs/knowledge/skill-rules.json` documents an auto-activation model (keywords,
`pathPatterns`, content patterns). This is a **design specification** for the
future `hooks/knowledge_router` hook.

### Future: `agentctl knowledge sync` + `hooks/knowledge_router`

1. **`agentctl knowledge sync`** will:
   - Walk `docs/knowledge/`, `.claude/agents/`, `.claude/commands/`.
   - Populate a SQLite **knowledge registry** (items, triggers, documents).
   - Optionally compute embeddings (`--embed`).

2. **`hooks/knowledge_router`** (event: `UserPromptSubmit`) will:
   - Match prompt against triggers + embeddings.
   - If **similarity ≥ threshold**, inject a short context hint naming the
     relevant packs.
   - If **below threshold**, emit `DecisionNone` with no extra context.

This keeps knowledge surfacing **advisory** (no blocking), while the task guard
remains the enforcement hook.

## Commands

Commands are reusable prompt templates stored in `.claude/commands/`. They are
intended to be invoked from Claude Code's command palette.

- **Location:** `.claude/commands/`
- **Available commands:**
  - `dev-docs` — create a comprehensive strategic plan with task breakdown
  - `dev-docs-update` — update an existing dev-docs plan
  - `route-research-for-testing` — research how to test specific routes

These commands currently describe filesystem-oriented workflows (e.g.
`dev/active/[task-name]/...`). You can optionally combine them with the
`agentctl todo` CLI by creating a corresponding task for each plan.

## Environment Variables

| Variable                   | Description             | Default       |
| -------------------------- | ----------------------- | ------------- |
| `AGENTCTL_TASK_GUARD_MODE` | `auto` or `strict`      | `auto`        |
| `AGENTCTL_HOME`            | Config/storage root     | `~/.agentctl` |
| `AGENTCTL_BIN`             | Path to agentctl binary | `agentctl`    |

## Storage Locations

- **Config**: `~/.agentctl/config.yaml`
- **Tasks DB**: `~/.agentctl/storage/tasks.db`
- **CAS**: `~/.agentctl/cas/sha256/<digest>`
- **Jobs**: `~/.agentctl/jobs/<ulid>/`

## Extending

### Adding a New Hook

1. Create skill in `skills/<name>/main.go`
2. Add `skill.yaml` manifest
3. Create bash wrapper in `.claude/hooks/<name>.sh`
4. Register in `.claude/settings.json`

### Hook Input/Output Contract

**Input** (stdin JSON):

```json
{
  "event": "PreToolUse",
  "workspace_root": "/path/to/project",
  "session_id": "...",
  "tool_name": "Edit",
  "tool_input": { "file_path": "..." }
}
```

**Output** (stdout JSON envelope):

```json
{
  "version": 1,
  "status": "ok",
  "command": "hooks/task_guard",
  "data": {
    "hook_output": {
      "decision": "approve",
      "reason": "task ensured",
      "meta": { "task_id": "...", "created": true }
    }
  }
}
```

## Development

```bash
# Build agentctl
make build

# Run tests
go test ./...

# Build skills
make skills-build
```

## Security Notes

- **TOCTOU vulnerabilities:** When validating paths (symlinks, containment
  checks), always use the _resolved_ path for all subsequent file operations
  (`os.Stat`, `os.Open`). Validating path A then operating on path B allows
  symlink swaps between check and use.
