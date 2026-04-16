# foxctl `evolve` plan

| Field | Value |
|-------|--------|
| Status | Draft plan |
| Canonical architecture | [docs/architecture/system-architecture.md](../../architecture/system-architecture.md) |
| Package placement gate | [docs/architecture/package-topology.md](../../architecture/package-topology.md) |
| Baseline reference | Python `evo` core under `~/repos/githubs/evo/plugins/evo/src/evo/` |
| Last reviewed | 2026-04-15 |

## Goal

Add a foxctl-native repo-evolution tool that treats code changes as
hypothesis-driven experiments:

1. define a target and benchmark
2. create child experiments in isolated worktrees
3. run benchmark and inherited gates
4. keep, discard, or prune branches based on evidence
5. persist the experiment graph, attempts, and artifacts durably

This is not a plugin port. `evo` is a baseline reference for feature shape and
workflow semantics, but the implementation should be Go-native and aligned with
existing foxctl storage, worktree, CAS, and envelope contracts.

## Non-goals

- Recreating Claude Code or Codex plugin packaging.
- Preserving `.evo/` on-disk compatibility.
- Putting the feature under `internal/v2/*`.
- Building the full multi-agent autonomous loop in the first milestone.
- Replacing foxctl's existing worktree support with a new git wrapper.

## Naming and placement

### User-facing surface

- Top-level command family: `foxctl evolve`

This stays separate from `foxctl optimize`, which already means
model/prompt/dataset optimization rather than repository evolution.

### Package family

Primary package family:

- `internal/tooling/evolve`

Subpackages:

- `internal/tooling/evolve/model`
- `internal/tooling/evolve/store`
- `internal/tooling/evolve/engine`
- `internal/tooling/evolve/exec`
- `internal/tooling/evolve/view`

This follows the package-topology rule: repo-evolution is tooling built on
existing runtime and platform primitives, not a new `internal/v2` runtime lane.

## Existing foxctl primitives to reuse

### Worktree lifecycle

Reuse foxctl's existing worktree abstraction instead of porting `evo`'s git
subprocess calls:

- [`internal/platform/worktree/types.go`](../../../internal/platform/worktree/types.go)
- [`internal/platform/worktree/manager.go`](../../../internal/platform/worktree/manager.go)

### Envelope output

CLI responses should use the canonical envelope helpers:

- [`internal/protocol/protocol.go`](../../../internal/protocol/protocol.go)

### Durable storage and artifacts

- DB-backed durable state for runs, nodes, attempts, gates, annotations, and
  infra events
- CAS-backed large artifacts for diff patches, benchmark logs, gate logs, and
  trace bundles

## Storage and observability model

The source of truth should be the DB, not repo-local JSON files.

Why:

- foxctl already has durable stores and CAS
- SQL is a better fit for graph queries, active-run lookup, and history
- the Python file-first design exists because `evo` is standalone, not because
  files are the best long-term fit here

### DB as authority

Persist these as structured rows:

- runs
- experiment nodes
- gates
- attempts
- gate results
- annotations
- infra events

### CAS for bulky artifacts

Store large or replay-oriented payloads in CAS:

- benchmark stdout/stderr logs
- gate logs
- unified diffs / patches
- trace bundles
- optional scratchpad snapshots

### Optional repo-local materialization

Repo-local files under `.foxctl/evolve/` are allowed only as derived views, not
as authority. Early examples:

- `.foxctl/evolve/current/scratchpad.md`
- `.foxctl/evolve/current/run.json`

Those files may help humans and agents orient quickly, but they should be
generated from DB state.

### Observability via foxctl wide events

`evolve` should use foxctl's existing observability layer for operational
telemetry rather than inventing a parallel event store.

Use:

- [`internal/runtime/observability`](../../../internal/runtime/observability)
- [docs/general/context-and-observability.md](../../general/context-and-observability.md)

This means:

- DB rows remain the source of truth for domain state
- CAS remains the source of truth for bulky artifacts
- wide events capture lifecycle telemetry, durations, correlations, and
  high-signal summaries

Do not add a second `evolve_events` persistence subsystem unless a later need is
demonstrated that the existing observability lane cannot satisfy.

## Domain model

### Run

Represents one experiment campaign for a workspace.

Suggested fields:

```go
type Run struct {
    ID               string
    WorkspacePath    string
    TargetPath       string
    BenchmarkCommand string
    Metric           string // max|min
    Status           string
    Active           bool
    CreatedAt        time.Time
    UpdatedAt        time.Time
}
```

### Node

Represents one experiment branch in the tree.

```go
type Node struct {
    ID                string
    RunID             string
    ParentID          string
    Status            string
    Hypothesis        string
    Score             *float64
    EvalEpoch         int
    Branch            string
    WorktreePath      string
    CommitSHA         string
    PrunedReason      string
    CurrentAttempt    int
    EvaluatedAttempts int
    CreatedAt         time.Time
    UpdatedAt         time.Time
}
```

### Gate

```go
type Gate struct {
    ID        string
    RunID     string
    NodeID    string
    Name      string
    Command   string
    CreatedAt time.Time
}
```

### Attempt

```go
type Attempt struct {
    ID                string
    NodeID            string
    AttemptNo         int
    Status            string
    Score             *float64
    BenchmarkArtifact string
    TraceArtifact     string
    DiffArtifact      string
    Error             string
    StartedAt         time.Time
    FinishedAt        time.Time
}
```

### GateResult

```go
type GateResult struct {
    AttemptID     string
    GateName      string
    SourceNodeID  string
    Passed        bool
    ReturnCode    *int
    LogArtifact   string
}
```

### Annotation and infra event

```go
type Annotation struct {
    ID         string
    RunID      string
    NodeID     string
    TaskID     string
    Analysis   string
    CreatedAt  time.Time
}

type InfraEvent struct {
    ID         string
    RunID      string
    Message    string
    Breaking   bool
    CreatedAt  time.Time
}
```

## State machine

Preserve the useful `evo` node lifecycle semantics:

- `root`
- `pending`
- `active`
- `committed`
- `evaluated`
- `failed`
- `discarded`
- `pruned`

### Intended meaning

- `pending`: child exists but has not been run
- `active`: benchmark/gates currently executing
- `committed`: benchmark improved or matched policy and all required gates passed
- `evaluated`: run completed but did not meet keep policy
- `failed`: benchmark or gate execution failed structurally
- `discarded`: operator chose to abandon the node
- `pruned`: branch family intentionally marked as exhausted

### Core transition rules

- `init` creates synthetic `root`
- `new` creates `pending`
- `run` moves `pending|evaluated|failed -> active`
- successful keep decision moves `active -> committed`
- non-keep but completed decision moves `active -> evaluated`
- structural error moves `active -> failed`
- `discard` moves `evaluated|failed|pending -> discarded`
- `prune` marks one or more committed/evaluated branch points `pruned`

## Benchmark and gate contract

`evo`'s Python loop is worth preserving conceptually:

- benchmark command runs with worktree-specific environment
- score is extracted programmatically
- inherited gates are collected from the ancestor path
- keep/discard is deterministic

### Command expansion

Support these placeholders:

- `{worktree}` -> absolute experiment worktree path
- `{target}` -> absolute target path inside the worktree

### Environment variables

Provide a stable env contract during benchmark and gate execution:

- `FOXCTL_EVOLVE_RUN_ID`
- `FOXCTL_EVOLVE_NODE_ID`
- `FOXCTL_EVOLVE_ATTEMPT`
- `FOXCTL_EVOLVE_WORKTREE`
- `FOXCTL_EVOLVE_TARGET`
- `FOXCTL_EVOLVE_TRACES_DIR` or an equivalent trace output contract

Benchmark and gate subprocesses should also inherit foxctl trace propagation
using the existing observability environment contract so child-process telemetry
correlates with the parent attempt.

### Score parsing

Initial score parsing should intentionally be permissive, matching the Python
baseline behavior:

- JSON object with `score`
- JSON scalar number
- trailing line containing `score=<number>` or `score: <number>`
- trailing bare number line

### Keep policy

First milestone keep decision:

- benchmark score compares favorably to parent score by metric direction
- all inherited gates pass

Metric directions:

- `max`: keep when `candidate >= parent`
- `min`: keep when `candidate <= parent`

### Gate inheritance

Gates should be collected from the ancestor path, root first, and deduplicated
by gate name. This is one of the main useful semantics from `evo`.

## Views and read models

The feature needs human-readable and agent-readable views, but these should be
rendered from DB state rather than treated as canonical storage.

### Read surfaces for the first milestone

- `status`
- `tree`
- `frontier`
- `get`
- `path`
- `diff`
- `gate list`

### Derived views later

- `scratchpad`
- `annotations`
- `infra`
- dashboard DTOs / API payloads

## Observability design

`evolve` should emit structured wide events for all meaningful lifecycle
transitions and long-running operations.

### Why this is the right fit

- foxctl already has trace/span propagation and event persistence
- existing query and SSE bridges can be reused later
- lifecycle telemetry should not be encoded indirectly via domain tables
- dashboard/API work becomes easier if telemetry already follows the platform
  standard

### Event families

Suggested operation/event names:

- `evolve.run.init`
- `evolve.node.create`
- `evolve.attempt.start`
- `evolve.benchmark.finish`
- `evolve.gate.finish`
- `evolve.node.commit`
- `evolve.node.evaluate`
- `evolve.node.fail`
- `evolve.node.discard`
- `evolve.node.prune`
- `evolve.run.reset`

### Event fields

At minimum, include structured fields for:

- `run_id`
- `node_id`
- `attempt_no`
- `workspace`
- `target_path`
- `benchmark_command` or gate name where relevant
- `metric`
- `score` when available
- `status`
- `duration_ms`
- artifact digests for logs, diff, and traces where relevant

### Event semantics

- Use wide events for operational telemetry, not durable business truth
- Emit start/end or success/error pairs around expensive operations
- Preserve trace ID continuity across subprocesses
- Attach CAS digests rather than large inline payloads
- Keep the event names stable enough that `foxctl errors`, later dashboards,
  and ad hoc queries remain useful

## CLI surface

### Milestone 1

- `foxctl evolve init`
- `foxctl evolve new`
- `foxctl evolve run`
- `foxctl evolve status`
- `foxctl evolve tree`
- `foxctl evolve frontier`
- `foxctl evolve get`
- `foxctl evolve path`
- `foxctl evolve diff`
- `foxctl evolve gate add`
- `foxctl evolve gate list`
- `foxctl evolve gate remove`

### Milestone 2

- `foxctl evolve discard`
- `foxctl evolve prune`
- `foxctl evolve reset`
- `foxctl evolve scratchpad`
- `foxctl evolve annotate`
- `foxctl evolve annotations`
- `foxctl evolve infra`

### Milestone 3

- dashboard / API surface
- agent-driven branching and brief generation
- richer retention and pruning policy

## Package responsibilities

### `model`

Pure domain types and state-transition helpers:

- run and node state enums
- keep decision helpers
- frontier computation
- gate inheritance rules

### `store`

Persistence adapters and schema ownership:

- run store
- node store
- attempt store
- gate store
- annotation store
- infra event store

### `engine`

High-level workflow orchestration:

- initialize run
- create child experiment
- execute state transitions
- prune/discard/reset logic

### `exec`

Command execution and artifact capture:

- placeholder expansion
- env assembly
- benchmark/gate process execution
- stdout/stderr capture
- score parsing
- CAS persistence for large outputs
- observability event emission around benchmark and gate execution

### `view`

Read-model rendering:

- ASCII tree
- status summary
- frontier summary
- scratchpad rendering
- dashboard payload shaping

## DB schema sketch

The exact schema can evolve, but the first implementation should expect these
tables or equivalent store contracts:

- `evolve_runs`
- `evolve_nodes`
- `evolve_gates`
- `evolve_attempts`
- `evolve_gate_results`
- `evolve_annotations`
- `evolve_infra_events`

Important indexed access patterns:

- active run by workspace
- nodes by run and parent
- attempts by node ordered by attempt number
- gates by node
- frontier lookup by run and node status

Observability events should use the existing foxctl observability storage rather
than introducing a dedicated `evolve_events` table.

## Worktree integration plan

### `new`

Use `worktree.Manager.Create` to allocate the child worktree and branch.

Required behavior:

- branch naming under a foxctl-owned namespace
- base ref chosen from parent node state
- worktree path recorded on node creation
- stale worktree cleanup handled via the existing manager and explicit reset

### `reset`

Use `worktree.Manager.Remove` and existing prune support instead of rolling a
new removal flow.

Required behavior:

- remove child worktrees for the selected run
- delete associated branches when safe
- keep operations idempotent

## Artifact policy

Store these as CAS by default:

- diff patch for each attempt
- benchmark log bundle
- gate log bundle
- trace bundle when present

Keep structured summaries in DB rows:

- parsed score
- benchmark outcome summary
- gate pass/fail summary
- short error messages

This keeps envelopes small and preserves replay/debug value.

## Documentation follow-through

When implementation begins:

- add command docs under `docs/general/*` or a dedicated evolve guide
- update `docs/README.md`
- update architecture docs once `evolve` is real current-state behavior

Until implementation exists, this plan is the canonical reference for the
proposed design and should not be treated as current architecture.

## Open questions

1. Should active-run selection be single-run-per-workspace or multi-run with an
   explicit `--run` selector on every command?
2. Should branch naming include the workspace identifier for easier cross-repo
   debugging?
3. Should trace output be a file-path contract, a JSON envelope contract, or
   both?
4. Should `scratchpad` be persisted as a generated markdown file, stored in CAS,
   or rendered on demand only?
5. When agent orchestration arrives, should it live directly under `evolve` or
   compose with the existing room/orchestration surfaces?

## Recommended implementation order

1. Land `model` and `store` contracts with DB schema.
2. Land `init`, `status`, and `tree`.
3. Land `new` on top of `worktree.Manager`.
4. Land `run` with benchmark parsing, inherited gates, and attempt artifacts.
5. Land `frontier`, `get`, `path`, and `diff`.
6. Land `discard`, `prune`, and `reset`.
7. Add `scratchpad` and dashboard/API later.
