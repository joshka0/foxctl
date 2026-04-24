# LongCoT × RLM Evaluation Plan

**Status:** Active plan  
**Created:** 2026-04-19  
**Related:** [LongCoT eval investigation](../../research/longcot-eval-investigation.md), [RLM next steps](foxctl-rlm-next-steps.md), [RLM retrieval findings](rlm-retrieval-findings.md), [RLM integration outline](foxctl-rlm-integration-outline.md), [RLM query runtime spec](../../spec/rlm_query_runtime.md)

## Summary

Build a LongCoT-backed internal eval that measures foxctl's RLM agent setup against an official-style no-tool baseline. The primary goal is not leaderboard comparison; it is a paired A/B harness that answers whether RLM scaffolding, staged planning, and later safe deterministic tools improve verifiable long-horizon reasoning accuracy per token.

The first implementation slice is intentionally low-risk: add explicit LongCoT-safe RLM profiles, pure eval schemas/summaries, and docs. Real LongCoT dataset loading, verifier process execution, and live LLM runs come later behind typed interfaces and leakage controls.

## Non-Goals

- Do not report RLM/tool runs as official LongCoT primary-setting results.
- Do not expose repo, vault, memory, artifact, file, shell, or subcall tools in primary LongCoT conditions.
- Do not add keyword-based route detection for LongCoT. Conditions must select route/profile/plan mode explicitly.
- Do not require Python, network, LongCoT downloads, or live LLM credentials in unit tests.

## Existing Code to Reuse

| Area | Existing files | Reuse |
|---|---|---|
| Eval command patterns | `cmd/foxctl/cmd/eval.go` | subcommand registration, report/save flow |
| RLM eval wiring | `cmd/foxctl/cmd/eval.go`, `cmd/foxctl/cmd/rlm.go` | RLM task/env/bootstrap/runner patterns |
| RLM runtime | `internal/rlm/{types,runner,llm_runner,plan}.go` | task/result contracts, single-pass/staged execution |
| RLM tools | `internal/rlm/env/{tools,tool_profiles,adapter}.go` | read-only tool boundaries and telemetry |
| Eval package pattern | `internal/tooling/evals/retrievaleval/*` | pure result/summary/markdown package shape |
| Experiment artifacts | `internal/tooling/evals/transcriptmemoryeval/experiment.go` | saved outputs and append-only records |
| Research basis | `docs/research/longcot-eval-investigation.md` | constraints, risks, phase outline |

## Evaluation Matrix

Start with a small balanced subset, then expand.

| Condition | Purpose | Tools | Leaderboard comparable? |
|---|---|---|---|
| `baseline_no_tools_official_prompt` | Official-style no-tool baseline | none | closest internal approximation |
| `rlm_no_tools_single` | RLM system prompt / agent setup without tools | none | no |
| `rlm_no_tools_staged` | RLM staged planning without tools | none | no |
| `rlm_no_model_tools_single` | Later safe deterministic tools | `longcot-no-model-tools` | no |
| `rlm_no_model_tools_staged` | Later staged RLM + safe tools | `longcot-no-model-tools` | no |
| `rlm_full_repo_agent_contaminated` | Optional internal stress mode only | current `default` / `code-intel` | no; excluded from primary summaries |

Primary comparisons:

1. `baseline_no_tools_official_prompt` vs `rlm_no_tools_single`
2. `rlm_no_tools_single` vs `rlm_no_tools_staged`
3. `rlm_no_tools_staged` vs `rlm_no_model_tools_staged`

This isolates scaffold effect, staged-planning effect, and safe-tool effect.

## Milestone 1 — Safety Rails and Typed Contracts

### Story 1.1 — Active plan and docs index

- Add this plan under `docs/plans/features/`.
- Link it from `docs/plans/README.md`.
- Keep the investigation report as research context, not the implementation source of truth.

Validation:

```bash
make check-doc-links
```

### Story 1.2 — LongCoT-safe RLM profiles

Add RLM profile constants:

```go
ToolProfileLongCoTNoModelTools = "longcot-no-model-tools"
ToolProfileLongCoTNoModelTools     = "longcot-no-model-tools"
```

Initial behavior: both return an empty tool list.

Rationale: current RLM tools are read-only but still too powerful for primary LongCoT conditions because they can expose repo/vault/memory/artifact/file/subcall state.

Validation:

```bash
go test ./internal/rlm/env
```

### Story 1.3 — Pure LongCoT eval package

Add `internal/tooling/evals/longcoteval` with no LLM, no Python, no network, and no DB dependency.

Initial files:

```text
types.go
summary.go
leakage.go
markdown.go
bridge.go
*_test.go
```

Core contracts:

- `Question`
- `Condition`
- `Attempt`
- `Usage`
- `ToolEvent`
- `LeakageFlags`
- `RLMAttemptMeta`
- `RunResult`
- paired `Summary`
- official-compatible `OfficialResponse`
- bridge interfaces for `QuestionLoader` and `Verifier`

Validation:

```bash
go test ./internal/tooling/evals/longcoteval
```

## Milestone 2 — Dry-Run CLI Shell

Add `cmd/foxctl/cmd/eval_longcot.go` and register it from `newEvalCommand`.

Initial behavior:

- `foxctl eval longcot --dry-run`
- load local fixture questions only
- normalize requested conditions
- compute leakage flags from exposed tools
- emit planned run config and selected question IDs
- no live LLM calls
- no Python bridge process execution

Recommended flags:

```text
--domain
--difficulty
--split
--limit
--seed
--condition
--provider
--model
--base-url
--api-key
--timeout
--max-tokens
--temperature
--output-dir
--save
--python
--longcot-dataset
--dry-run
--format
```

Default conditions:

```text
baseline_no_tools_official_prompt,rlm_no_tools_single
```

Validation:

```bash
go test ./cmd/foxctl/cmd
```

## Milestone 3 — Official-Compatible Baseline and Verifier Bridge

Add process-backed but optional LongCoT bridge support.

Flow:

1. Load questions through an official LongCoT Python bridge.
2. Run baseline no-tool condition with a direct no-tool `engine.LLMChatEngine`.
3. Write official-compatible JSONL per condition.
4. Run official verifier only after solve attempts complete.
5. Join verifier rows by `question_id`.
6. Render JSON and markdown reports.

Rules:

- The verifier process is never visible to solver conditions.
- Dataset, answers, and verifier files are not mounted into the RLM workspace.
- Missing verifier rows mark attempts as `unverified`, not incorrect.
- Duplicate question IDs or verifier rows fail early.

## Milestone 4 — RLM No-Tool Conditions

### Single-pass RLM

Run `rlm.LLMRunner` with:

- empty environment handles
- empty tool list
- `ToolProfileLongCoTNoModelTools`
- `RequireToolUse=false`
- `PlanModeFree`
- one fresh runner/session per attempt

Record:

- RLM route profile
- plan mode
- tool profile
- iterations
- subcalls
- parent input/output/total tokens
- RLM metadata
- leakage flags

### Staged RLM

Add explicit staged LongCoT behavior. Preferred design if shared by RLM:

```text
RouteProfileLongCoTReasoning
```

Phases:

```text
understand -> solve -> check -> final
```

Rules:

- selected explicitly by condition
- no keyword classifier
- no tools in primary no-tool staged condition
- phase metadata persisted in attempt result

## Milestone 5 — Safe Deterministic Tools

Only after no-tool RLM conditions are stable:

- add deterministic non-leaky tools, e.g. calculator or bounded symbolic checker
- add strict sandbox tests
- expose them through `longcot-no-model-tools`
- keep repo/vault/memory/artifact/file/subcall tools out of primary reports

## Leakage Policy

Primary LongCoT conditions are leaked if any of these are exposed during solve:

```text
get_top_of_mind
get_latest_handoff
search_scenes
get_scene
search_artifacts
load_artifact
semantic_search_code
smart_search_code
ripgrep_code
search_repo
expand_repo_graph
load_file
search_vault
read_note
memory_ensemble_retrieve
code_search_ensemble
subcall
shell
structured_shell
```

Use exact tool-name matching and explicit config flags, not prompt keyword heuristics.

## Output Artifacts

Default directory:

```text
<workspace>/.foxctl/exports/evals/longcot/<run_id>/
```

Files:

```text
result.json
report.md
attempts.ndjson
responses.<condition_id>.jsonl
verify.<condition_id>.json
config.json
```

## First Slice Checklist

- [x] Active plan doc created.
- [x] Docs index links to plan.
- [x] LongCoT-safe RLM profiles exist and return no tools.
- [x] Pure `longcoteval` schema/summary/leakage/markdown scaffolding exists.
- [x] Unit tests cover profiles, paired summaries, leakage, markdown, and official-response conversion.
- [x] Dry-run CLI shell.
- [ ] Official bridge fixtures.
- [x] Baseline no-tool runner.
- [x] RLM no-tool runner.
- [ ] Staged LongCoT RLM route.

## Validation Commands

For the first slice:

```bash
go test ./internal/rlm/env ./internal/tooling/evals/longcoteval
make check-doc-links
```
