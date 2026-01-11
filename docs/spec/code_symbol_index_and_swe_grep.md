# Code Symbol Index and SWE Grep Spec (Draft)

## 1. Overview

This spec defines two closely related capabilities for `agentctl`:

- A **code symbol index** (code-symbol DAG) that stores per-symbol embeddings
  and call relationships for source code in a workspace.
- A **SWE Grep** retrieval skill that, given a natural-language question and a
  set of candidate files or symbols, extracts high-signal code snippets using
  live reads and a small language model.

These components are designed to work with, not replace:

- The **semantic file index** (`semantic_file_index.md`), which provides
  single-embedding-per-file (and optional chunk) views.
- The **review gate** (`review_gate.md`), which defines post-review as the
  canonical moment to refresh derived indexes.
- The **dspy-go agent runtime** (`dspy_go_agents.md`), which exposes tools that
  call into this index and SWE Grep skill.
- **Trajectory capture** (`dspy_trajectory_capture.md`), which records how
  agents and tools interact with code and reviews.

At a high level, the symbol index + SWE Grep implement the **funnel
architecture** for code retrieval:

1. **Broad retrieval**: use file and symbol embeddings + call graph to find
   relevant files and symbols.
2. **SWE Grep**: live-read those files and extract high-density snippets.
3. **Reasoning**: dspy-go agents consume snippets plus other context (tasks,
   reviews, trajectories) to synthesize answers and changes.

This spec is **normative** for the symbol index data model and SWE Grep skill
contract, but **does not** introduce any new wire-level fields beyond Protocol
v1.

---

## 2. Goals and Non-Goals

### 2.1 Goals

- **Per-symbol semantic index**
  - Maintain embeddings and metadata for functions, methods, classes, and other
    symbols, with stable identifiers.
  - Track a lightweight call graph (who calls whom) for graph-based traversal.
- **Post-review as canonical refresh point**
  - Refresh symbol and call data **after** a reviewed change is accepted and
    written, using the same post-review pipeline as `semantic_file_index`.
- **Incremental updates for large files**
  - Avoid re-embedding entire "God" files when only a few symbols change, via
    per-symbol digests.
- **Kernel-owned SWE Grep skill**
  - Provide a deterministic SWE Grep exec skill that:
    - Reads live workspace files under `PathValidator`.
    - Uses a small LM to filter candidate files down to high-value snippets.
    - Emits snippets via Protocol v1 envelopes and CAS artifacts when large.
- **Agent-friendly tools**
  - Expose retrieval capabilities to dspy-go agents via tools like
    `code.symbol_search` and `code.swe_grep`, without giving agents direct
    control over embedding/indexing internals.

### 2.2 Non-Goals (v1)

- Building a full language server or refactoring engine.
- Modeling every kind of code relationship (data flow, inheritance, etc.).
- Exposing low-level SQL or DB topology to agents; all access is via Go APIs and
  skills.
- Real-time / keystroke-level re-indexing; post-review remains the canonical
  refresh, with optional heuristic updates.

---

## 3. Code Symbol Index Data Model

### 3.1 Symbols (Nodes)

The symbol index stores metadata and embeddings for individual code symbols.

Conceptual fields for a `symbol` row:

- `id` (string, primary key)
  - Stable identifier for the symbol, e.g. `"pkg/auth/login.go:Login"`.
  - MUST remain stable across re-indexing as long as the symbol logically exists
    at that path.
- `file_path` (string, not null)
  - Relative path within the workspace.
- `name` (string, not null)
  - Symbol name (e.g. `"Login"`, `"CalculateGravity"`).
- `language` (string, not null)
  - Normalized language identifier, such as `"go"`, `"python"`, `"gdscript"`.
- `kind` (string, not null)
  - Symbol kind, e.g. `"function"`, `"method"`, `"class"`, `"struct"`,
    `"file_summary"`.
- `start_byte`, `end_byte` (integer)
  - Byte offsets into `file_path` for navigation.
- `signature` (string, optional)
  - Skeleton representation (e.g.
    `"func Login(ctx context.Context, in Input) error"`).
- `body_digest` (string, optional)
  - `sha256:<hex>` digest of the symbol body contents, used to detect changes
    and avoid unnecessary re-embeds.
- `file_digest` (string, optional)
  - `sha256:<hex>` digest of the entire file contents at indexing time.
- `embedding` (vector / F32_BLOB, optional)
  - Embedding for the symbol. Dimension and provider are configured by the
    kernel; this spec does not mandate specific models.

Implementations MAY add additional columns (e.g. tags or documentation snippets)
so long as the fields above remain meaningful.

#### 3.1.1 ID Stability and Renames (v1 Behavior)

In v1, symbol IDs are derived from `(file_path, symbol_name)` using the format
`"<file_path>:<symbol_name>"`. This has the following implications:

- **File path changes:** If a file is renamed or moved, all symbol IDs in that
  file change. This is a known limitation of v1. Future versions MAY detect
  renames via `content_hash` matching and remap IDs to preserve embeddings.

- **Symbol renames:** Renaming a symbol (e.g. `Login` → `Authenticate`) is
  treated as a deletion of the old ID plus creation of a new ID. Embeddings are
  not preserved across renames in v1.

- **Symbol modifications:** Changing a symbol's body while keeping its name
  preserves the ID. The indexer uses `body_digest` to detect whether
  re-embedding is needed (see §4.3).

- **Symbol deletions:** Removing a symbol from a file causes its ID (and any
  call edges referencing it) to be removed from the index.

These semantics ensure that unchanged symbols at the same path retain stable IDs
across re-indexing, satisfying the MUST requirement above for the common case.

#### 3.1.2 Symbol Embeddings (Optional, Future Work)

The `embedding` column for `symbols` is populated by a **separate embedding
process**, not by the core symbol indexer described in §4. In v1, this section
is **non-normative**: implementations MAY adopt the pattern below, but symbol
embeddings are not required for a conforming Code Symbol Index.

- Embeddings are produced by dedicated jobs (conceptually
  `symbol_index.init_symbols` / `symbol_index.update_symbols`) that:
  - Read existing `symbols` rows and construct an embedding text per symbol
    (e.g. signature + documentation, optionally a truncated body slice).
  - Call a configured embedding provider (see `semantic_file_index.md` §5.2 for
    the provider shape) to generate vectors.
  - Store vectors in the `embedding` column keyed by `symbols.id`.
- Jobs SHOULD reuse `body_digest` and the provider model identifier to avoid
  unnecessary re-embeds:
  - If `(id, body_digest, provider_model)` is unchanged, implementations MAY
    skip re-embedding that symbol.
- Absence of symbol embeddings MUST NOT break callers; retrieval components
  SHOULD gracefully fall back to metadata-only symbol index behavior.

### 3.2 Calls (Edges)

The call graph is modeled as directed edges between symbols.

Conceptual fields for a `call` row:

- `source_id` (string, not null)
  - Caller symbol ID; foreign key to `symbols.id`.
- `target_id` (string, not null)
  - Callee symbol ID; foreign key to `symbols.id`.
- `count` (integer, default `1`)
  - Number of observed callsites (optional; primarily advisory).

Primary key: `(source_id, target_id)`.

This graph is intentionally conservative; heuristics (e.g., name-based
resolution, imports) MAY introduce extra edges with lower confidence, but the
spec does not require explicit confidence scores in v1.

### 3.3 File Meta (Freshness)

A `file_meta` concept tracks whether a file requires re-indexing.

Conceptual fields:

- `file_path` (string, primary key)
- `last_mod_time` (integer)
  - Last observed modification time.
- `content_hash` (string)
  - `sha256:<hex>` digest of the file contents.

Indexers MUST consult `file_meta` to avoid unnecessary work, but MAY still force
re-indexing under certain conditions (e.g. configuration changes).

### 3.4 Named Memory Storage Mapping

The code symbol index uses `named_memory` entries (as defined in
`core_profile_v1.md` §12) to persist symbols, call edges, and file metadata.
This mirrors the approach used by `semantic_file_index.md` §3.1 for file
embeddings.

**Type mapping:**

| Conceptual Table | Memory Entry Type         | Entry Name Format                                |
| ---------------- | ------------------------- | ------------------------------------------------ |
| `symbols`        | `"code_symbol"`           | `symbol://<workspace>/<file_path>:<symbol_name>` |
| `calls`          | `"code_symbol_call"`      | `call://<workspace>/<source_id>-><target_id>`    |
| `file_meta`      | `"code_symbol_file_meta"` | `symbol-meta://<workspace>/<file_path>`          |

**Symbol entries (`type="code_symbol"`):**

- `Entry.Name` follows the `symbol://` format above, providing a unique key per
  symbol per workspace.
- `Entry.Result` contains a JSON blob with the `Symbol` struct fields, plus
  optional `Source` provenance (task_id, review_id, reason).
- `Entry.Embedding` (when vector support is enabled) holds the symbol embedding
  vector. When vector support is disabled, this column is `NULL` and the index
  operates in metadata-only mode.

**Call edge entries (`type="code_symbol_call"`):**

- Each `(source_id, target_id)` pair is stored as a separate entry.
- `Entry.Result` contains the `CallEdge` struct (source_id, target_id, count).
- No embedding column is used for call edges.

**File meta entries (`type="code_symbol_file_meta"`):**

- Keyed by file path (not symbol), as file_meta tracks per-file freshness.
- `Entry.Result` contains the `FileMeta` struct (file_path, content_hash,
  last_mod_time, and optionally symbol_count for diagnostics).

This storage model ensures that symbol index data follows the same lifecycle and
garbage collection semantics as other named memory entries, and can participate
in the same search and retrieval APIs.

---

## 4. Indexer Behavior and Post-Review Lifecycle

### 4.1 Tree-sitter Based Parsing

The symbol indexer uses Tree-sitter (or equivalent parsers) to:

- Identify symbol definitions (functions, methods, classes, file summaries).
- Extract symbol ranges (`start_byte`, `end_byte`), names, and signatures.
- Find call expressions inside symbol bodies to populate `calls` rows.

Language-specific queries are implementation-defined but should follow the
pattern from the Tree-sitter documentation and the ingestion notes in this spec.

### 4.2 Handling Large Files ("God Classes")

For files above an implementation-defined threshold (e.g. > 500 LOC):

- The indexer MUST **not** embed the entire file body as a single symbol.
- Instead, it MUST:
  - Extract individual function/method symbols and embed them separately.
  - Optionally create a `kind = "file_summary"` symbol that captures high-level
    structure (e.g. list of methods and fields), without embedding the full
    body.

This ensures that minor edits to one method in a large file do not force
re-embedding of unrelated methods.

### 4.3 Incremental Updates per Symbol

When re-indexing a file at path `P` with current `content_hash`:

1. Load prior `file_meta` for `P`.
2. If `content_hash` matches the stored hash:
   - Indexer MAY skip all work for `P`.
3. Otherwise:
   - Parse the file with Tree-sitter.
   - Enumerate all symbols with computed `id` and `body_digest`.
   - For each parsed symbol:
     - If an existing `symbols` row for `id` exists with identical
       `body_digest`, KEEP its embedding and metadata.
     - If new or `body_digest` differs, recompute embedding and UPSERT the row.
   - Remove any `symbols` rows for `file_path = P` whose IDs are not present in
     the new parse.
   - For changed symbols, recompute their outgoing `calls` rows by re-scanning
     call expressions in the body.
   - Update `file_meta` for `P` with new `last_mod_time` and `content_hash`.

This per-symbol strategy limits churn in the DB and avoids repeated embedding
calls for unchanged symbols.

### 4.4 Post-Review as Canonical Refresh

The **canonical** point to update the symbol index is immediately after a review
passes and its diff is applied, as defined in `review_gate.md` and
`semantic_file_index.md`:

- When a review artifact transitions to `status = "ok"` and the associated diff
  has been written:
  - The overseer (kernel) SHOULD enqueue a post-review indexing job that:
    - Reads `inputs.files` and `inputs.diff_digest` from the review artifact.
    - Collects
      `(workspace_id, files[{path, digest, change_kind}], task_id,
      review_id, reason = "post_review")`.
    - Invokes the symbol indexer with this input.
- The indexer MUST treat this as the canonical refresh for the
  symbol/call-derived view of the codebase.

Heuristic triggers (e.g. on commit or large edit) MAY also schedule index
updates, but the post-review path remains normative for any downstream metrics
and training data.

### 4.5 In-Progress Changes, Live Reads, and Staleness

Symbol index contents represent the **last accepted snapshot** of the codebase,
not an always-live view:

- Post-review updates (>4.4) are the canonical source of truth for downstream
  metrics, training data, and most retrieval.
- Heuristic triggers (e.g. on git commits) MAY update the index sooner, but are
  treated as best-effort hints.

SWE Grep always operates on **live workspace files**:

- `code/snippet_extract` reads directly from disk under `PathValidator`, independent of
  whether symbol/semantic indexes have caught up.
- For unreviewed or in-progress changes it is expected that:
  - Index-based candidates may be slightly stale.
  - SWE Grep>s snippet locations and contents reflect the authoritative current
    state of the workspace.

In the typical funnel:

1. Use semantic file index and symbol index to propose candidate
   `(file, symbol_id)` pairs.
2. Optionally enrich candidates with recent git commits or diffs.
3. Call `code/snippet_extract` over those candidates (often in parallel per file) to
   obtain high-signal snippets from the current workspace contents.

Agents MUST NOT assume that index entries always match live files; when there is
a discrepancy, reasoning SHOULD favor SWE Grep snippets and diffs for the
current task over stale index metadata.

---

## 5. SWE Grep Skill Contract (`code/snippet_extract`)

### 5.1 Purpose and Role

The SWE Grep skill narrows high-recall candidate sets (files and symbols) down
to high-precision snippets for reasoning:

- **Inputs:** natural-language question + candidate files/symbols.
- **Behavior:** live-read code, run a small LM per file to score relevance and
  extract snippets with minimal surrounding context.
- **Outputs:** inline previews and, when large, a CAS artifact containing rich
  snippets.

It is implemented as an **exec** skill and is owned by the kernel; agents call
it only via tools.

### 5.2 Input Shape (`data`)

Command: `code/snippet_extract`

Conceptual `data` fields:

- `workspace_id` (string, required)
- `question` (string, required)
  - The natural-language question or task.
- `candidates` (array of objects, required)
  - Each object has:
    - `path` (string, required) – file path relative to workspace root.
    - `symbol_id` (string, optional) – matches `symbols.id` when available.
    - `priority` (float, optional) – advisory ordering from upstream retrieval.
- `limits` (object, optional)
  - `max_files` (int)
  - `max_snippets` (int)
  - `max_bytes_per_file` (int)

The skill MUST:

- Validate paths using the same path validation rules as other filesystem
  helpers (see policy/PathValidator and `AGENTCTL_WORKSPACE`).
- Read file contents from disk (not from the symbol DB) to ensure freshness.

### 5.3 Output Shape (`data`)

On success (`status: "ok"`), the envelope SHOULD contain:

```jsonc
{
  "data": {
    "summary": {
      "files_considered": 12,
      "files_relevant": 5,
      "snippets_emitted": 18
    },
    "snippets_inline": [
      {
        "file": "pkg/auth/login.go",
        "symbol_id": "pkg/auth/login.go:Login", // optional
        "start_line": 42,
        "end_line": 60,
        "preview": "func Login(...) { ... }" // truncated
      }
    ],
    "artifact": "sha256:..." // optional when large
  },
  "meta": {
    "cas_digest": "sha256:..." // optional; if set MUST match data.artifact
  }
}
```

Rules:

- If the total snippets are small enough to fit within configured inline
  thresholds, the skill MAY omit `artifact` and return all snippets in
  `snippets_inline`.
- If snippets would exceed inline limits, the skill MUST:
  - Write NDJSON content to CAS (one snippet per line, including `file`,
    optional `symbol_id`, `start_line`, `end_line`, and full `text`).
  - Set `data.artifact` to the CAS digest.
  - `meta.cas_digest` is optional; if set it MUST equal `data.artifact`.

### 5.4 Error Semantics

The SWE Grep skill reuses Protocol v1 and dspy tool error codes (see
`protocol_v1.md`, `dspy_go_agents.md` §11.3). Common examples:

- `E_GUARD_VIOLATION` – path blocked by `task_guard` / `file_guard`.
- `E_FILE_NOT_FOUND` – candidate path does not exist.
- `E_SWE_GREP_NO_CANDIDATES` – no usable candidates were provided.
- `ERUNTIME` – internal execution error (e.g. model process failure).
- `ETIMEOUT` – small-model inference timeout.

Error envelopes MUST:

- Use `status: "error"`.
- Set `error.code` to one of the above (or an existing Protocol v1 code).
- MAY include a `data.hint` and `error.details` with structured context (e.g.
  offending path).

---

## 6. Agent Tools and Usage

### 6.1 `code.symbol_search` Tool

The dspy-go tools layer MAY expose a `code.symbol_search` tool that wraps the
symbol index.

Inputs (conceptual):

- `workspace_id` (string)
- `question` (string)
- `mode` (string, optional) – `"search" | "callers" | "callees"`.
- Optional hints:
  - `symbol_hint` (string)
  - `max_results` (int)

Output (conceptual):

- `candidates[]`:
  - `file` (string)
  - `symbol_id` (string)
  - `name` (string)
  - `kind` (string)
  - `score` (float) – combined vector + graph score (implementation-defined).

This tool is implemented in Go against the symbol index; it does **not** expose
SQL to agents.

### 6.2 `code.swe_grep` Tool

The tools layer SHOULD expose a `code.swe_grep` tool that calls the
`code/snippet_extract` skill.

Inputs (conceptual):

- `workspace_id`
- `question`
- `candidate_files[]`:
  - `path` (string)
  - `symbol_id` (string, optional)
  - `priority` (float, optional)

Output (conceptual):

- `snippets[]` with `file`, optional `symbol_id`, `start_line`, `end_line`, and
  `text` or a truncated preview.
- Optional `cas_artifact` describing where full snippets live in CAS.

Agents typically:

1. Use `code.symbol_search` and/or `semantic_file_index` to propose candidates.
2. Call `code.swe_grep` to obtain high-quality snippets.
3. Use snippets, along with tasks, reviews, trajectories, and docs, to plan
   edits or answer questions.

---

## 7. Trajectory and Semantic Index Integration

### 7.1 Trajectory Events

When SWE Grep and symbol search are used by agents, trajectory capture
(`dspy_trajectory_capture.md`) SHOULD record these operations as
`TrajectoryEvent`s, typically with kinds such as:

- `"tool_call"` and `"tool_result"` for `code.symbol_search` and
  `code.swe_grep`.
- Optional more specific `kind` values (e.g. `"graph_search"`, `"swe_grep"`) may
  be used in derived views or exporters, but are not required by this spec.

Events SHOULD include:

- `command` set to the Protocol v1 `command` (e.g. `"code/snippet_extract"`).
- `meta.correlation_id`, `meta.task_id`, and other fields per the trajectory
  spec.
- `data_artifact` referencing CAS digests for large result sets, when present.

### 7.2 Relation to Semantic File Index

The symbol index and SWE Grep are **complementary** to `semantic_file_index.md`:

- `semantic_file_index` provides per-file and per-chunk embeddings, keyed by
  `(workspace, path, chunk)`.
- The symbol index provides per-symbol embeddings and a call graph, keyed by
  `(workspace, file_path, symbol_id)`.
- Both use the same post-review event as their canonical refresh point and
  SHOULD share `(workspace, path, digest, task_id, review_id)` inputs where
  practical.

Together, they allow retrieval strategies that combine:

- Whole-file / chunk-level similarity.
- Symbol-level semantics and graph structure.
- On-demand SWE Grep snippets from live files.

---

## 8. Future Extensions

Potential extensions building on this spec include:

- Adding per-symbol chunk embeddings for very large methods, while preserving a
  simple symbol-level view.
- Indexing additional relationships (e.g. type references, inheritance edges).
- Adding named_memory-backed embeddings for:
  - `qa_pair` (question-answer pairs).
  - `trajectory_episode` (exported episodes from `dspy_trajectory_capture.md`).
  - `doc_embedding` (codemaps and specs).
- Optimizing rename handling by recognizing identical `content_hash` values and
  remapping symbol IDs instead of re-embedding.

These remain out of scope for v1 but are intentionally compatible with the data
model and lifecycle defined here.
