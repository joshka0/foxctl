# Store Migration Plan: sqliteutil.OpenDB → dbdriver

## Overview

22 stores currently use `sqliteutil.OpenDB()` directly, bypassing the `dbdriver` abstraction. This prevents them from benefiting from:
- Local-first libsql sync
- Turso cloud backend support
- Unified configuration via environment variables

## Store Inventory

### Tier 1: Sync-Critical (High Priority)
These stores benefit most from cross-device sync:

| Store | Path | Notes |
|-------|------|-------|
| `memory` | `internal/storage/memory/store.go` | ✅ Has `factory.go` - done |
| `tasks` | `internal/storage/tasks/store.go` | Task continuity across devices |
| `sessions` | `internal/storage/sessions/store.go` | Session history sync |

### Tier 2: Sync-Useful (Medium Priority)
These stores could benefit from sync but aren't critical:

| Store | Path | Notes |
|-------|------|-------|
| `jobs/persist` | `internal/storage/jobs/persist/store.go` | Job queue state |
| `mailbox` | `internal/storage/mailbox/store.go` | Agent messages |
| `knowledge` | `internal/storage/knowledge/store.go` | Extracted knowledge |
| `teams` | `internal/storage/teams/store.go` | Team definitions |
| `agents` | `internal/storage/agents/store.go` | Agent registry |

### Tier 3: Local-Only (Low Priority)
These stores are inherently local and don't need sync:

| Store | Path | Notes |
|-------|------|-------|
| `cache` | `internal/storage/cache/store.go` | Ephemeral speedup |
| `blackboard` | `internal/storage/blackboard/store.go` | Local coordination |
| `board_store` | `internal/storage/blackboard/board_store.go` | Board state |
| `dedupe_sqlite` | `internal/agent/daemon/dedupe_sqlite.go` | Deduplication |
| `pattern_store` | `internal/agent/optimization/pattern_store.go` | Optimization patterns |
| `trajectory` | `internal/storage/trajectory/store.go` | Execution traces |
| `testwatch` | `internal/storage/testwatch/store.go` | Test watching |
| `contextbuffer` | `internal/storage/contextbuffer/store.go` | Context buffering |
| `quotas` | `internal/storage/quotas/store.go` | Rate limiting |
| `graph` | `internal/storage/graph/store.go` | Knowledge graph |
| `embedding` | `internal/intelligence/indexing/embedding/store.go` | Embedding cache |
| `postreview` | `internal/intelligence/indexing/postreview/store.go` | Post-review events |

## Migration Pattern

### Before (Direct SQLite)
```go
func Open(ctx context.Context, root string) (*Store, error) {
    dbPath := filepath.Join(root, "store.db")
    db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
    if err != nil {
        return nil, err
    }
    return &Store{db: db}, nil
}
```

### After (With dbdriver)
```go
func Open(ctx context.Context, cfg config.Config) (*Store, error) {
    loader := dbdriver.NewConfigLoader(cfg.Storage.Root)
    dbCfg := loader.LoadConfig("STORE", "store.db") // or custom loader method

    driver, err := dbdriver.Open(ctx, dbCfg)
    if err != nil {
        return nil, err
    }

    if err := migrate(ctx, driver.DB()); err != nil {
        return nil, err
    }

    return &Store{driver: driver}, nil
}
```

### Factory Pattern (for interface compliance)
```go
// OpenWithConfig opens store based on platform configuration
func OpenWithConfig(ctx context.Context, cfg config.Config) (storage.StoreInterface, error) {
    driver := dbdriver.DriverType(cfg.Database.Driver)

    switch driver {
    case dbdriver.DriverTurso:
        return openTursoStore(ctx, cfg)
    case dbdriver.DriverLibSQL:
        return openLibSQLStore(ctx, cfg)
    default:
        return Open(ctx, cfg.Storage.Root)
    }
}
```

## Implementation Plan

### Phase 1: Tasks Store (Highest Impact)
1. Create `internal/storage/tasks/factory.go`
2. Add `OpenWithConfig()` function
3. Update skill adapters to use factory
4. Test sync with sqld

### Phase 2: Sessions Store
1. Create `internal/storage/sessions/factory.go`
2. Add `OpenWithConfig()` function
3. Update session skills to use factory
4. Test session sync

### Phase 3: Secondary Stores
- jobs, mailbox, knowledge, teams, agents
- Lower priority, implement as needed

### Phase 4: Local-Only Stores
- Keep using sqliteutil.OpenDB
- Could add dbdriver support for consistency but sync disabled

## Environment Variables

Per-store configuration follows the pattern:
```bash
# Driver selection
FOXCTL_<STORE>_DB_DRIVER=libsql|sqlite|turso

# For libsql with sync
FOXCTL_<STORE>_SYNC_URL=http://localhost:8080
FOXCTL_<STORE>_SYNC_TOKEN=...

# For Turso
FOXCTL_<STORE>_DB_URL=libsql://...
FOXCTL_<STORE>_DB_TOKEN=...
```

Global fallbacks:
```bash
FOXCTL_LIBSQL_SYNC_URL=http://localhost:8080
FOXCTL_TURSO_URL=libsql://...
FOXCTL_TURSO_TOKEN=...
```

## Testing Strategy

1. **Unit Tests**: Mock dbdriver, verify correct driver selection
2. **Integration Tests**: Start sqld, test actual sync
3. **Migration Tests**: Verify existing SQLite data is preserved

## Rollback Plan

If issues arise:
1. Set `FOXCTL_<STORE>_DB_DRIVER=sqlite` to fall back
2. Platform config: `database.driver: sqlite`
3. Data in `.libsql` files is SQLite-compatible

## Notes

- Memory store already migrated via `factory.go`
- Platform config now defaults to `libsql` driver
- Vector search enabled by default for memory databases
- Stores without sync URL work in local-only mode
