# RLM Query Runtime

Status: proposed

## Purpose

Define a bounded RLM *(Recursive Language Models)* runtime for `agentctl`.

This runtime is:

- query-time
- read-only in the first version
- external-state oriented
- evidence-producing

It is not a replacement for ACA, companion memory, or embeddings.

## Core Position

`agentctl` uses three different layers:

- memory and ACA for durable structured state
- embeddings and indexes for candidate generation
- RLM for selective reasoning over large external state

In other words:

```text
memory plane   -> what is stored
embedding      -> what is likely relevant
RLM runtime    -> how to inspect and reason over it
```

## Runtime Contract

An RLM run accepts:

- a prompt
- workspace identity
- a bounded environment
- a bounded tool surface
- recursion/iteration limits

And returns:

- final answer
- evidence refs
- execution metadata
- trajectory reference

### Minimal data model

```go
type Task struct {
    Prompt        string
    WorkspaceID   string
    WorkspaceRoot string
    MaxDepth      int
    MaxIterations int
    MaxSubcalls   int
}

type Environment struct {
    TopOfMind       map[string]any
    LatestHandoff   map[string]any
    ActiveThreadIDs []string
    SceneHandles    []string
    ArtifactHandles []string
    RepoHandles     []string
    VaultHandles    []string
    Tools           []Tool
}

type Result struct {
    Answer       string
    EvidenceRefs []string
    Iterations   int
    Subcalls     int
    TrajectoryID string
}
```

## Environment Semantics

The environment should be typed, not one giant prompt string.

Recommended working set:

```text
prompt
workspace
top_of_mind
active_threads
scene_handles
artifact_handles
repo_handles
tool_functions
scratch_state
```

This lets the root model reason at the level of:

- thread
- scene
- artifact
- file
- code graph node

instead of forcing everything into one flat transcript.

## Tool Surface

First-version tools should be read-only.

Recommended tools:

- `get_top_of_mind`
- `get_latest_handoff`
- `search_scenes`
- `get_scene`
- `search_artifacts`
- `load_artifact`
- `search_repo`
- `expand_repo_graph`
- `load_file`
- `search_vault`
- `read_note`
- `subcall`

These should be typed wrappers over existing `agentctl` stores and indexes, not
raw database access.

## Safety Rules

Version 1 rules:

- no memory writes
- no file mutation
- no repo mutation
- no shell execution from the model
- no unbounded recursion
- no unrestricted external network access

Default limits:

- max depth: `1`
- max iterations: bounded
- max subcalls: bounded

## Sandbox Contract

The runtime executes model-guided code inside a controlled sandbox.

```go
type Sandbox interface {
    Init(ctx context.Context, state map[string]any) error
    Execute(ctx context.Context, code string) (ExecResult, error)
    Snapshot(ctx context.Context) (map[string]any, error)
    Close(ctx context.Context) error
}
```

Execution modes:

- local sandbox for development
- containerized sandbox for normal use
- optional remote sandbox later

Local same-process execution should be treated as development-only.

## Persistence and Logging

Every RLM run should emit structured trace data:

- root iterations
- executed code
- tool calls
- subcalls
- final answer
- evidence refs

These traces should persist through existing trajectory/event infrastructure.

Derived outputs may later become:

- scenes
- observations
- thread updates

but the first version should only guarantee trajectory persistence.

## Routing Policy

Do not use RLM for every query.

Recommended routing:

- use normal ACA/repo/vault retrieval for simple tasks
- use RLM only for:
  - large-context inspection
  - cross-session synthesis
  - evidence-heavy reasoning
  - repo-scale multi-file understanding

## Integration Targets

Current code that should feed the runtime:

- `internal/context/contextplane/*`
- `internal/context/companion/*`
- `internal/runtime/engine/rlm_tools.go`
- `internal/intelligence/indexing/repoindex/*`
- `internal/storage/obsidianindex/*`
- `internal/runtime/trajectorycapture/*`
- `internal/v2/runtime/*`

## Non-goals

First version is not:

- a fully autonomous recursive agent
- a write-capable recursive planner
- a replacement for companion memory
- a replacement for ACA
- a replacement for repo/vault retrieval

## Recommended First Deliverables

1. `internal/rlm`
   - read-only runner interfaces
   - no-op/local experimental implementation
2. environment bootstrap from ACA + companion + repo/vault handles
3. trajectory logging for all RLM runs
4. experimental CLI entrypoint

```text
agentctl rlm run
```

## Related

- [agentctl-rlm-integration-outline.md](../plans/features/agentctl-rlm-integration-outline.md)
- [v2_greenfield_bootstrap.md](v2_greenfield_bootstrap.md)
