# Context and Observability

Machine-friendly reference for proactive context injection and wide-event tracing.

## Core Packages

| Package | Responsibility |
|--------|----------------|
| `internal/context/updater` | Background worker that analyzes active sessions and injects relevant context |
| `internal/observability` | Wide-event emission, trace/span context propagation, NDJSON/SQLite persistence options |
| `internal/hooks` | Hook pipeline integrating context signals into tool/session lifecycle |

## Context Updater Flow

1. Poll active sessions.
2. Analyze recent turns for topic/intent drift.
3. Query candidate context.
4. Inject selected context with rate limiting and dedup safeguards.

## Observability Primitives

| Primitive | Purpose |
|----------|---------|
| Trace ID propagation (`AGENTCTL_TRACE_ID`) | Correlate operations across processes and events |
| Wide events | Capture high-signal operation context in structured envelopes |
| NDJSON persistence | Fast append-only event capture |
| SQLite persistence | Queryable long-term event analysis (when enabled) |

## When to Update These Docs

- New background workers that influence context injection cadence.
- Changes to event schemas or trace propagation behavior.
- Hook lifecycle changes that alter context/observability integration points.

## Related Docs

- `docs/general/events.md`
- `docs/general/hooks.md`
- `docs/general/runtime-orchestration.md`
