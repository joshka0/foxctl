---
name: agentctl Mailbox
description: Inter-agent messaging via blackboard. Send messages, check inbox, coordinate between agents with priorities.
---

# Mailbox Coordination with agentctl

SQLite-backed blackboard for inter-agent messaging with priority queues.

## Quick: Human-in-the-Loop with agentctl-mail

Send messages to a running Claude Code session from another terminal:

```bash
# Basic message to overseer (surfaces in Claude's context)
agentctl-mail "Priority change" "Focus on auth bug first"

# High priority message (priority 1 = urgent)
agentctl-mail -p 1 "STOP" "Pause and review this issue before continuing"

# Require acknowledgment from agent
agentctl-mail --ack "Review needed" "Check the API changes I made"

# Send to specific agent
agentctl-mail -to "actor:coder:luna" "Question" "What's your status?"

# Quiet mode - only output message ID
agentctl-mail -q "Status check" "Are you still running?"
```

### agentctl-mail Options

| Flag | Default | Description |
|------|---------|-------------|
| `-p` | 3 | Priority (1=urgent, 5=lowest) |
| `-to` | overseer | Recipient actor ID |
| `-from` | human | Sender name |
| `-kind` | instruction | Message kind: instruction, info, alert, review_request |
| `-ack` | false | Require acknowledgment |
| `-workspace` | auto | Workspace path (auto-detected) |
| `-q` | false | Quiet mode |

### How It Works

```
Terminal A (Claude Code running)     Terminal B (agentctl-mail)
─────────────────────────────────    ─────────────────────────────
Claude working on task...            $ agentctl-mail "Stop" "Review PR first"
     │                                      │
     │    ┌─────────────────────┐           │
     │    │  Mailbox Store      │◄──────────┘
     │    │  (SQLite)           │
     │    └─────────────────────┘
     │              │
     ▼              ▼
┌─────────────────────────────────┐
│ overseer-inbox hook (PreToolUse)│
│ Surfaces message to Claude      │
└─────────────────────────────────┘
     │
     ▼
Claude sees: "📬 Inbox (1 message):
  [P1] human: Stop - Review PR first"
```

### Use Cases

1. **Interrupt with context** - Send urgent info while Claude works
2. **Priority changes** - Redirect focus to different task
3. **Code review requests** - Ask Claude to review specific changes
4. **Status checks** - Query agent status from another terminal

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

## Agent Daemon Integration (L1 Loop)

Agent daemons poll their mailbox for messages via the L1 loop:

```
┌─────────────────────────────────────────────────────┐
│                Agent Daemon                          │
├─────────────────────────────────────────────────────┤
│                                                      │
│  L1 Loop (Poll Mailbox)                             │
│  ┌─────────────────────────────────────────────┐    │
│  │ 1. Poll mailbox for agent-id               │    │
│  │ 2. Lease message (visibility timeout)      │    │
│  │ 3. Route by message type:                  │    │
│  │    - ask → handleAsk()                     │    │
│  │    - cmd → handleCmd()                     │    │
│  │    - console_ask → handleConsoleAsk()      │    │
│  │ 4. Execute via engine (LLMChat)            │    │
│  │ 5. Reply via mailbox                       │    │
│  │ 6. Ack original message                    │    │
│  └─────────────────────────────────────────────┘    │
│                                                      │
└─────────────────────────────────────────────────────┘
```

### Sending to Agent Daemon

```bash
# Send ask message to running agent
agentctl agent ask <agent-id> \
  --question "What's the status?" \
  --conversation-id "user-session" \
  --wait

# Send command to agent
agentctl agent cmd <agent-id> \
  --command "summarize" \
  --args '{"depth": 2}'
```

### Message Types for Agents

| Type | Handler | Purpose |
|------|---------|---------|
| `ask` | handleAsk | Question requiring response |
| `cmd` | handleCmd | Command/action to execute |
| `console_ask` | handleConsoleAsk | Interactive console message |

### Storage

| Table | Location |
|-------|----------|
| `mailbox_messages` | `~/.agentctl/storage/mailbox.db` |
| `message_receipts` | `~/.agentctl/storage/mailbox.db` |

## Related

- [Agent Daemon](../../docs/general/agent-daemon.md) - Daemon architecture
- [Companion Memory](../../docs/general/companion-memory.md) - Conversation memory for agents
