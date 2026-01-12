---
title: Config-Driven Embedding Alignment & Memory Vectors
status: completed
owners:
    - ai
last_updated: 2025-12-24
completed: 2025-12-24
---

# Objective

Unify all embedding producers/consumers around the config-driven
3072‑dimensional `gemini-embedding-001` model and enable Turso-only vector
search for named memories while keeping SQLite/BM25 paths intact.

---

# Workstream Overview

| # | Area                   | Goal                                                                    |
| - | ---------------------- | ----------------------------------------------------------------------- |
| 1 | Config defaults & docs | Single source of truth for embedding dims/models                        |
| 2 | Producers              | session_summarize, embedding_worker, live index paths honor config dims |
| 3 | Storage                | Sessions + memories validate schema vs config; add Turso memory vectors |
| 4 | Query validation       | Detect mismatches at search time with actionable hints                  |
| 5 | Memory vectors         | Turso-only vector write/search path, SQLite fallback                    |
| 6 | Tests                  | Regression tests (unit + CGO) for dimension enforcement                 |
| 7 | Documentation          | Update design docs + skill help with new behavior                       |

---

# Detailed Plan

## 1. Config defaults & documentation updates

### Files

- `internal/platform/config/config.go`
- `docs/designs/unified_semantic_search.md`
- (Optional) `README.md` snippets referencing embeddings.

### Actions

1. Set `embedding.dimensions` & `database.vector.dimensions` defaults to `3072`.
2. Ensure `embedding.model` default is `gemini-embedding-001` (matches 3072).
3. Document the defaults + requirement to reindex when changing model/dims.
4. Note that SQLite remains BM25-only; vector search is Turso-only with
   `database.vector.enabled=true`.

### Acceptance

- `config.Config` struct yields 3072 when no overrides provided.
- Docs clearly state provider/model pairing and reindex guidance.

---

## 2. Propagate dimensions to embedding producers

### Files

- `skills/session_summarize/main.go`
- `skills/embedding_worker/main.go`
- `skills/embedding_queue` (if any metadata, ensure dims propagate)
- Hooks that enqueue embeddings (e.g., `.claude/hooks/live-index.sh` if they
  embed inline).

### Actions

1. Load `cfg.Embedding.Dimensions` in each skill.
2. After embedding generation, verify `len(vector)==cfg.Embedding.Dimensions`.
3. Fail fast with actionable error: mention config path + recommendation to
   re-run with matching model.
4. When enqueuing embedding jobs, include model/dim metadata if not already
   present (for observability + downstream validation).

### Acceptance

- Unit tests cover mismatch (e.g., forcing 768 output) → skill errors with
  descriptive message.
- Observability logs mention configured model/dim when embedding fails.

---

## 3. Storage migrations aligned to config

### Sessions (SQLite)

**Files:** `internal/storage/sessions/store.go`,
`internal/storage/sessions/migrate.go`

Steps:

1. Add `embedding_metadata` table creation if absent.
2. On `Open`, read metadata; if dims mismatch config, return error with hint:
   “run session reindex or delete DB to rebuild with new dimensions.”
3. Provide CLI-friendly message via caller (skills catch and emit `E_RUNTIME`).

### Sessions (Turso)

Already pass dims; ensure validation errors are surfaced cleanly. Just confirm
`validateDimensions` uses config dims and error text matches doc guidance.

### Memory (Turso path only)

**Files:** new `internal/storage/memory/turso_store.go` (CGO) + stub
`turso_store_nocgo.go`.

Steps:

1. Implement `OpenTurso` similar to sessions:
   - `named_memory.embedding F32_BLOB(<cfg dims>)`
   - Add vector index `idx_memory_vector`.
   - Insert/update `embedding_metadata` with dims/model/provider.
2. Validate metadata vs config on open; error on mismatch.

### Acceptance

- Opening SQLite/Turso stores with mismatched dims fails fast with guidance.
- Unit tests cover metadata creation + mismatch.

---

## 4. Query-time validation

### Files

- `skills/code_semantic_search/main.go`
- (If needed) retrieval packages that consume embeddings.

### Actions

1. After computing query embedding, compare `len` vs configured dims.
2. If mismatch, set `out.Stats.Hint` (non-fatal) and skip vector sources to
   avoid bad search results.
3. When opening stores (sessions/memories) catch validation errors; turn them
   into hints rather than silent failures.

### Acceptance

- Example hint: “Sessions vector store uses 3072 dims but provider returned 768;
  update embedding.model or reindex sessions.”
- Tests confirm hints when mismatched dims injected.

---

## 5. Memory vectors (Turso-only)

### Storage layer

1. `internal/storage/memory/store.go`: keep SQLite/BM25 behavior untouched.
2. New `internal/storage/memory/turso_store.go` (CGO) that wraps
   `dbdriver.OpenDB` with vector support.
3. Provide `VectorStore.SaveWithEmbedding` path for writing vectors using
   `vector()` SQL expression. Validate dims before persist.
4. Non-CGO stub returns clear “requires CGO & Turso vector support” error.

### Search integration

1. In `searchMemories` (code_semantic_search):
   - When driver==`turso`, `cfg.Database.Vector.Enabled==true`, and query
     embedding available → open Turso store and run vector/hybrid search.
   - On failure (no vector support / dimension mismatch) log hint & fallback to
     BM25.
2. Keep existing BM25 path for SQLite or when embeddings absent.

### Embedding pipeline

1. Memory embedding refresh hook/skill should detect Turso vector capability and
   call `SaveWithEmbedding`.
2. Optionally store metadata (model/dim) per workspace for audit.

### Acceptance

- Vector search returns ranked memory entries when Turso + embeddings exist.
- BM25 fallback still works when vectors disabled.

---

## 6. Tests & CI gates

### Add

1. Unit tests for config-driven dimension enforcement (session_summarize,
   embedding_worker).
2. SQLite session open test for dimension mismatch.
3. CGO-tagged Turso memory vector test (skips automatically if `TURSO_*` env
   vars missing or group lacks vector support).
4. code_semantic_search test verifying hints on mismatch and proper fallback.

### CI

- Ensure existing `make test` + `make test-race` unaffected (Skip CGO tests
  unless env set).
- Document how to run CGO tests locally (env vars + `CGO_ENABLED=1`).

---

## 7. Documentation & help text

### Files

- `docs/designs/unified_semantic_search.md`
- `docs/vector-alignment-plan.md` (if cross-referenced)
- Skill READMEs / CLI help (session_summarize, code_semantic_search, memory
  tools).

### Updates

1. Mention config-driven defaults and how to override safely.
2. Describe dimension validation errors + remediation steps (update config vs
   reindex).
3. Document Turso-only memory vectors + fallback behavior.
4. Provide short guide for reindexing sessions/memories after model change.

---

# Dependencies & Sequencing

1. **Config defaults + docs** (step 1) must land first.
2. **Producer validation** (step 2) can parallelize with **storage metadata**
   work (step 3) but should merge after config change.
3. **Memory vector enablement** requires storage + embedding pipeline updates;
   gate behind config flag.
4. Tests & docs updates conclude the effort.

---

# Rollout Considerations

- Dimension enforcement will surface errors where previously silent mismatches
  existed; communicate via changelog.
- Turso vector support requires CGO + proper env vars; ensure stubs keep default
  builds happy.
- Recommend reindex instructions for anyone who previously used non-3072
  embeddings.

---

# Tracking

- Link this plan in relevant PR descriptions.
- Update status fields (`status`, `last_updated`) as work progresses.
