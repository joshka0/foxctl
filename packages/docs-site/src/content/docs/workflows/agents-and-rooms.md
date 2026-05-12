---
title: Agents and rooms
description: End-to-end workflow for combining agents with rooms for multi-agent coordination.
---

foxctl supports local and durable agent coordination through the agent daemon and the room system. Agents operate independently or as part of an overseer-managed hierarchy, while rooms provide a durable transport layer for message exchange, task management, and coordination between agents. This page walks through the complete workflow from room creation to agent coordination.

## When to use agents and rooms together

Use agents alone when:

- You need a single autonomous research or coding task.
- The task is bounded and doesn't require multi-party coordination.

Use agents with rooms when:

- Multiple agents need to coordinate on a shared task.
- You need durable message history and task tracking.
- An overseer manages a tree of child agents.
- Agents need to communicate asynchronously across different execution schedules.

## Step 1: Create a room

Rooms are the coordination backbone. Create one before spawning agents that need to collaborate:

```bash
foxctl room create --title "Storage layer refactor" --description "Coordinate backend and test agents for the storage refactor"
```

Rooms persist messages, tasks, and membership state. They are identified by a room ID that agents reference when joining.

## Step 2: Spawn agents

Spawn the agents that will work in the room. For a coordinated workflow, spawn an overseer first and let it manage child agents:

```bash
# Spawn an overseer to coordinate the work
foxctl agent spawn \
  --role overseer \
  --prompt "Coordinate a code review and refactor of the storage layer. Break work into backend and test tasks." \
  --exec-mode autonomous \
  --max-auto-turns 5
```

Or spawn individual agents and join them to the room manually:

```bash
# Research agent to investigate the codebase
foxctl agent spawn \
  --role researcher \
  --prompt "Research the storage architecture and identify refactoring candidates" \
  --exec-mode proactive \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20

# Coder agent to implement changes
foxctl agent spawn \
  --role coder \
  --prompt "Implement the storage layer refactoring" \
  --exec-mode reactive
```

## Step 3: Join agents to the room

Each agent joins the room to participate in coordination:

```bash
# Join an agent to the room
foxctl room join --room-id <room-id> --participant-id <agent-id>
```

Agents that join a room can:

- Send and receive messages through the room's transport.
- Be assigned tasks from the room's task board.
- Subscribe to room events and updates.

## Step 4: Send messages and assign tasks

### Message exchange

Agents communicate through the room's message system:

```bash
# Send a message to a specific agent in the room
foxctl room send --room-id <room-id> --recipient <agent-id> --text "Focus on the SQLite migration path first"

# Send a broadcast message to all room members
foxctl room send --room-id <room-id> --text "Sprint review in 5 minutes"
```

Message kinds include:

| Kind | Purpose |
|------|---------|
| `instruction` | Directives — who should do what next |
| `info` | Status updates and context |
| `alert` | Warnings and blocker notifications |

### Task management

Rooms support a task board for tracking work:

```bash
# Add a task to the room
foxctl room task add --room-id <room-id> --title "Migrate SQLite to Turso" --description "Replace direct SQLite with Turso client"

# Assign the task to an agent
foxctl room task assign --room-id <room-id> --task-id <task-id> --recipient <agent-id>

# Mark a task as complete
foxctl room task complete --room-id <room-id> --task-id <task-id> --notes "Migration complete, tests passing"
```

Tasks can have dependencies, scope paths, and blockers:

```bash
# Add a task with dependencies
foxctl room task add --room-id <room-id> \
  --title "Write integration tests" \
  --depends-on <earlier-task-id>

# Block a task
foxctl room task block --room-id <room-id> --task-id <task-id> --reason "Waiting on upstream schema change"
```

## Step 5: Coordinate with the overseer

When using an overseer, the coordination flow follows the spawn protocol:

1. The overseer receives a goal and creates a plan.
2. The overseer spawns child agents for parallel or specialized work.
3. Child agents execute their tasks and report results back.
4. The overseer aggregates results and either continues or completes.

The overseer enforces depth constraints to prevent unmanaged spawn trees:

```
overseer (Depth=0, MaxDepth=3)
├── coder-backend (Depth=1)
│   └── reviewer-api (Depth=2)
└── coder-frontend (Depth=1)
```

See [Overseer and orchestration](/agents/orchestration) for the full spawn protocol and depth rules.

## Step 6: Inspect and monitor

### Agent status

```bash
# Check a specific agent's state
foxctl agent info <agent-id>

# Stream live events
foxctl agent watch <agent-id>

# View the agent hierarchy
foxctl agent hierarchy <session-id>
```

### Room status

```bash
# Show room state including members and recent messages
foxctl room status --room-id <room-id>

# Check inbox for messages addressed to a participant
foxctl room inbox --room-id <room-id> --participant-id <agent-id>

# List tasks in the room
foxctl room task list --room-id <room-id>
```

## Step 7: Ask and reply

Use the ask/reply pipeline for synchronous agent interactions:

```bash
# Ask an agent a question and wait for the reply
foxctl agent ask <agent-id> --question "What did you find about the storage architecture?" --wait
```

The ask flows through the mailbox system:

```text
CLI (agent ask)
  → mailbox.Send(ask)
  → daemon poll loop receives message
  → agent runtime executes turn(s)
  → mailbox.Send(reply)
  → CLI polls caller namespace and returns reply
```

For proactive agents that have already completed autonomous research, asking a follow-up question uses the full accumulated conversation history.

## Step 8: Resume and continue

Resume a previous session to continue work:

```bash
foxctl agent resume <session-id> --prompt "Continue from the prior summary"
```

The agent retains its conversation history across resume calls, bounded by the token limit configured at spawn time.

## Complete workflow example

Here is a full end-to-end example that researches a codebase, plans work, and coordinates execution:

```bash
# 1. Create a coordination room
foxctl room create --title "Auth refactor" --description "Refactor authentication to use Casbin"

# 2. Spawn a researcher to investigate
RESEARCHER=$(foxctl agent spawn --role researcher \
  --prompt "Research the current auth implementation. Read source files and identify what needs to change for Casbin integration." \
  --exec-mode autonomous \
  --max-auto-turns 3 --max-iterations 20 \
  --format json | jq -r '.data.agent_id')

# 3. Wait for research to complete, then ask for findings
foxctl agent ask $RESEARCHER --question "Summarize the auth files and the changes needed" --wait --timeout 120s

# 4. Join the researcher to the room
foxctl room join --room-id <room-id> --participant-id $RESEARCHER

# 5. Spawn an overseer to plan and coordinate implementation
foxctl agent spawn --role overseer \
  --prompt "Coordinate the Casbin auth refactor. Spawn coder agents for implementation and a reviewer for quality checks." \
  --exec-mode autonomous \
  --max-auto-turns 5

# 6. Monitor progress
foxctl room status --room-id <room-id>
foxctl room task list --room-id <room-id>
```

## Mail and message patterns

Room messages and agent mailbox messages serve complementary purposes:

| Channel | Use for | Persistence |
|---------|---------|-------------|
| Room messages | Multi-party coordination, task assignment, status broadcasts | Durable, room-scoped |
| Agent mailbox | Direct ask/reply, spawn requests, result aggregation | Durable, agent-scoped |
| Room tasks | Work tracking with assignment, blockers, dependencies | Durable, room-scoped |

### Coordination patterns

**Sequential handoff**: Agent A completes research, sends results to the room, and agent B picks up implementation based on those results.

```bash
# Agent A sends research results
foxctl room send --room-id <room-id> --kind info --text "Research complete. Key files: auth.go, middleware.go, policy.go"

# Agent B reads the message and starts implementation
foxctl room inbox --room-id <room-id> --participant-id <agent-b-id>
```

**Parallel execution**: An overseer spawns multiple coder agents for independent work streams, then aggregates results.

```bash
# Overseer spawns parallel workers (internally via agent.spawn tool)
# Each worker reports results via result.ready:<task-id> mail
# Overseer aggregates and completes the epic
```

**Review cycle**: A coder completes work, a reviewer inspects, and the coder addresses feedback.

```bash
# Add a review task
foxctl room task add --room-id <room-id> --title "Review auth changes" --assign-to <reviewer-id>

# Reviewer sends feedback
foxctl room send --room-id <room-id> --recipient <coder-id> --text "Add tests for the policy edge cases"
```
