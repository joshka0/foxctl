# agentctl Claude Code Integration

> Quick links: [README](../README.md) | [AGENTS.md](../AGENTS.md) |
> [Detailed docs](../docs/general/)

---

## Session Start / Post-Compaction

Get oriented with the codebase tree:

```bash
# Full repo overview (no query needed)
agentctl run code/semantic_search --input '{"format": "tree"}'

# Focused tree for your task
agentctl run code/semantic_search --input '{"query": "your task topic", "format": "tree", "limit": 30}'
```

Also read: `configs/USER_PREFS.md` and `configs/RECENT_GOTCHAS.md`

## Architecture

```
Claude Code → Hooks → agentctl skills → SQLite/CAS → JSON envelope
```

---

## Command Patterns

agentctl has two command styles:

| Style | Pattern | When to Use |
|-------|---------|-------------|
| **Skill invocation** | `agentctl run <skill> --input '<json>'` | Full skill with JSON input/output |
| **Convenience commands** | `agentctl <noun> <verb> [flags]` | Quick CLI access to common operations |

**Examples:**
```bash
# Skill invocation (programmatic, JSON I/O)
agentctl run code/semantic_search --input '{"query": "auth", "limit": 10}'
agentctl run todo/manage --input '{"action": "list"}'

# Convenience commands (interactive, flags)
agentctl todo list -f table
agentctl memory search "auth"
agentctl ci status --pr 123
```

Many convenience commands wrap skills internally. **Prefer convenience commands** for interactive use; **prefer skill invocation** for scripting.

---

## Hooks

> **Canonical source:** [docs/general/hooks.md](../docs/general/hooks.md)

| Event            | Hook                       | Purpose                         |
| ---------------- | -------------------------- | ------------------------------- |
| PreToolUse       | `semantic-search`          | Vector search on Grep/Glob      |
| PreToolUse       | `file-memory-recall`       | Surface memories before editing |
| PreToolUse       | `overseer-inbox`           | Human-in-the-loop messages      |
| PostToolUse      | `read-context-suggestions` | Suggest context after reading   |
| PostToolUse      | `lsp-diagnostics`          | Show LSP errors after editing   |
| SessionStart     | `session-restore`          | Restore context on resume       |
| PreCompact       | `session-summarize`        | Extract learnings via LLM       |
| Stop             | `todo-continuation`        | Block stop if tasks remain      |
| UserPromptSubmit | `skill-advisor`            | Suggest skills based on prompt  |

### Human-in-the-Loop

```bash
agentctl-mail "Subject" "Message body"
agentctl-mail -p 1 "URGENT" "Stop and review"
```

---

## Slash Commands

| Command               | Purpose                         |
| --------------------- | ------------------------------- |
| `/anchor <goal>`      | Set persistent session goal     |
| `/todo`               | Enable todo check-in mode       |
| `/counsel <question>` | Multi-perspective code analysis |
| `/context <query>`    | Quick code context gathering    |

---

## Quick Commands

```bash
# Tasks
agentctl todo add --title "Task" --description "Details"
agentctl todo list -f table
agentctl todo complete --id <id>

# Memory
agentctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
agentctl memory search "query"

# Skills (run any skill)
agentctl run <skill> --input '<json>'
agentctl skills list

# Code search
agentctl run code/semantic_search --input '{"query": "auth", "limit": 10}'

# CI
agentctl ci status --pr 123
agentctl ci comments --pr 123

# Codemap
agentctl codemap generate "trace auth flow"

# Observability
agentctl run obs/logs --input '{}'
agentctl run obs/logs --input '{"errors_only": true, "since": "1h"}'
```

---

## Agent Orchestration

Spawn autonomous agents via the daemon.

```bash
# Simple research agent
agentctl agent spawn --role researcher --prompt "Find all hook implementations"

# With limits
agentctl agent spawn --role coder --prompt "Analyze storage" \
  --exec-mode autonomous --max-iterations 20

# Session management
agentctl agent list
agentctl agent kill <session-id>
```

### Key Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--role` | - | overseer, researcher, coder, planner, reviewer |
| `--exec-mode` | reactive | reactive, autonomous, proactive |
| `--max-iterations` | 10 | Max tool calls per turn |
| `--max-context-tokens` | 0 | Context budget (0=no limit) |
| `--max-auto-turns` | 1 | Max autonomous continuations |

**Full details:** [AGENTS.md](../AGENTS.md#agent-orchestration)

---

## Environment

Environment variables are loaded from `<repo-root>/.env` by the config loader (gitignored, shared across worktrees).

| Variable         | Required | Purpose               |
| ---------------- | -------- | --------------------- |
| `VOYAGE_API_KEY` | Yes      | Vector embeddings     |
| `ANTHROPIC_API_KEY` | No    | Codemap generation    |

**Full list:** [README.md](../README.md#environment-setup)

---

## Critical Gotchas

### CGO Build

```bash
make build-cgo  # Correct - includes -tags=libsqlite3
# Never: CGO_ENABLED=1 go build ./...  (duplicate SQLite symbols)
```

### Skill Binary Location

```bash
make skill SKILL=my_skill  # Correct - builds AND installs
# Binary must be at ~/.agentctl/skills/my/skill/bin
```

### Skills Must Load .env

```go
import "github.com/jkatigb/agentctl/internal/platform/config"
func main() {
    config.LoadDotEnv() // BEFORE os.Getenv()
}
```

### API Keys via Config

```go
// Wrong: os.Getenv("VOYAGE_API_KEY")
// Correct:
cfg, _ := config.Load(ctx)
apiKey := cfg.Embedding.VoyageAPIKey
```

### Memory Path

```go
// Correct - storage/
store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)

// Wrong - cache/
store, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
```

### Session Archives are Gzipped

```go
if strings.HasSuffix(path, ".gz") {
    gzReader, err := gzip.NewReader(file)
    // ...
}
```

### Mailbox Replies Must Not Be Auto-Acked

Replies are for the caller to consume; acking in daemon poller drops async replies.

**All gotchas:** [docs/general/gotchas.md](../docs/general/gotchas.md)

---

## Package Patterns

### Dependency Direction

```
skills/ → internal/adapters/skillslib/ → internal/platform/
```

- `internal/` must NOT import `skillslib`
- `skillslib` is skill-facing API only

### Workspace Detection

```go
import "github.com/jkatigb/agentctl/internal/platform/workspace"
ws := workspace.Detect("")  // Handles sandboxes correctly
```

### Code Context Funnel

```
semantic_search → snippet_extract → counsel
  "where"           "what"          "meaning"
```

| Skill                  | When to Use                               |
| ---------------------- | ----------------------------------------- |
| `code/semantic_search` | Find code by concept/meaning              |
| `code/smart_search`    | Don't know which files - search + extract |
| `code/snippet_extract` | Have file list - just extract snippets    |
| `code/context_ripgrep` | Regex pattern - need full function bodies |
| `codemap/generate`     | Need AI-traced code relationships         |

---

## Storage

| Path                              | Purpose            |
| --------------------------------- | ------------------ |
| `~/.agentctl/storage/memory.db`   | Memories, codemaps |
| `~/.agentctl/storage/tasks.db`    | Tasks              |
| `~/.agentctl/storage/sessions.db` | Sessions           |
| `~/.agentctl/cas/sha256/`         | Large artifacts    |

---

## Links

| Topic            | Document                                                                |
| ---------------- | ----------------------------------------------------------------------- |
| Architecture     | [docs/general/architecture.md](../docs/general/architecture.md)         |
| Skills           | [docs/general/skills.md](../docs/general/skills.md)                     |
| Hooks            | [docs/general/hooks.md](../docs/general/hooks.md)                       |
| Memory           | [docs/general/memory.md](../docs/general/memory.md)                     |
| Sessions         | [docs/general/sessions.md](../docs/general/sessions.md)                 |
| Storage          | [docs/general/storage.md](../docs/general/storage.md)                   |
| Gotchas          | [docs/general/gotchas.md](../docs/general/gotchas.md)                   |
| Companion Memory | [docs/general/companion-memory.md](../docs/general/companion-memory.md) |
| RLM Context      | [docs/general/rlm-context.md](../docs/general/rlm-context.md)           |
