# Implementation Plan: Annotation Embedding & Enhanced Recall

## Problem Statement

1. `session/annotate` only persists turn metadata in `session_chunks`; does not write structured TOC rows to `annotations.db`, so TOC browsing remains unsupported.
2. Annotation embedding work is not queue-native: the existing queue payload is symbol-specific (`SymbolID`, `FilePath`, `SymbolName`) and cannot be reused for per-turn annotations.
3. `sessions.SearchChunks` has no session/workspace filtering, forcing `session/recall` to over-fetch and post-filter.
4. `annotations.Store.SearchSimilar` uses `[]byte` while session/store symbol flows use `[]float32`.
5. `session/recall` needs session-picker mode (`list_sessions` + `session_id`), TOC browsing (`toc`), and session-scoped retrieval.
6. `hooks/session_end` does not trigger annotation automatically.

## Architecture Decision

- **Dual-write** in `session/annotate`: always write legacy `session_chunks`, and additionally write rich `TurnAnnotation` rows to `annotations.db`.
- Dedicated **annotation queue wrapper** in `internal/storage/annotations` using generic `internal/queue` package.
- **Option-based retrieval methods** on `SessionStore` (non-breaking): filtered chunk/context-window search by `workspace`/`session_id`.
- Keep inline annotation flow unchanged by default; add opt-in queue-backed embedding and optional queue-based backfill in `embedding_worker`.
- Hook-triggered auto-annotation through `hooks/session_end` via `hooks.ActionRunSkill`.

### Embedding Dimension Contract

- Both inline and annotation queue modes use `semantic.ScopeSessions` → resolves to voyage-3.5 (1024 dims).
- Worker validates `len(result.Vec) == embedder.Dimensions()` before persistence.
- Local `embedding_model` override in `session/annotate` is inline-only — does NOT enqueue to the Voyage-backed annotation queue.

## Design Patterns

- **Additive API Evolution**: Extend interfaces and stores with new methods rather than replacing existing ones.
- **Dual-write (compat + migration)**: Preserve existing `session_chunks` behavior while enabling richer annotation queries from `turn_annotations`.
- **Queue Wrapper Pattern**: Annotation-specific payload and job type in `internal/storage/annotations/queue.go`, backed by generic `queue.Store`.
- **Mode-driven UX**: `session/recall` supports independent modes (`list_sessions`, `toc`, existing semantic modes).
- **Best-effort Side Effects**: Non-critical persistence/queue failures in `session/annotate` should not block core chunk save path.

## File Changes

### `internal/storage/interfaces.go` (modified)

Add search option types:

```go
type ChunkSearchOptions struct {
    Workspace string // workspace path or ID
    SessionID string
}

type ContextWindowSearchOptions struct {
    Workspace string
    SessionID string
}
```

Extend `SessionStore` interface with:
- `SearchChunksWithOptions(ctx, embedding []float32, limit int, opts ChunkSearchOptions) ([]ScoredChunk, error)`
- `SearchContextWindowsWithOptions(ctx, embedding []float32, limit int, opts ContextWindowSearchOptions) ([]ScoredContextWindow, error)`

Keep existing methods as compatibility surface.

### `internal/storage/sessions/store.go` (modified)

- Implement `SearchChunksWithOptions` with SQL:
  - Join `session_chunks` with `sessions` for workspace/session filtering
  - Apply optional `sc.session_id = ?` and workspace filter using `resolveWorkspaceSelector`
  - `embedding IS NOT NULL AND LENGTH(embedding) > 0`
- Delegate `SearchChunks` → `SearchChunksWithOptions` with zero-value options
- Similarly for `SearchContextWindowsWithOptions`

### `internal/storage/sessions/turso_store.go` (modified)

Mirror new methods for interface conformance.

### `internal/storage/annotations/store.go` (modified)

- **API normalization**: Change `SearchSimilar(ctx, embedding []byte, limit int)` → `SearchSimilar(ctx, embedding []float32, limit int)`
  - Skip candidates where `len(candidateEmbedding) != len(queryEmbedding)`
- Add new methods:
  - `ListWithoutEmbedding(ctx, sessionID string, limit int) ([]*TurnAnnotation, error)`
  - `SetEmbedding(ctx, sessionID string, turnIndex int, embedding []float32, model string, embeddingText string) error`

### `internal/storage/annotations/queue.go` (new)

Annotation-specific queue wrapper using `internal/queue`:

```go
type AnnotationEmbeddingPayload struct {
    SessionID     string `json:"session_id"`
    TurnIndex     int    `json:"turn_index"`
    EmbeddingText string `json:"embedding_text"`
}

type Queue struct { q *queue.Store }

type AnnotationEmbeddingJob struct {
    ID            string
    SessionID     string
    TurnIndex     int
    EmbeddingText string
}
```

Methods: `Open`, `Enqueue`, `ClaimNext`, `Complete`, `Fail`, `Close`.
- Dedupe key: `fmt.Sprintf("annotation:%s:%d", sessionID, turnIndex)`
- Queue table: `annotation_embedding_jobs`

### `skills/session_annotate/main.go` (modified)

- Extend `Input`: add `QueueEmbedding bool`
- Extend `Output`: add `EmbeddingsQueued int`
- Open annotations store once (best effort; fallback to chunk-only if unavailable)
- In per-turn loop:
  - Build and save `SessionChunk` as now (unchanged)
  - Build `TurnAnnotation` from same feature set with rich fields (TOCLabel, TOCCategory, Intent, etc.)
  - Save to `annotationStore.Save()`; non-fatal on failure
  - If `QueueEmbedding == true`, enqueue annotation embedding job (skip if local model override set)

### `skills/session_annotate/skill.yaml` (modified)

Add `queue_embedding` parameter:
```yaml
- name: queue_embedding
  type: boolean
  required: false
  description: Queue annotation embedding jobs for async processing/backfill
```

### `skills/session_recall/main.go` (modified)

- Extend `Input`: `SessionID`, `ListSessions`, `TOC`
- Extend `Output`: `SessionList []RecallSessionRef`, `TOCMatches []RecallTOCMatch`
- Mode flow:
  1. `list_sessions`: call `sessionStore.List(ctx, opts)` → return session refs
  2. `toc`: require `SessionID`, open annotations, call `ListBySession` → return TOC matches
  3. Existing modes: use `SearchChunksWithOptions` with `SessionID`/`Workspace` scoping

### `skills/session_recall/skill.yaml` (modified)

Add `session_id`, `list_sessions`, `toc` parameters and `session_list`, `toc_matches` return fields.

### `skills/embedding_worker/main.go` (modified)

- Add annotation processing mode inputs: `ProcessAnnotations`, `AnnotationSessionID`, `AnnotationBackfill`
- Open annotation stores + queue
- Annotation worker loop:
  - Backfill: `ListWithoutEmbedding` → enqueue jobs
  - Process: claim → embed with `ScopeSessions` → validate dims → `SetEmbedding` → complete/fail
- Preserve existing symbol queue flow unchanged

### `skills/hooks_session_end/main.go` (modified)

- Append `hooks.RunSkillAction("session/annotate", args)` to output actions
- Args: `session_id`, `workspace`, `queue_embedding: true`
- Non-blocking: if session_id missing or serialization fails, return existing behavior

## Testing Strategy

### Unit Tests
- `SearchChunksWithOptions`: workspace ID/path/session_id filtering, no-result behavior
- `annotations.SearchSimilar` with `[]float32`, dimension mismatch skipping
- `annotations.ListWithoutEmbedding` and `SetEmbedding`
- Queue wrapper: enqueue idempotency, claim/complete/fail lifecycle

### Integration Tests
- Dual-write: verify both `session_chunks` and `turn_annotations` written for same turn
- Recall modes: `list_sessions`, `session_id`-scoped chunk search, `toc` mode
- Embedding worker: annotation queue claim → generate → store path
- Hook: `hooks_session_end` output includes `run_skill` action

### Operational Validation
- Run worker with `ProcessAnnotations=true` on historical sessions with missing embeddings

## Error Handling

- `session_annotate`: annotation DB failures are non-fatal; chunk save continues, warning surfaced
- `session_recall`: `toc` without `session_id` → validation error; no results → `status: "no_matches"`
- `annotations.SearchSimilar`: skips dimension-mismatched candidates
- `embedding_worker`: dimension/storage failures fail the queue job (retryable); missing provider returns auth error

## Implementation Order

Dependency graph:
```
Step 1 (interfaces) ──→ Step 2 (sessions search) ──→ Step 7 (recall modes)
                    ╲
Step 3 (annotations API + queue) ──→ Step 4 (annotate dual-write)
                                 ╲──→ Step 6 (worker annotation mode)
Step 8 (hook auto-annotation) — independent
```

1. `internal/storage/interfaces.go` — new option types and method signatures
2. `internal/storage/sessions/store.go` + `turso_store.go` — SearchChunksWithOptions
   - **Checkpoint**: storage builds, existing SearchChunks unchanged
3. `internal/storage/annotations/store.go` — API normalization + new methods
4. `internal/storage/annotations/queue.go` (new) — annotation queue wrapper
   - **Checkpoint**: annotation store builds with old and new signatures
5. `skills/session_annotate/main.go` + `skill.yaml` — dual-write + queue enqueue
6. `skills/embedding_worker/main.go` — annotation queue processing + backfill
   - **Checkpoint**: annotation queue path processable independently from symbol mode
7. `skills/session_recall/main.go` + `skill.yaml` — list_sessions, toc, session_id scoping
8. `skills/hooks_session_end/main.go` — RunSkillAction for auto-annotation
   - **Checkpoint**: session_end output includes run_skill action, preserves existing behavior
