---
name: foxctl-flow-orchestration
description: "Build and run multi-agent DAG workflows using foxctl flow. Create pipelines, fan-outs, and complex agent coordination graphs."
user-invocable: true
---

# Foxctl Flow Orchestration

Use this skill when you need to coordinate multiple agents or skills into a
structured workflow — not just sequential chat, but a DAG where one agent's
output feeds into the next.

This skill covers the foxctl flow engine: a named directed graph of
envelope-producing nodes connected by typed edges with optional transforms.

## When to use me

- You need multiple agents to work in sequence (pipeline) or parallel (fan-out)
- You want one agent's structured output to become another agent's input
- You need to orchestrate skill chains (analyze → transform → write)
- You want durable workflow state that survives interruptions
- You need to run the same analysis across multiple files/languages in parallel

## Mental model

- A **flow** is a named graph of nodes and edges, stored in a workspace-scoped database
- A **node** is an execution unit: agent, skill, PTY, HTTP, transform, playwright, or image
- An **edge** routes envelope data from one node to the next with an optional transform
- Flows start from **source nodes** (no incoming edges) and terminate at **sink nodes**
- The engine topological-sorts the graph and executes in dependency order
- Edge **triggers** control when data flows: `output_ready` (default), `screen_match`, `exit`, `manual`

## Node kinds

| Kind | What it does | Key config fields |
|---|---|---|
| `agent` | Spawn a foxctl agent with role/prompt | `role`, `prompt`, `exec_mode`, `max_auto_turns`, `input_mode`, `output_mode` |
| `skill` | Execute a foxctl skill subprocess | `skill`, `extra_args`, `workspace` |
| `pty` | Run a terminal session | `cmd`, `cwd`, `adapter`, `submit_key` |
| `http` | Make an HTTP request | `url`, `method`, `headers`, `body_path` |
| `transform` | Pure data transform (no execution) | Config is the transform logic |
| `playwright` | Browser automation | Playwright-specific config |
| `image` | Image generation/capture | Image-specific config |

## Edge transforms

| Transform | What it does |
|---|---|
| `passthrough` | No transformation (default) |
| `regex_extract` | Extract matched groups from string output |
| `template` | Apply Go template to reshape data |
| `jq_filter` | JQ-style filter expression |
| `split_lines` | Split string into array of lines |
| `map_fields` | Rename/select fields from object |
| `file_write` | Write upstream data to a file |

## Agent node config

The `agent` node kind is the key primitive for multi-agent workflows:

```json
{
  "role": "researcher",
  "prompt": "Research the given topic and produce a structured analysis.",
  "exec_mode": "autonomous",
  "max_auto_turns": 3,
  "input_mode": "prompt",
  "output_mode": "session_summary",
  "skills_allow": ["fs/find", "code/symbols", "text/grep"]
}
```

- `input_mode: "prompt"` injects upstream data into the spawn prompt (default)
- `input_mode: "ask"` spawns first, then sends upstream data as an ask message
- `output_mode: "session_summary"` polls until completion (default)
- `output_mode: "ask"` uses the ask reply as output
- `output_mode: "push"` agent pushes output via `foxctl flow output`

## Workflow: Build and run a pipeline

### Option A: One-shot pipeline builder

Use `foxctl_flow_build_pipeline` to create a linear A → B → C chain:

```
foxctl_flow_build_pipeline(
  name="code-review-pipeline",
  description="Analyze → implement → review",
  stages=[
    {"kind": "agent", "label": "analyzer", "config": {"role": "analyst", "prompt": "...", "exec_mode": "autonomous"}},
    {"kind": "agent", "label": "implementer", "config": {"role": "coder", "prompt": "...", "exec_mode": "autonomous"}},
    {"kind": "agent", "label": "reviewer", "config": {"role": "reviewer", "prompt": "...", "exec_mode": "autonomous"}},
  ]
)
```

Then start it: `foxctl_flow_start(flow_id="code-review-pipeline")`

### Option B: Fan-out parallel work

Use `foxctl_flow_build_fan_out` to broadcast one source to multiple sinks:

```
foxctl_flow_build_fan_out(
  name="parallel-impl",
  source={"kind": "agent", "label": "analyst", "config": {...}},
  sinks=[
    {"kind": "agent", "label": "go-coder", "config": {...}},
    {"kind": "agent", "label": "python-coder", "config": {...}},
    {"kind": "agent", "label": "rust-coder", "config": {...}},
  ]
)
```

### Option C: Manual DAG construction

Build the graph node by node:

1. `foxctl_flow_create` — create the flow
2. `foxctl_flow_add_node` — add each node with its config
3. `foxctl_flow_add_edge` — connect nodes with transforms and triggers
4. `foxctl_flow_start` — execute
5. `foxctl_flow_status` — monitor
6. `foxctl_flow_logs` — inspect output

## Monitoring and debugging

- `foxctl_flow_status` shows per-node execution state and edge delivery counts
- `foxctl_flow_logs` shows envelope output from each node; filter with `node=`
- `foxctl_flow_stop` terminates a running flow
- `foxctl_flow_output` pushes external data into a running node

## Common patterns

### Code review pipeline
```
analyzer (agent) → implementer (agent) → reviewer (agent)
```
Analyzer finds issues, implementer fixes them, reviewer validates.

### Parallel research
```
coordinator (agent) → [go-expert, python-expert, rust-expert]
```
Coordinator breaks down a task, each expert handles their domain in parallel.

### Skill chain
```
find-files (skill: fs/find) → analyze-symbols (skill: code/symbols) → report (agent)
```
Skills do deterministic work, agent synthesizes the report.

### Human-in-the-loop
```
analyst (agent) → review-gate (manual trigger) → implementer (agent)
```
Analyst produces a plan, human approves via manual trigger, implementer executes.

## Rules

- Always inspect a flow with `foxctl_flow_show` before starting it
- Use `foxctl_flow_status` to monitor running flows — don't just fire-and-forget
- Clean up completed flows with `foxctl_flow_delete`
- Use transforms to reshape data between nodes when schemas differ
- Set `max_auto_turns` on agent nodes to prevent runaway execution
- Prefer `foxctl_flow_build_pipeline` and `foxctl_flow_build_fan_out` over manual construction for standard patterns
