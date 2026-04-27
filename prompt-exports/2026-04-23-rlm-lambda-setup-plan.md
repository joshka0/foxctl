# ChatGPT Prompt Export

## Prompt Type
Plan

## Task
Review the RLM and lambda-RLM setup across foxctl: how it is defined, configured, bootstrapped, routed, and used from CLI/eval paths; identify mismatches, integration seams, and concrete risk areas for follow-up review.

## Architecture
- `internal/rlm/*`: experimental read-only RLM runtime contract and executors.
- `internal/rlm/env/*`: environment bootstrap + read-only tool adapter + tool/profile catalogs.
- `cmd/foxctl/cmd/rlm.go`: user-facing `foxctl rlm run` entrypoint and runner selection (`inspect` vs `llm` vs lambda plan mode path).
- `cmd/foxctl/cmd/eval*.go` (sliced): eval harnesses that reuse the same bootstrap/adapter/runner wiring.
- `internal/runtime/engine/llmchat_engine.go` (sliced): provider/baseURL/API key detection and defaults used by LLM-backed runners.
- `internal/runtime/engine/rlm_tools.go` (sliced): separate engine-level “RLM context tools” subsystem (contextvar/personality) with overlapping naming.
- `docs/spec` + `docs/plans` + `docs/general`: intended model vs current implementation narrative.

## Selected Context
foxctl/cmd/foxctl/cmd/rlm.go: CLI bootstrap pipeline (`task -> bootstrapper.Build -> FilterTools -> ReadOnlyAdapter -> chooseRLMRunner -> recursive subcall callback`), flags for tool profile/route profile/plan mode, lambda selection branch, and RLM-specific env var overrides.
foxctl/cmd/foxctl/cmd/rlm_test.go: Ensures run command bootstraps environment and returns non-empty tool surface/answer.
foxctl/cmd/foxctl/cmd/eval.go (slice): `runRLMEvalMode` and `runRLMStagedEvalMode` wiring mirrors CLI but fixes runner options for eval variants.
foxctl/cmd/foxctl/cmd/eval_code_search_ensemble.go (slice): direct adapter execution path for `code_search_ensemble` eval cases.

foxctl/internal/rlm/types.go: Task/Environment/Result contracts.
foxctl/internal/rlm/interfaces.go: Runner/Sandbox/Bootstrapper interfaces.
foxctl/internal/rlm/runner.go + runner_test.go: base validation and read-only safety constraints.
foxctl/internal/rlm/inspect_runner.go + inspect_runner_test.go: deterministic inspection executor and optional subcall behavior.
foxctl/internal/rlm/plan.go + plan_test.go: route profiles + plan modes (`free/guided/staged/hard/lambda`) and staged code-retrieval phases.
foxctl/internal/rlm/llm_runner.go + llm_runner_test.go: LLM tool-loop executor, staged phase orchestration, tool constraints, retrieved path extraction, parent tool telemetry metadata.
foxctl/internal/rlm/lambda.go + lambda_classify.go + lambda_runner.go + lambda_test.go: lambda task typing, analytical `PlanLambda`, deterministic split/map/reduce flow, LLM usage points (classify/leaf judge/synthesize), metadata emission.

foxctl/internal/rlm/env/bootstrap.go + bootstrap_test.go: environment materialization from ACA top-of-mind/handoff + repoindex + vault + optional companion DB + trajectory handles.
foxctl/internal/rlm/env/tools.go + tools_test.go: typed read-only tool schema catalog (including `memory_ensemble_retrieve`, `code_search_ensemble`, `subcall`).
foxctl/internal/rlm/env/tool_profiles.go + tool_profiles_test.go: profile filtering (`default`, `code-intel`, `longcot-minimal`, `longcot-rlm`).
foxctl/internal/rlm/env/scout_roles.go: role normalization/prompt decoration/tool narrowing for memory scouts.
foxctl/internal/rlm/env/memory_ensemble.go: multi-scout subcall strategy and aggregation for memory recall lane.
foxctl/internal/rlm/env/adapter.go: concrete tool dispatch + skill invocation bridge + subcall tool + telemetry accumulation.
foxctl/internal/rlm/env/adapter_test.go (slices): targeted adapter tests for subcall/memory/code_search behavior.
foxctl/internal/rlm/env/code_search_ensemble.go (slices): main ensemble orchestration and separate LLM planner/replanner/selector integration.
foxctl/internal/rlm/env/code_search_ensemble_test.go (slices): probe/scoring/bridge extraction expectations.

foxctl/internal/runtime/engine/llmchat_engine.go (slices): default provider/auth/base URL resolution and LM Studio fallback behavior consumed indirectly by RLM runners.
foxctl/internal/runtime/engine/rlm_tools.go (slice): distinct “RLM context tools” executor and tool defs for contextvar memory/personality.
foxctl/internal/platform/config/config.go (slices): config shape + LMStudio/baseURL defaults + storage/CAS root defaults and normalization used by bootstrap/adapter.
foxctl/internal/context/companion/doc.go: companion package role naming context.

foxctl/docs/spec/rlm_query_runtime.md: runtime spec and intended contract.
foxctl/docs/plans/features/foxctl-rlm-next-steps.md: staged-route roadmap and implementation map.
foxctl/docs/plans/features/longcot-rlm-evaluation-plan.md: longcot-safe profile expectations and phased eval architecture.
foxctl/docs/general/rlm-context.md: contextvar/tool-based “RLM context system” narrative in engine/companion domain.

## Relationships
- `cmd/foxctl/cmd/rlm.go::newRLMRunCommand` -> `rlmenv.NewBootstrapper.Build` -> `rlmenv.FilterTools` -> `rlmenv.NewReadOnlyAdapter` -> `chooseRLMRunner` -> (`rlm.LLMRunner` or `rlm.LambdaRunner` or `rlm.InspectRunner`).
- `chooseRLMRunner` selects lambda path only when normalized plan mode is `lambda`; lambda config reuses LLM config fields but runs its own deterministic recursion.
- `rlm.LLMRunner` depends on `BuildPlan` and `route/profile/mode` to switch between single-pass vs staged execution.
- `rlm.LambdaRunner` depends on `classifyTask` + `estimateProblemSize` + `PlanLambda`; leaf execution routes through env adapter tools (`code_search_ensemble` or `memory_ensemble_retrieve`) and `load_file`.
- `env.Bootstrapper.Build` supplies handles/context used by all runner modes; insufficient/empty handles alter lambda `n` estimate and staged context.
- `env.ReadOnlyAdapter.Execute` is the runtime tool substrate for both CLI/eval RLM and lambda leaf calls; it bridges into skill binaries and repo/vault/companion stores.
- `env.FilterTools(profile)` constrains tool exposure before runner execution; longcot profiles intentionally return empty tools.
- `cmd/foxctl/cmd/eval.go` RLM eval modes reuse same recursive adapter+runner callback pattern, making CLI/runtime issues reproducible in eval paths.
- `internal/runtime/engine/llmchat_engine` provides provider/baseURL/auth defaults that affect all LLM calls in `llm_runner` and lambda classify/judge/synthesize.
- `internal/runtime/engine/rlm_tools` is a different subsystem (“rlm_context_*” tools) that can be conflated with `internal/rlm` due shared naming.

## Ambiguities
- Naming overlap: two distinct “RLM” systems coexist.
  - `internal/rlm/*` = query-time recursive runtime with env tools.
  - `internal/runtime/engine/rlm_tools.go` + `docs/general/rlm-context.md` = contextvar/personality stateless context tools.
- Documentation status mismatch:
  - `docs/spec/rlm_query_runtime.md` says `Status: proposed`.
  - implementation is substantial in `internal/rlm/*`, CLI/eval wiring, and tests.
- Profile/plan semantics overlap:
  - `PlanModeLambda` is runner-level behavior in `cmd/.../rlm.go` + `internal/rlm/lambda*`.
  - LongCoT profiles in `env/tool_profiles.go` are tool-surface constraints and currently intentionally empty for two profiles.
- LLM config source split:
  - `cmd/.../rlm.go` injects `FOXCTL_RLM_LLM_*`/`LMSTUDIO_*` overrides.
  - `engine/llmchat_engine.go` also auto-detects provider/baseURL/model via generic env/config defaults.
  This creates multiple fallback layers to audit for precedence consistency.
- `code_search_ensemble.go` is very large and only key orchestration + planner/replanner/selector sections are selected; some deep helper branches are intentionally omitted for budget.

## Generated Plan
# 1. Summary

The RLM stack is already materially implemented, not just scaffolded: `cmd/foxctl/cmd/rlm.go` and the eval paths bootstrap `internal/rlm/env` state, bind a `ReadOnlyAdapter`, and dispatch to `InspectRunner`, `LLMRunner`, or `LambdaRunner`. The main issues are not missing primitives; they are integration mismatches: runner/mode flags that silently degrade, tool-profile filtering that is only enforced for `LLMRunner` but bypassed by `InspectRunner` and `LambdaRunner`, split LLM-config resolution between CLI/env/config/engine defaults, and documentation that conflates the query-time RLM runtime with the separate `rlm_context_*` context-tools subsystem. This should be addressed as a targeted hardening pass, not a broader refactor.

# 2. Current-state analysis

## Runtime ownership and contracts

### `internal/rlm/*`
- `types.go` defines the core contracts:
  - `Task`
  - `Environment`
  - `Result`
- `interfaces.go` defines:
  - `Runner`
  - `Bootstrapper`
  - `Sandbox`
- `runner.go` only enforces:
  - prompt required
  - non-negative depth/iteration/subcall budgets
  - all declared `env.Tools` must be read-only

This contract layer is reusable and already appropriately narrow.

## Environment bootstrap and tool substrate

### `internal/rlm/env/bootstrap.go`
`Bootstrapper.Build` owns environment materialization:

`Task.WorkspaceRoot`
→ ACA top-of-mind JSON
→ latest handoff JSON
→ companion DB scene/thread handles
→ repoindex projected handles
→ Obsidian note handles
→ trajectory/artifact handles
→ `rlm.Environment{..., Tools: DefaultTools()}`

Important behavior:
- missing workspace root returns a tool-bearing but mostly empty environment
- `loadRepoHandles`, `loadVaultHandles`, and `loadTrajectoryHandles` swallow many store/index errors and return `nil, nil`
- this makes bootstrap resilient, but it also hides degraded mode from callers

### `internal/rlm/env/tools.go`
`DefaultTools()` is the authoritative catalog of exposed RLM env tools. It includes:
- raw state readers (`get_top_of_mind`, `get_latest_handoff`)
- store/search tools (`search_repo`, `search_vault`, `search_scenes`, `search_artifacts`)
- bounded readers (`load_file`, `read_note`, `load_artifact`, `get_scene`)
- composite tools (`memory_ensemble_retrieve`, `code_search_ensemble`)
- recursion (`subcall`)

### `internal/rlm/env/adapter.go`
`ReadOnlyAdapter.Execute` is the concrete execution boundary for every tool name. It owns:
- direct store/index access
- skill execution fallback (`runCurrentSkillDecode`)
- subcall dispatch (`subcallTool`)
- per-run telemetry accumulation (`adapterTelemetry`)

This is the actual mutable execution surface. `env.Tools` is only declarative unless a runner enforces it.

## Runner behavior

### CLI path
`cmd/foxctl/cmd/rlm.go::newRLMRunCommand`
builds:

`flags`
→ `rlm.Task`
→ `openRLMCompanionDB`
→ `rlmenv.NewBootstrapper(...).Build`
→ `rlmenv.FilterTools(env.Tools, toolProfile)`
→ recursive callback:
  - `applyRLMScoutRole`
  - `rlmenv.NewReadOnlyAdapter`
  - `adapter.SetSubcall(runRecursive)`
  - `chooseRLMRunner(...)`
  - `runner.Run(...)`

### Eval path
`cmd/foxctl/cmd/eval.go`
reuses the same bootstrap/adapter/subcall wiring in:
- `runRLMEvalMode`
- `runRLMStagedEvalMode`

The direct `code_search_ensemble` eval in `eval_code_search_ensemble.go` bypasses the runner layer and calls `adapter.Execute("code_search_ensemble", ...)` directly.

## Runner-specific execution flow

### `InspectRunner`
`internal/rlm/inspect_runner.go`
- deterministic
- directly calls tool names like `search_repo`, `search_vault`, `search_scenes`, `subcall`
- decides whether to recurse based on handle counts and budgets

**Mismatch:** it does not consult `env.Tools` before calling those tool names.

### `LLMRunner`
`internal/rlm/llm_runner.go`
- builds a `Plan` using `BuildPlan`
- if `PlanModeStaged` and `len(plan.Phases) > 0`, runs staged phases
- otherwise runs single-pass tool loop
- wraps the env adapter with `NewLLMToolExecutor`, then `engine.NewToolRunner(...)`
- exposes only `pass.Tools` to the model

**This is the only runner that actually honors filtered tool visibility.**

### `LambdaRunner`
`internal/rlm/lambda_runner.go`
- classifies task with one LLM call (`classifyTask`)
- estimates problem size (`estimateProblemSize`)
- computes `PlanLambda`
- executes deterministic split/map/reduce
- leaf calls use:
  - `SearchToolForTask(...)`
  - then direct `Tools.Execute(searchTool, ...)`
  - then direct `Tools.Execute("load_file", ...)`

**Mismatch:** it also bypasses `env.Tools` and can call tools that the profile removed.

## Mode/profile/config seams

## Highest-risk mismatches

| Area | Current behavior | Risk |
|---|---|---|
| Tool profiles | `FilterTools` only edits `env.Tools` | `InspectRunner` and `LambdaRunner` still access hidden tools through `adapter.Execute` |
| Empty-tool profiles | LongCoT profiles return `[]` | CLI default `--require-tool-use=true` makes no-tool LLM runs impossible |
| Plan modes | `guided`/`hard` parse but have no runtime behavior | user-visible flags are silent no-ops |
| Staged routing | only `code_retrieval` has phases | `memory_recall`/others silently fall back to single-pass |
| Lambda selection | `PlanModeLambda` only works inside `executor=llm` branch | invalid combinations degrade unclearly |
| LLM config | CLI uses `FOXCTL_RLM_LLM_*`/`LMSTUDIO_*`; internal search tools use `cfg.LLM.Resolve*`; engine has its own auto-detect | precedence is inconsistent across runner vs tool-internal LLM calls |
| Docs | `docs/spec/rlm_query_runtime.md` says `Status: proposed`; `docs/general/rlm-context.md` describes a different “RLM” subsystem | reviewers and implementers can easily read the wrong architecture |

## Separate “RLM” subsystem with overlapping name

`internal/runtime/engine/rlm_tools.go` is not the same system as `internal/rlm/*`.

- `internal/rlm/*` = query-time recursive runtime over repo/vault/scenes/artifacts
- `engine/rlm_tools.go` = `rlm_context_*` contextvar/personality tools used by `LLMChatEngine` stateless context mode

The naming overlap is a documentation and review hazard, not a code-level dependency.

## Hard constraints already present

- `rlm.ValidateEnvironment` only validates declared tool metadata, not adapter dispatch
- `Result.TrajectoryID` exists but no runner currently persists trajectories
- bootstrap/store access is synchronous; no actor ownership or persistence migrations are involved here
- `LambdaRunner.executePhi` uses goroutines with shared `partials` guarded by `sync.Mutex`
- `adapterTelemetry` is mutex-protected and safe for concurrent updates inside a single adapter instance

# 3. Design

## A. Execution selection must become explicit and honest

### Decision
Keep the current architecture (`Task` + `Environment` + `ReadOnlyAdapter` + runner choice), but stop allowing unsupported executor/mode/profile combinations to silently degrade.

### Why this is the right scope
The contracts are already clean; the problem is policy enforcement at the call edge.

### Required behavior changes
Implement a single validation/resolution step, used by both CLI and eval wiring, with these rules:

- `executor=inspect`
  - only valid with `PlanModeFree`
- `executor=llm`
  - valid for `free`
  - valid for `staged` only when `BuildPlan(...)` yields concrete phases
  - reject `guided` and `hard` until they have distinct runtime behavior
  - valid for `lambda` only as the explicit lambda path
- `PlanModeStaged` with non-`code_retrieval` route
  - fail fast, do not silently run single-pass
- `requireToolUse=true` with `len(env.Tools)==0`
  - fail fast with a user-facing error

### Data flow
`flags/defaults` → normalize route/mode/profile → validate against effective tool set → choose runner → run

That replaces the current hidden downgrade chain:
`flags` → `BuildPlan` → `len(plan.Phases)==0` → silent single-pass

### Suggested helper shape
Keep this internal to command/runtime wiring; no new public API needed.

Illustrative shape:

```go
func validateRLMSelection(
    executor string,
    route rlm.RouteProfile,
    mode rlm.PlanMode,
    tools []rlm.Tool,
    requireToolUse bool,
) error
```

### Call sites to update
- `cmd/foxctl/cmd/rlm.go`
- `cmd/foxctl/cmd/eval.go`

### Error handling
Use user-facing errors, not metadata warnings, because the current behavior changes meaning, not just performance.

## B. Tool profiles must be enforced at execution time, not only in `env.Tools`

### Decision
Add a scoped `ToolExecutor` wrapper inside `internal/rlm` and use it in runners that directly invoke tools.

### Why
`env.Tools` is currently descriptive. `InspectRunner` and `LambdaRunner` bypass it by holding a raw adapter.

### New component
Add one small helper type, e.g. `internal/rlm/tool_scope.go`:

```go
type ScopedToolExecutor struct {
    base ToolExecutor
    allow map[string]struct{}
}
```

Behavior:
- build `allow` from `env.Tools`
- reject unknown/disallowed tool names with a deterministic error
- no internal state beyond immutable allowlist

### Ownership
Created inside runner `Run` methods; not persisted.

### Runners to update
- `internal/rlm/inspect_runner.go`
- `internal/rlm/lambda_runner.go`

`LLMRunner` does not need this for model calls because it already derives tool defs from `pass.Tools`, but it may still reuse the same wrapper if any future direct tool calls are added.

### Exact state flow after change
`env.Tools` (already filtered by profile/role)
→ build allowlist once at runner start
→ every direct tool call passes through allowlist
→ adapter dispatch only happens if allowed

### Error behavior
Disallowed direct tool use should fail as:
- `rlm: tool "search_repo" not allowed in current environment`

That makes tool profiles, scout-role narrowing, and LongCoT no-tool profiles real across all runners.

### Concrete fixes this enables
- `longcot-minimal` / `longcot-rlm` truly become empty tool surfaces
- `InspectRunner` no longer leaks repo/vault/scene tooling
- `LambdaRunner` can no longer call `code_search_ensemble` behind an empty profile

## C. LLM config resolution should be unified for runner-owned LLM calls

### Decision
Keep `engine.NewLLMChatEngine` as the low-level resolver, but unify how top-level RLM runners populate `rlm.LLMConfig`.

### Current mismatch
There are three different resolution regimes:
1. `cmd/foxctl/cmd/rlm.go` hard-codes `FOXCTL_RLM_LLM_*` / `LMSTUDIO_*`
2. `internal/rlm/env/code_search_ensemble.go` uses `cfg.LLM.Resolve*`
3. `engine/llmchat_engine.go` auto-detects provider/model/base URL itself

That means the top-level runner and the tool-internal planner/selector can use different providers/models unintentionally.

### Required behavior
For runner-owned LLM calls (`LLMRunner`, `LambdaRunner` classify/judge/synthesize), use this precedence:

1. explicit CLI flag
2. `FOXCTL_RLM_LLM_*` env override
3. `cfg.LLM.Resolve*`
4. engine fallback/detect only if still unset

### Suggested helper
Internal to `cmd/foxctl/cmd`:

```go
func resolveRunnerLLMConfig(
    cfg config.Config,
    providerFlag, modelFlag, baseURLFlag, apiKeyFlag string,
    timeout time.Duration,
) rlm.LLMConfig
```

### Call sites to update
- `cmd/foxctl/cmd/rlm.go::chooseRLMRunner`
- `cmd/foxctl/cmd/eval.go` RLM eval helpers

### Non-goal for this slice
Do not expand `rlm.LLMConfig` for `AuthMode/AuthHeader/AuthPrefix` unless validation shows real provider usage needs it. That is a separate compatibility follow-up.

## D. `LLMRunner` should reject unsupported mode semantics early

### Decision
Keep `BuildPlan` as the route/phase source of truth, but make `LLMRunner.Run` reject unsupported plan modes instead of silently degrading.

### Required behavior
- `PlanModeGuided`: reject until distinct logic exists
- `PlanModeHard`: reject until distinct logic exists
- `PlanModeStaged` + no phases: reject

### Before
```go
if plan.Mode == PlanModeStaged && len(plan.Phases) > 0 {
    return r.runStaged(...)
}
return r.runSinglePass(...)
```

### After
Semantics should be:
- staged with phases → staged
- staged without phases → error
- guided/hard without implementation → error
- free → single-pass

### Reason
Today the flags overstate runtime support.

### Related call sites
No external API change; internal behavior change only.

## E. Lambda needs enforcement and observability before algorithm changes

### Decision
Do not retune `PlanLambda` yet. First make lambda execution truthful and diagnosable.

### Why
The biggest lambda problems are integration-level:
- hidden tool access
- no CLI/eval coverage
- child branch failures are mostly swallowed
- `estimateProblemSize` is heuristic, but there is not enough runtime evidence yet to redesign it safely

### Immediate changes
1. enforce tool allowlist wrapper
2. emit structured metadata when:
   - primary search tool is unavailable
   - all child branches fail
   - `load_file` attempts all fail
3. keep per-child error summaries in metadata, bounded in size

Illustrative metadata additions:

```go
"lambda_unavailable_search_tool": "code_search_ensemble"
"lambda_child_error_count": 3
"lambda_child_errors": ["branch 1: tool not allowed", ...]
```

### Concurrency
Current `executePhi` fan-out is analytically bounded by `k*` and `depth`, but there is no run-level semaphore. Do not add one in this slice without evidence; instead add stress tests and metadata first.

### Separate review item
`estimateProblemSize` defaulting to `50` when the bootstrap produced no handles is a real risk area, but it should be tuned only after actual lambda traces exist.

## F. Documentation must separate the two RLM systems and match current code

### Decision
Do docs cleanup as part of the hardening pass because the naming overlap is already causing architectural ambiguity.

### Required changes

#### `docs/spec/rlm_query_runtime.md`
- change status from `proposed` to something like `experimental / implemented slice`
- explicitly note current gaps:
  - trajectory persistence not yet wired
  - staged routing only implemented for `code_retrieval`
  - lambda exists but is experimental

#### `docs/general/rlm-context.md`
- retitle or clearly subtitle it as the contextvar/stateless-context subsystem
- add a top-note: “This is not `internal/rlm`”

#### `docs/plans/features/foxctl-rlm-next-steps.md`
Align to actual `plan.go` behavior:
- discovery currently requires `code_search_ensemble`, not `semantic_search_code`/`smart_search_code`/`search_repo`
- inspection phase does not currently allow `expand_repo_graph`

#### `docs/plans/features/longcot-rlm-evaluation-plan.md`
Make the no-tool requirement operationally explicit:
- empty tool profiles require `RequireToolUse=false`
- lambda/inspect must respect the same tool profile gates

## G. Explicitly out-of-scope, but high-risk follow-up items

These are real mismatches, but they should be reviewed separately from the hardening pass above:

1. **Trajectory persistence gap**
   - `Result.TrajectoryID` is unused
   - spec says first version should guarantee trajectory persistence
   - no current CLI/eval path writes trajectories for RLM runs

2. **Installed-binary skill fallback risk**
   - `ReadOnlyAdapter.runCurrentSkillDecode` may fall back to `resolveFoxctlRepoRoot()`
   - that assumes source checkout structure (`skills/code_semantic_search/main.go`)
   - this is fragile outside development environments

3. **Tool/internal-LLM consistency**
   - `code_search_ensemble` planner/replanner/selector use `cfg.LLM.Resolve*`
   - top-level runners use separate resolution
   - after the runner-config cleanup, these may still intentionally differ, but it should be documented

## File-by-file impact

### `cmd/foxctl/cmd/rlm.go`
- **What changes**
  - add centralized validation for executor/route/plan/tool-use combinations
  - add unified runner LLM-config resolution helper using `config.Config`
  - fail early for empty-tool + `require-tool-use`
- **Why**
  - current CLI surface advertises combinations that silently degrade or cannot work
- **Dependencies**
  - depends on plan support rules from `internal/rlm/plan.go`
  - depends on tool enforcement behavior in runners

### `cmd/foxctl/cmd/eval.go`
- **What changes**
  - reuse the same selection/config validation used by CLI
  - keep eval semantics aligned with CLI semantics for LLM/staged RLM modes
- **Why**
  - current eval helpers mirror bootstrap/runner wiring and should not drift
- **Dependencies**
  - same helper logic as `cmd/foxctl/cmd/rlm.go`

### `internal/rlm/plan.go`
- **What changes**
  - add explicit support helpers for route/mode combinations, or equivalent plan-validity checks
  - keep `BuildPlan` as source of truth for which staged routes actually exist
- **Why**
  - staged/guided/hard support is currently overstated by the public flags
- **Dependencies**
  - used by `LLMRunner` and command validation

### `internal/rlm/llm_runner.go`
- **What changes**
  - reject unsupported `PlanModeGuided`, `PlanModeHard`, and staged-without-phases
  - reject `RequireToolUse` when `pass.Tools` is empty
- **Why**
  - remove silent behavior downgrades
- **Dependencies**
  - depends on plan support logic from `plan.go`

### `internal/rlm/inspect_runner.go`
- **What changes**
  - wrap `r.Tools` with a scoped allowlist derived from `env.Tools`
  - all direct tool calls (`search_repo`, `search_vault`, `search_scenes`, `subcall`) go through that scoped executor
- **Why**
  - profile filtering is currently bypassed
- **Dependencies**
  - depends on new scoped tool helper

### `internal/rlm/lambda_runner.go`
- **What changes**
  - wrap direct tool execution with scoped allowlist enforcement
  - add bounded branch-error metadata
  - emit explicit metadata when required tools are unavailable or all branches fail
- **Why**
  - lambda currently bypasses tool profiles and hides failure causes
- **Dependencies**
  - depends on new scoped tool helper

### `internal/rlm/tool_scope.go` (new)
- **What changes**
  - add a minimal `ToolExecutor` wrapper that enforces allowed tool names
- **Why**
  - needed by both `InspectRunner` and `LambdaRunner`
- **Dependencies**
  - no persistence or external dependencies

### `internal/rlm/plan_test.go`
- **What changes**
  - add tests for unsupported route/mode combinations
- **Why**
  - support matrix becomes behavior, not documentation only
- **Dependencies**
  - `plan.go`

### `internal/rlm/llm_runner_test.go`
- **What changes**
  - add tests for:
    - staged-without-phases rejection
    - guided/hard rejection
    - empty-tools + require-tool-use rejection
- **Why**
  - these are the current semantic gaps
- **Dependencies**
  - `llm_runner.go`

### `internal/rlm/inspect_runner_test.go`
- **What changes**
  - add tests proving hidden tools cannot be called when absent from `env.Tools`
- **Why**
  - guards the profile-enforcement fix
- **Dependencies**
  - `inspect_runner.go`, new scoped tool helper

### `internal/rlm/lambda_test.go`
- **What changes**
  - add tests proving lambda leaf execution respects filtered/empty tool sets
  - add tests for branch-failure metadata
- **Why**
  - lambda currently has no coverage for its CLI integration seam
- **Dependencies**
  - `lambda_runner.go`, new scoped tool helper

### `cmd/foxctl/cmd/rlm_test.go`
- **What changes**
  - add CLI-level tests for rejected invalid combinations:
    - `--plan-mode staged` on unsupported routes
    - empty tool profile + `--require-tool-use`
- **Why**
  - user-facing semantics should be locked
- **Dependencies**
  - `cmd/foxctl/cmd/rlm.go`

### `docs/spec/rlm_query_runtime.md`
- **What changes**
  - update status and current implementation caveats
- **Why**
  - current status is materially outdated
- **Dependencies**
  - none

### `docs/general/rlm-context.md`
- **What changes**
  - add explicit subsystem boundary note
- **Why**
  - avoid conflating `internal/runtime/engine/rlm_tools.go` with `internal/rlm/*`
- **Dependencies**
  - none

### `docs/plans/features/foxctl-rlm-next-steps.md`
- **What changes**
  - align staged-route narrative with actual `plan.go`
- **Why**
  - current doc and current implementation disagree on allowed/required discovery tools
- **Dependencies**
  - `plan.go`

### `docs/plans/features/longcot-rlm-evaluation-plan.md`
- **What changes**
  - explicitly document that empty-tool LongCoT conditions require `RequireToolUse=false`
  - note that direct-runner tool enforcement must match LLM runner enforcement
- **Why**
  - current plan assumes no-tool conditions that the generic CLI defaults would reject or accidentally bypass
- **Dependencies**
  - runner/tool enforcement changes

## Risks and migration

### Behavior changes
These are intentional and should be treated as breaking CLI/runtime semantics:

- invalid runner/mode combinations that currently “work” by silently downgrading will start failing
- empty-tool runs with `require-tool-use=true` will fail immediately instead of later or inconsistently
- `InspectRunner` and `LambdaRunner` will lose access to tools filtered out of `env.Tools`

### Backward-compatibility strategy
- keep flag names unchanged
- change only semantics from “silent fallback” to “explicit error”
- document the new rules in CLI help text and docs

### No persistence migration
- no DB schema changes
- no CAS format changes
- no serialized `Result` changes required for this slice

### Rollback
Low-risk:
- scoped tool enforcement and selection validation can be reverted independently
- docs updates are independent
- no data migration makes rollback straightforward

### Unknowns to validate during implementation
1. **External usage of silent fallbacks**
   - validate by checking repo workflows/scripts/docs for `--plan-mode staged` on non-code routes and `--plan-mode lambda` usage assumptions
2. **Need for auth-mode fields in `rlm.LLMConfig`**
   - validate against actual configured providers beyond LM Studio/OpenAI/OpenRouter defaults
3. **Installed binary skill fallback**
   - validate by running RLM commands in a non-source-checkout environment

## Implementation order

1. **Add plan/mode support validation in `internal/rlm` and tests**
   - independently testable
2. **Add scoped tool executor helper**
   - independently testable
3. **Update `InspectRunner` to use scoped tool execution**
   - compile/test independently
4. **Update `LambdaRunner` to use scoped tool execution and emit explicit branch-failure metadata**
   - compile/test independently
5. **Update `LLMRunner` to reject unsupported/no-op modes and empty-tool require-tool-use**
   - must land with its tests
6. **Update `cmd/foxctl/cmd/rlm.go` to validate combinations and unify runner LLM config resolution**
   - must land with `rlm_test.go`
7. **Update `cmd/foxctl/cmd/eval.go` to use the same validation/resolution path**
   - keep CLI/eval parity
8. **Update docs to match implemented behavior and split the two RLM narratives**
   - can land after code, but should ship in the same review cycle
