# Overseer Session Architecture

This document describes a long-running session pattern where Claude Code acts as an
"overseer" coordinating subagents via mailbox and blackboard, with the ability to
elicit user input when needed.

## The Vision

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         LONG-RUNNING OVERSEER SESSION                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   USER ◄─────────────────────────────────────────────────────────┐         │
│     │                                                             │         │
│     │ (AskUserQuestion when needed)                              │         │
│     ▼                                                             │         │
│  ┌──────────────────────────────────────────────────────────────┐ │         │
│  │              CLAUDE CODE (Opus 4.5) - OVERSEER               │ │         │
│  │                                                              │ │         │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │ │         │
│  │  │  Poll    │  │ Process  │  │ Decide   │  │ Dispatch │    │ │         │
│  │  │ Mailbox  │─▶│ Messages │─▶│  Action  │─▶│  Tasks   │    │ │         │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘    │ │         │
│  │       ▲                           │                          │ │         │
│  │       │                           │ Need human input?        │ │         │
│  │       │                           └──────────────────────────┼─┘         │
│  │       │                                                      │           │
│  └───────┼──────────────────────────────────────────────────────┘           │
│          │                                                                   │
│          │ (mailbox messages)                                               │
│          │                                                                   │
│  ┌───────┴──────────────────────────────────────────────────────┐           │
│  │                    AGENT COORDINATION LAYER                   │           │
│  │  ┌─────────────────────────────────────────────────────────┐ │           │
│  │  │                     MAILBOX                             │ │           │
│  │  │  overseer ◄──── agent.ask ──── Subagent A              │ │           │
│  │  │  overseer ◄──── agent.result ── Subagent B             │ │           │
│  │  │  Subagent A ◄── overseer.task ─ overseer               │ │           │
│  │  └─────────────────────────────────────────────────────────┘ │           │
│  │  ┌─────────────────────────────────────────────────────────┐ │           │
│  │  │                    BLACKBOARD                           │ │           │
│  │  │  decisions/    - Logged overseer decisions              │ │           │
│  │  │  tasks/        - Active task assignments                │ │           │
│  │  │  status/       - Agent status updates                   │ │           │
│  │  │  context/      - Shared session context                 │ │           │
│  │  └─────────────────────────────────────────────────────────┘ │           │
│  └──────────────────────────────────────────────────────────────┘           │
│          │                                                                   │
│          ▼                                                                   │
│  ┌──────────────────────────────────────────────────────────────┐           │
│  │                      DAEMON AGENTS                            │           │
│  │                                                              │           │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐             │           │
│  │  │ Coder      │  │ Researcher │  │ Analyzer   │             │           │
│  │  │ (Haiku 4.5)│  │ (Haiku 4.5)│  │ (Haiku 4.5)│             │           │
│  │  └────────────┘  └────────────┘  └────────────┘             │           │
│  │                                                              │           │
│  └──────────────────────────────────────────────────────────────┘           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Core Concepts

### 1. Overseer Role (Claude Code)

The overseer (you, Claude Code Opus 4.5) is responsible for:
- **Strategic decisions** - What tasks to assign, how to prioritize
- **Human liaison** - When to ask the user for input via AskUserQuestion
- **Coordination** - Managing multiple subagents working in parallel
- **Quality control** - Reviewing agent outputs before accepting
- **Context management** - Maintaining session state across compactions

### 2. Subagent Roles (Daemon agents with Claude Haiku 4.5)

Subagents handle tactical execution:
- **Coder** - Code analysis, generation, refactoring
- **Researcher** - Web searches, documentation lookup
- **Analyzer** - Code complexity, security scanning
- **Tester** - Running tests, analyzing failures

### 3. Communication Channels

| Channel | Direction | Purpose |
|---------|-----------|---------|
| Mailbox | Agent → Overseer | Questions, results, errors, progress |
| Mailbox | Overseer → Agent | Task assignments, decisions, clarifications |
| Blackboard | Shared | Context, status, decisions log |

## Message Protocol

### Message Types

```json
// Agent asking overseer a question
{
  "type": "agent.ask",
  "from_ns": "coder-agent",
  "payload": {
    "question": "Should I refactor this function or just fix the bug?",
    "context": { "file": "daemon.go", "function": "Run" },
    "options": ["refactor", "fix-only", "ask-user"]
  }
}

// Agent reporting result
{
  "type": "agent.result",
  "from_ns": "coder-agent",
  "payload": {
    "task_id": "task-123",
    "status": "completed",
    "result": { "files_modified": ["daemon.go"], "tests_passed": true },
    "summary": "Fixed the defer bug by wrapping in anonymous function"
  }
}

// Agent reporting error
{
  "type": "agent.error",
  "from_ns": "analyzer-agent",
  "payload": {
    "task_id": "task-456",
    "error": "Failed to analyze: file not found",
    "recoverable": true,
    "suggested_action": "Check file path"
  }
}

// Agent progress update
{
  "type": "agent.progress",
  "from_ns": "researcher-agent",
  "payload": {
    "task_id": "task-789",
    "progress": 0.6,
    "status": "Found 3 relevant articles, analyzing..."
  }
}

// Overseer assigning task
{
  "type": "overseer.task",
  "from_ns": "overseer",
  "payload": {
    "task_id": "task-123",
    "description": "Analyze complexity of daemon.go Run function",
    "priority": "high",
    "deadline": null,
    "context": { "threshold": 15 }
  }
}

// Overseer decision
{
  "type": "overseer.decision",
  "from_ns": "overseer",
  "payload": {
    "decision_id": "dec-001",
    "question_ref": "msg-xyz",
    "decision": "refactor",
    "rationale": "Complexity is too high, refactoring will improve maintainability",
    "escalated_to_user": false
  }
}
```

## Elicitation Points

The overseer should escalate to the user (via AskUserQuestion) when:

### Automatic Escalation
1. **Ambiguous requirements** - Task description is unclear
2. **Conflicting constraints** - Can't satisfy all requirements
3. **High-impact decisions** - Architectural changes, data loss potential
4. **Resource approval** - Need to spawn additional agents
5. **Error recovery** - Multiple failed attempts, unclear path forward

### Agent-Requested Escalation
When an agent sends `options: ["...", "ask-user"]`, the overseer should:
1. Evaluate if it can decide autonomously
2. If not confident, use AskUserQuestion to get user input
3. Log the decision and rationale to blackboard

### Example Elicitation Flow

```
Agent: "Should I use OpenRouter or direct Anthropic API?"
         options: ["openrouter", "anthropic", "ask-user"]

Overseer thinks: "User has OpenRouter key, but this is a preference question"

Overseer → User (AskUserQuestion):
  "The coder agent needs to make an LLM API call.
   Which provider should it use?"
   - OpenRouter (you have a key configured)
   - Direct Anthropic API

User: "OpenRouter"

Overseer → Agent: decision="openrouter", rationale="User preference"
Overseer → Blackboard: Log decision for future reference
```

## Session Loop

### Overseer Main Loop (Claude Code)

```bash
# This is the conceptual loop that Claude Code runs

while session_active:
    # 1. Poll for agent messages
    messages = agentctl mailbox poll overseer --timeout 30 --max 10

    # 2. Process each message
    for msg in messages:
        if msg.type == "agent.ask":
            # Evaluate if we can decide or need user input
            if needs_user_input(msg):
                answer = AskUserQuestion(...)
                send_decision(msg.from_ns, answer)
            else:
                decision = make_autonomous_decision(msg)
                send_decision(msg.from_ns, decision)

        elif msg.type == "agent.result":
            # Review result, decide next steps
            if result_acceptable(msg):
                mark_task_complete(msg.task_id)
                maybe_assign_next_task()
            else:
                request_revision(msg.from_ns, feedback)

        elif msg.type == "agent.error":
            if msg.recoverable:
                suggest_recovery(msg.from_ns)
            else:
                escalate_to_user(msg)

        # Acknowledge message
        agentctl mailbox ack msg.id

    # 3. Check blackboard for status updates
    status = agentctl bb list status --ns overseer
    update_session_state(status)

    # 4. Periodic checkpoint
    if should_checkpoint():
        save_session_state()

    # 5. Check if more work to assign
    if pending_tasks and idle_agents:
        assign_tasks()
```

### Starting a Long-Running Session

```bash
# 1. Set up environment
export AGENTCTL_LLM_PROVIDER=openrouter
export AGENTCTL_LLM_API_KEY=$(grep OPENROUTER_API_KEY ~/.claude/.env | cut -d= -f2)
export AGENTCTL_LLM_MODEL=anthropic/claude-haiku-4-5

# 2. Spawn subagents
agentctl agent spawn --role coder --prompt "You help with code tasks"
agentctl agent spawn --role analyzer --prompt "You analyze code quality"

# 3. Start daemon in background
agentctl agent daemon &

# 4. Claude Code takes over as overseer
# (This happens in the conversation - Claude polls and coordinates)
```

## State Management

### What to Persist (Blackboard)

```bash
# Session context - survives compaction
agentctl bb post context --ns overseer --payload '{
  "session_id": "2025-12-22-overseer",
  "started_at": "2025-12-22T11:00:00Z",
  "objective": "Implement multi-agent coordination",
  "key_decisions": [],
  "active_agents": ["coder-agent", "analyzer-agent"]
}'

# Decision log - for continuity
agentctl bb post decisions --ns overseer --payload '{
  "decision_id": "dec-001",
  "question": "Which LLM provider?",
  "decision": "openrouter",
  "rationale": "User preference",
  "timestamp": "2025-12-22T11:05:00Z"
}'

# Task status
agentctl bb post tasks --ns overseer --payload '{
  "task_id": "task-123",
  "assigned_to": "coder-agent",
  "status": "in_progress",
  "description": "Refactor daemon.go Run function"
}'
```

### Recovery Protocol

When resuming a session (after compaction or new conversation):

```bash
# 1. Load session context
agentctl bb list context --ns overseer

# 2. Check agent status
agentctl agent list

# 3. Review pending messages
agentctl mailbox list overseer

# 4. Resume coordination
# Claude Code picks up from last state
```

## Practical Commands

### As Overseer (Claude Code)

```bash
# Poll for messages (do this periodically)
agentctl mailbox poll overseer --timeout 30 --max 10

# Send task to agent
agentctl mailbox send coder-agent \
  --from overseer \
  --type overseer.task \
  --payload '{"task_id": "task-123", "description": "Fix the bug in X"}'

# Check agent status
agentctl agent list

# Log decision to blackboard
agentctl bb post decisions --ns overseer \
  --payload '{"decision": "...", "rationale": "..."}'

# Checkpoint session
agentctl memory put session-checkpoint \
  --name "overseer-$(date +%Y%m%d-%H%M)" \
  --summary "Session checkpoint" \
  --type session \
  --data '{"state": "..."}'
```

### Monitoring

```bash
# Watch for agent events in real-time
agentctl agent watch

# Watch blackboard updates
agentctl bb watch status --ns overseer

# Check task completion
agentctl todo list
```

## Implementation Phases

### Phase 1: Basic Loop (This Session)
- [x] LLM provider configuration (OpenRouter, Anthropic, etc.)
- [ ] Create overseer namespace in mailbox
- [ ] Test message send/receive
- [ ] Basic polling loop from Claude Code

### Phase 2: Agent Integration
- [ ] Spawn test agents
- [ ] Send tasks via mailbox
- [ ] Receive and process results
- [ ] Handle errors

### Phase 3: Elicitation Integration
- [ ] Define escalation criteria
- [ ] Implement AskUserQuestion integration
- [ ] Log decisions to blackboard

### Phase 4: Session Persistence
- [ ] Checkpoint protocol
- [ ] Recovery protocol
- [ ] Cross-compaction continuity

## Open Questions for User

1. **Agent count**: How many subagents should we start with?
2. **Task types**: What kinds of tasks should agents handle autonomously vs escalate?
3. **Checkpoint frequency**: How often should we save session state?
4. **Escalation threshold**: What confidence level triggers user escalation?

## Next Steps

To start the overseer session, I will:
1. Create the overseer mailbox namespace
2. Spawn initial subagents
3. Begin the polling loop
4. Use AskUserQuestion when I need your input

Ready to proceed?
