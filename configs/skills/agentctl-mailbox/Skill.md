---
name: agentctl Mailbox
description: Inter-agent messaging via blackboard. Send messages, check inbox, coordinate between agents with priorities.
---

# Mailbox Coordination with agentctl

SQLite-backed blackboard for inter-agent messaging with priority queues.

## Send a Message

```bash
agentctl run mailbox --input '{
  "operation": "send",
  "send": {
    "workspace_id": "/path/to/workspace",
    "sender": "actor:coder:agent1",
    "recipient": "actor:coder:agent2",
    "subject": "Review needed",
    "body": "Please review changes to auth.go",
    "priority": 1,
    "task_id": "01ABC..."
  }
}'
```

Priority levels:

- `1` - Urgent (admin directives)
- `2` - High (overseer instructions)
- `3` - Normal (peer coordination)

## Check Inbox

```bash
agentctl run mailbox --input '{
  "operation": "inbox",
  "inbox": {
    "workspace_id": "/path/to/workspace",
    "actor_id": "actor:coder:agent1",
    "only_unread": true,
    "limit": 10
  }
}'
```

Filter options:

- `only_unread` - Only unread messages
- `task_id` - Filter by specific task
- `stream` - Filter by message stream

## Acknowledge Messages

```bash
agentctl run mailbox --input '{
  "operation": "ack",
  "ack": {
    "workspace_id": "/path/to/workspace",
    "actor_id": "actor:coder:agent1",
    "message_ids": ["msg-id-1", "msg-id-2"]
  }
}'
```

## Broadcast Messages

Send to all agents with `recipient: "*"`:

```bash
agentctl run mailbox --input '{
  "operation": "send",
  "send": {
    "workspace_id": "/path/to/workspace",
    "sender": "actor:system:admin",
    "recipient": "*",
    "subject": "Priority change",
    "body": "Focus on Task C immediately",
    "priority": 1
  }
}'
```

## Message Types

- `info` - Informational messages
- `instruction` - Directives from admin/overseer
- `status` - Status updates
- `question` - Questions requiring response

## Hooks Integration

The `hooks/mail_router` hook automatically surfaces unread messages to Claude's
context on PreToolUse events, prioritizing admin and overseer messages.
