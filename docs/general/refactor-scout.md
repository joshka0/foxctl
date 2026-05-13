# Refactor Scout

`foxctl` now includes a local structural refactor workflow for deterministic
hotspot discovery and shortlist planning.

## What It Is

There are two entrypoints:

- `foxctl refactor status`
- `foxctl refactor snapshot`
- `foxctl refactor deps`
- `foxctl refactor changes`
- `foxctl refactor hot`
- `foxctl refactor evidence`
- `foxctl refactor scout`
- `foxctl refactor advisor`

`refactor status` reports whether a given refactor scope will run
`index_backed` or fall back to `parser_only`, along with the concrete reason
codes for that decision and a file-level coverage summary for the requested
scope.

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

Python is now part of the repoindex-backed refactor lane as well. If you build
repoindex with `--python`, `refactor status` and `refactor scout --focus dead`
can run `index_backed` on Python package or script scopes instead of falling
back to parser-only mode.

Rust is now part of the repoindex-backed refactor lane too. If you build
repoindex with `--rust`, `refactor status` and `refactor scout --focus dead`
can run `index_backed` on Rust crate or module scopes instead of falling back
to parser-only mode.

## At A Glance

The current refactor surface is:

- `status`:
  decide `parser_only` vs `index_backed`, with reason codes and coverage detail
- `snapshot`:
  freeze one language scope into a deterministic artifact-backed record
- `deps`:
  inspect reverse or forward graph context from repoindex seeds
- `changes`:
  compare the current scope against a git ref or saved snapshot
- `hot`:
  rank recently changed files within scope from git churn
- `evidence`:
  reopen saved snapshot and hotspot artifacts from CAS
- `scout`:
  produce deterministic structural findings, slop findings, and dead-code
  candidates
- `advisor`:
  consume scout output and rank a shorter action sequence with an LLM

## Commands

Run the scout directly:

```bash
foxctl refactor status --path ./internal --language go
foxctl refactor snapshot --path ./internal --language go
foxctl refactor deps --path ./internal --language go --query "Builder.Build" --direction in
foxctl refactor changes --path ./internal --language go --since HEAD~5
foxctl refactor hot --path ./internal --language go --since HEAD~20
foxctl refactor evidence --snapshot-id refsnap-1775053803740
foxctl refactor evidence --artifact sha256:54cc680432b307307d524d7c857fcd179749a4f3fac30560f27c41f4d82e4ecf
foxctl refactor scout --path . --language go
foxctl refactor scout --path ./packages --language typescript
foxctl refactor scout --path ./scripts --language python
foxctl refactor scout --path ./crates/my-crate/src --language rust
foxctl refactor scout --path apps/praze-api/lib --language elixir
foxctl refactor scout --path ./internal --language go --focus slop
foxctl refactor scout --path ./internal/runtime/actor --language go --focus dead
foxctl refactor scout --path ./internal --language go --target improve-codebase-architecture
foxctl refactor scout --path ./internal --language go --target semantic-commenting
```

Run the two-stage advisor:

```bash
foxctl refactor advisor --path ./internal --language go
foxctl refactor advisor --path ./internal --language go --focus slop
```

Typical `refactor status` reads:

- `index_backed`:
  the repo index exists, matches the current Git HEAD, and includes the scoped
  language and path coverage
- `parser_only`:
  the refactor run will still work, but it will not attach repo-index evidence
  such as freshness-checked graph support; check `repo_index.coverage` for file
  counts and missing-path samples

Current reason codes include:

- `repoindex_missing`
- `repoindex_open_failed`
- `repoindex_meta_unavailable`
- `repoindex_stats_unavailable`
- `repoindex_schema_mismatch`
- `repoindex_head_mismatch`
- `git_head_unavailable`
- `scope_language_not_indexed`
- `scope_path_not_indexed`

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
- for TS/Elixir monorepos, build repoindex from the app root you actually care
  about *(for example `praze/`, `apps/praze-api/`, `apps/praze-presence/`)*
  rather than indexing the whole monorepo by default
- for Python, build repoindex with `--python` on the package root or scripts
  root you actually want to analyze rather than indexing unrelated monorepo
  siblings by default
- for Rust, build repoindex with `--rust` on the crate root you actually want
  to analyze rather than indexing unrelated monorepo siblings by default
- treat `--focus dead` as advisory and only trust it when `refactor status`
  reports a fully `index_backed` scope

## What It Surfaces

Current rule families include:

- dead-code candidates:
  - `unreachable_private_symbol`
  - `test_only_helper`
  - `stale_export_candidate`
  - `orphan_file`
  - `test_only_file`
  - `stale_package_candidate`
  - `test_only_package`
  - dead-code reachability now follows private method chains and treats files
    with repoindex `IMPLEMENTS` / `EMBEDS` edges as structural roots for
    method dispatch, which keeps interface-backed methods from being
    misclassified as dead
  - Go package-scope initializer references now produce file-level graph edges,
    so registrations like Cobra `RunE: runFoo` or similar top-level handler
    tables count as live roots for dead-code analysis
  - broad `all` runs now suppress child dead-code rows when a file or package
    dead family already summarizes the same seam; `focus=dead` keeps the full
    detail
  - Elixir call extraction now includes local helper calls as well as module
    references, which materially improves `defp` reachability in index-backed
    dead-code scans
  - Elixir dead-code reachability now also treats callback implementations from
    known framework behaviours and `use` targets *(Application, GenServer,
    Supervisor, Plug, Phoenix Channel/LiveView, Ecto type behaviours)* as live
    structural roots
  - Elixir dead-code candidate generation now avoids same-name type/function
    collisions, so declarations like `@type config` are not misclassified as
    dead functions just because the file also defines `defp config`
  - TypeScript dead-code reachability now includes same-file symbol
    `REFERS_TO` edges for bare identifier usage inside symbol bodies, so cases
    like `useReducer(authPromptReducer, ...)` no longer rely on call-only edges
    to keep reducer-style helpers live
  - Python dead-code analysis is now repoindex-backed on scopes indexed with
    `foxctl index repo build --python`; the first version uses module/file
    reachability plus same-file Python call edges, so it is useful for package
    helpers and script surfaces but still less framework-aware than Go
  - Rust dead-code analysis is now repoindex-backed on scopes indexed with
    `foxctl index repo build --rust`; the first version uses tree-sitter-backed
    Rust symbols when available plus same-file call edges, so crate-wide
    `use`/module reachability still lags Go
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
- `--focus dead` filters toward conservative dead-code candidates backed by
  repoindex reachability, starting with private unreachable helpers, test-only
  helpers, stale exported functions, and narrowly classified dead-ish files and
  packages.
- `refactor scout` now also emits `data.index_mode` so downstream review flows
  can tell whether a run was parser-only or index-backed.
- `--target` narrows returned findings toward a follow-up skill workflow:
  `small-composable-code`, `semantic-commenting`, or
  `improve-codebase-architecture`. The default `all` keeps the normal result
  set and still annotates findings with `targets` and `target_reasons`.
- successful scout runs now emit `data.snapshot_id` and
  `data.snapshot_artifact`
- `data.signals.evidence_backed` tells you whether the snapshot/evidence layer
  completed, independent of whether repoindex was fresh
- parser-only runs still attach scope snapshot evidence and recent churn data
  like `recent_change_count` and `hot_score` on `function_hotspot` findings
- when recent diff ranges overlap the hotspot body, scout now also attaches
  symbol-level churn evidence such as `symbol_hot_score`,
  `symbol_recent_change_count`, and `symbol_changed_line_count`
- scout also attaches file-level co-change evidence such as `cochange_paths`,
  `cochange_count`, and `cochange_strength` when nearby files repeatedly move
  with the hotspot inside the observed git window
- hotspots now also carry a deterministic `suggested_boundary_kind` such as
  `extract_workflow_step` or `extract_error_normalizer` when the rule mix is
  strong enough to name the likely seam
- index-backed runs additionally attach repo-graph evidence such as
  `seed_node_id`, reverse/forward dependency counts, and `suggested_reads`
- when repoindex is fresh, scout now also uses reverse deps, forward deps, and
  symbol/file hotness plus co-change pressure to compute an explicit `opportunity_score` for
  `function_hotspot` findings rather than treating that evidence as display-only
- when hotspot evidence rows are persisted, scout also emits
  `data.evidence_artifact`

## Scout Output Contract

The scout's top-level output now carries:

- `data.index_mode`:
  `parser_only` or `index_backed` for the requested scope
- `data.snapshot_id`:
  stable refactor snapshot handle like `refsnap-...`
- `data.snapshot_artifact`:
  CAS digest for the stored scope snapshot
- `data.evidence_artifact`:
  CAS digest for persisted hotspot evidence when emitted
- `data.signals.evidence_backed`:
  whether the snapshot/evidence pass completed for the run
- `finding.evidence.opportunity_score`:
  deterministic final hotspot score after dependency and churn pressure are
  folded into the structural base score
- `finding.evidence.opportunity_factors`:
  visible bonus breakdown for reverse deps, forward deps, symbol hotness, file
  hotness, co-change pressure, and recent change pressure
- `finding.evidence.suggested_boundary_kind`:
  deterministic seam label inferred from the hotspot's constituent rule family
- `finding.evidence.confidence_score`:
  explicit numeric trust score for the finding, derived from mode, evidence
  completeness, and rule-family-specific factors
- `finding.evidence.confidence_factors`:
  visible confidence contributions so downstream review flows can sort or filter
  without re-deriving trust locally
- `finding.targets` and `finding.target_reasons`:
  deterministic follow-up skill lanes derived from rule IDs, categories, and
  evidence, not keyword matching
- `data.presentation.lanes.skill_targets`:
  grouped top findings for the three follow-up skills:
  `small-composable-code`, `semantic-commenting`, and
  `improve-codebase-architecture`
- `data.dead_code_error`:
  optional advisory error if dead-code candidate generation could not complete

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
  repeated rescue-side or tuple-style clause error remaps. Dead-code reads are
  materially better after local helper-call extraction, callback-root modeling,
  and symbol-kind disambiguation, but they still trail Go on framework-aware
  reachability precision
- TypeScript:
  slop detection is strong on duplicated recovery and repeated guard/remap
  patterns. Dead-code reads are now materially better for reducer/callback
  helpers and nested app workspaces, but they still trail Go on richer
  framework-root modeling

## ACA Fit

The refactor workflow fits into ACA as follows:

- `L0` active run:
  use `foxctl refactor scout` to get the current hotspot surface
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
