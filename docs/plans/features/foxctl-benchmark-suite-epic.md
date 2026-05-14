# Foxctl Benchmark Suite Epic

| Field | Value |
|-------|-------|
| Status | Baseline implemented |
| Created | 2026-05-13 |
| Scope | Canonical benchmark and eval suite for foxctl runtime, DAG/repoindex, RLM, rooms, hooks, and retrieval behavior |
| Related | [LongCoT eval contract plan](longcot-eval-contract-plan.md), [RLM retrieval findings](../../archive/plans/features/rlm-retrieval-findings.md), [RLM next steps](../../archive/plans/features/foxctl-rlm-next-steps.md), [retrieval evals](../../general/retrieval-evals.md), [RLM gather context](../../architecture/rlm-gather-context.md), [RLM query runtime spec](../../spec/rlm_query_runtime.md) |

## Outcome

Build a benchmark program that makes foxctl's Go-based framework measurable:
component runtime behavior, graph traversal, repo indexing, retrieval quality,
RLM control loops, room operations, and integration overhead should each have
their own repeatable benchmark lane.

The suite should answer three questions without blending them into one vague
score:

1. Did the Go runtime path get faster, slower, or more allocation-heavy?
2. Did graph/retrieval/RLM behavior get more correct for fixed fixtures?
3. Did expensive or live eval paths produce enough evidence to justify their
   added cost?

## Non-Goals

- Do not create a single composite "foxctl score".
- Do not make live LLM, network, LongCoT downloads, or external services part of
  the default local or PR benchmark gate.
- Do not claim official LongCoT leaderboard comparability for tool-using RLM
  conditions.
- Do not route benchmark behavior with keyword heuristics. Conditions, fixtures,
  tool profiles, and runner modes must be explicit fields.
- Do not use benchmark work to change protocol envelope fields, WASI network
  policy, or storage backend choices.
- Do not make performance thresholds block PRs until the suite has enough
  baseline history to distinguish regressions from machine noise.

## Benchmark Map

```text
                              foxctl benchmark suite
                                      |
             +------------------------+-------------------------+
             |                                                  |
      Go microbenchmarks                                  Behavioral evals
             |                                                  |
   +---------+----------+----------+------------+        +-------+--------+----------+----------+
   |                    |          |            |        |                |          |          |
runtime              repoindex    DAG          RLM      retrieval        graph      LongCoT    rooms
   |                    |          |            |        |                |          |          |
   |                    |          |            |        |                |          |          |
   v                    v          v            v        v                v          v          v
Run(ctx) lifecycle   build/index  expand/trace repl     hit@k/MRR        node/edge  correct    send/ask/task
worker spawn         delta scans   blast radius braid   retrieved paths  recall     verified   relay latency
cancellation         query latency budget cap   lambda  budget use       ranking    tokens     backlog drain
bounded queues       projection   path explain  tools   alert checks     coverage   leakage    projection lag
allocations          snapshots    layer shape   cost    fixture drift    fixtures   cost       persistence
```

The suite has two output planes:

```text
              deterministic local gate                         extended evidence
                         |                                             |
        +----------------+----------------+              +-------------+-------------+
        |                                 |              |                           |
 go test -bench ./...             fixture evals      live/adapted evals        reports
        |                                 |              |                           |
 ns/op allocs/op B/op             hit@k MRR recall    cost tokens wall time     markdown/json
        |                                 |              |                           |
        +----------------+----------------+--------------+-------------+-------------+
                         |
                  benchmark ledger
                         |
             benchstat + saved artifacts + regression notes
```

## Existing Surfaces to Reuse

| Area | Current artifact | Reuse |
|------|------------------|-------|
| Benchmark manifest | [foxctl.json](../../../configs/benchmarks/foxctl.json) | Canonical category, gate, fixture, metric, artifact, and command inventory |
| Manifest tooling | [benchmark.go](../../../cmd/foxctl/cmd/benchmark.go), [manifest.go](../../../internal/tooling/benchmarks/manifest.go) | Validates explicit benchmark contracts and lists default Go benchmark packages |
| Root Go runner | [run-go-benchmarks.sh](../../../scripts/run-go-benchmarks.sh) | Offline default runner for the Go microbenchmark lane |
| Exec runtime microbenchmarks | [exec_bench_test.go](../../../internal/runtime/execution/exec/exec_bench_test.go) | Existing `go test -bench` pattern for allocations and synthetic execution path |
| Hook overhead | [benchmark-hooks.sh](../../../scripts/benchmark-hooks.sh) | Existing wall-clock benchmark script for Claude hook categories |
| Shell reducer savings | `foxctl shell report --preset typical-bash` | Raw shell output versus structured foxctl reductions with bytes, tokens, latency, and optional input-cost estimates |
| Orientation harness | [benchmark-orientation-harness.sh](../../../scripts/benchmark-orientation-harness.sh) | Homepage-ready shell, tool, and hot reducer evidence pack with explicit budget fields |
| Eval command registry | [eval.go](../../../cmd/foxctl/cmd/eval.go) | Existing `foxctl eval ...` command family and envelope output pattern |
| Retrieval eval modes | [rlm-retrieval-findings.md](../../archive/plans/features/rlm-retrieval-findings.md) | Current hit@k/MRR comparison lanes across ACA, repoindex, DAG, and RLM |
| LongCoT/RLM conditions | [eval_longcot.go](../../../cmd/foxctl/cmd/eval_longcot.go) | Stable condition IDs and explicit route/profile/plan mode selection |
| LongCoT typed results | [types.go](../../../internal/tooling/evals/longcoteval/types.go) | Usage, tool event, leakage, and RLM attempt telemetry shapes |
| LongCoT autoresearch loop | [longcot-autoresearch-loop.md](../../research/longcot-autoresearch-loop.md), [longcot_autoresearch.py](../../../scripts/longcot_autoresearch.py) | Fixed-budget experiment runner and TSV ledger pattern |
| RLM gather context | [rlm-gather-context.md](../../architecture/rlm-gather-context.md) | Retrieval/context task boundary and quality eval target |

## Implemented Baseline

The first implementation tranche turns this epic from a plan into an executable
benchmark map:

| Lane | Implemented artifact | Default gate |
|------|----------------------|--------------|
| Manifest contract | [configs/benchmarks/foxctl.json](../../../configs/benchmarks/foxctl.json), [internal/tooling/benchmarks](../../../internal/tooling/benchmarks/manifest.go) | yes |
| CLI manifest validation | `foxctl benchmark manifest validate`, `list`, `packages` | yes |
| Go benchmark runner | [scripts/run-go-benchmarks.sh](../../../scripts/run-go-benchmarks.sh), `make bench-go` | yes |
| Runtime | [tool_runner_bench_test.go](../../../internal/runtime/engine/tool_runner_bench_test.go), [protocol_bench_test.go](../../../internal/protocol/protocol_bench_test.go), [base_actor_bench_test.go](../../../internal/runtime/actor/base_actor_bench_test.go), [wfq_bench_test.go](../../../internal/runtime/execution/scheduler/wfq_bench_test.go) | yes |
| Repoindex/DAG | [dag_bench_test.go](../../../internal/intelligence/indexing/repoindex/dag_bench_test.go), existing query benchmarks | yes |
| RLM | [run_spec_bench_test.go](../../../internal/rlm/run_spec_bench_test.go) plus gather-context/LongCoT extended eval entries | yes for model-free planner/policy |
| Retrieval | Existing ACA retrieval eval target is recorded in the manifest | advisory until vault paths, fixtures, and embedder health produce a stable saved artifact |
| Room runtime | Existing room frontier/story state tests are recorded as correctness evals | yes |
| Integrations/hooks | Hook wall-clock script, [router_bench_test.go](../../../internal/tooling/shellreduce/router_bench_test.go), `foxctl shell report`, and [benchmark-orientation-harness.sh](../../../scripts/benchmark-orientation-harness.sh) | shell reducer yes, shell/tool evidence extended, hook script extended |

Default gate means local/offline and no network or live LLM dependency. Extended
gate means useful evidence but intentionally opt-in because it is slower, more
environment-sensitive, or outside the Go microbenchmark lane.

## Current Evidence Snapshot

Local snapshot from 2026-05-13, saved under `/private/tmp/foxctl-benchmarks`.
These are baseline measurements, not regression thresholds.

Go hot-path benchmark highlights:

| Lane | Result | Interpretation |
|------|--------|----------------|
| tool runner, no hooks | `96.5 ns/op`, `0 B/op`, `0 allocs/op` | Tool execution wrapper is effectively free without dispatch hooks |
| tool runner, dispatcher | `1.16 us/op`, `672 B/op`, `11 allocs/op` | Hook dispatch is visible but still microsecond-scale in the fake dispatcher |
| envelope write/decode | `2.22 us/op` write, `2.83 us/op` decode | Protocol envelopes are cheap enough for CLI/tool boundaries |
| actor lifecycle/dispatch | `23.5 ns/op` lifecycle, `12.9 ns/op` dispatch | In-memory actor primitives are not the bottleneck |
| repoindex search fallback | `80.1 us/op` syntax fallback, `161.8 us/op` zero-result fallback | Query fallback cost is sub-millisecond in the deterministic fixture |
| DAG grep structural | `691 us/op` | Explanation subgraph expansion is sub-millisecond for the small fixture |
| trace path hit | `1.32 ms/op` | Path explanation is measurable but still local-interactive |
| RLM run-spec resolve | `1.27 us/op` | Model-free RLM policy/planner setup is not the expensive part |
| shell reducer route/summary | `3.79-3.94 us/op` route, `405-415 ns/op` summary | Hot reducer logic is cheap once inside the Go process |

Tool skill cold-run report from
`/private/tmp/foxctl-benchmarks/orientation-summary-20260513T233000Z.md`:

| Tool case | Native | Foxctl | Budget |
|-----------|--------|--------|--------|
| `fs/ls` over `internal` | `28 ms`, `251` tokens | `894 ms`, `309` tokens | `max_entries=200` |
| `text/ripgrep` for `func ` | `35 ms`, `2,878` tokens | `198 ms`, `331` tokens | `max_matches=20` |
| `code/context_grep` for `func ` | `20 ms`, `2,878` tokens | `198 ms`, `3,365` tokens | `max_matches=20`, `max_blocks=20` |

Interpretation: the current cold skill report shows real `foxctl run` tool
startup overhead. `text/ripgrep` has a strong output-size win against native
`rg`; `code/context_grep` deliberately returns function context, so its value is
quality and structure rather than raw byte reduction for this fixture.

Shell savings report:

| Report | Raw | Reduced | Savings |
|--------|-----|---------|---------|
| orientation commands | `8,695` tokens, `31,940` bytes | `953` tokens, `3,550` bytes | `89.0%` token savings, `88.9%` byte savings |
| estimated input cost at `$1/Mtok` | `$0.008695` | `$0.000953` | `$0.007742` saved |
| cold command wall time | `0.60s` raw | `0.67s` reduced | mixed per row; aggregate slightly slower in this run because most rows pay reducer startup |

Interpretation: foxctl already has strong token/cost evidence for shell-shaped
orientation output, especially `ls`, `grep`, and `git log`. The latency claim
needs to distinguish hot in-process reducer/runtime benchmarks from cold
CLI-subprocess reports. The webpage should present "smaller context fast" and
"cold shell wrapper has startup overhead" as separate facts rather than claiming
every structured command is faster than native shell.

Bounded `gather_context` report:

| Report | Result | Notes |
|--------|--------|-------|
| repoindex refresh | `40.3s`, `1,813` files, `32,142` symbols, `33,774` nodes, `151,026` edges | forced rebuild against current dirty worktree after `index repo status` reported a stale zero-node store |
| one-case smoke after refresh | pass rate `1.00`, path recall `0.86`, fact recall `1.00`, duration `7.26s`, emitted context `1,096` chars | `/private/tmp/foxctl-benchmarks/gather-context-case1-bin-20260513T234000Z.json` |
| one-case head-to-head after refresh | gather pass rate `1.00`, path recall `0.86`, fact recall `1.00`, duration `10.52s`, emitted context `1,182` chars | `/private/tmp/foxctl-benchmarks/gather-context-head-to-head-openrouter-native-fixed-after-reindex-priced-20260513T233351Z.json` |
| two-case orientation harness | pass rate `0.50`, mean path recall `0.762`, mean fact recall `1.00`, mean duration `17.4s`, mean emitted context `1,842` chars | `/private/tmp/foxctl-benchmarks/orientation-gather-context-20260513T233000Z.json` |

Interpretation: the bounded gather path is now runnable and measurable. The
first case passes after repoindex refresh; the second case still misses
`internal/rlm/run_spec.go` at the reduction stage, so this lane is useful
evidence but not yet strong enough for a broad "foxctl beats native explorers"
claim.

Dataset-matched `code_search_ensemble` trace-symbol report:

| Report | Result | Notes |
|--------|--------|-------|
| deterministic `foxctl-trace-symbol` suite | `6` cases, pass rate `0.67`, mean path recall `1.00`, mean correctness `0.92`, mean duration `11.75s` | `/private/tmp/foxctl-benchmarks/code-search-foxctl-trace-symbol-20260514T001000Z.json` |
| context footprint | mean loaded token estimate `2,144`, emitted token estimate `703`, parent input-token savings estimate `1,441`, compaction ratio `0.33` | direct runtime tool output, no live LLM |
| old native mini trace on same dataset | `65,963` total tokens for one batched native explorer run | token usage only; no parsed quality rows in the current report contract |

Interpretation: for the same trace-symbol dataset, the deterministic foxctl
controller emits roughly `4.2k` evidence tokens across six cases versus the old
native explorer's `65.9k` transcript-token total. That is a useful token-class
comparison, but it is not a complete head-to-head because the native transcript
still needs parsed path/fact recall rows.

Native explorer/subagent comparison now has one authenticated smoke row, but it
is still a narrow one-case result. The checked-in baseline in
[code-search-evals.md](../../general/code-search-evals.md#native-subagent-baselines)
records a 2026-04-30 mini explorer trace with `65,963` total tokens for the
trace-symbol suite. A fresh unauthenticated `openai:gpt-5.4-mini` attempt was
saved at
`/private/tmp/foxctl-benchmarks/native-gpt54mini-case1-20260513T234500Z.json`
and correctly records `baseline_error_count: 1`.

The authenticated OpenRouter rerun for
`openrouter:openai/gpt-5.4-mini` is saved at
`/private/tmp/foxctl-benchmarks/native-openrouter-gpt54mini-case1-fixed-20260513T233351Z.json`.
It completed without tool-adapter errors but failed the eval: pass rate `0.00`,
mean quality `0.14`, `0.00` path/fact recall, `89.5s`, `199,620` total tokens,
`$0.19962` normalized cost at `$1/Mtok`, and `9` tool calls. Imported beside
the refreshed `gather_context` row, the priced head-to-head report shows gather
pass rate `1.00` versus native `0.00`, path recall `0.86` versus `0.00`, fact
recall `1.00` versus `0.00`, and `8.5x` duration speedup.

This is evidence for a homepage claim about the benchmark lane, not the final
claim. The native run still needs a broader multi-case baseline and provider
cost calculation before the website says foxctl broadly beats native explorers.
Also note the discovered runtime fix: the agent repoindex adapter was passing
the literal string `inline_mode` instead of the requested mode. That bug is now
covered by focused runtime tests, and eval target reports no longer serialize
API keys.

The `gather_context` and `eval agents` commands now have `--case-limit` so
quick benchmark smoke runs can bound dataset rows separately from evidence
budget. `--timeout` is per case and `--limit` controls gathered evidence, not
dataset rows.

## Benchmark Categories

### 1. Runtime Benchmarks

Purpose: prove that foxctl's Go runtime framework stays cheap, cancellable, and
bounded as more orchestration layers are added.

Benchmarks to add:

| Benchmark | Measures | Fixture shape |
|-----------|----------|---------------|
| `BenchmarkComponentRunLifecycle` | startup/shutdown latency, goroutine cleanup | fake `Run(ctx)` component tree |
| `BenchmarkWorkerSpawnCancel` | spawn latency, cancellation propagation, leaked work | in-memory worker runtime with bounded tasks |
| `BenchmarkEventAppendProjection` | event append throughput, projection lag | fixed event stream and projection reader |
| `BenchmarkBoundedQueueBackpressure` | queue latency under pressure, drop/block behavior | producer/consumer with explicit capacity |
| `BenchmarkRuntimeSnapshotRead` | immutable snapshot read cost | high-read snapshot table or atomic pointer |
| `BenchmarkToolRunnerEnvelopeOverhead` | envelope parse/write, stderr logging, CAS pointer paths | small, medium, and artifact-sized outputs |

Required metrics:

- `ns/op`
- `B/op`
- `allocs/op`
- p50/p95 for operation-level benchmarks where a single `testing.B` metric is
  too flat
- cancellation completion deadline
- goroutine leak check for lifecycle benchmarks

### 2. Repoindex Benchmarks

Purpose: measure source indexing and graph query cost independently from model
quality.

Benchmarks to add:

| Benchmark | Measures | Fixture shape |
|-----------|----------|---------------|
| `BenchmarkRepoindexBuildSmallGo` | clean build time and allocations | small Go fixture workspace |
| `BenchmarkRepoindexBuildMixedWorkspace` | Go + TypeScript + docs scan overhead | synthetic mixed fixture |
| `BenchmarkRepoindexDeltaRebuild` | update cost after one file changes | staged fixture mutation |
| `BenchmarkRepoindexSearchProjection` | query latency and result materialization | fixed query set |
| `BenchmarkRepoindexOpenExpand` | graph open + neighborhood expansion cost | fixed seed nodes |
| `BenchmarkRepoindexConcurrentReaders` | read contention and snapshot safety | N concurrent query goroutines |

Quality evals to pair with performance:

- expected symbol discovery coverage
- import edge coverage
- call/ref edge precision on known fixtures
- missing-node diagnostics for `ErrNotFound` paths
- database lock retry behavior where storage is shared

### 3. DAG Benchmarks

Purpose: measure explanation subgraph behavior: not just "did it return
anything?", but whether it returned the useful bounded graph.

Benchmarks to add:

| Benchmark | Measures | Fixture shape |
|-----------|----------|---------------|
| `BenchmarkDAGGrepStructuralSmall` | structural query latency | fixed seed and edge set |
| `BenchmarkDAGGrepBudgetScaling` | cost as budget/depth changes | depth 1/2/3 and budget 20/80/200 |
| `BenchmarkTracePathHit` | trace latency for connected endpoints | known source and target |
| `BenchmarkTracePathMiss` | missing endpoint behavior and error cost | absent source/target |
| `BenchmarkBlastRadius` | impacted node discovery cost | changed file/symbol seed |
| `BenchmarkDAGRenderTree` | render cost and output size | fixed explanation subgraph |

Quality metrics:

- node recall against expected explanation nodes
- edge recall against expected structural edges
- path validity for `trace-path`
- budget adherence
- duplicate node suppression
- stable ordering for golden output

### 4. Retrieval Benchmarks

Purpose: keep ACA, code retrieval, direct repoindex, DAG, and RLM retrieval lanes
honest against the same fixtures.

Current lanes to preserve:

- `skill_context`
- `skill_default_plus_context`
- `repoindex_search`
- `repoindex_dag`
- `rlm_llm`
- `rlm_llm_code_staged`

Benchmarks/evals to add:

| Eval | Measures | Fixture shape |
|------|----------|---------------|
| `foxctl-mixed` regression suite | hit@5, MRR, available references | existing foxctl retrieval suite |
| `praze-mixed` compatibility suite | cross-repo transfer quality | existing praze retrieval suite |
| query-type split suite | per-query-family quality | explicit query labels, not keyword routing |
| path validation suite | returned paths exist and are task-relevant | expected path sets |
| budget discipline suite | useful context per token/byte budget | fixed budget tiers |

Required outputs:

- hit@k
- MRR
- path existence rate
- expected-path coverage
- context bytes/tokens
- alert count from eval validators
- markdown and JSON artifacts

### 5. RLM Benchmarks

Purpose: separate the RLM runtime from the RLM controller. A weak controller
should not obscure whether the Go runtime, REPL, braid, lambda, and tool
telemetry are working.

Runtime microbenchmarks:

| Benchmark | Measures | Fixture shape |
|-----------|----------|---------------|
| `BenchmarkRLMPlanSinglePass` | planner/controller overhead without model calls | fake model response |
| `BenchmarkRLMStagedPlan` | staged phase overhead | deterministic staged fixture |
| `BenchmarkRLMReplLoop` | REPL iteration overhead | fake tool and model responses |
| `BenchmarkRLMRecursiveSubcall` | child call setup/merge cost | fixed parent/child tree |
| `BenchmarkRLMBraidMerge` | braid fanout and synthesis overhead | N deterministic branches |
| `BenchmarkRLMToolTelemetry` | tool event accounting and reduction metadata | fake tool output sizes |

Behavior evals:

- retrieval-controller quality from the retrieval suite
- gather-context quality and budget use
- LongCoT condition comparisons
- leakage flag correctness
- tool reduction ratio and CAS artifact usage
- parent/child token accounting

### 6. Room Runtime Benchmarks

Purpose: benchmark the durable room/control-plane path separately from local
CLI/eval paths.

Benchmarks/evals to add:

| Benchmark | Measures | Fixture shape |
|-----------|----------|---------------|
| `BenchmarkRoomMessageAppend` | append latency and allocations | in-memory or temp DB room store |
| `BenchmarkRoomInboxPoll` | inbox scan/poll latency | fixed participants and messages |
| `BenchmarkRoomTaskStateTransition` | task lifecycle update cost | created/claimed/done transitions |
| `BenchmarkRoomEpicFrontier` | ready-frontier computation cost | fixed milestone/story graph |
| `BenchmarkRoomProjectionRefresh` | projection rebuild latency | synthetic room event log |
| `BenchmarkRoomRelayDelivery` | transport adapter overhead | fake transport adapter |

Quality metrics:

- no duplicate delivery
- stable frontier ordering
- explicit dependency blockers
- room projection freshness
- durable replay produces the same state

### 7. Integration and Hook Benchmarks

Purpose: capture the operational overhead around foxctl without treating it as
core runtime performance.

Benchmarks to add or formalize:

| Benchmark | Measures | Fixture shape |
|-----------|----------|---------------|
| hook category benchmark | pre/post/stop/session hook latency | existing `benchmark-hooks.sh` samples |
| shell reducer benchmark | routing overhead for supported commands | fixed command argv set |
| shell reducer savings report | raw vs reduced bytes, tokens, latency, and input-cost estimate | `typical-bash` orientation command preset |
| MCP adapter benchmark | request/response overhead without live provider calls | fake MCP server |
| provider config benchmark | provider resolution overhead | fixed config fixtures |
| docs-site build benchmark | documentation app build time | optional non-Go lane |

Default gate rule:

- hook and adapter benchmarks can run locally, but provider/network-backed
  integrations are opt-in extended benchmarks only.

## Epic Milestones

### Milestone 1 - Inventory and Benchmark Contract

Goal: define the benchmark surface before adding more tests.

Stories:

1. Inventory all current benchmark/eval commands and classify them as
   microbenchmark, fixture eval, or extended eval.
2. Add a benchmark manifest schema with explicit fields:
   `id`, `category`, `runner`, `fixtures`, `metrics`, `artifacts`,
   `default_gate`, `extended_gate`, and `requires_network`.
3. Define artifact locations for markdown, JSON, raw `go test -bench` output,
   and `benchstat` comparisons.
4. Document the local command matrix for quick, full, and extended benchmark
   runs.

Exit criteria:

- Every existing benchmark/eval surface above is represented in the manifest.
- Default gates are explicitly offline.
- No benchmark condition relies on keyword route detection.

### Milestone 2 - Go Microbenchmark Baseline

Goal: make the Go-native framework path measurable with standard tooling.

Stories:

1. Add a root benchmark command that runs focused Go benchmarks and stores raw
   output for `benchstat`.
2. Extend runtime benchmarks beyond the current exec buffer benchmark into
   lifecycle, cancellation, queues, projections, and tool-envelope overhead.
3. Add repoindex and DAG microbenchmarks with fixed workspaces and deterministic
   graph fixtures.
4. Add model-free RLM runtime benchmarks for planner, REPL, recursive, braid,
   and telemetry overhead.

Exit criteria:

- `go test -run '^$' -bench ... -benchmem` runs without live services.
- Benchmark fixtures are deterministic and safe for repeated local runs.
- Benchmark names are stable enough for historical comparison.

### Milestone 3 - Structural Quality Evals

Goal: pair latency with correctness for repoindex, DAG, and retrieval.

Stories:

1. Add DAG quality fixtures with expected nodes, edges, paths, and budgets.
2. Add repoindex quality fixtures for symbol, import, call/ref, and missing-node
   behavior.
3. Consolidate retrieval quality suites so ACA, direct repoindex, DAG, and RLM
   modes run against the same task fixtures.
4. Add path validation and budget discipline checks to retrieval reports.

Exit criteria:

- Reports show both speed and quality for structural retrieval lanes.
- Existing `foxctl-mixed` and `praze-mixed` findings remain reproducible or are
  explicitly superseded by a new fixture version.
- Direct DAG/repoindex failures explain whether the issue is coverage, ranking,
  query shape, or controller behavior.

### Milestone 4 - Runtime Telemetry and Reports

Goal: make benchmark output useful enough to drive engineering decisions.

Stories:

1. Ensure tool events include duration, input/output bytes, token estimates,
   reduction ratios, and CAS digests where applicable.
2. Add operation-level p50/p95 reporting for benchmarks that are too coarse for
   a single `testing.B` aggregate.
3. Save markdown and JSON reports consistently for eval commands.
4. Add `benchstat` comparison support for Go benchmark output.

Exit criteria:

- Benchmark reports identify what changed, not just that a score moved.
- Reports distinguish model cost, tool cost, graph cost, and render/output
  cost.
- Large outputs are summarized with artifacts instead of pasted inline.

### Milestone 5 - RLM and LongCoT Evidence Suite

Goal: turn current RLM experiments into repeatable evidence without overstating
them.

Stories:

1. Formalize the current LongCoT condition matrix as internal benchmark arms.
2. Integrate the fixed-budget autoresearch ledger pattern into benchmark
   reporting.
3. Keep leakage flags prominent and exclude contaminated runs from primary
   summaries.
4. Add gather-context A/B comparisons against non-RLM retrieval baselines.

Exit criteria:

- RLM reports separate runtime health from controller quality.
- LongCoT/RLM summaries label internal, contaminated, and no-tool comparisons
  explicitly.
- Cost, tokens, wall time, and verifier status are available for every live
  attempt.

### Milestone 6 - CI, Scheduling, and Adoption

Goal: make benchmarks usable in day-to-day work without making PRs noisy.

Stories:

1. Add quick/offline benchmark commands for local validation.
2. Add extended/manual commands for live LLM, LongCoT, and integration-heavy
   runs.
3. Add scheduled historical benchmark collection once baseline variance is
   understood.
4. Define when a regression blocks a PR versus when it creates a follow-up
   investigation.

Exit criteria:

- Quick benchmarks are safe for PR authors to run locally.
- Extended benchmarks produce artifacts suitable for review without requiring
  every developer to have live credentials.
- Regression policy is explicit and does not block on noisy first-run numbers.

## Validation Plan

Initial documentation validation:

```bash
make check-doc-links
```

Baseline benchmark commands:

```bash
make benchmark-manifest
make bench-go
RUN_GATHER_CONTEXT=0 make bench-orientation
./bin/foxctl benchmark manifest validate
./bin/foxctl benchmark manifest list --gate default
./bin/foxctl benchmark manifest packages --gate default
go test -run '^$' -bench 'BenchmarkToolRunner|BenchmarkBaseActorLifecycle|BenchmarkWFQ' -benchmem ./internal/runtime/engine ./internal/runtime/actor ./internal/runtime/execution/scheduler
go test -run '^$' -bench BenchmarkEnvelope -benchmem ./internal/protocol
go test -run '^$' -bench 'BenchmarkDAGGrep|BenchmarkTracePath|BenchmarkBlastRadius' -benchmem ./internal/intelligence/indexing/repoindex
go test -run '^$' -bench BenchmarkResolveRunSpec -benchmem ./internal/rlm
go test -run '^$' -bench BenchmarkShellReduce -benchmem ./internal/tooling/shellreduce
go test -run 'TestRunRoomEpicDispatchFrontier|TestRunRoomStoryState' ./cmd/foxctl/cmd
make eval-retrieval-foxctl
```

Extended benchmark commands:

```bash
./scripts/benchmark-hooks.sh 5 all
go run ./cmd/foxctl shell report --preset typical-bash --save-file /private/tmp/foxctl-benchmarks/shell-typical.json --input-token-price-per-million-usd 1
RUN_GATHER_CONTEXT=1 GATHER_CASE_LIMIT=2 ./scripts/benchmark-orientation-harness.sh
make eval-retrieval-foxctl-mixed
make eval-retrieval-foxctl-cochange
./bin/foxctl eval gather-context --eval-dataset-file testdata/evals/gather-context/foxctl-repo-grounded.jsonl --case-limit 2 --report-file /private/tmp/foxctl-benchmarks/gather-context.json
./bin/foxctl eval longcot --dataset testdata/evals/longcot/fixture.jsonl --dry-run --save --output-dir /private/tmp/foxctl-benchmarks/longcot
```

The manifest is the source of truth for which commands are default versus
extended.

Current retrieval note: `make eval-retrieval-foxctl` now resolves
`~/.foxctl/templates/obsidian-vault` when present and falls back to the
repo-local ACA vault fixture. That lane is intentionally kept visible but
advisory until vault paths, expected fixtures, and embedder health are
reconciled into a stable saved artifact.

## Ready Frontier

Completed in the baseline tranche:

1. Add this epic and link it from the plans index.
2. Inventory existing benchmark and eval commands into a manifest.
3. Land a small root benchmark runner for offline Go microbenchmarks.
4. Add missing runtime microbenchmarks around tool runner, actor lifecycle, and
   WFQ scheduling.
5. Add deterministic repoindex/DAG and RLM planner/policy benchmarks.
6. Add behavior-focused manifest validation tests.
7. Add an offline orientation harness for homepage-ready shell and tool budget
   evidence.

Needs a decision:

1. Artifact storage location beyond the current `/private/tmp/foxctl-benchmarks`
   default: CAS, `.foxctl/benchmarks`, or a temp directory plus explicit copy
   step.
2. CI policy: advisory-only benchmark artifacts first, or a non-blocking
   scheduled job before PR gates.
3. Extended eval cadence: manual only, nightly, or pre-release.
4. Whether docs-site build timing belongs in this epic or in a separate docs
   quality epic.
5. Whether to re-embed `~/.foxctl` before the next semantic retrieval artifact
   run. This snapshot refreshed repoindex but did not re-embed because no
   embedding provider configuration or API key was available in the shell.

Dependencies:

1. Stable repoindex/DAG fixtures that do not depend on live repo drift.
2. Token/cost telemetry in RLM and tool events before making strong efficiency
   claims.
3. Benchstat or equivalent comparison tooling in the local developer path.
4. A clear separation between official-style no-tool evals and internal
   tool-using experiments.
5. A completed native explorer/subagent baseline imported through the eval
   external-results path before the website claims foxctl beats native agents.

## Room Import Command

When a room id exists, import this planning artifact as a room-agile epic:

```bash
foxctl room epic import-plan <room-id> \
  --plan-file docs/plans/features/foxctl-benchmark-suite-epic.md \
  --provider auto \
  --sender codex
```
