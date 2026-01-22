---
name: agentctl Observability
description: Query and browse observability logs - wide events, errors, stats.
---

# Observability Logs

Query wide events without navigating to the observability directory.

```bash
# Recent events (default: 20)
agentctl run obs/logs --input '{}'

# Errors only
agentctl run obs/logs --input '{"errors_only": true}'

# Filter by skill with stats
agentctl run obs/logs --input '{"command": "code/semantic_search", "stats": true}'

# Events from last hour
agentctl run obs/logs --input '{"since": "1h", "limit": 50}'

# Filter by operation
agentctl run obs/logs --input '{"operation": "skill.run", "status": "error"}'
```

## Parameters

| Param | Description |
|-------|-------------|
| `limit` | Max events (default: 20, max: 500) |
| `operation` | Filter by operation (e.g., `skill.run`, `hook.execute`) |
| `command` | Filter by skill/command name |
| `status` | Filter: `ok`, `error`, `canceled` |
| `component` | Filter: `cli`, `web`, `hook`, `skill`, `job` |
| `since` | RFC3339 timestamp or duration (`1h`, `30m`) |
| `stats` | Include aggregate statistics |
| `errors_only` | Only show error events |

## Output

Returns events sorted by timestamp (most recent first) with:
- `events[]`: Matching wide events
- `count`: Number of events returned
- `total_scanned`: Total events in file
- `stats`: Aggregate statistics (if requested)
- `summary`: Human-readable filter summary
