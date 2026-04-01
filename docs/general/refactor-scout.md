# Refactor Scout

`agentctl` now includes a local structural refactor workflow for deterministic
hotspot discovery and shortlist planning.

## What It Is

There are two entrypoints:

- `agentctl refactor status`
- `agentctl refactor snapshot`
- `agentctl refactor deps`
- `agentctl refactor changes`
- `agentctl refactor hot`
- `agentctl refactor evidence`
- `agentctl refactor scout`
- `agentctl refactor advisor`

`refactor status` reports whether a given refactor scope will run
`index_backed` or fall back to `parser_only`, along with the concrete reason
codes for that decision.

`refactor snapshot` freezes a single-language scope into a deterministic
artifact-backed payload and records a small metadata row for later lookup.

`refactor deps` expands forward or reverse dependencies from a scoped repoindex
seed. It is repoindex-backed rather than parser-backed, so it surfaces
`index_mode` and freshness reasons directly in the output.

`refactor changes` reports changed files and current symbol surfaces since a git
ref or refactor snapshot. Snapshot comparisons are structural diffs over stored
snapshot payloads; git-ref comparisons use the current workspace plus git
status/diff data scoped to the requested path.

`refactor hot` ranks recently hot files from git churn within the scoped path.
The first version is file-level only and weights touches by recency.

`refactor evidence` reads persisted refactor artifacts back out of CAS. It can
load either a stored snapshot by `snapshot_id` or a scout-produced evidence
artifact by digest.

`refactor scout` is the primary deterministic retrieval lane. It ranks likely
refactor seams and hotspots from local code structure.

`refactor advisor` runs the scout first, then uses a second-stage model to rank
or sequence the findings. The scout remains the source of discovery truth.

## Commands

Run the scout directly:

```bash
agentctl refactor status --path ./internal --language go
agentctl refactor snapshot --path ./internal --language go
agentctl refactor deps --path ./internal --language go --query "Builder.Build" --direction in
agentctl refactor changes --path ./internal --language go --since HEAD~5
agentctl refactor hot --path ./internal --language go --since HEAD~20
agentctl refactor evidence --snapshot-id refsnap-1775053803740
agentctl refactor evidence --artifact sha256:54cc680432b307307d524d7c857fcd179749a4f3fac30560f27c41f4d82e4ecf
agentctl refactor scout --path . --language go
agentctl refactor scout --path ./packages --language typescript
agentctl refactor scout --path ./scripts --language python
agentctl refactor scout --path apps/praze-api/lib --language elixir
agentctl refactor scout --path ./internal --language go --focus slop
```

Run the two-stage advisor:

```bash
agentctl refactor advisor --path ./internal --language go
agentctl refactor advisor --path ./internal --language go --focus slop
```

Typical `refactor status` reads:

- `index_backed`:
  the repo index exists, matches the current Git HEAD, and includes the scoped
  language
- `parser_only`:
  the refactor run will still work, but it will not attach repo-index evidence
  such as freshness-checked graph support

Current reason codes include:

- `repoindex_missing`
- `repoindex_open_failed`
- `repoindex_meta_unavailable`
- `repoindex_stats_unavailable`
- `repoindex_schema_mismatch`
- `repoindex_head_mismatch`
- `git_head_unavailable`
- `scope_language_not_indexed`

Typical `refactor deps` reads:

- use `--query` to resolve seeds from repoindex search within the scoped path
- use repeated `--seed` values when you already have repoindex node IDs
- keep `--direction in` for reverse dependencies and `--direction out` for
  downstream fan-out
- rebuild the repo index when `index_mode` comes back with freshness or coverage
  reasons you do not want to ignore

Typical `refactor changes` reads:

- use `--since HEAD~N` or another git ref for working-tree-relative change reads
- use `--since refsnap-...` for deterministic snapshot-to-current comparisons
- keep `--max-files` and `--max-symbols` bounded when scanning broad scopes

Typical `refactor hot` reads:

- use `--since HEAD~N` to define the commit window
- use a `refsnap-...` id when you want the baseline commit from a saved snapshot
- treat this first version as churn ranking, not a full co-change graph

Typical `refactor evidence` reads:

- use `--snapshot-id refsnap-...` for a compact view of one stored scope snapshot
- add `--full` when you want the snapshot's file and symbol lists inline
- use `--artifact sha256:...` for a scout `data.evidence_artifact` or
  `data.snapshot_artifact`
- treat this command as artifact inspection, not a fresh scout run

Important operating rules:

- use one language per run
- run from the target repo workspace
- prefer narrow paths over repo-root scans when the repo is large
- for Elixir, prefer app `lib/` roots over `apps/` or repo root

## What It Surfaces

Current rule families include:

- function hotspots:
  - `function_hotspot`
  - `fan_out_dependency_spread`
  - `duplicate_recovery_block`
  - `duplicated_error_remap`
  - `repeated_guard_ladder`
  - `duplicate_orchestration_fingerprint`
  - `same_file_extraction_candidate`
- file and module seams:
  - `structural_similarity_cluster`
  - `structural_similarity_module_cluster`
  - `call_family_cluster`
  - `call_family_module_cluster`
- signature and size signals:
  - `long_parameter_list`
  - `boolean_parameter`
  - `wide_return_tuple`
  - `oversized_function`
  - `high_cyclomatic_complexity`
  - `deep_nesting`
  - `god_file`

Cluster findings are classified into seam kinds:

- `workflow_abstraction`
- `thin_wrapper_api_layer`
- `shared_operation_family`

Focused usage:

- `--focus slop` filters the scout/advisor view toward slop-shaped cleanup
  findings *(in this repo's terms: duplicated recovery blocks, duplicate
  error remaps, repeated guard predicates, duplicate orchestration, and closely
  related extraction candidates)*.
- `refactor scout` now also emits `data.index_mode` so downstream review flows
  can tell whether a run was parser-only or index-backed.
- successful scout runs now emit `data.snapshot_id` and
  `data.snapshot_artifact`
- `data.signals.evidence_backed` tells you whether the snapshot/evidence layer
  completed, independent of whether repoindex was fresh
- parser-only runs still attach scope snapshot evidence and recent churn data
  like `recent_change_count` and `hot_score` on `function_hotspot` findings
- index-backed runs additionally attach repo-graph evidence such as
  `seed_node_id`, reverse/forward dependency counts, and `suggested_reads`
- when repoindex is fresh, scout now also uses reverse deps, forward deps, and
  hot-score evidence to rerank `function_hotspot` findings rather than treating
  that evidence as display-only
- when hotspot evidence rows are persisted, scout also emits
  `data.evidence_artifact`

## Language Read

Current confidence by language:

- Go:
  strongest, because the scout has AST-backed complexity and orchestration
  signals
- TypeScript:
  useful for file/module seams and operation-family clustering
- Python:
  useful for function hotspots and smaller operation families, but weaker than
  Go and TS
- Elixir:
  now useful for namespace-scoped module seams, file seams, and first-pass slop
  detection on repeated `if` guards, duplicated guarded recovery blocks, and
  repeated rescue-side or tuple-style clause error remaps, but still more
  sensitive to broad helper families than Go

## ACA Fit

The refactor workflow fits into ACA as follows:

- `L0` active run:
  use `agentctl refactor scout` to get the current hotspot surface
- `L1` top of mind:
  keep the current top 2-3 seams and function hotspots visible in the active
  frontier
- `L2` operational memory:
  record which seams were judged real, which were rejected, and why
- `L3` durable knowledge:
  promote recurring structural findings into durable repo notes when they become
  stable guidance

The intended usage is:

1. run the scout on a narrow, single-language scope
2. inspect the top seams and hotspots
3. choose one seam family or function hotspot to act on
4. capture the decision into ACA notes if the seam is durable or recurring

## Current Read

The scout is strongest when used for:

- hotspot discovery
- first-pass seam detection
- narrowing the set of files/functions worth reading

It is weaker when used for:

- final refactor design without reading the code
- broad mixed-language repo-root scans
- treating all cluster outputs as equally trustworthy without namespace context

For current working snapshots and repo-specific examples, see
[docs/notes/refactor-surface-2026-03-29.md](../notes/refactor-surface-2026-03-29.md).
