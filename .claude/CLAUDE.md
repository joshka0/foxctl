# foxctl Claude Code Integration

> Quick links: [README](../README.md) | [AGENTS.md](../AGENTS.md) |
> [Detailed docs](../docs/general/)

---

## Getting Started (New Repo Setup)

Initialize a new repository for full foxctl tooling support:

### 1. Environment Setup

Ensure API keys are configured in `~/.foxctl/.env`:

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
foxctl index repo build --workspace . --go --typescript

# Go only
foxctl index repo build --workspace . --go --typescript=false

# TypeScript only
foxctl index repo build --workspace . --go=false --typescript
```

### 3. Generate File & Symbol Summaries

LLM-powered summaries for better semantic search (requires OPENROUTER_API_KEY):

```bash
# File summaries - describes what each file does
foxctl index file-summaries --workspace .

# Symbol summaries - describes functions/types in repo graph
foxctl index symbol-summaries --workspace .

# Use --llm for richer symbol summaries (slower)
foxctl index symbol-summaries --workspace . --llm
```

### 4. Initialize Embeddings

Embed all knowledge scopes for vector search:

```bash
# All scopes at once (recommended for new repos)
foxctl index init --workspace .

# Specific scopes
foxctl index init --workspace . --scope symbols        # Code files
foxctl index init --workspace . --scope memory         # Gotchas/notes
foxctl index init --workspace . --scope tasks          # Task descriptions
foxctl index init --workspace . --scope sessions       # Session context

# For TypeScript-only repos, specify glob pattern
foxctl index init --workspace . --scope symbols --glob '**/*.{ts,tsx,js,jsx}'
```

### 5. Verify Setup

```bash
# Check all index status
foxctl index status --workspace .

# Check repo graph status
foxctl index repo status --workspace .

# Test semantic search
foxctl run code/semantic_search --input '{"format": "tree", "limit": 20}'

# Test repo graph search
foxctl index repo search --workspace . --query "authentication" --limit 10
```

### Quick Setup Script

For a new TypeScript repo:
```bash
# Build all indexes
foxctl index repo build --workspace . --go=false --typescript
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .
foxctl index init --workspace . --scope symbols --glob '**/*.{ts,tsx}'
foxctl index init --workspace . --scope memory,tasks,sessions
```

### Available Tools After Setup

| Tool | Command | Purpose |
|------|---------|---------|
| **Semantic Search** | `foxctl run code/semantic_search --input '{"query": "..."}'` | Find code by concept |
| **Graph Search** | `foxctl index repo search --query "..."` | Find nodes by text |
| **Graph Expand** | `foxctl index repo expand --seed "<id>" --edge CALLS` | Traverse relationships |
| **Graph Ask** | `foxctl index repo ask --question "..."` | Natural language queries |
| **Codemap** | `foxctl codemap generate "trace flow"` | AI-traced relationships |

---

## Session Start / Post-Compaction

Get oriented with the codebase tree:

```bash
# Full repo overview (no query needed)
foxctl run code/semantic_search --input '{"format": "tree"}'

# Focused tree for your task
foxctl run code/semantic_search --input '{"query": "your task topic", "format": "tree", "limit": 30}'
```

If your change touches `internal/*` package placement, read
`docs/architecture/package-topology.md` before editing. Use that family map as
the placement rule, and do not treat `internal/v2/*` as the default destination
for new non-runtime code.

For relationship navigation (calls/references/imports), build the repo graph:

```bash
foxctl index repo build --workspace . --go --typescript
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
Claude Code → Hooks → foxctl skills → SQLite/CAS → JSON envelope
```

---

## Command Patterns

foxctl has two command styles:

| Style | Pattern | When to Use |
|-------|---------|-------------|
| **Skill invocation** | `foxctl run <skill> --input '<json>'` | Full skill with JSON input/output |
| **Convenience commands** | `foxctl <noun> <verb> [flags]` | Quick CLI access to common operations |

**Examples:**
```bash
# Skill invocation (programmatic, JSON I/O)
foxctl run code/semantic_search --input '{"query": "auth", "limit": 10}'
foxctl run todo/manage --input '{"action": "list"}'

# Convenience commands (interactive, flags)
foxctl todo list -f table
foxctl memory search "auth"
foxctl ci status --pr 123
```

## Structured Shell For Retrieval

For command-shaped, read-only repo inspection, prefer `foxctl shell` before reconstructing the same request with multiple raw tools. This is the compact structured shell path, not an arbitrary shell executor.

Use `foxctl shell` first for supported noisy inspection commands such as:
- `find`
- `rg`, `grep`
- `sed -n 'A,Bp' file`
- `git status --short`
- `git diff --stat`
- `git log --stat -N`

Prefer raw/native tools instead when the command is already compact or exact-value oriented, for example:
- `git diff --name-only`
- `wc`
- plain `head` / `tail`
- exact-value queries such as `kubectl get -o jsonpath`

If `foxctl shell` reports unsupported, `keep_raw`, or `raw_unavailable`, fall back immediately to the raw/native command. Before editing, reread the target with a raw file/context tool such as `fs_read_file` or `context_grep`.

Examples:
```bash
foxctl shell --command "rg -n 'spawn' internal/agent | head -n 10"
foxctl shell --command "sed -n '1,120p' cmd/foxctl/cmd/agent.go"
foxctl shell --command "git status --short"
foxctl shell --measure --command "git log --stat -5"
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
foxctl-mail "Subject" "Message body"
foxctl-mail -p 1 "URGENT" "Stop and review"
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
foxctl todo add --title "Task" --description "Details"
foxctl todo list -f table
foxctl todo complete --id <id>

# Memory
foxctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
foxctl memory search "query"

# Skills (run any skill)
foxctl run <skill> --input '<json>'
foxctl skills list

# Code search
foxctl run code/semantic_search --input '{"query": "auth", "limit": 10}'

# CI
foxctl ci status --pr 123
foxctl ci comments --pr 123

# Codemap
foxctl codemap generate "trace auth flow"

# Repo index
foxctl index repo build --workspace .
foxctl index repo search --workspace . --query "Supervisor" --limit 10
foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2
foxctl index repo ask --workspace . --question "Where is task guard implemented?"

# Observability
foxctl run obs/logs --input '{}'
foxctl run obs/logs --input '{"errors_only": true, "since": "1h"}'
```

---

## Agent Orchestration

Spawn persistent daemon agents that maintain conversation history across turns.

```bash
# Research agent — autonomous research, then queryable via ask
foxctl agent spawn --role researcher \
  --prompt "Research the hook system architecture" \
  --exec-mode autonomous_reactive \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20

# Query findings after autonomous phase completes
foxctl agent ask <id> --question "What did you find?" --wait --timeout 120s

# Management
foxctl agent list
foxctl agent info <id>
foxctl agent kill <id>
```

### Key Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--role` | - | overseer, researcher, coder, planner, reviewer |
| `--exec-mode` | reactive | reactive, autonomous, autonomous_reactive, proactive |
| `--llm-provider` | auto-detect | openrouter, cerebras, groq, openai |
| `--llm-model` | provider default | e.g., `openrouter/aurora-alpha` (free) |
| `--max-iterations` | 10 | Max tool calls per engine run |
| `--max-auto-turns` | 1 | Max autonomous continuations |
| `--max-context-tokens` | 0 | Context budget (0=no limit) |

### Exec Modes

| Mode | Behavior |
|------|----------|
| `reactive` | Wait for mailbox messages |
| `autonomous` | Run turns then exit |
| `autonomous_reactive` | Research autonomously, then stay alive for `agent ask` |
| `proactive` | Periodic think cycles + message polling |

**Full details:** [AGENTS.md](../AGENTS.md#agent-orchestration)

---

## Environment

Environment variables are loaded from `~/.foxctl/.env` (global). The loader checks:
1. `~/.foxctl/.env` (global defaults)
2. `$FOXCTL_HOME/.env` (if set)
3. `$PWD/.env` (project overrides)

**Important:** The `.env` file must be a **real file**, not a symlink. Symlinks break in sandboxed/remote environments.

### Required

| Variable         | Purpose                       |
| ---------------- | ----------------------------- |
| `VOYAGE_API_KEY` | Vector embeddings (1024 dims) |

### Optional

| Variable                   | Default       | Purpose                              |
| -------------------------- | ------------- | ------------------------------------ |
| `FOXCTL_HOME`            | `~/.foxctl` | Storage root                         |
| `ANTHROPIC_API_KEY`        | -             | Codemap generation                   |
| `OPENROUTER_API_KEY`       | -             | Atomic fact processing (SimpleMem)   |
| `TAVILY_API_KEY`           | -             | Web search (Tavily provider)         |
| `EXA_API_KEY`              | -             | Web search (Exa provider)            |
| `PERPLEXITY_API_KEY`       | -             | Web search (Perplexity provider)     |
| `FOXCTL_SEMANTIC_RERANK` | `0`           | Enable reranking                     |
| `FOXCTL_OBS_DIR`         | -             | Observability (use `$HOME`, not `~`) |

### Embedding Models (Voyage AI)

| Scope      | Model            | Use   |
| ---------- | ---------------- | ----- |
| `symbols`  | `voyage-code-3`  | Code  |
| `memory`   | `voyage-3-large` | Text  |
| `codemaps` | `voyage-3.5`     | Mixed |

```bash
make env-sync        # Manual: copy repo .env → ~/.foxctl/.env
make env-watch       # Auto: watch and sync on changes (requires fswatch)
make env-watch-stop  # Stop the watcher
```

| Variable         | Required | Purpose               |
| ---------------- | -------- | --------------------- |
| `VOYAGE_API_KEY` | Yes      | Vector embeddings     |
| `ANTHROPIC_API_KEY` | No    | Codemap generation    |

**Full list:** [README.md](../README.md#environment-setup)

---

## Local CI (agent-ci)

Run GitHub Actions CI locally against your working tree — no commit or push needed.

```bash
# Run the CI workflow locally
npx agent-ci run --quiet --workflow .github/workflows/ci.yml

# If a step fails, fix it then retry just the failed step
npx agent-ci retry --name <runner-name>

# Abort a paused runner
npx agent-ci abort --name <runner-name>
```

**Rules:**
- Use `npx agent-ci run --quiet --workflow .github/workflows/ci.yml` to run CI locally before pushing
- Do NOT push to trigger remote CI when agent-ci can run it locally — it's instant and free
- CI was green before you started. Any failure is caused by your changes — do not assume pre-existing failures
- When a step fails, the run pauses. Fix the issue and `npx agent-ci retry --name <runner>` to retry just that step
- The `label-ai` and `release` jobs will fail locally (require GitHub API) — that's expected and non-blocking
- `secrets.*` are not available locally. The LLM planner test already has a skip guard for missing `OPENROUTER_API_KEY`

**Note:** The repo uses both GitHub Actions (`.github/workflows/ci.yml`) and GitLab CI (`.gitlab-ci.yml`). They should have parity. If you add a check to one, add it to the other.

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
# Binary must be at ~/.foxctl/skills/my/skill/bin
```

### Skills Must Load .env

```go
import "github.com/joshka0/foxctl/internal/platform/config"
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

`~/.foxctl/.env` must be a real file, not a symlink to the repo. Symlinks break in sandboxed/remote environments where the repo path doesn't exist.

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

View logs with: `foxctl run obs/logs --input '{}'`

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
import "github.com/joshka0/foxctl/internal/platform/workspace"
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
| `~/.foxctl/storage/memory.db`   | Memories, codemaps |
| `~/.foxctl/storage/tasks.db`    | Tasks              |
| `~/.foxctl/storage/sessions.db` | Sessions           |
| `~/.foxctl/cas/sha256/`         | Large artifacts    |

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
