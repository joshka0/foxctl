---
name: agentctl Mailbox
description: Inter-agent messaging via blackboard. Send messages, check inbox, coordinate between agents with priorities.
---

# Mailbox Coordination

SQLite-backed blackboard for inter-agent messaging with priority queues.

## Operations

```bash
# Send message
agentctl run mailbox --input '{
  "operation": "send",
  "send": {
    "workspace_id": "/path/to/workspace",
    "sender": "actor:coder:agent1",
    "recipient": "actor:coder:agent2",
    "subject": "Review needed",
    "body": "Please review auth.go",
    "priority": 1
  }
}'

# Check inbox
agentctl run mailbox --input '{
  "operation": "inbox",
  "inbox": {"actor_id": "actor:coder:agent1", "only_unread": true}
}'

# Acknowledge
agentctl run mailbox --input '{
  "operation": "ack",
  "ack": {"actor_id": "actor:coder:agent1", "message_ids": ["msg-1"]}
}'
```

## Priority Levels

- `1` - Urgent (admin directives)
- `2` - High (overseer instructions)
- `3` - Normal (peer coordination)

Broadcast: Use `recipient: "*"` for all agents.

Full docs: `~/.agentctl/share/configs/skills/agentctl-mailbox/Skill.md`
