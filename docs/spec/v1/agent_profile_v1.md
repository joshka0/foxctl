# Agent Profile v1

**Version:** 1.0.0
**Status:** Specification (Optional Extension)
**Last Updated:** 2025-11-15

> **Purpose:** This document defines the Agent Profile v1, an optional additive extension to foxctl Core Profile v1 that adds multi-agent orchestration capabilities including mailbox messaging, blackboard coordination, quotas, and advanced scheduling.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Relationship to Core Profile](#2-relationship-to-core-profile)
3. [Agent Commands](#3-agent-commands)
4. [Mailbox System](#4-mailbox-system)
5. [Blackboard Coordination](#5-blackboard-coordination)
6. [Quotas & Scheduling](#6-quotas--scheduling)
7. [OCI Runner](#7-oci-runner)
8. [Agent Profile Envelope Extensions](#8-agent-profile-envelope-extensions)
9. [Implementation Status](#9-implementation-status)

---

## 1. Overview

### 1.1 What is the Agent Profile?

The **Agent Profile** is an **additive profile** on top of Core Profile v1 that enables:

- **Multi-agent orchestration**: Spawn, manage, and coordinate multiple agents
- **Mailbox messaging**: Agents communicate via typed messages (ask/reply/cmd/event)
- **Blackboard coordination**: Shared state space with topics, leases, and claims
- **Resource quotas**: CPU, memory, network, and concurrency limits per agent
- **Advanced scheduling**: Priority queues, fairness, and backpressure
- **OCI runner**: Container-based skill execution with hardened security

### 1.2 Design Philosophy

**Additive, not invasive**:
- Core Profile works without Agent Profile
- Agent Profile reuses Core envelope shape, error codes, CAS, memory
- All Core skills work in Agent Profile (no rewrites needed)

**Production-grade multi-agent**:
- Fault-tolerant (agent crashes don't lose work)
- Observable (metrics, logs, traces)
- Secure (quota enforcement, network policies)

### 1.3 Use Cases

- **Distributed workflows**: Multi-stage pipelines with agent handoffs
- **Collaborative agents**: Agents specialize and cooperate via blackboard
- **Isolation**: Run untrusted code in OCI containers
- **Resource management**: Prevent runaway agents from consuming all resources

---

## 2. Relationship to Core Profile

### 2.1 Compatibility

**100% backward compatible**:
- Core Profile skills run unchanged in Agent Profile
- Core envelopes are valid Agent Profile envelopes
- Core error codes reused

**Additive changes**:
- `meta.profiles` includes `"agent/v1"` when Agent Profile is active
- New command namespaces: `agent/*`, `bb/*`, `mailbox/*`
- New envelope fields: agent-specific metadata

### 2.2 Profile Declaration

Envelopes from Agent Profile MUST include:

```json
{
  "meta": {
    "profiles": ["core/v1", "agent/v1"]
  }
}
```

### 2.3 Upgrading from Core to Agent

**No breaking changes required**:
1. Install Agent Profile runtime
2. Configure agent quotas (optional)
3. Start using agent commands (spawn, mailbox, blackboard)
4. Existing skills continue working

---

## 3. Agent Commands

### 3.1 Agent Lifecycle

| Command | Purpose | Status |
|---------|---------|--------|
| `agent/spawn` | Create new agent instance | Agent v1 |
| `agent/restart` | Restart failed agent | Agent v1 |
| `agent/kill` | Terminate agent | Agent v1 |
| `agent/send` | Send message to agent mailbox | Agent v1 |
| `agent/watch` | Stream agent events | Agent v1 |

**Implementation note (foxctl):** The current skill/tool names use
`agent.spawn` and `mailbox/manage.*` for execution. The envelope contract
retains `agent/*` and `mailbox/*`; adapters should map between these forms.

### 3.2 Spawn Agent

**Command**: `agent/spawn`

**Input**:
```json
{
  "version": 1,
  "command": "agent/spawn",
  "data": {
    "agent_id": "agent-001",
    "goal": "Process incoming webhooks from Stripe",
    "skills": ["http/openapi", "fs/write", "text/grep"],
    "quotas": {
      "max_cpu_ms": 10000,
      "max_memory_mb": 512,
      "max_concurrent_jobs": 5,
      "max_wall_time_s": 300
    },
    "workspace": "/workspace/agent-001",
    "initial_state": {
      "spec": "memory:stripe",
      "webhook_endpoint": "/webhooks/stripe"
    }
  }
}
```

**Output** (success):
```json
{
  "version": 1,
  "status": "ok",
  "command": "agent/spawn",
  "data": {
    "agent_id": "agent-001",
    "pid": 12345,
    "mailbox_id": "mailbox:agent-001",
    "status": "running"
  },
  "meta": {
    "profiles": ["core/v1", "agent/v1"]
  }
}
```

### 3.3 Kill Agent

**Command**: `agent/kill`

**Input**:
```json
{
  "version": 1,
  "command": "agent/kill",
  "data": {
    "agent_id": "agent-001",
    "graceful": true,
    "timeout_s": 30
  }
}
```

**Output**:
```json
{
  "version": 1,
  "status": "ok",
  "command": "agent/kill",
  "data": {
    "agent_id": "agent-001",
    "final_status": "terminated",
    "exit_code": 0,
    "cleanup_summary": {
      "jobs_canceled": 2,
      "mailbox_drained": true,
      "workspace_preserved": true
    }
  }
}
```

---

## 4. Mailbox System

### 4.1 Message Types

Agents communicate via **typed messages**:

| Type | Purpose | Response Expected |
|------|---------|-------------------|
| `ask` | Request with required response | Yes |
| `reply` | Response to ask | No |
| `cmd` | Command (fire-and-forget) | Optional |
| `event` | Notification | No |

### 4.2 Mailbox Commands

| Command | Purpose |
|---------|---------|
| `mailbox/send` | Send message to agent |
| `mailbox/poll` | Retrieve messages (blocking) |
| `mailbox/ack` | Acknowledge message processed |
| `mailbox/list` | List pending messages |

### 4.3 Send Message

**Command**: `mailbox/send`

**Input** (ask):
```json
{
  "version": 1,
  "command": "mailbox/send",
  "data": {
    "to": "agent-002",
    "from": "agent-001",
    "type": "ask",
    "message_id": "msg-12345",
    "correlation_id": "task-abc",
    "timeout_s": 60,
    "payload": {
      "skill": "http/openapi",
      "params": {
        "spec": "memory:github",
        "operationId": "listRepos"
      }
    }
  }
}
```

**Input** (event):
```json
{
  "version": 1,
  "command": "mailbox/send",
  "data": {
    "to": "agent-*",
    "from": "agent-001",
    "type": "event",
    "message_id": "evt-67890",
    "payload": {
      "event_type": "webhook_received",
      "webhook_id": "wh_123",
      "timestamp": "2025-11-15T12:34:56Z"
    }
  }
}
```

### 4.4 Poll Messages

**Command**: `mailbox/poll`

**Input**:
```json
{
  "version": 1,
  "command": "mailbox/poll",
  "data": {
    "agent_id": "agent-001",
    "timeout_s": 30,
    "max_messages": 10
  }
}
```

**Output**:
```json
{
  "version": 1,
  "status": "ok",
  "command": "mailbox/poll",
  "data": {
    "messages": [
      {
        "message_id": "msg-12345",
        "from": "agent-002",
        "to": "agent-001",
        "type": "ask",
        "correlation_id": "task-abc",
        "payload": { /* message content */ },
        "timestamp": "2025-11-15T12:34:56Z",
        "ttl_s": 300
      }
    ]
  }
}
```

---

## 5. Blackboard Coordination

### 5.1 Blackboard Concepts

The **blackboard** is a shared, persistent key-value store where agents coordinate via:
- **Topics**: Named channels for related data
- **Items**: Individual entries with metadata
- **Leases**: Temporary ownership for processing
- **Claims**: Acquire work items atomically

### 5.2 Blackboard Commands

| Command | Purpose |
|---------|---------|
| `bb/post` | Post item to topic |
| `bb/watch` | Stream updates from topic |
| `bb/search` | Query blackboard by filters |
| `bb/claim` | Claim item for processing |
| `bb/release` | Release claimed item |

### 5.3 Post to Blackboard

**Command**: `bb/post`

**Input**:
```json
{
  "version": 1,
  "command": "bb/post",
  "data": {
    "topic": "webhooks/stripe",
    "item": {
      "webhook_id": "wh_123",
      "event_type": "payment_intent.succeeded",
      "payload": {
        "amount": 1000,
        "currency": "usd"
      }
    },
    "metadata": {
      "priority": 5,
      "ttl_s": 3600,
      "tags": ["payment", "stripe"]
    }
  }
}
```

**Output**:
```json
{
  "version": 1,
  "status": "ok",
  "command": "bb/post",
  "data": {
    "item_id": "bb:webhooks/stripe:item-456",
    "topic": "webhooks/stripe",
    "created_at": "2025-11-15T12:34:56Z"
  }
}
```

### 5.4 Claim Item

**Command**: `bb/claim`

**Input**:
```json
{
  "version": 1,
  "command": "bb/claim",
  "data": {
    "topic": "webhooks/stripe",
    "filter": {
      "tags": ["payment"],
      "priority_min": 3
    },
    "agent_id": "agent-001",
    "lease_duration_s": 300
  }
}
```

**Output** (success):
```json
{
  "version": 1,
  "status": "ok",
  "command": "bb/claim",
  "data": {
    "item_id": "bb:webhooks/stripe:item-456",
    "lease_id": "lease-789",
    "item": {
      "webhook_id": "wh_123",
      "event_type": "payment_intent.succeeded",
      "payload": { /* ... */ }
    },
    "lease_expires_at": "2025-11-15T12:39:56Z"
  }
}
```

### 5.5 Release Item

**Command**: `bb/release`

**Input**:
```json
{
  "version": 1,
  "command": "bb/release",
  "data": {
    "lease_id": "lease-789",
    "result": "completed",
    "output": {
      "artifact": "sha256:abc123...",
      "summary": "Payment processed successfully"
    }
  }
}
```

---

## 6. Quotas & Scheduling

### 6.1 Agent Quotas

Each agent has enforceable limits:

```json
{
  "quotas": {
    "max_cpu_ms": 10000,
    "max_memory_mb": 512,
    "max_network_egress_mb": 100,
    "max_concurrent_jobs": 5,
    "max_wall_time_s": 300,
    "max_file_writes": 100,
    "max_cas_artifacts": 50
  }
}
```

### 6.2 Quota Enforcement

**CPU**:
- Track cumulative CPU time across all jobs
- Terminate agent when quota exceeded

**Memory**:
- Monitor RSS via cgroups (Linux) or process stats
- Kill agent if RSS exceeds limit

**Network**:
- Track bytes sent/received
- Block egress when quota exceeded

**Concurrency**:
- Max parallel jobs enforced by scheduler
- New jobs queued when limit reached

### 6.3 Quota Exceeded Error

**Envelope**:
```json
{
  "version": 1,
  "status": "error",
  "command": "agent/spawn",
  "data": {
    "hint": "Agent exceeded CPU quota (10000ms). Reduce workload or increase quota."
  },
  "error": {
    "code": "EPOLICY",
    "message": "CPU quota exceeded",
    "details": {
      "quota_type": "cpu_ms",
      "limit": 10000,
      "used": 12345
    }
  }
}
```

### 6.4 Scheduling Policies

**Round-robin**:
- Fair scheduling across agents
- Prevents starvation

**Priority**:
- Higher priority agents scheduled first
- Configured per agent or per job

**Backpressure**:
- When system overloaded, reject new spawns
- Return `ESKILLDOWN` with retry hint

---

## 7. OCI Runner

### 7.1 Purpose

The **OCI runner** executes skills in hardened containers for maximum isolation.

**Benefits**:
- Network isolation (no egress by default)
- Filesystem isolation (read-only root)
- Resource limits (cgroups)
- Security profiles (seccomp, AppArmor)

### 7.2 Skill Manifest for OCI

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: data/analyze
  version: 1.0.0
distribution:
  type: oci
  image: "ghcr.io/org/skill-data-analyze:v1.0.0"
  digest: "sha256:abcdef123..."
io:
  format: JSON
signature:
  command: data/analyze
  parameters:
    - name: input
      type: string
      required: true
capabilities:
  network: "none"
  filesystem: "read-only"
  ephemeral_storage_mb: 100
```

### 7.3 OCI Execution Flow

1. **Pull image** (if not cached)
2. **Validate digest** matches manifest
3. **Create container** with:
   - Read-only root filesystem
   - Ephemeral tmpfs at `/work` (size-limited)
   - No network namespace
   - Resource limits (CPU, memory)
4. **Execute** skill with input on stdin
5. **Capture** output on stdout (with size limits)
6. **Destroy** container (ephemeral only)

### 7.4 Security Hardening

**Network**:
- `network: "none"` → no network namespace
- `network: "egress"` → allow outbound, enforce egress list

**Filesystem**:
- Root filesystem read-only
- `/work` is ephemeral tmpfs (destroyed on exit)
- Workspace mounted read-only unless `workspace_rw: true`

**Capabilities**:
- Drop all Linux capabilities by default
- Seccomp profile blocks dangerous syscalls
- AppArmor profile enforces additional restrictions

**Resource limits**:
- CPU quota via cgroups
- Memory limit via cgroups
- Disk I/O limits via cgroups

---

## 8. Agent Profile Envelope Extensions

### 8.1 Additional Meta Fields

When Agent Profile is active, envelopes MAY include:

```json
{
  "meta": {
    "profiles": ["core/v1", "agent/v1"],
    "agent_id": "agent-001",
    "mailbox_id": "mailbox:agent-001",
    "correlation_id": "task-abc",
    "parent_job_id": "01H...",
    "quota_remaining": {
      "cpu_ms": 7500,
      "memory_mb": 412,
      "network_mb": 85
    }
  }
}
```

### 8.2 Agent-Specific Error Codes

Reuses Core error codes, plus:

| Code | Meaning |
|------|---------|
| `EPOLICY` | Quota exceeded or policy violation |
| `ESKILLDOWN` | Agent unavailable (circuit breaker open) |

### 8.3 Watch Streams

**Command**: `agent/watch`

**Output** (NDJSON stream):
```ndjson
{"version":1,"status":"progress","command":"agent/watch","data":{"event":"agent_started","agent_id":"agent-001"}}
{"version":1,"status":"progress","command":"agent/watch","data":{"event":"job_submitted","job_id":"01H..."}}
{"version":1,"status":"progress","command":"agent/watch","data":{"event":"mailbox_message","message_id":"msg-12345"}}
{"version":1,"status":"ok","command":"agent/watch","data":{"event":"agent_terminated","agent_id":"agent-001"}}
```

---

## 9. Implementation Status

### 9.1 Current Status

**Agent Profile v1.0**: Specification complete, implementation planned for **post-v1.0**

**Reason**: Core Profile v1 is the priority for initial release. Agent Profile adds significant complexity and is better delivered as a stable extension after Core is proven.

### 9.2 Roadmap

**v1.0** (Core Profile only):
- ✅ JSON envelopes
- ✅ Skills (WASI + exec)
- ✅ Jobs, CAS, memory
- ✅ OpenAPI skill
- ✅ Plugin protocol

**v1.1** (Agent Profile):
- Agent lifecycle (spawn, kill, restart)
- Mailbox messaging
- Blackboard coordination
- Quota enforcement
- OCI runner

**v1.2** (Advanced Agent Features):
- Distributed agents (multi-host)
- Agent-to-agent RPC
- Advanced scheduling (priority, fairness)
- Observability (metrics, traces)

### 9.3 Scaffolding

To prepare for Agent Profile, Core v1 includes:
- `meta.profiles` field (reserved for future use)
- Error codes (`EPOLICY`, `ESKILLDOWN`)
- Command namespace reservation (`agent/*`, `bb/*`, `mailbox/*`)

**No breaking changes** will be required to add Agent Profile in v1.1.

---

## Appendix A: Agent Profile CLI (Future)

### A.1 Agent Management

```bash
# Spawn agent
foxctl agent spawn \
  --id agent-001 \
  --goal "Process Stripe webhooks" \
  --skills http/openapi,fs/write \
  --quota-cpu-ms 10000 \
  --quota-memory-mb 512

# List agents
foxctl agent list

# Kill agent
foxctl agent kill agent-001 --graceful

# Watch agent events
foxctl agent watch agent-001
```

### A.2 Mailbox

```bash
# Send message
foxctl mailbox send agent-002 \
  --from agent-001 \
  --type ask \
  --payload '{"skill":"fs/read","params":{"path":"README.md"}}'

# Poll messages
foxctl mailbox poll agent-001 --timeout 30s

# Ack message
foxctl mailbox ack msg-12345
```

### A.3 Blackboard

```bash
# Post item
foxctl bb post webhooks/stripe \
  --item '{"webhook_id":"wh_123"}' \
  --priority 5 \
  --ttl 3600

# Claim item
foxctl bb claim webhooks/stripe \
  --agent agent-001 \
  --lease-duration 300

# Release item
foxctl bb release lease-789 --result completed
```

---

## Appendix B: References

- **Core Profile v1**: Foundation for Agent Profile
- **Protocol v1**: Wire contract (envelopes, errors, streaming)
- **OpenAPI Skill**: Reusable in Agent Profile unchanged
- **Plugin Protocol**: Extensibility for both Core and Agent profiles

---

**Document Status**: Specification (Optional Extension)
**Implementation Target**: v1.1 (post-Core v1.0)
**Dependencies**: Core Profile v1, Protocol v1
