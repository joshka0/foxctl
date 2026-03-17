# Workspace Embedding Overrides Research

Status: active rollout

## Why This Exists

Workspace-local `llm.*` overrides are already supported.

Workspace-local `embedding.*` overrides are now supported for the main CLI and
semantic/code retrieval path on this branch, but they still require rebuild
discipline because embedding model changes alter persisted vector contracts.

## Current Safe Boundary

Current behavior:

- workspace-local `.agentctl/config.yaml` may override `llm.*`
- workspace-local `.agentctl/config.yaml` may also override `embedding.*`
- `code/semantic_search` and the `index` command family now reload config for
  the resolved target workspace instead of reusing a stale process-global config

That means per-workspace embedding models now flow through the main retrieval
path, but operators still need to rebuild affected stores when switching
providers/models.

## Audit Matrix

| Surface | Scope isolation | Model/dims metadata | Mismatch behavior | Status | Notes |
| --- | --- | --- | --- | --- | --- |
| `internal/storage/memory/store.go` | per-workspace | yes | fail on write/query once metadata exists | safe with rebuild | sqlite writes now persist metadata, but legacy rows still need rebuild/backfill |
| `internal/storage/memory/turso_store.go` | per-workspace | yes | fail on open and write | safe now | `embedding_metadata` persists `provider/model/dimensions` |
| `internal/storage/sessions/store.go` | per-workspace metadata rows in one DB | yes | workspace-scoped queries fail fast; global queries skip mismatched dims | safe with rebuild | session, chunk, and context-window embeddings now share a workspace-scoped contract |
| `internal/storage/sessions/turso_store.go` | global table | yes | fail on open | safe with rebuild | stronger than sqlite sessions path |
| `internal/storage/obsidianindex/store.go` | per-vault DB | yes | fail on semantic ensure/query | safe with rebuild | model-level dimensions metadata now gates note/chunk semantic reuse |
| `internal/searchindex/sql_store.go` | per-workspace rows in one DB | yes | fail on upsert/query | safe with rebuild | workspace metadata now records model + dimensions and rejects mixed state |
| `skills/code_semantic_search` + `cmd/index.go` | workspace-scoped for primary local flows | yes via config reload + store metadata | local/single-workspace flows consistent; multi-workspace remote still mixed | partial | target-workspace config reload is fixed, but multi-workspace remote retrieval still needs clearer contract handling |
| `internal/v2/adapters/sourceimport/embedder_factory.go` | n/a | provider/model resolved | dimensions probe available | helper only | useful for probing/embedder introspection, not enough by itself |

## Concrete Findings

### Safe Now

1. Turso/libSQL named memory stores
- [turso_store.go](../../../internal/storage/memory/turso_store.go)

This is the strongest current surface: metadata is persisted and mismatches fail
hard on open and write.

### Safe With Rebuild

1. SQLite named memory store
- [store.go](../../../internal/storage/memory/store.go)

SQLite named memory now persists workspace-scoped embedding metadata on write
paths and fails similarity queries when the query dimensions do not match that
workspace contract. Existing workspaces with pre-metadata embeddings still need
a rebuild or backfill path, so this is "safe with rebuild" rather than "safe
now".

2. SQLite sessions store
- [store.go](../../../internal/storage/sessions/store.go)

SQLite sessions now persists workspace-scoped metadata rows and resolves
session/chunk/context-window embeddings against the owning session workspace.
Workspace-scoped similarity queries fail fast on mismatched dimensions, while
global queries only score compatible rows. Existing databases still need a
rebuild or metadata backfill path, so this is "safe with rebuild".

3. Turso session store
- [turso_store.go](../../../internal/storage/sessions/turso_store.go)

This is close to safe, but still needs a clearer rebuild/operator story before
workspace-local embedding overrides should be enabled by default.

### Unsafe / Needs Code Changes

1. Obsidian semantic index
- [store.go](../../../internal/storage/obsidianindex/store.go)

`obsidianindex` now persists model-level dimensions metadata and rejects same-
model/different-dimension reuse before semantic note/chunk query paths run.
This moves it from unsafe to safe-with-rebuild, but it still needs a clear
operator-facing rebuild workflow before workspace-local embedding overrides
should be enabled by default.

2. Search index
- [sql_store.go](../../../internal/searchindex/sql_store.go)

`searchindex` now persists workspace-level embedding metadata and rejects mixed
model or dimension state on upsert and vector query. That makes it much closer
to safe, but it still needs a documented rebuild workflow before workspace-local
embedding overrides should be enabled by default.

3. Semantic/code retrieval stack
- [main.go](../../../skills/code_semantic_search/main.go)
- [index.go](../../../cmd/agentctl/cmd/index.go)

These paths now reload workspace-local embedding config for the resolved target
workspace, which fixes the main single-workspace CLI and `code/semantic_search`
flows. The remaining weak spot is multi-workspace remote retrieval, where one
query may still span stores with different embedding contracts.

## Questions Still Open

1. Is all embedding state truly isolated per workspace in every path?
2. Which multi-workspace query paths should fail hard on contract mismatch
   versus degrade?
3. What is the canonical rebuild command set for each affected store?
4. Can two workspaces with different embedding dimensions coexist in one user
   home without `searchindex` or `obsidianindex` interference?

## Minimum Requirement Before Enabling Workspace Embedding Overrides

1. every embedding-backed store persists `provider/model/dimensions`
2. open/query paths validate dimensions consistently
3. mismatch errors include a rebuild hint
4. correction/eval coverage includes at least one dimension-mismatch case
5. docs explain the rebuild workflow clearly

## Recommended Rollout

1. keep workspace-local `llm.*` overrides enabled
2. use workspace-local `embedding.*` with the rebuild commands from
   [docs/general/embedding-rebuilds.md](../../general/embedding-rebuilds.md)
3. first tighten:
   - multi-workspace remote retrieval contract handling
   - any remaining non-index command paths that should explicitly reload config
     for a target workspace
4. if needed, add a feature flag only for the unresolved multi-workspace cases
