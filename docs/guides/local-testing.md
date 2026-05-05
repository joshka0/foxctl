# Local Testing with Turso

This guide explains how to test foxctl with local vector-capable storage without
requiring Turso Cloud access.

## Overview

Foxctl's canonical SQLite-family vector store is **Turso** through
`turso.tech/database/tursogo`. It supports local database files, vector
functions, and optional remote sync from the same `database.driver=turso`
contract.

Use local Turso when tests need native vector behavior. Use SQLite when tests
only need basic relational storage.

| Feature | SQLite | Turso local | Turso sync |
| --- | --- | --- | --- |
| Local file | Yes | Yes | Yes |
| Vector functions | No | Yes | Yes |
| Remote sync | No | No | Yes |
| Credentials required | No | No | Yes, when `DB_URL` is set |
| Best for | Basic CRUD tests | Vector and memory tests | Cross-device/manual sync tests |

## Environment

```bash
export FOXCTL_MEMORY_DB_DRIVER=turso
export FOXCTL_MEMORY_DB_PATH=/tmp/test-memory.turso
export FOXCTL_MEMORY_VECTOR_SEARCH=true
export FOXCTL_MEMORY_VECTOR_DIMS=384

go test ./internal/storage/memory ./internal/storage/dbdriver
```

For remote sync, add:

```bash
export FOXCTL_MEMORY_DB_URL=libsql://your-db.turso.io
export FOXCTL_MEMORY_DB_TOKEN=...
```

`libsql://` is still the Turso remote URL scheme. It is not the old local
`libsql` driver contract.

## Programmatic Configuration

```go
cfg := dbdriver.DefaultTursoLocalConfig("/tmp/test.turso", true)
db, err := dbdriver.OpenDB(ctx, cfg, nil)
if err != nil {
    t.Fatal(err)
}
defer db.Close()
```

For memory tests:

```go
store, err := memory.OpenTurso(ctx, dbdriver.TursoConfig{
    Path:             filepath.Join(t.TempDir(), "memory.turso"),
    VectorDimensions: 384,
})
if err != nil {
    t.Fatal(err)
}
defer store.Close()
```

## Testing Strategy

- Use SQLite for basic deterministic store tests.
- Use local Turso for vector storage, memory search, and native vector SQL
  behavior.
- Use Turso sync only for explicit manual or integration tests that provide a
  remote URL and token.
- Keep test databases under `t.TempDir()` or `/tmp`; do not write to
  `~/.foxctl` from unit tests.

## Useful Commands

```bash
go test ./internal/storage/dbdriver
go test ./internal/storage/memory
go test ./internal/v2/adapters/libsql/turns
go test ./cmd/foxctl/cmd -run 'Test.*Memory|Test.*Orchestration'
```

The `internal/v2/adapters/libsql` import path is an internal historical package
name. Its live default storage path is now Turso.
