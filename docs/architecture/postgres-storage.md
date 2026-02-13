# PostgreSQL + CAS Storage Architecture (Current)

This document reflects the implemented storage topology as of commit `93bcbb3b` (HEAD in this workspace).

## Driver model

The storage layer supports per-store database drivers through `internal/storage/dbdriver`.

- Driver selection per store:
  - `AGENTCTL_<STORE>_DB_DRIVER`
  - fallback `AGENTCTL_DB_DRIVER`
  - default: `sqlite`
- Supported drivers in current implementation:
  - `sqlite` (default)
  - `libsql`
  - `turso`
  - `postgres`

`dbdriver.ConfigLoader` loads store-specific config in `internal/storage/dbdriver/config_loader.go` and `OpenStoreDB` applies it in `internal/storage/dbutil/open.go`.

## PostgreSQL architecture

### Control-plane design

- `agentctl` runs most logical stores as separate PostgreSQL schemas.
- Default schema name is lower-cased store name (for example: `sessions`, `tasks`, `memory`).
- DB is opened via `AGENTCTL_<STORE>_POSTGRES_DSN`, then fallback to:
  - `AGENTCTL_POSTGRES_DSN`
  - `DATABASE_URL`
- Schema and pooling controls are global:
  - `AGENTCTL_POSTGRES_MAX_CONNS`
  - `AGENTCTL_POSTGRES_MAX_IDLE_CONNS`

### What changes in connection behavior

`internal/storage/dbdriver/postgres.go`:

- Appends `search_path` to the PostgreSQL DSN.
- Creates schema if missing.
- Adds compatibility helper functions for existing SQL patterns.
- Runs migrations under advisory lock per schema to avoid concurrent migrations from multiple pods.
- Detects `pgvector` availability:
  - best-effort enable by default (falls back when not required)
  - hard-fail only when `AGENTCTL_POSTGRES_REQUIRE_VECTOR=true`

### Storage migration notes

`dbutil.OpenStoreDB` uses:

- `sqliteutil.OpenDBShared` for sqlite stores
- `dbdriver.OpenDBCompatWithCloser` for all non-sqlite drivers

This keeps migration and lifecycle semantics consistent while allowing pooled/pooled-compatible PostgreSQL behavior.

## CAS architecture

`internal/storage/cas/config.go` defines CAS backends via `AGENTCTL_CAS_DRIVER`:

- `file` (filesystem)
- `sqlite`
- `turso` (legacy cloud option)
- `s3` (enterprise object store)

Current PostgreSQL-oriented deployment overlays use:

- `AGENTCTL_CAS_DRIVER=s3`
- `AGENTCTL_CAS_S3_BUCKET`
- `AGENTCTL_CAS_S3_REGION`
- `AGENTCTL_CAS_S3_ENDPOINT` (optional, for MinIO)
- `AGENTCTL_CAS_S3_PREFIX`
- `AGENTCTL_CAS_S3_FORCE_PATH_STYLE`
- `AGENTCTL_CAS_S3_DISABLE_SSL`

Legacy base manifests may still show older env keys (`AGENTCTL_CAS_BACKEND`, `AGENTCTL_CAS_BUCKET`); these are documented as historical drift in `docs/kubernetes.md`.

## Teams conversation references

Teams adapter webhooks can persist conversation references outside the store-driver lattice:

- `internal/storage/convref` opens PostgreSQL for conversation refs when global PostgreSQL is active.
- Otherwise falls back to SQLite.

## Deployment implications

- In-memory lock fallback is used unless PostgreSQL lock support is explicitly active.
- Horizontal pod scaling is safe for lock-sensitive and shared state pathways once PostgreSQL-backed stores are enabled (for affected stores that use shared state).

## Cross-reference

- Kubernetes runtime summary for DSN/env wiring: `docs/architecture/kubernetes-runtime.md`
- Historical implementation plan: `docs/plans/k8s-sql-storage.md`
