# Refactor Intelligence Substrate Plan

Status: active plan

Owner: foxctl

Last updated: 2026-04-01

## Goal

Turn `foxctl refactor scout` from a mostly local parser pass into an
index-aware refactor intelligence workflow with stable evidence, dependency
context, and change-aware prioritization.

The intended outcome is:

- keep `refactor scout` as the deterministic scorer
- reuse the existing repo graph index instead of building a second scout-local
  index
- add a small set of refactor-oriented query primitives that expose the same
  kind of substrate found in fast code intelligence tools *(tree, outline,
  symbol lookup, dependency expansion, hot files, change cursors, snapshots)*
- let each scout/advisor finding carry stronger evidence than local AST
  duplication alone

## Problem

Current `refactor scout` is already useful for local structural smells, but it
is still comparatively weak when answering questions such as:

- which files depend on this hotspot
- how active this code has been recently
- whether a function is isolated slop or a blast-radius seam
- what nearby sibling functions or modules should be read next
- whether a result is based on a fresh repo graph or parser-only fallback

This is not primarily a "missing detector rule" problem. It is a missing
evidence substrate problem.

## Decision

Do **not** build a second parallel "scout index."

Instead:

- treat `repoindex` as the graph/source-of-truth substrate
- add a thin refactor-oriented query layer and CLI surface on top of existing
  graph, symbol, semantic tree, and git/change primitives
- make `refactor scout` consume those primitives when available
- preserve parser-only fallback when the index is missing or stale

This keeps one graph truth, avoids identity drift between tools, and lets the
refactor workflow inherit future repoindex improvements instead of forking them.

## Existing Assets

The repo already has most of the hard primitives:

- repo graph nodes, edges, and metadata in
  [internal/intelligence/indexing/repoindex/types.go](../../../internal/intelligence/indexing/repoindex/types.go)
- repo graph build orchestration in
  [internal/intelligence/indexing/repoindex/builder.go](../../../internal/intelligence/indexing/repoindex/builder.go)
- query/expand/open over the graph in
  [internal/intelligence/indexing/repoindex/query.go](../../../internal/intelligence/indexing/repoindex/query.go)
- graph-to-code anchor projection in
  [internal/intelligence/repoquery/project.go](../../../internal/intelligence/repoquery/project.go)
- user-facing repo graph skills such as
  [skills/repo_index_search/main.go](../../../skills/repo_index_search/main.go)
- semantic tree and retrieval-adjacent reading lanes documented in
  [docs/general/repoindex.md](../../general/repoindex.md) and
  [docs/general/refactor-scout.md](../../general/refactor-scout.md)

The current scout remains largely local:

- symbol extractor registry setup in
  [skills/code_refactor_scout/main.go](../../../skills/code_refactor_scout/main.go)
- local call extraction in
  [skills/code_refactor_scout/main.go](../../../skills/code_refactor_scout/main.go)
- explicit `repo_graph: false` output signal in
  [skills/code_refactor_scout/main.go](../../../skills/code_refactor_scout/main.go)

That split is the seam this plan addresses.

## Design Invariants

1. `refactor scout` remains the deterministic scoring engine.
2. `repoindex` remains the only graph/index source of truth for refactor
   relationship queries.
3. Parser-only fallback must continue to work when the graph is absent or stale.
4. No keyword heuristics for routing or severity decisions.
5. Every new refactor-facing primitive must emit machine-usable output with
   stable fields and code anchors.
6. New commands should be thin wrappers over existing graph/storage capabilities
   where possible, not bespoke one-off implementations.

## Full Surface

This is the target refactor intelligence surface.

| Surface | Purpose | Primary backing |
| --- | --- | --- |
| `refactor status` | show freshness, index coverage, indexed languages, drift, and fallback mode | repoindex meta + git |
| `refactor snapshot` | freeze a scope into a stable file/symbol/hash summary | repoindex + symbol/file counts |
| `refactor tree` | tree view with language, line counts, symbol counts, churn | repoindex file/package nodes + git |
| `refactor outline` | symbols in a file/module with lines and signatures | repoindex symbol nodes or `code/symbols` fallback |
| `refactor symbol` | locate symbol definitions/usages with anchors | repoindex search/open |
| `refactor search` | fast lexical/FTS search for refactor context | repoindex FTS; optional future trigram index |
| `refactor deps` | forward and reverse dependency graph expansion | repoindex expand |
| `refactor hot` | recently hot files/symbols by churn and co-change | git/change stats |
| `refactor changes` | changed files/symbols since ref, cursor, or snapshot | git diff + snapshot delta |
| `refactor evidence` | stable evidence pack per hotspot/seam | snapshot + deps + changes + local findings |
| `refactor scout` | deterministic scoring over local and indexed evidence | scout rules + query substrate |
| `refactor advisor` | optional second-stage sequencing/explanation | scout output + model |

## Mapping to External Tool Shapes

The screenshot-style primitives map cleanly to the refactor surface:

- `_tree` -> `refactor tree`
- `_outline` -> `refactor outline`
- `_symbol` -> `refactor symbol`
- `_search` -> `refactor search`
- `_word` -> optional later lexical helper, lower priority
- `_hot` -> `refactor hot`
- `_deps` -> `refactor deps`
- `_read` -> existing read/context tools; do not duplicate in refactor
- `_edit` -> existing edit tools; do not duplicate in refactor
- `_changes` -> `refactor changes`
- `_status` -> `refactor status`
- `_snapshot` -> `refactor snapshot`

The important distinction is that `refactor` should consume these capabilities
as evidence and navigation helpers, not become a generic editor shell.

## Command Shape

Recommended CLI surface:

```bash
foxctl refactor status --workspace .
foxctl refactor snapshot --path ./internal --language go
foxctl refactor tree --path ./internal --language go
foxctl refactor outline --path internal/intelligence/indexing/repoindex/builder.go
foxctl refactor symbol --query "Builder.Build" --workspace .
foxctl refactor deps --seed "sym:..." --direction in --edge-set structural
foxctl refactor hot --path ./internal --since HEAD~20
foxctl refactor changes --since HEAD~5 --path ./internal
foxctl refactor scout --path ./internal --language go --focus slop
foxctl refactor advisor --path ./internal --language go --focus slop
```

Notes:

- `status`, `snapshot`, `deps`, `hot`, and `changes` are the high-value first
  additions
- `symbol`, `tree`, and `outline` are mostly UX wrappers once the substrate
  exists
- `read` and `edit` remain elsewhere in the tool stack

## Shared Types

Add a small shared internal package or local command-layer types for:

- `Scope`
  - `path`
  - `language`
  - `include_tests`
  - `workspace`
  - `snapshot_id`
- `IndexStatus`
  - `workspace`
  - `repo_root`
  - `head_sha`
  - `indexed_at`
  - `schema_version`
  - `languages`
  - `stale`
  - `reason`
  - `mode` = `index_backed | parser_only`
- `ChangeCursor`
  - `git_ref`
  - `snapshot_id`
  - `sequence`
- `RefactorSnapshot`
  - file list
  - symbol list
  - line counts
  - hashes
  - package/module membership
  - anchor map
- `EvidencePack`
  - hotspot metadata
  - local structural findings
  - dependency expansion summary
  - recent change summary
  - sibling overlap summary
  - suggested read set

## How Scout Changes

Current scout flow is roughly:

1. walk files
2. parse symbols
3. extract local calls
4. score rules
5. synthesize hotspots

Target scout flow becomes:

1. resolve scope
2. load `IndexStatus`
3. build or load a `RefactorSnapshot`
4. gather local structural findings *(existing behavior)*
5. enrich candidate symbols with:
   - reverse deps
   - forward deps
   - file/module context
   - change/hotness data
   - sibling overlap context
6. score and synthesize hotspots
7. emit findings plus optional evidence artifact

This means local AST/tree-sitter rules continue to matter, but the final finding
quality improves because each score is no longer context-blind.

## Follow-On Detection Backlog

The substrate work in this plan is now complemented by a detection-focused
backlog in:

- [Refactor Deterministic Detection Backlog](./refactor-deterministic-detection-backlog.md)

That backlog is the right place for follow-on work such as symbol-level
hotness, opportunity scoring, co-change clustering, stronger dead-code roots,
and other deterministic detection improvements on top of this substrate.

## Evidence Model

Each major finding should be able to attach:

- `anchor`
- `scope_snapshot_id`
- `index_mode`
- `reverse_dep_count`
- `forward_dep_count`
- `recent_change_count`
- `hot_score`
- `same_file_overlap_count`
- `related_symbols`
- `suggested_reads`
- `artifact`

Example shape:

```json
{
  "rule_id": "function_hotspot",
  "score": 92,
  "file": "internal/intelligence/indexing/repoindex/builder.go",
  "symbol": "Builder.Build",
  "evidence": {
    "index_mode": "index_backed",
    "scope_snapshot_id": "refsnap_01...",
    "reverse_dep_count": 7,
    "recent_change_count": 3,
    "hot_score": 0.78,
    "contributing_rules": [
      "duplicate_orchestration_fingerprint",
      "repeated_guard_ladder",
      "same_file_extraction_candidate"
    ],
    "suggested_reads": [
      "internal/intelligence/indexing/repoindex/query.go",
      "internal/intelligence/repoquery/project.go"
    ]
  }
}
```

## Order of Work

### Phase 1 — Freshness and Stable Scope

Deliver:

- `refactor status`
- `refactor snapshot`

Detailed command contract:

- [Refactor Phase 1 Spec: Status and Snapshot](./refactor-phase1-status-and-snapshot.md)

Why first:

- tells users whether scout is parser-only or index-backed
- creates a stable scope/evidence boundary for later commands
- enables deterministic "since snapshot" comparisons later

Acceptance:

- status reports indexed languages, head SHA, and staleness reason
- snapshot stores file/symbol/hash summary for a scoped run
- scout can emit `index_mode`

### Phase 2 — Dependency and Change Context

Deliver:

- `refactor deps`
- `refactor changes`

Why second:

- these are the biggest robustness gains for hotspot triage
- they make findings explain blast radius and recency

Acceptance:

- reverse dependency expansion works from file/symbol anchors
- changes can diff against git refs and snapshot IDs
- scout can enrich findings with dep counts and change counts

### Phase 3 — Evidence-Backed Scout

Deliver:

- index-aware `refactor scout`
- `refactor evidence`

Why third:

- only once status/snapshot/deps/changes exist does scout have enough substrate
  to become materially more robust

Acceptance:

- scout keeps parser-only fallback
- scout emits `index_backed` findings when the graph is fresh
- major hotspots include evidence artifacts and suggested reads

### Phase 4 — Triage and Navigation UX

Deliver:

- `refactor hot`
- `refactor tree`
- `refactor outline`
- optional `refactor symbol`

Why fourth:

- useful for operator UX, but not required to improve scout scoring

Acceptance:

- tree and outline show stable anchorable context
- hot surfaces churn-aware top files/symbols

### Phase 5 — Optional Retrieval Upgrades

Deliver:

- optional trigram search backend for `refactor search`
- optional word lookup helper
- optional incremental/live cursor support

Why last:

- these are optimizations, not the core missing robustness seam

## Recommended First Slice

If only one thin slice is tackled next, do:

1. `refactor status`
2. `refactor snapshot`
3. attach `index_mode` and `snapshot_id` to scout output

That is the smallest change that:

- makes the workflow introspectable
- establishes deterministic scope snapshots
- prepares the codebase for dep/change-aware scoring without overcommitting to
  the entire command surface at once

## Risks

- duplicated concepts across `refactor`, `repo index`, and `semantic_search`
  if wrappers become too thick
- stale graph data producing misleading scout evidence if `status` is weak
- mixed-language scope ambiguity if snapshot/status do not remain
  single-language-aware where required
- overbuilding search niceties before adding the high-value primitives
  (`status`, `snapshot`, `deps`, `changes`)

## Non-Goals

- replacing `repoindex` with a new refactor-specific store
- turning `refactor` into a general-purpose file read/edit API
- requiring a fresh repo graph for every scout run
- removing existing local AST/tree-sitter rules

## Success Criteria

The plan is successful when:

- users can tell whether scout findings are parser-only or index-backed
- hotspot findings include dependency and recency evidence when available
- repeated refactor runs on large repos become more reproducible through
  snapshots
- advisor recommendations are stronger because their inputs have better evidence
  rather than just more model interpretation
