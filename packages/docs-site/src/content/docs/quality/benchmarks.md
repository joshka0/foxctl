---
title: Benchmarks
description: Repeatable local benchmark entry points for foxctl repo performance checks.
---

Benchmarks are for performance-sensitive code paths where regressions are easy to
miss in correctness tests: repoindex search, CAS storage, DB scan helpers,
execution setup, domain builders, and shared time utilities.

## Run the curated suite

```bash
bun run bench:go
```

The runner writes a timestamped copy of the output under
`/private/tmp/foxctl-benchmarks/` and prints the path at the end.

## Tune a run

Use environment variables to keep local experiments repeatable:

| Variable | Default | Purpose |
|---|---|---|
| `BENCH_COUNT` | `3` | Number of benchmark samples per package |
| `BENCH_TIME` | `1s` | Per-benchmark timing target passed to `go test` |
| `BENCH_PATTERN` | `.` | Regex for matching benchmark names |
| `BENCH_OUT` | timestamped file in `/private/tmp` | Output capture path |

Example focused run:

```bash
BENCH_COUNT=5 BENCH_TIME=2s BENCH_PATTERN=Repoindex bun run bench:go ./internal/intelligence/indexing/repoindex
```

## Package set

The default suite covers packages that already contain benchmarks:

| Package | Why it is included |
|---|---|
| `./internal/domain/agent` | Agent profile and builder allocation behavior |
| `./internal/intelligence/indexing/repoindex` | Search, scoring, fallback, and query paths |
| `./internal/platform/timeutil` | Shared time formatting and parsing helpers |
| `./internal/runtime/execution/exec` | Command execution setup and hot-path overhead |
| `./internal/storage/cas` | Content-addressed storage write/read behavior |
| `./internal/storage/dbutil` | Scan helper overhead for storage rows |

Add a package to `scripts/run-go-benchmarks.sh` only when the benchmark protects a
known hot path or a previously regressed operation. Keep benchmark inputs
deterministic, avoid network access, and prefer explicit fixtures over host
machine state.

## Benchmark solutions

The benchmark suite is organized around concrete performance risks:

| Solution | Protected behavior | Signal to watch |
|---|---|---|
| Curated runner | Keeps benchmark invocation repeatable across local agents and CI-style checks | Command exits cleanly and captures output path |
| Repoindex search benchmarks | Protects zero-result fallback, scored search, syntax fallback, and query allocation behavior | `ns/op`, `B/op`, and `allocs/op` drift |
| CAS buffer benchmarks | Protects write/read buffering and large artifact handling | Throughput and allocation drift |
| Slice preallocation benchmarks | Guards against accidental allocation regressions in hot storage loops | Allocation count changing from zero |
| Context cancellation benchmarks | Makes cancellation checks visible in storage loops | Unexpected overhead or behavior drift |
| DB scan helper benchmarks | Protects timestamp and JSON row scan helper cost | `B/op` and `allocs/op` drift |
| Time utility benchmarks | Keeps common time format/parse helpers honest | Allocation-free parsing stays allocation-free |

When a benchmark fails or drifts, fix the underlying hot path first. Update the
benchmark only when the new behavior is intentional and the docs explain the
new expected cost.

## Current evidence snapshot

The latest local benchmark evidence is saved under
`/private/tmp/foxctl-benchmarks/`. These numbers are homepage-ready only with
the caveats shown here.

| Claim area | Current evidence | Caveat |
|---|---:|---|
| Local Codex native-subagent comparison | `gather_context` passed one RLM subsystem map case in `6.50s` after a fresh repoindex build; the local Codex subagent took `204.3s`, found all expected paths, but failed exact fact scoring with `0.14` fact recall and `1,202,987` transcript tokens (`1,049,728` cached) | One dirty-worktree smoke with repoindex freshness current after an incremental build request that fell back to a full graph rebuild; useful for budget shape, not a broad native-agent benchmark |
| Provider-backed researcher comparison | `gather_context` passed one RLM subsystem map case in `10.52s`; an OpenRouter-backed GPT-5.4-mini researcher failed the same case after `89.5s`, `199,620` tokens, and `$0.19962` normalized cost at `$1/Mtok` | One authenticated provider-backed case, not a local Codex native-subagent benchmark |
| Shell orientation context | Structured shell reduced `8,695` raw tokens to `953` returned tokens | Cold reduced calls were slightly slower overall in this run |
| Cold tool skill output | Foxctl skill calls returned `4,005` tokens versus `6,007` native-output tokens across the orientation set | `fs/ls` alone was not a token or latency win; the aggregate win came mostly from bounded ripgrep output |
| Hot Go runtime | No-hook tool runner measured `96.5ns/op`, shell route around `4us/op`, shell summary around `415ns/op` | Hot in-process cost is separate from CLI startup overhead |
| Repoindex and DAG fixtures | Search fallback stayed sub-millisecond; DAG structural grep measured around `691us/op` | Fixture results protect local hot paths, not full-workspace indexing time |

Use the conservative version of these claims on the homepage: foxctl can make
agent context smaller and more auditable, and the Go hot paths are cheap. Do
not claim every foxctl wrapper is faster than native shell. Treat the local
Codex row as a budget-and-scoring smoke: it proves the import path works and
shows path/fact scoring separately, but it is not a broad native-agent
benchmark.
