# agentctl Claude Code Integration

> Protocol, profiles, invariants: `AGENTS.md` | Full docs: `docs/`

## Architecture

```
Claude Code → Hooks → agentctl skills → SQLite/CAS → JSON envelope
```

## New: Session Lineage & Context Preservation

- **Single active session per workspace/agent**: session start/resume/fork
  checks for an active session and requires `--force` to override. Active is
  determined by status (e.g., `running`), not `ended_at`.
- **Status-driven lifecycle**: terminal statuses (`ok`, `error`, `canceled`) set
  `ended_at`; non-terminal statuses clear it when a session reopens.
- **Identity fallback file**:
  `~/.agentctl/sessions/active/<workspace_hash>.json` stores `session_id`,
  `agent_id`, and lineage fields for hooks without env access.
- **Env propagation to skills**: Exec and WASI runners now forward
  `AGENTCTL_SESSION_ID`, `AGENTCTL_AGENT_ID`, and fallbacks
  (`CLAUDE_SESSION_ID`, `OPENCODE_SESSION_ID`, `CURSOR_SESSION_ID`,
  `TERM_SESSION_ID`) so skills correctly attribute turns, embeddings, and
  artifacts.
- **Lineage visibility**: `agentctl sessions chain` shows ancestors via
  `parent_session_id`; trajectories and capture now record `session_id` for
  joins.

## Active Hooks

| Event            | Hook                       | Purpose                                                                        |
| ---------------- | -------------------------- | ------------------------------------------------------------------------------ |
| PreToolUse       | `overseer-inbox`           | Surfaces human messages on Read/Bash/Grep/Glob/Task for human-in-the-loop      |
| PreToolUse       | `semantic-search`          | Vector search on Grep/Glob (symbols, sessions, memories, tasks, codemaps)      |
| PreToolUse       | `file-memory-recall`       | Surfaces memories/gotchas before editing                                       |
| PreToolUse       | `task-guard`               | Ensures task exists for writes                                                 |
| PostToolUse      | `read-context-suggestions` | Suggests context_ripgrep for symbols after reading code (full function bodies) |
| PostToolUse      | `lsp-diagnostics`          | Shows LSP errors after editing                                                 |
| PostToolUse      | `memory-prompt`            | Prompts to save memories when tasks are completed                              |
| PreCompact       | `session-save`             | Captures session state                                                         |
| SessionStart     | `session-restore`          | Restores context on compact/resume                                             |
| UserPromptSubmit | `memory-detector`          | Detects save/recall/todo patterns                                              |
| UserPromptSubmit | `skill-advisor`            | Suggests agentctl skills based on prompt patterns                              |
| Stop             | `todo-continuation`        | Blocks stop if tasks remain; injects PageRank-prioritized continuation prompt  |
| Stop             | `plan-sync`                | Syncs plans to tasks                                                           |

### Human-in-the-Loop: Overseer Inbox

Send messages to a running Claude session from terminal, scripts, or other agents:

```bash
# Using agentctl-mail (recommended - auto-detects workspace)
agentctl-mail "Priority change" "Focus on auth bug first"
agentctl-mail -p 1 "STOP" "Pause and review this issue"
agentctl-mail --ack "Review needed" "Check the API changes"
agentctl-mail --to claude "Question" "How should we handle auth?"

# Shell script wrapper (backwards compatibility)
./scripts/overseer-send.sh "Subject" "Body"
```

**How it works:**
- `agentctl-mail` binary auto-detects workspace via git root, CLAUDE_PROJECT_DIR, or cwd
- `overseer-inbox` hook runs on Read/Bash/Grep/Glob/Task tool calls (PreToolUse)
- Checks board mailbox for unread messages addressed to `overseer`
- Injects messages into Claude's context before tool execution
- Auto-acks displayed messages (configurable via `AGENTCTL_OVERSEER_AUTOACK=0`)

**Environment variables:**
| Variable                     | Default    | Description                                      |
| ---------------------------- | ---------- | ------------------------------------------------ |
| `AGENTCTL_OVERSEER_RECIPIENT` | `overseer` | Recipient to monitor (use `*` for broadcast)    |
| `AGENTCTL_OVERSEER_AUTOACK`  | `1`        | Auto-mark displayed messages as read            |

## Quick Commands

```bash
# Tasks
agentctl todo add --title "Task" --description "Details"
agentctl todo list                              # JSON output (default)
agentctl todo list -f table                     # Pretty table with status icons
agentctl todo list -f compact                   # One-liner per task
agentctl todo list --status pending -f table    # Filter by status
agentctl todo list --jq '.data.tasks[].title'   # Extract with jq
agentctl todo active
agentctl todo complete --id <id> --notes "Done"

# CI / PR Review
agentctl ci status --pr 123                      # Unified: CI + comments + merge
agentctl ci comments --pr 123 --source greptile  # Filter: coderabbit, greptile, human
agentctl ci results --pr 123 --failed            # CI check results

# Incremental Indexing
agentctl index git-diff                          # Index files changed by git pull
agentctl index git-diff --base HEAD~3            # Index last 3 commits
agentctl index git-diff --dry-run                # Preview what would be indexed

# Skills
agentctl run <skill> --input '<json>'
agentctl run <skill> --input '<json>' -f table   # Format output as table
agentctl run <skill> --input '<json>' --jq '...' # Filter with jq expression
agentctl skills list

# Workflows
agentctl workflow run pre-impl-analysis --input '{"path": "."}'
agentctl workflow list

# Cross-Workspace Index Sync (Turso)
agentctl index sync push --scope memory     # Push local embeddings to Turso
agentctl index sync query --query "..." --global  # Search across all workspaces
```

### Cross-Workspace Turso Sync

Push local embeddings to a central Turso database for cross-workspace knowledge sharing:

```bash
# Set Turso credentials
export TURSO_DATABASE_URL=libsql://your-db.turso.io
export TURSO_AUTH_TOKEN=your-token

# Push local memory embeddings to Turso
agentctl index sync push --scope memory

# Query across all workspaces globally
agentctl index sync query --query "authentication middleware" --global

# Query specific workspaces
agentctl index sync query --query "API design" --workspaces project-a,project-b
```

**Requirements:**
- CGO build (`make build-cgo`) for Turso support
- `VOYAGE_API_KEY` for query embedding generation
- `TURSO_DATABASE_URL` and `TURSO_AUTH_TOKEN` for remote access

### Git Pull Auto-Index

Automatically index changed files after `git pull`:

```bash
# One-time setup
echo '#!/bin/sh
agentctl index git-diff' > .git/hooks/post-merge
chmod +x .git/hooks/post-merge
```

### Output Formatting

The `--format` (`-f`) and `--jq` flags work on `agentctl run` and `agentctl todo list`:

| Format    | Description                                      |
| --------- | ------------------------------------------------ |
| `json`    | Default JSON output                              |
| `table`   | Pretty table with columns, status icons (✅🔄⏳🚫) |
| `compact` | One-liner per item                               |

```bash
# Examples
agentctl todo list -f table                    # Pretty task table
agentctl todo list --jq '.data.tasks[].title'  # Extract just titles
agentctl run code/symbols --input '...' -f table
```

## Key Skills

| Skill                          | Description                                               |
| ------------------------------ | --------------------------------------------------------- |
| `code/complexity`              | Complexity analysis                                       |
| `code/symbols`                 | Extract symbols                                           |
| `code/swe_grep`                | Smart code retrieval                                      |
| `code/context_ripgrep`         | Search and return full function bodies containing matches |
| `code/smart_write`             | Symbol-based editing with dry-run diff preview            |
| `test/run`                     | Run tests with coverage                                   |
| `lsp/gopls`                    | Go LSP operations                                         |
| `mobile/ios`, `mobile/android` | Simulator automation                                      |
| `embedding/queue`              | Background embedding generation                           |
| `code/semantic_search`         | Semantic code search                                      |

## Environment

| Variable                              | Default       | Description                                          |
| ------------------------------------- | ------------- | ---------------------------------------------------- |
| `AGENTCTL_TASK_GUARD_MODE`            | `auto`        | `auto` or `strict`                                   |
| `AGENTCTL_TODO_CONTINUATION_DISABLED` | `0`           | Set to `1` to disable todo continuation enforcement  |
| `AGENTCTL_TODO_CONTINUATION_MIN_PENDING` | `1`        | Minimum pending tasks to trigger continuation        |
| `AGENTCTL_TODO_CONTINUATION_TOP_N`    | `3`           | Number of top tasks to show in continuation prompt   |
| `AGENTCTL_HOME`               | `~/.agentctl` | Storage root                                         |
| `VOYAGE_API_KEY`              | -             | For Voyage embeddings/reranking (1024 dimensions)    |
| `GEMINI_API_KEY`              | -             | For Gemini embeddings (3072 dimensions)              |
| `MISTRAL_API_KEY`             | -             | For Mistral/Codestral embeddings (1024 dimensions)   |
| `AGENTCTL_VECTOR_DIMS`        | `1024`        | Global default vector dimensions (Voyage=1024)       |
| `AGENTCTL_SEMANTIC_RERANK`    | `0`           | Set to `1` to enable Voyage rerank-2.5 in hooks      |
| `AGENTCTL_EMBEDDING_RATE_LIMIT` | `3`         | Embedding RPM: 0=disabled (paid tier), >0=limit      |

### Scope-Based Embedding Models

Voyage AI is the recommended provider for embeddings (based on Dec 2024 benchmarks):

| Scope      | Content Type    | Model            | Price/1M | Rationale                             |
| ---------- | --------------- | ---------------- | -------- | ------------------------------------- |
| `symbols`  | Code            | `voyage-code-3`  | $0.18    | 13.80% better than OpenAI on code     |
| `memory`   | Gotchas/notes   | `voyage-3.5`     | $0.06    | Good quality at 3x cost savings       |
| `codemaps` | Semantic maps   | `voyage-3.5`     | $0.06    | Matches memory - semantic text        |
| `tasks`    | Task desc       | `voyage-3.5`     | $0.06    | Good quality at 1/3 cost              |
| `sessions` | Session context | `voyage-3.5`     | $0.06    | Good quality at 1/3 cost              |

All Voyage models use 1024 dimensions. Use `ScopeModelRecommendation(scope)` to get the
appropriate model for each scope.

> ⚠️ **Embedding Dimension Mismatch**: Gemini=3072, Voyage/Mistral/Codestral=1024.
> Query and stored embeddings MUST use the same provider per scope.

### Observability (Wide Events)

**ENABLED** via `.env`: `AGENTCTL_OBS_DIR=$HOME/.agentctl/observability`
(Note: Use absolute path, not `~` - tilde isn't expanded in env vars)

| Variable                       | Default | Description                           |
| ------------------------------ | ------- | ------------------------------------- |
| `AGENTCTL_OBS_DIR`             | -       | Enable observability; root directory  |
| `AGENTCTL_OBS_SAMPLE_ERRORS`   | `true`  | Always sample error events            |
| `AGENTCTL_OBS_SLOW_THRESHOLD_MS` | `1000`  | Slow request threshold (ms)         |
| `AGENTCTL_OBS_SAMPLE_RATE`     | `0.05`  | Random sample rate for healthy events |
| `AGENTCTL_TRACE_ID`            | auto    | Propagate trace ID to child processes |

**Why enabled:** Captures skill execution events, errors, and timing for debugging.
Events are stored as NDJSON in `~/.agentctl/observability/events/`.

See `docs/observability/wide-events.md` for full documentation.

## Storage

**Persistent data** (`~/.agentctl/storage/`):
- `memory.db` - Named memories (gotchas, decisions, patterns, codemaps, symbols)
- `tasks.db` - Task management with dependencies
- `sessions.db` - Session lineage and context
- `trajectory.db` - Agent work audit trail
- `agents.db` - Agent registry

**Cache/ephemeral** (`~/.agentctl/cache/`):
- `embedding_queue.db` - Symbol embedding job queue (regenerable)
- `cache.db` - Skill result cache with TTL

**Other**:
- CAS: `~/.agentctl/cas/sha256/<digest>`
- Jobs: `~/.agentctl/jobs/<ulid>/`
- Observability: `~/.agentctl/observability/events/` (NDJSON event logs)
- Backups: `~/.agentctl/backups/` (includes observability component)

## TypeScript Packages (GUI/TUI/API)

The `packages/` directory contains TypeScript applications for viewing agentctl data:

```
packages/
├── data/     # Shared API client + types (@agentctl/data)
├── gui/      # Web GUI - React/Vite/Tailwind (@agentctl/gui)
│   └── server/index.js  # API server (Express, port 8090)
└── tui/      # Terminal UI - OpenTUI (@agentctl/tui)
```

### Running the Viewers

```bash
# Install dependencies (from repo root)
bun install

# Start API server + Web GUI (concurrent)
make ts-dev-gui
# or: bun run dev:all

# Start TUI (connect to running API server)
AGENTCTL_API_URL=http://localhost:8090 bun run --cwd packages/tui dev

# Individual commands
bun run dev:server   # API server only (port 8090)
bun run dev:gui      # Web GUI only (needs server)
bun run dev:tui      # TUI only (needs server)
```

### TUI Views (keyboard shortcuts)

| Key | View         | Description                           |
| --- | ------------ | ------------------------------------- |
| 1   | Jobs         | Job queue with status, timing         |
| 2   | Tasks        | Task list with dependencies           |
| 3   | Insights     | PageRank, critical path, cycles       |
| 4   | Mailbox      | Actor messages with priority          |
| 5   | Reservations | File locks (exclusive/shared)         |
| 6   | Stats        | Job statistics dashboard              |
| 7   | Blackboard   | Key-value store browser               |
| 8   | SQLite       | Direct SQL query interface            |
| 9   | Search       | Full-text search                      |

Navigation: `j/k` (up/down), `[/]` (prev/next view), `r` (refresh), `q` (quit)

## Development

```bash
make build        # Build agentctl (pure Go, no CGO)
make build-cgo    # Build with CGO (required for Turso vector search)
go test ./...     # Run tests
make skills-install # Build AND install all skills (preferred over skills-build)
```

> **Note:** Use `make skills-install` instead of `make skills-build` - it
> rebuilds everything and installs to `~/.agentctl/skills/`. **TODO:** Add
> `make skills-install SKILL=<name>` for single-skill rebuild+install.

## ⚠️ CGO Build Requirement

**NEVER use raw `CGO_ENABLED=1 go build`** - it causes duplicate SQLite symbol
errors.

Both `go-libsql` (Turso) and `go-sqlite3` embed SQLite, causing 266 linker
conflicts.

**Always use:**

```bash
# Makefile target (recommended)
make build-cgo

# Or with the required tag
CGO_ENABLED=1 go build -tags=libsqlite3 -o bin/agentctl-cgo ./cmd/agentctl
```

The `-tags=libsqlite3` flag tells `go-sqlite3` to use system SQLite instead of
embedding.

## Gemini Coordination

```bash
# Get second opinion
context=$(agentctl run code/complexity --input '{"path": "."}')
echo "$context" | gemini -p "Refactoring priorities?"
```

## Security

**TOCTOU:** Always use resolved paths for all file operations after validation.

## Gotchas

### Build Commands

**NEVER run `go build ./...`** - causes 266 duplicate SQLite symbol errors due
to `go-libsql` (Turso) and `go-sqlite3` both embedding SQLite.

Use instead:

```bash
make build          # Pure Go (no CGO)
make build-cgo      # CGO with -tags=libsqlite3
CGO_ENABLED=0 go build ./internal/...  # Compile-check specific packages
```

### Skill Build → Install (Two-Stage Process)

**Skills require TWO steps to deploy changes:**

1. **Build**: Compiles the skill source code to a binary
2. **Install**: Copies the binary to `~/.agentctl/skills/<name>/bin`

The skill resolver always looks for `bin` in the installed skill directory
(`~/.agentctl/skills/<skill-name>/`), NOT the source directory.

**Common mistake:** Running `go build ./skills/<name>` creates a binary in the
wrong location. The installed skill continues running the OLD binary.

**Correct workflow:**

```bash
# Option 1: Rebuild all skills and install (safest)
make skills-install

# Option 2: Single skill build + install
CGO_ENABLED=0 go build -o ~/.agentctl/skills/<skill-category>/<skill-name>/bin ./skills/<source-dir>

# Example for code/semantic_search:
CGO_ENABLED=0 go build -o ~/.agentctl/skills/code/semantic_search/bin ./skills/code_semantic_search
```

**Debug tip:** If skill changes aren't taking effect:
1. Check timestamps: `ls -la ~/.agentctl/skills/<path>/bin`
2. Verify binary: `strings ~/.agentctl/skills/<path>/bin | grep "<expected-string>"`
3. Rebuild to correct location as shown above

### Memory Hooks

| Hook                 | Trigger                  | Notes                                                               |
| -------------------- | ------------------------ | ------------------------------------------------------------------- |
| `memory-detector`    | UserPromptSubmit         | Detects patterns → suggests `/remember`                             |
| `memory-prompt`      | PostToolUse (TodoWrite)  | Prompts to save memories when tasks are completed                   |
| `memory-capture`     | PostToolUse (Edit/Write) | **Enabled by default** - set `AGENTCTL_MEMORY_CAPTURE=0` to disable |
| `file-memory-recall` | PreToolUse (Edit/Write)  | Surfaces stored memories before editing                             |

**memory-detector patterns:**

- **Save**: "remember this", "the trick is", "TIL", "watch out for", "please
  don't", "don't forget"
- **Recall**: "how did we", "didn't we already", "last time we"
- **Todo**: "we need to", "let's make sure", "TODO:", "before we"

**skill-advisor patterns:**

| Pattern                                      | Suggested Skill                    |
| -------------------------------------------- | ---------------------------------- |
| "pr comments", "review comments", "feedback" | `ci/prcomments`                    |
| "ci status", "build status", "checks failed" | `ci/github_checks`                 |
| "semantic search", "vector search"           | `code/semantic_search`             |
| "investigate", "dig into", "explore"         | `code/swe_grep`, `semantic_search` |
| "complexity", "cyclomatic", "nesting"        | `code/complexity`                  |
| "symbols", "functions", "types"              | `code/symbols`                     |
| "imports", "dependencies"                    | `code/imports`                     |
| "security", "vulnerabilities"                | `code/security`                    |
| "git status", "uncommitted"                  | `git/status`                       |
| "blame", "hotspots", "who changed"           | `code/git`                         |
| "run tests", "coverage"                      | `test/run`                         |
| "definition", "references", "call hierarchy" | `lsp/gopls`                        |
| "past sessions", "session history"           | `session/recall`                   |
| "codemap", "trace code"                      | `codemap/generate`                 |

**`/remember` skill** - Dual-save to both agentctl memory AND CLAUDE.md:

```bash
# 1. Saves to agentctl memory
agentctl memory put --name "gotcha-<topic>" --type "gotcha" \
  --summary "<note>" --data '{"details": "..."}'

# 2. Appends to CLAUDE.md under Gotchas section
```

### Memory Storage Path

**Memory is stored in `storage/memory.db`, NOT `cache/memory.db`.**

When opening memory stores in Go code, always use `cfg.Storage.Root`:
```go
// CORRECT - persistent user data goes in storage/
store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)

// WRONG - cache is for ephemeral/regenerable data
store, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
```

---

Full documentation: `docs/` | Skills: `.claude/skills/` | Workflows:
`.claude/workflows/`
