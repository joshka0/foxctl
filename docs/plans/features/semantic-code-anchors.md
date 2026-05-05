# Semantic Code Anchors

Status: Proposed  
Owner: Solo maintainer  
Last Updated: 2026-05-05

## Goal

Turn high-signal code comments into typed semantic anchors that improve repo
graph search, ACA retrieval, embeddings, review, and agent memory without
turning source files into long-form memory storage.

The core idea is:

```go
// [[foxctl:invariant/no-send-without-read]]
// [[foxctl:risk/agent-terminal-desync]]
// [[foxctl:test/tmuxbridge-requires-read-before-type]]
func (b *Bridge) Type(...) error {
	...
}
```

Those comments are not just notes. They are durable graph edges and embedding
anchors. They let agents find code by product, protocol, or safety intent, not
only by symbol names and raw code tokens.

The target retrieval path is:

```text
source anchor -> repo graph edge -> semantic envelope -> embedding -> ACA retrieval
```

## Existing System Fit

This plan should extend the current ACA, repoindex, and embedding stack rather
than create a separate `agentgraph` system.

Relevant existing pieces:

- [AgentCTL context architecture](../../architecture/context-architecture.md)
  already separates the ACA control plane from the durable Obsidian knowledge
  plane.
- [Memory](../../general/memory.md) documents named memory persistence,
  workspace scoping, optional embeddings, and `code/semantic_search` memory
  scope.
- [Repo graph index](../../general/repoindex.md) already stores packages,
  files, symbols, concept nodes, structural edges, and comment-derived soft
  edges.
- `internal/intelligence/indexing/repoindex/comment_edges.go` already parses
  structured `Index:` doc comments into concept nodes and soft edges.
- `internal/intelligence/indexing/embeddingtext` already builds symbol embedding
  text from symbol metadata, docs, relationships, aliases, and optional code.
- `internal/intelligence/searchindex` already has typed code retrieval
  documents with anchors, keywords, metadata, and embeddings.
- `internal/storage/obsidianindex` already indexes vault notes, headings, links,
  repo paths, repo symbols, note embeddings, and chunk embeddings.
- `internal/context/memorycore` already has lifecycle, trust, provenance, and
  telemetry envelopes for memory records.
- `internal/context/contextplane` already has typed proposal governance and
  retrieval correction machinery.

The existing `Index:` comment syntax is a useful prototype. Semantic anchors
should become the stricter, typed, human-friendly successor while keeping
backward compatibility with `Index:` during migration.

## Non-Goals

- Do not make comments the memory store.
- Do not let agents add long explanations, speculation, or analysis graffiti to
  source files.
- Do not replace tests, docs, ADRs, or Obsidian notes with inline anchors.
- Do not make embeddings mandatory for correctness. Graph and lexical behavior
  must still work.
- Do not add a new top-level storage or retrieval stack before extending
  repoindex/searchindex.
- Do not use ad hoc keyword heuristics to assign authority, promote anchors, or
  classify lifecycle.
- Do not put anchor extraction, graph indexing, or retrieval code under
  `internal/v2`.

## Hard Invariants

Reviewers should reject implementation patches that violate these invariants:

1. Inline source comments contain stable anchors only, not long-form memory.
2. Anchor authority is evidence-level until backed by docs, tests, or reviewed
   memory.
3. Semantic anchors are typed and parseable. Freeform "important" comments do
   not enter the graph.
4. Anchor extraction is deterministic and testable.
5. Anchor edges never override structural code facts from AST/repoindex.
6. Existing `Index:` comments remain readable during migration.
7. Embedding text is generated from a deterministic semantic envelope, not from
   prompt-time freeform synthesis.
8. Anchor proposals are reviewed through ACA/memorycore lifecycle before agents
   edit source comments automatically.
9. Anchor IDs must not include secrets, tokens, user PII, terminal output, or
   transient session IDs.
10. Package placement follows the intelligence/context/storage families, not
    `internal/v2`.

## Terminology

### Semantic Anchor

A small typed link embedded near code:

```ts
// [[invariant:mutuality-over-unilateral-access]]
// [[risk:premature-unilateral-access]]
```

It identifies a stable concept and lets the index create graph edges.

### Anchor Target

The node the anchor names, such as:

- `invariant:no-send-without-read`
- `risk:agent-terminal-desync`
- `test:tmuxbridge-requires-read-before-type`
- `doc:docs/general/tmux-collaboration.md#room-access`

### Anchor Occurrence

One physical occurrence of an anchor in a file at a line/span, bound to the
nearest symbol or to the file if no symbol binding is available.

### Semantic Envelope

A generated embedding document for one code node. It combines symbol metadata,
doc comments, semantic anchors, graph neighbors, linked docs/tests, and a capped
code excerpt.

### Anchor Proposal

A reviewed ACA proposal to add, remove, or change anchors. Agents may propose
anchor changes, but source edits should stay visible in normal diffs.

### Curator

The lifecycle process that detects stale, vague, duplicated, orphaned, or
unused anchors and proposes cleanup.

## Anchor Syntax

Support two forms.

### Inline Wikilink Form

This is the ergonomic daily-use form:

```go
// [[foxctl:invariant/no-send-without-read]]
// [[foxctl:protocol/read-guard]]
// [[foxctl:risk/agent-terminal-desync]]
// [[foxctl:test/tmuxbridge-requires-read-before-type]]
```

Generic form:

```text
[[scope:type/slug]]
[[type:slug]]
[[doc:path/to/doc.md#heading]]
```

Rules:

- `scope` is optional and names the project or domain.
- `type` is required.
- `slug` must be stable, lowercase, and human-readable.
- `doc:` targets may reference repo docs or reviewed vault note paths.
- Multiple anchors may appear above one symbol.
- Anchors bind to the nearest following symbol when possible.

### Block Relation Form

This form is more explicit and better for generated code or dense contracts:

```go
/*
Semantic:
  enforces:
    - invariant:no-send-without-read
  participates_in:
    - protocol:agent-terminal-safety
  protects_against:
    - risk:agent-terminal-desync
  verified_by:
    - test:tmuxbridge-requires-read-before-type
  described_by:
    - doc:docs/general/tmux-collaboration.md#room-access
*/
```

The parser may also support the existing `Index:` doc block as a compatibility
source:

```go
// Index:
// - Purpose: Update semantic file embeddings for post-review changes
// - Related: deleteFileEmbedding, indexFile
// - Keywords: semantic_file_index, embeddings, post_review
```

Compatibility mapping:

| `Index:` field | Anchor relation |
|------|------------------|
| `Purpose` | envelope summary hint |
| `Keywords` | `related_to` concept nodes |
| `Related` | `related_to` symbol edges |
| `Flow` | `flows_to` symbol edges |
| `Resources` | `touches` resource nodes |
| `Events` | `emits` event nodes |
| `OutputFields` | `outputs` field nodes |

## Anchor Taxonomy

Start with a small vocabulary.

| Anchor type | Meaning | Default relation from code |
|------|---------|----------------------------|
| `invariant` | behavior that must remain true | `enforces` |
| `principle` | higher-level design/product intent | `implements_principle` |
| `protocol` | wire/runtime/process contract | `implements_protocol` |
| `risk` | known failure mode | `protects_against` |
| `test` or `test-contract` | behavior verification | `verified_by` |
| `decision` | ADR or explicit decision record | `decided_by` |
| `doc` | documentation anchor | `described_by` |
| `beacon` | retrieval beacon for common agent tasks | `beacon_for` |
| `domain` | product or runtime domain concept | `participates_in` |
| `event` | runtime event emitted/handled | `emits` or `handles` |
| `resource` | external resource, DB table, API, config, queue | `touches` |
| `policy` | reviewed policy memory or runtime rule | `governed_by` |

Do not add new anchor types casually. New types need a parser test, relation
mapping, retrieval behavior, and at least one usage example.

## Graph Model

The first implementation should extend repoindex.

### Nodes

Existing repoindex nodes:

- package
- file
- symbol
- concept

Semantic anchor nodes can initially use concept nodes with typed prefixes:

```text
anchor:invariant:no-send-without-read
anchor:risk:agent-terminal-desync
anchor:test:tmuxbridge-requires-read-before-type
anchor:doc:docs/general/tmux-collaboration.md#room-access
```

If concept nodes become too overloaded, a later migration can introduce a
dedicated `semantic_anchor` node kind.

### Edges

Add typed soft edges. Candidate edge types:

```text
ENFORCES
IMPLEMENTS_PRINCIPLE
IMPLEMENTS_PROTOCOL
PARTICIPATES_IN
PROTECTS_AGAINST
VERIFIED_BY
DESCRIBED_BY
DECIDED_BY
BEACON_FOR
TOUCHES_RESOURCE
EMITS_EVENT
HANDLES_EVENT
ANCHOR_RELATED
```

Rules:

- Structural edges such as `CALLS`, `REFERS_TO`, and `IMPORTS` remain separate.
- Anchor edges carry lower default weight than structural edges.
- Edges store occurrence metadata: file, line, syntax form, confidence, and
  extraction version.
- Missing or unresolved doc/test targets create lint findings, not hard index
  failures.

### Occurrences

The index should preserve occurrence metadata even when multiple symbols point
to the same anchor target:

```json
{
  "workspace_id": "repo:/Users/joshka/repos/personal/foxctl",
  "path": "internal/runtime/terminal/tmuxbridge/bridge.go",
  "line": 42,
  "owner_node": "sym:go:...:Bridge.Type",
  "target": "foxctl:invariant/no-send-without-read",
  "relation": "enforces",
  "syntax": "wikilink",
  "status": "active"
}
```

## Semantic Envelope Contract

Embeddings should target deterministic semantic envelopes instead of raw code
chunks alone.

Example envelope:

```md
# Symbol: Bridge.Type

Kind: method
Path: internal/runtime/terminal/tmuxbridge/bridge.go
Package: internal/runtime/terminal/tmuxbridge

Summary:
Sends terminal input through the tmux bridge.

Semantic anchors:
- enforces [[foxctl:invariant/no-send-without-read]]
- protects_against [[foxctl:risk/agent-terminal-desync]]
- verified_by [[foxctl:test/tmuxbridge-requires-read-before-type]]

Related docs:
- docs/general/tmux-collaboration.md#room-access

Graph neighbors:
- calls Bridge.Read
- called_by agent terminal control path

Review cautions:
- Do not remove read-before-write behavior without replacing the safety
  invariant and tests.

Code excerpt:
...
```

Rules:

- Envelope generation is deterministic.
- It uses normalized anchor IDs and sorted lists for stable digests.
- It includes a capped code excerpt, never unbounded files.
- It includes reviewed external notes only as compact summaries or links.
- It excludes secrets, terminal output, and unreviewed agent speculation.
- The embedding digest includes code digest, anchor digest, doc summary digest,
  and selected graph-neighborhood digest.

## ACA Integration

Semantic anchors should become another high-signal input to ACA retrieval, not a
replacement for ACA.

### Retrieval

`context retrieve` can blend:

- top-of-mind
- latest handoff
- observations and tensions
- Obsidian note hits
- repoindex path and symbol hints
- semantic anchor graph hits
- semantic envelope vector hits
- co-change and repo motif priors

Example query:

```text
agents keep typing into the wrong terminal pane
```

Expected retrieval path:

```text
query -> beacon:agent-terminal-safety
      -> risk:agent-terminal-desync
      -> invariant:no-send-without-read
      -> Bridge.Type / terminal control code
      -> linked tests and docs
```

### Proposal Governance

ACA should represent anchor changes as typed proposals before automatic source
edits:

```text
semantic_anchor_patch
```

Proposal examples:

- add an invariant anchor to a trust-critical function
- remove a stale test anchor whose target no longer exists
- split a vague `beacon` into a precise `risk` and `invariant`
- add a missing doc backlink for an anchor used by several symbols

### Retrieval Inspection

`context retrieve-inspect` and retrieval evals can classify misses as:

- missing anchor
- stale anchor
- orphaned anchor target
- ambiguous anchor taxonomy
- envelope missing linked docs/tests
- graph edge exists but ranker underweights it

## Obsidian and Docs Integration

Keep durable reasoning outside source files.

Inline source:

```go
// [[foxctl:invariant/no-send-without-read]]
```

Durable note or doc:

```md
# No Send Without Read

repo_anchors:
  - foxctl:invariant/no-send-without-read
repo_symbols:
  - internal/runtime/terminal/tmuxbridge.Bridge.Type
repo_docs:
  - docs/general/tmux-collaboration.md
```

Rules:

- Source anchors are small ports into the graph.
- Obsidian notes or repo docs carry long-form explanation.
- `obsidian graph build` may generate concept-note drafts for heavily used
  anchors.
- `obsidian bridge reconcile` can suggest doc/vault backlinks for anchor nodes.
- Generated notes remain inbox-first until reviewed.

## Agent Workflow

When an agent edits code in an anchor-aware repo:

1. Read nearby anchors for the symbols being touched.
2. Retrieve linked invariants, risks, docs, decisions, and test contracts.
3. Edit code.
4. Run the tests linked by `test` or `test-contract` anchors when feasible.
5. Run anchor lint/stale checks for touched files.
6. Emit a graph diff in the final or PR summary.
7. Propose new anchors only when they encode a stable invariant, risk, test
   contract, decision, protocol, or retrieval beacon.

Example graph diff:

```text
Added:
  Bridge.Type protects_against foxctl:risk/agent-terminal-desync

Verified:
  foxctl:invariant/no-send-without-read by tmuxbridge-requires-read-before-type

Warning:
  foxctl:protocol/agent-terminal-safety has no doc anchor
```

## Commands

First useful surfaces:

```bash
foxctl index anchors lint --workspace .
foxctl index anchors explain --workspace . --path internal/runtime/terminal/tmuxbridge/bridge.go
foxctl index repo build --workspace . --semantic-anchors
foxctl run code/semantic_search --input '{"query":"agent terminal safety","scope":["symbols"],"use_semantic_anchors":true}'
foxctl context retrieve "agent terminal safety"
```

Later:

```bash
foxctl index anchors query --workspace . "where do we enforce read before write?"
foxctl index anchors stale --workspace .
foxctl context anchors propose --workspace . --path internal/... --kind semantic_anchor_patch
foxctl obsidian graph build --include-anchor-concepts
```

The exact command names can change, but the split should remain:

- index/lint/explain belongs with `foxctl index`
- context retrieval and proposals belong with `foxctl context`
- vault note generation belongs with `foxctl obsidian`

## Package Placement

Follow the package topology boundary:

| Concern | Target package family |
|------|------------------------|
| Anchor syntax parser and normalizer | `internal/intelligence/indexing/semanticanchors` or repoindex parser package |
| Repo graph anchor nodes/edges | `internal/intelligence/indexing/repoindex` |
| Semantic envelope generation | `internal/intelligence/indexing/embeddingtext` and `internal/intelligence/searchindex` |
| Anchor lint/explain CLI | `cmd/foxctl/cmd` calling intelligence packages |
| ACA retrieval blending and proposals | `internal/context/contextplane` |
| Memory lifecycle/trust projection | `internal/context/memorycore` |
| Durable SQL tables if needed | `internal/storage/*` |
| Obsidian note/bridge integration | `internal/tooling/tools/obsidian` and `internal/storage/obsidianindex` |

Do not place this under `internal/v2`.

## Data Model Direction

### V0: Repoindex Extension

Use existing repoindex nodes and edges:

- concept nodes for anchor targets
- soft edges for anchor relationships
- edge metadata for occurrence details

This is enough to validate parsing, graph search, and retrieval value.

### V1: Anchor Tables if Needed

If occurrence lifecycle, lint, or curation outgrows edge metadata, add a small
storage projection:

```sql
semantic_anchor_occurrences(
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  path TEXT NOT NULL,
  owner_node_id TEXT NOT NULL,
  line INTEGER NOT NULL,
  syntax TEXT NOT NULL,
  relation TEXT NOT NULL,
  target TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_slug TEXT NOT NULL,
  confidence REAL NOT NULL,
  status TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  extracted_at TEXT NOT NULL
);

semantic_anchor_targets(
  workspace_id TEXT NOT NULL,
  target TEXT NOT NULL,
  target_type TEXT NOT NULL,
  slug TEXT NOT NULL,
  title TEXT,
  canonical_note_path TEXT,
  review_status TEXT NOT NULL,
  lifecycle_state TEXT NOT NULL,
  PRIMARY KEY(workspace_id, target)
);
```

Use SQLite/libSQL-compatible SQL first. Do not require Neo4j.

## Lint Rules

Anchor lint should catch:

- unknown anchor type
- malformed target
- uppercase or unstable slug
- anchor not bound to a symbol or file
- duplicate anchors on one owner
- doc target path does not exist
- test target cannot be resolved
- vague `beacon` with no linked invariant/risk/doc
- inline long-form agent note
- anchor target with no usages
- anchor occurrence whose owner symbol disappeared after refactor

Lint output should be machine-readable and human-readable.

## Curation Rules

Curator can propose changes when anchors are:

- unused in retrieval
- stale after refactor
- contradicted by tests or code
- duplicated across nearby symbols
- too vague to improve search
- missing linked docs or tests
- attached to generated/vendor code

Curator must not silently rewrite source. It should create ACA proposals or
normal diffs for review.

Lifecycle states can reuse the memorycore spirit:

```text
candidate -> active -> stale -> archived
candidate -> rejected
active -> deprecated -> superseded
```

## Implementation Sequence

### PR-A: Anchor Syntax Parser and Linter

Goal: prove the anchor contract without changing retrieval behavior.

Tasks:

- add parser for `[[scope:type/slug]]`, `[[type:slug]]`, and `doc:` targets
- parse block relation form
- keep existing `Index:` parser untouched or wrap it as compatibility input
- add normalization and validation rules
- add linter for malformed, duplicate, unknown, and unbound anchors
- add tests for Go, TypeScript, Markdown doc targets, and malformed inputs

No repoindex storage changes yet.

### PR-B: Repoindex Anchor Edges

Goal: make anchors visible in the graph.

Tasks:

- extend repoindex comment edge extraction to emit anchor concept nodes
- add typed anchor edge relations
- store occurrence metadata on edges
- expose anchor edges through repoindex search/expand/DAG grep
- add fixture tests for symbol binding and edge traversal
- document `foxctl index repo build --semantic-anchors` or equivalent behavior

Keep existing `Index:` edges working.

### PR-C: Semantic Envelope Builder

Goal: embed richer retrieval documents.

Tasks:

- extend `embeddingtext.SymbolInfo` or add a neighboring envelope type with
  semantic anchors, linked docs/tests, and selected graph neighbors
- update searchindex document building to include anchor text and metadata
- include anchor digest in embedding invalidation
- add deterministic envelope golden tests
- prove raw code plus anchors retrieves better than raw code alone on a small
  fixture suite

### PR-D: ACA Retrieval Blend

Goal: route task queries through anchors when useful.

Tasks:

- add anchor graph hits to `context retrieve`
- expose anchor-derived code hints in retrieval inspect output
- classify retrieval misses caused by missing/stale anchors
- add an eval suite with queries like "where is read-before-write enforced?"
- keep lexical and graph behavior useful when embeddings are unavailable

### PR-E: Anchor Proposals and Curator

Goal: prevent comment spam and stale anchors.

Tasks:

- add `semantic_anchor_patch` proposal kind or equivalent
- let retrieval inspection and linter findings create deduped proposals
- track proposed change, source refs, confidence, blast radius, and review
  requirement
- add apply/reject surfaces that produce normal source diffs
- keep source rewrites review-gated by default

### PR-F: Obsidian Bridge and Concept Notes

Goal: connect anchors to durable notes without storing prose in code.

Tasks:

- let `obsidian graph build` draft concept notes for high-value anchors
- let bridge reconciliation suggest `repo_anchors`, `repo_symbols`, and
  `repo_docs` backlinks
- add health checks for orphaned anchors and missing canonical notes
- keep all generated note changes inbox-first and reviewed

### PR-G: Agent Workflow and PR Review Surface

Goal: make anchor awareness routine for agents.

Tasks:

- add touched-file anchor lint to the developer loop
- add graph diff output for PRs or agent finals
- retrieve linked test contracts for touched anchors
- add review checks for trust-critical anchors changed without tests
- document agent rules for when to add, update, or remove anchors

## Verification Matrix

| Area | Required checks |
|------|-----------------|
| Docs | `make check-doc-links` |
| Parser | malformed/valid inline anchors, block relations, doc targets |
| Symbol binding | anchors bind to nearest symbol or file deterministically |
| Repoindex | anchor nodes/edges appear in search/expand/DAG output |
| Embedding envelope | golden tests prove stable ordering and digest behavior |
| Retrieval | anchor-aware queries improve fixture suite results |
| ACA proposals | duplicate findings dedupe into one proposal |
| Obsidian bridge | generated anchor notes remain inbox-first |
| Safety | secrets/transient values are rejected by lint |

## Risks

### Risk: Source Files Become Agent Graffiti

Mitigation: inline anchors are tiny and typed. Longer notes live in ACA
proposals, Obsidian notes, docs, or memorycore records.

### Risk: Bad Anchors Become False Authority

Mitigation: anchors are evidence until linked to reviewed docs, tests, decisions,
or validated memory. Retrieval should surface trust metadata.

### Risk: Taxonomy Fragmentation

Mitigation: start with a small allowlist and require parser/retrieval tests for
new anchor types.

### Risk: Embeddings Become Noisier

Mitigation: embed deterministic envelopes with capped lists and stable ordering.
Measure on retrieval fixtures before enabling broadly.

### Risk: Refactors Leave Anchors Stale

Mitigation: bind anchors to symbol IDs/spans, lint touched files, and let curator
propose stale-anchor cleanup.

### Risk: Parallel Graph Store Appears

Mitigation: PR-B extends repoindex first. Only add dedicated anchor tables if
occurrence lifecycle cannot fit in repoindex metadata.

## Reject Criteria

A patch should be rejected if it:

- adds long-form agent analysis to source comments
- treats anchors as instructions without lifecycle/trust review
- creates a new graph database instead of extending repoindex/searchindex first
- uses untyped freeform comments as graph edges
- puts anchor extraction or retrieval under `internal/v2`
- silently rewrites source comments from an agent or daemon
- makes embeddings required for anchor correctness
- accepts secrets, PII, tokens, terminal output, or transient session IDs in
  anchor targets
- adds a new anchor type without tests and relation mapping

## Open Questions

1. Should the canonical inline form be `[[foxctl:invariant/no-send-without-read]]`
   or `[[invariant:no-send-without-read]]` with workspace scope inferred?
2. Should block relation syntax use `Semantic:` or an `@semantic` marker?
3. Should anchor targets eventually be first-class repoindex node kinds, or are
   typed concept nodes sufficient?
4. Should anchors point to tests by stable test names, file paths, or generated
   test contract IDs?
5. Should agent-added anchor proposals be allowed in source diffs by default, or
   only through an explicit command?
6. Which retrieval eval should become the first acceptance gate: foxctl terminal
   safety, ACA retrieval, or memorycore lifecycle?

## Recommended First Slice

Start with PR-A and a narrow part of PR-B:

1. define the inline and block syntax
2. add parser and linter tests
3. bind anchors to file/symbol owners
4. map anchors into repoindex concept nodes and soft edges
5. expose an explain command for one file or symbol
6. do not change embedding or ACA ranking yet

That proves whether semantic anchors are parseable, stable, and useful in the
graph before they affect retrieval behavior or agent editing workflows.
