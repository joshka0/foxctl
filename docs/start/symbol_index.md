# Code Symbol Index

The code symbol index stores extracted symbols (functions, methods, types) and
their call relationships in named memory. It refreshes automatically after
accepted reviews via the post-review pipeline.

## Post-Review Integration Flow

```
Review accepted
      │
      ▼
ReviewArtifact (status="ok")
      │
      ▼
PostReviewHandler (internal/indexing/handler.go)
      │
      ├── Emits PostReviewEvent with:
      │     workspace_id, task_id, review_id, reason
      │     files[{path, digest, change_kind, language, size_bytes}]
      │     (note: files may be empty until the diff application layer is wired)
      │
      ▼
Symbol Indexer (internal/indexing/symbol/indexer.go)
      │
      ├── For each file:
      │     1. Check file freshness via FileMeta (content_hash)
      │     2. Extract symbols via GoExtractor (or other language extractor)
      │     3. Compare per-symbol body_digest with previous run
      │     4. Save only changed symbols; delete removed symbols
      │     5. Update FileMeta with new symbol digests
      │
      ▼
Named Memory entries:
  - type="code_symbol"       → symbol definitions
  - type="code_symbol_file_meta" → file freshness tracking

Call relationships are currently recorded inside the `code_symbol` entry result
as a `calls[]` list. Persisting call edges as separate `code_symbol_call` entries
is tracked as a follow-up.
```

## Source Provenance

When symbols are indexed, the `Source` metadata captures provenance:

| Field       | Value                                                |
| ----------- | ---------------------------------------------------- |
| `task_id`   | From `PostReviewEvent.TaskID`                        |
| `review_id` | From `PostReviewEvent.ReviewID`                      |
| `actor`     | `"actor:system:symbol_indexer"`                      |
| `reason`    | From `PostReviewEvent.Reason` (e.g. `"post_review"`) |

## Job Entrypoints

The symbol indexer exposes two job types for programmatic use:

- **`code_symbol_index.init_files`** – Initial indexing for new files.
- **`code_symbol_index.update_files`** – Update/delete for changed files.

Both accept `JobArgs`:

```go
type JobArgs struct {
    WorkspaceID string         `json:"workspace_id"`
    Files       []JobFileInput `json:"files"`
    Reason      IndexReason    `json:"reason,omitempty"`
    TaskID      string         `json:"task_id,omitempty"`
    ReviewID    string         `json:"review_id,omitempty"`
}
```

And return `JobResult`:

```go
type JobResult struct {
    Summary  JobSummary  `json:"summary"`
    Failures []JobFailure `json:"failures,omitempty"`
}
```

## Related Specs

- `docs/spec/code_symbol_index_and_swe_grep.md` – Data model and indexer
  behavior
- `docs/spec/post_review_harness.md` – Post-review event schema
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase4_code_symbol_index_todo.md`
  – Implementation plan
