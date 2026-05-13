# Foxctl Paper-Style RLM Runtime Plan

## Status

Implementation in progress. This supersedes treating `eval longcot` as the
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

Async fan-out/fan-in recursion is tracked in
[rlm-recursive-fanout-runtime-plan.md](rlm-recursive-fanout-runtime-plan.md).
That phase has since landed in core runtime form; remaining work is mostly CLI
ergonomics, reporting, and LongCoT/helper-pipeline hardening.

## Direction Update: Simplify The Harness

Recent LongCoT lambda probes showed that the strict parent-verifier harness can
become a burden instead of an additive runtime. Gemini can solve some easy tasks
without the full ceremony, while the forced sequence of context dump, three
children, verifier-code generation, schema artifacts, repair children, and final
handoff adds token cost and new failure modes.

The next experimental path is `rlm_lambda_adaptive_single`: keep the useful RLM
properties, but remove mandatory ceremony. The runtime should first allow a
direct REPL-backed solve, register the resulting `solution =` candidate, and
only add recursion or verifier code after concrete failure. Recursion becomes a
repair/escalation mechanism, not the default shape. If this simpler condition
beats the strict lambda/BRAID variants, make it the default LongCoT RLM harness
and keep stricter graph/certification paths for tasks that demonstrably need
them.

The current simplification moves the LongCoT Lambda-RLM handoff to a single
structured sentinel first: `RLM_ANSWER_JSON={"answer":"solution = ...","pass":true,"checks":[...]}`.
Deterministic failures can still be surfaced as
`RLM_CHECK_JSON={"pass":false,"reason":"..."}` and should block final
forwarding. BRAID-style graph certification remains a later certification layer,
not a prerequisite for the adaptive experiment.

## Current State

`internal/rlm` now has both the original bounded tool-orchestration layer and a
paper-style runtime lane.

- `internal/rlm/llm_runner.go` still wraps `engine.LLMChatEngine` and supports a
  single pass or staged retrieval-oriented phases.
- `internal/rlm/runtime/repl_runner.go` provides the REPL-backed runner with
  structured `RLM_ANSWER_JSON` / `RLM_CHECK_JSON` answer handoff.
- `internal/rlm/runtime/scheduler.go`, `node_store.go`, `budget.go`, and
  `rlm_tools.go` provide async `rlm_query`, `rlm_wait`, and `rlm_result` with
  depth, child, concurrency, and total-node budgets.
- `cmd/foxctl/cmd/eval_longcot.go` includes scaffolded LongCoT RLM conditions
  such as REPL, recursive, lambda-adaptive, and BRAID variants.
- `ephemeral_helper_solve` exists as a parent-facing helper shortcut, but its
  pipeline trace and preset decomposition are still being hardened in
  [rlm-helper-pipeline-repair-plan.md](rlm-helper-pipeline-repair-plan.md).

The main caution is evaluation language: helper-assisted or recursive LongCoT
conditions are internal scaffolded comparisons, not official leaderboard
conditions.

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

1. Keep `rlm run` CLI budget/reporting aligned with the async runtime fields.
2. Harden `ephemeral_helper_solve` trace output so parent prompts receive only
   compact pipeline summaries, source hashes, and digestible metadata.
3. Extract reusable helper-pipeline types under
   `internal/rlm/runtime/helperpipeline`.
4. Split the BlocksWorld helper preset into explicit parse/solve/verify/format
   steps before adding durable repair/cache behavior.
