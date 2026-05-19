---
name: foxctl-agent-coordination
description: "Protocol for agents coordinating through foxctl rooms: claim tasks, broadcast context, pipe data, and build multi-agent workflows."
user-invocable: true
---

# Foxctl Agent Coordination

Use this skill when multiple agents need to coordinate work through a foxctl room.
This covers the full coordination stack from low-level pipes to high-level flow DAGs.

This skill assumes you are already a room participant (see `foxctl-room-agent`).

## When to use me

- Multiple agents are in a room and need to divide work
- You need to send structured data to another agent
- You want to claim and complete tasks from a shared backlog
- You need to build a workflow where agents pass work to each other
- You want to broadcast your current state so other agents know what you're doing

## The coordination stack

```
Layer 5: Flow DAG        — Multi-agent orchestration (n8n-like)
                           analyzer → implementer → reviewer
                           analyst → [go-coder, python-coder, rust-coder]
Layer 4: Pipe Protocol   — Structured agent-to-agent data flow
Layer 3: Room Tasks      — Claim/complete/block work items
Layer 2: Room Messages   — Durable inbox + relay to terminals
Layer 1: Room Relay      — Herdr terminal delivery to panes
```

Use the highest layer that fits your needs:
- Simple request → room message (Layer 2)
- Structured data exchange → pipe (Layer 4)
- Work allocation → room tasks (Layer 3)
- Complex multi-step workflow → flow DAG (Layer 5)

## Layer 3: Room Tasks

Room tasks are the work allocation layer. Any participant can create tasks;
agents claim and complete them.

### Create a task
```
foxctl_room_task_add(title="Optimize embedding worker", description="Increase batch size from 20 to 50")
```

### Claim a task
```
foxctl_room_task_list(status="pending")  → find available tasks
foxctl_room_task_claim(task_id="...")    → claim it (assigned to you)
```

### Complete a task
```
foxctl_room_task_complete(task_id="...", notes="Batch size increased, 2.5x throughput improvement")
```

### Block a task
```
foxctl_room_task_block(task_id="...", reason="Waiting for embedding server restart")
```

### Abandon a task
```
foxctl_room_task_abandon(task_id="...", reason="Cannot reproduce the issue")
```

## Layer 4: Pipe Protocol

Pipes are lightweight structured data channels between agents. One agent emits,
others receive.

### Emit data
```
foxctl_pipe_emit(
  pipe_id="code-review",
  payload='{"findings": ["unused import", "missing error handling"], "file": "main.go"}',
  target_agents=["actor:pi:local"]
)
```

### Receive data
```
foxctl_pipe_receive(pipe_id="code-review")  → get pending pipe messages
```

### Pipe conventions

- `pipe_id` should be descriptive: `code-review`, `test-results`, `architecture-analysis`
- `target_agents=["*"]` broadcasts to all room participants
- Pipe messages are room messages with subject `pipe:<pipe_id>`
- Consume promptly — messages accumulate in the inbox

## Layer 5: Flow DAG

Flows are the orchestration layer for complex multi-agent workflows. See
`foxctl-flow-orchestration` for the full reference.

### Quick pipeline
```
foxctl_flow_build_pipeline(
  name="review-pipeline",
  stages=[
    {"kind": "agent", "label": "analyzer", "config": {"role": "analyst", "prompt": "...", "exec_mode": "autonomous"}},
    {"kind": "agent", "label": "fixer", "config": {"role": "coder", "prompt": "...", "exec_mode": "autonomous"}},
  ]
)
foxctl_flow_start(flow_id="review-pipeline")
```

### Quick fan-out
```
foxctl_flow_build_fan_out(
  name="parallel-analysis",
  source={"kind": "agent", "label": "coordinator", "config": {...}},
  sinks=[
    {"kind": "agent", "label": "go-analyst", "config": {...}},
    {"kind": "agent", "label": "python-analyst", "config": {...}},
  ]
)
foxctl_flow_start(flow_id="parallel-analysis")
```

## Context broadcasting

Let other agents know what you're doing:

```
foxctl_publish_context(context={
  "current_task": "optimizing embedding worker",
  "progress": "60%",
  "blockers": [],
  "findings": ["Qwen3-Embedding-8B handles 4096d vectors"],
  "next_steps": ["benchmark batch size 50", "update config"]
})
```

## Agent discovery

Find who else is in the room:

```
foxctl_agent_list()  → all room participants with roles and status
foxctl_room_status()  → delivery binding status for each member
```

## Coordination patterns

### Claim → execute → report → handoff

The standard agent workflow:

```
1. foxctl_room_task_list(status="pending")     → find work
2. foxctl_room_task_claim(task_id="...")        → claim it
3. [do the work using intelligence tools]
4. foxctl_room_task_complete(task_id="...", notes="...")
5. foxctl_pipe_emit(pipe_id="handoff", payload=...)  → pass results to next agent
```

### Research → implement → review pipeline

```
1. foxctl_flow_build_pipeline with 3 agent stages
2. foxctl_flow_start
3. foxctl_flow_status (monitor)
4. foxctl_flow_logs(node="reviewer") (get final output)
```

### Parallel fan-out + aggregation

```
1. foxctl_flow_build_fan_out with specialist sinks
2. foxctl_flow_start
3. Each specialist works in parallel
4. Collect results from flow logs
```

### Proactive coordination

```
1. foxctl_context_curator()                    → check context plane health
2. foxctl_publish_context()                    → broadcast your state
3. foxctl_room_inbox()                         → check for messages
4. foxctl_room_task_list(status="pending")     → look for unclaimed work
5. If you see work you can do, claim and execute
```

## Rules

- Always claim before you start working on a task — prevents duplicate work
- Complete tasks with notes explaining what you did and any gotchas
- Use pipes for structured data, room messages for coordination/chat
- Use flows when the workflow has more than 2 steps or needs parallelism
- Broadcast your context periodically so other agents know what you're doing
- Check your inbox regularly — other agents may be waiting on you
- Run the context curator periodically to keep the shared context clean
- When blocking, always provide a reason so the coordinator can unblock you
