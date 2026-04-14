# Local-First LibSQL Sync Architecture

## Overview

Enable foxctl to use libsql as the primary database with optional remote sync, while maintaining SQLite as a fallback for non-CGO builds.

## Current State

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   SQLite    │     │   LibSQL    │     │   Turso     │
│  (fallback) │     │ (local-only)│     │  (cloud)    │
└─────────────┘     └─────────────┘     └─────────────┘
      │                   │                   │
      └───────────────────┴───────────────────┘
                          │
                    dbdriver.DB
```

- **SQLite** (`sqlite.go`): Pure Go, no vector search, always available
- **LibSQL** (`libsql.go`): CGO build, local file with vector search, `NewEmbeddedReplicaConnector(path, "")`
- **Turso** (`turso.go`): CGO build, embedded replica syncs with cloud, `NewEmbeddedReplicaConnector(path, url, WithAuthToken(...))`

## Proposed Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     dbdriver.Open()                          │
└──────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
       ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
       │   LibSQL    │ │   LibSQL    │ │   SQLite    │
       │ + Remote    │ │ Local-only  │ │  Fallback   │
       │   Sync      │ │             │ │             │
       └─────────────┘ └─────────────┘ └─────────────┘
              │               │               │
              ▼               ▼               ▼
       ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
       │ sqld server │ │  .db file   │ │  .db file   │
       │  (remote)   │ │  (local)    │ │  (local)    │
       └─────────────┘ └─────────────┘ └─────────────┘
```

### Priority Order

1. **LibSQL + Remote Sync** (if `SyncURL` configured and CGO available)
2. **LibSQL Local-only** (if CGO available)
3. **SQLite Fallback** (always available)

## Implementation Plan

### Phase 1: Extend LibSQLConfig

Add remote sync fields to `LibSQLConfig`:

```go
// LibSQLConfig holds local libSQL-specific configuration
type LibSQLConfig struct {
    // Path to the libSQL database file
    Path string `json:"path" yaml:"path"`

    // EnableVectorSearch enables vector search capabilities
    EnableVectorSearch bool `json:"enable_vector_search" yaml:"enable_vector_search"`

    // VectorDimensions specifies the dimension of vector embeddings
    VectorDimensions int `json:"vector_dimensions" yaml:"vector_dimensions"`

    // --- New fields for remote sync ---

    // SyncURL is the remote sqld URL for sync (optional)
    // When set, enables embedded replica mode with sync
    // Example: "http://localhost:8080" or "libsql://your-db.turso.io"
    SyncURL string `json:"sync_url,omitempty" yaml:"sync_url,omitempty"`

    // AuthToken for remote sync authentication (optional)
    AuthToken string `json:"auth_token,omitempty" yaml:"auth_token,omitempty"`

    // SyncInterval for periodic sync (default: 0 = sync on write)
    // When > 0, syncs every N seconds in background
    SyncInterval int `json:"sync_interval,omitempty" yaml:"sync_interval,omitempty"`
}
```

### Phase 2: Modify openLibSQL()

Update `libsql.go` to handle remote sync:

```go
func openLibSQL(ctx context.Context, cfg LibSQLConfig, migrate MigrationFunc) (DB, error) {
    // ... existing directory creation ...

    var connector *libsql.Connector
    var err error

    if cfg.SyncURL != "" {
        // Remote sync mode - create embedded replica
        opts := []libsql.Option{}
        if cfg.AuthToken != "" {
            opts = append(opts, libsql.WithAuthToken(cfg.AuthToken))
        }
        connector, err = libsql.NewEmbeddedReplicaConnector(cfg.Path, cfg.SyncURL, opts...)
    } else {
        // Local-only mode
        connector, err = libsql.NewEmbeddedReplicaConnector(cfg.Path, "")
    }

    // ... rest of implementation ...
}
```

### Phase 3: Add Sync Methods to libsqlDB

```go
// libsqlDB wraps libsql connection for local file-based database
type libsqlDB struct {
    db                 *sql.DB
    connector          *libsql.Connector
    enableVectorSearch bool
    vectorDimensions   int
    driverType         DriverType
    syncURL            string // tracks if sync is enabled
}

// Sync triggers a manual sync with the remote server.
// No-op if SyncURL is not configured.
func (l *libsqlDB) Sync() error {
    if l.syncURL == "" {
        return nil // local-only mode
    }
    _, err := l.connector.Sync()
    return err
}

// IsSyncEnabled returns true if remote sync is configured.
func (l *libsqlDB) IsSyncEnabled() bool {
    return l.syncURL != ""
}
```

### Phase 4: Adaptive Store Factory

Create a unified factory that selects the best available backend:

```go
// internal/storage/memory/factory.go

// OpenAdaptive opens a memory store using the best available backend.
// Priority: LibSQL+Sync > LibSQL > SQLite
func OpenAdaptive(ctx context.Context, cfg AdaptiveConfig) (MemoryStore, error) {
    // Try LibSQL with sync if configured
    if cfg.LibSQL.SyncURL != "" && IsCGOEnabled() {
        store, err := OpenLibSQL(ctx, cfg.LibSQL)
        if err == nil {
            return store, nil
        }
        log.Printf("[WARN] LibSQL+sync failed, trying local-only: %v", err)
    }

    // Try local-only LibSQL
    if IsCGOEnabled() {
        store, err := OpenLibSQL(ctx, cfg.LibSQL)
        if err == nil {
            return store, nil
        }
        log.Printf("[WARN] LibSQL failed, falling back to SQLite: %v", err)
    }

    // Fallback to SQLite
    return Open(ctx, cfg.SQLite.Path, cfg.CASPath)
}
```

### Phase 5: Configuration

Environment variables:

```bash
# Enable remote sync
AGENTCTL_LIBSQL_SYNC_URL=http://localhost:8080
AGENTCTL_LIBSQL_AUTH_TOKEN=your-token

# Or use config file
# ~/.foxctl/config.yaml
storage:
  driver: libsql
  libsql:
    path: ~/.foxctl/storage/memory.db
    enable_vector_search: true
    sync_url: http://localhost:8080
    auth_token: ${AGENTCTL_LIBSQL_AUTH_TOKEN}
```

## Sync Behavior

### Write Path

```
Write() → Local SQLite → Sync() → Remote sqld
                           ↑
                      (async if SyncInterval > 0)
```

### Read Path

```
Read() → Local SQLite (always fast, local-first)
```

### Conflict Resolution

Turso's embedded replicas use WAL-based sync, not CRDTs. Behavior:
- **Reads**: Always from local replica (fast)
- **Writes**: Applied locally first, then synced
- **Conflicts**: Last-write-wins at row level (by primary key)

For foxctl's use case (single-user, single-device primary):
- Memories: `UNIQUE(name, workspace)` - conflict unlikely
- Sessions: UUID primary key - no conflicts

## Migration Path

1. Existing SQLite users continue working unchanged
2. Users with `sync_url` configured get automatic sync
3. `libsql/migrate` skill migrates existing data to synced libsql

## Files to Modify

| File | Changes |
|------|---------|
| `internal/storage/dbdriver/config.go` | Add `SyncURL`, `AuthToken`, `SyncInterval` to `LibSQLConfig` |
| `internal/storage/dbdriver/libsql.go` | Handle `SyncURL` in `openLibSQL()`, add `Sync()` method |
| `internal/storage/dbdriver/db.go` | Add `Syncer` interface |
| `internal/storage/memory/libsql_store.go` | New file: LibSQL-backed memory store |
| `internal/storage/memory/factory.go` | New file: Adaptive store factory |
| `internal/platform/config/config.go` | Add libsql sync config fields |

## Testing

```bash
# Start local sqld
sqld --db-path ~/.foxctl/sync/primary.db

# Configure foxctl
export AGENTCTL_LIBSQL_SYNC_URL=http://localhost:8080

# Test sync
foxctl memory put --name "test" --summary "testing sync"
foxctl run libsql/migrate --input '{"libsql_url": "http://localhost:8080", "dry_run": true}'
```

## Future Enhancements

1. **Multi-device sync**: Share memories across machines via Turso cloud
2. **Selective sync**: Sync only certain workspaces
3. **Offline queue**: Queue writes when offline, sync when connected
4. **Conflict UI**: Surface conflicts for manual resolution (if needed)
