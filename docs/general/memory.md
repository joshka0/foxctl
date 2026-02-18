# Memory

Machine-friendly reference for named memory persistence and retrieval.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical packages | `internal/storage/memory`, `cmd/agentctl/cmd/memory*`, `skills/session_*` |
| Last reviewed | 2026-02-17 |

## Scope

`memory` covers durable named entries in `memory.db`, workspace scoping, optional embeddings, and recall surfaces used by CLI, hooks, and session summarization.

## Command Surfaces

### CLI (`agentctl memory`)

Source: `cmd/agentctl/cmd/memory.go`.

| Subcommand | Purpose |
|-----------|---------|
| `list` | List named entries (workspace-scoped) |
| `search --query` | Lexical search over names/summaries |
| `get <name>` | Fetch a named entry envelope |
| `put` | Store envelope as named memory |
| `save <job-id>` | Save job result into memory |
| `update <name>` | Update metadata/content |
| `delete <name>` | Delete named memory |
| `relevant --query` | Retrieve likely relevant entries |
| `recent`, `cache`, `stats` | Operational introspection |
| `migrate-workspace` | Workspace-id migration/repair |

### Skill/Runtime integration

| Surface | Purpose |
|--------|---------|
| `session/summarize` + related session hooks | Extract and persist learnings |
| hook scripts (for example `configs/hooks/memory-detector.sh`, `configs/hooks/memory-recall.sh`) | Prompt/capture/recall workflow |
| `code/semantic_search` with scope `memories` | Semantic retrieval path |

## Data Contract

Source of truth: `internal/storage/memory/store.go`.

| Table | Role |
|------|------|
| `named_memory` | Primary memory records (`name`, `type`, `workspace`, `summary`, `result`, digests, access stats) |
| `embedding_metadata` | Workspace embedding provider/model/dimensions metadata |
| `indexer_state` | Indexer progress checkpoints per workspace/indexer |

Important `named_memory` constraints:

- Unique key: `(name, workspace)`
- Workspace is canonicalized before read/write
- Optional columns exist for embeddings and atomic enrichment (`embedding`, `atomic_text`, `entities`, `keywords`)

## Retrieval Modes

| Mode | Command path | Notes |
|-----|---------------|-------|
| Lexical | `agentctl memory search --query ...` | String match in memory store |
| Semantic | `agentctl run code/semantic_search --input '{"query":"...","scope":["memories"]}'` | Uses embedding-backed retrieval pipeline |

## Embedding and Model Selection

Defaults and overrides come from config + semantic provider logic.

| Setting | Meaning |
|--------|---------|
| `VOYAGE_API_KEY` / `GEMINI_API_KEY` | Embedding provider keys |
| `AGENTCTL_EMBEDDING_MODEL_<SCOPE>` | Per-scope override (for example `..._MEMORY`) |
| `AGENTCTL_EMBEDDING_MODEL_TEXT` | Text-scope fallback override |
| `AGENTCTL_EMBEDDING_RATE_LIMIT` | Provider-side request rate control |

## Operational Examples

```bash
agentctl memory list --limit 20
agentctl memory search --query "oauth callback" --limit 10
agentctl memory get gotcha-oauth-state
agentctl memory put --name gotcha-oauth-state --type gotcha --summary "state must be validated" --data '{"note":"..."}'
agentctl run code/semantic_search --input '{"query":"oauth gotcha","scope":["memories"],"limit":5}'
```

## Invariants

| Invariant | Why it matters |
|----------|----------------|
| Workspace canonicalization on all queries | Prevents duplicate/missed entries across path variants |
| Unique `(name, workspace)` key | Stable upsert/get behavior |
| Embedding dimension validation | Prevents mixed-model vector corruption |
| Envelope payloads stored as memory result blobs | Preserves replayable command context |

## Failure Modes

| Symptom | Likely cause |
|--------|---------------|
| `memory: not found` | Name/workspace mismatch |
| Embedding dimension mismatch errors | Provider/model drift across indexing runs |
| Weak semantic results | Missing embedding provider key or stale embeddings |

## Related Docs

- [docs/general/storage.md](storage.md)
- [docs/general/search.md](search.md)
- [docs/general/hooks.md](hooks.md)
- [docs/general/companion-memory.md](companion-memory.md)
