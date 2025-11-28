# Semantic File Index Spec (Draft)

## 1. Overview

`semantic_file_index` defines how agentctl maintains a semantic index of
workspace files using the existing memory store and optional vector search.

The index is updated at **trusted points in the workflow**:

- Initial indexing for a file (with optional chunking if needed).
- Automatic refresh **after a reviewed change is accepted and written**
  ("post-review"), so embeddings track the latest source-of-truth.

This spec is intentionally aligned with:

- `review_gate.md` (review lifecycle, post-review hook).
- `overseer_profile.md` (overseer orchestration and hooks).
- `vector-search.md` and `vector-search-implementation-notes.md` (optional
  sqlite-vector integration).
- The existing `named_memory` and `embedding` column in the memory store.

The goal is to expose a **single, canonical embedding view per file** (with
optional stable chunking) while keeping implementation details (which embedding
API, how chunking is done) pluggable.

---

## 2. Goals and Non-Goals

### 2.1 Goals

- **Single canonical embedding per file**
  - Each logical file path has exactly one _current_ embedding set in the index.
  - Optional chunking is allowed, but chunk boundaries are stable once chosen.
- **Post-review as the canonical update point**
  - When a change passes review and is applied, the semantic index is
    automatically refreshed for the touched files.
  - Search results stay consistent with the reviewed state of the workspace.
- **Reuse existing infrastructure**
  - Store embeddings in `named_memory` with the existing `embedding` column.
  - Reuse vector search support as documented in `vector-search.md`.
- **Pluggable embedding providers**
  - Allow different embedding backends (exec/WASI skills, `http/openapi`, etc.)
    without changing the index contract.
- **Extensible post-review pipeline**
  - Make the post-review hook the standard place to run _other_ indexers, such
    as a code-symbol DAG (Joern/Neo4J or similar).

### 2.2 Non-Goals (v1)

- No real-time / keystroke-level embedding updates.
- No new external vector database; we rely on the existing optional
  sqlite-vector integration or a future pure-Go alternative.
- No changes to the Core Profile envelope wire contract.

---

## 3. Data Model

### 3.1 Named Memory Entries for File Embeddings

Semantic file index entries are modeled as `named_memory` rows with specific
`type` values and a populated `embedding` column when vector support is
available.

**Conceptual fields (subset of NamedEntry):**

- `id` (string) – internal identifier (ULID).
- `name` (string) – stable logical key.
- `workspace` (string) – workspace identifier.
- `type` (string) – one of:
  - `"file_embedding"` – single-embedding-per-file entry.
  - `"file_embedding_chunk"` – optional chunked entry for large files.
- `summary` (string) – short human-readable description.
- `result` (JSON blob) – structured metadata:
  - `path` (string) – relative path within the workspace.
  - `digest` (string) – CAS digest of the file snapshot used to compute the
    embedding.
  - `language` (string, optional) – detected or configured language.
  - `chunk` (object, optional) – present only for chunked entries:
    - `id` (string) – stable chunk identifier for this file.
    - `index` (int) – zero-based chunk index.
    - `of` (int) – total number of chunks for this file.
    - `span` (object, optional) – span within the file. MUST use one of:
      - **Byte-based:** `{ "unit": "byte", "start": int, "end": int }`.
      - **Line-based:** `{ "unit": "line", "start": int, "end": int }`. The
        `unit` discriminator is required when `span` is present; unspecified
        optional fields are omitted and have no implicit defaults.
  - `source` (object, optional) – provenance:
    - `task_id` (string, optional) – task that triggered (re)indexing.
    - `review_id` (string, optional) – review record if triggered post-review.
    - `actor` (string, optional) – e.g. `"actor:system:overseer"`.

When vector search is enabled (see `vector-search.md`):

- The `embedding` column is populated with the embedding vector (e.g. 384-dim
  float32 array).

When vector search is **not** enabled:

- The same entries exist, but `embedding` remains `NULL`.
- Callers MUST treat vector search as unavailable and fall back to keyword/BM25
  or other mechanisms.

### 3.2 Name and Keying Strategy

For **single-embedding-per-file** entries:

- `type = "file_embedding"`
- `name = "file://<workspace_id>/<rel_path>"`

For **chunked** entries:

- `type = "file_embedding_chunk"`
- `name = "file://<workspace_id>/<rel_path>#chunk-<chunk_id>?cfg=<hash>"`

Where:

- `chunk_id` is a stable identifier determined at initial indexing for the given
  chunking configuration.
- `cfg=<hash>` is a stable `chunking_config_hash` derived from
  `(chunk_bytes, chunk_overlap_bytes, provider.model)`.

This ensures that:

- A file has exactly one _active_ `file_embedding` entry.
- If chunking is enabled, a file has a fixed set of `file_embedding_chunk`
  entries whose IDs and boundaries do not change across re-embedding runs as
  long as `chunking_config_hash` remains the same.

#### 3.2.1 Chunking Configuration Changes

When chunking configuration changes (e.g. `chunk_bytes` or
`chunk_overlap_bytes`), the indexer MUST treat the new configuration as a
distinct `chunking_config_hash` and MUST NOT silently reuse old chunk spans.

Preferred behavior (**automatic replacement**, normative):

1. Detect that the effective chunking configuration for `(workspace, path)` has
   changed by comparing the stored `chunking_config_hash` with the current
   configuration.
2. Generate a new set of `file_embedding_chunk` entries using the new
   configuration and a new `cfg=<hash>` value in their `name`.
3. Mark the previous chunk set as deprecated via a flag in `result.source` or a
   dedicated `result.deprecated: true` field, or tombstone them according to the
   memory store’s deletion semantics.
4. Once the new chunks are written successfully, the deprecated set MUST NOT be
   used for search; implementations MAY physically delete them as a cleanup
   step.

Alternative behavior (**manual reindex**, optional):

- The indexer MAY reject re-embedding under a changed configuration with a clear
  error (`CHUNK_BOUNDARY_MISMATCH`, see §11) instructing the caller to trigger
  an explicit full re-index job. Implementations choosing this path SHOULD
  document it clearly in operator docs.

---

## 4. Lifecycle & Triggers

### 4.1 Initial Index (First-Time Embedding)

The first time a file is indexed, the indexer performs an **initial index**.

**Trigger options:**

- Explicit CLI command (e.g. `agentctl semantic-index init ...`).
- Background job initiated by overseer when a file is first seen in a task.
- Future: automatic on first semantic search hit for that file.

**Behavior:**

1. Determine if a file is _unindexed_ by looking up a `file_embedding` entry for
   `(workspace, path)`.
2. If no entry exists:
   - Decide whether chunking is required based on config (see §5.1).
   - If **no chunking**:
     - Create a single `file_embedding` entry and embedding.
   - If **chunking enabled**:
     - Create a `file_embedding` entry with a high-level summary.
     - Create a stable set of `file_embedding_chunk` entries, each with its own
       embedding and `chunk.id`.
3. Store `digest` for the file snapshot used in `result.digest`.

Once a file has been initially indexed, all subsequent updates **reuse the same
chunk IDs and boundaries** (while configuration is stable).

### 4.2 Post-Review Auto-Update (Canonical Hook)

The **post-review** moment is the canonical place to refresh embeddings after a
change is accepted.

**Post-review definition (normative):**

- As defined in `review_gate.md`, a task reaches a state where:
  - All required review checks are satisfied.
  - A specific review record transitions to `accepted`.
  - The associated diff is applied to the workspace.

At this point, the overseer executes a **post-review pipeline** that includes
semantic index updates.

**Behavior:**

1. Overseer gathers the set of **touched files** from the accepted review,
   consistent with `review_gate.md`:
   - Prefer `review.inputs.files` where `changed_since_last_review = true`.
   - Optionally reconcile with `inputs.diff_digest` to detect path-level
     changes.
2. For each file:
   - Look up existing `file_embedding` / `file_embedding_chunk` entries.
   - If the file has never been indexed → fall back to **initial index**
     behavior.
   - If the file has been indexed:
     - Recompute embeddings using the **same chunking scheme** (same `chunk.id`,
       `index`, and spans).
     - Update entries in-place with new `digest` and embedding values.
3. The semantic index now reflects the **reviewed, accepted state** of each
   touched file.

This ensures that semantic search and context retrieval are always based on the
latest reviewed contents, not stale pre-review versions.

### 4.3 Manual / CLI Updates

Users may request explicit reindexing, for example:

- `agentctl semantic-index update --workspace <id> --path <file>...`

Behavior mirrors post-review updates but without requiring a review record.

- If entries exist → re-embed using existing chunking.
- If not → perform initial index.

### 4.4 Concurrency & Idempotency

Indexing operations MUST be safe under retries and concurrent attempts.

- **Idempotency**
  - Indexers MUST use upsert semantics keyed by
    `(workspace, path, type,
    chunk.id, chunking_config_hash)`.
  - If the stored `result.digest` matches the currently indexed file digest,
    implementations MAY short-circuit and skip re-embedding to avoid duplicate
    work.
- **Concurrency controls**
  - Implementations SHOULD use either per-file mutex/lease mechanisms or
    optimistic concurrency with conditional updates based on
    `(digest, last_write_timestamp, last_write_actor)`.
  - Concurrent attempts to index the same `(workspace, path)` MUST resolve
    deterministically (e.g. last-write-wins with a monotonic timestamp) and MUST
    NOT corrupt the index.
- **Ordering & consistency**
  - Post-review indexing (§4.2) is the **canonical refresh** for reviewed state.
    It may run asynchronously by default (eventual consistency), but an
    implementation MAY offer a blocking mode where task completion waits for a
    successful post-review index update.
  - Manual/CLI updates MUST NOT regress the index below the last known reviewed
    state unless explicitly requested.

---

## 5. Indexer Configuration

### 5.1 Semantic Index Config

Configuration keys (conceptual) to control semantic file indexing:

- `semantic_index.enabled` (bool, default: `false`)
- `semantic_index.auto_on_post_review` (bool, default: `true`)
- `semantic_index.include_globs` ([]string)
- `semantic_index.exclude_globs` ([]string)
- `semantic_index.max_file_kb` (int, default: e.g. 256)
- `semantic_index.chunk_bytes` (int, optional)
- `semantic_index.chunk_overlap_bytes` (int, optional)
- `semantic_index.provider` (object):
  - `kind`: `"exec_skill" | "http_openapi" | "wasi_skill"` (conceptual).
  - `name` / `command`: skill or binary name if `exec`/`wasi`.
  - `model`: embedding model identifier.
  - `max_tokens_per_call`, `batch_size`, etc.

Chunking policy:

- If `chunk_bytes` is unset → **no chunking**; one embedding per file.
- If `chunk_bytes` is set and file size > `chunk_bytes` → initial index
  generates fixed, overlapping chunks.
- Chunk boundaries are a pure function of
  `(path, digest, chunk_bytes,
  chunk_overlap_bytes)`; as long as configuration
  does not change, boundaries stay stable across re-embeds.

### 5.2 Interaction with Vector Support

- When built with `-tags vector` and vector search is enabled in the DB,
  embeddings are stored in the `embedding` column and can be used with
  `VectorStore` / hybrid search utilities.
- When vector support is missing, `semantic_file_index` **MUST NOT fail the
  pipeline** solely for lack of vectors; it may still:
  - Create/update metadata-only memory entries.
  - Emit a warning in logs.

---

## 6. Embedding Jobs

### 6.1 Job Types

Two conceptual job types are defined:

- `semantic_index.init_files` – initial indexing for one or more files.
- `semantic_index.update_files` – reindex existing entries (used for post-review
  and manual refresh).

Both are internal jobs that follow the standard job lifecycle and envelope
conventions from the Core Profile spec.

### 6.2 Job Input Shape (Conceptual)

**Common fields:**

- `workspace_id` (string)
- `files` (list of objects):
  - `path` (string)
  - `digest` (string, optional) – CAS digest; if omitted, job reads from
    filesystem.
  - `size_bytes` (int, optional)
  - `language` (string, optional)
  - `change_kind` (string, optional) – `"added" | "modified" | "deleted"`.
- `reason` (string, optional) – e.g. `"initial_index"`, `"post_review"`,
  `"manual"`.
- `task_id` / `review_id` (optional) – if triggered from a task/review.

### 6.3 Job Behavior

For each file in `files`:

1. **Resolve content**
   - If `digest` present → resolve via CAS.
   - Else → read from workspace path (guarded by path validation rules).

2. **Determine index mode**
   - If no existing `file_embedding` entry → initial index (§4.1).
   - If entry exists → update mode (§4.2).

3. **Chunking decision**
   - Based on `semantic_index.max_file_kb`, `chunk_bytes`, and file size.

4. **Embedding calls**
   - Construct embedding requests to the configured provider (`provider.kind`).
   - Batch multiple chunks/files when possible, respecting provider limits.

5. **Upsert memory entries**
   - Use the memory store API to create/update `file_embedding` and
     `file_embedding_chunk` entries.
   - When vector is enabled, use the vector helper / store to write embeddings.

6. **Result envelope**
   - `data.summary` MUST include numeric counts:
     - `files_indexed` (int)
     - `chunks_indexed` (int)
     - `files_skipped` (int)
   - `data.cas_artifact` (optional) MAY point to a CAS blob with detailed
     per-file/chunk results:
     - `artifact_id` (string), `path` (string), `digest` (string),
       `entries_count` (int).

### 6.4 Job Result Envelope & Error Reporting

Embedding jobs use standard Protocol v1 envelopes for their terminal result.
Conceptual shape:

```jsonc
{
	"version": 1,
	"status": "ok|error",
	"command": "jobs/info", // or jobs/tail final event
	"data": {
		"summary": {
			"files_indexed": 12,
			"chunks_indexed": 34,
			"files_skipped": 1
		},
		"failures": [
			{
				"file": {
					"path": "foo/bar.go",
					"digest": "sha256:..." // optional; may be empty on read failures
				},
				"error_code": "EMBEDDING_PROVIDER_FAILURE",
				"error_message": "HTTP 503 from embedding provider",
				"provider_request_id": "req-123", // optional
				"timestamp": "2025-11-15T12:34:56Z"
			}
		],
		"cas_artifact": {
			"artifact_id": "semantic_index.update_files:01HF...",
			"path": "jobs/01HF.../semantic_index_results.ndjson",
			"digest": "sha256:...",
			"entries_count": 46
		}
	},
	"meta": {
		"job_id": "01HF...",
		"workspace_id": "ws-123"
	},
	"error": {
		/* see §11 for error codes; may be null when status:"ok" */
	}
}
```

Job-level status semantics:

- `status: "ok"` with `data.failures` empty → **success**.
- `status: "ok"` with one or more `data.failures` entries → **partial_success**
  (per-file failures, but no systemic errors).
- `status: "error"` → **failure** due to a systemic error (see §11).

Systemic errors that SHOULD cause `status: "error"` include:

- Embedding provider outage or repeated timeouts above a configured threshold.
- Database write failures for the memory or vector stores.
- CAS being unavailable or failing integrity checks.
- Authentication/authorization failures when calling embedding providers.

---

## 7. Integration with Search & Agents

### 7.1 Search APIs

The semantic file index is consumed by existing search mechanisms:

- Vector-only search via `VectorStore` against `named_memory` entries with
  `type in ("file_embedding", "file_embedding_chunk")`.
- Hybrid BM25 + vector search using `HybridSearcher` over the same dataset.

Callers can choose:

- To search only `file_embedding` (whole-file semantics).
- To include `file_embedding_chunk` for more granular matches.

### 7.2 Agent Usage

Agents (including dspy-go agents) can:

- Retrieve semantically related files as part of planning or coding:
  - E.g. "files similar to the one I just edited".
- Bias retrieval by workspace and optionally by `task_id`/`review_id` provenance
  in `result.source`.

Overseer may also use semantic search internally:

- To suggest relevant past tasks or patches when reviewing a change.

---

## 8. Post-Review Indexing Pipeline (Generalized)

### 8.1 Concept

The **post-review** point is not only for embeddings; it is the natural place to
run other indexing pipelines over the reviewed codebase, such as:

- Building or updating a **code-symbol DAG** (e.g. via Joern, Neo4J, or an
  internal graph builder).
- Updating test coverage maps, dependency graphs, or other derived artifacts.

This spec treats semantic file indexing as one particular **post-review
indexer**, and defines the shape for additional indexers.

### 8.2 Post-Review Indexer Abstraction

Conceptually, the overseer maintains a list of **post-review indexers**:

- Each indexer is a component that:
  - Receives the set of files and CAS digests associated with an accepted
    review.
  - Updates its own storage (memory, CAS, external DB, etc.).

Configuration (illustrative):

```yaml
indexing:
    post_review:
        enabled: true
        indexers:
            - id: semantic_embed
              kind: semantic_file_index
              include_globs: ["**/*.go", "**/*.md"]
              exclude_globs: ["vendor/**", "dist/**"]
            - id: code_symbol_graph
              kind: code_symbol_dag
              include_globs: ["**/*.go"]
              # Implementation may call out to Joern, Neo4J, or an internal graph
              # builder using exec/WASI skills and CAS snapshots.
```

The overseer’s **post-review handler** (see `overseer_profile.md` and
`review_gate.md`) becomes the unified coordination point for:

- Semantic embedding updates.
- Code graph/DAG updates.
- Any other future indexers.

### 8.3 Code-Symbol DAG (Future Work)

While detailed design of a symbol DAG is out of scope here, this spec clarifies
that:

- The **same post-review event** used for semantic embeddings is the preferred
  place to:
  - Run Joern or similar tools over changed files.
  - Update a Neo4J or other graph store with symbol and relationship nodes.
- These indexers should use the same `(workspace, path, digest)` inputs as the
  semantic indexer to maintain consistency.

---

## 9. Failure Modes & Safety

- If semantic indexing fails for some files, the overseer SHOULD log and surface
  this, but task completion and review acceptance remain valid.
- If vector support is misconfigured or unavailable:
  - Semantic indexing SHOULD degrade gracefully (metadata-only updates).
- Embedding providers must respect Core Profile rules:
  - Envelopes on stdout, logs on stderr.
  - No secrets logged.
  - Network access only as allowed by runner configuration.

---

## 10. Future Enhancements

Potential follow-ons:

- Incremental / diff-based embedding where only affected chunks are re-embedded.
- Multi-vector per file (e.g. per-symbol or per-region embeddings) while still
  preserving a simple whole-file view.
- Tight integration with a code-symbol DAG for joint semantic + structural
  retrieval.
- Cross-workspace semantic search over shared libraries.
- A dedicated `code_symbol_dag` / dependency graph spec building on the
  post-review indexer abstraction defined here.

---

## 11. Error Codes & Contract

This section standardizes error codes and result shapes for the semantic file
index and its embedding jobs. It extends the Protocol v1 error catalog in
`protocol_v1.md` §5.

### 11.1 Standard Error Codes

| Code                         | Description                                                             |
| ---------------------------- | ----------------------------------------------------------------------- |
| `SEMANTIC_INDEX_NOT_FOUND`   | Requested file/path has no semantic index entry.                        |
| `PROVIDER_CONFIG_INVALID`    | Embedding provider configuration is incomplete or invalid.              |
| `CHUNK_BOUNDARY_MISMATCH`    | Existing chunk metadata does not match current chunking configuration.  |
| `VECTOR_NOT_ENABLED`         | Vector search/embedding support is not enabled in the current build/DB. |
| `EMBEDDING_PROVIDER_FAILURE` | Embedding provider returned non-2xx or protocol error after retries.    |
| `CAS_RESOLVE_ERROR`          | CAS read/write failed or digest mismatch detected.                      |

Implementations MAY define additional, more granular codes as long as they do
not conflict with these names.

### 11.2 Classification & Recovery Guidance

Each error code is classified as recoverable/non-recoverable and comes with
guidance:

| Code                         | Recoverable? | Suggested Handling                                                                                                          |
| ---------------------------- | ------------ | --------------------------------------------------------------------------------------------------------------------------- |
| `SEMANTIC_INDEX_NOT_FOUND`   | yes          | Log at debug; return empty search results or trigger initial index if configured.                                           |
| `PROVIDER_CONFIG_INVALID`    | no           | Log at error; fail job; require operator to fix configuration.                                                              |
| `CHUNK_BOUNDARY_MISMATCH`    | yes          | Log at warn; either perform automatic replacement (§3.2.1) or surface to caller to trigger manual reindex.                  |
| `VECTOR_NOT_ENABLED`         | yes          | Log at info; skip embedding work; allow metadata-only entries.                                                              |
| `EMBEDDING_PROVIDER_FAILURE` | yes (often)  | Log at error; retry with backoff up to budget; on exhaustion, mark job as failure or partial_success depending on coverage. |
| `CAS_RESOLVE_ERROR`          | no           | Log at error; fail job; operator must correct storage or disk issues.                                                       |

Recoverable errors MAY be retried within a single job attempt; non-recoverable
errors SHOULD cause the job to fail fast.

### 11.3 Result Error Shape

Per-file errors MUST be expressed as entries in `data.failures` on the job
result envelope (see §6.4). Each failure object MUST include:

- `file.path` (string)
- `error_code` (string; one of the codes above or from Protocol v1 catalog)
- `error_message` (string)

It SHOULD also include when available:

- `file.digest` (string, optional)
- `provider_request_id` (string, optional)
- `timestamp` (string, RFC3339 UTC)

Systemic errors (provider outage, DB errors, CAS failures) MUST be surfaced via
the top-level `error` object on the job envelope, using one of the codes above
and a meaningful `error.message`. In that case, `status` MUST be `"error"` and
consumers SHOULD treat `data.failures` as incomplete.

### 11.4 Protocol v1 Semantics

All envelopes and error codes described here are extensions of the Core Profile
v1 contract defined in `protocol_v1.md`:

- `version` MUST be `1`.
- `status` MUST be `"ok"` or `"error"` for terminal job envelopes.
- `error.code` MUST be set for `status: "error"`.
- `meta.cas_digest` MUST be set when `data.cas_artifact` is present and MUST
  match the artifact digest.

HTTP or RPC layers integrating with these jobs SHOULD map error codes to
statuses consistent with Core Profile guidance (e.g. provider configuration
issues → `400`, authorization issues → `401/403`, storage issues → `500`).
