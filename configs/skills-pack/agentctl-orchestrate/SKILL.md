---
name: agentctl-orchestrate
description: "Orchestration: tasks/todos, sessions, mailbox/inbox, and multi-agent coordination."
---

## What I do
- Keep work tracked (tasks), durable (sessions), and collaborative (mailbox/inbox).

## When to use me
- You’re doing multi-step work and don’t want to lose state.
- You want human-in-the-loop messaging (“overseer inbox”).

## Tasks (todo)
```bash
agentctl run todo/manage --input '{"operation":"list"}'
agentctl run todo/manage --input '{"operation":"add","title":"<task>","description":"<details>"}'
agentctl run todo/manage --input '{"operation":"set_active","id":"<task-id>"}'
agentctl run todo/manage --input '{"operation":"complete","id":"<task-id>"}'
```

## Sessions
```bash
agentctl run session/save --input '{"name":"<short-name>"}'
agentctl run session/restore --input '{"name":"<short-name>"}'
```

## Mailbox / inbox
```bash
agentctl run mailbox/manage --input '{"operation":"inbox","inbox":{"actor_id":"overseer","limit":20}}'
agentctl run mailbox/manage --input '{"operation":"send","send":{"recipient":"overseer","subject":"<subject>","body":"<body>","priority":2}}'
```
