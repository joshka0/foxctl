# Interactive Agent System Integration Plan

> **Status**: Draft
> **Created**: 2026-01-02
> **Goal**: Unify OpenTUI, DSPy agents, orchestration, and progressive memory into an interactive agent platform

## Executive Summary

agentctl has a rich foundation with **87 skills**, a **reactive actor system**, **DSPy-go agents**, **progressive memory with embeddings**, and an **OpenTUI viewer**. This plan unifies these into an **interactive agent orchestration platform** - a "Codex/Claude Code" style tool that's fully observable, trainable, and user-driven.

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
