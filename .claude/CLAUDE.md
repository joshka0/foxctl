# agentctl Claude Code Integration

> Quick links: [README](../README.md) | [AGENTS.md](../AGENTS.md) |
> [Detailed docs](../docs/general/)

---

## Getting Started (New Repo Setup)

Initialize a new repository for full agentctl tooling support:

### 1. Environment Setup

Ensure API keys are configured in `~/.agentctl/.env`:

```bash
# Required for embeddings
VOYAGE_API_KEY=your_key_here

# Optional but recommended
ANTHROPIC_API_KEY=your_key      # Codemap generation
OPENROUTER_API_KEY=your_key     # Atomic fact processing, file/symbol summaries
```

### 2. Build Repo Graph Index

Creates a graph of code relationships (calls, references, imports):

```bash
# For Go + TypeScript projects
agentctl index repo build --workspace . --go --typescript

# Go only
agentctl index repo build --workspace . --go --typescript=false

# TypeScript only
agentctl index repo build --workspace . --go=false --typescript
```

### 3. Generate File & Symbol Summaries

LLM-powered summaries for better semantic search (requires OPENROUTER_API_KEY):

```bash
# File summaries - describes what each file does
agentctl index file-summaries --workspace .

# Symbol summaries - describes functions/types in repo graph
agentctl index symbol-summaries --workspace .

# Use --llm for richer symbol summaries (slower)
agentctl index symbol-summaries --workspace . --llm
```

### 4. Initialize Embeddings

Embed all knowledge scopes for vector search:

```bash
# All scopes at once (recommended for new repos)
agentctl index init --workspace .

# Specific scopes
agentctl index init --workspace . --scope symbols        # Code files
agentctl index init --workspace . --scope memory         # Gotchas/notes
agentctl index init --workspace . --scope tasks          # Task descriptions
agentctl index init --workspace . --scope sessions       # Session context

# For TypeScript-only repos, specify glob pattern
agentctl index init --workspace . --scope symbols --glob '**/*.{ts,tsx,js,jsx}'
```

### 5. Verify Setup

```bash
# Check all index status
agentctl index status --workspace .

# Check repo graph status
agentctl index repo status --workspace .

# Test semantic search
agentctl run code/semantic_search --input '{"format": "tree", "limit": 20}'

# Test repo graph search
agentctl index repo search --workspace . --query "authentication" --limit 10
```

### Quick Setup Script

For a new TypeScript repo:
```bash
# Build all indexes
agentctl index repo build --workspace . --go=false --typescript
agentctl index file-summaries --workspace .
agentctl index symbol-summaries --workspace .
agentctl index init --workspace . --scope symbols --glob '**/*.{ts,tsx}'
agentctl index init --workspace . --scope memory,tasks,sessions
```

### Available Tools After Setup

| Tool | Command | Purpose |
|------|---------|---------|
| **Semantic Search** | `agentctl run code/semantic_search --input '{"query": "..."}'` | Find code by concept |
| **Graph Search** | `agentctl index repo search --query "..."` | Find nodes by text |
| **Graph Expand** | `agentctl index repo expand --seed "<id>" --edge CALLS` | Traverse relationships |
| **Graph Ask** | `agentctl index repo ask --question "..."` | Natural language queries |
| **Codemap** | `agentctl codemap generate "trace flow"` | AI-traced relationships |

---

## Session Start / Post-Compaction

Get oriented with the codebase tree:

```bash
# Full repo overview (no query needed)
agentctl run code/semantic_search --input '{"format": "tree"}'

# Focused tree for your task
agentctl run code/semantic_search --input '{"query": "your task topic", "format": "tree", "limit": 30}'
```

For relationship navigation (calls/references/imports), build the repo graph:

```bash
agentctl index repo build --workspace . --go --typescript
```

Also read: `configs/USER_PREFS.md` and `configs/RECENT_GOTCHAS.md`

## Interaction Style

**Terminology coaching:** When the user asks something technical but uses imprecise or informal language, provide the correct terminology in parentheses as a mini-lesson. This helps build vocabulary over time.

Examples:
- User: "the menu cuts off" → Answer + *(in CSS: `overflow-y: auto` handles content overflow when it exceeds container height)*
- User: "make it not jump around" → Answer + *(layout shift — use fixed dimensions or `aspect-ratio` to reserve space)*
- User: "the thing that runs when you click" → Answer + *(event handler or onClick callback)*

Keep corrections brief and parenthetical — don't lecture, just annotate.

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

# Repo index
agentctl index repo build --workspace .
agentctl index repo search --workspace . --query "Supervisor" --limit 10
agentctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2
agentctl index repo ask --workspace . --question "Where is task guard implemented?"

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

Environment variables are loaded from `~/.agentctl/.env` (global). The loader checks:
1. `~/.agentctl/.env` (global defaults)
2. `$AGENTCTL_HOME/.env` (if set)
3. `$PWD/.env` (project overrides)

**Important:** The `.env` file must be a **real file**, not a symlink. Symlinks break in sandboxed/remote environments.

### Required

| Variable         | Purpose                       |
| ---------------- | ----------------------------- |
| `VOYAGE_API_KEY` | Vector embeddings (1024 dims) |

### Optional

| Variable                   | Default       | Purpose                              |
| -------------------------- | ------------- | ------------------------------------ |
| `AGENTCTL_HOME`            | `~/.agentctl` | Storage root                         |
| `ANTHROPIC_API_KEY`        | -             | Codemap generation                   |
| `OPENROUTER_API_KEY`       | -             | Atomic fact processing (SimpleMem)   |
| `TAVILY_API_KEY`           | -             | Web search (Tavily provider)         |
| `EXA_API_KEY`              | -             | Web search (Exa provider)            |
| `PERPLEXITY_API_KEY`       | -             | Web search (Perplexity provider)     |
| `AGENTCTL_SEMANTIC_RERANK` | `0`           | Enable reranking                     |
| `AGENTCTL_OBS_DIR`         | -             | Observability (use `$HOME`, not `~`) |

### Embedding Models (Voyage AI)

| Scope      | Model            | Use   |
| ---------- | ---------------- | ----- |
| `symbols`  | `voyage-code-3`  | Code  |
| `memory`   | `voyage-3-large` | Text  |
| `codemaps` | `voyage-3.5`     | Mixed |

```bash
make env-sync        # Manual: copy repo .env → ~/.agentctl/.env
make env-watch       # Auto: watch and sync on changes (requires fswatch)
make env-watch-stop  # Stop the watcher
```

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

### API Keys via Config (FC/IS Pattern)

Never use `os.Getenv` directly for API keys. Use Config struct fields instead:

```go
// Wrong - direct env access scattered throughout code
apiKey := os.Getenv("VOYAGE_API_KEY")

// Correct - use Config loaded once at startup
cfg, _ := config.Load(ctx)
apiKey := cfg.Embedding.VoyageAPIKey    // For Voyage
apiKey := cfg.Search.TavilyAPIKey       // For Tavily
apiKey := cfg.Search.ExaAPIKey          // For Exa
apiKey := cfg.LLM.OpenRouterAPIKey      // For OpenRouter (atomic processing)
apiKey := cfg.LLM.ResolveAPIKey("anthropic")  // For LLM providers
```

For skills, API keys are available via `RunContext.Config`:

```go
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
    // Access via rc.Config
    if rc.Config.Search.ExaAPIKey != "" {
        // use Exa
    }
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

### Mailbox Replies Must Not Be Auto-Acked

Replies are for the caller to consume; acking in daemon poller drops async replies.

### .env Must Be a Real File

`~/.agentctl/.env` must be a real file, not a symlink to the repo. Symlinks break in sandboxed/remote environments where the repo path doesn't exist.

### Logging via Wide Events (Not stderr)

Never use `fmt.Fprintf(stderr, ...)` or `log.Printf` for operational logging. Use observability wide events instead:

```go
// Wrong - loses structured data, not queryable
fmt.Fprintf(cmd.ErrOrStderr(), "processed: %d tokens ($%.6f)\n", tokens, cost)

// Correct - structured, queryable via obs/logs
event := observability.NewEvent("memory.atomic_processing").
    WithComponent(observability.ComponentCLI).
    WithData(obs.KeyLLMTotalTokens, tokens).
    WithData(obs.KeyLLMTotalCostUSD, cost)
observability.Emit(ctx, event.Success(duration))
```

For LLM token tracking, use the constants from `internal/adapters/skillslib/obs`:
- `obs.KeyLLMModel`, `obs.KeyLLMInputTokens`, `obs.KeyLLMOutputTokens`
- `obs.KeyLLMTotalTokens`, `obs.KeyLLMInputCostUSD`, `obs.KeyLLMTotalCostUSD`

View logs with: `agentctl run obs/logs --input '{}'`

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
