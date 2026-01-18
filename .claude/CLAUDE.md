# agentctl Claude Code Integration

> Quick links: [README](../README.md) | [AGENTS.md](../AGENTS.md) | [Detailed docs](../docs/general/)
> Start-of-session: read `configs/USER_PREFS.md` and `configs/RECENT_GOTCHAS.md`

## Architecture

```
Claude Code → Hooks → agentctl skills → SQLite/CAS → JSON envelope
```

**Detailed:** [docs/general/architecture.md](../docs/general/architecture.md)

---

## Hooks

| Event | Hook | Purpose |
|-------|------|---------|
| PreToolUse | `semantic-search` | Vector search on Grep/Glob |
| PreToolUse | `file-memory-recall` | Surface memories before editing |
| PreToolUse | `overseer-inbox` | Human-in-the-loop messages |
| PostToolUse | `read-context-suggestions` | Suggest context after reading |
| PostToolUse | `lsp-diagnostics` | Show LSP errors after editing |
| SessionStart | `session-restore` | Restore context on resume |
| PreCompact | `session-summarize` | Extract learnings via LLM |
| Stop | `todo-continuation` | Block stop if tasks remain |
| UserPromptSubmit | `skill-advisor` | Suggest skills based on prompt |

**Detailed:** [docs/general/hooks.md](../docs/general/hooks.md)

### Human-in-the-Loop

```bash
agentctl-mail "Subject" "Message body"
agentctl-mail -p 1 "URGENT" "Stop and review"
```

---

## Slash Commands

| Command | Purpose |
|---------|---------|
| `/anchor <goal>` | Set persistent session goal |
| `/todo` | Enable todo check-in mode |
| `/counsel <question>` | Multi-perspective code analysis |
| `/context <query>` | Quick code context gathering |

---

## Quick Commands

```bash
# Tasks
agentctl todo add --title "Task" --description "Details"
agentctl todo list -f table
agentctl todo complete --id <id>

# Skills
agentctl run <skill> --input '<json>'
agentctl run <skill> -f table
agentctl skills list

# Memory
agentctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
agentctl memory search "query"
# Date-based search: "January gotchas", "recent decisions", "last week debugging"
# Activity search: "feature sessions", "bug-fix work", "refactoring"

# CI
agentctl ci status --pr 123
agentctl ci comments --pr 123

# Codemap
agentctl codemap generate "trace auth flow"
```

---

## Code Intelligence Tools

### Search & Extract

```bash
# Semantic search - find code by meaning
agentctl run code/semantic_search --input '{"query": "auth middleware", "limit": 10}'

# Timeline search - search sessions and see what happened (chunk summaries + learnings)
agentctl run code/semantic_search --input '{"query": "database issues", "scope": ["sessions"], "timeline": true}'

# Smart search - auto-find candidates + extract snippets (all-in-one)
agentctl run code/smart_search --input '{"query": "error handling patterns"}'

# Snippet extract - extract from known files
agentctl run code/snippet_extract --input '{
  "query": "validation logic",
  "candidates": [{"path": "internal/auth/validate.go", "line": 25}]
}'

# Context ripgrep - full function bodies matching pattern
agentctl run code/context_ripgrep --input '{"pattern": "func.*Auth", "path": "."}'
```

### Codemaps

```bash
# Generate new codemap (AI-powered)
agentctl run codemap/generate --input '{"query": "trace session lifecycle"}'

# Get existing codemap by ID
agentctl run codemap/get --input '{"id": "01KES88RGGVWG0T33WY7NH3AFR"}'

# List codemaps
agentctl run codemap/list --input '{"limit": 10}'

# Check if codemap is stale
agentctl run codemap/check --input '{"id": "01KES..."}'
```

### Analysis

```bash
# Complexity analysis
agentctl run code/complexity --input '{"path": "internal/auth/"}'

# Security scan
agentctl run code/security --input '{"path": "."}'

# Extract symbols
agentctl run code/symbols --input '{"path": "internal/auth/handler.go"}'

# Import analysis
agentctl run code/imports --input '{"path": ".", "mode": "graph"}'
```

### Pipeline Pattern

```
semantic_search → snippet_extract → counsel
   "where"           "what"          "meaning"
```

| Skill | When to Use |
|-------|-------------|
| `code/semantic_search` | Find code by concept/meaning |
| `code/smart_search` | Don't know which files - search + extract |
| `code/snippet_extract` | Have file list - just extract snippets |
| `code/context_ripgrep` | Regex pattern - need full function bodies |
| `codemap/generate` | Need AI-traced code relationships |
| `codemap/get` | Retrieve previously generated map |

**Detailed:** [docs/general/skills.md](../docs/general/skills.md)

---

## Environment

### Required

| Variable | Purpose |
|----------|---------|
| `VOYAGE_API_KEY` | Vector embeddings (1024 dims) |

### Optional

| Variable | Default | Purpose |
|----------|---------|---------|
| `AGENTCTL_HOME` | `~/.agentctl` | Storage root |
| `ANTHROPIC_API_KEY` | - | Codemap generation |
| `AGENTCTL_SEMANTIC_RERANK` | `0` | Enable reranking |
| `AGENTCTL_OBS_DIR` | - | Observability (use `$HOME`, not `~`) |

### Embedding Models (Voyage AI)

| Scope | Model | Use |
|-------|-------|-----|
| `symbols` | `voyage-code-3` | Code |
| `memory` | `voyage-3-large` | Text |
| `codemaps` | `voyage-3.5` | Mixed |

---

## Storage

| Path | Purpose |
|------|---------|
| `~/.agentctl/storage/memory.db` | Memories, codemaps |
| `~/.agentctl/storage/tasks.db` | Tasks |
| `~/.agentctl/storage/sessions.db` | Sessions |
| `~/.agentctl/cas/sha256/` | Large artifacts |

**Detailed:** [docs/general/storage.md](../docs/general/storage.md)

---

## Development

```bash
make build           # Pure Go
make build-cgo       # With CGO (Turso)
make skills-install  # Build + install skills
make test            # Run tests
```

---

## Critical Gotchas

### CGO Build
```bash
# Correct
make build-cgo

# Wrong - duplicate SQLite symbols
CGO_ENABLED=1 go build ./...
```

### Skill Binary Location
```bash
# Correct
go build -o ~/.agentctl/skills/my/skill/bin ./skills/my_skill

# Wrong - loader won't find it
go build -o ./my_skill ./skills/my_skill
```

### Building Skills After Edits
```bash
# Correct - use make target (builds AND installs)
make skill SKILL=session_restore

# Wrong - just builds, doesn't install properly
go build -o ~/.agentctl/skills/session_restore/bin ./skills/session_restore/
```

### Skills Must Load .env
```go
import "github.com/jkatigb/agentctl/internal/platform/config"

func main() {
    config.LoadDotEnv() // BEFORE os.Getenv()
}
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

---

## GUI/TUI

```bash
make ts-dev-gui  # Web GUI at http://localhost:5173
```

| Key | TUI View |
|-----|----------|
| 1 | Jobs |
| 2 | Tasks |
| 3 | Insights |
| 4 | Mailbox |
| 5 | Search |

---

## Links

| Topic | Document |
|-------|----------|
| Architecture | [docs/general/architecture.md](../docs/general/architecture.md) |
| Skills | [docs/general/skills.md](../docs/general/skills.md) |
| Hooks | [docs/general/hooks.md](../docs/general/hooks.md) |
| Memory | [docs/general/memory.md](../docs/general/memory.md) |
| Sessions | [docs/general/sessions.md](../docs/general/sessions.md) |
| Storage | [docs/general/storage.md](../docs/general/storage.md) |
| Gotchas | [docs/general/gotchas.md](../docs/general/gotchas.md) |
