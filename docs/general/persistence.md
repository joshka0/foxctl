# Observability Persistence

This document describes the persistence options for wide events, enabling SQLite
queryability alongside the default NDJSON file storage.

## Overview

By default, wide events are written to NDJSON files for fast, append-only
storage. For events that need queryability (debugging, analysis, auditing),
you can enable SQL persistence.

The recommended approach is **hybrid persistence**: fast NDJSON writes on the
hot path with background synchronization to SQLite for querying.

## Persistence Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `PersistDefault` | NDJSON file (default) | Most events - fast, append-only |
| `PersistNDJSON` | Explicit NDJSON file | Same as default, explicit |
| `PersistSQL` | Direct SQLite write | High-value events needing queryability |
| `PersistHybrid` | NDJSON + background SQLite sync | Best of both - fast writes + queryability |
| `PersistNone` | No persistence | Events only for sampling/logging |

## Using Persistence in Skills

### Via skillslib/obs (Recommended)

```go
import "github.com/joshka0/foxctl/internal/adapters/skillslib/obs"

// Default persistence (NDJSON)
ctx, done, span := obs.StartSpan(ctx, "skill.run",
    obs.WithCommand("code/snippet_extract"),
)
defer func() { done(err) }()

// SQL persistence for high-value events
ctx, done, span := obs.StartSpan(ctx, "skill.run",
    obs.WithCommand("session/summarize"),
    obs.WithPersistence(obs.PersistSQL),  // Direct SQLite write
)
defer func() { done(err) }()

// Hybrid persistence (recommended for queryable events)
ctx, done, span := obs.StartSpan(ctx, "agent.spawn",
    obs.WithCommand("agent/spawn"),
    obs.WithPersistence(obs.PersistHybrid),  // NDJSON + background SQLite
)
defer func() { done(err) }()

// Custom NDJSON file
ctx, done, span := obs.StartSpan(ctx, "memory.write",
    obs.WithCommand("memory/put"),
    obs.WithPersistenceFile("memory_operations"),  // Separate file
)
defer func() { done(err) }()
```

### Via internal/runtime/observability

```go
import "github.com/joshka0/foxctl/internal/runtime/observability"

// Using span options
ctx, done, span := observability.StartSpan(ctx, observability.OpSkillRun,
    observability.WithSpanCommand("session/summarize"),
    observability.WithSpanPersistence(observability.PersistHybrid),
)
defer func() { done(err) }()

// Using event builder directly
event := observability.NewEvent(observability.OpSkillRun).
    WithCommand("session/summarize").
    WithPersistence(observability.PersistSQL).
    EnrichFromEnv()

observability.Emit(ctx, event.Success(duration))
```

## High-Value Skills for SQL Persistence

The following skills should use `PersistSQL` or `PersistHybrid` for their events:

| Skill | Reason | Recommended Mode |
|-------|--------|------------------|
| `session/summarize` | Creates queryable learnings | `PersistHybrid` |
| `session/extract_learnings` | Extracts insights | `PersistHybrid` |
| `session/restore` | Session lifecycle tracking | `PersistHybrid` |
| `memory/put` | Memory write operations | `PersistSQL` |
| `codemap/generate` | Expensive AI operations | `PersistHybrid` |
| `agent/spawn` | Agent lifecycle events | `PersistHybrid` |
| `agent/run` | Agent execution tracking | `PersistHybrid` |
| `epic/complete` | Goal completion tracking | `PersistSQL` |

### Guidelines for Choosing Persistence Mode

**Use `PersistDefault` (NDJSON) for:**
- High-frequency, low-value events
- Events that don't need querying
- Performance-critical hot paths

**Use `PersistSQL` for:**
- Critical events that must be immediately queryable
- Low-frequency, high-value events
- Events used for auditing

**Use `PersistHybrid` for:**
- Events that need both fast writes and queryability
- Medium-frequency events
- Agent lifecycle and session events

**Use `PersistNone` for:**
- Debug/trace events during development
- Events captured elsewhere (e.g., in separate logs)

## Background Sync Configuration

The hybrid persistence mode uses a background syncer that periodically reads
from NDJSON files and writes to SQLite.

### Initialization

```go
import "github.com/joshka0/foxctl/internal/runtime/observability"

// Initialize persistence at startup (call once)
if err := observability.InitPersistence(ctx); err != nil {
    log.Warn().Err(err).Msg("persistence init failed")
}
defer observability.ClosePersistence()
```

### Default Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| Sync Interval | 30 seconds | How often to sync NDJSON → SQLite |
| Batch Size | 100 | Max events to sync per run |

### Custom Configuration

```go
config := observability.SyncConfig{
    Interval:  15 * time.Second,  // Sync every 15s
    BatchSize: 200,               // Up to 200 events per sync
}
syncer := observability.NewSyncer(store, config)
syncer.Start()
defer syncer.Stop()
```

## SQLite Schema

Events persisted to SQLite use this schema:

```sql
CREATE TABLE wide_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    span_id TEXT NOT NULL UNIQUE,
    trace_id TEXT NOT NULL,
    parent_id TEXT,
    ts TEXT NOT NULL,
    service TEXT NOT NULL,
    version TEXT,
    component TEXT,
    operation TEXT NOT NULL,
    command TEXT,
    subtype TEXT,
    session_id TEXT,
    agent_id TEXT,
    workspace_id TEXT,
    job_id TEXT,
    status TEXT NOT NULL,
    duration_ms INTEGER,
    error_type TEXT,
    error_code TEXT,
    error_message TEXT,
    retriable INTEGER,
    data TEXT,  -- JSON
    created_at TEXT DEFAULT (datetime('now'))
);

-- Indexes for common queries
CREATE INDEX idx_wide_events_trace_id ON wide_events(trace_id);
CREATE INDEX idx_wide_events_ts ON wide_events(ts);
CREATE INDEX idx_wide_events_operation ON wide_events(operation);
CREATE INDEX idx_wide_events_command ON wide_events(command);
CREATE INDEX idx_wide_events_status ON wide_events(status);
CREATE INDEX idx_wide_events_session_id ON wide_events(session_id);
CREATE INDEX idx_wide_events_workspace_id ON wide_events(workspace_id);
```

## Querying Persisted Events

### Common Queries

```sql
-- Find all agent spawn events
SELECT * FROM wide_events
WHERE operation = 'agent.spawn'
ORDER BY ts DESC LIMIT 10;

-- Find errors in session summarization
SELECT * FROM wide_events
WHERE command = 'session/summarize' AND status = 'error';

-- Performance analysis by skill
SELECT command,
       COUNT(*) as count,
       AVG(duration_ms) as avg_ms,
       MAX(duration_ms) as max_ms
FROM wide_events
WHERE operation = 'skill.run'
GROUP BY command;

-- Find slow operations (> 5s)
SELECT operation, command, duration_ms, ts
FROM wide_events
WHERE duration_ms > 5000
ORDER BY duration_ms DESC;

-- Trace a specific request
SELECT * FROM wide_events
WHERE trace_id = '01JFXYZ...'
ORDER BY ts;

-- Session activity timeline
SELECT operation, command, status, duration_ms, ts
FROM wide_events
WHERE session_id = 'sess_abc123'
ORDER BY ts;
```

### Extracting Data from JSON

```sql
-- Extract specific data fields
SELECT
    command,
    json_extract(data, '$.cache_hit') as cache_hit,
    json_extract(data, '$.files') as files,
    duration_ms
FROM wide_events
WHERE operation = 'skill.run';

-- Filter by data field
SELECT * FROM wide_events
WHERE json_extract(data, '$.iteration') > 10;
```

## Storage Location

| File | Location | Purpose |
|------|----------|---------|
| NDJSON events | `$AGENTCTL_OBS_DIR/events/wide_events.ndjson` | Default event stream |
| SQLite database | `$AGENTCTL_OBS_DIR/events.db` | Queryable events |
| Custom NDJSON | `$AGENTCTL_OBS_DIR/events/<name>.ndjson` | Skill-specific files |

## Best Practices

1. **Initialize persistence once at startup** - Call `InitPersistence()` in your
   main entry point, not in each skill

2. **Use hybrid for most queryable events** - It provides the best balance of
   performance and queryability

3. **Use SQL for critical events only** - Direct SQL writes have higher latency;
   reserve for must-have events

4. **Group related events by custom files** - Use `WithPersistenceFile()` to
   separate high-volume event types

5. **Monitor sync lag** - If the syncer falls behind, consider increasing batch
   size or reducing sync interval

## Migration

Existing NDJSON events can be imported to SQLite:

```bash
# Manual import via jq + sqlite3
cat $AGENTCTL_OBS_DIR/events/wide_events.ndjson | \
  jq -c '.' | \
  while read line; do
    # Parse and insert each event
  done
```

Or use the syncer to catch up:

```go
// The syncer tracks its position and will sync all unprocessed events
syncer := observability.NewSyncer(store, observability.SyncConfig{
    Interval:  5 * time.Second,   // Fast catch-up
    BatchSize: 500,               // Large batches
})
syncer.Start()
// Wait for catch-up...
syncer.Stop()
```
