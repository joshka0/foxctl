# Generic Agent Experiment Loop

Status: draft design

This document adapts the `autoresearch` pattern into a generic `agentctl`
autonomy model for feature work, refactors, retrieval improvements, and other
engineering tasks that can be treated as bounded experiments.

## Core Idea

The useful part of `autoresearch` is not "overnight ML research". It is the
execution contract:

1. establish a baseline
2. make one bounded intervention
3. run a deterministic evaluation bundle
4. keep or discard based on evidence
5. log the result and continue

For `agentctl`, that should become a general autonomy mode for software work.

## Why This Fits `agentctl`

`agentctl` already has most of the machinery needed:

- durable task state
- task continuity packs
- ACA notes and handoffs
- repoindex / DAG context
- v2 events and projections
- Jido-backed tick/proactive runtime paths
- deterministic checks:
  - tests
  - lint
  - vet
  - retrieval evals
  - quality gates

What is missing is a generic experiment controller that treats feature work
more like a scientific process than a free-form "just keep coding" loop.

## Better Term

The right framing is:

- autonomous experiment loop

or more specifically:

- hypothesis-driven autonomous execution

This is stronger than "auto-running ability" because it makes evaluation and
reversibility first-class.

## Mapping From `autoresearch`

### `autoresearch`

- single editable target
- fixed wall-clock budget
- one scalar metric
- keep/discard branch advancement
- experiment log

### `agentctl`

- bounded editable scope:
  - task scope path
  - package set
  - issue lane
- fixed execution budget:
  - max iterations
  - max wall time
  - max tool calls
  - max changed files
- deterministic evaluation bundle:
  - tests
  - linters
  - retrieval evals
  - perf or benchmark checks
  - review findings
- keep/discard decision:
  - promote patch
  - revert patch
  - log artifact + result

## Scientific Feature Loop

The default feature loop should look like this:

1. Define a baseline.
2. State a hypothesis.
3. Apply one intervention.
4. Run a bounded evaluator.
5. Compare against the baseline.
6. Keep, discard, or queue for human review.

This keeps long autonomous runs legible and auditable.

## Example Hypotheses

### Retrieval work

Hypothesis:

- "Adding deterministic package-note fallback will improve `praze-mixed`
  `hit@5` without hurting `agentctl-mixed`."

Evaluator:

- `agentctl eval retrieval ...`

Keep rule:

- improvement on target suite
- no regression beyond tolerance on control suite

### Refactor work

Hypothesis:

- "Replacing subprocess skill chaining with shared internal packages will reduce
  runtime and preserve behavior."

Evaluator:

- targeted tests
- benchmark or timing smoke
- no new review findings

### Hook/runtime work

Hypothesis:

- "Task continuity refresh on runtime-state reads will improve agent inspection
  quality without increasing ask latency."

Evaluator:

- targeted tests
- state inspection smoke
- no regression in Jido ask path

### UI/API feature work

Hypothesis:

- "Adding structured continuity summaries to the workbench improves operator
  comprehension and reduces follow-up queries."

Evaluator:

- deterministic integration tests
- interaction smoke checks
- optional human review rubric

## Deterministic Pieces First

The experiment controller should not let the model decide the scoring logic.

The model may help with:

- proposing hypotheses
- choosing candidate interventions
- writing code
- summarizing results

But these parts should stay programmatic:

- baseline capture
- evaluator selection
- command execution
- metric extraction
- keep/discard decision
- artifact persistence

That is what makes the loop useful for long-running agents.

## Proposed Runtime Objects

```go
type ExperimentSpec struct {
    ID                string
    WorkspaceRoot     string
    TaskID            string
    Goal              string
    Hypothesis        string
    ScopePaths        []string
    Budget            ExperimentBudget
    Evaluators        []EvaluatorSpec
    PromotionPolicy   PromotionPolicy
    BaselineArtifact  string
}

type ExperimentBudget struct {
    MaxWallTime       time.Duration
    MaxIterations     int
    MaxToolCalls      int
    MaxChangedFiles   int
    MaxTrials         int
}

type EvaluatorSpec struct {
    Kind              string
    Command           []string
    MetricExtractors  []MetricExtractor
    Required          bool
}

type TrialResult struct {
    TrialID           string
    Status            string
    Metrics           map[string]float64
    Artifacts         []string
    Decision          string
    Summary           string
}
```

## Proposed Modes

### 1. Guided experiment mode

The agent is still model-driven, but the control layer chooses:

- baseline evaluator set
- allowed scope
- promotion thresholds

Good for:

- feature work
- retrieval tuning
- refactors

### 2. Hard experiment mode

The control layer fully specifies:

- exact checks
- exact success thresholds
- exact rollback rules

Good for:

- benchmark improvement work
- regression hunts
- reliability/perf tuning

### 3. Research mode

More open-ended ideation, but still with mandatory trial logging and bounded
evaluation.

Good for:

- exploratory architecture changes
- broad design-space search

## Generic Keep/Discard Policy

The simplest version is:

```text
baseline
  -> intervention
  -> evaluate
  -> compare
  -> keep if:
       required checks pass
       target metric improves or holds within tolerance
       complexity penalty stays under threshold
     else discard
```

Complexity penalty should also be programmatic where possible:

- files changed
- lines changed
- added dependencies
- reviewer findings count
- runtime/cost increase

## Where ACA, DAG, and RLM Fit

### ACA

Use ACA to define the experiment context:

- active task
- handoffs
- prior decisions
- canonical notes
- task continuity pack

### DAG / repoindex

Use repoindex to bound and explain interventions:

- affected packages
- dependency neighborhood
- touched structural anchors

### RLM

Use RLM only as a planning/synthesis assistant:

- generate candidate hypotheses
- compare failed trials
- summarize why a keep/discard decision happened

Do not let RLM own the final experiment score.

## Jido / Classic Flow

### Jido-backed agents

Best fit for long-running experiments.

Flow:

1. agent starts with `task_continuity`
2. runtime builds or resumes `ExperimentSpec`
3. each tick executes exactly one trial step
4. evaluator results are logged into projections + CAS
5. state inspection shows current trial frontier

### Classic daemon agents

Useful for simpler bounded loops.

Flow:

1. spawn with `--exec-mode tick` or `--exec-mode proactive`
2. use the same `ExperimentSpec`
3. execute trial loop locally
4. persist trial artifacts and decisions

## First Concrete Slice

The smallest credible first implementation is:

1. add an experiment spec doc/artifact format
2. add a deterministic evaluator runner
3. add a trial log store
4. add one autonomy profile:
   - `retrieval_experiment`

That profile would:

- use ACA task continuity for context
- restrict scope to retrieval files
- run retrieval eval suites
- keep/discard based on configured metric deltas

## Candidate Future Commands

```bash
agentctl experiment init --task-id T-123 --profile retrieval_experiment
agentctl experiment run --spec .agentctl/runtime/experiments/E-123.json
agentctl experiment status --id E-123
agentctl experiment review --id E-123
```

For agents:

```bash
agentctl agent spawn --role researcher \
  --exec-mode tick \
  --autonomy-profile experiment
```

## Strong Recommendation

Treat autonomous feature work as:

- hypothesis-driven
- evaluator-bounded
- keep/discard
- artifact-logged

That gives us a much safer and more useful form of autonomy than a generic
"keep working forever" loop.

It also matches the existing architecture:

- ACA for context
- repoindex/DAG for scope and structure
- RLM for planning/synthesis
- deterministic evaluators for decisions
