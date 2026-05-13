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
