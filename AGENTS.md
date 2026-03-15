---
vault_refs:
  - notes/repo/agentctl/platform-and-web.md
  - notes/repo/agentctl/semantic-and-memory.md
  - notes/repo/agentctl/skills-runtime-wiring.md
  - notes/repo/agentctl/index.md
  - 00-home/index.md
---

# AGENTS.md — AI Assistant Guide

**Target Audience:** AI coding assistants (Claude, Cursor, Copilot) and human
contributors

---

## Quick Links

| Resource                                           | Purpose                                          |
| -------------------------------------------------- | ------------------------------------------------ |
| [README.md](README.md)                             | Overview, quick start                            |
| [docs/README.md](docs/README.md)                   | Canonical documentation map                      |
| [docs/DOC_LIFECYCLE.md](docs/DOC_LIFECYCLE.md)     | Documentation lifecycle policy                   |
| [docs/start/README.md](docs/start/README.md)       | Fast orientation guides                          |
| [.claude/CLAUDE.md](.claude/CLAUDE.md)             | Claude Code hooks, commands, environment         |
| [docs/general/](docs/general/)                     | Detailed documentation                           |
| [docs/architecture/](docs/architecture/)           | Current architecture overviews (runtime + storage + adapters) |
| [docs/general/gotchas.md](docs/general/gotchas.md) | Common pitfalls                                  |
| [docs/codemaps/](docs/codemaps/)                   | Generated codemap artifacts and notes            |
| [docs/kubernetes.md](docs/kubernetes.md)           | Kubernetes deployment guide                      |
| [deploy/kubernetes/](deploy/kubernetes/)           | Kubernetes manifests and overlays                |
| [docs/spec/agent_hierarchy.md](docs/spec/agent_hierarchy.md) | Subagent hierarchy and spawn protocol   |
| [docs/spec/overseer_profile.md](docs/spec/overseer_profile.md) | Overseer coordination profile |
| [docs/spec/v1/agent_profile_v1.md](docs/spec/v1/agent_profile_v1.md) | Multi-agent profile specification |
| [docs/architecture/chat-platform-adapter.md](docs/architecture/chat-platform-adapter.md) | Chat adapter runtime architecture (current) |
| [docs/architecture/kubernetes-runtime.md](docs/architecture/kubernetes-runtime.md) | Kubernetes runtime architecture (current) |
| [docs/architecture/postgres-storage.md](docs/architecture/postgres-storage.md) | PostgreSQL + CAS storage architecture |
| [docs/architecture/context-architecture.md](docs/architecture/context-architecture.md) | ACA dual-plane context + Obsidian knowledge layer |
| [docs/architecture/simulator-agents.md](docs/architecture/simulator-agents.md) | Simulator agent pattern for app/API integrations |
| [docs/plans/chat-platform-adapter.md](docs/plans/chat-platform-adapter.md) | Implementation plan + historical notes |
| [docs/plans/k8s-sql-storage.md](docs/plans/k8s-sql-storage.md) | Historical implementation plan (now partially complete) |

---

## Subagents & Multi-Agent Coordination

- Multi-agent execution uses the **Overseer model** with explicit spawn control:
  - The overseer (`actor:system:overseer`) owns plan changes and cross-agent coordination.
  - Non-overseer agents should request subagents via the `agent.spawn` workflow rather than creating sessions directly.
- `agent.spawn` is currently documented as:
  - Request-based (mail to overseer),
  - Depth-governed (`Depth`, `MaxDepth`, `LocalMaxDepth`),
  - Potentially denied when limits are exceeded.
- CLI agent daemons still support direct `agentctl agent spawn` for session creation; protocol-level behavior is now defined by the overseer/agent hierarchy specs.
- If you need to coordinate subagents, consult these first:
  - [docs/spec/agent_hierarchy.md](docs/spec/agent_hierarchy.md)
  - [docs/spec/overseer_profile.md](docs/spec/overseer_profile.md)
  - [docs/spec/v1/agent_profile_v1.md](docs/spec/v1/agent_profile_v1.md)
  - [docs/general/agent-daemon.md](docs/general/agent-daemon.md)

## TL;DR

1. **Start with a tree** — `agentctl run code/semantic_search --input '{"query": "your task", "format": "tree"}'`
2. **Build the repo graph** — `agentctl index repo build --workspace . --go --typescript --elixir` *(use `--go=false` for non-Go repos)* for call/ref navigation
3. **Envelope contract is sacred** — never change `meta.*` fields without spec
   updates (downstream tooling relies on stable envelope shape; breaking it breaks hooks, GUIs, and golden tests)
4. **WASI = `network:"none"`** — Core v1 mandates isolation
5. **Large outputs → CAS** — use `data.summary` + `data.artifact`
6. **Check gotchas first** — read
   [docs/general/gotchas.md](docs/general/gotchas.md)
7. **Task titles** — generate based on current work; do not require user-provided titles
8. **Native tools** — prefer agentctl skills, but if a skill is unavailable or makes completion harder, fall back to native tools
9. **Subagent-aware planning** — before requesting agent splits, verify current spawning rules in [docs/spec/agent_hierarchy.md](docs/spec/agent_hierarchy.md) (depth constraints, actor roles, rejection paths).
10. **Terminology coaching** — when the user asks something technical but uses imprecise language, provide the correct terminology in parentheses as a mini-lesson (e.g., "Fixed. Added scrolling *(in CSS terms: `overflow-y: auto` to handle content overflow)*")
11. **Docs link hygiene** — run `make check-doc-links` for markdown/doc updates; CI enforces this via `.github/workflows/docs.yml`
12. **Go-native runtime rules (v2)** — prefer `Run(ctx)` components, bounded channels, single-writer state ownership, and immutable snapshots for high-read paths
13. **ACA vault refresh** — after repo docs, repo graph, or bridge metadata changes, rebuild the Obsidian layer with `agentctl obsidian graph build`, `graph promote`, `bridge reconcile`, and `index build`
14. **Task continuity split** — use `agentctl context task-history-summary` for Codex/agents/scripts *(structured summary + artifact pointer)* and `configs/hooks/task-continuity-summary.sh` for hook injection *(prompt-ready wrapper output)*

## ACA / Obsidian Refresh

When repo docs or structure change, refresh the ACA knowledge layer *(in this system: graph rebuild + docs bridge reconcile + vault reindex)*:

```bash
agentctl obsidian graph build --workspace . --vault-path "/path/to/vault"
agentctl obsidian graph promote --workspace . --vault-path "/path/to/vault"
agentctl obsidian bridge reconcile --workspace . --vault-path "/path/to/vault"
agentctl obsidian index build --vault-path "/path/to/vault"
```

See [docs/architecture/context-architecture.md](docs/architecture/context-architecture.md) for the dual-plane model and the full command surface.

## Machine-Readable Decision Matrix

Use this table as the deterministic execution contract.

| Condition | Required action | Verification |
|-----------|------------------|--------------|
| Exploring unfamiliar code | Run semantic tree and (when needed) repo graph index before deep edits | `agentctl run code/semantic_search --input '{"format":"tree"}'` and relevant `agentctl index repo ...` command succeeds |
| Any state-changing command (DB writes, workspace edits, CAS writes, agent spawn, task edits) | Prefer feature-flagged rollout + idempotency keys + append-only writes for safety | Rollout can be scoped/rolled back and repeated requests are safe |
| Editing envelope/protocol behavior | Preserve `version/status/command/data/meta/error` shape; do not change `meta.*` without spec update | Existing envelope tests or golden files still pass |
| WASI skill manifest or runtime change | Keep `capabilities.network: "none"` | Manifest validation (`ValidateWASIPolicy`) continues to pass |
| Large output result (>64KB or blob-like) | Persist to CAS and return `data.summary` + `data.artifact` pointer | Output envelope contains artifact digest instead of large inline payload |
| Documentation changed (`*.md`) | Run doc link checker | `make check-doc-links` passes |
| User asks for terminology clarification | Include corrected term in parentheses with the fix | Response includes concise term mapping |
| Skill unavailable or unsuitable | Fall back to native tools and continue | Completion does not block on missing skill |

## Preflight Checklist (Before Final Output)

1. Confirm mutating operations are protected by feature flags, idempotency, or equivalent rollback-safe controls.
2. Confirm protocol/envelope invariants were not broken by changes.
3. Confirm WASI/network and path policy constraints still hold for modified skills.
4. Confirm docs links are valid when markdown changed (`make check-doc-links`).
5. Confirm summaries/references point to canonical docs under `docs/architecture/*` and `docs/general/*`.

## Agent Orchestration

`agentctl` supports persistent, multi-agent coordination via the daemon. Agents persist to `agents.db`, maintain conversation history across turns, and can be queried after autonomous research completes.

### Commands

| Command | Purpose |
|---------|---------|
| `agentctl agent spawn` | Create and start a new agent |
| `agentctl agent list` | List all agents |
| `agentctl agent info <id>` | Detailed agent status |
| `agentctl agent ask <id> --question "..." --wait` | Send a question and wait for reply |
| `agentctl agent kill <id>` | Stop an agent |
| `agentctl agent resume <session-id> --prompt "..."` | Continue a previous session |
| `agentctl agent hierarchy [session-id]` | Show agent tree |
| `agentctl agent watch <id>` | Live activity stream |

### Execution Modes

| Mode | Behavior |
|------|----------|
| `reactive` (default) | Waits for mailbox messages, responds to each |
| `autonomous` | Runs initial prompt + continuation turns, then exits |
| `proactive` | Runs autonomous turns and stays alive with periodic think cycles + message polling |

### Spawn Examples

```bash
# Research agent — autonomous turns + stays available for asks
agentctl agent spawn --role researcher \
  --prompt "Research the hook system architecture" \
  --exec-mode proactive \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20 --think-interval 60

# Wait for research, then query findings
agentctl agent ask <id> --question "What did you find?" --wait --timeout 120s

# Overseer — coordinates subagents
agentctl agent spawn --role overseer \
  --prompt "Coordinate a code review of the storage layer" \
  --exec-mode autonomous --max-auto-turns 5

# Simple reactive agent
agentctl agent spawn --role coder --prompt "Help with Go code"
```

### Spawn Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--role` | - | `overseer`, `researcher`, `coder`, `planner`, `reviewer` |
| `--exec-mode` | `reactive` | `reactive`, `autonomous`, `proactive` |
| `--llm-provider` | auto-detect | `openrouter`, `cerebras`, `groq`, `openai`, `anthropic` |
| `--llm-model` | provider default | Model name (e.g., `openrouter/aurora-alpha`) |
| `--max-iterations` | 10 | Max tool calls per engine run |
| `--max-auto-turns` | 1 | Max autonomous continuation turns |
| `--max-context-tokens` | 0 | Context budget (0=no limit) |

### Role-Specific Tools

| Role | Tools |
|------|-------|
| **All roles** | `fs_read_file`, `fs_list_dir`, `code_search`, `think` |
| **researcher** | + `context_search`, `smart_search`, `context_grep`, `repo_index_search`, `repo_index_expand`, `repo_index_open`, `repo_index_dag_grep`, `memory_query`, `session_recall`, `session_timeline` |
| **coder** | + `fs_write_file` |
| **overseer** | + `context_search`, `smart_search`, `context_grep`, `session_timeline`, `agent_spawn`, `agent_list`, `agent_status`, `agent_kill`, `agent_hierarchy`, `agent_wait` |

### Conversation Memory

Agents maintain conversation history across engine `Run()` calls. When a `proactive` agent:
1. Runs initial prompt → researches via tool calls
2. Runs autonomous continuation turns → finds more
3. Receives a mailbox `ask` → **responds with full context from prior research**

History is accumulated in-memory on the Session via `ConversationHistory []engine.Message`. Token budget (`MaxContextTokens`) prevents unbounded growth.

### Ask-Reply Pipeline

The `agent ask` command sends a `MessageTypeAsk` to the agent's mailbox. After the engine processes it, a `MessageTypeReply` is sent back with correlation headers. The caller polls its own namespace for the matching reply.

```
CLI (agent ask) → mailbox.Send(ask) → daemon polls → engine.Run(with history) → mailbox.Send(reply) → CLI polls → reply received
```

Execution-path note:
- Be explicit about whether a smoke test or bug report is exercising the classic `agent run` runtime path or the Jido-backed ask/dispatch path (`agent ask --dispatcher jido` plus the Jido socket/runtime).
- Do not treat findings from classic `agent run` as evidence about the Jido bridge unless the Jido dispatcher/runtime was actually used.
- When debugging nested tool execution, record which binary path invoked `agentctl` and which dispatcher/runtime path was active.

### LLM Provider Priority

Auto-detection order (first available key wins): openrouter → cerebras → groq → openai.

Default models: `openrouter/aurora-alpha` (free), context updater uses `qwen/qwen3-coder-next`.

### Inspecting Agent State and Replies

Use these commands to inspect progress and retrieve responses:

```bash
# View agent metadata/status
agentctl agent info <agent-id>

# Stream live events
agentctl agent watch <agent-id>

# Ask a follow-up and wait for the reply
agentctl agent ask <agent-id> --question "What did you find?" --wait
```

### Researcher Workflow

The hybrid researcher combines semantic search tools with file access tools for deep codebase investigation.

**Recommended spawn command:**

```bash
agentctl agent spawn --role researcher \
  --prompt "Research <topic>. Read the actual source files and include code snippets." \
  --exec-mode autonomous \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20
```

**Strategy (built into the researcher system prompt):**
1. DISCOVER — `context_search` or `smart_search` to find relevant files
2. READ — `fs_read_file` to read key source files (at least 3-5)
3. GREP — `code_search` and `context_grep` for exact patterns
4. DEEPEN — `memory_query` for past gotchas, `session_recall` for prior sessions
5. GRAPH — `repo_index_dag_grep` for call/reference relationships

**Model benchmarks** (companion memory pipeline research task):

| Model | Time | Output | Notes |
|-------|------|--------|-------|
| `openrouter/aurora-alpha` | ~20s | 12,890 chars | Best tradeoff — free, fast, deep |
| `minimax/minimax-m2.5` | ~120s | 8,761 chars | Slower and shallower than aurora |
| Claude Code reviewer (Opus) | ~173s | 24,901 chars | Deepest but ~$15/run |

Aurora-alpha with `--max-auto-turns 3 --max-iterations 20` produces ~52% of Opus depth at 0% cost and 10x speed.

### Specs & Docs

- [docs/spec/agent_hierarchy.md](docs/spec/agent_hierarchy.md)
- [docs/spec/overseer_profile.md](docs/spec/overseer_profile.md)
- [docs/spec/v1/agent_profile_v1.md](docs/spec/v1/agent_profile_v1.md)
- [docs/general/agent-daemon.md](docs/general/agent-daemon.md)

---

## Architecture Overview

```mermaid
flowchart LR
    subgraph Input
        CLI[agentctl CLI]
        HOOK[Claude Hooks]
    end

    subgraph Core
        SKILL[Skills]
        MEM[Memory]
        SESS[Sessions]
        CHAT[Chat Adapter Layer]
        COMPANION[Companion]
    end

    subgraph Storage
        DB[(sqlite/libsql/turso/postgres)]
        CAS[CAS]
        VEC[Vectors]
    end

    CLI --> SKILL
    HOOK --> SKILL
    SKILL --> MEM
    SKILL --> SESS
    SKILL --> CHAT
    SKILL --> COMPANION
    MEM --> DB
    MEM --> VEC
    SESS --> DB
    CHAT --> SVC[API/Websocket Services]
    COMPANION --> SVC
    SKILL --> CAS
```

**Detailed docs:** [docs/general/architecture.md](docs/general/architecture.md) and [docs/architecture/](docs/architecture/)

---

## Getting Oriented (Session Start / Post-Compaction)

Use the semantic tree to understand the codebase structure:

```bash
# Full repo tree with LLM-generated file summaries
agentctl run code/semantic_search --input '{"format": "tree"}'

# Focused tree for your task (recommended)
agentctl run code/semantic_search --input '{"query": "storage memory", "format": "tree", "limit": 25}'
```

**Output includes:**
- Hierarchical directory structure with relevance scores
- LLM-generated summaries for each file (via Devstral)
- Root summary synthesizing the codebase area

**Tree options:**
- `tree_depth: 1` — root level only
- `tree_depth: 2` — two levels (default)
- `tree_depth: 0` — unlimited depth (special case: 0 means "no limit")
- `tree_max_children: 50` — max items per directory
- `tree_include_summaries: false` — disable summaries for faster output

### Repo Graph Index

Use repoindex when you need relationships (calls, references, imports):

```bash
# Build the repo graph index.
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
agentctl index repo build --workspace . --go --typescript --elixir

agentctl index repo search --workspace . --query "repoindex" --limit 10
agentctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2
```

#### DAG grep (Explanation Subgraph)

Use `code/dag_grep` when you want a small explanation subgraph in one call (similar to `code/context_grep`, but for the repo graph index):

```bash
agentctl run code/dag_grep --input '{
  "query": "buildEvidencePack",
  "workspace": ".",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80,
  "k": 5
}'
```

Notes:
- TypeScript adds heuristic `CALLS` edges; Elixir adds heuristic `REFERS_TO` edges. These are best-effort (no type-checking) and conservative (ambiguous targets are skipped).
- If you run skills in a restricted filesystem sandbox, set writable paths (CAS + storage + observability), e.g. `AGENTCTL_STORAGE_ROOT=/tmp/agentctl/storage AGENTCTL_PATHS_CAS=/tmp/agentctl/cas AGENTCTL_OBS_DIR=/tmp/agentctl/observability`.

---

## Branching & PR Workflow

1. **Create branch** from `main`
2. **Open PR** — never push directly to `main`
3. **CI must pass** — lint, vet, tests, race
4. **Approval required** — at least one human
5. **Squash merge** to main

```bash
# Standard workflow
git checkout -b feat/my-feature
# ... make changes ...
make check  # fmt, lint, vet, test
git push -u origin feat/my-feature
gh pr create
```

---

## Key Conventions

### JSON Envelopes

All I/O uses canonical envelopes (Protocol v1):

```json
{
  "version": 1,
  "status": "ok|error|progress",
  "command": "skill/name",
  "data": { ... },
  "meta": { "ts": "2026-01-12T12:00:00Z" },
  "error": {}
}
```

- `meta.ts` **MUST** be present (RFC3339 UTC)
- `status:"ok"` → `error` fields empty
- `status:"error"` → `error.code` and `error.message` required
- `status:"progress"` → progress updates (non-terminal), terminal envelope still `ok` or `error`
- **stdout** = envelopes only, **stderr** = logs only

### Large Outputs

Use CAS for large results (threshold: **64KB** or large blobs):

```json
{
  "data": {
    "summary": "Brief description (≤2KB)",
    "artifact": "sha256:abc123..."
  }
}
```

### Security

- **WASI**: `network:"none"` (no exceptions)
- **Exec**: workspace-confined, rlimits enforced
- **Paths**: must go through `policy.PathValidator`
- **Secrets**: mount at `/run/secrets/<name>`, redact in logs

---

## Engineering Principles

These principles keep agentctl **safe, deterministic, and testable**.

| Principle             | Rule                                                         | Why                                             |
| --------------------- | ------------------------------------------------------------ | ----------------------------------------------- |
| **Functional Core**   | Business logic = pure functions (no IO, env, clock, globals) | Unit tests stay fast; behavior is deterministic |
| **Imperative Shell**  | IO + wiring in thin shell only                               | Isolates effects; skills can swap runtimes      |
| **Plan/Apply**        | State changes split into `Plan() → Apply()`                  | Enables previews and safer refactors            |
| **Explicit Deps**     | Inject clock/UUID/config as parameters                       | Reproducible tests; stable golden files         |
| **Boundary Parsing**  | Validate envelopes at edge; domain types inside              | No stringly-typed bugs in core                  |
| **Context Threading** | `context.Context` through all calls                          | Clean cancellation; timeout safety              |
| **Component Lifecycle** | Long-lived services expose `Run(ctx context.Context) error` | Predictable startup/shutdown, testable loops    |
| **Bounded Concurrency** | Async queues are bounded with explicit backpressure policy  | Prevents runaway memory and hidden deadlocks    |
| **Snapshot Reads** | Hot read paths use immutable snapshots (`atomic.Value`/`atomic.Pointer`) | Low contention and deterministic read behavior  |

### Preferred Shape

```go
// Core (functional) - pure, testable
func Plan(input DomainInput) (Plan, error) {
    // Business logic only - no IO, no time.Now(), no env reads
}

// Shell (imperative) - IO lives here
func Apply(ctx context.Context, deps Dependencies, plan Plan) (Result, error) {
    // Perform effects: DB writes, file ops, network calls
}
```

### Dependency Direction

```
skills/ → internal/adapters/skillslib/ → internal/platform/
              ↓ (never reverse)
        internal/domain/  ← pure, no IO imports
```

- Core/domain packages must NOT import `os`, `database/sql`, or adapter packages
- Envelope/transport types stay at boundaries, domain types inside

---

## Testing Requirements

```bash
make test        # Unit tests
make test-race   # Race detection
make lint        # golangci-lint
make check       # All of the above
```

- New features need unit + golden tests
- Coverage target: 85%
- Golden files in `testdata/*.json`
- **Determinism:** Golden tests must be reproducible
  - Sort keys/arrays in output
  - Inject timestamps via clock interface (no `time.Now()` in core)
  - Use stable IDs or inject UUID generator
- Prefer testing the **functional core** with table-driven tests (no IO)
- Use fakes/adapters for shell tests; keep integration tests focused

---

## Hard Fails (AUTO-REJECT)

| Pattern                                                         | Why                          |
| --------------------------------------------------------------- | ---------------------------- |
| Changing `meta.*` or envelope fields                            | Wire contract break          |
| `network:` not `"none"` in WASI manifest                        | Core v1 isolation            |
| Non-JSON stdout from CLI/skills                                 | Envelopes-only forever       |
| CGO build without `-tags=libsqlite3`                            | Duplicate SQLite symbols     |
| IO mixed into core logic (time.Now, env reads, DB in pure func) | Untestable; nondeterministic |

### Code Smells (Flag in Review)

These aren't auto-reject but should be called out:

| Smell                                                  | Why It Matters                            |
| ------------------------------------------------------ | ----------------------------------------- |
| `time.Now()`, `rand`, UUID generation in core logic    | Breaks determinism; use injected deps     |
| `map[string]any` deep in domain logic                  | Stringly-typed bugs; parse at boundary    |
| Core packages importing `os`, `database/sql`, adapters | Architecture violation; invert dependency |
| Non-deterministic output ordering                      | Flaky golden tests; sort before emit      |
| Unbounded goroutines / missing `ctx` cancellation      | Resource leaks; hang on shutdown          |
| Giant in-memory buffers when CAS exists                | OOM risk; stream to CAS                   |
| Unbounded channels or no backpressure policy           | Hidden queue growth and latency collapse  |
| Shared mutable maps with no single owner goroutine     | Races and non-deterministic behavior      |

---

## Code Intelligence Tools

Use these skills for code exploration, search, and analysis.

### Quick Reference

| Task                      | Skill                  | Example                                            |
| ------------------------- | ---------------------- | -------------------------------------------------- |
| Find code semantically    | `code/semantic_search` | `--input '{"query": "auth middleware"}'`           |
| Search + extract snippets | `code/smart_search`    | `--input '{"query": "error handling"}'`            |
| Extract from known files  | `code/snippet_extract` | `--input '{"query": "...", "candidates": [...]}'`  |
| Repo graph navigation     | repoindex (CLI)        | `agentctl index repo search --query "Supervisor"`  |
| Get stored codemap        | `codemap/get`          | `--input '{"id": "01KES..."}'`                     |
| Generate new codemap      | `codemap/generate`     | `--input '{"query": "trace auth flow"}'`           |
| Full function bodies      | `code/context_ripgrep` | `--input '{"pattern": "func.*Auth", "path": "."}'` |

### code/semantic_search — Vector Search

Find code by meaning, not just text matching.

```bash
# Basic search
agentctl run code/semantic_search --input '{"query": "database connection pooling"}'

# With scope filter (symbols, sessions, memory, codemaps)
agentctl run code/semantic_search --input '{"query": "auth", "scope": ["symbols"]}'

# Limit results
agentctl run code/semantic_search --input '{"query": "error handling", "limit": 5}'
```

**Output:** Ranked candidates with `path`, `symbol`, `line`, `score`.

### code/smart_search — End-to-End Search

Auto-generates candidates from indexes, then extracts relevant snippets. Use
when you don't have specific files.

```bash
# Find and extract code about a topic
agentctl run code/smart_search --input '{"query": "how does session restore work"}'

# With file pattern filter
agentctl run code/smart_search --input '{"query": "error types", "glob": "*.go"}'
```

**Output:** Extracted snippets with full context (function bodies, surrounding
code).

### code/snippet_extract — Extract from Known Files

Use when you already have candidate files (from semantic_search or manual list).

```bash
# Extract snippets from specific candidates
agentctl run code/snippet_extract --input '{
  "query": "authentication logic",
  "candidates": [
    {"path": "internal/auth/handler.go", "line": 45},
    {"path": "internal/auth/middleware.go", "line": 12}
  ]
}'

# With masked mode (structure only)
agentctl run code/snippet_extract --input '{
  "query": "API endpoints",
  "candidates": [{"path": "cmd/server/routes.go"}],
  "mode": "masked"
}'
```

**Modes:** `snippets` (default), `masked` (structure), `flow` (control flow).

### codemap/get — Retrieve Stored Codemap

Fetch a previously generated codemap by ID.

```bash
# Get full codemap with traces
agentctl run codemap/get --input '{"id": "01KES88RGGVWG0T33WY7NH3AFR"}'

# List available codemaps first
agentctl run codemap/list --input '{"limit": 10}'
```

**Output:** Full codemap with traces, mermaid diagrams, annotations, file
references.

### codemap/generate — AI-Powered Code Mapping

Generate semantic code traces using an AI agent.

```bash
# Generate codemap for a topic
agentctl run codemap/generate --input '{"query": "trace the authentication flow"}'

# With workspace scope
agentctl run codemap/generate --input '{
  "query": "how does session management work",
  "workspace": "/path/to/repo"
}'
```

**Output:** Structured codemap with multiple traces, each containing ASCII
trees, mermaid diagrams, and annotated code paths.

### code/context_ripgrep — Full Function Bodies

Search patterns and return complete surrounding blocks (functions, methods,
classes).

```bash
# Find functions matching pattern
agentctl run code/context_ripgrep --input '{"pattern": "func.*Handle", "path": "."}'

# With file type filter
agentctl run code/context_ripgrep --input '{
  "pattern": "class.*Service",
  "path": "src/",
  "glob": "*.py"
}'
```

**Output:** Full function/method bodies containing the match, not just the
matching line.

### Pipeline Pattern

Chain tools for comprehensive code understanding:

```
semantic_search → snippet_extract → counsel
   "where"           "what"          "meaning"
```

```bash
# 1. Find candidates
agentctl run code/semantic_search --input '{"query": "error handling"}' > candidates.json

# 2. Extract snippets
agentctl run code/snippet_extract --input "$(cat candidates.json | jq '{query: "error handling", candidates: .data.results}')"

# 3. Or use smart_search for steps 1+2 combined
agentctl run code/smart_search --input '{"query": "error handling"}'
```

---

## Common Tasks

### Adding a Skill

1. Create `skills/my_skill/` with `main.go` and `skill.yaml`
2. Implement with JSON envelope I/O
3. Add `config.LoadDotEnv()` if using API keys
4. Build:
   `CGO_ENABLED=0 go build -o ~/.agentctl/skills/my/skill/bin ./skills/my_skill`

**Detailed docs:** [docs/general/skills.md](docs/general/skills.md)

### Adding a Hook

1. Create script in `configs/hooks/`
2. Register in `.claude/settings.json`
3. Output context injection text (or nothing)

**Detailed docs:** [docs/general/hooks.md](docs/general/hooks.md)

### Working with Memory

```bash
agentctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
agentctl memory search "query"
```

**Detailed docs:** [docs/general/memory.md](docs/general/memory.md)

---

## Error Codes

```
EARG, ERUNTIME, EPOLICY, ENOTFOUND, ETIMEOUT, EPARSE,
EENVELOPE, EIO, ECANCELED, ECACHE_MISS
```

Always include `data.hint` to help users fix the problem.

---

## Quick Reference

```yaml
Project: agentctl
Language: Go 1.25+ (CGO off by default)
CLI: Cobra + Viper
I/O: JSON envelopes (version: 1)
  Storage: SQLite + CAS (~/.agentctl/)
  Runners: Exec (default), WASI (isolated)
  Lint: golangci-lint + gofumpt
  Tests: go test ./... -race
```

---

## Detailed Documentation

| Topic        | Document                                                     |
| ------------ | ------------------------------------------------------------ |
| Architecture | [docs/general/architecture.md](docs/general/architecture.md) |
| Skills       | [docs/general/skills.md](docs/general/skills.md)             |
| Hooks        | [docs/general/hooks.md](docs/general/hooks.md)               |
| Memory       | [docs/general/memory.md](docs/general/memory.md)             |
| Sessions     | [docs/general/sessions.md](docs/general/sessions.md)         |
| Storage      | [docs/general/storage.md](docs/general/storage.md)           |
| Gotchas      | [docs/general/gotchas.md](docs/general/gotchas.md)           |
| Multi-Agent  | [docs/spec/v1/agent_profile_v1.md](docs/spec/v1/agent_profile_v1.md) |
| Deployment   | [docs/kubernetes.md](docs/kubernetes.md)                    |
| Deployment Manifests | [deploy/kubernetes/](deploy/kubernetes/)              |
| Chat Adapter | [docs/architecture/chat-platform-adapter.md](docs/architecture/chat-platform-adapter.md) |
| Postgres     | [docs/architecture/postgres-storage.md](docs/architecture/postgres-storage.md) |

---

**You are here to help us ship a safe, deterministic CLI.** Prefer small,
well-tested changes; when in doubt, ask for a human decision.
