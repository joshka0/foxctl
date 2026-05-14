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

## Command output comparison

The latest local benchmark evidence is saved under
`/private/tmp/foxctl-benchmarks/`. These numbers are homepage-ready only with
the caveats shown here. Add future rows to this table when a run has a saved
artifact, a product signal, and an explicit caveat.

The current shell-output artifact is
`/private/tmp/foxctl-benchmarks/command-output-counts-20260514T0725-broader-command-output-counts.json`.
Each row runs the native binary and foxctl shell reduction on the same task.

| Binary | Same task | Native output | foxctl output | Output reduction |
|---|---|---:|---:|---:|
| `ls` | `ls -la internal` | `483` tokens / `1,002` bytes | `30` tokens / `106` bytes | `93.8%` less |
| `find` | `find internal/tooling -name '*.go'` | `825` tokens / `3,061` bytes | `66` tokens / `237` bytes | `92.0%` less |
| `cat` | `cat go.mod` | `7,520` tokens / `19,723` bytes | `1,011` tokens / `2,216` bytes | `86.6%` less |
| `head` | `head -n 80 cmd/foxctl/cmd/shell.go` | `679` tokens / `2,680` bytes | `580` tokens / `2,245` bytes | `14.6%` less |
| `tail` | `tail -n 80 cmd/foxctl/cmd/shell.go` | `655` tokens / `2,376` bytes | `623` tokens / `2,245` bytes | `4.9%` less |
| `grep` | `grep -rn 'func ' internal/tooling/shellreduce` | `4,779` tokens / `18,632` bytes | `53` tokens / `209` bytes | `98.9%` less |
| `sed` | `sed -n '1,120p' cmd/foxctl/cmd/shell.go` | `1,148` tokens / `4,617` bytes | `556` tokens / `2,216` bytes | `51.6%` less |
| `git status` | `git status --short` | `1,422` tokens / `5,095` bytes | `72` tokens / `215` bytes | `94.9%` less |
| `git diff` | `git diff --stat` | `1,760` tokens / `6,458` bytes | `182` tokens / `503` bytes | `89.7%` less |
| `git diff` | `git diff --name-only` | `1,313` tokens / `4,755` bytes | `225` tokens / `768` bytes | `82.9%` less |
| `git log` | `git log --stat -5` | `3,303` tokens / `11,464` bytes | `89` tokens / `337` bytes | `97.3%` less |
| `go test` | `go test ./internal/tooling/shellreduce` | `22` tokens / `68` bytes | `14` tokens / `41` bytes | `36.4%` less |
| **Total** | Twelve command-output rows where foxctl reduced output | `23,910` tokens / `79,932` bytes | `3,501` tokens / `11,338` bytes | `85.4%` less |

These are output-size numbers, not a cold latency claim. The product table is
limited to command rows with measured output reductions. The broader run also
exposed a router gap: `rg -m` / `rg --max-count` is not accepted yet even though
the equivalent `grep` case works.

## Other benchmark signals

| Surface | Current result | Signal | Caveat |
|---|---:|---|---|
| Context gather | `31.4x` faster evidence path | `6.50s`, path recall `0.86`, fact recall `1.00` | One dirty-worktree case; comparison artifact is kept in the eval notes |
| Runtime hot path | `96.5ns` runner, around `4us` shell route, around `415ns` shell summary | Hot in-process overhead stays allocation-visible | Separate from cold CLI startup and subprocess cost |
| Repoindex / DAG | Sub-millisecond search fallback, around `691us` DAG structural grep | Graph retrieval latency stays visible in Go benches | Fixture result, not full-workspace indexing time |
| Repoindex refresh | `30.96s`-`54.20s` samples, `1,813` files, `31,665` nodes, `141,648` edges | Repoindex freshness verified current after each run | Incremental detection works; global-edge rebuild still falls back to full |

Use the conservative version of these claims on the homepage: foxctl can make
agent context smaller and more auditable, and the Go hot paths are cheap. Do
not claim every foxctl wrapper is faster than native shell. Treat the local
context gather row as a narrow evidence-path smoke. Baseline accounting and
provider-backed comparison artifacts live in
`docs/general/code-search-evals.md#native-subagent-baselines`.
