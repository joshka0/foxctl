# v1 Specifications Overview

This document summarizes the foundational v1 specifications that define agentctl's architecture. For full details, see [docs/spec/v1/](../spec/v1/).

---

## Protocol v1

**File:** [protocol_v1.md](../spec/v1/protocol_v1.md)

The canonical wire contract for all agentctl I/O.

### Key Concepts

- **JSON Envelope**: Single structure for all operations (skills, plugins, jobs, agents)
- **Status**: `ok | error | progress`
- **Artifactization**: Large outputs → CAS with `data.summary` + `data.artifact`
- **Error Codes**: `EARG`, `ERUNTIME`, `EPOLICY`, `ENOTFOUND`, `ETIMEOUT`, etc.

### Envelope Shape

```json
{
  "version": 1,
  "status": "ok",
  "command": "namespace/verb",
  "data": { },
  "meta": { "ts": "...", "duration_ms": 153, "runner": "exec" },
  "error": { }
}
```

### Invariants

- `meta.ts` **MUST** be RFC3339 UTC
- `status:"ok"` → `error` fields empty
- `status:"error"` → `error.code` + `error.message` required
- **stdout** = envelopes only, **stderr** = logs only

---

## Core Profile v1

**File:** [core_profile_v1.md](../spec/v1/core_profile_v1.md)

Single-agent runtime for structured LLM workflows.

### What agentctl Core Provides

| Concept | Description |
|---------|-------------|
| **Skills** | Typed tools with WASI/exec isolation |
| **Jobs** | Async execution with `queued → running → ok\|error\|canceled` |
| **Artifacts (CAS)** | Content-addressable storage for large outputs |
| **Memory** | Auto-cache (24h) + named persistent memories |

### Design Principles

- **Token efficiency**: Large outputs → CAS, summaries inline
- **Memory-first**: Recent work auto-cached, durable work explicitly named
- **Deterministic**: Same inputs → same outputs
- **Composable**: Unix-style piping and digest chaining

### Skill Naming

Pattern: `category/verb[-noun]` (e.g., `test/run`, `code/symbols`)

Regex: `^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*(?:-[a-z0-9-]+)?$`

---

## Agent Profile v1

**File:** [agent_profile_v1.md](../spec/v1/agent_profile_v1.md)

Additive extension for multi-agent orchestration.

### What Agent Profile Adds

| Feature | Description |
|---------|-------------|
| **Multi-agent** | Spawn, manage, coordinate multiple agents |
| **Mailbox** | Typed messages: `ask`, `reply`, `cmd`, `event` |
| **Blackboard** | Shared state with topics, leases, claims |
| **Quotas** | CPU, memory, network limits per agent |
| **OCI Runner** | Container-based skill execution |

### Compatibility

- **100% backward compatible** with Core Profile
- Core skills work unchanged in Agent Profile
- Envelope extends with `meta.profiles: ["core/v1", "agent/v1"]`

### Use Cases

- Distributed workflows with agent handoffs
- Collaborative agents via blackboard coordination
- Isolated execution for untrusted code
- Resource management for runaway agents

---

## Plugin Protocol

**File:** [plugin_protocol.md](../spec/v1/plugin_protocol.md)

Extension system for custom auth and pagination.

### Plugin Types

| Type | Command | Purpose |
|------|---------|---------|
| **Auth** | `plugin/auth` | Sign requests, inject headers |
| **Pagination** | `plugin/pagination` | Determine next page from response |

### Transport

- **Protocol**: JSON envelopes over stdin/stdout
- **Lifecycle**: Spawn → write request → read response → exit
- **Discovery**: `AGENTCTL_PLUGIN_PATH` convention

### Execution Constraints

| Limit | Default |
|-------|---------|
| Timeout | 500ms |
| CPU time | 200ms |
| Memory | 64MB |
| Input size | 128KB |
| Output size | 32KB |

### Why Plugins?

Built-in auth/pagination covers ~90% of APIs. Plugins handle "snowflake" APIs without bloating core.

---

## Daemon Protocol

**File:** [daemon_protocol.md](../spec/v1/daemon_protocol.md)

Long-lived agent process specification.

### Daemon Responsibilities

- Poll mailbox queue for messages to agent namespace
- Execute DSPy turns for `agent.ask` and `agent.cmd`
- Emit `agent.reply` messages back to callers
- Maintain heartbeats and agent state

### Lifecycle States

```
starting → running → stopped|error
```

### Mailbox Polling

- Select messages where `to_ns == <agent namespace>`
- Filter by `visible_at <= now`
- Order by ascending timestamp
- Visibility lease prevents duplicate processing

### Heartbeats

- Periodic `heartbeat_at` updates while running
- Observers infer liveness by comparing to current time

---

## Quick Reference

| Spec | Purpose | Status |
|------|---------|--------|
| Protocol v1 | Wire contract (envelopes, errors, CAS) | Final |
| Core Profile v1 | Single-agent runtime | Final |
| Agent Profile v1 | Multi-agent orchestration | Final |
| Plugin Protocol | Auth/pagination extensions | Final |
| Daemon Protocol | Long-lived agent processes | Draft |

---

## API Server v1 (Draft)

See `docs/general/api-server.md`.

### Skill Execution
- POST /api/v1/skills/run returns the raw skill envelope in data.envelope.
- Skill envelopes keep Protocol v1 invariants (meta.ts, status, error).

---

## Related Documentation

- [Architecture](architecture.md) - System design overview
- [Skills](skills.md) - Skill development guide
- [Storage](storage.md) - Database schemas
- [Gotchas](gotchas.md) - Common pitfalls
