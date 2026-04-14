# Retrieval/Search Refactor Plan

> **Goal:** Rebuild code search around one retrieval pipeline so lexical recall, vector recall, fusion, grouping, tree output, and snippet extraction each have a single owner.

## Problem Statement

> Status note (2026-03-07): the main code-search migration described below is now largely implemented on this branch. The historical references to `internal/intelligence/retrieval/candidates.go` and the legacy generator path are retained here for change history, not as the current runtime architecture.

Code search is currently spread across several overlapping paths:

- `internal/storage/memory/search.go` and `internal/storage/dbdriver/search.go` mix backend recall with hybrid ranking logic.
- `internal/intelligence/retrieval/candidates.go` merges symbol, semantic, and ripgrep candidates after collapsing too early to file-path-oriented candidates.
- `skills/code_smart_search/main.go` generates candidates, then shells out to `code/snippet_extract` as a second-stage pipeline.
- `skills/code_snippet_extract/main.go` owns its own extraction, fallback, rendering, and related-session logic.
- `skills/code_semantic_search/main.go` still carries separate tree-building and snippet-reading logic, including direct file reads.

The result is one user-facing capability with too many competing implementations:

- ranking logic lives in multiple layers
- vector semantics are ambiguous across backends
- file grouping happens before ranking is complete
- anchor information is lost on the way to snippet extraction
- query parsing drifts between retrieval and extraction stages
- tree mode is too close to retrieval instead of being a projection over ranked file hits

## Scope

This refactor covers **code retrieval only**:

- symbol docs
- file-summary docs
- lexical recall
- vector recall
- ripgrep-backed line anchors
- grouped file hits
- tree projection
- anchor-aware snippet extraction
- the skill wrappers for `code/semantic_search`, `code/smart_search`, and `code/snippet_extract`

## Non-goals

- Do not fold `repoindex` into the new retrieval index in the first pass.
- Do not migrate sessions, memories, tasks, or codemaps into the new `searchindex` yet.
- Do not introduce Tree-sitter in this refactor.
- Do not delete the legacy retrieval path until parity tests pass.

## Current State Assessment

### Current entrypoints

- Retrieval candidates are produced by `internal/intelligence/retrieval/candidates.go`.
- Shared code-context types and rendering already exist in `internal/intelligence/codecontext/types.go`, `internal/intelligence/codecontext/collect.go`, and `internal/intelligence/codecontext/render.go`.
- Safe file reading already exists in `internal/intelligence/codecontext/files/reader.go`.
- `skills/code_smart_search/main.go` still treats retrieval as candidate generation plus a second skill invocation.
- `skills/code_snippet_extract/main.go` still owns code extraction logic directly.
- `skills/code_semantic_search/main.go` still has a separate tree path and direct snippet extraction path.

### Problems to remove

1. Storage-layer hybrid search
   - Backend code should expose lexical recall and vector recall only.
   - Fusion and reranking belong in retrieval, not in DB helpers.

2. Early path-level collapse
   - The current `retrieval.Candidate` model is file-oriented too early.
   - Multiple relevant symbols in the same file cannot survive cleanly through the pipeline.

3. Duplicate snippet extraction logic
   - `internal/intelligence/codecontext` exists, but `skills/code_snippet_extract/main.go` still implements extraction/fallback/rendering itself.

4. Ambiguous vector score semantics
   - Distance and similarity handling are not explicit enough at backend boundaries.

5. Tree output mixed with retrieval
   - Tree mode should be a projection over ranked file hits, not a separate ranking path.

## Design Invariants

These rules are non-negotiable for the new design:

1. Cross-source fusion is rank-based, not raw-score-based.
2. Vector semantics are explicit at the backend boundary.
3. Storage/index code performs recall only; retrieval performs fusion and grouping.
4. Node ranking happens before file grouping.
5. File grouping preserves multiple anchors per file.
6. Tree mode is a view over ranked file hits, not a separate search algorithm.
7. Snippet extraction consumes anchors, not just file paths.
8. Query planning is shared by retrieval and extraction.
9. Skills remain thin wrappers over internal packages.

## Target Architecture

```text
internal/intelligence/searchquery/
  plan.go           // shared terms/identifiers/phrases/path-hints planner

internal/intelligence/searchindex/
  model.go          // typed retrieval document model
  store.go          // lexical/vector recall API
  sql_store.go      // initial SQL-backed implementation
  build_code.go     // bootstrap docs from current symbol/file-summary data

internal/intelligence/retrieval/
  engine.go         // orchestration
  sources_*.go      // lexical/vector/ripgrep recall sources
  fuse.go           // RRF + feature reranking
  group.go          // node hits -> file hits with anchors
  tree.go           // file hits -> tree projection
  legacy_adapter.go // temporary bridge for Generator callers

internal/intelligence/codecontext/
  collect.go        // anchor-aware evidence collection
  proposals.go      // anchor/query-based snippet proposals
  scoring.go        // local snippet scoring and dedupe
  output.go         // inline + artifact output policy
  types.go          // evidence request/response model
  adapters/
    retrieval.go    // FileHit -> codecontext candidate bridge

skills/
  code_semantic_search/  // thin wrapper over retrieval.Engine
  code_smart_search/     // retrieval.Engine + codecontext.CollectEvidence
  code_snippet_extract/  // legacy-compatible wrapper over codecontext
```

### Ownership model

- `searchindex` owns typed retrieval documents and primitive recall.
- `retrieval` owns planning, fusion, reranking, grouping, and tree output.
- `codecontext` owns evidence extraction from ranked file hits and anchors.
- `searchquery` owns shared query parsing and lightweight textual matching helpers.
- skills own input normalization and output envelope compatibility only.

## Retrieval Document Model

The new index should search typed retrieval docs, not raw `named_memory` rows.

Core fields:

- `id`
- `workspace_id`
- `scope`
- `kind`
- `group_key`
- `path`
- `symbol_id`
- `symbol_name`
- `title`
- `summary`
- `search_text`
- `keywords`
- `anchor`
- `metadata`
- optional `embedding` and `embedding_model`

For code docs:

- symbol docs group by file path and carry symbol anchors
- file docs group by file path and carry file anchors

This replaces path recovery from opaque names and keeps grouping explicit.

## Implementation Phases

### Phase 0: correctness hotfixes in the current stack

**Goal:** Stabilize vector semantics and remove the most dangerous ambiguity before the larger refactor lands.

Files:

- `internal/storage/dbdriver/vector.go`
- `internal/storage/dbdriver/vector_postgres.go`
- `internal/storage/dbdriver/search.go`
- `internal/storage/memory/search.go`
- `internal/storage/memory/vector.go`

Changes:

- make metric contracts explicit
- distinguish distance from similarity
- stop relying on guessed score direction
- keep any existing hybrid path as compatibility-only logic

Acceptance criteria:

- vector recall sorts correctly for each backend
- normalization is explicit in code and tests
- no new retrieval logic is added in storage after this phase

### Phase 1: add `internal/intelligence/searchindex`

**Goal:** Create a dedicated retrieval document index beside the legacy storage paths.

Files to add:

- `internal/intelligence/searchindex/model.go`
- `internal/intelligence/searchindex/store.go`
- `internal/intelligence/searchindex/sql_store.go`
- `internal/intelligence/searchindex/build_code.go`

Changes:

- define the retrieval `Document` model
- add lexical/vector recall APIs that return raw hits
- add a SQL-backed initial store
- bootstrap symbol and file docs from current symbol/file-summary data

Acceptance criteria:

- code symbol docs and file docs can be built for a workspace
- lexical recall works without the new retrieval engine
- vector recall can be enabled without mixing in fusion logic

### Phase 2: add `internal/intelligence/searchquery`

**Goal:** Share one query-planning path between retrieval and extraction before the codecontext rewrite lands.

Files to add:

- `internal/intelligence/searchquery/plan.go`

Changes:

- extract terms, identifiers, phrases, and path hints once
- share lexical-query construction between retrieval sources
- add lightweight text-match scoring helpers for codecontext fallback scoring

Acceptance criteria:

- retrieval and codecontext use the same parsed query model
- no new skill-local token parsing is introduced in new code
- phrase, identifier, and path-hint behavior is covered by unit tests

### Phase 3: add retrieval v2 beside the legacy generator

**Goal:** Add a new retrieval engine without breaking current skill paths.

Files to add/reshape:

- `internal/intelligence/retrieval/engine.go`
- `internal/intelligence/retrieval/sources_symbols.go`
- `internal/intelligence/retrieval/sources_files.go`
- `internal/intelligence/retrieval/sources_ripgrep.go`
- `internal/intelligence/retrieval/fuse.go`
- `internal/intelligence/retrieval/group.go`
- `internal/intelligence/retrieval/tree.go`
- optional `internal/intelligence/retrieval/legacy_adapter.go`

Changes:

- parse queries into terms, identifiers, phrases, and path hints
- recall symbol docs, file docs, and ripgrep anchors independently
- fuse with weighted RRF
- apply small explicit feature bonuses
- group node hits into file hits while preserving multiple anchors
- build trees from grouped file hits only

Acceptance criteria:

- a file with two relevant symbols preserves both anchors
- tree output is derived from file hits, not raw candidates
- ripgrep contributes line anchors instead of acting as a separate late-stage ranking system

### Phase 4: make the existing generator a compatibility wrapper

**Goal:** Keep current callers alive while routing behavior through retrieval v2.

Files:

- `internal/intelligence/retrieval/candidates.go`
- `internal/intelligence/retrieval/options.go`

Changes:

- keep `Generator.Generate(...)` public
- adapt retrieval v2 file hits back into legacy `Candidate` values temporarily
- preserve the best symbol anchor when emitting legacy candidates

Acceptance criteria:

- existing callers of `retrieval.Generator` continue to compile
- `code/smart_search` can move to retrieval v2 without a flag day

### Phase 5: unify snippet extraction under `internal/intelligence/codecontext`

**Goal:** Make `internal/intelligence/codecontext` the only owner of code evidence extraction.

Files to add/modify:

- `internal/intelligence/codecontext/types.go`
- `internal/intelligence/codecontext/collect.go`
- `internal/intelligence/codecontext/proposals.go`
- `internal/intelligence/codecontext/scoring.go`
- `internal/intelligence/codecontext/output.go`
- `internal/intelligence/codecontext/adapters/retrieval.go`

Changes:

- keep `files`, `expander`, and `guard` as the stable support layers
- add an evidence request that consumes retrieval file hits and anchors
- normalize legacy `Candidate` fields into anchors
- prefer symbol anchors, then line anchors, then file/keyword fallback
- generate local snippet proposals from anchors and query matches
- score and dedupe proposals inside `codecontext`
- deduplicate overlaps after extraction
- centralize inline-vs-artifact output policy
- remove extraction/fallback/rendering ownership from `skills/code_snippet_extract/main.go`

Acceptance criteria:

- no new extraction logic is added to skills
- structural fallback is better than “top of file”
- safe file reading continues to use `files.NewSafeReader(...)`
- multiple anchors in the same file can yield multiple proposals/snippets
- output preparation works in-package without skill-local artifact logic

### Phase 6: cut over skills incrementally

**Goal:** Replace skill-local ranking/extraction logic with the new internal pipeline.

Cutover order:

1. `skills/code_snippet_extract/main.go`
2. `skills/code_smart_search/main.go`
3. code scopes inside `skills/code_semantic_search/main.go`

Changes:

- `code/snippet_extract` becomes a legacy-compatible wrapper
- `code/smart_search` becomes in-process retrieval-engine + codecontext orchestration
- remove subprocess invocation from `code/smart_search`
- `code/semantic_search` uses retrieval v2 for code scopes and tree mode

Acceptance criteria:

- `code/smart_search` no longer loses multi-anchor files
- `code/smart_search` no longer shells out to `foxctl run code/snippet_extract`
- `code/snippet_extract` remains envelope-compatible
- tree mode shows the same ranked file set as non-tree mode

### Phase 7: remove legacy retrieval paths

**Goal:** Delete old search/fusion code only after the new path reaches parity.

Candidates for removal after verification:

- legacy merge helpers in `internal/intelligence/retrieval`
- old symbol/semantic-specific ranking paths that the new engine replaces
- duplicated extraction logic in `skills/code_snippet_extract/main.go`

## Parallel Workstreams

These are good `-spark` slices after the plan is approved:

1. Vector semantics hotfix
   - write scope: `internal/storage/dbdriver/*`, `internal/storage/memory/*`
   - objective: explicit metric contract and correct ordering

2. Searchindex skeleton
   - write scope: `internal/intelligence/searchindex/*`
   - objective: model, store interface, SQL store, bootstrap builder

3. Shared query planner
   - write scope: `internal/intelligence/searchquery/*`
   - objective: one parsed query model reused by retrieval and codecontext

4. Retrieval v2 skeleton
   - write scope: `internal/intelligence/retrieval/{engine.go,sources_*.go,fuse.go,group.go,tree.go}`
   - objective: compile-ready engine with tests

5. Codecontext extraction consolidation
   - write scope: `internal/intelligence/codecontext/{collect.go,proposals.go,scoring.go,output.go,adapters/*}`
   - objective: `CollectEvidence` and anchor-aware extraction path

6. Skill cutover wrappers
   - write scope: `skills/code_smart_search/main.go`, `skills/code_snippet_extract/main.go`, `skills/code_semantic_search/main.go`
   - objective: move orchestration to internal packages while preserving envelopes

## Validation Plan

### Unit tests

- vector metric normalization by backend contract
- query-plan extraction for identifiers, phrases, and path hints
- weighted RRF fusion
- multi-anchor file grouping
- tree projection from file hits
- snippet extraction preference order
- in-process smart-search orchestration without subprocess invocation
- legacy candidate adaptation

### End-to-end tests

Add at least one golden path covering:

- query resolves a top file
- top file retains symbol anchors
- snippet extraction returns the symbol body rather than file start

## Risks and Rollout Notes

### Main risks

1. Carrying old and new retrieval paths in parallel for too long
2. Breaking skill envelopes while improving internal architecture
3. Accidentally reintroducing ranking logic into storage-layer helpers
4. Letting tree mode diverge from ranked file hits again
5. Re-implementing query parsing separately inside codecontext or skills

### Rollout strategy

- land the new system beside the old one
- keep compatibility wrappers during migration
- cut over one skill at a time
- remove legacy code only after parity tests and real usage checks

## Definition of Done

- [ ] Storage/vector semantics are explicit and tested
- [ ] `internal/intelligence/searchindex` exists and indexes code docs
- [ ] `internal/intelligence/searchquery` exists and is shared by retrieval and codecontext
- [ ] retrieval v2 exists and produces ranked node hits and grouped file hits
- [ ] file grouping preserves multiple anchors
- [ ] tree mode is a pure projection over file hits
- [ ] `internal/intelligence/codecontext` owns code evidence extraction
- [ ] `code/smart_search` uses retrieval v2 + codecontext
- [ ] `code/smart_search` no longer shells out to `code/snippet_extract`
- [ ] `code/snippet_extract` is a compatibility wrapper
- [ ] `code/semantic_search` code scopes use retrieval v2
- [ ] legacy retrieval paths scheduled for removal with tests in place

## Immediate Next Step

Start with **Phase 0**, **Phase 1**, and **Phase 2** only.

That gives a safe base for later parallel work:

- correct metric semantics first
- then add the new retrieval document index
- then add the shared query planner

After that, we can hand off the retrieval-v2 skeleton and codecontext consolidation to separate `-spark` agents without them tripping over unstable scoring behavior.
