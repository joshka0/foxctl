# Reactive Actor System

> Design document for event-driven actor architecture in agentctl

## Contracts

These invariants MUST be maintained by any implementation:

### Delivery Contract

- **At-least-once delivery**: Messages may be delivered multiple times on
  failure; handlers must be idempotent
- **Leased queue semantics**: Messages are claimed with a caller-provided
  visibility timeout (`leaseDuration`); lease expires on crash → auto-retry.
  Claims are atomic and non-blocking (no long waits in `Poll`).
- **Notify is wake-up only**: SQLite triggers signal "work available", but
  consumption always goes through `Poll()` with atomic claim

### Concurrency Contract

- **Sequential processing (MVP)**: Per-actor concurrency = 1; supervisor only
  claims next message when actor is idle
- **Queue depth in SQLite**: No in-memory message buffering; SQLite is the queue

### Observability Contract

- **EventBus is ephemeral**: In-memory pub/sub for low-latency fanout;
  subscribers may miss events
- **Important events persisted**: `mail.received`, `mail.sent`, `mail.acked`,
  `agent.started/stopped/error`, `task.completed` → `trajectory.db`
- **Event metadata**: `mail.*` events carry `source`, `target`, `session_id`,
  and `workspace` for traceability.
- **CLI follows envelope rules**: Outputs are JSON envelopes; large data → CAS +
  summary

## Overview

This document describes the reactive actor system that transforms agentctl
agents from poll-based daemons into event-driven actors. Each agent becomes a
true actor in the system, reacting to messages as they arrive rather than
polling for work.

## Motivation

### Current State: Poll-Based Model

```
Agent Daemon                      Mailbox (SQLite)
┌──────────┐                     ┌──────────────┐
│          │ ───Poll(30s)───────▶│              │
│  dspy-go │                     │  messages    │
│ ReAct    │ ◀───Messages────────│  (queue)     │
│  Agent   │                     │              │
│          │ ───Ack/Nack────────▶│              │
└──────────┘                     └──────────────┘
```

**Problems:**

- Polling introduces latency (up to 30s delay)
- CPU waste when idle
- No true event-driven semantics
- Agents must be "running" to receive messages
- Each agent is a separate process

### Target State: Reactive Actor Model

```
                  Actor Supervisor
(Single long-running process managing all actor lifecycles)
         │                    │                    │
         ▼                    ▼                    ▼
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│ Actor: coder │    │Actor: planner│    │Actor:reviewer│
│              │    │              │    │              │
│ onMailRecv() │    │ onMailRecv() │    │ onMailRecv() │
│ onTimeout()  │    │ onTimeout()  │    │ onTimeout()  │
│ onError()    │    │ onError()    │    │ onError()    │
└──────────────┘    └──────────────┘    └──────────────┘
         ▲                    ▲                    ▲
         └────────────────────┼────────────────────┘
                              │
                  Event Bus (SQLite + Notify)
```

**Benefits:**

- ~50ms latency vs 30s polling
- No CPU waste when idle
- Single supervisor manages many actors
- All events flow through EventBus for observability
- Supervision strategies (restart, backoff, escalate)

## Core Components

### 1. Actor Interface

Every actor implements this reactive interface:

```go
// Actor defines the reactive interface for all agents
type Actor interface {
    // Identity
    ID() string
    Namespace() string

    // Lifecycle
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    // Reactive handlers
    OnMailReceived(ctx context.Context, msg *mailbox.Message) error
    OnTimeout(ctx context.Context, timer TimerEvent) error
    OnError(ctx context.Context, err error) Directive

    // State
    State() ActorState
    Mailbox() chan *mailbox.Message
}

type ActorState string
const (
    ActorStateStarting   ActorState = "starting"
    ActorStateIdle       ActorState = "idle"
    ActorStateProcessing ActorState = "processing"
    ActorStateStopped    ActorState = "stopped"
    ActorStateError      ActorState = "error"
)

// Directive tells supervisor how to handle failures
type Directive int
const (
    DirectiveResume   Directive = iota // Continue processing
    DirectiveRestart                    // Restart actor
    DirectiveStop                       // Stop actor
    DirectiveEscalate                   // Escalate to parent/supervisor
)
```

### 2. Actor Supervisor

The supervisor manages actor lifecycles and message routing:

```go
type Supervisor struct {
    actors    map[string]Actor          // namespace -> actor
    mailbox   *mailbox.Store            // Shared mailbox store
    eventBus  *EventBus                 // For cross-actor events
    watcher   *MailboxWatcher           // SQLite change notifications
    strategy  SupervisionStrategy
}

type SupervisionStrategy struct {
    MaxRestarts     int           // Max restarts within window
    RestartWindow   time.Duration // Time window for restart counting
    BackoffInitial  time.Duration // Initial backoff delay
    BackoffMax      time.Duration // Maximum backoff delay
}
```

**Supervisor responsibilities:**

1. Start/stop actors on demand
2. Route messages to appropriate actors
3. Handle actor failures with configured strategy
4. Provide health monitoring and metrics
5. Manage graceful shutdown

### 3. Mailbox Watcher

Reactive notifications using SQLite triggers. **Important:** The watcher only
signals that work is available; actual message consumption uses the existing
`Poll()` mechanism to maintain lease semantics.

```go
type MailboxWatcher struct {
    db         *sql.DB
    notifyCh   chan WakeUp      // Signal only, no message content
    supervisor *Supervisor
}

type WakeUp struct {
    Namespace string    // Which actor has pending work
    Timestamp time.Time
}
```

**Implementation approach:**

1. Create a `mailbox_notify` table with trigger on `mailbox` INSERT
2. Fast poll (50ms) on notify table for new entries
3. **Wake-up only**: Watcher sends namespace to supervisor
4. Supervisor calls `mailbox.Poll(ctx, namespace, leaseTimeout)` to atomically
   claim (uses the provided lease duration; no blocking wait)
5. Cleanup processed notifications

```sql
-- Notify table (minimal, just wake-up signal)
CREATE TABLE IF NOT EXISTS mailbox_notify (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    to_ns TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Trigger on message insert
CREATE TRIGGER IF NOT EXISTS mailbox_notify_trigger
AFTER INSERT ON mailbox
BEGIN
    INSERT INTO mailbox_notify (to_ns) VALUES (NEW.to_ns);
END;

-- Index for efficient polling
CREATE INDEX IF NOT EXISTS idx_mailbox_notify_created
ON mailbox_notify(created_at);
```

**Dispatch algorithm:**

```go
func (w *MailboxWatcher) Run(ctx context.Context) error {
    ticker := time.NewTicker(50 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            namespaces, err := w.checkNotifications(ctx)
            if err != nil {
                continue // Log and retry
            }
            for _, ns := range namespaces {
                w.notifyCh <- WakeUp{Namespace: ns, Timestamp: time.Now()}
            }
        }
    }
}

func (s *Supervisor) handleWakeUp(ctx context.Context, wake WakeUp) {
    actor, ok := s.actors[wake.Namespace]
    if !ok || actor.State() != ActorStateIdle {
        return // Actor busy or not found
    }

    // Atomically claim message with lease
    msg, err := s.mailbox.Poll(ctx, wake.Namespace, s.leaseTimeout)
    if err != nil || msg == nil {
        return // No message or already claimed
    }

    actor.SetState(ActorStateProcessing)
    go s.processMessage(ctx, actor, msg)
}
```

This gives us near-real-time notifications (~50ms) without requiring external
dependencies like Redis or a message broker, while preserving the existing
lease/ack semantics for crash safety.

### 4. Event Bus

Central hub for all system events. **Note:** The EventBus is ephemeral
(in-memory); important events are selectively persisted to `trajectory.db` for
audit/debug.

```go
type EventBus struct {
    subscribers map[EventType][]chan Event
    events      chan Event
    persister   *TrajectoryPersister  // Selective persistence
    mu          sync.RWMutex
}

type EventType string
const (
    // Mail events (PERSISTED)
    EventMailReceived   EventType = "mail.received"   // 
    EventMailSent       EventType = "mail.sent"
    EventMailAcked      EventType = "mail.acked"      // 
    EventMailExpired    EventType = "mail.expired"    // 

    // Task events
    EventTaskCreated    EventType = "task.created"
    EventTaskUpdated    EventType = "task.updated"
    EventTaskCompleted  EventType = "task.completed"  // 

    // Agent events (PERSISTED)
    EventAgentStarted   EventType = "agent.started"   // 
    EventAgentStopped   EventType = "agent.stopped"   // 
    EventAgentError     EventType = "agent.error"     // 

    // Hook events (ephemeral - too noisy)
    EventHookTriggered  EventType = "hook.triggered"
    EventHookBlocked    EventType = "hook.blocked"

    // File events (ephemeral - reconstruct from git)
    EventFileChanged    EventType = "file.changed"
    EventFileCreated    EventType = "file.created"
)

// Events marked for persistence
var persistedEvents = map[EventType]bool{
    EventMailReceived:  true,
    EventMailAcked:     true,
    EventMailExpired:   true,
    EventAgentStarted:  true,
    EventAgentStopped:  true,
    EventAgentError:    true,
    EventTaskCompleted: true,
}

type Event struct {
    ID        string            `json:"id"`
    Type      EventType         `json:"type"`
    Source    string            `json:"source"`     // Actor namespace
    Target    string            `json:"target"`     // Optional target
    Timestamp time.Time         `json:"timestamp"`
    Data      json.RawMessage   `json:"data"`
    SessionID string            `json:"session_id"`
    Workspace string            `json:"workspace"`
}

func (eb *EventBus) Publish(event Event) {
    // Fan out to subscribers (ephemeral)
    eb.mu.RLock()
    for _, ch := range eb.subscribers[event.Type] {
        select {
        case ch <- event:
        default: // Drop if subscriber is slow
        }
    }
    eb.mu.RUnlock()

    // Persist important events
    if persistedEvents[event.Type] {
        eb.persister.Persist(event)
    }
}
```

### 5. DSPy-Go Actor

Wraps a dspy-go ReActAgent as a reactive actor:

```go
type DspyActor struct {
    id              string
    namespace       string
    agent           *dspygo.ReActAgent
    mailboxCh       chan *mailbox.Message
    state           atomic.Value
    shortTermMemory *ShortTermMemory      // See: actor-progressive-memory.md
    memoryManager   *MemoryManager
    handlers        map[mailbox.MessageType]MessageHandler
}

type MessageHandler func(ctx context.Context, msg *mailbox.Message) (*mailbox.Message, error)
```

## Message Flow

```
1. External message arrives (CLI, hook, another actor)
        │
        ▼
2. mailbox.Send() inserts into SQLite
        │
        ▼
3. INSERT trigger fires → mailbox_notify row created
        │
        ▼
4. MailboxWatcher detects notification (~50ms)
        │
        ▼
5. Watcher sends WakeUp{namespace} to Supervisor (signal only!)
        │
        ▼
6. Supervisor checks: actor idle?
        │
        ├── No → ignore (actor will poll when done)
        │
        └── Yes ↓
                │
                ▼
7. Supervisor.Poll(namespace, leaseTimeout) → atomically claims message
        │
        ├── No message available → done (already claimed by another)
        │
        └── Message claimed ↓
                │
                ▼
8. Actor.OnMailReceived() invoked
        │
        ▼
9. Actor processes with dspy-go (tool calls, etc.)
        │
        ├── Crash → lease expires → message becomes visible → retry
        │
        └── Success ↓
                │
                ▼
10. Actor sends reply (if ask message)
        │
        ▼
11. Actor acks original message (removes from queue)
        │
        ▼
12. EventBus emits mail.acked event (persisted to trajectory.db)
```

**Failure scenarios:**

| Scenario                     | Behavior                                               |
| ---------------------------- | ------------------------------------------------------ |
| Actor crashes mid-processing | Lease expires → message visible again → retry          |
| Actor returns error          | Nack with backoff → message requeued with delay        |
| Max retries exceeded         | Message moved to dead letter / human escalation        |
| Supervisor crashes           | All leases expire → messages become visible on restart |

## Integration with Existing Components

### Storage Integration

| Store           | Purpose in Actor System               |
| --------------- | ------------------------------------- |
| `mailbox.db`    | Message queue, triggers notifications |
| `agents.db`     | Actor metadata, lifecycle state       |
| `sessions.db`   | Context persistence, turn history     |
| `memory.db`     | Long-term learnings, gotchas          |
| `graph.db`      | Task→file relationships               |
| `trajectory.db` | Tool call traces for debugging        |
| `tasks.db`      | Current task context                  |

### Hook Integration

Hooks emit events to the EventBus:

- `PreToolUse` → `hook.triggered` event
- `PostToolUse` → `hook.triggered` event
- Hook blocks → `hook.blocked` event

Actors can subscribe to hook events for coordination.

### Session Lineage

Actors participate in session lineage:

- Each actor run creates/continues a session
- `parent_session_id` tracks lineage
- Context restoration on actor restart
- Session summaries for long-running actors

## CLI Commands

```bash
# Start the actor supervisor (long-running daemon)
agentctl actor supervisor start
agentctl actor supervisor status
agentctl actor supervisor stop

# Spawn an actor (registers with supervisor)
agentctl actor spawn --role coder --namespace coder-1 --llm gemini

# Send message (reactive delivery)
agentctl actor send coder-1 --type ask --payload '{"task": "implement feature"}'

# Subscribe to events (for debugging/monitoring)
agentctl actor events --filter "mail.*" --format ndjson

# Actor status
agentctl actor status
# Output:
# NAMESPACE    ROLE      STATE       QUEUE  LAST_MSG
# coder-1      coder     processing  3      2s ago
# planner-1    planner   idle        0      5m ago
# reviewer-1   reviewer  idle        0      1h ago

# Actor logs
agentctl actor logs coder-1 --follow
```

## Supervision Strategies

### One-for-One

Default strategy. Only restart the failed actor:

```go
SupervisionStrategy{
    MaxRestarts:    3,
    RestartWindow:  time.Minute,
    BackoffInitial: time.Second,
    BackoffMax:     time.Minute,
}
```

### Escalate

Escalate to human when actor repeatedly fails:

```go
func (a *Actor) OnError(ctx context.Context, err error) Directive {
    if isRecoverable(err) {
        return DirectiveResume
    }
    if a.restartCount < 3 {
        return DirectiveRestart
    }
    // Too many failures, need human intervention
    a.notifyHuman(err)
    return DirectiveStop
}
```

## Migration Path

| Phase | Current                  | New                         | Effort |
| ----- | ------------------------ | --------------------------- | ------ |
| 1     | Poll-based daemon        | Add MailboxWatcher          | Low    |
| 2     | Single agent per process | Supervisor manages multiple | Medium |
| 3     | Manual message routing   | Event-driven routing        | Medium |
| 4     | No event subscriptions   | EventBus with pub/sub       | Medium |
| 5     | Hooks are external       | Hooks emit to EventBus      | Low    |

### Phase 1: MailboxWatcher (Low effort)

Add reactive notifications without changing agent structure:

1. Create `mailbox_notify` table and trigger
2. Implement `MailboxWatcher`
3. Replace `Poll()` loop with watcher channel

### Phase 2: Supervisor (Medium effort)

Centralize actor management:

1. Implement `Supervisor` struct
2. Convert agents to `Actor` interface
3. Single supervisor process, multiple actors

### Phase 3-5: Full Event System

Complete the reactive architecture with EventBus and integrations.

## File Structure

```
internal/actor/
├── actor.go           # Actor interface and types
├── supervisor.go      # Supervisor implementation
├── watcher.go         # SQLite change notifications
├── event_bus.go       # Event routing and pub/sub
├── dspy_actor.go      # DSPy-Go actor wrapper
├── memory/            # Actor memory management
│   └── ...            # See: actor-progressive-memory.md
└── handlers/
    ├── coder.go       # Coder message handlers
    ├── planner.go     # Planner message handlers
    └── reviewer.go    # Reviewer message handlers

cmd/agentctl/cmd/
├── actor.go           # Actor CLI commands (extend existing)
└── supervisor.go      # Supervisor commands
```

## Design Decisions

These questions have been resolved:

### 1. Single vs multi-supervisor

**Decision:** Single supervisor per workspace for MVP.

- Simpler deployment and debugging
- Can evolve to multi-supervisor later if needed
- Workspace isolation is handled at the SQLite level (separate DBs)

### 2. Actor persistence

**Decision:** Actors are ephemeral; messages are durable.

- Actor config stored in `agents.db` (can respawn on supervisor restart)
- In-flight messages: lease expires → message becomes visible → reprocessed
- No need for actor state persistence beyond what's in sessions.db

### 3. Backpressure

**Decision:** Queue depth stays in SQLite; sequential actor processing.

- **Per-actor concurrency = 1** (MVP): Supervisor only claims next message when
  actor is idle
- **No in-memory buffering**: SQLite is the queue, no mailbox overflow possible
- **Sender never blocks**: `mailbox.Send()` always succeeds (INSERT into SQLite)
- Later: Add per-actor concurrency > 1 for specific message types if needed

### 4. Priority queues

**Decision:** Defer to later phase.

- MVP: FIFO ordering within namespace
- Future: Add `priority` column to mailbox, modify Poll() to order by priority
- Interrupt messages (e.g., "stop") can use a separate high-priority namespace

## Future Considerations

- **Multi-supervisor**: For large deployments, support multiple supervisors with
  leader election
- **Actor migration**: Move actors between supervisors for load balancing
- **Distributed mailbox**: Replace SQLite with distributed queue for multi-node
- **Actor hierarchies**: Parent-child actor relationships with cascading
  supervision
- **Message priorities**: Add priority lanes for urgent/interrupt messages

## Related Documents

- [Actor Progressive Memory](./actor-progressive-memory.md) - Context management
- [Unified Session Lineage](./unified-session-lineage.md) - Session tracking
- [DSPy-Go Agents Spec](../spec/dspy_go_agents.md) - Agent implementation
