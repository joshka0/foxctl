# RLM General Solver Proposal

**Status:** Slices 1-8 initial implementation landed  
**Created:** 2026-04-26  
**Updated:** 2026-04-27  
**Worktree:** `/Users/joshka/repos/worktrees/feat-rlm-general-solver`  
**Related:** [LongCoT eval contract plan](longcot-eval-contract-plan.md), [RLM helper pipeline repair plan](rlm-helper-pipeline-repair-plan.md), [RLM recursive fanout runtime plan](rlm-recursive-fanout-runtime-plan.md), [LongCoT autoresearch loop](../../research/longcot-autoresearch-loop.md)

## Summary

Build a domain-general RLM solver runtime for long-horizon reasoning tasks.

The next architecture should not be a set of domain-specific solvers such as
`chemistry_solver`, `chess_solver`, or `hm_type_solver`. It should be a
root-owned solver controller that keeps one explicit state graph, memo table,
failure log, verifier budget, and ready queue while delegating bounded
work-item computation to code-first helpers, child RLM calls, and verifier
loops.

The core design rule:

```text
The model may propose.
The runtime owns state.
The verifier decides what can be committed.
```

This follows from our LongCoT experiments:

- DeepSeek v4-flash solved LongCoT logic/easy reliably once verified final
  handoff was forwarded directly.
- Liquid 1.2B could call tools but could not reliably emit the controller graph.
- CS easy Hindley-Milner tracing failed at the helper-first solve node, not at
  final answer copying.
- Mixed-domain easy smoke tests entered repeated length-capped loops, showing
  that the logic-oriented braid scaffold is not yet compact or general enough
  for broader LongCoT categories.

The proposed solver keeps the successful parts of the current braid/RLM work,
but moves from a flat `parse -> compute -> verify -> format` shape toward a
persistent graph solver:

```text
model problem
seed work graph
pick ready work item
propose candidate plans
execute code-first unit solver
verify locally
commit, repair, branch, or backtrack
compact history
finalize from committed artifacts
```

## Goals

- Create one root-owned state graph for long-horizon reasoning runs.
- Keep dependency IDs, memoized artifacts, failed attempts, verifier
  diagnostics, and frontier state outside model context.
- Support domain-general computational archetypes rather than explicit domain
  routing.
- Make work-item solvers produce structured artifacts, not only prose answers.
- Attach cheap and expensive verification loops to each work item.
- Preserve enough telemetry for later workflow optimization and autoresearch.
- Keep scaffolded LongCoT results clearly marked as non-leaderboard-comparable.

## Non-Goals

- Do not build per-domain benchmark shortcuts.
- Do not use ad hoc keyword heuristics to route behavior.
- Do not let recursive subagents own the global dependency graph.
- Do not expose child IDs, graph mutation internals, or retry state to models.
- Do not require every task to use a formal verifier.
- Do not run broad mixed-domain LongCoT evals until one-question probes pass.
- Do not treat verified-handoff runs as raw model reasoning benchmarks.

## Research Takeaways

The useful synthesis from PAL, PoT, least-to-most, Tree/Graph of Thoughts,
RDD-style decomposition, ProgCo, SDV, and agent-tool orchestration work is:

- Exact computation should move into executable artifacts whenever possible.
- Intermediate variables and artifacts need names, schemas, and provenance.
- Explicit dependency structure should be scheduled by a runtime, not carried
  only in subagent prose.
- Search-heavy tasks need simulator-backed branching and backtracking.
- Verification should be local, iterative, and adaptive to risk.
- Memory compaction should preserve solved artifacts and failed paths, not just
  truncate old text.

For LongCoT specifically, the important lesson is that output length is not the
only hard part. Dependency propagation and state retention are central. A model
can solve local pieces and still fail when dependencies, failed explorations, or
variable bindings are dropped across handoffs.

## Current Evidence

### Logic/Easy Success

DeepSeek v4-flash with the current `rlm_braid_single` scaffold and verified
final handoff:

```text
dataset: LongCoT logic/easy 1-50
model: deepseek/deepseek-v4-flash
condition: rlm_braid_single
flag: --rlm-final-from-verified-handoff
result: 50/50
wrong formatting: 0
leakage: 0
mean duration: ~73s/question
mean tokens: ~11.7k/question
```

Key artifact:

```text
.foxctl/longcot-autoresearch/deepseek-v4-flash-1-50-handoff/longcot/longcot-live-3c394d7d7c590f18/result.json
```

Interpretation: DeepSeek often reached a verified answer, but previously failed
while restating long final answers. Runtime-verified final handoff fixed that
serialization bottleneck.

### Local Liquid Failure

Liquid via LM Studio:

```text
model: liquid/lfm2.5-1.2b
balanced 12-question probe
with handoff: 0/12
without handoff: 0/12
```

Failure mode: Liquid can call tools, but cannot reliably satisfy the controller
contract. It emits `solution = ...` where the graph-planning phase requires a
braid graph JSON object. Verified handoff cannot help because no verified
handoff is produced.

### CS Easy Failure

DeepSeek v4-flash on `HM_easy_1`:

```text
dataset: /tmp/foxctl-longcot-cs-easy-1.jsonl
question: HM_easy_1
result: 0/1
duration: ~459s
tokens: 24,483
failure: helper-first solve node failed twice
```

Artifact:

```text
.foxctl/longcot-autoresearch/deepseek-v4-flash-cs-easy-1/longcot/longcot-live-81887a12b1cedefe/result.json
```

Trace shape:

```text
graph plan accepted
extract node completed
solve node helper-first attempt timed out/fail
repair attempted
solve node helper-first failed again
graph_fanout rejected
```

Interpretation: this is no longer a final-answer-copying problem. The next
failure frontier is work-item solving and verifier-guided repair for new
computational archetypes.

## One-Question Probe Results (2026-04-26)

All five domains probed with `deepseek/deepseek-v4-flash`, `rlm_braid_single`,
`--general-helper --helper-language python --rlm-final-from-verified-handoff`.

### Summary Table

| Domain | Question | Result | Duration | Tokens | Failure Phase |
|--------|----------|--------|----------|--------|---------------|
| logic/easy | BlocksWorld_easy_1 | ERROR | 27s | 9,580 | graph parse |
| cs/easy | HM_easy_1 | ERROR | 720s | 24,788 | helper-first solve (x2) |
| chemistry/easy | easy1_0 | ERROR | 720s | 10,810 | helper-first solve (x2) |
| chess/easy | uci_to_fen_easy_1 | ERROR | 720s | 7,555 | helper-first solve (x2) |
| math/easy | backtracking_easy_1 | ERROR | 189s | 23,770 | max-token stop (no graph) |

### Logic/Easy: BlocksWorld_easy_1

```text
status: error
phase: graph_plan_parse
duration: 27s
tokens: 9,580
```

**Issue:** Model emitted `python_repl` as an unknown JSON field in the braid
graph, then the repair attempt produced an `expected_output` of 474 chars
(exceeds the 240-char max). The graph never entered the fanout execution phase.

**Root cause:** The model generated a non-conforming braid graph twice. The
repair loop did not converge because the model kept including extra fields and
oversized expected_output.

**Possible causes:**

1. Model variance: DeepSeek v4-flash sometimes adds `python_repl` to the graph
   schema (was seen in prior runs that passed, but this instance also had the
   oversized field).
2. Prompt length: The BlocksWorld prompt with 60 blocks produces a very long
   root task, which may push the model toward more verbose `expected_output`
   fields.
3. Repair guidance: The repair loop retried with the same graph plan but did
   not strip the unknown field or truncate expected_output before resubmitting.

**Fix targets:**

- Normalize unknown fields out of the graph JSON before validation (rather than
  rejecting outright).
- Truncate `expected_output` to max 240 chars during normalization.
- Consider a graph-plan-only retry that explicitly asks the model to produce
  only `id`, `kind`, `question`, `depends_on`, `max_summary_chars`,
  `helper_policy`.

### CS/Easy: HM_easy_1

```text
status: error
phase: helper-first solve on n_solve
duration: 720s (timeout)
tokens: 24,788
helper_attempts: 5 draft, all timed out
trajectory: accepted -> n_extract completed -> n_solve helper_first -> helper_first_failed (616s) -> repair -> helper_first_failed (1ms) -> rejected
```

**Issue:** Extract node completed successfully with 364 let-bindings. The solve
node ran helper-first with preferred policy. All 5 helper draft attempts timed
out (context deadline exceeded).

**Root cause:** The helper factory could not produce a working solver for
Hindley-Milner type inference within the per-attempt timeout. The problem is
computationally complex (Algorithm W over 364 let-bindings with 5 queries) and
the helper drafts could not converge on a correct implementation.

**Possible causes:**

1. Helper task is too large for the draft model (also deepseek-v4-flash). A 364-
   binding HM inference problem requires non-trivial unification code that is
   hard to generate in a single-shot draft.
2. No typed handoff scaffold matched: the HM problem does not trigger any of the
   existing scaffold detectors (transition system, graph search, numeric DP,
   constraint solver, sequence simulation). The helper received a generic
   prompt without structured input.
3. The `symbolic_trace` archetype from the proposal is not yet implemented.
   There is no scaffold that decomposes the problem into state variables,
   ordered rules, named bindings, and executable trace checks.

**Fix targets:**

- Implement the `symbolic_trace` archetype (Slice 4 from the proposal).
- Provide a typed handoff that gives the helper structured input: variable
  bindings, type schemes, unification constraints, and query targets.
- Consider splitting the solve node into sub-items (one per query) rather than
  solving all 5 queries in a single helper.

### Chemistry/Easy: easy1_0

```text
status: error
phase: helper-first solve on n_solve_selections
duration: 720s (timeout)
tokens: 10,810
helper_attempts: 1 validate (syntax error) + 4 draft (all timed out)
trajectory: accepted -> n_extract completed -> n_solve_selections helper_first -> failed (383s) -> repair -> failed (202s) -> rejected
```

**Issue:** Extract node correctly identified 5 subproblems (pick equivalent
SMILES, pick 3rd most heteroatoms, pick tertiary amine, pick 2nd most implicit
H, reaction at 100C). First helper attempt produced code with a syntax error
(unterminated string literal). Remaining 4 attempts all timed out.

**Root cause:** The helper cannot reliably generate working chemistry SMILES
parsing and property-counting code. The first draft had a basic syntax error.
After repair feedback, the subsequent drafts could not converge within timeout.

**Possible causes:**

1. No chemistry-specific scaffold exists. The helper receives generic
   instructions without a structured SMILES parsing library or property
   enumeration framework.
2. The `candidate_verify` archetype (Slice 5) is not implemented. Chemistry
   questions are fundamentally about generating candidates (SMILES strings) and
   testing properties, which maps well to candidate-verify.
3. The helper model (deepseek-v4-flash) may not have enough chemistry domain
   knowledge to produce correct RDKit-style SMILES manipulation code.
4. String literal handling in generated Python is fragile for SMILES with
   special characters (brackets, at-signs, charges).

**Fix targets:**

- Implement `candidate_verify` archetype (Slice 5) so the runtime can enumerate
  candidates and test properties independently.
- Add a chemistry-typed handoff that provides structured candidate lists and
  property checks as input to the helper.
- Consider pre-seeding the helper with a minimal SMILES parsing utility.

### Chess/Easy: uci_to_fen_easy_1

```text
status: error
phase: helper-first solve on n_solve_sim
duration: 720s (timeout)
tokens: 7,555
helper_attempts: 5 draft, all timed out
trajectory: accepted -> n_extract completed -> n_solve_sim helper_first -> failed (539s) -> repair -> failed (0ms) -> rejected
```

**Issue:** Extract node correctly identified 558 UCI moves. The solve node was
labeled `n_solve_sim` (the model recognized this needs simulation). All 5
helper draft attempts timed out. The second attempt failed instantly (0ms),
suggesting the helper context from repair exceeded limits or produced an
immediate error.

**Root cause:** Simulating 558 chess moves to compute a final FEN board state
requires either `python-chess` library access or a complete move application
engine. The helper drafts could not produce working code within timeout.

**Possible causes:**

1. No `state_transition` scaffold matched despite the model naming the node
   `n_solve_sim`. The existing transition system scaffold only matches
   `initial_state`/`goal_state` patterns, not UCI move sequences.
2. The 558-move sequence is too long for reliable inline code generation. A
   correct solution needs `python-chess` or a compact move application loop.
3. The helper has no access to external libraries. If `python-chess` is not
   available in the sandbox, the helper must implement move logic from scratch,
   which is error-prone for 558 moves.
4. The 0ms second failure suggests the repair feedback may have corrupted the
   helper input or the context was exhausted.

**Fix targets:**

- Ensure `python-chess` is available in the sandbox (or add it as a preset
  dependency for chess archetype detection).
- Add a `state_transition` scaffold for UCI move sequences that gives the helper
  structured input: move list, starting position, and target (final FEN).
- Consider detecting `uci` / `fen` / chess-related keywords in the extract
  summary to trigger a chess-specific typed handoff.

### Math/Easy: backtracking_easy_1

```text
status: error
phase: model max-token stop (no graph produced)
duration: 189s
tokens: 23,770
helper_attempts: n/a (helper_scaffolded=True but not invoked)
trajectory: repl_call -> repl_result -> max_token stop
```

**Issue:** The model used all 8,192 output tokens in a single completion and
hit the length limit before producing any structured output (no braid graph, no
tool calls, no solution line).

**Root cause:** The math problem (rectangular paper folding with backtracking)
requires extended chain-of-thought reasoning. DeepSeek v4-flash attempted to
solve it entirely in prose within one completion and ran out of tokens.

**Possible causes:**

1. The model did not produce a braid graph plan. Without a graph, the runtime
   never entered the fanout execution phase and the helper was never invoked.
2. The `max_tokens=8192` limit is too low for this problem's reasoning length.
   Math backtracking problems often require 15-25k tokens of intermediate work.
3. The REPL phase contract may not be clear enough that the model should
   decompose into a graph rather than solving inline.

**Fix targets:**

- Increase `max_tokens` for math-domain questions (at least 16k or 24k).
- Improve the graph-planning prompt to more strongly encourage graph
  decomposition for problems that require backtracking.
- Add a retry with higher token budget when the model hits max-token stop
  without producing any structured output.

### Cross-Cutting Patterns

1. **Helper draft timeout is the dominant failure mode** (3 of 5 probes). The
   helper factory using the same model (deepseek-v4-flash) cannot reliably
   draft correct code for unfamiliar computational archetypes within the
   per-attempt timeout.

2. **No typed handoff scaffold matched for any failing probe.** The existing
   scaffolds (transition system, graph search, numeric DP, constraint solver,
   sequence simulation) cover the domains that were already passing (logic
   BlocksWorld). New domains need new archetype scaffolds.

3. **The 0ms second failure pattern.** CS, chemistry, and chess all show a
   second helper attempt that fails instantly (0-1ms). This suggests the repair
   feedback may exceed the helper's input budget or the factory cancels
   immediately when prior attempts exhausted the overall timeout.

4. **Graph parse failures are recoverable.** The logic probe failed at graph
   parse, not at solving. Normalizing unknown fields and truncating oversized
   fields would make the parser more resilient to model variance.

## Proposed Runtime Model

### Core Objects

```go
type ProblemArchetype string

const (
    ArchetypeExplicitDAG        ProblemArchetype = "explicit_dag"
    ArchetypeStateTransition    ProblemArchetype = "state_transition"
    ArchetypeSymbolicTrace      ProblemArchetype = "symbolic_trace"
    ArchetypeGraphSearch        ProblemArchetype = "graph_search"
    ArchetypeTableRecurrence    ProblemArchetype = "table_recurrence"
    ArchetypeConstraintSolve    ProblemArchetype = "constraint_solve"
    ArchetypeCandidateVerify    ProblemArchetype = "candidate_verify"
    ArchetypeMixed              ProblemArchetype = "mixed"
)

type WorkItemStatus string

const (
    WorkItemPending   WorkItemStatus = "pending"
    WorkItemReady     WorkItemStatus = "ready"
    WorkItemSolving   WorkItemStatus = "solving"
    WorkItemSolved    WorkItemStatus = "solved"
    WorkItemBlocked   WorkItemStatus = "blocked"
    WorkItemFailed    WorkItemStatus = "failed"
)

type WorkItem struct {
    ID              string
    Goal            string
    Archetype       ProblemArchetype
    DependsOn       []string
    Status          WorkItemStatus
    Attempts        int
    Priority        float64
    Risk            float64
    MaxSummaryChars int
    Payload         map[string]any
}

type WorkArtifact struct {
    WorkItemID     string
    Status         string
    Answer         any
    Code           string
    Derived        map[string]any
    Evidence       map[string]any
    Checks         []string
    Counterexamples []map[string]any
    Confidence     float64
}

type WorkVerdict struct {
    Accept     bool
    Repairable bool
    Confidence float64
    Feedback   map[string]any
}

type SolverState struct {
    Items        map[string]WorkItem
    ReverseDeps  map[string][]string
    Artifacts    map[string]WorkArtifact
    FailureLog   []map[string]any
    ReadyQueue   []string
    Digests      []string
}
```

These types should start in `internal/rlm/runtime` or a subpackage under that
family. They belong to the RLM runtime family because they orchestrate model,
tool, helper, and verifier execution.

### Controller Loop

```text
1. Model the problem into a ProblemModel.
2. Seed a root-owned WorkGraph.
3. Select the next ready WorkItem by priority/risk/dependencies.
4. Ask the model for candidate approaches for that work item only.
5. Generate or select executable helper code.
6. Run helper code in the sandbox.
7. Verify the artifact with a tiered verifier stack.
8. Commit accepted artifacts to root state.
9. Repair, branch, or backtrack failed artifacts.
10. Compact history into digests.
11. Finalize only from committed artifacts.
```

The model should not manage child IDs, output paths, retry counters, or graph
state. The model receives a bounded work-item packet and returns a candidate
artifact.

## Archetypes

Archetypes are computational shapes, not domains.

| Archetype | Intended fit | Examples |
|---|---|---|
| `explicit_dag` | visible dependency graph | math backtracking nodes, chained subproblems |
| `state_transition` | move/action simulation | BlocksWorld, Hanoi, Sokoban-like tasks |
| `symbolic_trace` | evolving symbolic environment | type inference, algebraic derivations, program traces |
| `graph_search` | shortest path / reachability / resource path | dungeon, routing, dependency traversal |
| `table_recurrence` | DP or recurrence table | scheduling, counting, recurrence problems |
| `constraint_solve` | finite-domain assignment | Sudoku-like, CSP, some chemistry selection |
| `candidate_verify` | generate candidates and test them | chess moves, SMILES choices, multiple-choice chemistry |
| `mixed` | more than one inner loop | tasks requiring DAG plus simulation |

This lets us avoid domain-specific branching while still giving the runtime
enough structure to pick the right execution mode.

## Work-Item Solver Contract

Each work item should return a structured artifact:

```json
{
  "status": "solved|partial|blocked|failed",
  "answer": "...",
  "named_state": {},
  "derived_values": {},
  "code": "...",
  "checks": ["..."],
  "counterexamples": [],
  "next_actions": [],
  "confidence": 0.0
}
```

The runtime can then:

- commit solved artifacts;
- repair partial artifacts;
- branch on ambiguous candidates;
- preserve failed paths;
- synthesize final answers without asking the model to copy long state.

## Verifier Stack

Verification should happen per work item before commit.

Tier 1: cheap universal checks

- schema validation;
- type validation;
- deterministic rerun;
- output format check;
- dependency substitution check;
- invariant check.

Tier 2: executable local verifier

- ask the model to synthesize a checker for the artifact;
- run it separately from the solver code;
- compare independent implementations where feasible.

Tier 3: escalation

- use a stronger model for high-risk or high-fanout nodes;
- increase helper budget;
- ask for alternate implementation;
- use a formal verifier when the item can be translated.

Verifier budget should depend on:

- work item risk;
- fanout / graph centrality;
- previous failed attempts;
- verifier confidence;
- final answer sensitivity.

## Memory and Compaction

The root solver should compact history, but never lose operational state.

Preserve exactly:

- committed artifacts;
- dependency IDs and reverse edges;
- current ready queue;
- compact failure summaries;
- verifier counterexamples;
- final output schema;
- high-risk unresolved questions.

Compress aggressively:

- full prompts;
- old candidate code;
- verbose model rationales;
- repeated failed attempts.

This differs from ordinary context truncation. It is state compaction, not text
compaction.

## Relationship to Current RLM Braid Runtime

Keep:

- staged REPL phases;
- runtime-owned recursion IDs;
- `ephemeral_helper_solve`;
- helper validation/repair loop;
- official LongCoT verification bridge;
- `--rlm-final-from-verified-handoff`;
- telemetry around node lifecycle and helper attempts.

Change:

- graph plan should seed a root-owned `SolverState`, not only a one-shot braid;
- node solving should return structured `WorkArtifact` packets;
- helper failures should produce reusable counterexamples and failed candidate
  records;
- finalization should read committed artifacts from root state;
- recursion should be an inner tactic for hard work items, not top-level global
  state ownership.

## Proposed Implementation Slices

Slices 1-3 are complete (pure state core, artifact contract, braid bridge).
The remaining work follows a tight sequence: stabilize graph planning, fix
helper deadline accounting, make failures produce artifacts, then add the
first archetype. Only then re-run probes.

### Slice 1: Pure Solver State Package (DONE)

```text
internal/rlm/runtime/generalsolver/
  types.go state.go scheduler.go verifier.go markdown.go
  artifact.go braid_bridge.go
  *_test.go
```

96 tests. No IO, no time calls, no external imports.

### Slice 2: Work-Item Artifact Contract (DONE)

Parser, validator, text extraction, counterexample normalization. Table tests
for solved, partial, blocked, malformed, missing-answer artifacts.

### Slice 3: Braid Bridge Adapter (DONE)

`BraidToWorkItems`, `BraidSummaryToArtifact`, `ArtifactToBraidSummary`.
Maps braid nodes into generalsolver state without changing the public CLI.

### Slice 4: Graph Normalization Hardening

The logic/easy probe failed because the model produced `python_repl` as an
unknown JSON field and an `expected_output` of 474 chars (max is 240). The
graph parser rejected the entire graph. Normalization before validation would
recover this cheaply.

Changes in `internal/rlm/runtime/braid_graph.go`:

1. Decode the raw graph JSON into a permissive map structure, then extract
   only the known fields (`id`, `kind`, `question`, `depends_on`,
   `expected_output`, `max_summary_chars`, `helper_policy`) into the canonical
   `BraidNode` struct. Silently drop unknown fields.
2. Clamp `expected_output` to 240 chars (truncate with `...[trunc]`).
3. Clamp `question` to 480 chars.
4. Default missing optional fields: `depends_on` to `[]`, `helper_policy` to
   `""`, `max_summary_chars` to `0`.
5. Add a graph-plan repair prompt that explicitly says: return only `id`,
   `kind`, `question`, `depends_on`, `max_summary_chars`, `helper_policy`.
6. Log stripped/clamped fields at warning level for telemetry.

Success criterion:

```text
BlocksWorld_easy_1 passes with the same model even when the model
produces extra JSON fields or verbose expected_output.
```

Validation:

```bash
CGO_ENABLED=0 go test ./internal/rlm/runtime/ -run TestNormalizeBraid
CGO_ENABLED=0 go test ./internal/rlm/runtime/ -run TestBraidGraph
CGO_ENABLED=0 go test ./internal/rlm/runtime/ -run TestParseBraid
```

### Slice 5: Helper Deadline Accounting

CS, chemistry, and chess all show a second helper attempt failing instantly
(0-1ms). The repair attempt inherits an almost-expired context or the overall
attempt deadline is consumed by the first attempt. The failure message is
opaque: "helper did not produce an answer after 1ms" tells us nothing useful.

Changes in `internal/rlm/runtime/braid_executor.go` and
`internal/rlm/runtime/helper_factory_tools.go`:

1. Give each helper draft/repair attempt its own bounded sub-timeout, derived
   from the remaining node budget, not from the overall attempt deadline.
   Example: if node timeout is 12m and attempt 1 consumed 10m, attempt 2 gets
   the remaining 2m, not a fresh 12m and not 0m.
2. Track cumulative helper duration per node separately from the overall node
   timeout.
3. When a helper attempt fails, the feedback must include the stage:
   `draft`, `parse`, `validate`, `run`, or `verify`. Not just "helper did not
   produce an answer after Xms" but "draft timed out after 120s" or
   "validate failed: SyntaxError at line 4".
4. When a repair attempt is skipped due to deadline exhaustion, report
   `deadline_exhausted` with the cumulative duration and number of prior
   attempts. Do not report a generic helper failure.
5. Cap repair feedback length to avoid pushing the next attempt's input over
   the token budget.

Success criterion:

```text
Second helper attempt either runs with meaningful budget and structured
feedback, or is skipped with a "deadline_exhausted" verdict that
includes stage, cumulative duration, and attempt count.
```

Validation:

```bash
CGO_ENABLED=0 go test ./internal/rlm/runtime/ -run TestHelper
```

### Slice 6: Make Helper Failures Produce Artifacts

Right now failed helper attempts are dead ends: opaque timeout messages with no
structured state. This slice makes every failure a structured artifact that the
root solver can use for intelligent retry.

Wire `generalsolver.SolverState` into `executePhaseBraidGraph`:

1. Seed a `SolverState` from the braid graph using the bridge adapter when
   graph fanout begins.
2. When a helper attempt succeeds, commit a `WorkArtifact` with status, answer,
   confidence, and checks.
3. When a helper attempt fails, record a `FailureEntry` with structured data:

```json
{
  "stage": "draft|parse|validate|run|verify",
  "source_excerpt": "first 200 chars of generated code",
  "error": "SyntaxError: unterminated string literal (line 4)",
  "diagnostic": {"line": 4, "col": 41},
  "counterexample": {},
  "repair_hint": "fix the unterminated string at line 4"
}
```

4. After graph execution completes (or fails), include the solver state
   summary in the output telemetry alongside the existing summaries map.
5. The generalsolver state runs as a structured shadow. It does not replace
   the existing `summaries` map or `executed` set yet.

Success criterion:

```text
Probe result.json includes generalsolver state summary showing
committed items, failed items with stage and error, and failure digests.
```

Validation:

```bash
CGO_ENABLED=0 go test ./internal/rlm/runtime/ -run TestBraidExecutor
CGO_ENABLED=0 go test ./internal/rlm/runtime/generalsolver/
```

### Slice 7: Symbolic Trace Archetype

HM_easy_1 is the first real general-solver test. The helper cannot draft
Algorithm W code because it receives raw problem text instead of structured
input. This archetype gives the helper typed handoff data.

New scaffold under `internal/rlm/runtime/braid_scaffold.go`:

```text
BraidScaffoldClassSymbolicTrace = "symbolic_trace"
BraidScaffoldIDTypeInferenceV1  = "type_inference_v1"
```

Generic contract (not Hindley-Milner-specific):

- `events`: ordered list of symbolic events (e.g., let-binding, application,
  annotation).
- `state`: environment/binding map (variable -> type scheme).
- `rules`: ordered inference rules (generalize, instantiate, unify).
- `queries`: specific targets to answer.
- `checker`: executable function that verifies answers against constraints.

The helper receives these as typed input, not as raw problem prose.

Success criterion:

```text
HM_easy_1 produces a structured failure report (wrong type, unification
conflict, or timeout with partial progress) instead of an opaque helper
timeout. Passing correctness is not required initially.
```

### Slice 8: Re-Run One-Question Probes

After slices 4-7, re-run one question per domain, sequentially:

```text
logic/easy/BlocksWorld_easy_1       should recover after slice 4
cs/easy/HM_easy_1                   target of slice 7
chemistry/easy/easy1_0              expect structured failure after slices 5-6
chess/easy/uci_to_fen_easy_1        expect structured failure after slices 5-6
math/easy/backtracking_easy_1       needs separate token-budget fix
```

Do not run mixed 5/15/30 question sets until the one-question probes are
stable. Each probe must produce a saved artifact with structured state.

## Definition of Done

For the first general-solver implementation slice, done means:

- Pure state transitions are covered by unit tests:
  - add work item;
  - resolve dependencies;
  - compute ready queue;
  - commit artifact;
  - record verifier failure;
  - compact failure digest.
- `WorkArtifact` parsing and validation are covered by table tests:
  - solved artifact;
  - partial artifact;
  - blocked artifact;
  - malformed JSON;
  - solved artifact without answer;
  - artifact with counterexamples.
- The current LongCoT logic/easy known-good path does not regress.
- The `HM_easy_1` CS probe no longer fails as an opaque helper timeout. It may
  still be incorrect, but the report must identify a structured failure stage:
  graph planning, work-item solving, verifier rejection, repair exhaustion, or
  finalization.
- One-question probes exist for all five official LongCoT domains and write
  saved artifacts under `.foxctl/longcot-autoresearch/`.
- Probe reports include:
  - question ID;
  - domain and difficulty;
  - selected archetype;
  - phase where the attempt ended;
  - helper attempts;
  - verifier status;
  - duration;
  - token usage;
  - final answer or structured failure.
- No benchmark-specific deterministic shortcut is added for an official
  question. Generic archetypes and runtime-verified helpers are allowed.
- Docs link checks pass.
- Relevant Go tests pass with `CGO_ENABLED=0`.

## Test and Probe Commands

Run from this worktree:

```bash
cd /Users/joshka/repos/worktrees/feat-rlm-general-solver
```

The live OpenRouter credentials are in the main checkout env file:

```bash
set -a
source /Users/joshka/repos/personal/foxctl/.env
set +a
```

Do not commit `.env` or copy secrets into docs, fixtures, or artifacts.

### Unit and Build Checks

```bash
GOCACHE=/tmp/foxctl-go-build-cache CGO_ENABLED=0 go test ./internal/rlm/runtime
GOCACHE=/tmp/foxctl-go-build-cache CGO_ENABLED=0 go test ./cmd/foxctl/cmd
GOCACHE=/tmp/foxctl-go-build-cache CGO_ENABLED=0 go build -o bin/foxctl ./cmd/foxctl
make check-doc-links
```

If a new pure package is added for the solver state, also run:

```bash
GOCACHE=/tmp/foxctl-go-build-cache CGO_ENABLED=0 go test ./internal/rlm/runtime/generalsolver
```

### Build One-Question Probe Datasets

Use the official local LongCoT checkout:

```bash
LONGCOT_REPO=/Users/joshka/repos/githubs/LongCoT
```

Create one JSONL file per domain:

```bash
python3 - <<'PY'
import json
import pathlib

root = pathlib.Path("/Users/joshka/repos/githubs/LongCoT/src/data")
cases = [
    ("logic", "easy", "BlocksWorld_easy_1", "/tmp/foxctl-longcot-probe-logic-easy-1.jsonl"),
    ("cs", "easy", "HM_easy_1", "/tmp/foxctl-longcot-probe-cs-easy-1.jsonl"),
    ("chemistry", "easy", "easy1_0", "/tmp/foxctl-longcot-probe-chemistry-easy-1.jsonl"),
    ("chess", "easy", "uci_to_fen_easy_1", "/tmp/foxctl-longcot-probe-chess-easy-1.jsonl"),
    ("math", "easy", "backtracking_easy_1", "/tmp/foxctl-longcot-probe-math-easy-1.jsonl"),
]

for domain, difficulty, question_id, out_path in cases:
    data = json.load(open(root / domain / f"{difficulty}.json"))
    match = None
    for q in data.get("questions", []):
        if q.get("question_id") == question_id:
            match = q
            break
    if match is None:
        raise SystemExit(f"missing {domain}/{difficulty}/{question_id}")
    row = {
        "question_id": match["question_id"],
        "domain": domain,
        "difficulty": difficulty,
        "template": (match.get("problem") or {}).get("template"),
        "prompt": match["prompt"],
        "answer": match.get("answer"),
    }
    with open(out_path, "w") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")
    print(out_path)
PY
```

### Run One Probe

Replace `DOMAIN` and `DATASET` as needed:

```bash
DOMAIN=logic
DATASET=/tmp/foxctl-longcot-probe-logic-easy-1.jsonl

set -a
source /Users/joshka/repos/personal/foxctl/.env
set +a

bin/foxctl eval longcot \
  --dataset "$DATASET" \
  --longcot-repo /Users/joshka/repos/githubs/LongCoT \
  --condition rlm_braid_single \
  --provider openrouter \
  --model deepseek/deepseek-v4-flash \
  --timeout 12m \
  --max-tokens 8192 \
  --max-iterations 64 \
  --sandbox python \
  --helper-language python \
  --helper-attempts 5 \
  --save \
  --output-dir ".foxctl/longcot-autoresearch/general-solver-probe-${DOMAIN}-easy-1" \
  --verify \
  --format markdown \
  --general-helper \
  --blocksworld-helper=false \
  --rlm-final-from-verified-handoff
```

### Run All Five Probes Sequentially

Run sequentially rather than in parallel so provider latency, local sandbox
state, and logs remain easy to inspect:

```bash
set -a
source /Users/joshka/repos/personal/foxctl/.env
set +a

for spec in \
  "logic:/tmp/foxctl-longcot-probe-logic-easy-1.jsonl" \
  "cs:/tmp/foxctl-longcot-probe-cs-easy-1.jsonl" \
  "chemistry:/tmp/foxctl-longcot-probe-chemistry-easy-1.jsonl" \
  "chess:/tmp/foxctl-longcot-probe-chess-easy-1.jsonl" \
  "math:/tmp/foxctl-longcot-probe-math-easy-1.jsonl"
do
  DOMAIN="${spec%%:*}"
  DATASET="${spec#*:}"
  bin/foxctl eval longcot \
    --dataset "$DATASET" \
    --longcot-repo /Users/joshka/repos/githubs/LongCoT \
    --condition rlm_braid_single \
    --provider openrouter \
    --model deepseek/deepseek-v4-flash \
    --timeout 12m \
    --max-tokens 8192 \
    --max-iterations 64 \
    --sandbox python \
    --helper-language python \
    --helper-attempts 5 \
    --save \
    --output-dir ".foxctl/longcot-autoresearch/general-solver-probe-${DOMAIN}-easy-1" \
    --verify \
    --format markdown \
    --general-helper \
    --blocksworld-helper=false \
    --rlm-final-from-verified-handoff
done
```

### Inspect Probe Results

```bash
find .foxctl/longcot-autoresearch -path '*general-solver-probe-*' -name result.json -print \
  -exec jq '.summary.conditions[0]' {} \;
```

For a failed probe, inspect the RLM trajectory:

```bash
jq '.attempts[0].rlm.metadata.trajectory_events // .attempts[0].rlm.trajectory_events // []' path/to/result.json
```

Known baseline after probe run (2026-04-26):

```text
logic/easy/BlocksWorld_easy_1       graph parse failure (unknown field, oversized expected_output)
cs/easy/HM_easy_1                   helper-first solve timed out 5/5 drafts
chemistry/easy/easy1_0              helper-first solve: 1 syntax error + 4 timeouts
chess/easy/uci_to_fen_easy_1        helper-first solve timed out 5/5 drafts
math/easy/backtracking_easy_1       max-token stop, no graph produced
```

## Agent Experiment Prompts

Agents should be scoped to one slice each. Agents 1-2 can run in parallel
since they touch different files.

### Agent 1: Graph Normalization (Slice 4)

Harden the braid graph parser in `internal/rlm/runtime/braid_graph.go` to
handle model variance. Strip unknown JSON fields before validation. Clamp
`expected_output` to 240 chars and `question` to 480 chars. Default missing
optional fields. Log stripped/clamped fields at warning level. Add a
graph-plan repair prompt that says: return only `id`, `kind`, `question`,
`depends_on`, `max_summary_chars`, `helper_policy`. Add tests for graphs with
unknown fields, oversized fields, and mixed issues. Do not change validation
rules -- only the normalization step before validation.

### Agent 2: Helper Deadline Accounting (Slice 5)

Fix the 0ms/1ms second helper failure pattern in
`internal/rlm/runtime/braid_executor.go` and
`internal/rlm/runtime/helper_factory_tools.go`. Give each helper attempt its
own bounded sub-timeout derived from remaining node budget. Track cumulative
helper duration per node. Include draft stage in failure feedback. When a
repair attempt is skipped due to deadline, report `deadline_exhausted` with
cumulative duration and attempt count. Cap repair feedback length. Add tests
for budget tracking, feedback capping, and early rejection when budget is
exhausted.

### Agent 3: Structured Helper Failure Artifacts (Slice 6)

Wire `generalsolver.SolverState` into `executePhaseBraidGraph` in
`internal/rlm/runtime/braid_executor.go`. Seed state from braid graph using
the bridge adapter. Commit successful helper results as `WorkArtifact`. Record
failed attempts as `FailureEntry` with `stage`, `source_excerpt`, `error`,
`diagnostic`, `counterexample`, and `repair_hint`. Include solver state summary
in output telemetry. Run alongside existing summaries map as structured shadow.

### Agent 4: Symbolic Trace Archetype (Slice 7)

Prototype a domain-general symbolic trace archetype in
`internal/rlm/runtime/braid_scaffold.go` using `HM_easy_1` as the first test
case. The generic contract is: ordered events, environment/state map, named
bindings, query targets, executable transition/check function. Provide typed
handoff with `variables`, `rules`, `queries`, `bindings`, and `checker`. Do
not hardcode Hindley-Milner names.

### Agent 5: Probe Re-Run (Slice 8)

After slices 4-7 land, re-run the one-question probes sequentially. Each probe
must produce a saved artifact under `.foxctl/longcot-autoresearch/`. Report
should show: question_id, domain, archetype, failure phase, helper attempts,
verifier status, duration, tokens, and whether the failure was graph-planning,
work-item solving, verification, repair exhaustion, or finalization.

## Open Questions

- Should `WorkArtifact.Code` store full source, source hash, or both?
- How much model-generated verifier code should be retained in artifacts?
- Should recursive child calls be allowed to spawn their own local
  `SolverState`, or only return artifacts to the root state?
- What is the minimum telemetry needed for later workflow optimization?
- How should verified final handoff interact with partial-but-useful artifacts?
- Should the graph normalizer strip unknown fields silently, or attempt to
  preserve them in a `metadata` passthrough?
- Is the 0ms second-failure a context deadline issue or a token budget issue?
  Need to inspect the helper factory's cancel path.
- How should the symbolic trace archetype handle problems where the event log
  is too long to fit in a single helper input?

## Immediate Recommendation

Slices 1-3 are done. The quickest practical wins are:

1. **Slice 4: graph normalization** (Agent 1) -- cheapest fix, recovers
   logic/easy.
2. **Slice 5: helper deadline accounting** (Agent 2) -- stops wasteful retries,
   fixes the 0ms pattern.

These two can land in parallel since they touch different files.

After both land, re-run the logic/easy probe to confirm recovery. Then:

3. **Slice 6: structured failure artifacts** (Agent 3) -- makes every failure
   a structured artifact instead of an opaque timeout.
4. **Slice 7: symbolic trace archetype** (Agent 4) -- first real archetype
   test on HM_easy_1.
5. **Slice 8: re-run probes** (Agent 5) -- confirm structured failures across
   all five domains.

Do not run mixed 5/15/30 question sets until the one-question probes are
stable. The next useful milestone is not a higher benchmark score. It is a
solver trace that can explain, for each failed work item, what state existed,
what was tried, what verifier rejected, and what should be attempted next.

## Implementation Results

Initial implementation of all 8 slices has landed on branch `feat/rlm-general-solver`.

### Files Changed

| File | Change |
|------|--------|
| `internal/rlm/runtime/generalsolver/` (new package) | Pure solver state types: `types.go`, `state.go`, `scheduler.go`, `verifier.go`, `markdown.go`, `artifact.go`, `braid_bridge.go` + 4 test files |
| `internal/rlm/runtime/braid_graph.go` | Removed `DisallowUnknownFields`, added field clamping, symbolic trace detection and handoff |
| `internal/rlm/runtime/braid_graph_test.go` | Normalization tests, budget tests, symbolic trace scaffold tests |
| `internal/rlm/runtime/braid_executor.go` | Helper deadline budget tracking, `braidNodeHelperBudget`, solver state wiring, telemetry |
| `internal/rlm/runtime/braid_scaffold.go` | `symbolic_trace` / `type_inference_v1` constants, scaffold resolution, preset source, verifier |
| `internal/rlm/runtime/repl_runner_test.go` | Updated telemetry expectations |
| `docs/plans/features/rlm-general-solver-proposal.md` | This file |

### Slice-by-Slice Summary

| Slice | What | Status |
|-------|------|--------|
| 1 | Pure solver state package (`generalsolver/`) | Done, 96 tests |
| 2 | Artifact contracts (`artifact.go`) | Done |
| 3 | Braid bridge adapter (`braid_bridge.go`) | Done |
| 4 | Graph normalization hardening | Done: strips unknown fields, clamps oversized fields |
| 5 | Helper deadline accounting | Done: per-node budget, staged failure messages |
| 6 | SolverState wiring + telemetry | Done: structured shadow, JSON telemetry tool call |
| 7 | Symbolic trace archetype | Done: detection, handoff, preset, verifier |
| 8 | One-question probe re-run | Done: all 5 domains |

### Probe Results (Post-Implementation)

All probes used `deepseek/deepseek-v4-flash` via OpenRouter (v1-v3) or
`qwen/qwen3.6-flash` (v4), 12m timeout, 5 helper attempts.

| Domain | Question | Status | Duration | Tokens | Failure Detail |
|--------|----------|--------|----------|--------|----------------|
| logic | BlocksWorld_easy_1 | **ok/correct** | 46s | 5,307 | Fully solved |
| cs | HM_easy_1 | error | 720s | 10,262 | `stage=run duration_ms=380403` — helper ran but could not produce answer for 364-binding HM type inference |
| chemistry | easy1_0 | error | 720s | 12,582 | `stage=run duration_ms=107647` — helper ran 107s before timeout |
| chess | uci_to_fen_easy_1 | error | 720s | 7,542 | `stage=run duration_ms=0` — helper failed immediately |
| math | backtracking_easy_1 | error | 720s | 6,407 | `stage=run duration_ms=0` — helper failed immediately |

**V3 probe re-run (after candidate_verify archetype + preflight):**

| Domain | Status | Duration | Tokens | Failure Detail |
|--------|--------|----------|--------|----------------|
| cs | error | 720s | 11,626 | `stage=run 379s` — unchanged, needs work-item splitter |
| chemistry | error | 580s | 7,110 | `stage=run 234s` — ran 2x longer than v1, candidate_verify archetype likely applied |
| chess | error | 720s | 22,384 | `stage=run 232s` — ran much longer than v1 (0ms), but no archetype scaffold yet |
| math | error | 720s | 4,456 | `stage=run 44s` on verify node, not solve — model produced different graph plan |

**V4 probe re-run (qwen/qwen3.6-flash, after splitter + fail-closed scaffold):**

| Domain | Status | Duration | Tokens | Archetype | Scaffolded | Failure Detail |
|--------|--------|----------|--------|-----------|------------|----------------|
| logic | ok | 8.8s | 4,216 | state_transition | yes | — |
| chemistry | ok | 76.6s | 4,425 | candidate_verify | yes | — |
| cs | error | 67.1s | 11,188 | symbolic_trace | yes | `stage=run 27s` — helper scaffolded but couldn't produce answer |
| chess | error | 88.6s | 6,925 | (none detected) | yes | `stage=run 25s` — helper ran but no archetype matched |
| math | error | 123.1s | 4,226 | (none detected) | yes | `stage=run 33s` — helper ran but no archetype matched |

**Improvements vs. v3:**

- Logic: stable, still solved. 8.8s with state_transition archetype.
- Chemistry: **SOLVED** (was error). candidate_verify archetype with fail-closed
  scaffold produces a verifiable answer in 76.6s.
- CS: scaffolded (symbolic_trace/type_inference_v1) but helper still can't
  produce a working type inference program in 27s. The splitter didn't trigger
  because the graph plan from qwen doesn't produce >50KB payloads.
- Chess: helper ran 25s but no archetype matched. Needs state_transition/UCI
  scaffold (fix order item 5).
- Math: helper ran 33s but no archetype matched. Needs explicit_dag/graph
  retry (fix order item 6).

**Improvements vs. initial probes (v1 → v4):**

- Logic: was graph parse failure (unknown field v1), now fully solved (v1-v4).
- CS: was opaque "helper did not produce an answer after 1ms" (v1), now
  structured `stage=run 27s` with symbolic_trace scaffold + archetype-based
  graph splitting. Still fails — helper cannot produce working type inference
  code in the time budget.
- Chemistry: was "1 syntax error + 4 timeouts" (v1), now **SOLVED** (v4)
  with candidate_verify archetype + fail-closed scaffold in 76.6s.
- Chess: was `stage=run 0ms` (v1), now `stage=run 25s` (v4) with helper
  running. state_transition/uci_to_fen archetype added — pending v5 re-run.
- Math: was `stage=run 0ms` (v1), now `stage=run 33s` (v4) with helper
  running. explicit_dag/backtracking archetype added — pending v5 re-run.

### Probe Log Locations

Each probe writes results under
`.foxctl/longcot-autoresearch/` in this worktree:

```
.foxctl/longcot-autoresearch/
  general-solver-probe-logic-easy-1/     # logic probe
  general-solver-probe-cs-easy-1-v2/     # CS probe (with symbolic trace detection fix)
  general-solver-probe-chemistry-easy-1/ # chemistry probe
  general-solver-probe-chess-easy-1/     # chess probe
  general-solver-probe-math-easy-1/      # math probe
```

Each directory contains:

```
longcot/longcot-live-<run-id>/
  report.md                   # Human-readable summary
  result.json                  # Full structured result (status, tokens, error, tool events, rlm metadata)
  responses.official.jsonl     # Official LongCoT response format
```

**To inspect a specific probe:**

```bash
# Quick summary
cat .foxctl/longcot-autoresearch/general-solver-probe-cs-easy-1-v2/longcot/longcot-live-*/report.md

# Full structured result (status, error, tool_events, rlm metadata)
RESULT=$(ls .foxctl/longcot-autoresearch/general-solver-probe-cs-easy-1-v2/longcot/longcot-live-*/result.json | head -1) && \
python3 -c "import json,sys; d=json.load(open('$RESULT')); a=d['attempts'][0]; print('status:', a['status']); print('error:', a.get('error','N/A')[:300]); print('rlm:', json.dumps(a.get('rlm',{}), indent=2)[:1000])"

# Re-run a single probe
bin/foxctl eval longcot \
  --dataset /tmp/foxctl-longcot-probe-cs-easy-1.jsonl \
  --longcot-repo /Users/joshka/repos/githubs/LongCoT \
  --condition rlm_braid_single \
  --provider openrouter \
  --model deepseek/deepseek-v4-flash \
  --timeout 12m --max-tokens 8192 --max-iterations 64 \
  --sandbox python --helper-language python --helper-attempts 5 \
  --save --output-dir ".foxctl/longcot-autoresearch/general-solver-probe-cs-easy-1-v3" \
  --verify --format markdown --general-helper --blocksworld-helper=false \
  --rlm-final-from-verified-handoff
```

### Implementation Status

Slices 1-8 have landed as initial implementation. The runtime shadow wiring
(seeding, commit, failure recording, telemetry) is functional. Post-hoc
event reconstruction (`StateFromBraidEvents`) is partial — it seeds items
and records outcomes from events but does not reconstruct full dependency
edges or artifact payloads.

### Root Cause: Helpers Invent Solvers from Prose

The remaining failures across CS, chemistry, chess, and math share one
deeper issue: **extract works, then helper-run fails because the work item
is too underspecified and too large.** The helper receives the original
prompt plus a short summary and is expected to invent a complete solver from
scratch. This fails reliably for non-trivial domains.

The fix is not domain-specific solvers. It is three generic layers:

#### Layer 1: Typed Handoff Packets

Every work item should enter the helper as structured data, not the original
prompt plus a summary. Generic packet shape:

```json
{
  "archetype": "symbolic_trace|state_transition|candidate_verify|explicit_dag",
  "task": "...",
  "inputs": {},
  "queries": [],
  "rules": [],
  "constraints": [],
  "output_schema": {},
  "checks": []
}
```

Chess UCI moves, chemistry candidates, HM bindings, and math subproblems all
become different values inside the same packet contract.

#### Layer 2: Archetype Presets, Not Domain Solvers

Reusable scaffold presets by computational shape:

| Archetype | Shape | Fits |
|-----------|-------|------|
| `symbolic_trace` | ordered events, environment updates, queries | HM/type inference, program traces, algebraic derivations |
| `state_transition` | initial state, actions, transition rules, final state query | chess move replay, Hanoi, BlocksWorld, simulations |
| `candidate_verify` | candidates, property predicates, selection rule | chemistry multiple choice, chess best-move filtering, answer tuple selection |
| `explicit_dag` | nodes, dependencies, required outputs | math backtracking, chained subproblems |
| `table_recurrence` | recurrence/state table, indexes, query points | scheduling, DP, counting problems |

The preset provides the helper with a small known harness and asks it only to
fill domain details or predicates, not write a full engine.

#### Layer 3: Split Large Work Items Before Helper Solve

CS failed because one helper had to solve 364 HM bindings and 5 queries. That
is too big for a single helper invocation.

Generic split rule:

```text
If extract creates multiple queries or a long event list:
  create one parse/model item
  create one or more solve items over slices/windows/queries
  create one merge item
```

Examples:

- **HM**: one environment builder, one query item per requested query, one
  merge.
- **Chess**: one move parser, one simulation chunk per N moves, one final FEN
  item.
- **Chemistry**: one candidate parser, one property-check item per subproblem,
  one merge.
- **Math**: one DAG node per subproblem, not one huge graph-plan completion.

#### Domain Mapping Without Domain Lock-In

| Domain | Archetype | Runtime owns | Helper fills |
|--------|-----------|-------------|--------------|
| CS/HM | `symbolic_trace` | environment, bindings, event log, query targets | transition functions, repair |
| Chemistry | `candidate_verify` | candidates, requested predicates | property checkers (RDKit if available, else structured failure) |
| Chess | `state_transition` | move list, starting state | python-chess replay if available, else scaffold detects missing backend early |
| Math | `explicit_dag` | nodes, deps, output schema | subproblem solvers per node |

### Fix Order

1. **Helper preflight**: detect library availability, validate typed packet
   size, reject too-large helper tasks before drafting code.
   **Done.** `helperPreflightCheck` in `braid_executor.go` rejects packets
   over 50KB before spending LLM tokens.
2. **Generic work-item splitter**: decompose multi-query/large-event work
   items into parse + solve + merge.
   **Done.** `splitter.go` in `generalsolver/` with `AnalyzeForSplit`,
   `ApplySplit`, `SplitWorkItem`. Handles query decomposition (queries,
   sub_problems, bindings, events, constraints arrays) and fallback
   chunk decomposition. Wired into executor via `applyGraphLevelSplits`
   (proactive split during graph seeding) and `attemptSplitHelperExecution`
   (reactive split when preflight rejects oversized input).
3. **`candidate_verify` packet contract** for chemistry.
   **Done.** `BraidScaffoldClassCandidateVerify` /
   `BraidScaffoldIDPropertyCheckV1` in `braid_scaffold.go` with detection,
   handoff, preset source, and verifier. Wired into `braid_graph.go` with
   prompt-based detection for SMILES/molecule/compound + selection patterns.
   **Updated**: preset source now fail-closed — unknown predicate types
   default to `False` instead of `True`; explicit check_type implementations
   for equals, contains, not_contains, greater_than, less_than.
4. **`symbolic_trace` packet contract** for CS (partially done in slice 7,
   needs the split layer from item 2).
   **Done** — splitter handles HM bindings decomposition.
5. **`state_transition` packet contract** for chess (partially exists as
  `finite_state_transition`, needs UCI/FEN variant).
6. **Graph-only retry for math**: force graph creation before solving; if
  graph planning hits max tokens, retry with lower-output schema ("emit only
  nodes/deps/output schema, no reasoning").
