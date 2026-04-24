# RLM Helper Pipeline and Repair Loop Plan

## Status

Draft implementation plan. The first proof-of-concept slice exists in the
LongCoT eval runner:

- `ephemeral_helper_solve` is exposed as one parent-facing tool.
- BlocksWorld stack prompts can select a runtime-owned preset helper
  (`blocksworld_stack_greedy_v1`).
- The preset path is verified against official LongCoT with
  `--blocksworld-helper=false`, so it exercises the general helper path rather
  than the older direct `blocksworld_solve` tool.

Critical review has been incorporated into the execution order:

- `--general-helper` should imply helper-only phase enforcement.
- Helper-assisted attempts must be explicitly marked as scaffolded and not
  confused with no-tool/official LongCoT conditions.
- Compact helper trace output is a blocking next slice, not a late cleanup.
- Cache keys must include concrete input and verifier identity, not only
  capability and task signature.
- Benchmark-specific presets should move out of CLI command code during the
  pipeline extraction.

This plan generalizes that proof into a reusable helper-pipeline runtime with
repair and rerun support.

## Goal

Build a runtime-owned helper pipeline behind one model-facing tool:

```text
parent RLM
  -> ephemeral_helper_solve(task, constraints)
  -> capability planner
  -> parser / solver / verifier / formatter helpers
  -> repair failed step when needed
  -> compact answer + trace
```

The parent model asks for a capability. It does not manage helper IDs, helper
source, pipeline order, cache keys, retries, or repair prompts.

## Non-Goals

- Do not turn synthesized helpers into durable skills automatically.
- Do not expose helper IDs or pipeline internals as required model-managed
  arguments.
- Do not treat scaffolded LongCoT helper runs as official leaderboard
  conditions.
- Do not add ad hoc keyword routing. Capability selection must use explicit
  structured task signatures, parser outputs, or typed eval metadata.
- Do not build microVM isolation in this slice. Keep the helper API shaped so a
  later microVM runner can replace the local yaegi execution layer.

## Current Behavior

`internal/rlm/runtime.HelperFactoryTools` currently supports:

- parent-facing `ephemeral_helper_solve`;
- optional runtime preset source/input;
- model-drafted Go helper fallback;
- validation through `internal/tooling/skillrun/ephemeral.GoSkillRunner`;
- compact success/failure trace in RLM metadata;
- bounded helper attempts, helper model override, helper timeout, and helper
  token budget from `eval longcot`.

The first preset is selected in `cmd/foxctl/cmd/eval_longcot.go` by parsing
official BlocksWorld stack prompts into compact JSON state:

```json
{
  "initial_json": "[[...], ...]",
  "goal_json": "[[...], ...]"
}
```

and running a short-lived Go helper through the same ephemeral skill path.

## Target Architecture

### Core Types

Add a runtime package under the existing RLM family:

```text
internal/rlm/runtime/helperpipeline/
```

This is runtime-owned orchestration, so it belongs under `internal/rlm/runtime`
rather than a new top-level root.

Proposed types:

```go
type Capability string

const (
    CapabilityParseProblem Capability = "parse_problem"
    CapabilitySolve        Capability = "solve"
    CapabilityVerify       Capability = "verify"
    CapabilityFormatAnswer Capability = "format_answer"
)

type TaskSignature struct {
    Domain     string         `json:"domain,omitempty"`
    Template   string         `json:"template,omitempty"`
    Shape      string         `json:"shape,omitempty"`
    InputDigest string        `json:"input_digest,omitempty"`
    VerifierID  string        `json:"verifier_id,omitempty"`
    InputKeys  []string       `json:"input_keys,omitempty"`
    Constraints map[string]any `json:"constraints,omitempty"`
}

type StepSpec struct {
    ID           string       `json:"id"`
    Capability   Capability   `json:"capability"`
    PresetName   string       `json:"preset_name,omitempty"`
    Requires     []string     `json:"requires,omitempty"`
    OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

type PipelineSpec struct {
    Signature TaskSignature `json:"signature"`
    Steps     []StepSpec    `json:"steps"`
}

type StepRun struct {
    StepID      string         `json:"step_id"`
    Capability  Capability     `json:"capability"`
    Status      string         `json:"status"`
    Input       map[string]any `json:"input,omitempty"`
    Output      map[string]any `json:"output,omitempty"`
    Error       string         `json:"error,omitempty"`
    SourceHash  string         `json:"source_hash,omitempty"`
    PresetName  string         `json:"preset_name,omitempty"`
    DurationMS  int64          `json:"duration_ms,omitempty"`
}

type PipelineRun struct {
    PipelineID string    `json:"pipeline_id"`
    Scaffolded bool      `json:"scaffolded"`
    Status     string    `json:"status"`
    Steps      []StepRun `json:"steps"`
    Answer     string    `json:"answer,omitempty"`
    Error      string    `json:"error,omitempty"`
}
```

### Runtime Components

```text
HelperPipelineEngine
  SignatureExtractor
  CapabilityRegistry
  PipelinePlanner
  StepExecutor
  RepairController
  TraceRecorder
```

- `SignatureExtractor`: turns task prompt and explicit eval metadata into a
  structured `TaskSignature`.
- `CapabilityRegistry`: stores runtime presets and attempt/run cache entries.
- `PipelinePlanner`: selects parser/solver/verifier/formatter steps from the
  registry and identifies missing capabilities.
- `StepExecutor`: runs one preset or synthesized helper through yaegi today;
  later through a microVM.
- `RepairController`: reruns from the earliest affected step when validation or
  verification fails.
- `TraceRecorder`: emits compact trace metadata plus optdata trajectory records.

## Repair Loop

The repair loop operates at the helper-pipeline level, not at the whole parent
RLM attempt level.

```text
run parse
run solve
run verify
run format
if verify/format fails:
  build failure packet
  select earliest affected step
  repair or replace that step
  rerun from that step forward
```

Failure packet:

```json
{
  "question_id": "BlocksWorld_easy_1",
  "signature": {
    "domain": "logic",
    "template": "BlocksWorld",
    "shape": "stack_rearrangement"
  },
  "failed_step_id": "solve",
  "failed_capability": "solve",
  "status": "verify_failed",
  "candidate_answer": "solution = ...",
  "verifier_error": "final state does not match goal",
  "compact_state": {
    "initial_json": "...",
    "goal_json": "..."
  },
  "prior_step_outputs": {}
}
```

Repair rules:

- Parser failure reruns parser and all downstream steps.
- Solver failure reruns solver, verifier, and formatter.
- Verifier failure can either repair solver or replace verifier depending on
  whether verifier itself errored.
- Formatter failure reruns formatter only.
- A step is promoted to cache only after verified success across a threshold,
  never from a single successful repair.

## Cache Model

Use an in-run cache first:

```go
type HelperCacheKey struct {
    Capability Capability
    SignatureHash string
    InputDigest string
    VerifierID string
    PresetName string
    SourceHash string
}
```

Cache entries should record:

- capability;
- task signature;
- concrete compact input digest;
- verifier name/version or deterministic verifier hash;
- source hash;
- input/output schema hash;
- verified success count;
- failure count;
- last failure packet digest;
- provenance (`preset`, `synthesized`, `repaired`).

Promotion policy:

- `preset`: checked into code or config, deterministic, immediately reusable.
- `run_cache`: reusable only within one eval/run.
- `durable_candidate`: persisted as data but not exposed by default.
- `durable_promoted`: requires repeated verified success and review.

## LongCoT Integration

The LongCoT eval should continue exposing one parent-facing tool:

```text
ephemeral_helper_solve
```

The implementation behind it becomes:

```text
official prompt
  -> signature extraction
  -> pipeline plan
  -> execute/repair pipeline
  -> compact answer
```

For BlocksWorld stack prompts, the initial pipeline is:

```text
parse_stack_state        preset/runtime parser
solve_stack_rearrange    preset blocksworld_stack_greedy_v1
verify_stack_moves       deterministic verifier
format_solution_moves    deterministic formatter
```

Current proof collapses these into one preset helper. The next slices should
split them into explicit steps so repair can rerun only affected steps.

## Implementation Sequence

### Slice 0: Harden Current LongCoT Helper Contract

- Make `--general-helper` imply helper-only phase enforcement.
- Report helper-only tool surface for general-helper conditions.
- Remove prompt conflicts between REPL-first and helper-first contracts.
- Add metadata flag for scaffolded helper usage:
  - `helper_scaffolded=true`;
  - `helper_preset=<name>`;
  - `leaderboard_comparable=false`.
- Ensure `blocksworldHelper` fallback cannot silently overwrite helper-pipeline
  results in helper conditions.

### Slice 1: Compact Trace Output

- Stop returning full helper source and full large move arrays in the parent
  phase tool result by default.
- Return:
  - `answer`;
  - `pipeline_id`;
  - `preset_name`;
  - compact step summary;
  - source hash;
  - artifact/digest pointer for large traces when persistence is enabled.
- Keep full trace in artifacts/metadata, not in the next model prompt.

### Slice 2: Extract Helper Pipeline Types

- Add `internal/rlm/runtime/helperpipeline`.
- Move generic pipeline types and result structs there.
- Keep `HelperFactoryTools` public API stable.
- Add table-driven tests for pipeline status and JSON stability.

### Slice 3: Split BlocksWorld Preset into Steps

- Parse official prompt into `initial_json` and `goal_json`.
- Run solver helper from compact JSON.
- Add deterministic move verifier helper.
- Add answer formatter helper.
- Test that solver output is rejected if verifier fails.

### Slice 4: Repair Controller

- Add `--helper-repair-attempts`.
- On verifier failure, produce failure packet and rerun from solver.
- For synthesized helpers, repair prompt receives:
  - previous source;
  - compact failure packet;
  - expected input/output schemas.
- For preset helpers, repair does not mutate preset source; it may choose an
  alternate preset or fallback to synthesized helper within budget.

### Slice 5: Run Cache

- Add in-memory cache scoped to one root RLM attempt/run.
- Cache by structured capability and task signature.
- Record hit/miss/create/repair telemetry.
- Do not persist durable cache yet.

### Slice 6: LongCoT Reporting

- Add helper pipeline summary to `RLMAttemptMeta.Metadata`:
  - pipeline status;
  - step statuses;
  - cache hits;
  - repair attempts;
  - verifier failures;
  - compact error categories.
- Render a small helper-pipeline section in markdown.

### Slice 7: Optdata / Optimization Records

- Emit trajectory records for:
  - synthesized helper draft prompts;
  - validation errors;
  - verifier failures;
  - repair attempts;
  - successful promoted candidates.
- Keep records small and source-hash large code blobs.

## Testing Plan

Unit tests:

- pipeline plan construction;
- earliest affected step selection;
- repair budget exhaustion;
- cache hit/miss semantics;
- verifier failure packet shape;
- compact trace redaction;
- preset source hash stability.

Integration tests:

- BlocksWorld stack preset succeeds with deterministic verifier.
- Corrupted solver output fails verifier and triggers repair path.
- Formatter-only failure reruns formatter only.
- Parent REPL phase receives compact helper result, not full source/move trace.

Live smoke tests:

```bash
foxctl eval longcot \
  --longcot-repo /path/to/longcot \
  --difficulty longcot-mini \
  --domain logic \
  --limit 5 \
  --condition rlm_repl_no_subcalls \
  --provider lmstudio \
  --model liquid/lfm2.5-1.2b \
  --general-helper \
  --require-ephemeral-skills \
  --blocksworld-helper=false \
  --verify \
  --verify-no-fallback
```

Expected result for current BlocksWorld stack subset: verified correct while
using `ephemeral_helper_solve`, not `blocksworld_solve`.

## Risks

- Presets can become hidden benchmark-specific solvers. Reports must label them
  as scaffolded helper conditions.
- The existing deterministic `blocksworld_solve` fallback can invalidate
  helper-pipeline conclusions if left enabled in scaffolded comparisons.
- Full helper traces can explode parent prompt size. Compact trace output is
  high priority.
- Synthesized helpers are unreliable for whole solvers. Prefer small missing
  steps over full-pipeline synthesis.
- Capability cache promotion could lock in buggy helpers. Promotion must require
  repeated verified success plus provenance.
- Repair loops can mask bad parsers if verifier is weak. Deterministic
  verifiers are required before trusting solver repairs.

## Open Questions

- Should durable helper candidates live under `internal/rlm/optdata`, a new
  storage-backed registry, or eval artifacts first?
- Should synthesized helper repair use the same local model as the parent,
  a separate helper model, or a provider-specific structured-output model?
- How should microVM execution receive cached helper sources and compact inputs?
- Should LongCoT helper presets be repo-code only, config-driven, or skill-file
  driven?
