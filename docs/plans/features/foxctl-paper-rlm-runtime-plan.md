# Foxctl Paper-Style RLM Runtime Plan

## Status

Draft implementation plan. This supersedes treating `eval longcot` as the
primary workstream. LongCoT remains the benchmark consumer, but the immediate
priority is a real Recursive Language Model runtime for `foxctl`.

## Goal

Implement a Go-native RLM runtime that matches the core abstraction from
Recursive Language Models:

- the prompt is externalized as environment state, not treated only as one chat
  message;
- a persistent REPL exposes the prompt as a variable;
- the model can write code to inspect, slice, transform, and verify work;
- the model can issue bounded recursive self-calls over subproblems;
- recursion bottoms out at a normal LM call;
- all execution is bounded, observable, and replayable through trajectory
  artifacts.

The first benchmark target is official LongCoT, but the runtime must be generic
enough for other foxctl tasks.

Async fan-out/fan-in recursion is tracked as the next implementation phase in
[rlm-recursive-fanout-runtime-plan.md](rlm-recursive-fanout-runtime-plan.md).

## Current State

`internal/rlm` is currently a bounded tool-orchestration layer, not a paper
RLM.

- `internal/rlm/llm_runner.go` wraps `engine.LLMChatEngine` and supports a
  single pass or staged retrieval-oriented phases.
- `internal/rlm/env/adapter.go` has a `subcall` tool callback, but subcalls are
  optional tool invocations, not a first-class recursive runtime.
- `internal/rlm/interfaces.go` defines `Sandbox`, but no active RLM runner uses
  a REPL/code sandbox.
- LongCoT conditions currently force an empty tool environment and set
  `MaxSubcalls` to zero.

This means current LongCoT `rlm_*` conditions are not meaningful RLM
comparisons.

## Target Architecture

```text
root RLM runner
  budget manager
  trajectory recorder
  persistent REPL session
    prompt = official/user prompt
    rlm_query(...)
    rlm_map(...)
  parent LM loop
    calls python_repl(code)
    calls rlm_query(subprompt)
    observes results
    returns final answer
```

## Package Placement

Per `docs/architecture/package-topology.md`, this belongs under the existing
`internal/rlm` family. New top-level `internal/*` roots are unnecessary.

Initial package split:

```text
internal/rlm/runtime/
  budget.go
  trajectory.go
  runner.go

internal/rlm/repl/
  python.go
```

`internal/rlm/runtime` owns core RLM execution concepts. `internal/rlm/repl`
owns reusable REPL implementations.

## Milestones

### M1: Runtime Foundation

Deliver:

- central budget state:
  - max depth
  - max subcalls
  - max REPL calls
  - max iterations
  - parent/child token limits
  - wall-clock deadline
- trajectory event model:
  - parent LM call
  - REPL call/result
  - subcall start/end
  - budget event
  - final answer
  - error
- in-memory recorder and JSONL-ready event shape

Definition of done:

- unit tests cover exhaustion, depth checks, token accounting, event ordering,
  and JSON marshal stability.

### M2: Persistent REPL

Deliver:

- persistent Python subprocess session;
- initial state binding, especially `prompt`;
- snippet execution with stdout/stderr/result capture;
- per-call timeout and output cap;
- temp working directory;
- clean close behavior.

Definition of done:

- tests cover variable persistence, prompt binding, stdout, exception capture,
  timeout, and close.

Security note: MVP local Python is acceptable for development but is not the
final sandbox. The final production runner should move toward WASI/Pyodide or a
stricter isolated subprocess policy.

### M3: Recursive Self-Calls

Deliver:

- generic `rlm_query(prompt, options)` callback;
- depth decrementing and total subcall budget enforcement;
- optional separate child model;
- child token budget;
- child trajectory nesting or parent-linked events.

Definition of done:

- tests demonstrate depth-0 bottom-out and depth-1 child calls;
- budget cannot be bypassed by tool arguments.

### M4: REPL-Backed RLM Runner

Deliver:

- model loop exposing:
  - `python_repl`
  - `rlm_query`
  - optionally `rlm_map`
- RLM system prompt based on the paper:
  - prompt is bound as `prompt`;
  - use REPL to inspect/decompose/verify;
  - use recursive subcalls for bounded subproblems;
  - return final answer in requested format.
- support `--no-think` style prompt prefixing for local Qwen reasoning models.

Definition of done:

- a synthetic task proves the model can use the REPL to compute/check an answer;
- trajectory proves REPL/subcall usage.

### M5: LongCoT Integration

Add conditions:

```text
rlm_repl_no_subcalls
rlm_repl_recursive
```

Preserve:

- official LongCoT prompt loading;
- official response JSONL;
- post-run `run_eval.py` verification;
- no access to official answers/verifier/dataset files during solve.

For LongCoT, scaffolded RLM conditions are not leaderboard-equivalent to the
official no-tools baseline. Reports must label them as scaffolded conditions.

Definition of done:

- `limit 1` BlocksWorld run shows:
  - REPL parses initial/goal state;
  - candidate moves are simulated before final output;
  - invalid move detection appears in trajectory;
  - final response is verified by official LongCoT verifier.

## Open Design Decisions

- Python subprocess MVP versus Pyodide/WASI-first implementation.
- Whether `rlm_query` is exposed as a model tool, a REPL function, or both.
- Whether child calls use the same model by default or a cheaper child model.
- How much of the parent hidden/reasoning trace to retain in trajectory without
  leaking chain-of-thought into user-facing reports.
- Whether domain-specific helper modules are allowed for benchmark conditions.

## Immediate Next Slice

1. Build `internal/rlm/runtime` budget and trajectory primitives.
2. Build `internal/rlm/repl` persistent Python session.
3. Add an experimental runner that exposes REPL only, no subcalls.
4. Run a synthetic BlocksWorld-like local task before reconnecting official
   LongCoT.
