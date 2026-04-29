# Implementation Plan: RLM Recursive Fan-Out Runtime

## Problem Statement

Foxctl's current RLM work can run REPL-backed reasoning and can expose a
bounded `rlm_query` tool, but the runtime is still effectively synchronous:
the parent calls one child and waits for that child before continuing. That is
not the recursive language model shape we want.

The target behavior is:

- a parent model decomposes a task into independent subproblems;
- the parent calls `rlm_query` multiple times to create child reasoning nodes;
- the parent calls `rlm_wait` to collect completed children;
- child nodes may recursively create grandchildren within budget;
- each child returns structured findings, evidence, artifacts, telemetry, and
  errors;
- the parent synthesizes those child results and returns a compact result to
  its own parent.

This needs to be solved before LongCoT and agent eval work can make a credible
claim about RLM behavior. A single REPL call or one synchronous subcall is not
enough to test divide-and-conquer reasoning, parallel child exploration, or
cost/latency tradeoffs.

Expected user-facing outcome:

```bash
foxctl rlm run \
  --executor repl \
  --max-depth 3 \
  --max-children 4 \
  --max-concurrent 3 \
  --prompt "Investigate why the eval fails and summarize root causes."
```

The run should produce a tree-shaped artifact showing parent and child nodes,
child summaries, tool usage, token usage, output paths, and final synthesis.

## Architecture Decision

Use a Go-owned RLM scheduler and node store under `internal/rlm/runtime`, backed
by the existing REPL runner and v2 `rlm_repl` adapter. `rlm_query` becomes an
async spawn primitive; `rlm_wait` becomes the fan-in primitive. The first
implementation uses one process and one root sandbox/VM per root run, with
logical node isolation through node IDs and output namespaces.

```text
root RLM run
  scheduler
    root node
      parent model turn
        rlm_query -> child root.1
        rlm_query -> child root.2
        rlm_wait  -> [root.1, root.2]
      synthesize
    bounded worker pool
      child nodes run REPL/v2 agents
  node store
    run.json
    tree.json
    nodes/<node_id>/result.json
    nodes/<node_id>/trajectory.jsonl
```

Why this approach:

- It matches the paper-style RLM abstraction: recursive calls are runtime
  primitives, not just another text prompt.
- It keeps recursion controlled by typed budgets instead of model convention.
- It reuses current `internal/rlm/runtime` budget, trajectory, identity, REPL,
  Python, Yaegi, and telemetry code.
- It keeps the v2 agent/runtime lane as the integration target without forcing
  a large rewrite of the classic `foxctl rlm run` command first.
- It keeps smolvm as one top-level isolation boundary rather than recursively
  creating VMs for every child.

Alternatives considered:

- **Blocking `rlm_query` only**: rejected because a parent cannot fan out
  multiple children before waiting.
- **Spawn classic foxctl agents directly from the model**: rejected because it
  bypasses RLM-specific budgets, depth accounting, node identity, and result
  shape.
- **One microVM per child**: deferred. It is stronger isolation, but it is too
  expensive and complicates parent/child artifact sharing. Use one root VM
  first, then harden later.
- **Put all new code under `internal/v2`**: rejected by
  `docs/architecture/package-topology.md`. RLM runtime primitives belong under
  `internal/rlm`; v2 should host runtime adapters and orchestration integration.

## Design Patterns

- **Scheduler / Worker Pool**
  - **Where applied**: `internal/rlm/runtime/scheduler.go`
  - **Why chosen**: bounds concurrent child execution and centralizes
    cancellation, timeout, and fan-in behavior.

- **Repository**
  - **Where applied**: `internal/rlm/runtime/node_store.go`
  - **Why chosen**: persists run/node/result state behind a small interface so
    tests can use memory storage and later implementations can use SQLite.

- **Adapter**
  - **Where applied**:
    `internal/rlm/runtime/agent_backend.go`,
    `internal/v2/runtime/runner/rlm_repl_backend.go`
  - **Why chosen**: lets the scheduler execute nodes through existing REPL/v2
    runners without coupling core scheduling logic to a specific model backend.

- **Strategy**
  - **Where applied**: `internal/rlm/runtime/backend.go`
  - **Why chosen**: supports multiple node execution strategies: REPL runner
    first, v2 agent backend next, and later sandboxed/smolvm-backed execution.

- **Plan / Apply**
  - **Where applied**:
    `internal/rlm/runtime/layout.go`,
    `internal/runtime/sandbox/smolvm/command_plan.go`
  - **Why chosen**: output layouts, mount plans, and node execution should be
    inspectable before running.

- **Immutable Snapshot**
  - **Where applied**: `internal/rlm/runtime/node_store.go`
  - **Why chosen**: parents need stable child result snapshots during
    `rlm_wait` without sharing mutable node internals.

## File Changes

### `internal/rlm/types.go` (modified)

- **Purpose**: extend top-level RLM task/result types with async node metadata.
- **Key changes**:
  - add optional `NodeID`, `ParentNodeID`, `RootRunID`;
  - add `MaxChildren`, `MaxConcurrent`, `MaxTotalNodes`;
  - add structured `ChildResults []ChildResultRef` or metadata summary;
  - preserve backward compatibility for existing `Result.Answer` users.

```go
type ChildResultRef struct {
    NodeID    string `json:"node_id"`
    Status    string `json:"status"`
    Summary   string `json:"summary,omitempty"`
    ResultURI string `json:"result_uri,omitempty"`
}
```

### `internal/rlm/runtime/node.go` (new)

- **Purpose**: define the canonical RLM run/node/result model.
- **Key changes**:
  - add `Run`, `Node`, `NodeStatus`, `NodeResult`, `Finding`,
    `EvidenceRef`, `ArtifactRef`;
  - define status transitions: `queued -> running -> waiting -> completed`;
  - represent failure and cancellation as terminal statuses.

### `internal/rlm/runtime/node_store.go` (new)

- **Purpose**: provide durable and testable storage for RLM node state.
- **Key changes**:
  - define `NodeStore` interface;
  - implement in-memory store for tests;
  - implement JSONL/filesystem store for first production slice;
  - ensure append-only event records and immutable snapshots.

### `internal/rlm/runtime/layout.go` (new)

- **Purpose**: centralize readable output paths for root and child nodes.
- **Key changes**:
  - plan paths under `runs/<run_id>/nodes/<node_id>/`;
  - write `tree.json`, `result.json`, `trajectory.jsonl`, and `artifacts/`;
  - keep parent-visible artifacts out of child `scratch/`.

### `internal/rlm/runtime/budget.go` (modified)

- **Purpose**: extend existing budget primitives from synchronous subcall
  accounting to full tree scheduling.
- **Key changes**:
  - add `MaxChildren`, `MaxConcurrent`, `MaxTotalNodes`;
  - add reservation APIs for child node creation and active worker slots;
  - track per-node and run-total token/tool-call budgets;
  - emit budget events for child rejections, timeouts, and partial results.

### `internal/rlm/runtime/scheduler.go` (new)

- **Purpose**: own async child lifecycle and fan-in.
- **Key changes**:
  - implement bounded worker pool;
  - expose `Submit(ctx, parentID, QueryRequest) (NodeHandle, error)`;
  - expose `Wait(ctx, parentID, WaitRequest) (WaitResult, error)`;
  - support `min_complete`, timeout, cancellation, and partial results;
  - prevent children from waiting on siblings they do not own.

### `internal/rlm/runtime/backend.go` (new)

- **Purpose**: define node execution as a strategy interface.
- **Key changes**:
  - add `NodeBackend` interface;
  - implement `REPLNodeBackend` over current `REPLRunner`;
  - keep backend free of scheduler storage concerns.

```go
type NodeBackend interface {
    RunNode(ctx context.Context, node Node, input NodeInput) (NodeResult, error)
}
```

### `internal/rlm/runtime/repl_runner.go` (modified)

- **Purpose**: replace direct synchronous `rlm_query` execution with scheduler
  tool calls when configured.
- **Key changes**:
  - keep current synchronous mode for compatibility;
  - add `RLMToolModeSync` and `RLMToolModeAsync`;
  - expose both `rlm_query` and `rlm_wait` when async mode is enabled;
  - include child result summaries in parent model context after `rlm_wait`.

### `internal/rlm/runtime/rlm_tools.go` (new)

- **Purpose**: implement tool-level schemas and execution for `rlm_query`,
  `rlm_wait`, and `rlm_result`.
- **Key changes**:
  - `rlm_query` validates task, budget, role, output format, and max depth;
  - `rlm_wait` validates ownership and returns completed child summaries;
  - `rlm_result` optionally fetches a child result by ID for late inspection.

### `internal/rlm/runtime/trajectory.go` (modified)

- **Purpose**: record async lifecycle events as first-class RLM telemetry.
- **Key changes**:
  - add `node_queued`, `node_started`, `node_wait_started`,
    `node_wait_completed`, `node_completed`, `node_failed`;
  - include parent ID, node ID, output namespace, and scheduler wait metadata.

### `internal/rlm/runtime/telemetry.go` (modified)

- **Purpose**: emit the new node lifecycle events into foxctl observability.
- **Key changes**:
  - map new event types to operations such as `rlm.node_queued`;
  - include tree depth, child index, parent node ID, and result artifact path.

### `internal/rlm/runtime/identity.go` (modified)

- **Purpose**: produce stable readable node IDs.
- **Key changes**:
  - keep existing agent-ish identities for compatibility;
  - add canonical node IDs: `root`, `root.1`, `root.2`, `root.2.1`;
  - keep path-safe names for output layout.

### `internal/v2/core/run/types.go` (modified)

- **Purpose**: allow the v2 `rlm_repl` backend to opt into async recursion.
- **Key changes**:
  - add scheduler-related fields to `RLMREPLConfig`;
  - add budget fields for children/concurrency/total nodes;
  - add output layout root and node identity fields.

### `internal/v2/runtime/runner/rlm_repl_backend.go` (modified)

- **Purpose**: wire v2 RLM REPL execution through the scheduler instead of
  direct recursive runner construction.
- **Key changes**:
  - create scheduler for root runs;
  - map `run.RLMREPLConfig` to `runtime.SchedulerConfig`;
  - pass scheduler-backed tool executor into `REPLRunner`;
  - preserve existing sync recursion tests as compatibility mode.

### `internal/agent/tools/rlm_tools.go` (new)

- **Purpose**: expose scheduler-backed RLM tools to agent roles that should be
  able to recursively decompose tasks.
- **Key changes**:
  - register `rlm_query`, `rlm_wait`, and `rlm_result`;
  - delegate to a runtime scheduler injected through `tools.Config`;
  - hide tools unless the role/profile explicitly allows recursive RLM.

### `internal/agent/tools/tools.go` (modified)

- **Purpose**: carry optional RLM scheduler dependency through the existing
  tool registry.
- **Key changes**:
  - add `RLMQueryService` or `RLMRuntime` dependency to `Config`;
  - register RLM tools only when dependency is non-nil;
  - keep current role tool surfaces unchanged by default.

### `internal/agent/daemon/config.go` (modified)

- **Purpose**: configure recursive RLM execution for agent daemon runs.
- **Key changes**:
  - add optional `EnableRLMRuntime`;
  - add RLM budget and output root options;
  - default off for non-RLM agent runs.

### `internal/agent/daemon/daemon.go` (modified)

- **Purpose**: instantiate one scheduler per root daemon run when enabled.
- **Key changes**:
  - build scheduler after stores open;
  - pass scheduler into tool registry config;
  - close scheduler on daemon shutdown;
  - preserve shared SQLite pool behavior.

### `cmd/foxctl/cmd/rlm.go` (modified)

- **Purpose**: expose async recursive RLM from the CLI.
- **Key changes**:
  - add `--recursive-mode sync|async`;
  - add `--max-children`, `--max-concurrent`, `--max-total-nodes`;
  - add `--output-root`;
  - emit run tree and artifact paths in the envelope.

### `cmd/foxctl/cmd/sandbox_smolvm.go` (modified)

- **Purpose**: pass RLM tree budgets/output paths through smolvm agent runs.
- **Key changes**:
  - add flags mirroring CLI budgets when running RLM agents;
  - mount one output root and let the guest create node subdirectories;
  - avoid nested smolvm creation for child nodes.

### `cmd/foxctl/cmd/eval_longcot.go` (modified)

- **Purpose**: use async recursion for the recursive LongCoT condition.
- **Key changes**:
  - keep `rlm_repl_no_subcalls` unchanged;
  - map `rlm_repl_recursive` to async scheduler mode;
  - record child count, wait count, max depth reached, and child summaries in
    `RLMAttemptMeta`.

### `internal/tooling/evals/longcoteval/types.go` (modified)

- **Purpose**: report tree-shaped RLM metadata.
- **Key changes**:
  - add `NodesTotal`, `MaxDepthReached`, `WaitCalls`, `ChildSummaries`;
  - keep existing `Subcalls` for summary compatibility.

### `docs/plans/features/foxctl-paper-rlm-runtime-plan.md` (modified)

- **Purpose**: link this plan as the next implementation phase after
  synchronous REPL/subcall support.
- **Key changes**:
  - add a short note that async fan-out/fan-in is tracked here.

### `docs/plans/features/smolvm-foxctl-agent-runtime-plan.md` (modified)

- **Purpose**: clarify that recursive RLM children share the root sandbox.
- **Key changes**:
  - link to this plan;
  - document node output layout under `/mnt/out/runs/<run_id>/nodes/`.

## Testing Strategy

### Unit Tests

- `internal/rlm/runtime/node_store_test.go`
  - `TestMemoryNodeStoreTransitions`
  - `TestMemoryNodeStoreRejectsInvalidTransition`
  - `TestMemoryNodeStoreSnapshotsAreImmutable`

- `internal/rlm/runtime/layout_test.go`
  - `TestPlanNodeLayoutRootAndChildren`
  - `TestPlanNodeLayoutSanitizesIDs`
  - `TestPlanNodeLayoutRejectsEmptyRunID`

- `internal/rlm/runtime/budget_test.go`
  - extend with `TestBudgetRejectsMaxChildren`
  - extend with `TestBudgetRejectsMaxConcurrent`
  - extend with `TestBudgetRejectsMaxTotalNodes`
  - extend with `TestBudgetReleasesConcurrentSlotOnFailure`

- `internal/rlm/runtime/scheduler_test.go`
  - `TestSchedulerSubmitReturnsBeforeChildCompletes`
  - `TestSchedulerWaitCollectsMultipleChildren`
  - `TestSchedulerWaitMinComplete`
  - `TestSchedulerWaitTimeoutReturnsPending`
  - `TestSchedulerCancelsChildrenOnRootCancel`
  - `TestSchedulerRejectsDepthExceeded`

- `internal/rlm/runtime/rlm_tools_test.go`
  - `TestRLMQueryReturnsHandle`
  - `TestRLMWaitReturnsStructuredResults`
  - `TestRLMResultFetchesCompletedChild`
  - `TestRLMWaitRejectsForeignChild`

- `internal/v2/runtime/runner/rlm_repl_backend_test.go`
  - update existing recursive wiring tests for async mode;
  - add `TestDefaultRLMREPLRunnerFactoryUsesSchedulerInAsyncMode`.

- `internal/agent/tools/rlm_tools_test.go`
  - `TestRLMToolsHiddenWithoutScheduler`
  - `TestRLMToolsExposeQueryWaitResultWithScheduler`

Mocking strategy:

- use fake `NodeBackend` for scheduler unit tests;
- use memory `NodeStore` for fast tests;
- use fake `RLMRunner` for v2 adapter tests;
- avoid live LLMs in unit tests.

### Integration Tests

- `cmd/foxctl/cmd/rlm_test.go`
  - add command-level tests for new flags and envelope fields.

- `cmd/foxctl/cmd/eval_longcot_test.go`
  - assert `rlm_repl_recursive` carries async scheduler metadata.

- Live smoke, not CI by default:

```bash
foxctl sandbox smolvm run-agent \
  --llm-provider lmstudio \
  --llm-model liquid/lfm2.5-1.2b \
  --max-depth 2 \
  --max-children 3 \
  --max-concurrent 2 \
  --ask-question 'Split this into two independent repo investigations...'
```

Pass criteria:

- root creates at least two children before waiting;
- children write distinct node result files;
- parent final answer cites child summaries;
- no child exceeds budget;
- smolvm output root contains `tree.json` and per-node results.

### Edge Cases

- Parent calls `rlm_wait` before any children exist.
  - Return an empty completed/pending set, not an error.
- Parent calls `rlm_query` after depth is exhausted.
  - Return a budget error and emit a budget event.
- Parent creates children until `MaxChildren` is reached.
  - Reject the next query deterministically.
- Child fails with tool/model error.
  - Persist failed result and allow parent to synthesize from partials.
- `rlm_wait` times out.
  - Return completed and pending handles; do not cancel pending unless caller
    requests cancellation.
- Root context is canceled.
  - Cancel all queued/running children and mark them canceled.
- Child returns output too large for inline result.
  - Persist artifact and return summary plus artifact ref.

## Error Handling

Use normal Go errors at package boundaries and structured error payloads inside
tool results.

Expected scenarios:

- invalid `rlm_query` input: return tool error with validation message;
- depth exhausted: return budget error and emit `rlm.budget`;
- child budget exhausted: child returns partial `NodeResult` with
  `Status=failed` or `Status=completed_partial`;
- backend/model failure: persist failed node result with error summary;
- store write failure: fail the affected scheduler operation and mark the node
  failed if possible;
- parent waits on unauthorized child: return permission-style tool error;
- root shutdown: cancel children and close worker pool cleanly.

Recovery strategies:

- parents can call `rlm_wait` again for pending children;
- parents can call `rlm_result` for a completed child handle;
- failed children remain visible as failed evidence, not hidden runtime noise;
- root final synthesis can proceed with partial completed children when
  `min_complete` is satisfied.

## Migration Notes

No database migration is required in the first slice because the initial store
is filesystem/JSONL-backed. A SQLite-backed `NodeStore` can be added later
under `internal/storage` if we need cross-process resume.

Backward compatibility:

- existing synchronous `rlm_query` behavior stays available as
  `--recursive-mode sync`;
- existing `rlm_repl_no_subcalls` and LongCoT no-subcall conditions are
  unchanged;
- existing `Result.Subcalls` remains populated as a summary count.

Rollback plan:

- gate async recursion behind `--recursive-mode async`;
- default existing commands to current behavior until live smokes are stable;
- leave LongCoT recursive condition opt-in.

## Dependencies

No new dependency is required for the first implementation.

Existing dependencies affected:

- Go standard library `context`, `sync`, `time`, `encoding/json`, and file IO;
- existing foxctl observability and RLM packages;
- existing smolvm packaging path for live sandbox smokes.

License impact: none.

## Implementation Order

1. Define node/result types in `internal/rlm/runtime/node.go`.
2. Add memory and filesystem node stores in `internal/rlm/runtime/node_store.go`.
3. Add output layout planning in `internal/rlm/runtime/layout.go`.
4. Extend budgets for child/concurrency/total-node accounting.
5. Implement scheduler with fake backend tests.
6. Implement `rlm_query`, `rlm_wait`, and `rlm_result` tool executor.
7. Wire async tools into `REPLRunner` behind config while keeping sync mode.
8. Add v2 `RLMREPLConfig` fields and map them into scheduler config.
9. Wire `cmd/foxctl/cmd/rlm.go` flags and envelope output.
10. Add optional agent tool registry wiring for RLM tools.
11. Update smolvm command planning to pass recursive budgets/output roots.
12. Update LongCoT recursive condition metadata and reporting.
13. Run focused unit tests.
14. Run local LMStudio Liquid smoke outside CI.
15. Update the RLM and smolvm plans with cross-links.

Each of steps 1-8 is unit-testable without live LLMs. Steps 9-12 add command
and eval integration tests. Step 14 is a manual smoke gate.

## Open Questions

- Should `rlm_query` default to async for all recursive modes, or should CLI
  default remain sync until more smoke evidence exists?
  - Default: keep sync as default, async opt-in.
- Should child nodes run through `REPLRunner` only, or through the v2 agent
  framework immediately?
  - Default: scheduler backend over `REPLRunner` first, v2 agent backend second.
- Should `rlm_wait` cancel pending children on timeout?
  - Default: no; timeout returns pending handles.
- Should the node store become SQLite-backed before LongCoT eval integration?
  - Default: no; filesystem/JSONL is easier to debug and enough for one-process
    smolvm runs.
- Should children share the same memory/context stores as the parent?
  - Default: yes inside the same root sandbox, but with node-specific output
    namespaces and no shared scratch directories.
- How much child reasoning should be persisted?
  - Default: persist tool calls, summaries, evidence, artifacts, and model
    metadata. Do not expose hidden chain-of-thought in user-facing reports.

