# Wide Events Observability

This document describes the wide events observability system, implementing
principles from [loggingsucks.com](https://loggingsucks.com).

## Overview

Wide events (also known as "canonical log lines") are comprehensive observability
events that capture the full context of an operation in a single event. Unlike
narrow events that log what code does, wide events log **what happened to the
request**.

### Key Principles

1. **One event per operation** - A single event captures all context needed for
   debugging
2. **Business context included** - Session IDs, workspace IDs, not just
   technical details
3. **Tail sampling** - Always sample errors and slow requests; randomly sample
   healthy ones
4. **Extensible data** - Domain-specific metrics in a flexible `data` map

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENTCTL_OBS_DIR` | - | Root observability directory (required to enable) |
| `AGENTCTL_OBS_SAMPLE_ERRORS` | `true` | Always sample error events |
| `AGENTCTL_OBS_SLOW_THRESHOLD_MS` | `1000` | Slow request threshold (ms) |
| `AGENTCTL_OBS_SAMPLE_RATE` | `0.05` | Random sample rate for healthy events (0.0-1.0) |
| `AGENTCTL_TRACE_ID` | auto | Propagate trace ID to child processes |

### Enabling Wide Events

Set `AGENTCTL_OBS_DIR` to enable observability:

```bash
export AGENTCTL_OBS_DIR=~/.agentctl/observability
```

Events are written to `$AGENTCTL_OBS_DIR/events/wide_events.ndjson`.

## Event Schema

Each line in the NDJSON file is a `WideEvent`:

```json
{
  "ts": "2025-12-27T15:30:00Z",
  "trace_id": "01JFXYZ...",
  "span_id": "01JFXYZ...",
  "parent_id": "",
  "service": "agentctl",
  "version": "0.1.0",
  "component": "skill",
  "operation": "skill.run",
  "command": "code/snippet_extract",
  "session_id": "sess_abc123",
  "agent_id": "agent_def456",
  "workspace_id": "my-project",
  "job_id": "",
  "status": "ok",
  "duration_ms": 250,
  "error_type": "",
  "error_code": "",
  "error_message": "",
  "retriable": null,
  "data": {
    "source": "run",
    "cache_hit": false,
    "files": 10
  }
}
```

### Field Reference

#### Identity Fields

| Field | Type | Description |
|-------|------|-------------|
| `ts` | RFC3339 | Event timestamp (UTC) |
| `trace_id` | ULID | Correlates all events in an operation |
| `span_id` | ULID | Unique ID for this specific event |
| `parent_id` | ULID | Parent span for nested operations |

#### Service Metadata

| Field | Type | Description |
|-------|------|-------------|
| `service` | string | Always "agentctl" |
| `version` | string | Build version |
| `component` | string | "cli", "web", "hook", "skill", "job" |

#### Operation Context

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | "skill.run", "hook.execute", "job.submit", etc. |
| `command` | string | Skill/hook/command name |
| `subtype` | string | Additional classification |

#### Business Context

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | From `AGENTCTL_SESSION_ID` |
| `agent_id` | string | From `AGENTCTL_AGENT_ID` |
| `workspace_id` | string | Logical workspace |
| `job_id` | string | Background job ID |

#### Outcome

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | "ok", "error", "canceled" |
| `duration_ms` | int64 | Wall-clock duration |

#### Error Details (when status=error)

| Field | Type | Description |
|-------|------|-------------|
| `error_type` | string | Category: "timeout", "permission", "validation", etc. |
| `error_code` | string | Machine-readable error code |
| `error_message` | string | Human-readable message |
| `retriable` | bool | Whether operation can be retried |

#### Domain Data

| Field | Type | Description |
|-------|------|-------------|
| `data` | map | Skill-specific metrics (counts, booleans, hashes) |

## Usage Examples

### Building Events

```go
import "github.com/jkatigb/agentctl/internal/observability"

// Create an event builder
event := observability.NewEvent(observability.OpSkillRun).
    WithComponent(observability.ComponentSkill).
    WithCommand("code/snippet_extract").
    WithWorkspace(workspaceID).
    EnrichFromEnv().
    EnrichFromContext(ctx)

start := time.Now()

// ... perform operation ...

if err != nil {
    observability.Emit(ctx, event.Error(err, time.Since(start)))
} else {
    observability.Emit(ctx, event.
        WithData("files", fileCount).
        WithData("cache_hit", false).
        Success(time.Since(start)))
}
```

### Context Propagation

```go
// Ensure trace ID exists (generates if needed)
ctx, traceID := observability.EnsureTraceID(ctx)

// Get trace ID from context
traceID := observability.TraceIDFromContext(ctx)

// Propagate to child processes via env
env := observability.PropagationEnv(ctx)
// Returns: ["AGENTCTL_TRACE_ID=01JFXYZ..."]
```

### Custom Sampling

```go
// Use custom sampler for testing
observability.SetSamplerForTesting(observability.SampleAll{})

// Or create a configured sampler
sampler := observability.NewTailSampler(
    true,  // sampleErrors
    1000,  // slowThresholdMS
    0.10,  // randomRate (10%)
)
```

## Tail Sampling Strategy

Following loggingsucks.com recommendations:

1. **Always sample errors** - All events with `status=error` are recorded
2. **Always sample canceled** - Context cancellations often indicate problems
3. **Always sample slow** - Events exceeding `slowThresholdMS` threshold
4. **Random sample healthy** - Configurable % of fast, successful events

Default configuration samples:
- 100% of errors
- 100% of events > 1 second
- 5% of healthy, fast events

## Consumption Patterns

### OTEL Collector

```yaml
receivers:
  filelog:
    include:
      - /path/to/observability/events/*.ndjson
    start_at: beginning

processors:
  json_parser:
    parse_from: body
    timestamp:
      parse_from: attributes.ts
      layout: '%Y-%m-%dT%H:%M:%SZ'

exporters:
  otlp:
    endpoint: "localhost:4317"
```

### Querying with jq

```bash
# Find all errors in the last hour
jq -c 'select(.status == "error")' wide_events.ndjson

# Find slow skill runs
jq -c 'select(.operation == "skill.run" and .duration_ms > 1000)' wide_events.ndjson

# Group by command
jq -s 'group_by(.command) | map({command: .[0].command, count: length})' wide_events.ndjson
```

## Migration from Narrow Events

The existing `SweGrepEvent` and other narrow events continue to work. Wide
events provide additional comprehensive context. For skills that emit both:

- Narrow events go to `$AGENTCTL_OBS_DIR/events/<skill_name>.ndjson`
- Wide events go to `$AGENTCTL_OBS_DIR/events/wide_events.ndjson`

Eventually, narrow events may be deprecated in favor of the unified wide event
format with domain-specific data in the `data` map.

## Storage & Retention

### Directory Structure

Wide events are stored under the observability directory:

```
~/.agentctl/observability/
└── events/
    ├── wide_events.ndjson    # Main wide event stream
    └── code_swe_grep.ndjson  # Legacy narrow events (if enabled)
```

The default observability path is `~/.agentctl/observability`, configurable via:
- Config file: `paths.observability`
- Environment: `AGENTCTL_OBS_DIR` (overrides config)

### Backup Integration

Observability logs are included in agentctl backups as the `observability`
component:

```bash
# Include observability in backup (default)
agentctl backup create

# Exclude observability from backup
agentctl backup create --exclude observability

# Backup only observability
agentctl backup create --components observability
```

**Retention in backups**: Only event files modified in the last 7 days are
included to prevent backup size bloat.

### Log Pruning

Events can be pruned by age or size to manage disk usage:

```go
import "github.com/jkatigb/agentctl/internal/observability"

// Prune events older than 30 days
opts := observability.PruneOptions{
    OlderThan: 30 * 24 * time.Hour,
    DryRun:    false,
}
result, err := observability.Prune(ctx, obsDir, opts)

// Prune by size (keep newest events up to 100MB)
result, err := observability.PruneBySize(ctx, obsDir, 100*1024*1024, false)
```

**Prune strategies**:

| Strategy | Use Case |
|----------|----------|
| `Prune()` with `OlderThan` | Time-based retention (e.g., 30 days) |
| `PruneBySize()` | Disk space constraints |
| `DryRun: true` | Preview what would be deleted |

**Default retention**: 30 days (via `DefaultPruneOptions()`)

### Recommended Retention Policy

For most deployments:

1. **Disk space**: Keep observability logs under 100MB with periodic size-based
   pruning
2. **Time-based**: Retain 7-30 days depending on debugging needs
3. **Backup frequency**: Weekly backups capture the last 7 days of events

Example cron job for daily pruning:

```bash
# Prune events older than 7 days
0 2 * * * agentctl observability prune --older-than 7d
```
