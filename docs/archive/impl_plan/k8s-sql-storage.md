# Plan: PostgreSQL (RDS + pgvector) Storage Backend for K8s Enterprise Deployment

> **Type:** Implementation plan (historical)
> **Current architecture status:** Implemented with driver-based PostgreSQL support and dedicated k8s overlays in `internal/storage/dbdriver/*`, `internal/storage/dbutil/*`, `deploy/kubernetes/overlays/postgres`, and `docs/architecture/postgres-storage.md`.
> **Active docs for architecture:** [`docs/architecture/postgres-storage.md`](../../architecture/postgres-storage.md).

## Context

foxctl already supports Kubernetes deployments backed by Turso/libSQL (see `docs/guides/kubernetes.md`). This plan is for environments that require a self-hosted/shared SQL control-plane (AWS RDS PostgreSQL) for enterprise multi-pod deployments.

foxctl currently uses per-store SQLite/libSQL databases selected via the dbdriver env-var convention (e.g. `AGENTCTL_SESSIONS_DB_DRIVER`). This works well for single-node CLI usage but is not sufficient for enterprise K8s deployments where multiple pods (GUI server, chat adapters, background workers) need shared state.

**Goal:** Add PostgreSQL (AWS RDS + pgvector) as an alternative storage backend using the repository pattern (Option B). Local dev stays SQLite; enterprise deploys use PostgreSQL with native vector search.

**Branch:** `feat/storage-k8s-teams-sql`

## Key Decisions (From Review)

1. **Canonical SQL placeholders:** Use PostgreSQL-style `$1..$N` placeholders in queries that must run on Postgres. This also works with the repo's SQLite driver (modernc) and avoids a brittle runtime "rebind" layer, especially for `*sql.Tx` usage.
2. **Store isolation:** Preserve the existing "one logical DB per store" model by mapping each store to its own PostgreSQL schema (preferred) or database. This avoids cross-store table collisions (notably `schema_migrations`) and keeps migrations independent.
3. **Distributed migrations:** In K8s, multiple pods can start at once. PostgreSQL opens must guard migrations with a per-store advisory lock (schema-scoped) to avoid concurrent migration races.
4. **Vector dimensions:** Do not hardcode `1024`. Use the configured dimensions (`AGENTCTL_VECTOR_DIMS` / `cfg.Database.Vector.Dimensions`) and keep metadata tables authoritative to detect mismatches (sessions/memory already track this).
5. **pgvector provisioning:** Long-term production-ready behavior:
   - Migrations attempt `CREATE EXTENSION IF NOT EXISTS vector;` for vector-enabled stores.
   - If this fails due to permissions, either (a) fail fast when `AGENTCTL_POSTGRES_REQUIRE_VECTOR=true`, or (b) log + fall back to Go cosine distance (no HNSW).
6. **JSON columns:** Store JSON as `jsonb` in PostgreSQL for long-term correctness/perf. Keep TEXT in SQLite. Ensure inserts remain parameterized JSON strings (must be valid JSON).
7. **CAS in production:** Use S3/MinIO for blob payloads. Keep any DB-backed CAS as metadata-only (or for very small objects) to avoid Postgres bloat.

## Architecture Overview

```
Local Dev (unchanged):          Enterprise K8s:
┌──────────┐                    ┌──────────┐  ┌──────────┐
│ foxctl │                    │ pod: GUI │  │ pod: Teams│
│ CLI/Web  │                    │ + Web    │  │ adapter   │
└────┬─────┘                    └────┬─────┘  └────┬──────┘
     │                               │              │
┌────▼─────┐                    ┌────▼──────────────▼─────┐
│ SQLite   │                    │  RDS PostgreSQL         │
│ (local)  │                    │  + pgvector (HNSW)      │
└──────────┘                    └─────────────────────────┘
```

## Store Classification

Not all stores need PostgreSQL. Classify by deployment need:

| Tier | Stores | Why PostgreSQL |
|------|--------|----------------|
| **Tier 1 (Shared State)** | coordination, sessions, tasks, mailbox, agents, memory, companion, contextvar | Multi-pod reads/writes; required for "shared brain" |
| **Tier 2 (Shared / Optional)** | knowledge, teams, trajectory | Useful across pods/instances, but can be deferred |
| **Tier 3 (Keep Local Initially)** | cache, quotas, testwatch, contextbuffer, graph, embedding_queue, summary_queue, daemon_dedupe, patterns, post_review, repoindex, conversation_settings, blackboard, board | Rebuildable or intentionally local-only; revisit if you need true horizontal workers |
| **Tier 4 (CAS)** | CAS metadata + object store | Prefer S3/MinIO for blobs; DB only for metadata/small payloads |

Focus initial PostgreSQL support on Tier 1. Tier 2 is straightforward once Tier 1 works.

---

## Phase 1: SQL Portability Layer (Dialects + Placeholder Normalization)

**Goal:** Make Tier 1 stores portable across SQLite/libSQL and PostgreSQL without maintaining two copies of every query.

### 1.0 Canonical Placeholder Style (`$1..$N`)

Before adding Postgres, normalize Tier 1 store queries (and shared helpers like `internal/storage/sqlutil/*`) away from `?` placeholders to `$1..$N`.

Rationale:
- Postgres requires `$N`.
- The repo's SQLite driver accepts `$N` placeholders with positional args.
- This avoids missing "rebind" at transaction callsites (`*sql.Tx`) and keeps the migration surface smaller.

### 1.1 New file: `internal/storage/dbdriver/dialect.go`

```go
type Dialect interface {
    // Name returns "sqlite" or "postgres"
    Name() string

    // Rebind rewrites "?" placeholders in a query to the dialect's format.
    //
    // Prefer normalizing Tier 1 stores to `$1..$N` instead of relying on runtime rebinding
    // (store code commonly uses *sql.DB/*sql.Tx directly). Keep Rebind for transitional callsites
    // that do use dbdriver.DB.
    Rebind(query string) string

    // Now returns SQL expression for current timestamp.
    // SQLite: "datetime('now')", PostgreSQL: "NOW()"
    Now() string

    // JSONExtract returns SQL for extracting a JSON text field.
    // SQLite: "json_extract(col, '$.key')"
    // PostgreSQL: "col->>'key'" (cast legacy TEXT columns as needed: "(col::jsonb)->>'key'")
    JSONExtract(column, key string) string

    // JSONType returns the preferred column type for JSON payloads.
    // SQLite: "TEXT". PostgreSQL: "JSONB".
    JSONType() string

    // BlobType returns column type for binary data.
    // SQLite: "BLOB", PostgreSQL: "BYTEA"
    BlobType() string

    // VectorType returns column type for vector embeddings.
    // SQLite: "BLOB", libSQL: "F32_BLOB(dims)", PostgreSQL: "vector(dims)"
    VectorType(dims int) string

    // UpsertIgnore wraps an INSERT to ignore conflicts.
    // Prefer "INSERT INTO ... ON CONFLICT DO NOTHING" since it works in both SQLite and PostgreSQL.
    UpsertIgnore(insert string) string

    // TableExists returns SQL to check if a table exists.
    TableExists(tableName string) string

    // ColumnExists returns SQL to check if a column exists on a table.
    ColumnExists(tableName, columnName string) string
}
```

### 1.2 `internal/storage/dbdriver/dialect_sqlite.go`

Implement `SQLiteDialect` — mostly pass-through (current behavior).

### 1.3 `internal/storage/dbdriver/dialect_postgres.go`

Implement `PostgresDialect`:
- `Rebind()`: Rewrite `?` → `$1, $2, ...` (same algorithm as `sqlx.Rebind`)
- `JSONType()`: Return `JSONB`
- `JSONExtract()`: Use JSONB operators. If a legacy column is TEXT, cast (e.g. `(column::jsonb)->>'key'`)
- `VectorType()`: Return `vector(dims)` (pgvector extension)
- `UpsertIgnore()`: Prefer `ON CONFLICT DO NOTHING` (works in both SQLite and Postgres)

### 1.4 Extend `DB` interface in `internal/storage/dbdriver/driver.go`

Add method:
```go
GetDialect() Dialect
```

All existing drivers return `SQLiteDialect{}`. New PostgreSQL driver returns `PostgresDialect{}`.

### 1.5 Files to modify

| File | Change |
|------|--------|
| `internal/storage/dbdriver/driver.go` | Add `GetDialect() Dialect` to DB interface |
| `internal/storage/dbdriver/sqlite.go` | Implement `GetDialect()` returning `SQLiteDialect{}` |
| `internal/storage/dbdriver/libsql.go` | Implement `GetDialect()` returning `SQLiteDialect{}` |
| `internal/storage/dbdriver/turso.go` | Implement `GetDialect()` returning `SQLiteDialect{}` |
| `internal/storage/dbdriver/compat.go` | Update `WrapSQLDB()` to accept dialect |

### 1.6 Checkpoint

```bash
make build && go test ./internal/storage/dbdriver/...
```

---

## Phase 2: PostgreSQL Driver

**Goal:** Add `DriverPostgres` so stores can connect to PostgreSQL through the existing `*sql.DB` interface.

### 2.1 New: `internal/storage/dbdriver/postgres.go`

```go
type PostgresConfig struct {
    DSN             string        // "postgres://user:pass@host:5432/dbname?sslmode=require"
    Schema          string        // default: store name lowercased (used for search_path)
    MaxOpenConns    int           // default: 25
    MaxIdleConns    int           // default: 5
    ConnMaxLifetime time.Duration // default: 1h
    ConnMaxIdleTime time.Duration // default: 30m
}
```

Implementation:
- Use `github.com/jackc/pgx/v5/stdlib` to get `*sql.DB` from pgx
- Register pgvector types via `pgx/v5` `AfterConnect` callback
- Return a `postgresDB` struct implementing `dbdriver.DB`
- `IsVectorSearchEnabled()` should detect pgvector extension availability (and/or be configurable); do not assume it always exists
- `GetDialect()` returns `PostgresDialect{}`
- `GetDriverType()` returns `DriverPostgres`
- Set `search_path` to the per-store schema (from config) for store isolation
- Guard migrations with a per-schema advisory lock (safe for multi-pod startup)

### 2.2 Update `internal/storage/dbdriver/config.go`

Add constant and config:
```go
const DriverPostgres DriverType = "postgres"
```

### 2.3 Update `internal/storage/dbdriver/config_loader.go`

Extend `LoadConfig()` to handle `driver = "postgres"`:
- Read `AGENTCTL_POSTGRES_DSN` (or `DATABASE_URL` as fallback)
- Read pool settings: `AGENTCTL_POSTGRES_MAX_CONNS`, etc.
- Per-store DSN override: `AGENTCTL_{STORE}_POSTGRES_DSN`
- Derive per-store schema name (default: lowercase store name)

### 2.4 Update `internal/storage/dbutil/open.go`

Extend `OpenStoreDB()` to handle `DriverPostgres`:
- Preserve store isolation with a per-store schema (preferred): `agents.*`, `sessions.*`, etc.
- Ensure schema exists and set `search_path` for the pool
- Guard migrations with an advisory lock per schema so multiple pods can start safely

Notes:
- If you want a single shared pool across all stores, you must schema-qualify every query/table name. The schema-per-store + per-store pool approach keeps query changes smaller and matches current architecture.

### 2.5 PostgreSQL Migration Helpers

New: `internal/storage/dbutil/migrate_postgres.go`

PostgreSQL-aware migration that uses `information_schema` checks instead of SQLite's error-swallowing `ALTER TABLE ADD COLUMN`:

```go
func AddColumnIfNotExists(ctx context.Context, db *sql.DB, table, column, colType, defaultVal string) error
```

### 2.6 New dependencies

```bash
go get github.com/jackc/pgx/v5
go get github.com/pgvector/pgvector-go
```

### 2.7 Checkpoint

```bash
docker run -d --name foxctl-pg -p 5432:5432 \
  -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=foxctl \
  pgvector/pgvector:pg17-v0.8.0

AGENTCTL_DB_DRIVER=postgres \
AGENTCTL_POSTGRES_DSN="postgres://postgres:dev@localhost:5432/foxctl?sslmode=disable" \
  go test ./internal/storage/dbdriver/...
```

---

## Phase 3: Store Migration Scripts (Tier 1 Stores)

**Goal:** Make Tier 1 stores' migrations PostgreSQL-compatible.

### 3.1 Pattern: Dialect-aware migrations

Each store's `migrate()` should avoid SQLite-only constructs (notably `pragma_table_info`) and multi-statement `Exec` assumptions.

Expect real edits here:
- Several existing migrations check columns using SQLite `pragma_table_info(...)` and need Postgres equivalents (`information_schema`).
- Many migrations use multi-statement `ddl := \`...\`` strings. Ensure these are executed in a Postgres-safe way (split statements or execute individually).
- Where columns are logically JSON, use `dialect.JSONType()` (SQLite: TEXT, Postgres: JSONB). Inserts can remain JSON strings as long as they are valid JSON.

### 3.2 Stores to update

| Store | File | Key DDL Differences |
|-------|------|---------------------|
| memory | `internal/storage/memory/store.go` | Refactor Open() to use `OpenStoreDB` (not `OpenSQLiteDBShared`); remove SQLite-only checks; optional pgvector; use configured dimensions |
| sessions | `internal/storage/sessions/store.go` | Replace `pragma_table_info`; embedding metadata compatibility |
| tasks | `internal/storage/tasks/store.go` | Placeholder normalization + any embedding columns |
| agents | `internal/storage/agents/store.go` | Straightforward TEXT columns |
| companion | `internal/context/companion/memory.go` | Conversation titles, messages |
| mailbox | `internal/storage/mailbox/store.go` | Simple TEXT + JSON columns |
| coordination | `internal/storage/coordination/store.go` | Lease upserts + placeholder normalization |
| contextvar | `internal/storage/contextvar/store.go` | Tier 1 store in `internal/storage/registry.go` |

### 3.3 Query placeholder handling

Use `$1..$N` placeholders everywhere for Tier 1 stores (Phase 1.0). Do not rely on runtime rebinding.

### 3.4 SQLite-specific function replacement

Known SQLite-specific query patterns to replace:

| Store | Function | Fix |
|-------|----------|-----|
| sessions | `pragma_table_info(...)` | Use `information_schema.columns` (or `ALTER TABLE .. ADD COLUMN IF NOT EXISTS`) |
| trajectory | `json_extract()` | Use `dialect.JSONExtract()` |
| graph | `datetime('now')` | Use `dialect.Now()` |

### 3.5 `INSERT OR IGNORE` replacement

| Store | Fix |
|-------|-----|
| `sqlutil/migration.go` | Prefer `ON CONFLICT DO NOTHING` + `$N` placeholders |
| `trajectory/workspace_repair.go` | Prefer `ON CONFLICT DO NOTHING` |
| `jobs/persist/store.go` | Prefer `ON CONFLICT DO NOTHING` |

### 3.6 Checkpoint

```bash
AGENTCTL_DB_DRIVER=postgres AGENTCTL_POSTGRES_DSN="..." \
  go test ./internal/storage/memory/... ./internal/storage/sessions/... ./internal/storage/tasks/...
```

---

## Phase 4: pgvector Integration

**Goal:** Native vector search with HNSW indexing in PostgreSQL.

### 4.1 New: `internal/storage/dbdriver/vector_postgres.go`

PostgreSQL VectorHelper using pgvector operators:
- `CosineSimilarity()` → `1 - (col <=> query)` 
- `TopK()` → `ORDER BY col <=> $1 LIMIT k`
- `CreateHNSWIndex()` → `USING hnsw (col vector_cosine_ops) WITH (m=16, ef_construction=64)`

### 4.1a Extension Provisioning

Migrations should attempt:
```sql
CREATE EXTENSION IF NOT EXISTS vector;
```
This requires elevated privileges. In locked-down RDS setups, the extension may need to be provisioned out-of-band.

Behavior:
- If extension creation fails and `AGENTCTL_POSTGRES_REQUIRE_VECTOR=true`, fail startup with an actionable error.
- Otherwise, log a warning and run without native vector search (fall back to Go cosine scan).

### 4.2 Update vector search in stores

| Store | Current | PostgreSQL |
|-------|---------|------------|
| memory | `vector_top_k()` (libSQL) or Go cosine | `ORDER BY embedding <=> $1` with HNSW |
| sessions | BLOB + Go cosine | Native pgvector query |
| tasks | BLOB + Go cosine | Native pgvector query |

### 4.3 Embedding storage format

- **SQLite**: Keep BLOB (little-endian float32) via existing `serializeFloat32()`
- **PostgreSQL**: Use `pgvector.NewVector([]float32{...})` — pgvector-go handles serialization
- **Dimensions:** Use configured dimensions (`AGENTCTL_VECTOR_DIMS` / `cfg.Database.Vector.Dimensions`)

### 4.4 HNSW indexes in PostgreSQL migrations

```sql
CREATE INDEX IF NOT EXISTS idx_memory_embedding_hnsw
ON named_memory USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);
```

### 4.5 Checkpoint

```bash
AGENTCTL_DB_DRIVER=postgres AGENTCTL_POSTGRES_DSN="..." \
  go test ./internal/storage/memory/... -run TestVectorSearch
```

---

## Phase 5: CAS S3/MinIO Backend (Production)

**Goal:** Content-addressable storage that works across pods without storing blobs inside Postgres.

### 5.1 Approach

- **Blobs:** S3 (or MinIO) keyed by digest: `s3://<bucket>/<prefix>/<digest>`
- **Metadata:** PostgreSQL `cas_objects` table (tags, pinning, created/updated, kind, size)

This keeps Postgres lean and lets CAS scale independently from the relational control-plane.

### 5.2 New CAS driver: `DriverS3`

Update `internal/storage/cas/config.go`:
- Add `DriverS3 DriverType = "s3"`
- Add `S3Config`:
  - `Bucket`, `Prefix`, `Region`
  - `Endpoint` + `ForcePathStyle` (for MinIO)
  - `SSE` config (optional KMS key)

Env vars (K8s-friendly):
- `AGENTCTL_CAS_DRIVER=s3`
- `AGENTCTL_CAS_S3_BUCKET=...`
- `AGENTCTL_CAS_S3_PREFIX=cas/` (optional)
- `AWS_REGION=...` (or `AGENTCTL_CAS_S3_REGION`)

Auth:
- **EKS:** IRSA (recommended)
- Dev: static creds in env (not recommended for prod)

Dependencies:
```bash
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/service/s3
go get github.com/aws/aws-sdk-go-v2/feature/s3/manager
```

### 5.3 New implementation: `internal/storage/cas/s3_store.go`

Implement `storage.CASStore`:
- `Put`: compute digest, upload to S3 (idempotent), upsert metadata row (merge tags)
- `Get`: stream from S3, return metadata from DB
- `Head/List/Pin/Unpin/AddTags/GC`: operate on metadata table; delete blobs from S3 during GC

Metadata schema (Postgres, per-store schema like the other stores):
```sql
CREATE TABLE IF NOT EXISTS cas_objects (
  digest TEXT PRIMARY KEY,
  size_bytes BIGINT NOT NULL,
  kind TEXT NOT NULL,
  tags JSONB,
  pinned BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cas_created ON cas_objects(created_at);
CREATE INDEX IF NOT EXISTS idx_cas_pinned ON cas_objects(pinned);
CREATE INDEX IF NOT EXISTS idx_cas_kind ON cas_objects(kind);
```

### 5.4 Wire Up

Update `internal/storage/cas/factory.go` to include `DriverS3`.

### 5.5 Tests

- Unit: digest/idempotency/tag merge behavior
- Integration: MinIO in docker-compose / testcontainers; verify Put/Get/List/GC

---

## Phase 6: K8s Deployment Infrastructure

### 6.1 Deployment Manifests

The repo already contains `deploy/kubernetes/` (kustomize). Prefer adding a Postgres/RDS overlay there, unless you intentionally want Helm.

Optional Helm chart path (if desired): `deploy/helm/foxctl/`

```
deploy/helm/foxctl/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   └── hpa.yaml
```

### 6.2 Health check endpoints

The web server already exposes `GET /api/health`. If adding Kubernetes-native probes:
- `GET /healthz` — liveness probe
- `GET /readyz` — readiness probe (pings PostgreSQL)

### 6.3 RDS recommendations (docs only)

- Instance: `db.r6g.large` minimum (for pgvector HNSW)
- PostgreSQL 17+ with pgvector 0.8.0
- Multi-AZ for production
- Ensure pgvector is available and provisioned (`CREATE EXTENSION vector`).

---

## Phase 7: Config + CLI Wiring

### 7.1 Update `internal/platform/config/config.go`

Extend the existing `DatabaseSettings` with a `postgres` block (and redaction), rather than introducing a parallel struct.

Note: the store-opening path is currently env-var driven (`internal/storage/dbdriver/ConfigLoader`). Platform-config wiring can be added once Postgres is stable.

### 7.2 Update `cmd/foxctl/cmd/web.go`

Add flags:
- `--db-driver string` (sqlite|postgres, default sqlite)
- `--db-dsn string` (PostgreSQL DSN)

### 7.3 Env vars

| Variable | Purpose | Default |
|----------|---------|---------|
| `AGENTCTL_DB_DRIVER` | Global driver | `sqlite` |
| `AGENTCTL_POSTGRES_DSN` | PostgreSQL DSN | (none) |
| `DATABASE_URL` | Standard fallback | (none) |
| `AGENTCTL_POSTGRES_MAX_CONNS` | Max connections | `25` |
| `AGENTCTL_POSTGRES_REQUIRE_VECTOR` | Fail fast if pgvector isn't available | `false` |

---

## Full Touchpoint List

### New Files

| File | Purpose |
|------|---------|
| `internal/storage/dbdriver/dialect.go` | Dialect interface |
| `internal/storage/dbdriver/dialect_sqlite.go` | SQLite dialect |
| `internal/storage/dbdriver/dialect_postgres.go` | PostgreSQL dialect |
| `internal/storage/dbdriver/dialect_test.go` | Dialect tests |
| `internal/storage/dbdriver/postgres.go` | PostgreSQL driver |
| `internal/storage/dbdriver/postgres_test.go` | Driver tests |
| `internal/storage/dbdriver/vector_postgres.go` | pgvector VectorHelper |
| `internal/storage/dbutil/migrate_postgres.go` | PG migration helpers |
| `internal/storage/cas/s3_store.go` | CAS S3/MinIO backend (S3 blobs + Postgres metadata) |
| `internal/storage/cas/s3_store_test.go` | CAS S3 tests (MinIO integration) |
| `deploy/helm/foxctl/*` | Helm chart |

### Modified Files

| File | Change |
|------|--------|
| `internal/storage/dbdriver/driver.go` | Add `GetDialect()` to DB interface |
| `internal/storage/dbdriver/config.go` | Add `DriverPostgres`, `PostgresConfig` |
| `internal/storage/dbdriver/config_loader.go` | Parse PostgreSQL env vars |
| `internal/storage/dbdriver/sqlite.go` | Implement `GetDialect()` |
| `internal/storage/dbdriver/libsql.go` | Implement `GetDialect()` |
| `internal/storage/dbdriver/turso.go` | Implement `GetDialect()` |
| `internal/storage/dbdriver/compat.go` | Update `WrapSQLDB()` for dialect |
| `internal/storage/dbutil/open.go` | Add PostgreSQL path |
| `internal/storage/memory/store.go` | PG migration DDL |
| `internal/storage/memory/search.go` | pgvector search queries |
| `internal/storage/sessions/store.go` | PG migration DDL + vector |
| `internal/storage/tasks/store.go` | PG migration DDL + vector |
| `internal/storage/agents/store.go` | PG migration DDL |
| `internal/storage/mailbox/store.go` | PG migration DDL |
| `internal/storage/coordination/store.go` | PG migration DDL |
| `internal/storage/contextvar/store.go` | PG migration DDL |
| `internal/storage/trajectory/store.go` | Replace `json_extract()` |
| `internal/storage/graph/store.go` | Replace `datetime('now')` |
| `internal/storage/sqlutil/migration.go` | Replace `INSERT OR IGNORE` |
| `internal/storage/cas/config.go` | Add `DriverS3` + S3 config/env vars |
| `internal/storage/cas/factory.go` | Add `DriverS3` case |
| `internal/context/companion/memory.go` | PG migration DDL |
| `internal/platform/config/config.go` | Extend `DatabaseSettings` with Postgres settings + redaction |
| `cmd/foxctl/cmd/web.go` | Add `--db-driver`/`--db-dsn` flags |
| `internal/interfaces/web/server.go` | Health check endpoints |
| `go.mod` / `go.sum` | Add pgx/v5, pgvector-go, aws-sdk-go-v2 (S3) |

---

## Implementation Order

| Step | Phase | Description |
|------|-------|-------------|
| 1 | P1 | Normalize Tier 1 queries to `$1..$N` placeholders (avoid runtime rebind) |
| 2 | P1 | Dialect interface + SQLite/Postgres implementations |
| 3 | P1 | Add `GetDialect()` to DB interface + all drivers |
| 4 | P1 | Dialect tests |
| 5 | P2 | PostgreSQL driver with pgx/stdlib (per-store schema `search_path` + advisory-lock migrations) |
| 6 | P2 | Config loader for PostgreSQL env vars (+ schema derivation) |
| 7 | P2 | Update `OpenStoreDB()` for PostgreSQL |
| 8 | P2 | PostgreSQL migration helpers |
| 9 | P2 | Integration test with Docker PostgreSQL |
| 10 | P3 | Tier 1 store migrations (memory, sessions, tasks, agents, companion, mailbox, coordination, contextvar) |
| 11 | P3 | Replace SQLite-specific functions (sessions pragma checks, trajectory, graph, sqlutil) |
| 12 | P3 | Integration tests: all Tier 1 stores on PostgreSQL |
| 13 | P4 | PostgresVectorHelper (pgvector ops) |
| 14 | P4 | Update memory/sessions/tasks vector search |
| 15 | P4 | HNSW index creation + vector search tests |
| 16 | P5 | CAS S3/MinIO implementation + config/factory wiring |
| 17 | P6 | K8s manifests (kustomize overlay; Helm optional) + health checks |
| 18 | P7 | Platform config + CLI flags |

---

## Testing Strategy

### Unit Tests
- Placeholder normalization correctness (`$N` placeholders across SQLite + Postgres)
- Dialect helper correctness (JSON extraction, existence checks)
- PostgresVectorHelper SQL generation
- Config loader parsing

### Integration Tests (Docker)
```bash
docker run -d --name foxctl-pg -p 5432:5432 \
  -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=foxctl \
  pgvector/pgvector:pg17-v0.8.0

AGENTCTL_DB_DRIVER=postgres \
AGENTCTL_POSTGRES_DSN="postgres://postgres:dev@localhost:5432/foxctl?sslmode=disable" \
  go test ./internal/storage/...
```

### CI Job
```yaml
services:
  postgres:
    image: pgvector/pgvector:pg17-v0.8.0
    env:
      POSTGRES_PASSWORD: test
      POSTGRES_DB: agentctl_test
    ports:
      - 5432:5432
```

### Smoke Test
```bash
AGENTCTL_DB_DRIVER=postgres AGENTCTL_POSTGRES_DSN="..." \
  foxctl web serve --chat teams
# Verify: tables created, vector search works, sessions persist across restart
```

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Placeholder normalization mistakes break queries | Automated conversion + comprehensive tests + focused review on dynamic SQL |
| PG migration differs from SQLite | `information_schema` checks, not error-swallowing |
| Vector search perf regression | HNSW indexing; benchmark against Go cosine |
| Connection pool exhaustion | Configurable max conns, health checks |
| Breaking local dev workflow | SQLite default; PostgreSQL opt-in |

## NOT in this PR

- S3 CAS backend (future: objects > 1MB at scale)
- Read replicas / connection routing
- Data migration tool (SQLite → PostgreSQL import)
- Tier 3 store PostgreSQL support
- Multi-database sharding
