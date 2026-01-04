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

**Real-time**: `@agentctl/data.subscribeToEvents()` expects an `/api/events` SSE endpoint; GUI wires this via `useSSE`, but `packages/gui/server/index.js` does not currently implement `/api/events` and the TUI does not yet subscribe.

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

**Optimization Skills (6 total)**:
- `optimize/feedback`, `optimize/patterns`, `optimize/reflect`
- `optimize/bootstrap`, `optimize/weights`, `optimize/analyze`

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

**Mailbox message types** (`internal/domain/agent/mailbox.go`):
  - `agent.ask` - Request expecting a response
  - `agent.reply` - Response to `agent.ask`
  - `agent.cmd` - Fire-and-forget command
  - `agent.event` - Notification/heartbeat
  - `console.ask` - User request from console
  - `console.reply` - Final response to console
  - `console.event` - Streaming update to console
  - `console.cmd` - Console control command (e.g., cancel)

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

1. **SSE Event Stream**: Implement `/api/events` in Express to emit coarse invalidation events (`job|task|mailbox|blackboard`) for TUI/GUI; optionally extend later with rich agent/console events.
2. **Interactive Session Mode**: Re-use `agentctl console attach` / console sessions (`console.*` mailbox messages) as the interactive bridge.
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
│  │ (Terminal)  │  │ (Browser)   │  │ • console attach --actor ...│
│  │ • AgentView │  │ • AgentView │  │ • agent watch <agent-id>    │  │
│  │ • MemoryView│  │ • MemoryView│  │ • actorsys logs <ns> --follow│ │
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
│  │ ├── /api/{jobs,tasks,stats,insights,mailbox,...} (existing)  │   │
│  │ ├── /api/sessions/* (captured sessions browsing; existing)   │   │
│  │ ├── /api/events (SSE stream; missing today)                  │   │
│  │ │    • type: "job"|"task"|"mailbox"|"blackboard"             │   │
│  │ └── /api/{agents,consoles,memory,trajectory}/* (proposed)    │   │
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
│  │     └── DspyActor (LLM-driven via dspy-go)                │    │
│  │         ├── CoderHandler                                   │    │
│  │         ├── PlannerHandler                                 │    │
│  │         └── ReviewerHandler                                │    │
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
│  │ ├── session/* (capture, recall, export_dspy)                  │ │
│  │ ├── optimize/* (feedback, patterns, reflect, bootstrap, weights, analyze) │ │
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
- **AgentView** (key: `0`) - Interactive agent session
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
- Background worker runs `agentctl optimize session analyze` periodically

### 3. Subagent Orchestration → Visual Hierarchy

**Current**: Overseer coordinates via mailbox (invisible to user)

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
│ [12:34:55] overseer → coder-01: agent.cmd "Fix JWT..."     │
│ [12:34:50] reviewer-01 → overseer: agent.event "LGTM"      │
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
| `agentctl console attach` | Medium | High |
| TUI <-> DspyActor bridge | High | Critical |
| Human-in-loop for `agent.ask` | Medium | High |
| Inline feedback collection | Low | High |

**New components**:
- Console session API surface (list/attach/send/poll or SSE)
- Feedback capture UI widget

---

## Phase 2 Deep Dive: Interactive Agent Sessions

### Current State Analysis

**What Already Exists:**

| Component | Status | Location |
|-----------|--------|----------|
| Console CLI commands | ✅ Complete | `cmd/agentctl/cmd/console.go` (attach, list, rm) |
| Console store (SQLite) | ✅ Complete | `internal/storage/console/store.go` |
| DspyActor runtime | ✅ Complete | `internal/actor/dspy_actor.go` (agent.* handlers) |
| Console message types | ⚠️ Defined only | `internal/domain/agent/mailbox.go` |
| Trajectory capture | ✅ Complete | `internal/storage/trajectory/` |
| HumanRating field | ✅ Complete | `Outcome.HumanRating *int` (1-5 scale) |
| Agent spawn protocol | ✅ Complete | `internal/agent/tools/spawn_tools.go` |
| SSE endpoint | ❌ Missing | `packages/gui/server/index.js` |
| Console view TUI | ❌ Missing | `packages/tui/src/views/` |

**What Needs Building:**

1. **DspyActor console handlers** - Bridge console.* messages to agent execution
2. **SSE /api/events endpoint** - Real-time event streaming to TUI
3. **Console API endpoints** - REST surface for TUI interaction
4. **ConsoleView TUI component** - Interactive agent session UI
5. **Feedback widget** - 1-5 star rating inline

---

### Implementation Tasks

#### Task 2.1: DspyActor Console Handlers

**Goal**: Enable DspyActor to receive user input and stream results

**Files to modify**: `internal/actor/dspy_actor.go`

**New handlers to add:**
```go
// Register in NewDspyActor()
actor.RegisterHandler("console.ask", actor.handleConsoleAsk)
actor.RegisterHandler("console.cmd", actor.handleConsoleCmd)

// handleConsoleAsk - Execute user request with streaming
func (a *DspyActor) handleConsoleAsk(ctx context.Context, msg *actor.Message) (*actor.Message, error) {
    // 1. Extract prompt from msg.Data
    // 2. Create trajectory with RootRequestID = msg.ID
    // 3. Execute ReAct agent (same as handleAsk)
    // 4. During execution, emit console.event for each iteration:
    //    - thought, action, observation, tool_result
    // 5. On completion, send console.reply with final answer
    // 6. Record trajectory outcome
}

// handleConsoleCmd - Control execution
func (a *DspyActor) handleConsoleCmd(ctx context.Context, msg *actor.Message) (*actor.Message, error) {
    // Handle: cancel, pause, timeout, resume
    // Store command in actor state, check in ReAct loop
}
```

**Streaming mechanism:**
```go
// During ReAct execution, emit events
a.sendConsoleEvent(msg.From, &ConsoleEventPayload{
    Type:        "thought",
    Content:     thoughtText,
    Iteration:   i,
    TrajectoryID: traj.ID,
})
```

**Effort**: Medium-High (2-3 days)

---

#### Task 2.2: SSE Event Endpoint

**Goal**: Real-time event streaming from Express API to TUI

**Files to modify**: `packages/gui/server/index.js`

**Implementation:**
```javascript
// Add SSE endpoint
app.get('/api/events', (req, res) => {
  res.writeHead(200, {
    'Content-Type': 'text/event-stream',
    'Cache-Control': 'no-cache',
    'Connection': 'keep-alive',
  });

  // Subscribe to SQLite changes via polling or triggers
  const interval = setInterval(() => {
    // Check for new events in:
    // - jobs (state changes)
    // - tasks (status changes)
    // - mailbox (new messages)
    // - blackboard (key updates)
    // - trajectory_events (agent progress)

    const events = checkForNewEvents(lastEventId);
    events.forEach(event => {
      res.write(`data: ${JSON.stringify(event)}\n\n`);
    });
  }, 100); // 100ms polling

  req.on('close', () => clearInterval(interval));
});
```

**Event types:**
```typescript
type SSEEvent =
  | { type: "job"; data: { id: string; state: string } }
  | { type: "task"; data: { id: string; status: string } }
  | { type: "mailbox"; data: { actor: string; message_id: string } }
  | { type: "blackboard"; data: { ns: string; topic: string } }
  | { type: "console.event"; data: ConsoleEventPayload }
  | { type: "console.reply"; data: ConsoleReplyPayload };
```

**Effort**: Low-Medium (1-2 days)

---

#### Task 2.3: Console REST API

**Goal**: HTTP endpoints for TUI to interact with console sessions

**Files to modify**: `packages/gui/server/index.js`, `packages/data/src/client.ts`

**New endpoints:**
```
GET  /api/consoles                - List console sessions
POST /api/consoles                - Create/attach console session
GET  /api/consoles/:id            - Get console session details
DELETE /api/consoles/:id          - Delete console session

POST /api/consoles/:id/send       - Send console.ask message
GET  /api/consoles/:id/events     - SSE stream for this console only
POST /api/consoles/:id/cancel     - Send console.cmd cancel
POST /api/consoles/:id/feedback   - Record trajectory feedback
```

**Send endpoint handler:**
```javascript
app.post('/api/consoles/:id/send', async (req, res) => {
  const { prompt, context } = req.body;
  const consoleId = req.params.id;

  // 1. Get console session → actor_id
  // 2. Create console.ask message
  // 3. Insert into mailbox for actor
  // 4. Return message_id for tracking

  res.json({ message_id: newMessageId, status: 'sent' });
});
```

**Effort**: Medium (2 days)

---

#### Task 2.4: ConsoleView TUI Component

**Goal**: Interactive agent session UI in terminal

**Files to create**: `packages/tui/src/views/ConsoleView.tsx`

**Layout:**
```
┌─────────────────────────────────────────────────────────────────┐
│ CONSOLE SESSION: coder-01                    [connected] [P:3]  │
├─────────────────────────────────────────────────────────────────┤
│ HISTORY                                                         │
│ ─────────                                                       │
│ [12:34:56] USER: Fix the JWT refresh race condition             │
│ [12:34:57] AGENT: Searching for token refresh patterns...       │
│ [12:34:58] TOOL: code/swe_grep (3 results)                      │
│ [12:34:59] AGENT: Found issue in auth/jwt.ts:123                │
│ [12:35:00] TOOL: fs/read auth/jwt.ts                            │
│ [12:35:01] AGENT: Applying mutex pattern...                     │
│ [12:35:02] TOOL: code/smart_write auth/jwt.ts                   │
│ [12:35:03] RESULT: Fixed race condition with sync.Mutex         │
│                                                                 │
│ [Rating: ★★★★☆] [Feedback: Good approach]                      │
├─────────────────────────────────────────────────────────────────┤
│ > Type your message... (Enter to send, Esc to cancel)           │
├─────────────────────────────────────────────────────────────────┤
│ [1-5]rate [c]ancel [h]istory [m]emory [Enter]send              │
└─────────────────────────────────────────────────────────────────┘
```

**Key features:**
- SSE subscription for real-time updates
- Input field with Enter to send
- 1-5 number keys for quick rating after each response
- Cancel button sends console.cmd
- Memory panel toggle shows recalled context
- Scroll through history with j/k

**State management:**
```typescript
interface ConsoleState {
  consoleId: string;
  actorId: string;
  connected: boolean;
  history: ConsoleEvent[];
  inputValue: string;
  isProcessing: boolean;
  currentTrajectoryId: string | null;
  lastRating: number | null;
}
```

**Effort**: High (3-4 days)

---

#### Task 2.5: Inline Feedback Widget

**Goal**: Quick 1-5 star rating after each agent response

**Files to modify**: `packages/tui/src/views/ConsoleView.tsx`

**Behavior:**
1. After `console.reply` received, show rating prompt
2. User presses 1-5 key for quick rating
3. Optionally press `f` for text feedback modal
4. Rating sent to `/api/consoles/:id/feedback`
5. Stored in `trajectory.outcome.human_rating`

**Feedback flow:**
```
console.reply → Show rating prompt → User presses 1-5
                                          ↓
                               POST /api/consoles/:id/feedback
                                          ↓
                               trajectory.outcome.human_rating = N
                                          ↓
                               Background: optimize/feedback runs
```

**API handler:**
```javascript
app.post('/api/consoles/:id/feedback', async (req, res) => {
  const { trajectory_id, rating, feedback_text } = req.body;

  // Update trajectory outcome
  db.prepare(`
    UPDATE trajectories
    SET outcome = json_set(
      COALESCE(outcome, '{}'),
      '$.human_rating', ?,
      '$.feedback', ?,
      '$.recorded_at', ?
    )
    WHERE id = ?
  `).run(rating, feedback_text, new Date().toISOString(), trajectory_id);

  res.json({ success: true });
});
```

**Effort**: Low (1 day)

---

#### Task 2.6: Human-in-Loop for agent.ask

**Goal**: Intercept agent.ask messages for human review before overseer responds

**Files to modify**:
- `configs/hooks/overseer-inbox.sh` (or new hook)
- `packages/tui/src/views/OrchestrationView.tsx`

**Mechanism:**
1. When agent sends `agent.ask` to overseer, hook surfaces it
2. TUI OrchestrationView shows pending questions with priority
3. Human can:
   - **Answer directly**: Type response, send as `agent.reply`
   - **Delegate to overseer**: Press `d` to let overseer handle
   - **Modify and delegate**: Edit question, then delegate

**Hook enhancement:**
```bash
# In overseer-inbox.sh or new agent-ask-intercept.sh
# Check for agent.ask messages to overseer
# Set a short delay (e.g., 5s) before auto-delegating
# If human responds within delay, use their answer
```

**TUI integration:**
```
┌─────────────────────────────────────────────────────────────────┐
│ PENDING QUESTIONS (2)                                           │
├─────────────────────────────────────────────────────────────────┤
│ [P1] coder-01 asks: "Should I use mutex or channel for sync?"   │
│      Context: auth/jwt.ts race condition fix                    │
│      [a]nswer  [d]elegate  [v]iew context                       │
│                                                                 │
│ [P2] reviewer-01 asks: "Is this error handling sufficient?"     │
│      Context: review of auth.ts changes                         │
│      [a]nswer  [d]elegate  [v]iew context                       │
└─────────────────────────────────────────────────────────────────┘
```

**Effort**: Medium (2 days)

---

### Implementation Order

```
Week 1:
├── Day 1-2: Task 2.2 - SSE Event Endpoint
├── Day 3-4: Task 2.3 - Console REST API
└── Day 5: Task 2.5 - Feedback Widget (API part)

Week 2:
├── Day 1-3: Task 2.1 - DspyActor Console Handlers
├── Day 4-5: Task 2.4 - ConsoleView TUI (basic)

Week 3:
├── Day 1-2: Task 2.4 - ConsoleView TUI (polish)
├── Day 3-4: Task 2.6 - Human-in-Loop agent.ask
└── Day 5: Integration testing + bug fixes
```

---

### Success Criteria

| Metric | Target |
|--------|--------|
| Input → Agent response start | <100ms |
| Console.event streaming latency | <50ms |
| SSE connection stability | 99%+ uptime |
| Feedback collection rate | >30% of interactions |
| Human-in-loop response window | 5-10s configurable |

---

### Dependencies

**From Phase 1 (✅ Complete):**
- AgentView, MemoryView, OrchestrationView TUI components
- Sessions/Agents/Trajectories API endpoints
- Enter key detail views

**External:**
- dspy-go library (already integrated)
- Voyage/Gemini API keys (for memory recall)

---

### Risk Mitigations

| Risk | Mitigation |
|------|------------|
| SSE connection drops | Auto-reconnect with exponential backoff |
| DspyActor hangs | Configurable timeout + console.cmd cancel |
| Feedback fatigue | Make ratings optional, 1-key shortcuts |
| Message ordering | Use trajectory_id + sequence numbers |
| Memory pressure | Limit history buffer, paginate old events |

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
GET  /api/events                 - SSE stream for real-time updates (missing today)
     Events: job, task, mailbox, blackboard

GET  /api/consoles               - List console sessions (interactive)
POST /api/consoles/attach        - Create/reuse console session
POST /api/consoles/:id/input     - Send user input (console.ask)
GET  /api/consoles/:id/events    - Poll/SSE for console.event/console.reply

POST /api/agents/spawn           - Create new agent (proposed)
GET  /api/agents                 - List active agents (proposed)
GET  /api/agents/:id             - Agent detail + status (proposed)
POST /api/agents/:id/message     - Send message to agent (proposed)
DELETE /api/agents/:id           - Kill agent (proposed)

GET  /api/memory/recalled        - Get currently recalled memories (proposed)
POST /api/memory/pin             - Pin a memory (proposed)
POST /api/memory/add             - Add new memory (proposed)
GET  /api/memory/search          - Semantic search (proposed)

POST /api/trajectory/feedback    - Submit feedback (proposed)
```

### New TUI Views

| View | Key | Purpose |
|------|-----|---------|
| AgentView | `0` | Interactive agent session |
| MemoryView | `m` | Progressive context panel |
| OrchestrationView | `o` | Agent hierarchy + mailbox |
| TrajectoryView | `t` | Live trajectory capture |

### Event Types for SSE

```typescript
// Matches @agentctl/data.subscribeToEvents() and packages/gui/src/api/hooks.ts useSSE().
type SSEEvent =
  | { type: "job"; data: { id?: string; state?: string } }
  | { type: "task"; data: { id?: string } }
  | { type: "mailbox"; data: { actor?: string; message_id?: string } }
  | { type: "blackboard"; data: { ns?: string; topic?: string } };
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
- Console session API surface (bridges TUI input to console sessions)
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
