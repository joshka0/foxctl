# Investigation: LongCoT as a foxctl Token/Reasoning Eval

## Summary

LongCoT can be incorporated as an **internal paired A/B eval** to test whether foxctl tools/scaffolding reduce token cost and improve long-horizon reasoning accuracy, but the tool-augmented condition must be explicitly labeled **not comparable to LongCoT's primary no-tool leaderboard setting**. The best architecture is a new `foxctl eval longcot` command and `internal/tooling/evals/longcoteval` package that reuses foxctl's eval/report/runtime primitives while delegating question loading and answer verification to the official LongCoT Python package.

## Symptoms / Product Question

- We want evidence that foxctl tools save tokens and improve long-horizon reasoning accuracy, not just anecdotal impressions.
- LongCoT is designed to isolate long-horizon chain-of-thought capability without tools/scaffolding, while foxctl's hypothesis is specifically that tools/scaffolding can improve production agent performance.
- Therefore, LongCoT is useful as an internal ablation suite, but tool-augmented runs should not be submitted or described as official primary-setting LongCoT results.

## External Context Verified

- LongCoT's website describes it as benchmarking long-horizon chain-of-thought reasoning and says the primary benchmark is designed to measure this directly rather than through retrieval, tool use, or agent scaffolds: https://longcot.ai/
- It contains 2,500 expert-designed problems across chemistry, mathematics, computer science, chess, and logic; each prompt is short with a verifiable answer, but requires long interdependent reasoning traces: https://longcot.ai/
- LongCoT's GitHub README states that responses are JSONL with `question_id`, `successful`, `response_text`, `model`, `usage`, and optionally `reasoning`, and that `run_eval.py` verifies JSONL responses: https://github.com/LongHorizonReasoning/longcot
- The Python API exposes `longcot.load_questions(...)` and `longcot.verify(...)`, making an official verifier bridge feasible: https://github.com/LongHorizonReasoning/longcot
- The dataset is hosted on Hugging Face as `LongHorizonReasoning/longcot` with all/domain subsets and easy/medium/hard splits: https://huggingface.co/datasets/LongHorizonReasoning/longcot

## Investigation Log

### Phase 1 - Initial Assessment

**Hypothesis:** Existing foxctl eval infrastructure can support LongCoT with limited new code.

**Findings:** There is no LongCoT-specific code today, but the repo has multiple eval harnesses and telemetry structures that can be reused.

**Evidence:**

- Root eval command registers existing eval subcommands in `cmd/foxctl/cmd/eval.go:36-45`.
- Prompt-eval JSONL loading already exists in `cmd/foxctl/cmd/optimize.go:1342-1377`.
- Current prompt eval cases support `question`, `context`, expected paths/symbols/snippets/facts, category, and session fields in `cmd/foxctl/cmd/optimize.go:1823-1838`.

**Conclusion:** Confirmed. The harness shape exists, but LongCoT correctness should use the official verifier rather than foxctl's prompt judge or code-grounding scores.

### Phase 2 - Broad Context Gathering

**Hypothesis:** `context_builder` can discover the full relevant file set.

**Findings:** `context_builder` was attempted twice but failed with a context-window error before producing an analysis or selection. I continued with targeted RepoPrompt searches and three explore-agent investigations.

**Evidence:**

- First attempt failed with: `Codex ran out of room in the model's context window`.
- Second tighter attempt failed with the same error and selected zero files.
- Manual selection then focused the oracle on eval, runtime, token, trajectory, and tool files.

**Conclusion:** Context-builder broad discovery was unavailable in this session. This is a process limitation, not a codebase finding.

### Phase 3 - Existing Eval Harnesses

**Hypothesis:** Existing eval commands can be adapted for LongCoT reporting and experiment logging.

**Findings:** Several pieces are reusable, but LongCoT should be its own eval package/command.

**Evidence:**

- `cmd/foxctl/cmd/eval.go:36-45` registers eval subcommands; LongCoT can be added there.
- `cmd/foxctl/cmd/eval.go:443-612` shows the pattern for a suite-backed eval command with modes, policy file, workspace resolution, suite load, per-query result collection, and summary.
- `cmd/foxctl/cmd/eval.go:1385-1425` writes JSON and markdown artifacts for eval outputs.
- `internal/tooling/evals/retrievaleval/eval.go:1-130` defines a small suite/result/summary model with deterministic `LoadSuite`, `EvaluateMode`, and `Summarize` functions.
- `internal/tooling/evals/retrievaleval/eval.go:130-210` renders markdown summaries.
- `internal/tooling/evals/transcriptmemoryeval/experiment.go:1-80` defines saved artifact and append-only experiment record patterns.

**Conclusion:** Reuse command/report/artifact patterns. Do not reuse retrieval/code correctness scoring as the primary LongCoT score.

### Phase 4 - Agent Runtime and Token Accounting

**Hypothesis:** Current runtime already records enough token/tool telemetry to compare baseline vs tool use.

**Findings:** Current runtime records prompt/completion/total tokens and tool calls, but it lacks reasoning-token, cached-token, raw provider usage, generic per-tool raw/reduced token accounting, and clean separation of system/user/tool-schema/tool-result overhead.

**Evidence:**

- `internal/runtime/engine/engine.go:80-92` defines per-iteration usage with prompt tokens, completion tokens, total tokens, tool calls, tool names, tool result chars, and tool result token estimate.
- `internal/runtime/engine/engine.go:194-210` defines token usage only as input/output/total tokens.
- `internal/runtime/engine/llmchat_engine.go:320-327` appends per-iteration usage from OpenAI-compatible `prompt_tokens` and `completion_tokens`.
- `internal/runtime/engine/llmchat_engine.go:391-447` accumulates tokens and emits observability data for prompt/completion/total tokens.
- `internal/runtime/engine/llmchat_engine.go:459-479` estimates tool result tokens from tool-result chars after execution.
- `internal/runtime/engine/llmchat_engine.go:862-866` only models provider usage as `prompt_tokens` and `completion_tokens`; there is no field for reasoning tokens or raw usage details.
- `internal/agent/runtime/runtime.go:210-229` stores session-level input/output/total tokens and parent tool usage.
- `internal/agent/runtime/runtime.go:4238-4262` aggregates engine output token usage into the session and summarizes `code_search_ensemble` parent tool usage.
- `internal/adapters/skillslib/obs/llm.go:9-18` defines token/cost fields for model, input/output/total tokens, and USD cost.
- `internal/adapters/skillslib/obs/llm.go:124-159` calculates token cost and attaches it to observability spans.

**Conclusion:** Partially confirmed. Existing accounting is enough for a rough baseline, but not enough to defend claims about reasoning-token savings or all-in scaffold cost.

### Phase 5 - Tool Execution and Tool Result Accounting

**Hypothesis:** Tool execution is centralized enough to instrument per-tool cost/savings once.

**Findings:** Classic runtime has a central `ToolRunner` and `agentToolExecutor`; this is the right first instrumentation point. v2 has better turn/tool lineage, but its current run records do not include token accounting.

**Evidence:**

- `internal/agent/runtime/runtime.go:441-550` creates the LLM engine, resolves max context, creates tool definitions, and attaches a `ToolRunner`.
- `internal/agent/runtime/runtime.go:575-675` centralizes runtime tool execution and available tool handlers.
- `internal/runtime/engine/tool_runner.go:1-28` defines the `ToolExecutor` interface and notes that CAS offload is not handled in the runner.
- `internal/runtime/engine/tool_runner.go:88-169` executes tools with pre/post hook integration and has access to timing and result content; this is the right place to add generic per-tool input/output byte and token estimates.
- `internal/agent/tools/shell_tools.go:1-90` exposes a structured shell tool with `measure_raw` and `token_model` options.
- `internal/tooling/shellreduce/measure.go:1-58` already compares raw command output to reduced summary output and reports bytes/tokens saved.
- `internal/tooling/shellreduce/measure.go:190-220` uses `tiktoken` and a fallback heuristic token counter.
- `internal/v2/core/run/turn_record.go:17-34`, `internal/v2/core/run/iteration_record.go:3-13`, and `internal/v2/core/run/tool_call_record.go:7-24` provide v2 lineage records for turns, iterations, and tool calls.
- `internal/v2/runtime/runner/model_call.go:17-118` emits tool invoked/responded events and records tool-call results.
- `internal/v2/core/events/payloads.go:27-42` defines tool-invoked/tool-responded/turn-recorded payloads, but no token/cost fields.

**Conclusion:** Confirmed. Instrument classic `ToolRunner` first for per-tool telemetry; v2 should later get usage fields in run/iteration/tool records.

### Phase 6 - Trajectory / Provenance Storage

**Hypothesis:** Trajectory storage can serve as the result store for LongCoT.

**Findings:** Trajectory is useful for provenance/audit linkage, but LongCoT needs a benchmark-specific JSONL/results schema as its primary result format.

**Evidence:**

- `internal/storage/trajectory/types.go:36-62` defines trajectory event kinds including tool call and tool result.
- `internal/storage/trajectory/types.go:64-134` defines trajectory records with workspace, task/epic/job/trace/session fields and optional outcome.
- `internal/storage/trajectory/types.go:233-263` defines events with inline data and optional artifact digest.
- `internal/runtime/trajectorycapture/capture.go:56-130` starts trajectory capture and creates user request/trajectory records.
- `internal/runtime/trajectorycapture/capture.go:202-340` captures hook calls/results and result events.

**Conclusion:** Use trajectory IDs as optional provenance fields in LongCoT attempt results, but do not make trajectory the canonical LongCoT eval output.

### Phase 7 - Explore Agent Findings

**Hypothesis:** Parallel exploration will surface missing adjacent code.

**Findings:** Two explore agents produced useful findings; one failed to read enough files.

**Evidence:**

- Eval-infra agent confirmed useful patterns in `eval.go`, `optimize.go`, `eval_agents.go`, `retrievaleval`, `transcriptmemoryeval`, and `testdata/evals` fixtures.
- Token/trajectory agent confirmed four recording paths: trajectory DB, classic agent session turns, observability NDJSON, and v2 append-only events.
- Agent-tool-harness agent failed to complete; manual reads covered the classic tool loop and v2 tool records.

**Conclusion:** Parallel exploration supported the same architecture and highlighted v2 run records as important future integration points.

## Root Cause / Core Gap

foxctl has multiple eval and telemetry systems, but they are optimized for repo-retrieval, prompt comparison, transcript-memory, and agent role evaluation. LongCoT needs a different contract:

1. **Official verifier correctness**, not prompt-judge or code-grounding similarity.
2. **Paired A/B condition tracking** for the same `question_id`.
3. **Strict no-leakage isolation**, because tools could otherwise read datasets, answers, verifier code, memory, vault notes, or previous runs.
4. **Detailed reasoning/tool token accounting**, because current runtime only exposes prompt/completion/total tokens and a narrow `code_search_ensemble` parent-usage estimate.

The existing code gets us close on command/report mechanics and rough token usage, but not on reasoning-token availability, raw provider usage preservation, generic tool overhead accounting, or leakage-aware benchmark isolation.

## Recommended Architecture

### Add a dedicated eval command

Add:

```text
foxctl eval longcot
```

Register it in `cmd/foxctl/cmd/eval.go` alongside the existing eval subcommands.

### Add a dedicated package

Add:

```text
internal/tooling/evals/longcoteval/
  suite.go       // load questions via official LongCoT Python bridge
  verify.go      // verify responses via official LongCoT Python bridge
  runner.go      // no-tool and tool-augmented runner interfaces
  result.go      // question/condition/attempt/pair/summary schemas
  summary.go     // paired A/B aggregation
  markdown.go    // markdown reports
```

### Use three conditions, not two

For clean attribution:

1. `baseline_no_tools_official_prompt`
   - Direct no-tool LLM call.
   - Prompt should match LongCoT as closely as possible.
   - This is the only condition close to leaderboard comparability.

2. `foxctl_no_tools_scaffold`
   - Same model, no tools, but foxctl answer/scaffold prompt.
   - Measures prompt/scaffold effect without external tools.

3. `foxctl_tools_minimal`
   - Fresh agent/engine session with strict tool allowlist.
   - Measures tool effect.

### Recommended initial tool profile

Start conservative:

```text
think only, or a new deterministic scratchpad/calculator tool if implemented safely
```

Avoid initially:

```text
filesystem, shell, repo search, context search, memory, session recall, Obsidian, network, subagents, verifier access
```

Only after leakage controls are proven should the eval add specialized deterministic tools like calculators, simulators, or domain-specific solvers.

### Official verifier bridge

Do not reimplement LongCoT verification in Go.

Flow:

1. Python bridge loads questions from official package / dataset.
2. foxctl runs attempts and writes official-compatible JSONL:
   - `question_id`
   - `successful`
   - `response_text`
   - `model`
   - `usage`
   - optional `reasoning`
3. Python bridge runs official `verify` / `run_eval.py` logic.
4. foxctl imports `correct`, `incorrect`, `failed`, `wrong_formatting`, `accuracy`, and `overall_accuracy`.
5. foxctl adds paired token/tool/cost deltas.

## Proposed Result Schema

### Attempt fields

Required fields beyond current eval structs:

```text
run_id
pair_id
attempt_id
question_id
domain
difficulty
task_family
question_hash
condition_id
condition_kind
prompt_template_version
tool_profile
allowed_tools
max_tokens
max_iterations
timeout_ms
temperature
seed
provider
model
runner
status
response_text
reasoning_text or reasoning_artifact_digest
successful
verifier_status
wrong_formatting
verification_error
normalized_answer
duration_ms
error
usage
raw_provider_usage
tool_events
leakage_flags
trajectory_id
session_id
```

### Usage fields

```text
input_tokens
output_tokens
total_tokens
reasoning_tokens
cached_input_tokens
system_prompt_token_estimate
user_prompt_token_estimate
tool_schema_token_estimate
tool_result_token_estimate
parent_prompt_delta_estimate
loaded_token_estimate
emitted_token_estimate
compaction_ratio
input_cost_usd
output_cost_usd
total_cost_usd
raw_provider_usage
iterations[]
```

### Tool event fields

```text
call_id
name
status
duration_ms
input_bytes
output_bytes
input_token_estimate
output_token_estimate
raw_output_bytes
reduced_output_bytes
raw_output_token_estimate
reduced_output_token_estimate
reduction_ratio
cas_digest
error
```

### Leakage flags

```text
filesystem_enabled
network_enabled
repo_search_enabled
memory_enabled
vault_enabled
shell_enabled
verifier_accessible_during_solve
dataset_accessible_during_solve
answer_accessible_during_solve
external_io_observed
forbidden_tool_names[]
```

## Implementation Plan

### Phase 1 - Baseline-only LongCoT harness

- Add `internal/tooling/evals/longcoteval` package.
- Add `cmd/foxctl/cmd/eval_longcot.go`.
- Add `foxctl eval longcot --condition baseline_no_tools_official_prompt`.
- Use official Python package to load questions and verify responses.
- Use direct no-tool `LLMChatEngine` path.
- Write JSON + markdown + official-compatible JSONL.
- Record prompt/completion/total tokens and costs with current accounting.

### Phase 2 - Paired scaffold/tool conditions

- Add `foxctl_no_tools_scaffold` and `foxctl_tools_minimal` conditions.
- Ensure one fresh session per question per condition.
- Add strict tool allowlist and leakage metadata.
- Forbid memory/session/vault/repo/shell/subagents initially.
- Pair results by `question_id` and summarize win/loss/tie, accuracy delta, token delta, cost delta, and wrong-formatting delta.

### Phase 3 - Token accounting instrumentation

- Extend OpenAI-compatible response usage in `internal/runtime/engine/llmchat_engine.go` to preserve raw `usage` and parse provider-specific details like reasoning tokens and cached input tokens.
- Extend `internal/runtime/engine/engine.go` `TokenUsage` / `IterationUsage` or add a richer benchmark usage layer for reasoning/cached tokens.
- Add generic per-tool measurement in `internal/runtime/engine/tool_runner.go` for input/output bytes, estimated tokens, duration, and CAS/result sizes.
- Generalize `shellreduce.Measure` concepts beyond shell reducers.
- Add tool-schema/system/user prompt token estimates for both baseline and tool conditions.

### Phase 4 - v2/Jido alignment

- Add token/cost fields to v2 `IterationRecord` / `ToolCallRecord` or companion telemetry records.
- Ensure v2 `tool.invoked`, `tool.responded`, and `turn.recorded` events can reconstruct LongCoT tool traces.
- Keep classic and v2 result schemas compatible.

## Eliminated / Rejected Approaches

### Reusing `promptEvalCase` as-is

Rejected as the primary schema. It is useful for JSONL loading patterns, but LongCoT needs question metadata, official verifier status, reasoning text/artifacts, condition metadata, leakage flags, and paired A/B fields.

### Using foxctl prompt judge for correctness

Rejected. LongCoT has an official verifier; prompt-judge quality would undermine validity.

### Using code-search or retrieval eval scores

Rejected for primary LongCoT correctness. `path_recall`, `symbol_recall`, `snippet_recall`, and `fact_recall` are repo-grounding metrics, not LongCoT answer verification.

### Exposing shell/repo/memory tools in the first tool condition

Rejected for MVP. These tools create leakage and accounting ambiguity. Shell may be valuable later for controlled simulation, but only after sandboxing and leakage checks are enforced.

## Risks and Mitigations

### Benchmark comparability

Risk: tool-augmented results could be mistaken for official LongCoT primary-setting results.

Mitigation: Label reports as `internal paired A/B, scaffolded/tool-augmented, not leaderboard comparable`.

### Dataset/verifier leakage

Risk: filesystem, shell, memory, context, or network tools can access benchmark answers or verifier behavior.

Mitigation: Separate solver workspace from dataset/verifier process; disable broad tools; record leakage flags; quarantine attempts using forbidden tools.

### Token-accounting apples-to-oranges

Risk: tools can shift work from model reasoning tokens to external CPU/tool execution.

Mitigation: Report billed LLM tokens, reasoning tokens when available, tool execution counts/duration, raw/reduced tool output estimates, and cost per correct answer separately.

### Provider usage inconsistency

Risk: providers differ in whether reasoning tokens are exposed or included in completion tokens.

Mitigation: Store raw provider usage and mark `reasoning_tokens_available` before comparing reasoning tokens.

### State carryover

Risk: session history/memory from prior questions contaminates later answers.

Mitigation: one fresh session per question per condition; no cross-question memory or conversation history.

## Preventive Measures

- Add tests that the solver process cannot read dataset, answers, or verifier files.
- Add golden tests for LongCoT result JSON schema and paired summary aggregation.
- Add a regression test that a tool-augmented run with forbidden tool usage is quarantined or marked leaked.
- Add docs warning that tool-augmented LongCoT runs are internal ablations only.
- Store raw provider usage for future normalization rather than only flattened token counts.

## RLM-Focused Addendum

The primary foxctl condition should be framed as **LongCoT × RLM**, not generic tool use. In this repo, RLM already has retrieval-eval integration and enough metadata to make this the natural next benchmark target.

### Existing RLM Eval Surfaces

RLM modes are already exposed through the retrieval eval command:

- `cmd/foxctl/cmd/eval.go:721-731` adds `rlm_llm`, `rlm_llm_codeintel`, and `rlm_llm_code_staged` retrieval modes.
- `cmd/foxctl/cmd/eval.go:1064-1110` builds an RLM task, bootstraps environment state, filters tools by profile, constructs a read-only adapter, and runs the LLM-backed RLM runner.
- `cmd/foxctl/cmd/eval.go:1112-1149` does the same with staged code-retrieval routing.

This means a LongCoT harness should not start with the generic `agentruntime.NewRuntime(...)` path. It should add a first-class RLM runner condition that calls the same lower-level RLM primitives but changes the task prompt, tools, and scoring target from repo path retrieval to LongCoT answer verification.

### RLM Runtime Properties Relevant to LongCoT

RLM's first-version contract is read-only:

- `internal/rlm/runner.go:20-36` validates and delegates bounded RLM runs.
- `internal/rlm/runner.go:53-69` rejects non-read-only tools in the environment.
- `internal/rlm/types.go:8-17` defines `Task` with prompt, role, workspace, max depth, max iterations, and max subcalls.
- `internal/rlm/types.go:19-29` defines read-only tool handles.
- `internal/rlm/types.go:31-40` defines the environment visible to RLM: top-of-mind, handoff, thread IDs, scene handles, artifact handles, repo handles, vault handles, and tools.
- `internal/rlm/types.go:48-55` defines final result fields: answer, evidence refs, retrieved paths, iterations, subcalls, trajectory ID, and metadata.

For LongCoT, `Result.Answer` maps to official `response_text`, while `Result.Metadata` should carry provider/tool/token details.

### RLM LLM Runner Behavior

- `internal/rlm/llm_runner.go:70-98` runs either single-pass or staged RLM depending on route profile and plan mode.
- `internal/rlm/llm_runner.go:113-166` configures `engine.LLMChatEngine`, attaches the RLM tool executor, and runs the model with RLM tools.
- `internal/rlm/llm_runner.go:167-206` returns answer, evidence refs, retrieved paths, tool count, tool names, parent input/output/total tokens, parent iteration count, and parent tool usage metadata.
- `internal/rlm/llm_runner.go:260-379` implements staged mode, splitting the run into phases, requiring tools in specific phases, aggregating phase metadata, and emitting total parent-token and `code_search_ensemble` usage estimates.
- `internal/rlm/llm_runner.go:534-578` estimates prompt delta and result-token impact for a target tool, currently focused on `code_search_ensemble`.

For LongCoT, this implies three RLM conditions are worth testing:

1. `rlm_single_default`
   - Single-pass RLM with default profile.
   - Best measure of current “agent setup” behavior.

2. `rlm_single_minimal`
   - Single-pass RLM with a new LongCoT-safe tool profile.
   - Best leakage-controlled tool/scaffold arm.

3. `rlm_staged_minimal`
   - Staged RLM with explicit phases such as understand → solve/check → final-answer.
   - Best test of whether RLM structure improves accuracy per token.

### RLM Tool Profiles Need a LongCoT-Safe Profile

Current profiles are repo-oriented:

- `internal/rlm/env/tool_profiles.go:9-40` defines `default` and `code-intel` only.
- `code-intel` allows `semantic_search_code`, `smart_search_code`, `ripgrep_code`, `code_search_ensemble`, `load_file`, `search_vault`, `read_note`, and `subcall`.

That is not appropriate for LongCoT because it risks dataset/verifier leakage and measures repo retrieval rather than reasoning. Add:

```go
const ToolProfileLongCoTMinimal = "longcot-minimal"
const ToolProfileLongCoTRLM = "longcot-rlm"
```

Initial `longcot-minimal` should probably expose **no external state tools**. If we want to test RLM structure first, use the RLM planner/system prompt and bounded iterations but no tool access.

A later `longcot-rlm` profile can include only deterministic, non-leaky tools, for example:

```text
scratchpad / think
calculator
chess-state validator, if built without answer access
bounded code runner for toy algorithms, if sandboxed
```

Avoid in LongCoT RLM conditions:

```text
search_repo
semantic_search_code
smart_search_code
ripgrep_code
code_search_ensemble
load_file
search_vault
read_note
search_artifacts
load_artifact
memory_ensemble_retrieve
search_scenes
get_scene
subcall
```

unless the experiment is explicitly labeled as contaminated/full-agent mode.

### RLM Adapter Telemetry We Can Reuse

RLM already has useful telemetry collection:

- `internal/rlm/env/adapter.go:61-130` records per-tool call counts, durations, models, input/output/total tokens, and total cost.
- `internal/rlm/env/adapter.go:133-169` centralizes RLM tool dispatch.
- `internal/rlm/env/adapter.go:920-935` records token usage from skill envelopes after direct skill execution.
- `internal/rlm/env/adapter.go:1013-1089` extracts token usage from `token_usage`, `usage`, `summary.tokens_used`, or `stats` fields.
- `internal/rlm/env/code_search_ensemble.go:945-999` attaches `metadata.telemetry` including loaded/emitted chars, token estimates, parent input token savings estimate, and compaction ratio.

For LongCoT, reuse the telemetry structure but generalize it so it can report all RLM tools, not just `code_search_ensemble`, and preserve raw provider usage from the parent LLM call.

### RLM-Specific LongCoT Result Fields

Add these fields to LongCoT attempts when `condition_kind == "rlm"`:

```text
rlm_route_profile
rlm_plan_mode
rlm_tool_profile
rlm_max_depth
rlm_max_iterations
rlm_max_subcalls
rlm_iterations
rlm_subcalls
rlm_evidence_refs
rlm_retrieved_paths
rlm_metadata
rlm_phase_count
rlm_phases[]
rlm_parent_input_tokens
rlm_parent_output_tokens
rlm_parent_total_tokens
rlm_parent_iteration_count
rlm_parent_tool_usage
rlm_tool_usage
rlm_tool_duration_ms
rlm_loaded_token_estimate
rlm_emitted_token_estimate
rlm_compaction_ratio
```

For staged mode, preserve each phase:

```text
phase.name
phase.allowed_tools
phase.required_tools
phase.tool_names
phase.parent_input_tokens
phase.parent_output_tokens
phase.parent_total_tokens
phase.answer_excerpt
```

### Recommended LongCoT × RLM Eval Matrix

Start with a small balanced subset, for example 10-25 questions per domain/difficulty:

| Condition | Purpose | Tools |
|---|---|---|
| `baseline_no_tools_official_prompt` | Official-style baseline | none |
| `rlm_no_tools_single` | Measures RLM prompt/agent setup without tools | none |
| `rlm_no_tools_staged` | Measures staged RLM structure without tools | none |
| `rlm_minimal_tools_single` | Measures safe deterministic tools | `longcot-minimal` |
| `rlm_minimal_tools_staged` | Measures staged RLM + safe tools | `longcot-minimal` |
| `rlm_full_repo_agent_contaminated` | Optional internal stress mode only | current `default` / `code-intel` |

Primary comparisons:

```text
baseline_no_tools_official_prompt vs rlm_no_tools_single
rlm_no_tools_single vs rlm_no_tools_staged
rlm_no_tools_staged vs rlm_minimal_tools_staged
```

This isolates:

1. RLM scaffold effect.
2. RLM staged planning effect.
3. Tool effect.

### Key RLM Risk

The current RLM environment bootstraps repo/vault/artifact/thread handles and has powerful read-only tools. That is good for foxctl repository work, but unsafe for LongCoT unless isolated. For credible LongCoT results:

- do not mount the dataset/verifier/answer files into the RLM workspace;
- disable memory, repo, vault, artifact, scene, and subcall tools for primary conditions;
- use a fresh environment per question;
- record every exposed tool name in the result;
- quarantine any attempt using a forbidden tool.

## Recommended First PR Boundary

The smallest valuable PR should include:

1. `internal/tooling/evals/longcoteval` result/summary schemas and unit tests.
2. `foxctl eval longcot --dry-run --max-questions N` loading questions through the official Python bridge.
3. Baseline no-tool runner that writes official-compatible JSONL.
4. RLM no-tool single-pass and staged runners that use `internal/rlm.LLMRunner` with an empty/LongCoT-safe environment.
5. Verification bridge that imports official verifier results.
6. JSON/markdown report with accuracy, total tokens, cost, duration, wrong-formatting, RLM iterations, RLM subcalls, RLM plan mode, and RLM tool profile.

Do **not** add RLM tool-augmented claims until leakage controls and richer token accounting are in place.
