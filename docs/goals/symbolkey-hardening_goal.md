# Goal: SymbolKey Cross-Plane Hardening

## Goal

Complete the hardening work described in
`docs/plans/symbolkey-followup-improvements.md` on branch
`feat/symbolhardening`.

The delivered behavior must make symbol identity consistent across symbol
indexing, named-memory `code_symbol` entries, repoindex nodes, embedding queue
jobs, embedding worker updates, symbol summaries, semantic anchor owner
resolution, and retrieval/search projections. The canonical logical identity is:

```text
workspace + language + package_id + symbol_key
```

File path, symbol name, and span are locators only. They must not be the durable
write identity for symbol memory or symbol embeddings.

## Context

- Plan: `docs/plans/symbolkey-followup-improvements.md`
- Current branch: `feat/symbolhardening`
- Recent finding: the main symbol key work is partly implemented, but several
  cross-plane write paths still risk drifting back to path/name or legacy symbol
  IDs.
- Highest-risk known spots:
  - `skills/embedding_worker/main.go` can still update symbol memory through
    legacy `symbol://<workspace>/<file_path>:<symbol_name>` names.
  - `skills/code_incremental_index/main.go` queues embeddings with legacy
    `sym.ID` in at least one path instead of `sym.EffectiveID()`.
  - `internal/intelligence/indexing/embedding/types.go` and store payloads do
    not carry enough canonical symbol identity for worker updates.
  - `internal/storage/memory/store.go` `SyncSymbolEmbeddings` needs proof for
    keyed `pkg::symbolKey` memory entries or must be clearly treated as legacy.
  - Repo/search/anchor projections still use path/name in some surfaces and need
    explicit metadata or tests to prevent write-path drift.
- The worktree may already contain queue/backend changes. Preserve user and
  prior-agent changes; do not revert unrelated edits.

## Milestones

### Milestone 1: Baseline and Current-State Triage

Done when:

- `gofmt` has been run on any touched Go files.
- The interrupted embedding worker / queue edits are either made buildable or the
  exact compile blocker is recorded before deeper changes.
- Current implementation state is compared against
  `docs/plans/symbolkey-followup-improvements.md`; do not reimplement items that
  are already correctly landed.
- A short checklist of remaining deltas is kept in the final self-review.

### Milestone 2: Canonical Symbol Contracts

Done when:

- Shared package derivation and keyed entry-name helpers are the only write
  contract for new symbol memory entries.
- `SymbolKey` constructors cover Go exported, Go non-exported, Go `init`,
  TypeScript/JavaScript, Elixir, Python, Rust, and C# as already supported by the
  current indexers.
- Package-scoped key entry names use the documented
  `symbol://<workspace>/<pkg>::<symbolKey>` shape.
- Summary entry names use the matching package-scoped shape.
- Tests cover package collisions, root paths, non-exported Go collisions, and
  Python keys.

### Milestone 3: Key-Only Symbol Writers

Done when:

- Main symbol indexer writes new `code_symbol` entries through the package-scoped
  key name only.
- Incremental indexing writes through the same package-scoped key name and no
  longer queues or persists legacy symbol identities for new work.
- Delete/stale cleanup targets the key-based names and treats legacy cleanup as
  best-effort compatibility only.
- File metadata schema changes force re-indexing where necessary.
- Tests prove that two symbols with the same display name in different packages
  do not collide.

### Milestone 4: Embedding Queue and Worker Identity

Done when:

- Symbol embedding jobs carry enough identity to update the exact keyed memory
  entry: workspace, language, package ID, symbol key, and/or canonical memory
  entry name.
- Queue dedupe cannot collapse distinct same-name symbols from different
  packages.
- Worker completion updates the keyed `code_symbol` memory entry, not a
  path/name legacy entry.
- `SyncSymbolEmbeddings` either works with keyed entries and has regression
  tests, or is explicitly constrained as legacy-only with no active key-based
  caller relying on it.
- Tests prove indexer or incremental skill -> queue -> worker completion uses one
  logical symbol identity end to end.

### Milestone 5: Consumers, Search, and Semantic Anchor Projections

Done when:

- Symbol summary lookups thread package identity through their public and
  internal call sites.
- Semantic search and `code_semantic_search` parse package-scoped symbol names
  without fabricating file paths from `<pkg>::<symbolKey>`.
- Search documents or metadata preserve the canonical symbol reference wherever
  they expose `SymbolID`, path, and display name.
- Repoquery and semantic-anchor owner surfaces do not create new durable write
  identities from path/name alone.
- Any remaining path/name matching is documented and tested as a locator or
  read-time fallback only.

## Constraints

- Treat `workspace + language + package_id + symbol_key` as canonical for
  durable symbol identity.
- Do not introduce new legacy symbol writes.
- Do not remove compatibility reads unless the call site is proven unused or the
  plan explicitly calls for a hard cut.
- Do not add dependencies without explicit approval.
- Do not introduce keyword heuristics for routing, classification, promotion, or
  suppression behavior. Use explicit schemas, typed fields, scored features, or
  tests.
- Preserve canonical envelope fields and `meta.*` invariants.
- Preserve WASI `capabilities.network: "none"` for skills.
- Do not use direct `github.com/mattn/go-sqlite3`, sqlite-vector extension
  loading, `-tags=libsqlite3`, or `foxctl-cgo`; Turso is the canonical
  SQLite-family path.
- Before adding or moving `internal/*` packages, read
  `docs/architecture/package-topology.md` and explain the family boundary.
- Use semantic comments only for durable retrieval intent: invariants, domain
  boundaries, cross-plane contracts, and code-to-test/doc relationships. Do not
  add comments that merely restate implementation mechanics.
- Avoid full live embedding runs during this goal unless the queue is configured
  to pace work safely. Use fake embedders or tiny fixtures by default.

## Verification

Run focused tests after each milestone, then the combined gate before marking
the goal complete.

Initial build sanity:

```bash
gofmt -w <touched-go-files>
go test -count=1 ./skills/embedding_worker ./skills/embedding_queue ./skills/code_incremental_index
```

Core symbol identity and writers:

```bash
go test -count=1 ./internal/platform/symbolutil ./internal/intelligence/indexing/symbol ./internal/intelligence/indexing/repoindex
go test -count=1 ./skills/code_incremental_index ./cmd/foxctl/cmd -run 'Test.*(Symbol|SymbolKey|Incremental|Summary|IndexRepo|SemanticSearch)'
```

Embedding queue and worker proof:

```bash
go test -count=1 ./internal/intelligence/indexing/embedding ./internal/intelligence/indexing/embedqueue ./internal/storage/memory
go test -count=1 ./skills/embedding_queue ./skills/embedding_worker ./skills/embedding_memories
```

Retrieval and projection proof:

```bash
go test -count=1 ./internal/intelligence/retrieval ./internal/intelligence/searchindex ./internal/intelligence/repoquery
go test -count=1 ./skills/code_semantic_search
```

Skill and repo gates:

```bash
make skill SKILL=embedding_queue
make skill SKILL=embedding_worker
make skill SKILL=code_incremental_index
make skill SKILL=code_semantic_search
make build
make check-doc-links
git diff --check
```

Optional live smoke, only after unit tests pass and only with paced batching:

```bash
./bin/foxctl index repo build --workspace . --go --typescript=false --elixir=false
```

If running LM Studio/Qwen embedding, keep the fixture tiny: one small file and at
most one large sliced file. Batch size must be one or otherwise explicitly paced.
Stop immediately on GPU pressure, daemon crash, or reboot symptoms.

## Done Criteria

- The active parts of `docs/plans/symbolkey-followup-improvements.md` are
  implemented or explicitly marked as no longer applicable with current-code
  evidence.
- New symbol writes and embedding updates use keyed, package-scoped identity.
- Queue dedupe and worker completion preserve distinct logical symbols with the
  same display name.
- Main indexer and incremental indexer agree on package derivation, symbol key,
  memory entry name, and embedding job identity.
- Repoindex/search/semantic-anchor projections cannot accidentally become a
  second durable symbol identity.
- Focused unit and integration tests pass.
- `make build`, `make check-doc-links`, and `git diff --check` pass, or any
  unrelated pre-existing failure is recorded with exact command output.

## Stop Conditions

- Stop after three failed attempts at the same failing check and summarize the
  blocker with the exact command and failure.
- Stop before adding dependencies.
- Stop before broad storage redesign, repoindex schema replacement, or public API
  changes outside the symbol identity contract.
- Stop before running broad live embedding jobs if queue pacing is not proven.
- Stop if fixing one caller requires reverting unrelated worktree changes.
- Stop if a compatibility read path must be removed but no replacement or
  migration path is proven.

## Final Self-Review

Before completion, write a short review note covering:

- Which symbol identity contracts changed.
- Which write paths were hardened.
- Which read/projection paths still use path/name as locators only.
- Which tests prove collision resistance and queue/worker identity consistency.
- Any remaining legacy compatibility paths and why they are safe.
- Residual risks and confidence score.
