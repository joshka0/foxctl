# Storage

Machine-friendly reference for persisted and ephemeral state.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical packages | `internal/storage/*`, `internal/storage/registry.go`, `internal/platform/config` |
| Last reviewed | 2026-02-17 |

## Path Roots (Defaults)

Defaults come from `internal/platform/config/config.go`.

| Config key | Default |
|-----------|---------|
| `storage.root` | `~/.agentctl/storage` |
| `paths.cas` | `~/.agentctl/cas` |
| `paths.cache` | `~/.agentctl/cache` |
| `paths.jobs` | `~/.agentctl/jobs` |
| `paths.observability` | `~/.agentctl/observability` |

## Canonical Store Registry

Source of truth: `internal/storage/registry.go`.

| Class | Examples | Purpose |
|------|----------|---------|
| `sync_critical` | `coordination.db`, `sessions.db`, `tasks.db`, `mailbox.db`, `agents.db`, `memory.db`, `companion.db`, `contextvar.db` | Core continuity and agent/runtime state |
| `sync_useful` | `knowledge.db`, `teams.db`, `trajectory.db` | Useful cross-device context |
| `local_only` | `blackboard.db`, `cache.db`, `jobs.db`, `quotas.db`, `graph.db`, `repoindex/<key>.db`, `embedding_queue.db`, `summary_queue.db` | Rebuildable or device-local state |
| `observability` | `events.db` | Structured event persistence |
| `external` | `opencode.db` | External import surface |

## Schema Reality Checks

This section reflects current table names in code (not legacy aliases).

| Database | Current anchors |
|---------|------------------|
| `memory.db` | `named_memory`, `embedding_metadata`, `indexer_state` |
| `contextvar.db` | `context_variables`, `context_sequences` |
| `companion.db` | `companion_turns`, `companion_events`, `companion_hard_state_entries`, `companion_soft_episodes`, `companion_evidence_snippets`, `companion_assumptions_ledger`, `companion_memory_mode_state`, and related hybrid pipeline tables |
| `repoindex/<key>.db` | graph nodes/edges/index metadata per workspace |

## CAS Contract

| Rule | Behavior |
|------|----------|
| Addressing | SHA-256 digest (`sha256:...`) |
| Inline threshold | Large outputs should move to CAS with summary + artifact pointer |
| Integrity | Reads re-validate digest and fail on mismatch |
| Retention | Use `pin`/`gc` controls for lifecycle |

## Operational Commands

```bash
agentctl cas put < artifact.json
agentctl cas get sha256:...
agentctl cas pin sha256:...
agentctl cas gc --older-than=168h --dry-run
agentctl backup create --name nightly
agentctl backup list
```

## Invariants

| Invariant | Why it matters |
|----------|----------------|
| Deterministic schema migrations | Keeps CLI/daemon startup predictable |
| Store class boundaries | Prevents accidental syncing of local-only data |
| CAS for large payloads | Keeps envelopes small and replayable |
| Context cancellation through storage calls | Prevents stuck long-running DB operations |

## Related Docs

- [docs/general/sessions.md](sessions.md)
- [docs/general/memory.md](memory.md)
- [docs/general/search.md](search.md)
- [docs/architecture/postgres-storage.md](../architecture/postgres-storage.md)
