# Sessions

Machine-friendly reference for session lifecycle, lineage, and retrieval.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical packages | `internal/storage/sessions`, `cmd/agentctl/cmd/sessions.go`, `skills/session_restore`, `skills/session_summarize`, `skills/session_recall`, `internal/sessionkit` |
| Last reviewed | 2026-02-17 |

## Scope

Sessions capture agent/user interaction history, lineage edges, context windows, chunk summaries, and restore metadata for continuity across compaction and resume.

## CLI Surfaces (`agentctl sessions`)

| Command | Purpose |
|--------|---------|
| `list`, `show`, `search`, `stats` | Inspect stored sessions |
| `capture`, `import`, `summarize`, `windows`, `export` | Capture and process session data |
| `new`, `resume`, `fork`, `chain`, `close` | Manage active lineage and session state |

Notable contract details:

- `sessions chain` requires `--session <id>`.
- `sessions close` status must be one of `ok`, `error`, `canceled`.

## Skill Contracts

| Skill | Role |
|------|------|
| `session/restore` | Rebuild context after resume/compaction triggers |
| `session/summarize` | Generate window/session summaries and extracted learnings |
| `session/recall` | Retrieve historical sessions and related context |

## Identity Resolution

Session id fallback chain (from `internal/sessionkit/identity.go`):

1. Explicit id input
2. `AGENTCTL_SESSION_ID`
3. `CLAUDE_SESSION_ID`
4. `OPENCODE_SESSION_ID`
5. `CURSOR_SESSION_ID`
6. Identity file under `~/.agentctl/sessions/active/<workspace_hash>-<agent>.json`
7. `TERM_SESSION_ID` (last resort)

## Storage Contract

Source of truth: `internal/storage/sessions/store.go`.

| Table | Purpose |
|------|---------|
| `sessions` | Session header, lineage pointers, agent/LLM context, status |
| `session_turns` | Per-turn previews, tool/error metadata |
| `session_chunks` | Chunked archive metadata + optional embeddings |
| `session_chunk_summaries` | Persisted chunk-window summaries |
| `session_context_windows` | Compaction window tracking with token/chunk bounds |
| `session_edges` | Explicit lineage edges (`continues`, `forked_from`, related) |
| `embedding_metadata` | Embedding model/dimension metadata for session vectors |

## Lifecycle (Condensed)

1. Create or resume active session (`new`/`resume`/`fork`).
2. Persist turns/chunks/edges as work progresses.
3. Summarize on compaction boundaries (`session/summarize`).
4. Set `pending_restore_at` for post-compact restore workflows.
5. Restore and continue with `session/restore` on next start.
6. Close with terminal status (`ok`/`error`/`canceled`).

## Operational Examples

```bash
agentctl sessions list --limit 20
agentctl sessions chain --session <session-id> --depth 10
agentctl sessions close --status ok
agentctl run session/restore --input '{"session_id":"<id>","trigger":"session_start"}'
agentctl run session/recall --input '{"query":"oauth callback failure","limit":10}'
```

## Invariants

| Invariant | Why it matters |
|----------|----------------|
| Lineage uses explicit parent links + edges | Enables deterministic ancestry and graph-based analysis |
| Workspace-aware active identity files | Supports recovery when env vars are absent |
| Session status transitions are explicit | Prevents ambiguous closed/active state |
| Context windows/chunk summaries are persisted | Improves post-compaction continuity and recall |

## Failure Modes

| Symptom | Likely cause |
|--------|---------------|
| `--session is required` on chain | Missing required chain selector |
| Resume cannot find active session | Workspace/agent mismatch or closed status |
| Weak recall quality | Missing/stale summaries or embeddings |

## Related Docs

- [docs/general/storage.md](storage.md)
- [docs/general/memory.md](memory.md)
- [docs/general/search.md](search.md)
- [docs/general/hooks.md](hooks.md)
