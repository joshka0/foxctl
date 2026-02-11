# Plan 04: Turn Serialization and Distribution

**Status**: Proposed
**Depends on**: 01-principal-and-tenant-isolation, 03-river-background-jobs
**Blocks**: None (final scaling plan)

## Problem

The current architecture has two critical concurrency issues:

1. **No turn serialization**: `companion.Service.Chat()` has no mutex. Two concurrent calls for the same conversation can interleave `buildChatMessages()` and `storeConversationTurns()`, producing inconsistent history.

2. **Single-process sessions**: `consolews.Hub` stores all sessions in a `map[string]*Session` in memory. No session affinity, no shared state, no external store. Cannot scale to N pods.

3. **Last-writer-wins**: Both `SessionBridge` and `consolews.Session` cancel the previous in-flight request on new message. Breaks in group channels with concurrent users.

## Design: Incremental (Not Proto.Actor)

### Why Not Proto.Actor

Proto.Actor cluster is alpha in Go. Its value proposition (virtual actor placement across a cluster) only matters at >100k concurrent conversations. Before that:

- **Postgres advisory locks** serialize turns per conversation across pods
- **River job claiming** provides distributed work distribution via `SKIP LOCKED`
- **Consistent hash routing** at the k8s ingress level provides session affinity

These three primitives cover horizontal scaling to ~50 pods with zero new infrastructure.

### Phase 4a: Per-Conversation Turn Serialization (Single Pod)

Fix the concurrency bug without changing deployment model.

```go
// internal/companion/turnlock.go
package companion

import "sync"

// TurnLock provides per-conversation mutual exclusion for turn processing.
type TurnLock struct {
    mu    sync.Mutex
    locks map[string]*sync.Mutex
}

func NewTurnLock() *TurnLock {
    return &TurnLock{locks: make(map[string]*sync.Mutex)}
}

// Acquire returns a mutex for the given conversation ID.
// Callers must Lock/Unlock the returned mutex.
func (tl *TurnLock) Acquire(conversationID string) *sync.Mutex {
    tl.mu.Lock()
    defer tl.mu.Unlock()
    m, ok := tl.locks[conversationID]
    if !ok {
        m = &sync.Mutex{}
        tl.locks[conversationID] = m
    }
    return m
}

// Evict removes the mutex for a conversation (call on session end).
func (tl *TurnLock) Evict(conversationID string) {
    tl.mu.Lock()
    defer tl.mu.Unlock()
    delete(tl.locks, conversationID)
}
```

Wire into `companion.Service.Chat()`:

```go
func (s *Service) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    mu := s.turnLock.Acquire(req.ConversationID)
    mu.Lock()
    defer mu.Unlock()

    // ... existing turn pipeline (now serialized per conversation)
}
```

Wire into `SessionBridge.HandleMessage()`:

Replace the "cancel previous request" pattern with queued processing:

```go
func (sb *SessionBridge) HandleMessage(ctx context.Context, evt MessageEvent) error {
    // Instead of cancelling previous, wait for turn lock
    mu := sb.turnLock.Acquire(evt.ChannelID)
    mu.Lock()
    defer mu.Unlock()

    // ... process message (now serialized per channel)
}
```

### Phase 4b: Postgres Advisory Locks (Multi-Pod)

For multi-pod deployments, replace in-memory `TurnLock` with Postgres advisory locks.

```go
// internal/companion/turnlock_pg.go
package companion

import (
    "context"
    "database/sql"
    "hash/fnv"
)

// PgTurnLock uses Postgres advisory locks for cross-pod turn serialization.
type PgTurnLock struct {
    db *sql.DB
}

func NewPgTurnLock(db *sql.DB) *PgTurnLock {
    return &PgTurnLock{db: db}
}

// conversationLockID generates a stable int64 from conversation ID for pg_advisory_lock.
func conversationLockID(conversationID string) int64 {
    h := fnv.New64a()
    _, _ = h.Write([]byte(conversationID))
    return int64(h.Sum64())
}

// WithTurnLock executes fn while holding an advisory lock for the conversation.
// The lock is automatically released when the transaction commits or rolls back.
func (pl *PgTurnLock) WithTurnLock(ctx context.Context, conversationID string, fn func(ctx context.Context) error) error {
    tx, err := pl.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    lockID := conversationLockID(conversationID)
    _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockID)
    if err != nil {
        return fmt.Errorf("acquire advisory lock: %w", err)
    }

    if err := fn(ctx); err != nil {
        return err
    }

    return tx.Commit()
}

// TryWithTurnLock attempts to acquire the lock without blocking.
// Returns false if the lock is held by another session.
func (pl *PgTurnLock) TryWithTurnLock(ctx context.Context, conversationID string, fn func(ctx context.Context) error) (bool, error) {
    tx, err := pl.db.BeginTx(ctx, nil)
    if err != nil {
        return false, err
    }
    defer tx.Rollback()

    lockID := conversationLockID(conversationID)
    var acquired bool
    err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1)", lockID).Scan(&acquired)
    if err != nil {
        return false, err
    }
    if !acquired {
        return false, nil
    }

    if err := fn(ctx); err != nil {
        return true, err
    }

    return true, tx.Commit()
}
```

### Phase 4c: River-Based Conversation Routing (Multi-Pod)

Instead of sticky sessions, each incoming turn becomes a River job. River's `SKIP LOCKED` claiming ensures exactly one worker processes each turn.

```go
// internal/jobs/workers/conversation_turn.go
type ConversationTurnArgs struct {
    ConversationID string          `json:"conversation_id"`
    Content        string          `json:"content"`
    PrincipalJSON  json.RawMessage `json:"principal"`
    Platform       string          `json:"platform"`
    ChannelID      string          `json:"channel_id"`
    ReplyTo        string          `json:"reply_to,omitempty"`
}

func (ConversationTurnArgs) Kind() string { return "conversation.turn" }

// InsertOpts returns options that ensure FIFO per conversation.
func (a ConversationTurnArgs) InsertOpts() river.InsertOpts {
    return river.InsertOpts{
        Queue:    "conversations",
        UniqueOpts: river.UniqueOpts{
            // Only one pending turn per conversation at a time
            ByArgs: true,
            ByQueue: true,
            ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning},
        },
    }
}

type ConversationTurnWorker struct {
    river.WorkerDefaults[ConversationTurnArgs]
    companionService *companion.Service
    turnLock         *PgTurnLock // advisory lock within River worker
}

func (w *ConversationTurnWorker) Work(ctx context.Context, job *river.Job[ConversationTurnArgs]) error {
    return w.turnLock.WithTurnLock(ctx, job.Args.ConversationID, func(ctx context.Context) error {
        // Process the turn through companion.Service.Chat()
        // Advisory lock ensures no other pod processes this conversation concurrently
    })
}
```

### Phase 4d: K8s Session Affinity (Optional Optimization)

For WebSocket connections, k8s can route based on conversation ID hash:

```yaml
# deploy/kubernetes/base/deployment.yaml
spec:
  template:
    metadata:
      annotations:
        # Sticky sessions for WebSocket connections
        nginx.ingress.kubernetes.io/affinity: "cookie"
        nginx.ingress.kubernetes.io/session-cookie-name: "agentctl-affinity"
        nginx.ingress.kubernetes.io/session-cookie-hash: "sha1"
```

This is an optimization (avoid re-creating consolews sessions on every request), not a correctness requirement. The advisory lock + River approach handles correctness.

## Files to Create

| File | Purpose |
|------|---------|
| `internal/companion/turnlock.go` | In-memory per-conversation mutex (Phase 4a) |
| `internal/companion/turnlock_pg.go` | Postgres advisory lock implementation (Phase 4b) |
| `internal/companion/turnlock_test.go` | Tests for both implementations |
| `internal/jobs/workers/conversation_turn.go` | River-based turn processing (Phase 4c) |

## Files to Modify

| File | Change |
|------|--------|
| `internal/companion/service.go` | Add TurnLock; serialize Chat() per conversation |
| `internal/chatadapter/session_bridge.go` | Replace cancel-previous with turn lock |
| `internal/web/server.go` | Initialize TurnLock (memory or Postgres based on config) |

## Implementation Order

1. **Phase 4a** (do now): `TurnLock` with in-memory mutexes. Fixes the concurrency bug. Zero dependencies.
2. **Phase 4b** (when deploying multi-pod): `PgTurnLock` with advisory locks. Swap implementation based on config.
3. **Phase 4c** (when needed): River conversation turn worker. Removes need for session affinity.
4. **Phase 4d** (optimization): K8s sticky sessions for WebSocket. Reduces session recreation.

Phase 4a is a **bug fix** and should ship immediately. Phases 4b-4d are scaling work.

## Scaling Characteristics

| Pods | Approach | Latency | Throughput |
|------|----------|---------|------------|
| 1 | In-memory TurnLock (4a) | ~0ms overhead | Limited by CPU/memory |
| 2-10 | Pg advisory locks (4b) | ~1-5ms per lock | Limited by Postgres connections |
| 10-50 | River routing (4c) | ~10-50ms (job claim) | Limited by River workers |
| 50+ | Consider Proto.Actor | Needs evaluation | Actor placement overhead |

For reference: a single Postgres instance handles ~10k advisory lock operations/second. That's ~10k conversation turns/second across all pods, well beyond typical chat platform throughput.

## What About Proto.Actor?

**Defer until Phase 4c proves insufficient.** Proto.Actor's value is:

1. **Virtual actor placement**: Automatic grain migration on node failure
2. **Location transparency**: Client doesn't know which node hosts the grain
3. **In-memory state**: Grain keeps conversation state in memory (faster than DB)

None of these matter until you're processing >10k concurrent conversations. At that scale:

- Postgres advisory locks start contending
- River job claiming overhead becomes measurable
- In-memory state on the grain avoids repeated DB reads

**When to revisit**: If Postgres advisory lock wait times exceed 50ms p99, or River job claim latency exceeds 100ms p95, evaluate Proto.Actor cluster.

## Verification

1. **Phase 4a**: Two concurrent `Chat()` calls to same conversation are serialized (test with `sync.WaitGroup` + channel)
2. **Phase 4a**: Different conversations process concurrently (no global lock)
3. **Phase 4b**: Two pods processing same conversation — one blocks, then proceeds
4. **Phase 4b**: `TryWithTurnLock` returns false when lock held
5. **Phase 4c**: River worker processes turns in FIFO order per conversation
6. Load test: 100 concurrent conversations, 10 messages each — no interleaving
