# Wide Events Observability

This document describes the foxcular events observability system, implementing
principles from [loggingsucks.com](https://loggingsucks.com).

## Overview

Foxcular events (also known as "canonical log lines") are comprehensive observability
events that capture the full context of an operation in a single event. Unlike
narrow events that log what code does, foxcular events log **what happened to the
request**.

### Key Principles

1. **One event per operation** - A single event captures all context needed for
   debugging. In foxctl, an operation is the unit of work (skill run, hook,
   job, agent iteration), linked via `parent_id`.
2. **Business context included** - Session IDs, workspace IDs, not just
   technical details
3. **Tail sampling** - Always sample errors and slow requests; randomly sample
   healthy ones
4. **Extensible data** - Domain-specific metrics in a flexible `data` map

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `FOXCTL_OBS_DIR` | - | Root observability directory (required to enable) |
| `FOXCTL_OBS_SAMPLE_ERRORS` | `true` | Always sample error events |
| `FOXCTL_OBS_SLOW_THRESHOLD_MS` | `1000` | Slow request threshold (ms) |
| `FOXCTL_OBS_SAMPLE_RATE` | `0.05` | Random sample rate for healthy events (0.0-1.0) |
| `FOXCTL_OBS_ALWAYS_SAMPLE_SESSIONS` | - | Always sample specific session IDs (comma-separated) |
| `FOXCTL_OBS_ALWAYS_SAMPLE_WORKSPACES` | - | Always sample specific workspace IDs (comma-separated) |
| `FOXCTL_TRACE_ID` | auto | Propagate trace ID to child processes |

### Enabling Wide Events

Set `FOXCTL_OBS_DIR` to enable observability:

```bash
export FOXCTL_OBS_DIR=~/.foxctl/observability
```

Events are written to `$FOXCTL_OBS_DIR/events/foxcular_events.ndjson`.

## Event Schema

Each line in the NDJSON file is a `Event`:

```json
{
  "ts": "2025-12-27T15:30:00Z",
  "trace_id": "01JFXYZ...",
  "span_id": "01JFXYZ...",
  "parent_id": "",
  "service": "foxctl",
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
| `service` | string | Always "foxctl" |
| `version` | string | Build version |
| `component` | string | "cli", "web", "hook", "skill", "job", "agent", "contextbuilder" |

#### Operation Context

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | "skill.run", "hook.execute", "job.submit", "agent.spawn", etc. |
| `command` | string | Skill/hook/command name |
| `subtype` | string | Additional classification |

#### Business Context

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | From `FOXCTL_SESSION_ID` |
| `agent_id` | string | From `FOXCTL_AGENT_ID` |
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

### Using skillslib/obs (Recommended for Skills)

The `skillslib/obs` package provides a clean API for skills to emit foxcular events
without importing internal packages directly:

```go
import "github.com/joshka0/foxctl/internal/adapters/skillslib/obs"

// Start a span for an operation
ctx, done, span := obs.StartSpan(ctx, "skill.run",
    obs.WithCommand("code/snippet_extract"),
    obs.WithWorkspace(workspaceID),
)
defer func() { done(err) }()

// Add data during operation
span.WithData("files", fileCount)
span.WithData("cache_hit", false)

// Span is automatically completed when done() is called
```

For structured logging within skills, use `obs.Logger`:

```go
log := obs.NewLogger("my/skill")
log.Info("processing files", obs.Int("count", 10), obs.Str("path", path))
log.Warn("slow operation", obs.Duration("elapsed", elapsed))
log.Error("failed", obs.Err(err))
```

### Building Events (Internal)

For internal packages, use `internal/runtime/observability` directly:

```go
import "github.com/joshka0/foxctl/internal/runtime/observability"

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
// Returns: ["FOXCTL_TRACE_ID=01JFXYZ..."]
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

1. **Always sample allowlisted/flagged** - Matching sessions/workspaces or `data.debug`/`data.always_sample`
2. **Always sample errors** - All events with `status=error` are recorded
3. **Always sample canceled** - Context cancellations often indicate problems
4. **Always sample slow** - Events exceeding `slowThresholdMS` threshold
5. **Random sample healthy** - Configurable % of fast, successful events

Default configuration samples:
- 100% of errors
- 100% of events > 1 second
- 5% of healthy, fast events

## V2 Semantic Retrieval Observability (PR-17)

V2 layered context assembly now tracks semantic artifact retrieval path usage
for dynamic context building:

- `vector` path (native libsql vector search)
- `fallback` path (in-process cosine fallback)
- `disabled` path (semantic query not configured or retriever unavailable)
- `error` path (semantic retriever call failed; context assembly still completes)

### Runtime Counters

`internal/v2/runtime/contextbuilder.Builder` exposes a point-in-time snapshot:

- `total_calls`
- `vector_calls`
- `fallback_calls`
- `disabled_calls`
- `error_calls`
- `total_hits`
- `vector_hits`
- `fallback_hits`
- `vector_capability_enabled_calls`
- `vector_capability_disabled_calls`
- `vector_capability_unknown_calls`
- vector latency buckets:
  - `vector_latency_le_10ms`
  - `vector_latency_le_50ms`
  - `vector_latency_le_100ms`
  - `vector_latency_gt_100ms`
- fallback latency buckets:
  - `fallback_latency_le_10ms`
  - `fallback_latency_le_50ms`
  - `fallback_latency_le_100ms`
  - `fallback_latency_gt_100ms`
- vector hit buckets:
  - `vector_hit_bucket_zero`
  - `vector_hit_bucket_one_to_three`
  - `vector_hit_bucket_four_to_ten`
  - `vector_hit_bucket_gt_ten`
- fallback hit buckets:
  - `fallback_hit_bucket_zero`
  - `fallback_hit_bucket_one_to_three`
  - `fallback_hit_bucket_four_to_ten`
  - `fallback_hit_bucket_gt_ten`

Layered context responses also include per-request metadata:

- `artifact_search_path`
- `artifact_vector_capability`
- `artifact_hit_count`
- `artifact_hit_bucket`
- `artifact_search_error` (only on non-fatal semantic retrieval failures)

### Wide Event Mapping (Implemented)

When exporting semantic retrieval behavior as foxcular events, use:

- `component`: `contextbuilder`
- `operation`: `context.semantic_artifact_search`
- `status`: `ok` for vector/fallback/disabled paths, `error` for retriever failures
- `trace_id`: inherited from request context when present
- `parent_id`: inherited from current span when present
- `data.search_path`: `vector|fallback|disabled|error`
- `data.vector_capability`: `enabled|disabled|unknown`
- `data.hit_count`: integer
- `data.latency_bucket`: `le_10ms|le_50ms|le_100ms|gt_100ms`
- `data.hit_bucket`: `zero|one_to_three|four_to_ten|gt_ten`
- `data.session_id`: v2 session ID
- `data.artifact_types`: optional list filter
- `data.min_similarity`: optional threshold

Example payload:

```json
{
  "component": "contextbuilder",
  "operation": "context.semantic_artifact_search",
  "status": "ok",
  "data": {
    "search_path": "fallback",
    "vector_capability": "disabled",
    "hit_count": 2,
    "latency_bucket": "le_50ms",
    "hit_bucket": "one_to_three",
    "session_id": "run-layered",
    "artifact_types": ["embedding", "annotation"],
    "min_similarity": 0.5
  }
}
```

Emission is best-effort and non-blocking with respect to context assembly:
semantic retrieval still returns normally even if observability persistence is
disabled or sampled out.

This keeps semantic retrieval telemetry aligned with wide-event principles: one
event per operation with both technical path and business context.

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
jq -c 'select(.status == "error")' foxcular_events.ndjson

# Find slow skill runs
jq -c 'select(.operation == "skill.run" and .duration_ms > 1000)' foxcular_events.ndjson

# Group by command
jq -s 'group_by(.command) | map({command: .[0].command, count: length})' foxcular_events.ndjson
```

## Agent Orchestration Events

Agent operations emit structured foxcular events for debugging multi-agent workflows.

### Operation Types

| Operation | Description | Key Data Fields |
|-----------|-------------|-----------------|
| `agent.spawn` | Agent spawn request | `role`, `task_len`, `spawned_session`, `spawned_actor`, `spawned_depth` |
| `agent.wait` | Waiting for child agents | `timeout_seconds`, `children_count`, `children_ids`, `all_completed` |
| `agent.iteration` | LLM iteration in agent loop | `iteration`, `prompt_tokens`, `completion_tokens`, `finish_reason`, `tool_calls` |
| `agent.complete` | Agent session completed | `final_status`, `total_iterations`, `result_artifact` |
| `agent.kill` | Agent terminated | `kill_reason` |

### Example: Tracing Agent Hierarchies

```bash
# Find all spawn events for a session
jq -c 'select(.operation == "agent.spawn" and .session_id == "01KFH...")' foxcular_events.ndjson

# Get iteration token usage for troubleshooting
jq -c 'select(.operation == "agent.iteration") | {iter: .data.iteration, tokens: .data.total_tokens, finish: .data.finish_reason}' foxcular_events.ndjson

# Find agents that hit context budget
jq -c 'select(.operation == "agent.iteration" and .data.budget_exceeded == true)' foxcular_events.ndjson

# Trace spawn → wait → complete for an overseer
jq -c 'select(.session_id == "01KFH..." and .operation | startswith("agent."))' foxcular_events.ndjson
```

### Agent Data Fields

| Field | Type | Description |
|-------|------|-------------|
| `data.role` | string | Agent role (coder, researcher, overseer) |
| `data.task_len` | int | Length of task prompt in characters |
| `data.iteration` | int | Current iteration number |
| `data.prompt_tokens` | int | Tokens in prompt |
| `data.completion_tokens` | int | Tokens in response |
| `data.finish_reason` | string | LLM finish reason (stop, tool_calls) |
| `data.tool_calls` | int | Number of tool calls in response |
| `data.children_count` | int | Number of child agents being waited on |
| `data.spawned_session` | string | Session ID of spawned agent |
| `data.result_artifact` | string | CAS digest of agent output (for replay) |

## Orchestration Events (Kanban)

Symphony/Kanban runtime surfaces emit orchestration-focused foxcular events from web command handlers.

### Operation Types

| Operation | Command | Description |
|-----------|---------|-------------|
| `web.orchestration.board_get` | `orchestration/board-get` | Board query + lane/count summary retrieval |
| `web.orchestration.board_card_get` | `orchestration/board-card-get` | Single card retrieval with lane/outcome metadata |
| `web.orchestration.refresh` | `orchestration/refresh` | Projection replay/refresh enqueue |

### SSE-visible fields (contract)

These fields are forwarded by `internal/runtime/observability/sse_bridge.go` into GUI activity events:

- always: top-level `trace_id`
- when present in source event data: `data.request_id`, `data.lane`, `data.last_outcome`

Operation-specific guarantees:

- `web.orchestration.board_card_get` includes `data.request_id`, `data.lane`, and `data.last_outcome`.
- `web.orchestration.board_get` includes `data.request_id` and board summary fields (for example `data.card_count`, `data.lane_filter`).
- `web.orchestration.refresh` includes `data.request_id` and refresh ack fields (for example `data.queued`, `data.coalesced`, `data.idempotent`).

### Additional orchestration fields (when present)

- `data.issue_id`
- `data.issue_identifier`
- `data.policy_status`
- `data.eligibility`
- `data.queued`
- `data.coalesced`
- `data.idempotent`
- `data.card_count`
- `data.lane_filter`

Example:

```json
{
  "operation": "web.orchestration.board_card_get",
  "command": "orchestration/board-card-get",
  "trace_id": "01K...",
  "status": "ok",
  "data": {
    "request_id": "req-card-001",
    "issue_id": "issue-123",
    "issue_identifier": "ABC-123",
    "lane": "Running",
    "last_outcome": "policy_denied",
    "policy_status": "denied"
  }
}
```

## Migration from Narrow Events

The existing `SweGrepEvent` and other narrow events continue to work. Wide
events provide additional comprehensive context. For skills that emit both:

- Narrow events go to `$FOXCTL_OBS_DIR/events/<skill_name>.ndjson`
- Foxcular events go to `$FOXCTL_OBS_DIR/events/foxcular_events.ndjson`

Eventually, narrow events may be deprecated in favor of the unified foxcular event
format with domain-specific data in the `data` map.

## Storage & Retention

### Directory Structure

Foxcular events are stored under the observability directory:

```
~/.foxctl/observability/
└── events/
    ├── foxcular_events.ndjson    # Main foxcular event stream
    └── code_swe_grep.ndjson  # Legacy narrow events (if enabled)
```

The default observability path is `~/.foxctl/observability`. CLI/daemon loads
`paths.observability` and sets `FOXCTL_OBS_DIR` at startup unless it is already
set in the environment.

### Backup Integration

Observability logs are included in foxctl backups as the `observability`
component:

```bash
# Include observability in backup (default)
foxctl backup create

# Exclude observability from backup
foxctl backup create --exclude observability

# Backup only observability
foxctl backup create --components observability
```

**Retention in backups**: Only event files modified in the last 7 days are
included to prevent backup size bloat.

### Log Pruning

Events can be pruned by age or size to manage disk usage:

```go
import "github.com/joshka0/foxctl/internal/runtime/observability"

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
0 2 * * * foxctl observability prune --older-than 7d
```

## Persistence Options

By default, foxcular events are written to NDJSON files. For events that need
queryability, you can enable SQL persistence with different modes:

| Mode | Description |
|------|-------------|
| `PersistDefault` | NDJSON file (default) |
| `PersistSQL` | Direct SQLite write for high-value events |
| `PersistHybrid` | NDJSON + background SQLite sync (recommended for queryable events) |

See [persistence.md](persistence.md) for full details on configuring persistence
modes, background sync, and querying events via SQLite.

## Related Documents

| Document | Description |
|----------|-------------|
| [persistence.md](persistence.md) | SQLite persistence and hybrid mode configuration |
| [events.md](events.md) | Legacy narrow events format |
| [../general/skills.md](../general/skills.md) | Skill development guide including observability |
