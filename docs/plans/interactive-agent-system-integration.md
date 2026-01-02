# Interactive Agent System Integration Plan

> **Status**: Draft
> **Created**: 2026-01-02
> **Goal**: Unify OpenTUI, DSPy agents, orchestration, and progressive memory into an interactive agent platform

## Executive Summary

agentctl has a rich foundation with **87 skills**, a **reactive actor system**, **DSPy-go agents**, **progressive memory with embeddings**, and an **OpenTUI viewer**. This plan unifies these into an **interactive agent orchestration platform** - a "Codex/Claude Code" style tool that's fully observable, trainable, and user-driven.

---

## Research Summary

This section captures findings from deep exploration of six system areas.

### 1. OpenTUI Architecture

**Location**: `packages/tui/`, `packages/data/`, `packages/gui/server/`

| Component | Technology | Purpose |
|-----------|------------|---------|
| TUI | React 19 + @opentui/core | Terminal rendering at 30 FPS |
| Data Client | TypeScript + fetch | API abstraction (`@agentctl/data`) |
| API Server | Express.js (port 8090) | Backend aggregation layer |

**Current Views (9 total)**:
- Jobs, Tasks, Insights, Mailbox, Reservations, Stats, Blackboard, SQLite, Search
- Navigation: Number keys (1-9), `j/k` for cursor, `r` refresh, `q` quit

**Key Files**:
- `packages/tui/src/App.tsx` - View orchestrator with keyboard handling
- `packages/tui/src/views/*.tsx` - Individual view components
- `packages/tui/src/hooks/useData.ts` - Data fetching hooks (useJobs, useTasks, etc.)
- `packages/data/src/client.ts` - 40+ API functions
- `packages/gui/server/index.js` - Express server (1000+ LOC, 38 endpoints)

**Extension Pattern**:
1. Create view component in `src/views/`
2. Export from `src/views/index.ts`
3. Add to `App.tsx` (header, keyboard, render switch)
4. Add hook in `useData.ts`
5. Add client function in `@agentctl/data`

**Real-time**: SSE via `subscribeToEvents()` - currently placeholder, needs full implementation.

---

### 2. DSPy Integration

**Location**: `internal/actor/dspy_actor.go`, `internal/storage/trajectory/`, `skills/session_export_dspy/`

**Architecture**:
```
DspyActor (wraps dspy-go ReActAgent)
    ↓
Trajectory Capture (episodes + outcomes)
    ↓
Session Export (JSONL/JSON/CSV)
    ↓
Learnable Scorer (weight adaptation)
```

**Key Components**:

| Component | File | Purpose |
|-----------|------|---------|
| DspyActor | `internal/actor/dspy_actor.go` (592 lines) | LLM-driven agent runtime |
| Trajectory Types | `internal/storage/trajectory/types.go` (347 lines) | Episode schema |
| Feedback Collector | `internal/agent/optimization/feedback.go` (174 lines) | Human ratings |
| Learnable Scorer | `internal/agent/optimization/learnable_scorer.go` (453 lines) | Weight learning |
| Session Export | `skills/session_export_dspy/main.go` (319 lines) | DSPy format export |

**DSPy Example Format**:
```json
{
  "input": { "user_request": "...", "context": "...", "files": [...] },
  "output": { "response": "...", "tools_used": [...], "files_edited": [...] },
  "metadata": { "session_id": "...", "turn_index": 0, "has_error": false }
}
```

**Optimization Skills (7 total)**:
- `optimize/feedback`, `optimize/patterns`, `optimize/reflect`
- `optimize/bootstrap`, `optimize/weights`, `optimize/analyze`, `optimize/from-feedback`

**Scorer Weights (learnable)**:
- CriticalPath: 0.30, PageRank: 0.20, AdminMail: 0.25, OverseerMail: 0.15, Recency: 0.10

---

### 3. Actor/Orchestration System

**Location**: `internal/actor/`

**Core Actors**:

| Actor | File | Purpose |
|-------|------|---------|
| Supervisor | `supervisor.go` | Lifecycle management, restart with backoff |
| Watcher | `watcher.go` | 50ms reactive notifications via SQLite triggers |
| EventBus | `event_bus.go` | Pub/sub with selective persistence |
| DspyActor | `dspy_actor.go` | LLM-driven agents (coder, planner, reviewer) |
| BaseActor | `base_actor.go` | Common handler registration, timer management |

**Message Types**:
- `agent.ask` - Agent requests decision
- `agent.reply` - Response to ask
- `agent.result` - Task completion
- `agent.error` - Error with recovery
- `overseer.task` - Assignment from overseer
- `overseer.decision` - Decision to agent

**Spawn Protocol** (`internal/agent/tools/spawn_tools.go`):
- Depth-constrained hierarchy (global + local MaxDepth)
- `spawn.request` → overseer evaluation → `spawn.response`
- Parent tracking via `Agent.ParentID`

**Key Specs**:
- `docs/spec/agent_hierarchy.md` - Spawn protocol
- `docs/spec/overseer_profile.md` - Overseer responsibilities
- `docs/designs/reactive-actor-system.md` - Event-driven design

---

### 4. Memory & Embedding Architecture

**Location**: `internal/storage/memory/`, `internal/indexing/`

**3-Tier Progressive Memory**:
```
Tier 1: Embeddings (50ms)     → Fast vector lookup
Tier 2: Summaries (200ms)     → Chunk-level search
Tier 3: Full Conversations    → On-demand JSONL decompression
```

**Embedding Providers**:

| Provider | Models | Dimensions | Use Case |
|----------|--------|------------|----------|
| Voyage AI | voyage-code-3, voyage-3.5 | 1024 | Code (symbols), Text (memory) |
| Gemini | text-embedding-004, gemini-embedding-001 | 768-3072 | Alternative |
| Codestral | Mistral code model | 1024 | Code alternative |

**Scope-Based Model Selection**:
- `symbols` → voyage-code-3 (13.8% better on code)
- `memory`, `sessions`, `tasks`, `codemaps` → voyage-3.5 (cost-effective)

**Storage Backends**:
- SQLite (local): `~/.agentctl/storage/memory.db`
- Turso (remote): Native F32_BLOB vectors, `vector_top_k()` for fast search

**Key Files**:
- `internal/storage/memory/store.go` - SQLite MemoryStore
- `internal/storage/memory/turso_store.go` - Turso with vector search
- `internal/indexing/embedding/worker.go` - Background embedding processor
- `internal/indexing/semantic/provider_voyage.go` - Voyage API client

**Embedding Pipeline**:
```
File Change → Hook → Enqueue Job → Worker Claims → Provider API → Store Vector
```

---

### 5. Skill Ecosystem

**Location**: `skills/`, `internal/domain/skill/`, `internal/execution/`

**Scale**: 87 skills across 25+ categories

**Categories**:
- **Code**: symbols, complexity, semantic_search, swe_grep, smart_write, context_ripgrep
- **Filesystem**: read, write, find, tree, apply_edit
- **Session**: capture, recall, export_dspy, summarize, feedback
- **Optimize**: feedback, patterns, reflect, bootstrap, weights
- **Mobile**: ios, android, expo
- **LSP**: gopls, pylsp, tsserver
- **MCP**: bridge (external tool integration)

**Execution Models**:

| Type | Runtime | Network | Use Case |
|------|---------|---------|----------|
| EXEC | Native binary | Configurable | Performance-critical |
| WASI | wazero (WebAssembly) | None (sandboxed) | Security-isolated |

**Workflow Engine** (`internal/workflow/`):
- DAG-based scheduling
- Parallel execution within batches
- Template expressions for data flow: `{{.stepID.data.field}}`
- Error handling: fail, continue, retry

**Manifest Structure**:
```yaml
apiVersion: agentctl/v1
kind: Skill
metadata:
  name: code/symbols
distribution:
  type: exec
signature:
  command: code/symbols
  parameters: [...]
capabilities:
  network: "none"
  filesystem: [workdir]
```

---

### 6. Daemon & API Server

**Daemon** (`internal/daemon/`):
- Unix socket: `/tmp/agentctl-{uid}.sock`
- Pre-loaded SQLite pool for sub-50ms hook latency
- JSON-RPC-like protocol: `status`, `run`, `warm`, `shutdown`
- CLI: `agentctl daemon start|stop|status`

**Express API Server** (`packages/gui/server/index.js`):
- Port 8090, CORS + cookie parsing
- Read-only SQLite access (better-sqlite3)
- 38 endpoints across jobs, tasks, stats, sessions, search, sqlite
- Workspace isolation via cookies (`agentctl_workspace`)

**Data Flow**:
```
CLI/Hooks → Daemon (Unix Socket) → Storage (SQLite)
                                       ↑
TUI/GUI → Express API (HTTP:8090) ─────┘
```

**Authentication**: Cookie-based workspace selection (no JWT/OAuth)

---

### Key Integration Opportunities

Based on research, the highest-impact integrations are:

1. **SSE Event Stream**: Wire actor events (agent.thinking, tool_call, ask) to Express → TUI
2. **Interactive Session Mode**: New `agentctl session start --interactive` with TUI bridge
3. **Memory Panel**: Surface `code/semantic_search` results in dedicated view
4. **Feedback Widget**: Inline 1-5 rating → `trajectory.outcome.human_rating`
5. **Agent Hierarchy View**: Visualize spawned agents with mailbox activity

---

## Current State vs Target State

| Current State | Target State |
|---------------|--------------|
| CLI-driven (`agentctl run`) | Interactive TUI agent session |
| Passive viewers (jobs/tasks) | Real-time agent coordination |
| Batch DSPy export | Live trajectory capture + feedback |
| Memory hooks (async) | Progressive context surfacing |
| Separate overseer | Visual overseer with human-in-loop |

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         USER INTERFACE LAYER                         │
├─────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────────┐  │
│  │ OpenTUI     │  │ Web GUI     │  │ CLI (agentctl)              │  │
│  │ (Terminal)  │  │ (Browser)   │  │ • session start --interactive│
│  │ • AgentView │  │ • AgentView │  │ • run --watch               │  │
│  │ • MemoryView│  │ • MemoryView│  │ • feedback                  │  │
│  │ • Orchestr. │  │ • Orchestr. │  │                             │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────────┬──────────────┘  │
│         │                │                        │                  │
│         └────────────────┼────────────────────────┘                  │
│                          ▼                                           │
├─────────────────────────────────────────────────────────────────────┤
│                       API & EVENT LAYER                              │
├─────────────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ Express API Server (port 8090)                               │   │
│  │ ├── /api/agents/* (spawn, list, kill, status)               │   │
│  │ ├── /api/sessions/* (interactive session control)           │   │
│  │ ├── /api/memory/* (recall, search, pin)                     │   │
│  │ ├── /api/trajectory/* (capture, rate, feedback)             │   │
│  │ └── /api/events (SSE stream)                                │   │
│  │      • agent.thinking, agent.tool_call, agent.ask           │   │
│  │      • memory.recalled, feedback.collected                  │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                          │                                           │
├──────────────────────────┼──────────────────────────────────────────┤
│                    CORE AGENT LAYER                                  │
├──────────────────────────┼──────────────────────────────────────────┤
│                          ▼                                           │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │ Actor System (internal/actor/)                              │    │
│  │ ├── Supervisor (lifecycle, restart, backoff)               │    │
│  │ ├── Watcher (50ms reactive notifications)                  │    │
│  │ ├── EventBus (pub/sub with selective persistence)          │    │
│  │ └── Actors:                                                │    │
│  │     ├── OverseerActor (planning, coordination)             │    │
│  │     ├── DspyActor (LLM-driven via dspy-go)                │    │
│  │     │   ├── CoderHandler                                   │    │
│  │     │   ├── PlannerHandler                                 │    │
│  │     │   └── ReviewerHandler                                │    │
│  │     └── UserProxyActor (NEW: bridges TUI input)           │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                          │                                           │
├──────────────────────────┼──────────────────────────────────────────┤
│                    MEMORY & LEARNING LAYER                           │
├──────────────────────────┼──────────────────────────────────────────┤
│                          ▼                                           │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │ Progressive Memory System                                      │ │
│  │ ┌────────────┐  ┌────────────┐  ┌──────────────────────────┐  │ │
│  │ │ Tier 1:    │  │ Tier 2:    │  │ Tier 3:                  │  │ │
│  │ │ Embeddings │→ │ Summaries  │→ │ Full Conversations       │  │ │
│  │ │ (50ms)     │  │ (200ms)    │  │ (on-demand JSONL)        │  │ │
│  │ └────────────┘  └────────────┘  └──────────────────────────┘  │ │
│  │                                                                │ │
│  │ Scopes: symbols | memory | sessions | tasks | codemaps        │ │
│  │ Providers: Voyage (code-3, 3.5) | Gemini | Codestral          │ │
│  │ Storage: SQLite (local) | Turso (remote vector search)        │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                          │                                           │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │ DSPy Training Pipeline                                         │ │
│  │ ┌──────────┐   ┌──────────────┐   ┌─────────────────────────┐ │ │
│  │ │Trajectory│ → │ Feedback     │ → │ Learnable Scorer        │ │ │
│  │ │ Capture  │   │ Collection   │   │ • CriticalPath: 0.30    │ │ │
│  │ │          │   │ (1-5 stars)  │   │ • PageRank: 0.20        │ │ │
│  │ │ Events:  │   │              │   │ • AdminMail: 0.25       │ │ │
│  │ │ • tool   │   │ Skills:      │   │ • OverseerMail: 0.15    │ │ │
│  │ │ • thought│   │ • optimize/* │   │ • Recency: 0.10         │ │ │
│  │ │ • result │   │ • session/*  │   │                         │ │ │
│  │ └──────────┘   └──────────────┘   └─────────────────────────┘ │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│                         SKILL EXECUTION LAYER                        │
├──────────────────────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │ 87 Skills across 25+ categories                                │ │
│  │ ├── code/* (symbols, complexity, semantic_search, swe_grep)   │ │
│  │ ├── fs/* (read, write, find, tree)                            │ │
│  │ ├── session/* (capture, recall, export-dspy)                  │ │
│  │ ├── optimize/* (feedback, patterns, weights)                  │ │
│  │ ├── mobile/* (ios, android, expo)                             │ │
│  │ ├── lsp/* (gopls, pylsp, tsserver)                            │ │
│  │ ├── mcp/* (bridge to external tools)                          │ │
│  │ └── ... (git, ci, test, embedding, codemap, todo, hooks)      │ │
│  │                                                                │ │
│  │ Execution: EXEC (native binary) | WASI (WebAssembly)          │ │
│  │ Workflow: DAG scheduler with parallel execution                │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│                         STORAGE LAYER                                │
├──────────────────────────────────────────────────────────────────────┤
│  ~/.agentctl/                                                        │
│  ├── storage/                                                        │
│  │   ├── tasks.db (PageRank, dependencies)                          │
│  │   ├── sessions.db (embeddings, summaries)                        │
│  │   ├── memory.db (named entries, codemaps)                        │
│  │   ├── agents.db (hierarchy, state)                               │
│  │   ├── trajectory.db (episodes, outcomes, feedback)               │
│  │   └── graph.db (unified dependency graph)                        │
│  ├── cas/ (content-addressable storage)                             │
│  ├── jobs/ (execution records)                                      │
│  └── observability/ (wide events, tracing)                          │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Key Integration Points

### 1. OpenTUI → Interactive Agent Session

**Current**: 9 passive read-only views (Jobs, Tasks, etc.)

**Target**: Add interactive agent views

```
┌─────────────────────────────────────────────────────────────┐
│ [1]Jobs [2]Tasks [3]Insights [4]Mailbox [5]Agent [6]Memory  │
├─────────────────────────────────────────────────────────────┤
│ AGENT SESSION: coder-01JFXYZ                                │
│ ┌─────────────────────────────┬───────────────────────────┐ │
│ │ Current Task                │ Agent Thinking            │ │
│ │ ─────────────               │ ──────────────            │ │
│ │ Fix JWT refresh race        │ > Searching for token...  │ │
│ │ condition in auth.ts        │ > Found 3 relevant files  │ │
│ │                             │ > Analyzing patterns...   │ │
│ │ Dependencies:               │                           │ │
│ │ ├─ auth/middleware.ts       │ [Tools Used]              │ │
│ │ └─ lib/jwt.ts               │ • code/swe_grep (0.8s)    │ │
│ │                             │ • fs/read (0.1s)          │ │
│ │ PageRank: 0.85              │                           │ │
│ │ Critical Path: YES          │ [Memory Recalled]         │ │
│ │                             │ • JWT gotcha from session │ │
│ │                             │   abc123: "Use mutex"     │ │
│ └─────────────────────────────┴───────────────────────────┘ │
│ [i]nput [p]ause [r]oll-back [f]eedback [s]pawn-subagent    │
└─────────────────────────────────────────────────────────────┘
```

**New Views**:
- **AgentView** (key: `0` or `a`) - Interactive agent session
- **MemoryView** - Progressive context panel
- **OrchestrationView** - Agent hierarchy visualization

### 2. DSPy Integration → Live Learning Loop

**Current**: Export sessions post-hoc via `session/export-dspy`

**Target**: Real-time trajectory capture with inline feedback

```
User Prompt → Agent Action → [Feedback UI] → Trajectory DB
                    ↓
               Rating: ⭐⭐⭐⭐☆
               Note: "Good approach but missed edge case"
                    ↓
             Stored in trajectory.outcome
                    ↓
             optimize/feedback analyzes patterns
                    ↓
             LearnableScorer updates weights
```

**TUI Integration**:
- After each agent action, prompt for optional quick rating (1-5 keys)
- `f` key opens feedback modal
- Ratings flow directly to `trajectory.outcome.human_rating`
- Background worker runs `optimize/from-feedback` periodically

### 3. Subagent Orchestration → Visual Hierarchy

**Current**: Overseer coordinates via mailbox (invisible to user)

**Target**: Orchestration dashboard with human-in-loop

```
┌─────────────────────────────────────────────────────────────┐
│ AGENT HIERARCHY                              [PageRank ▼]   │
├─────────────────────────────────────────────────────────────┤
│ 🎯 overseer (running)                                       │
│ ├── 💻 coder-01 (working on: Fix JWT race condition)       │
│ │   └── 🔍 reviewer-01 (reviewing: auth.ts changes)        │
│ ├── 📋 planner-01 (idle - waiting for coder-01)            │
│ └── 🧪 tester-01 (queued - 3 tasks pending)                │
│                                                             │
│ MAILBOX ACTIVITY                                            │
│ ─────────────────                                           │
│ [12:34:56] coder-01 → overseer: agent.ask "Should I..."    │
│ [12:34:55] overseer → coder-01: overseer.task "Fix JWT..." │
│ [12:34:50] reviewer-01 → overseer: agent.result "LGTM"     │
├─────────────────────────────────────────────────────────────┤
│ [s]pawn agent  [k]ill agent  [m]essage  [e]scalate to user │
└─────────────────────────────────────────────────────────────┘
```

**Key Features**:
- Watch agent hierarchy in real-time (via watcher polling)
- Intercept `agent.ask` messages before overseer auto-responds
- Human can answer questions directly or let overseer handle
- Spawn/kill agents with visual confirmation

### 4. Progressive Memory → Context Panel

**Current**: Memory surfaces via hooks (invisible to user)

**Target**: Live memory panel showing recalled context

```
┌───────────────────────────────────────────────────────────┐
│ ACTIVE CONTEXT                    [refresh] [search]      │
├───────────────────────────────────────────────────────────┤
│ 📍 SESSION MEMORY (3 recalled)                            │
│ ├── "JWT race condition" (similarity: 0.92)               │
│ │   └── Session abc123: Used mutex, see auth/lock.ts:45   │
│ ├── "Token refresh pattern" (similarity: 0.87)            │
│ │   └── Session def456: Debounce approach worked better   │
│ └── "Authentication gotcha" (similarity: 0.81)            │
│     └── Memory: "Always validate exp before refresh"      │
│                                                           │
│ 🔍 RELEVANT SYMBOLS (via code/semantic_search)            │
│ ├── refreshToken (auth/jwt.ts:123) - similarity: 0.95     │
│ ├── TokenManager (lib/auth.ts:45) - similarity: 0.89      │
│ └── useAuth (hooks/auth.ts:12) - similarity: 0.85         │
│                                                           │
│ 💡 GOTCHAS (from memory store)                            │
│ └── "Concurrent map writes in token cache - use sync.Map" │
├───────────────────────────────────────────────────────────┤
│ [a]dd memory  [p]in  [d]ismiss  [/]search                 │
└───────────────────────────────────────────────────────────┘
```

---

## Implementation Roadmap

### Phase 1: Foundation (TUI Extensions)

| Task | Effort | Impact | Files |
|------|--------|--------|-------|
| Add AgentView to TUI | Medium | High | `packages/tui/src/views/AgentView.tsx` (new) |
| Add MemoryView to TUI | Low | Medium | `packages/tui/src/views/MemoryView.tsx` (new) |
| SSE real-time updates | Low | High | `packages/gui/server/index.js` |
| Keyboard shortcuts for agent control | Low | High | `packages/tui/src/App.tsx` |
| Add agent/memory API endpoints | Medium | High | `packages/data/src/client.ts` |

### Phase 2: Agent Session (Interactive Mode)

| Task | Effort | Impact |
|------|--------|--------|
| `agentctl session start --interactive` | Medium | High |
| TUI <-> DspyActor bridge | High | Critical |
| Human-in-loop for `agent.ask` | Medium | High |
| Inline feedback collection | Low | High |

**New components**:
- Interactive session mode in daemon
- WebSocket or SSE agent event stream
- Feedback capture UI widget
- UserProxyActor for bridging TUI input

### Phase 3: Progressive Memory UI

| Task | Effort | Impact |
|------|--------|--------|
| Memory recall visualization | Medium | High |
| Inline `/remember` command | Low | Medium |
| Search across scopes | Medium | High |
| Memory pinning/dismissal | Low | Medium |

**Integration points**:
- `code/semantic_search` skill for unified search
- `memory` storage APIs exposed to TUI
- Session-aware memory recall display

### Phase 4: DSPy Training Loop

| Task | Effort | Impact |
|------|--------|--------|
| Real-time trajectory capture in TUI | Medium | High |
| Inline rating widget (1-5 stars) | Low | High |
| Background pattern extraction | Medium | High |
| Weight update visualization | Low | Medium |

**Integration points**:
- `trajectory` storage with session linkage
- `optimize/*` skills running in background
- Scorer weight persistence and display

### Phase 5: Multi-Agent Orchestration

| Task | Effort | Impact |
|------|--------|--------|
| Orchestration dashboard | High | High |
| Spawn agent from TUI | Medium | High |
| Intercept overseer decisions | High | Critical |
| Cross-agent memory sharing | Medium | High |

**Integration points**:
- Actor system events surfaced to TUI
- Mailbox polling with visual queue
- Blackboard display for shared state

---

## Technical Details

### New API Endpoints Required

```
POST /api/agents/spawn           - Create new agent
GET  /api/agents                 - List active agents
GET  /api/agents/:id             - Agent detail + status
POST /api/agents/:id/message     - Send message to agent
DELETE /api/agents/:id           - Kill agent

GET  /api/sessions/active        - Get current interactive session
POST /api/sessions/start         - Start interactive session
POST /api/sessions/input         - Send user input
POST /api/sessions/feedback      - Submit feedback

GET  /api/memory/recalled        - Get currently recalled memories
POST /api/memory/pin             - Pin a memory
POST /api/memory/add             - Add new memory
GET  /api/memory/search          - Semantic search

GET  /api/events                 - SSE stream for real-time updates
     Events: agent.thinking, agent.tool_call, agent.ask,
             agent.result, memory.recalled, feedback.collected
```

### New TUI Views

| View | Key | Purpose |
|------|-----|---------|
| AgentView | `0` or `a` | Interactive agent session |
| MemoryView | `m` | Progressive context panel |
| OrchestrationView | `o` | Agent hierarchy + mailbox |
| TrajectoryView | `t` | Live trajectory capture |

### Event Types for SSE

```typescript
interface AgentEvent {
  type: 'agent.thinking' | 'agent.tool_call' | 'agent.ask' |
        'agent.result' | 'agent.error' | 'agent.spawn';
  agent_id: string;
  session_id: string;
  timestamp: string;
  data: any;
}

interface MemoryEvent {
  type: 'memory.recalled' | 'memory.added' | 'memory.pinned';
  scope: 'symbols' | 'memory' | 'sessions' | 'tasks' | 'codemaps';
  entries: Array<{ id: string; name: string; similarity: number }>;
}

interface FeedbackEvent {
  type: 'feedback.collected';
  trajectory_id: string;
  rating: number;
  note?: string;
}
```

---

## Success Metrics

1. **Interactive session latency**: <100ms for user input to agent response start
2. **Memory recall accuracy**: Top-3 recalled items relevant >80% of time
3. **Feedback collection rate**: >30% of agent actions receive ratings
4. **Agent coordination visibility**: 100% of mailbox messages visible in TUI
5. **Training data quality**: >50% of sessions exportable as DSPy examples

---

## Dependencies

### Existing Components (Ready)
- OpenTUI framework (packages/tui)
- Express API server (packages/gui/server)
- Actor system (internal/actor)
- DspyActor (internal/actor/dspy_actor.go)
- Memory storage (internal/storage/memory)
- Trajectory capture (internal/storage/trajectory)
- 87 skills (skills/*)

### New Components Required
- UserProxyActor (bridges TUI input to actor system)
- SSE event broadcaster
- Interactive session manager
- Feedback collection API

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| SSE connection stability | Implement reconnection with exponential backoff |
| Agent state synchronization | Use EventBus for consistent state propagation |
| Memory recall latency | Pre-warm embeddings, use Tier 1 (fast) first |
| Feedback fatigue | Make ratings optional, use 1-key shortcuts |
| Overseer contention | Queue human decisions, don't block agent |

---

## References

- `docs/designs/reactive-actor-system.md` - Actor architecture
- `docs/designs/progressive-memory-system.md` - Memory tiers
- `docs/spec/dspy_go_agents.md` - DSPy integration
- `docs/spec/overseer_profile.md` - Overseer coordination
- `docs/designs/unified-session-lineage.md` - Session tracking
