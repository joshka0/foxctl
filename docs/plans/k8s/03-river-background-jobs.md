# Plan 03: River Background Jobs

**Status**: Proposed
**Depends on**: 01-principal-and-tenant-isolation (for tenant context in jobs)
**Blocks**: Approval workflows (Phase 3b)

## Problem

Background work is currently handled by homegrown primitives:

- **Mailbox store**: SQS-style queue with visibility-timeout polling. No `SKIP LOCKED`, no `LISTEN/NOTIFY`, no DLQ, no scheduled delivery.
- **Agent daemon**: Single-process polling loop with 2-5s latency. No horizontal scaling.
- **Compression daemon**: Background goroutines with `sync.Mutex` rate limiting. No persistence, no retry, no observability.
- **Job store**: Metadata-only state machine (queued→running→ok/error). No worker claiming, no retry policy, no scheduling.
- **Heartbeat**: Periodic ticker in daemon goroutine. Lost on process restart.

These work for single-process deployments but break under multi-pod k8s.

## Why River (Not Temporal)

| Factor | River | Temporal |
|--------|-------|----------|
| Infrastructure | Library (no server) | Server cluster (3+ pods) |
| Storage | Your existing Postgres | Separate DB (Cassandra/Postgres) |
| Enqueuing | `InsertTx` in your app tx | gRPC to Temporal server |
| Atomicity | Same-transaction writes | Eventual (start workflow API) |
| Complexity | ~200 LOC integration | SDK + server + worker fleet |
| Scheduling | Periodic jobs + `run_at` | Durable timers + cron |
| Fit | Background tasks + retry | Multi-service sagas + long waits |

River covers compression, heartbeat, scheduled tasks, and approval execution. Temporal adds value only for multi-step workflows with human-in-the-loop waits spanning days — defer that.

## Design

### River Client as a Shared Service

```go
// internal/jobs/river.go
package jobs

import (
    "context"
    "database/sql"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/riverqueue/river"
    "github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Config configures the River job client.
type Config struct {
    Pool         *pgxpool.Pool
    MaxWorkers   int // default 25
    Queues       map[string]int // queue name -> max workers
}

// NewClient creates a River client with registered workers.
func NewClient(cfg Config, workers *river.Workers) (*river.Client[pgx.Tx], error) {
    queues := map[string]river.QueueConfig{
        river.QueueDefault: {MaxWorkers: cfg.MaxWorkers},
    }
    for name, max := range cfg.Queues {
        queues[name] = river.QueueConfig{MaxWorkers: max}
    }

    return river.NewClient(riverpgxv5.New(cfg.Pool), &river.Config{
        Queues:  queues,
        Workers: workers,
    })
}
```

### Job Types

#### 1. Compression Job

Replaces `companion.CompressionDaemon` goroutines.

```go
// internal/jobs/workers/compression.go
package workers

type CompressDailyArgs struct {
    ConversationID string `json:"conversation_id"`
    Date           string `json:"date"` // YYYY-MM-DD
}

func (CompressDailyArgs) Kind() string { return "companion.compress_daily" }

type CompressDailyWorker struct {
    river.WorkerDefaults[CompressDailyArgs]
    memory *companion.ConversationMemory
}

func (w *CompressDailyWorker) Work(ctx context.Context, job *river.Job[CompressDailyArgs]) error {
    return w.memory.CompressDay(ctx, job.Args.ConversationID, job.Args.Date)
}
```

#### 2. Agent Heartbeat Check

Replaces daemon heartbeat ticker. Periodic job checks for stale agents.

```go
type AgentHeartbeatCheckArgs struct{}

func (AgentHeartbeatCheckArgs) Kind() string { return "agent.heartbeat_check" }

type AgentHeartbeatCheckWorker struct {
    river.WorkerDefaults[AgentHeartbeatCheckArgs]
    agentStore *agents.Store
}

func (w *AgentHeartbeatCheckWorker) Work(ctx context.Context, job *river.Job[AgentHeartbeatCheckArgs]) error {
    // Find agents with stale heartbeats and transition to error state
    return w.agentStore.RecoverStaleAgents(ctx, 2*time.Minute)
}
```

#### 3. Approval Execution

When a user approves a proposed tool call, enqueue execution as a River job.

```go
type ApprovalExecuteArgs struct {
    ProposalID     string         `json:"proposal_id"`
    PrincipalJSON  json.RawMessage `json:"principal"`
    ToolName       string         `json:"tool_name"`
    ToolArgs       json.RawMessage `json:"tool_args"`
    ConversationID string         `json:"conversation_id"`
}

func (ApprovalExecuteArgs) Kind() string { return "approval.execute" }

type ApprovalExecuteWorker struct {
    river.WorkerDefaults[ApprovalExecuteArgs]
    toolRunner *engine.ToolRunner
    enforcer   *casbin.Enforcer // re-check authorization at execution time
}

func (w *ApprovalExecuteWorker) Work(ctx context.Context, job *river.Job[ApprovalExecuteArgs]) error {
    // 1. Load proposal, verify status == approved
    // 2. Re-check Casbin authorization
    // 3. Execute tool
    // 4. Store result
    // 5. Notify conversation
}
```

#### 4. Scheduled Message Delivery

Replace mailbox `visible_at` with River's built-in scheduling.

```go
type ScheduledMessageArgs struct {
    ToNamespace string          `json:"to_ns"`
    Type        string          `json:"type"`
    Payload     json.RawMessage `json:"payload"`
}

func (ScheduledMessageArgs) Kind() string { return "mailbox.deliver" }
```

Enqueue with `river.InsertOpts{ScheduledAt: time.Now().Add(delay)}`.

### Periodic Jobs

```go
// Registered at client creation time
periodicJobs := []*river.PeriodicJob{
    // Check for conversations needing daily compression (every 30 min)
    river.NewPeriodicJob(
        river.PeriodicInterval(30*time.Minute),
        func() (river.JobArgs, *river.InsertOpts) {
            return CompressScanArgs{}, nil
        },
        &river.PeriodicJobOpts{RunOnStart: true},
    ),

    // Agent heartbeat check (every 60 seconds)
    river.NewPeriodicJob(
        river.PeriodicInterval(60*time.Second),
        func() (river.JobArgs, *river.InsertOpts) {
            return AgentHeartbeatCheckArgs{}, nil
        },
        nil,
    ),

    // Agent index janitor - Telegram (every 30 min)
    river.NewPeriodicJob(
        river.PeriodicInterval(30*time.Minute),
        func() (river.JobArgs, *river.InsertOpts) {
            return AgentIndexCleanupArgs{}, nil
        },
        nil,
    ),
}
```

## Files to Create

| File | Purpose |
|------|---------|
| `internal/jobs/client.go` | River client factory |
| `internal/jobs/workers/compression.go` | Daily/weekly compression workers |
| `internal/jobs/workers/heartbeat.go` | Agent heartbeat check worker |
| `internal/jobs/workers/approval.go` | Approval execution worker |
| `internal/jobs/workers/message.go` | Scheduled message delivery worker |
| `internal/jobs/workers/cleanup.go` | Index janitor worker |
| `internal/jobs/register.go` | Worker registration (all workers in one place) |

## Files to Modify

| File | Change |
|------|--------|
| `internal/web/server.go` | Create River client, start/stop with server lifecycle |
| `internal/companion/daemon.go` | Replace goroutine compression loops with River periodic jobs |
| `internal/companion/service.go` | Enqueue compression jobs after turns via `riverClient.InsertTx` |
| `internal/agent/daemon/daemon.go` | Replace heartbeat ticker with River periodic job |
| `go.mod` | Add `github.com/riverqueue/river`, `riverpgxv5`, `pgxpool` |

## Postgres-Native Mailbox Improvements

While River handles new job types, improve the existing mailbox for message delivery:

### Add `SKIP LOCKED` to Poll (Postgres path)

```go
// internal/storage/mailbox/store.go — Postgres-specific poll query
const pollQueryPostgres = `
    SELECT id, from_ns, to_ns, type, headers, payload, ts, visible_at, attempt
    FROM mailbox
    WHERE to_ns = $1 AND visible_at <= now()
    ORDER BY ts
    FOR UPDATE SKIP LOCKED
    LIMIT $2
`
```

This eliminates the optimistic-lock retry pattern (`RowsAffected == 0` check) for Postgres deployments.

### Add `LISTEN/NOTIFY` Wakeup

```go
// internal/storage/mailbox/notify.go
func (s *store) listenForMessages(ctx context.Context, namespace string) <-chan struct{} {
    ch := make(chan struct{}, 1)
    go func() {
        conn, _ := s.pool.Acquire(ctx)
        defer conn.Release()
        _, _ = conn.Exec(ctx, "LISTEN mailbox_"+namespace)
        for {
            _, err := conn.WaitForNotification(ctx)
            if err != nil {
                return
            }
            select {
            case ch <- struct{}{}:
            default:
            }
        }
    }()
    return ch
}
```

Plus a trigger on INSERT:

```sql
CREATE OR REPLACE FUNCTION notify_mailbox() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('mailbox_' || NEW.to_ns, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER mailbox_insert_notify
    AFTER INSERT ON mailbox
    FOR EACH ROW EXECUTE FUNCTION notify_mailbox();
```

## Migration Strategy

1. Add River dependency and client factory
2. Create worker types (no behavior change yet)
3. Wire River client into server lifecycle (Start/Stop)
4. Add periodic jobs (compression scan, heartbeat check)
5. Migrate compression daemon: enqueue River jobs instead of running goroutines
6. Migrate heartbeat: River periodic job replaces ticker
7. Add SKIP LOCKED to mailbox poll (Postgres only, SQLite unchanged)
8. Add LISTEN/NOTIFY (Postgres only, optional)

Steps 1-4 are additive. Steps 5-6 replace existing goroutines. Steps 7-8 are pure optimizations.

## River Migrations

River requires its own tables. Run via `river migrate-up`:

```bash
# In server startup or as a migration step
river migrate-up --database-url "$AGENTCTL_POSTGRES_DSN"
```

Or programmatically:

```go
migrator, _ := rivermigrate.New(riverpgxv5.New(pool), nil)
_, _ = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
```

## Verification

1. `go build ./internal/jobs/...` — compiles
2. `go test ./internal/jobs/...` — worker tests with mock dependencies
3. Integration: enqueue compression job, verify it runs
4. Integration: periodic heartbeat check triggers on schedule
5. Integration: approval execution worker re-checks Casbin before executing
6. Performance: mailbox poll latency drops from 2-5s to <100ms with LISTEN/NOTIFY
