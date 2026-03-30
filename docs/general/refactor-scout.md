# Refactor Scout

`agentctl` now includes a local structural refactor workflow for deterministic
hotspot discovery and shortlist planning.

## What It Is

There are two entrypoints:

- `agentctl refactor scout`
- `agentctl refactor advisor`

`refactor scout` is the primary deterministic retrieval lane. It ranks likely
refactor seams and hotspots from local code structure.

`refactor advisor` runs the scout first, then uses a second-stage model to rank
or sequence the findings. The scout remains the source of discovery truth.

## Commands

Run the scout directly:

```bash
agentctl refactor scout --path . --language go
agentctl refactor scout --path ./packages --language typescript
agentctl refactor scout --path ./scripts --language python
agentctl refactor scout --path apps/praze-api/lib --language elixir
```

Run the two-stage advisor:

```bash
agentctl refactor advisor --path ./internal --language go
```

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
  now useful for namespace-scoped module seams and file seams, but still more
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
