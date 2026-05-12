---
title: Storage, CAS, and persistence
description: Understand foxctl's local stores, CAS artifacts, observability persistence, backup flow, and rebuildable indexes.
---

foxctl uses several storage classes with different durability expectations. Production operators and skill developers must distinguish canonical state (preserve and migrate) from rebuildable local projections (regenerate from inputs).

## Path roots

Defaults come from `internal/platform/config/config.go`:

| Config key | Default | Purpose |
|---|---|---|
| `storage.root` | `~/.foxctl/storage` | Root for all database files |
| `paths.cas` | `~/.foxctl/cas` | Content-addressable storage |
| `paths.cache` | `~/.foxctl/cache` | Transient cache files |
| `paths.jobs` | `~/.foxctl/jobs` | Async job tracking |
| `paths.observability` | `~/.foxctl/observability` | Event persistence |

Override any path with the corresponding environment variable (e.g. `FOXCTL_STORAGE_ROOT`, `FOXCTL_PATHS_CAS`, `FOXCTL_OBS_DIR`).

## Store classes

The canonical store registry lives in `internal/storage/registry.go`. Each store has a class that determines its backup and sync policy:

| Class | Examples | Production stance |
|---|---|---|
| `sync_critical` | `coordination.db`, `sessions.db`, `tasks.db`, `mailbox.db`, `agents.db`, `memory.db`, `companion.db`, `contextvar.db` | Core continuity and agent/runtime state. Preserve and migrate deliberately. |
| `sync_useful` | `knowledge.db`, `teams.db`, `trajectory.db` | Useful cross-device context. Back up regularly. |
| `local_only` | `blackboard.db`, `cache.db`, `jobs.db`, `quotas.db`, `graph.db`, `repoindex/<key>.db`, `embedding_queue.db`, `summary_queue.db` | Rebuildable or device-local state. Regenerate from canonical inputs. |
| `observability` | `events.db` | Structured event persistence. See [Observability](/operations/observability/) for query patterns. |
| `external` | `opencode.db` | External import surface. |

### Database schemas

| Database | Key tables |
|---|---|
| `memory.db` | `named_memory`, `embedding_metadata`, `indexer_state` |
| `contextvar.db` | `context_variables`, `context_sequences` |
| `companion.db` | `companion_turns`, `companion_events`, `companion_hard_state_entries`, `companion_soft_episodes`, `companion_evidence_snippets`, `companion_assumptions_ledger` |
| `repoindex/<key>.db` | Graph nodes, edges, index metadata per workspace |

## Database drivers

The storage layer supports per-store database drivers through `internal/storage/dbdriver`:

| Driver | Use case |
|---|---|
| `sqlite` | Default. Local development, single-node. |
| `turso` | Remote SQLite-compatible. Cloud deployments. |
| `postgres` | Horizontal scaling, shared state. |

Driver selection uses per-store config with fallback:

1. `FOXCTL_<STORE>_DB_DRIVER` (store-specific)
2. `FOXCTL_DB_DRIVER` (global fallback)
3. `sqlite` (default)

### PostgreSQL configuration

For PostgreSQL-backed stores, set the DSN environment variable. The DSN is loaded via `FOXCTL_<STORE>_POSTGRES_DSN` with fallback to `FOXCTL_POSTGRES_DSN` then `DATABASE_URL`. Connection pooling controls:

- `FOXCTL_POSTGRES_MAX_CONNS` — maximum connections (default: 10)
- `FOXCTL_POSTGRES_MAX_IDLE_CONNS` — maximum idle connections (default: 5)

Each logical store runs as a separate PostgreSQL schema (default schema name is the lower-cased store name). Migrations run under advisory lock per schema to prevent concurrent migration conflicts across pods.

See [PostgreSQL storage architecture](https://github.com/joshka0/foxctl/blob/main/docs/architecture/postgres-storage.md) for full details on DSN wiring, pgvector detection, and pooling behavior.

## Content-addressable storage (CAS)

CAS stores large outputs by SHA-256 digest. When a skill produces output exceeding the inline threshold (default 32KB), the payload moves to CAS and the envelope returns a summary plus an artifact digest.

### CAS backends

| Backend | Config key | Use case |
|---|---|---|
| `file` | `FOXCTL_CAS_DRIVER=file` | Local filesystem (default) |
| `sqlite` | `FOXCTL_CAS_DRIVER=sqlite` | Single-file CAS |
| `turso` | `FOXCTL_CAS_DRIVER=turso` | Remote SQLite-compatible |
| `s3` | `FOXCTL_CAS_DRIVER=s3` | Enterprise object store |

For S3-backed CAS:

```bash
FOXCTL_CAS_DRIVER=s3
FOXCTL_CAS_S3_BUCKET=my-cas-bucket
FOXCTL_CAS_S3_REGION=us-east-1
FOXCTL_CAS_S3_ENDPOINT=https://s3.example.com  # optional, for MinIO
FOXCTL_CAS_S3_PREFIX=foxctl/
FOXCTL_CAS_S3_FORCE_PATH_STYLE=true
```

### CAS contract

| Rule | Behavior |
|---|---|
| Addressing | SHA-256 digest (`sha256:...`) |
| Inline threshold | Large outputs move to CAS with summary + artifact pointer |
| Integrity | Reads re-validate digest and fail on mismatch |
| Retention | Use `pin`/`gc` controls for lifecycle management |

### CAS commands

```bash
# Store an artifact
foxctl cas put < artifact.json

# Retrieve by digest
foxctl cas get sha256:abc123...

# Pin an artifact to prevent garbage collection
foxctl cas pin sha256:abc123...

# Garbage collect unpinned artifacts older than 7 days
foxctl cas gc --older-than=168h --dry-run
```

### Artifactized envelope

When a skill artifactizes output, the envelope returns a compact summary instead of the full payload:

```json
{
  "data": {
    "summary": "Evidence bundle with 247 records across 3 files",
    "artifact": "sha256:abc123..."
  }
}
```

See [Protocol v1](/reference/protocol-v1/) for the full artifactization rules.

## Observability persistence

Events use a hybrid persistence model with five modes:

| Mode | Description | Use case |
|---|---|---|
| `PersistDefault` | NDJSON file | Most events — fast, append-only |
| `PersistNDJSON` | Explicit NDJSON file | Same as default, explicit selection |
| `PersistSQL` | Direct SQLite write | High-value events needing queryability |
| `PersistHybrid` | NDJSON + background SQLite sync | Fast writes + queryability |
| `PersistNone` | No persistence | Events only for sampling/logging |

Storage locations:

| File | Location |
|---|---|
| NDJSON events | `$FOXCTL_OBS_DIR/events/foxcular_events.ndjson` |
| SQLite database | `$FOXCTL_OBS_DIR/events.db` |
| Custom NDJSON | `$FOXCTL_OBS_DIR/events/<name>.ndjson` |

The background syncer runs every 30 seconds with a batch size of 100 events by default. See [Observability](/operations/observability/) for query examples.

## Backup and database inspection

```bash
# Create a named backup
foxctl backup create --name nightly

# List existing backups
foxctl backup list

# Inspect a specific database
foxctl db --help
```

## Storage invariants

| Invariant | Why it matters |
|---|---|
| Deterministic schema migrations | Keeps CLI/daemon startup predictable |
| Store class boundaries | Prevents accidental syncing of local-only data |
| CAS for large payloads | Keeps envelopes small and replayable |
| Context cancellation through storage calls | Prevents stuck long-running DB operations |

## Production boundaries

- Do not treat local repoindex databases as source of truth — they are rebuildable projections.
- Preserve `repo_key` and workspace identity boundaries when moving artifacts.
- Keep generated or local-only database files out of git.
- Record whether a store is file, sqlite/libsql/Turso, Postgres, or remote.
- When running in a restricted filesystem sandbox, set writable paths explicitly:

```bash
FOXCTL_STORAGE_ROOT=/tmp/foxctl/storage \
FOXCTL_PATHS_CAS=/tmp/foxctl/cas \
FOXCTL_OBS_DIR=/tmp/foxctl/observability \
foxctl run <skill>
```

