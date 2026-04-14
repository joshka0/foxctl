# Mailbox + Blackboard

**Status:** Draft\
**Scope:** Canonical overview of mailbox/blackboard semantics for foxctl.

---

## 1. Overview

This spec defines the shared concepts for:

- **Mailbox**: persistent per-actor message queues with ack semantics.
- **Blackboard**: shared topic-based coordination with claims/leases.

Implementation details and migration steps live in
[`task_graph_mailbox_implementation.md`](task_graph_mailbox_implementation.md)
and the Agent Profile v1 envelope contract lives in
[`v1/agent_profile_v1.md`](v1/agent_profile_v1.md).

---

## 2. Mailbox

Mailbox messages are durable and acked explicitly. Core message types:

- `ask`: request expecting a reply
- `reply`: response to a prior ask
- `cmd`: fire-and-forget instruction
- `event`: notification/heartbeat

Typical fields:

- `to` / `from` mailbox namespace
- `type` (ask|reply|cmd|event)
- `message_id`, `correlation_id`, `timestamp`
- `payload` (JSON)
- `ack_required` (bool)

---

## 3. Blackboard

Blackboard is a shared topic store with optional claims:

- `topic` identifies a logical coordination stream.
- `post` writes data to a topic.
- `claim` / `release` enforces advisory ownership.

---

## 4. Implementation Notes (foxctl)

- Current skills use `mailbox/manage.*` and `bb/manage.*` namespaces.
- Agent Profile v1 uses `mailbox/*` and `bb/*` in envelopes; foxctl tools
  map these to the skill namespaces for execution.
