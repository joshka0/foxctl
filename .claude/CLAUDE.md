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
| PreToolUse       | `semantic-search`          | Vector search on Grep/Glob (symbols, sessions, memories, tasks)                |
| PreToolUse       | `file-memory-recall`       | Surfaces memories/gotchas before editing                                       |
| PreToolUse       | `task-guard`               | Ensures task exists for writes                                                 |
| PostToolUse      | `read-context-suggestions` | Suggests context_ripgrep for symbols after reading code (full function bodies) |
| PostToolUse      | `lsp-diagnostics`          | Shows LSP errors after editing                                                 |
| PreCompact       | `session-save`             | Captures session state                                                         |
| SessionStart     | `session-restore`          | Restores context on compact/resume                                             |
| UserPromptSubmit | `memory-detector`          | Detects save/recall/todo patterns                                              |
| Stop             | `plan-sync`                | Syncs plans to tasks                                                           |

## Quick Commands

```bash
# Tasks
agentctl todo add --title "Task" --description "Details"
agentctl todo list
agentctl todo active
agentctl todo complete --id <id> --notes "Done"

# Skills
agentctl run <skill> --input '<json>'
agentctl skills list

# Workflows
agentctl workflow run pre-impl-analysis --input '{"path": "."}'
agentctl workflow list
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

| Variable                   | Default       | Description                                      |
| -------------------------- | ------------- | ------------------------------------------------ |
| `AGENTCTL_TASK_GUARD_MODE` | `auto`        | `auto` or `strict`                               |
| `AGENTCTL_HOME`            | `~/.agentctl` | Storage root                                     |
| `GEMINI_API_KEY`           | -             | For Gemini embeddings (3072 dimensions)          |
| `VOYAGE_API_KEY`           | -             | For Voyage embeddings/reranking (1024 dimensions)|
| `MISTRAL_API_KEY`          | -             | For Mistral/Codestral embeddings (1024 dimensions) |
| `AGENTCTL_SEMANTIC_RERANK` | `0`           | Set to `1` to enable Voyage rerank-2.5 in hooks  |

### Scope-Based Embedding Models

Voyage AI is the recommended provider for embeddings (based on Dec 2024 benchmarks):

| Scope      | Content Type    | Model            | Price/1M | Rationale                             |
| ---------- | --------------- | ---------------- | -------- | ------------------------------------- |
| `symbols`  | Code            | `voyage-code-3`  | $0.18    | 13.80% better than OpenAI on code     |
| `memory`   | Gotchas/notes   | `voyage-3-large` | $0.18    | Best text retrieval (nDCG@10: 0.837)  |
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

- Tasks: `~/.agentctl/storage/tasks.db`
- CAS: `~/.agentctl/cas/sha256/<digest>`
- Jobs: `~/.agentctl/jobs/<ulid>/`
- Observability: `~/.agentctl/observability/events/` (NDJSON event logs)
- Backups: `~/.agentctl/backups/` (includes observability component)

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
context=$(bin/agentctl run code/complexity --input '{"path": "."}')
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
| `memory-capture`     | PostToolUse (Edit/Write) | **Enabled by default** - set `AGENTCTL_MEMORY_CAPTURE=0` to disable |
| `file-memory-recall` | PreToolUse (Edit/Write)  | Surfaces stored memories before editing                             |

**memory-detector patterns:**

- **Save**: "remember this", "the trick is", "TIL", "watch out for", "please
  don't", "don't forget"
- **Recall**: "how did we", "didn't we already", "last time we"
- **Todo**: "we need to", "let's make sure", "TODO:", "before we"

**`/remember` skill** - Dual-save to both agentctl memory AND CLAUDE.md:

```bash
# 1. Saves to agentctl memory
bin/agentctl memory put --name "gotcha-<topic>" --type "gotcha" \
  --summary "<note>" --data '{"details": "..."}'

# 2. Appends to CLAUDE.md under Gotchas section
```

---

Full documentation: `docs/` | Skills: `.claude/skills/` | Workflows:
`.claude/workflows/`
