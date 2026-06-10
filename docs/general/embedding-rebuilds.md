# Embedding Rebuilds

Canonical rebuild guide for embedding-backed stores after provider/model/dimension
changes.

## When To Use This

Use these rebuild paths when you see embedding model or dimension mismatch
errors, or after intentionally switching embedding providers/models for a
workspace or vault.

## Rebuild Matrix

| Surface | Rebuild command | Notes |
| --- | --- | --- |
| Workspace memory embeddings | `foxctl index init --workspace /path/to/workspace --scope memory` | Rebuilds named-memory embeddings for that workspace. |
| Workspace task embeddings | `foxctl index init --workspace /path/to/workspace --scope tasks` | Rebuilds task embeddings for that workspace. |
| Workspace session embeddings | `foxctl index init --workspace /path/to/workspace --scope sessions` | Rebuilds session summary embeddings for that workspace. |
| Workspace symbol/search embeddings | `foxctl index init --workspace /path/to/workspace --scope symbols` | Rebuilds symbol embeddings and the code-search/searchindex side of the workspace. |
| Obsidian semantic note/chunk embeddings | `foxctl obsidian index build --vault-path /path/to/vault` | Rebuilds the local vault index and semantic note/chunk embeddings. |

## Named-Memory Queue Processing

Named-memory embedding jobs drain through the shared processor in
`internal/intelligence/indexing/embedding/processor.go`. Queue producers,
including LongMem ingestion, should enqueue jobs with the intended
workspace/provider/model/dimension metadata and let the shared processor perform
dimension validation before writing embeddings.

`foxctl agent run` drains a bounded batch of daemon-workspace `kind=memory`
embedding jobs on daemon poll ticks. The `embedding/worker` skill remains the
explicit manual drain path for bounded checks, alternate workspace IDs, and
operational queue inspection.

LongMem ingestion must not call an embedder directly or embed benchmark labels.
Only conversation-derived memory content should become embedding input; expected
answers, evidence labels, case IDs, and eval metadata belong in provenance or
eval artifacts.

## Full ContextWiki Refresh

When repo docs, bridge metadata, or vault structure changed alongside embedding
model changes, rebuild the full ContextWiki layer:

```bash
foxctl obsidian graph build --workspace /path/to/workspace --vault-path /path/to/vault
foxctl obsidian graph promote --workspace /path/to/workspace --vault-path /path/to/vault
foxctl obsidian bridge reconcile --workspace /path/to/workspace --vault-path /path/to/vault
foxctl obsidian index build --vault-path /path/to/vault
```

## Notes

- Prefer rebuilding only the affected scope first.
- Workspace-scoped SQLite stores now tolerate mixed embedding dimensions across
  workspaces more cleanly, but a workspace that already has stale embeddings
  still needs a rebuild.
- If you run the CGO-backed CLI, make sure the binary you are invoking is the
  one you just rebuilt.
