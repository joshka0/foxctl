# Interactive Agent Terminal Design

> Vision: A web-based terminal for spawning, monitoring, and coordinating AI agents in real-time.

## Overview

Transform the web-ui into a mission control center for multi-agent orchestration. Users can spawn an Overseer agent, watch it coordinate child agents, monitor their communication via mailbox, and interact with the agent hierarchy in real-time.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Web UI (React)                                  │
├─────────────┬─────────────┬─────────────┬─────────────┬────────────────┤
│  Agent      │  Hierarchy  │  Mailbox    │  Terminal   │  Task          │
│  Spawner    │  Visualizer │  Monitor    │  Console    │  Dashboard     │
└──────┬──────┴──────┬──────┴──────┬──────┴──────┬──────┴───────┬────────┘
       │             │             │             │              │
       └─────────────┴─────────────┼─────────────┴──────────────┘
                                   │
                          ┌────────▼────────┐
                          │   SSE Events    │ (real-time updates)
                          │   REST API      │ (commands & queries)
                          └────────┬────────┘
                                   │
┌──────────────────────────────────▼──────────────────────────────────────┐
│                      Go Backend (foxctl_web)                          │
├─────────────┬─────────────┬─────────────┬─────────────┬────────────────┤
│  Agent      │  Overseer   │  Mailbox    │  Session    │  Skill         │
│  Manager    │  Controller │  Gateway    │  Tracker    │  Runner        │
└──────┬──────┴──────┬──────┴──────┬──────┴──────┬──────┴───────┬────────┘
       │             │             │             │              │
       └─────────────┴─────────────┼─────────────┴──────────────┘
                                   │
                    ┌──────────────▼──────────────┐
                    │      Storage Layer          │
                    ├──────────────┬──────────────┤
                    │  mailbox.db  │  agents.db   │
                    │  sessions.db │  tasks.db    │
                    └──────────────┴──────────────┘
```

## Core Components

### 1. Agent Hierarchy Visualizer

**Purpose**: Real-time tree view of agent relationships

```
Overseer (running) ─────────────────────────────────┐
  ├── Planner-A1 (running) depth=1                  │
  │   ├── Coder-B1 (ok) depth=2                     │
  │   └── Coder-B2 (running) depth=2                │
  └── Reviewer-A2 (blocked) depth=1                 │
      └── [cannot spawn - at depth limit]           │
────────────────────────────────────────────────────┘
```

**Data Source**:
- `GET /api/agents` - List all agents with hierarchy info
- `GET /api/agents/{id}/children` - Get agent's children
- SSE events: `agent.spawned`, `agent.status_changed`, `agent.completed`

**Key Fields**:
```typescript
interface AgentNode {
  id: string;
  namespace: string;          // actor:agent:coder:epic-123
  role: AgentRole;            // overseer|planner|coder|reviewer|fixer
  status: AgentStatus;        // running|ok|blocked|error|needs_review
  depth: number;
  maxDepth: number;
  localMaxDepth: number;
  parentId: string | null;
  children: string[];
  sessionId: string;
  createdAt: string;
  lastHeartbeat: string;
}
```

### 2. Mailbox Monitor

**Purpose**: Real-time view of inter-agent messaging

```
┌─────────────────────────────────────────────────────────────────┐
│ Mailbox Monitor                                    [Filter: All]│
├─────────────────────────────────────────────────────────────────┤
│ ● 12:34:05  Planner-A1 → Overseer    [cmd]  spawn(coder)       │
│ ● 12:34:06  Overseer → Planner-A1    [reply] spawned: Coder-B1 │
│ ○ 12:34:10  Coder-B1 → Planner-A1    [ask]  need file context  │
│ ● 12:34:12  Planner-A1 → Coder-B1    [reply] context provided  │
│ ◐ 12:34:15  Coder-B1 → Reviewer-A2   [event] code ready        │
└─────────────────────────────────────────────────────────────────┘
```

**Message Types**:
- `agent.ask` - Question requiring response
- `agent.reply` - Response to ask
- `agent.cmd` - Command (spawn, cancel, etc.)
- `agent.event` - Notification (heartbeat, status change)
- `console.ask/reply/cmd/event` - User-to-agent communication

**Data Source**:
- `GET /api/mailbox?ns={namespace}` - Messages for an agent
- `GET /api/mailbox/stream` - SSE stream of all messages
- `POST /api/mailbox/send` - Send message to agent

### 3. Agent Spawner Panel

**Purpose**: Spawn new agents with configuration

```
┌─────────────────────────────────────────────────────────────────┐
│ Spawn New Agent                                                 │
├─────────────────────────────────────────────────────────────────┤
│ Role:     [Overseer ▼]                                          │
│ Parent:   [None (root) ▼]                                       │
│ Epic ID:  [auto-generated]                                      │
│ Prompt:   ┌──────────────────────────────────────────────────┐  │
│           │ Coordinate the implementation of feature X...    │  │
│           └──────────────────────────────────────────────────┘  │
│ Skills:   [x] code.*  [x] file.*  [ ] test.*  [x] mail.*        │
│ Limits:   Max Depth: [3]  Local Max: [2]                        │
│                                                                 │
│                              [Cancel]  [Spawn Agent]            │
└─────────────────────────────────────────────────────────────────┘
```

**Spawn Flow**:
1. User configures agent in UI
2. `POST /api/agents/spawn` sends request
3. Backend validates and creates agent via Runtime.Spawn()
4. SSE broadcasts `agent.spawned` event
5. UI updates hierarchy tree

### 4. Agent Console (Terminal)

**Purpose**: Interactive communication with agents

```
┌─────────────────────────────────────────────────────────────────┐
│ Console: Coder-B1 (actor:agent:coder:epic-123)     [Disconnect] │
├─────────────────────────────────────────────────────────────────┤
│ [12:34:05] Agent started with task: Implement auth module       │
│ [12:34:10] Searching for existing auth patterns...              │
│ [12:34:15] Found 3 relevant files, analyzing...                 │
│ [12:34:20] ⚠ BLOCKED: Need approval for modifying user.go       │
│                                                                 │
│ > ▌                                                             │
├─────────────────────────────────────────────────────────────────┤
│ [Approve] [Deny] [Ask Question] [Send Command] [Kill Agent]     │
└─────────────────────────────────────────────────────────────────┘
```

**Interaction Types**:
- **Approve/Deny**: Respond to `agent.ask` with kind=approval
- **Ask Question**: Send `console.ask` to agent
- **Send Command**: Send `console.cmd` (pause, resume, change priority)
- **View Logs**: Stream agent's tool calls and outputs

### 5. Task Dashboard Integration

**Purpose**: Connect agent work to task graph

```
┌─────────────────────────────────────────────────────────────────┐
│ Active Task: Implement user authentication                      │
├─────────────────────────────────────────────────────────────────┤
│ Assigned Agent: Coder-B1                                        │
│ Status: in_progress (iteration 3/10)                            │
│ Dependencies: [Setup DB schema ✓] [Create models ✓]             │
│ Blocking: [Write tests] [Update docs]                           │
│                                                                 │
│ Tool Calls:                                                     │
│   1. code/symbols → found 12 symbols                            │
│   2. file/read → user.go (245 lines)                            │
│   3. file/edit → user.go (+32 lines)                            │
└─────────────────────────────────────────────────────────────────┘
```

---

## API Additions Required

### New Endpoints

```go
// Agent Management
POST   /api/agents/spawn              // Spawn new agent
GET    /api/agents                    // List all agents
GET    /api/agents/{id}               // Get agent details
GET    /api/agents/{id}/hierarchy     // Get subtree
DELETE /api/agents/{id}               // Kill agent
POST   /api/agents/{id}/pause         // Pause agent
POST   /api/agents/{id}/resume        // Resume agent

// Mailbox Gateway
GET    /api/mailbox                   // List messages (with filters)
GET    /api/mailbox/stream            // SSE stream of messages
POST   /api/mailbox/send              // Send message to agent
POST   /api/mailbox/{id}/ack          // Acknowledge message

// Overseer Control
POST   /api/overseer/start            // Start overseer daemon
GET    /api/overseer/status           // Overseer health/stats
POST   /api/overseer/stop             // Stop overseer

// Console
POST   /api/console/{agentId}/ask     // Send ask to agent
POST   /api/console/{agentId}/reply   // Reply to agent's ask
POST   /api/console/{agentId}/cmd     // Send command
GET    /api/console/{agentId}/stream  // SSE stream for agent
```

### SSE Event Types

```typescript
// New event types for real-time updates
type AgentEvent =
  | { type: 'agent.spawned'; data: AgentNode }
  | { type: 'agent.status'; data: { id: string; status: AgentStatus } }
  | { type: 'agent.completed'; data: { id: string; summary: string } }
  | { type: 'agent.error'; data: { id: string; error: string } }
  | { type: 'agent.heartbeat'; data: { id: string; iteration: number } };

type MailboxEvent =
  | { type: 'mailbox.message'; data: Message }
  | { type: 'mailbox.ack'; data: { messageId: string } };

type ConsoleEvent =
  | { type: 'console.output'; data: { agentId: string; text: string } }
  | { type: 'console.tool_call'; data: { agentId: string; tool: string; args: any } }
  | { type: 'console.blocked'; data: { agentId: string; reason: string; askId: string } };
```

---

## Frontend Components

### New Pages

```
/agents              → AgentsPage (hierarchy + spawner)
/agents/:id          → AgentDetailPage (console + logs)
/agents/:id/mailbox  → AgentMailboxPage (filtered messages)
/terminal            → TerminalPage (full mission control)
```

### Component Hierarchy

```
<TerminalPage>
  ├── <AgentHierarchy>         // Tree visualization
  │   ├── <AgentNode>          // Individual node
  │   └── <SpawnDialog>        // Spawn form
  │
  ├── <MailboxPanel>           // Message stream
  │   ├── <MessageList>        // Scrolling list
  │   ├── <MessageFilters>     // Type/agent filters
  │   └── <ComposeMessage>     // Send new message
  │
  ├── <ConsolePanel>           // Agent interaction
  │   ├── <OutputLog>          // Streaming output
  │   ├── <InputPrompt>        // User input
  │   └── <QuickActions>       // Approve/Deny/Kill
  │
  └── <TaskSidebar>            // Task context
      ├── <ActiveTask>         // Current assignment
      └── <DependencyGraph>    // Mini graph view
```

---

## Implementation Phases

### Phase 1: Agent Visibility (Foundation)

**Goal**: See what agents exist and their status

**Backend**:
- [ ] Add `GET /api/agents` endpoint (reads from agents.db)
- [ ] Add `GET /api/agents/{id}` endpoint
- [ ] Add agent events to SSE hub (`agent.status`, `agent.heartbeat`)
- [ ] Connect to existing `internal/storage/agents` store

**Frontend**:
- [ ] Create `AgentsPage` with list view
- [ ] Create `AgentDetailPage` with status/info
- [ ] Add agents link to navigation
- [ ] Subscribe to agent SSE events

**Deliverable**: View all agents, their status, and hierarchy depth

---

### Phase 2: Mailbox Visibility

**Goal**: Monitor inter-agent communication

**Backend**:
- [ ] Add `GET /api/mailbox` with namespace filtering
- [ ] Add `GET /api/mailbox/stream` SSE endpoint
- [ ] Broadcast mailbox events through existing hub

**Frontend**:
- [ ] Create `<MailboxPanel>` component
- [ ] Add message type badges and icons
- [ ] Implement real-time message streaming
- [ ] Add filters (by agent, by type, by time)

**Deliverable**: Real-time view of all agent messages

---

### Phase 3: Agent Spawning

**Goal**: Spawn agents from the UI

**Backend**:
- [ ] Add `POST /api/agents/spawn` endpoint
- [ ] Integrate with `internal/agent/runtime.Spawn()`
- [ ] Validate spawn requests (depth limits, allowed roles)
- [ ] Return session ID and agent namespace

**Frontend**:
- [ ] Create `<SpawnDialog>` component
- [ ] Role selection with descriptions
- [ ] Prompt textarea with templates
- [ ] Skills checklist
- [ ] Depth limit configuration

**Deliverable**: Spawn overseer and child agents from UI

---

### Phase 4: Agent Console

**Goal**: Interact with agents in real-time

**Backend**:
- [ ] Add `POST /api/console/{id}/ask` - send question to agent
- [ ] Add `POST /api/console/{id}/reply` - answer agent's ask
- [ ] Add `GET /api/console/{id}/stream` - agent output stream
- [ ] Route messages through mailbox with `console.*` types

**Frontend**:
- [ ] Create `<ConsolePanel>` with terminal-like UI
- [ ] Streaming output display
- [ ] Input prompt with history
- [ ] Quick action buttons (approve/deny/kill)
- [ ] Blocked state handling with approval UI

**Deliverable**: Full bidirectional communication with agents

---

### Phase 5: Hierarchy Visualization

**Goal**: Visual tree of agent relationships

**Backend**:
- [ ] Add `GET /api/agents/{id}/hierarchy` - get subtree
- [ ] Include depth, limits, and spawn capability

**Frontend**:
- [ ] Create `<AgentHierarchy>` tree component
- [ ] Collapsible nodes with status indicators
- [ ] Click to select agent (updates console)
- [ ] Right-click context menu (spawn child, kill, pause)
- [ ] Animated spawning/completion transitions

**Deliverable**: Interactive agent tree with full context

---

### Phase 6: Mission Control (Terminal Page)

**Goal**: Unified command center

**Frontend**:
- [ ] Create `<TerminalPage>` with panel layout
- [ ] Resizable panels (hierarchy, mailbox, console)
- [ ] Agent selection syncs across panels
- [ ] Keyboard shortcuts (Ctrl+Enter to send, Esc to cancel)
- [ ] Dark terminal theme option

**Deliverable**: Full mission control interface

---

### Phase 7: Task Integration

**Goal**: Connect agents to task graph

**Backend**:
- [ ] Add task assignment to agent spawn
- [ ] Track which agent is working on which task
- [ ] Update task status based on agent completion

**Frontend**:
- [ ] Show assigned task in agent detail
- [ ] Link from task detail to assigned agent
- [ ] Mini task graph in console sidebar

**Deliverable**: Unified agent + task management

---

## Data Flow Examples

### Spawn Overseer Flow

```
User clicks "Spawn Overseer"
    │
    ▼
POST /api/agents/spawn {role: "overseer", prompt: "...", epicId: "..."}
    │
    ▼
Backend: runtime.Spawn(AgentConfig{...})
    │
    ├── Creates session in memory
    ├── Stores agent in agents.db
    └── Broadcasts SSE: {type: "agent.spawned", data: {...}}
    │
    ▼
Frontend receives SSE, updates hierarchy tree
    │
    ▼
User sees new Overseer node in tree
```

### Agent Ask/Reply Flow

```
Agent hits blocking point, needs approval
    │
    ▼
Agent calls mail.send → Mailbox stores message
    │
    ▼
Backend broadcasts SSE: {type: "mailbox.message", data: {type: "agent.ask", ...}}
    │
    ▼
Frontend shows "BLOCKED" in console, enables [Approve] button
    │
    ▼
User clicks [Approve]
    │
    ▼
POST /api/console/{id}/reply {askId: "...", answer: {approved: true}}
    │
    ▼
Backend: mailbox.Send(replyMessage)
    │
    ▼
Agent polls mailbox, receives reply, continues
    │
    ▼
SSE: {type: "agent.status", data: {status: "running"}}
```

---

## Security Considerations

1. **No Auth Currently**: Web UI has no authentication - suitable for local development only
2. **Future**: Add basic auth or OAuth for multi-user scenarios
3. **Rate Limiting**: Prevent spawn storms with configurable limits
4. **Workspace Isolation**: Agents should only access their workspace's data
5. **Audit Trail**: Log all spawn/kill/message actions

---

## Performance Considerations

1. **SSE Scalability**: Current hub handles ~256 concurrent clients
2. **Message Volume**: High-frequency heartbeats need throttling for UI
3. **Hierarchy Size**: Lazy-load deep hierarchies (>50 nodes)
4. **Database Polling**: Consider SQLite WAL mode for concurrent reads

---

## Open Questions

1. **Persistence**: Should agent state survive server restarts?
2. **Multi-Instance**: How to handle multiple web-ui instances?
3. **Remote Agents**: Support agents running on different machines?
4. **Cost Tracking**: Show LLM token usage per agent?
5. **Replay**: Ability to replay agent sessions for debugging?

---

## Success Metrics

- [ ] Spawn overseer in <2 clicks
- [ ] See agent output in <500ms of generation
- [ ] Handle 10+ concurrent agents without UI lag
- [ ] User can approve/deny in <1 second
- [ ] Full hierarchy visible without scrolling (up to 20 agents)
