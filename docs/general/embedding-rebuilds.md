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
| Workspace memory embeddings | `agentctl index init --workspace /path/to/workspace --scope memory` | Rebuilds named-memory embeddings for that workspace. |
| Workspace task embeddings | `agentctl index init --workspace /path/to/workspace --scope tasks` | Rebuilds task embeddings for that workspace. |
| Workspace session embeddings | `agentctl index init --workspace /path/to/workspace --scope sessions` | Rebuilds session summary embeddings for that workspace. |
| Workspace symbol/search embeddings | `agentctl index init --workspace /path/to/workspace --scope symbols` | Rebuilds symbol embeddings and the code-search/searchindex side of the workspace. |
| Obsidian semantic note/chunk embeddings | `agentctl obsidian index build --vault-path /path/to/vault` | Rebuilds the local vault index and semantic note/chunk embeddings. |

## Full ACA Refresh

When repo docs, bridge metadata, or vault structure changed alongside embedding
model changes, rebuild the full ACA layer:

```bash
agentctl obsidian graph build --workspace /path/to/workspace --vault-path /path/to/vault
agentctl obsidian graph promote --workspace /path/to/workspace --vault-path /path/to/vault
agentctl obsidian bridge reconcile --workspace /path/to/workspace --vault-path /path/to/vault
agentctl obsidian index build --vault-path /path/to/vault
```

## Notes

- Prefer rebuilding only the affected scope first.
- Workspace-scoped SQLite stores now tolerate mixed embedding dimensions across
  workspaces more cleanly, but a workspace that already has stale embeddings
  still needs a rebuild.
- If you run the CGO-backed CLI, make sure the binary you are invoking is the
  one you just rebuilt.
