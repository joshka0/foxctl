# AgentCTL RLM Integration Outline

Status: proposed

## Intent

Add an RLM *(Recursive Language Models)* execution layer to `agentctl` as a
read-mostly query-time runtime over existing memory, repo, and artifact stores.

The key architectural decision is:

- memory remains the durable system of record
- embeddings remain the candidate-generation layer
- RLM becomes the selective reasoning/exploration layer for hard queries over
  large external state

This is not a proposal to replace the Memory Canon v2 or ACA. It is a proposal
to give the model a better way to inspect them.

## Current Building Blocks

The repo already has several pieces that make an RLM layer practical:

- `internal/runtime/engine/rlm_tools.go`
  - existing RLM-style tool executor over external context state
  - already supports context variables, semantic memory lookup, and companion DB access
- `internal/storage/contextvar`
  - persistent external state for context variables
- `internal/context/companion/*`
  - hybrid companion memory with:
    - conversation events
    - hard state entries
    - soft episodes
    - evidence snippets
    - assumptions ledger
- `internal/context/contextplane/*`
  - ACA control plane:
    - top-of-mind
    - handoffs
    - observations
    - tensions
    - retrieve/report/rethink/dispatch
- `internal/intelligence/indexing/repoindex/*`
  - code + infra graph indexing
- `internal/storage/obsidianindex/*`
  - durable knowledge plane index
- `internal/runtime/runservice/*`, `internal/trajectorycapture/*`
  - event/result/trajectory persistence
- `internal/v2/runtime/*`, `internal/v2/services/*`
  - clean place to host a future production execution mode

That means the repo is already close to an RLM substrate. What is missing is a
disciplined runtime around those stores.

## Mental Model

Use a two-plane interpretation:

```text
Memory plane
  - ACA state
  - companion episodic state
  - artifacts
  - repo indexes
  - vault indexes
  - trajectories

RLM execution plane
  - bounded external environment
  - tool/function surface over memory + repo + artifacts
  - optional recursive subcalls
  - evidence-producing final synthesis
```

So:

- Memory v2 answers what is stored and how it is organized
- Embeddings answer what is likely relevant
- RLM answers how to inspect and reason over the external state without trying
  to inline everything into one prompt

## Recommended First Architecture

```text
user query
  -> normal retrieval planner
  -> if simple:
       standard answer using ACA + repo/vault retrieval
  -> if hard or large-context:
       launch read-only RLM mode
         -> bootstrap env from threads/scenes/artifacts/repo handles
         -> let root model inspect external state through typed tools
         -> allow bounded subcalls over selected slices
         -> return answer + evidence
         -> persist trajectory + derived scene
```

This keeps RLM expensive and selective rather than universal.

## Package Boundary Proposal

Create a dedicated experimental package family:

```text
internal/rlm/
  runner.go
  task.go
  result.go
  env/
    bootstrap.go
    handles.go
    tools.go
  sandbox/
    interface.go
    local.go
    docker.go
  logging/
    trajectory.go
  policy/
    rollout.go
```

Do not hide RLM inside `companion` or `contextplane`. Those should be inputs.

Why:

- `companion` is already a hybrid memory consumer and generator
- `contextplane` is the ACA state model
- `repoindex` and `obsidianindex` are retrieval substrates
- RLM should sit above them as a query-time orchestrator

## Concrete Interfaces

First read-only version:

```go
type Task struct {
    Prompt         string
    WorkspaceID    string
    WorkspaceRoot  string
    MaxDepth       int
    MaxIterations  int
    MaxSubcalls    int
}

type Environment struct {
    TopOfMind        any
    LatestHandoff    any
    ActiveThreadIDs  []string
    SceneHandles     []string
    ArtifactHandles  []string
    RepoHandles      []string
    VaultHandles     []string
    Toolset          []Tool
}

type Result struct {
    Answer         string
    EvidenceRefs   []string
    Iterations     int
    Subcalls       int
    TrajectoryID   string
}

type Runner interface {
    Run(ctx context.Context, task Task, env Environment) (Result, error)
}
```

Sandbox boundary:

```go
type Sandbox interface {
    Init(ctx context.Context, state map[string]any) error
    Execute(ctx context.Context, code string) (ExecResult, error)
    Snapshot(ctx context.Context) (map[string]any, error)
    Close(ctx context.Context) error
}
```

The first implementation should be read-only and depth-limited.

## Tool Surface for RLM

Expose typed read tools instead of generic DB access:

- `search_artifacts(query, filters)`
- `load_artifact(handle, slice_spec)`
- `search_scenes(query, thread_id)`
- `get_scene(scene_id)`
- `get_thread_snapshot(thread_id)`
- `get_top_of_mind(workspace_id)`
- `get_latest_handoff(workspace_id)`
- `search_repo(query, scope)`
- `load_file(path, line_range)`
- `expand_repo_graph(seed, edges, depth)`
- `search_vault(query, filters)`
- `read_note(path, heading)`
- `subcall(prompt, handle_set)`

These map well onto:

- `internal/context/contextplane`
- `internal/storage/contextvar`
- `internal/context/companion`
- `internal/intelligence/indexing/repoindex`
- `internal/storage/obsidianindex`
- `internal/trajectorycapture`

## How Scenes Fit

The strongest fit is scene-first recursion:

```text
thread
  -> scene
    -> observation
      -> artifact slice / transcript slice / file slice
```

This hierarchy is almost ideal for RLM because the root model can:

- inspect a thread summary first
- select a few scenes
- descend into only the relevant artifacts or files

That is much better than treating the whole history as one blob.

## How This Maps to Existing Code

### Phase 0: harden current proto-RLM pieces

Use the existing `internal/runtime/engine/rlm_tools.go` and `internal/context/companion/*` as
the seed, but do not let them become the final architecture.

Immediate actions:

- inventory current `RLMToolExecutor` capabilities and gaps
- document how `contextvar`, companion memory, and ACA overlap
- remove accidental duplication between companion recall and ACA retrieve where possible

### Phase 1: read-only RLM runner

Add `internal/rlm` with:

- `Task`
- `Environment`
- `Runner`
- `Sandbox`

Keep it read-only:

- no memory writes
- no file edits
- no repo mutations

Output should be:

- answer
- evidence refs
- trajectory ID

### Phase 2: bootstrap from ACA + companion

Environment bootstrap should combine:

- ACA `top_of_mind`
- latest handoff
- active observations/tensions
- companion scene/episode handles
- artifact handles
- repo/vault retrieval seeds

This is the point where RLM becomes useful for:

- repo-scale reasoning
- scene-thread reconstruction
- large transcript/doc inspection

### Phase 3: trajectory persistence

Every RLM run should persist:

- root iterations
- executed code
- tool calls
- subcalls
- final answer
- evidence refs

Store the raw trace as events and derive:

- trajectory
- optional scene
- optional observations

This makes the RLM itself part of the memory plane.

### Phase 4: sandboxing

Treat local same-process execution as dev-only.

Production shape:

- local sandbox for development
- containerized sandbox for normal use
- later remote sandbox if needed

### Phase 5: routing and policy

Add a simple gate:

- normal retrieval for easy tasks
- RLM mode only for hard or large-context tasks

Routing signals:

- too many candidate files/artifacts
- too many relevant scenes
- large transcript/doc/blob handle
- evidence-heavy question

## Evaluation Plan

Do not evaluate RLM on easy tasks first.

Use it on:

- repo-scale cross-file reasoning
- cross-session decision reconstruction
- evidence assembly across artifacts
- large-doc / large-transcript question answering

Track:

- answer quality
- evidence quality
- token use
- wall-clock latency
- recursion depth
- subcall count

Recommended default:

- depth `1`
- strict iteration budget
- read-only tools only

## Suggested First Deliverables

1. `docs/spec/rlm_query_runtime.md`
   - behavior contract
2. `internal/rlm/runner.go`
   - experimental runner interface + local stub
3. `internal/rlm/env/bootstrap.go`
   - build environment from ACA + companion + repo/vault handles
4. `internal/rlm/logging/trajectory.go`
   - persist raw execution trace to existing trajectory/event infrastructure
5. `cmd/agentctl/cmd/rlm.go`
   - experimental entrypoint:

```text
agentctl rlm run
```

## Non-goals for the First Version

- generic autonomous recursive agent
- write-capable recursive runtime
- deep recursion by default
- replacing ACA or Memory v2
- replacing embeddings or repo/vault retrieval

## Recommendation

Implement RLM as:

- a bounded read-only experimental runtime
- over ACA + companion + repo/vault state
- with full trajectory logging

That keeps the first version aligned with the current codebase and lets us
prove value without destabilizing the rest of the system.
