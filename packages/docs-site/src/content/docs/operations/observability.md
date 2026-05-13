---
title: Observability
description: Events, persistence modes, inspection commands, and operational monitoring for foxctl.
---

foxctl provides an optional on-disk observability layer that captures structured
events without coupling the core runtime to any specific monitoring stack.

## Event Surfaces

| Surface | Purpose | Location |
|---------|---------|----------|
| NDJSON event streams | Local inspection, lightweight collectors | `$FOXCTL_OBS_DIR/events/*.ndjson` |
| SQLite event store | Queryable long-term event analysis | `$FOXCTL_OBS_DIR/events.db` |
| Structured logs | Runtime diagnostics | stderr (zerolog JSON) |
| Protocol v1 envelopes | Skill/job output | stdout |
| Jobs store | Skill/job state and execution metadata | `~/.foxctl/` storage |
| CAS | Large artifacts and evidence bundles | `~/.foxctl/cas/` |
| Sessions | Interaction history and continuity | `~/.foxctl/` storage |

## Enabling Observability

Observability is controlled by the `FOXCTL_OBS_DIR` environment variable. When set, foxctl appends NDJSON events to files under the configured directory. When unset or empty, all event writers are no-ops.

```bash
# Enable on-disk events
export FOXCTL_OBS_DIR=/var/log/foxctl

# Must be an absolute path — tilde is not expanded
# Wrong: FOXCTL_OBS_DIR=~/.foxctl/observability
# Correct:
export FOXCTL_OBS_DIR=$HOME/.foxctl/observability
```

## Directory Layout

When enabled, foxctl creates this structure:

```
$FOXCTL_OBS_DIR/
  events/
    core.ndjson               # CLI and core events
    code_swe_grep.ndjson      # Snippet extract events
    foxcular_events.ndjson    # General foxcular event stream
    <name>.ndjson             # Skill-specific streams
  events.db                   # SQLite event store (when SQL persistence is enabled)
```

Skill and subsystem file names use `[a-z0-9_.-]` only (flat namespace, no subdirectories).

### File semantics

- Files are **append-only** NDJSON streams — each line is a single JSON object.
- Writers never truncate or rotate files.
- Rotation, retention, and shipping are the operator's responsibility (logrotate, OTEL Collector, etc.).
- Writers tolerate missing directories by creating `$FOXCTL_OBS_DIR/events` before the first write.

## Event Schema

Each NDJSON line is a JSON object. For example, the `code/snippet_extract` skill emits:

```json
{
  "ts": "2025-12-03T17:19:11Z",
  "command": "code/snippet_extract",
  "workspace_id": "test-ws",
  "question_hash": "a1b2c3d4",
  "candidates": 3,
  "files_considered": 3,
  "files_relevant": 2,
  "snippets_emitted": 5,
  "has_artifact": true,
  "duration_ms": 187,
  "source": "run"
}
```

Key privacy properties:

- `question_hash` is a truncated SHA-256 (first 8 hex chars). Raw question text never appears.
- Snippet contents, file contents, and raw paths never appear — only aggregate counts and booleans.

## Persistence Modes

foxctl supports five persistence modes for events:

| Mode | Description | Use Case |
|------|-------------|----------|
| `PersistDefault` | NDJSON file (default) | Most events — fast, append-only |
| `PersistNDJSON` | Explicit NDJSON file | Same as default, explicitly chosen |
| `PersistSQL` | Direct SQLite write | High-value events needing immediate queryability |
| `PersistHybrid` | NDJSON + background SQLite sync | Fast writes plus queryability |
| `PersistNone` | No persistence | Debug/development events only |

### Choosing a persistence mode

**Use `PersistDefault` (NDJSON) for:**
- High-frequency, low-value events
- Events that don't need querying
- Performance-critical hot paths

**Use `PersistSQL` for:**
- Critical events that must be immediately queryable
- Low-frequency, high-value events (e.g., memory writes, goal completions)
- Auditing

**Use `PersistHybrid` for:**
- Events that need both fast writes and queryability
- Agent lifecycle, session events, and expensive AI operations

### Using persistence in Go code

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
    obs.WithPersistence(obs.PersistSQL),
)
defer func() { done(err) }()

// Hybrid persistence (recommended for queryable events)
ctx, done, span := obs.StartSpan(ctx, "agent.spawn",
    obs.WithCommand("agent/spawn"),
    obs.WithPersistence(obs.PersistHybrid),
)
defer func() { done(err) }()
```

## Background Sync

Hybrid persistence uses a background syncer that periodically reads from NDJSON files and writes to SQLite.

```go
import "github.com/joshka0/foxctl/internal/runtime/observability"

// Initialize persistence once at startup
if err := observability.InitPersistence(ctx); err != nil {
    log.Warn().Err(err).Msg("persistence init failed")
}
defer observability.ClosePersistence()
```

Default configuration:

| Setting | Default | Description |
|---------|---------|-------------|
| Sync interval | 30 seconds | How often to sync NDJSON to SQLite |
| Batch size | 100 | Max events to sync per run |

Custom configuration:

```go
config := observability.SyncConfig{
    Interval:  15 * time.Second,
    BatchSize: 200,
}
syncer := observability.NewSyncer(store, config)
syncer.Start()
defer syncer.Stop()
```

## SQLite Schema

Events persisted to SQLite use this schema:

```sql
CREATE TABLE foxcular_events (
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
```

Indexed fields: `trace_id`, `ts`, `operation`, `command`, `status`, `session_id`, `workspace_id`.

## Querying Events

### Common SQL queries

```sql
-- Recent agent spawn events
SELECT * FROM foxcular_events
WHERE operation = 'agent.spawn'
ORDER BY ts DESC LIMIT 10;

-- Errors in session summarization
SELECT * FROM foxcular_events
WHERE command = 'session/summarize' AND status = 'error';

-- Performance analysis by skill
SELECT command,
       COUNT(*) as count,
       AVG(duration_ms) as avg_ms,
       MAX(duration_ms) as max_ms
FROM foxcular_events
WHERE operation = 'skill.run'
GROUP BY command;

-- Slow operations (> 5 seconds)
SELECT operation, command, duration_ms, ts
FROM foxcular_events
WHERE duration_ms > 5000
ORDER BY duration_ms DESC;

-- Trace a specific request by ID
SELECT * FROM foxcular_events
WHERE trace_id = '01JFXYZ...'
ORDER BY ts;
```

### Extracting JSON data fields

```sql
SELECT
    command,
    json_extract(data, '$.cache_hit') as cache_hit,
    json_extract(data, '$.files') as files,
    duration_ms
FROM foxcular_events
WHERE operation = 'skill.run';
```

## Storage Locations

| File | Location | Purpose |
|------|----------|---------|
| NDJSON events | `$FOXCTL_OBS_DIR/events/foxcular_events.ndjson` | Default event stream |
| SQLite database | `$FOXCTL_OBS_DIR/events.db` | Queryable events |
| Custom NDJSON | `$FOXCTL_OBS_DIR/events/<name>.ndjson` | Skill-specific files |

## Context Updater

The context updater is a background worker that:

1. Polls active sessions.
2. Analyzes recent turns for topic/intent drift.
3. Queries candidate context.
4. Injects selected context with rate limiting and dedup safeguards.

It is part of the observability surface but runs as a separate subsystem within the runtime.

## Trace ID Propagation

`FOXCTL_TRACE_ID` propagates across processes and events, enabling correlation of operations that span multiple skills, agents, or sessions. This trace ID appears in both NDJSON events and SQLite records.

## Consumption Patterns

The NDJSON event stream is designed for out-of-process consumption:

### OTEL Collector

Use a `filelog` receiver pointing at `$FOXCTL_OBS_DIR/events/*.ndjson`. Each line is a log record with attributes matching the JSON fields. Standard OTEL processors can derive metrics such as `swe_grep_requests_total` and `swe_grep_duration_seconds`.

### Prometheus via node_exporter

Run a sidecar that tails NDJSON files, aggregates counters in memory, and periodically writes a textfile-format `.prom` snapshot into a directory watched by node_exporter's textfile collector. This keeps foxctl free of Prometheus-specific formatting.

## Extension Guidelines

When adding observability events for new skills or subsystems:

- Reuse the same directory and NDJSON conventions.
- Prefer small, aggregate fields (counts, booleans, hashes) over raw payloads.
- Avoid logging secrets, raw questions, or large blobs.
- Keep event schemas stable. For breaking changes, prefer additive fields or a new `<name>.ndjson` stream.
