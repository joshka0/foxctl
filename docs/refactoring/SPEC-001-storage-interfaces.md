# SPEC-001: Storage Interfaces

## Status
**Draft** | Priority: Critical | Complexity: High

## Problem Statement

The agentctl codebase currently has **zero interfaces**, relying entirely on concrete type dependencies. This creates several problems:

1. **Testing Difficulty**: Cannot mock storage implementations in tests
2. **Tight Coupling**: Packages directly depend on concrete implementations
3. **Limited Flexibility**: Cannot swap implementations or add middleware
4. **Violation of Dependency Inversion**: High-level modules depend on low-level details

### Affected Files
- `internal/cache/store.go` (374 LOC)
- `internal/jobs/store.go` (662 LOC)
- `internal/cas/store.go` (400 LOC)
- `internal/memory/store.go` (362 LOC)
- All files in `cmd/agentctl/cmd/` that use these stores

## Current State Analysis

### Example: Jobs Store Usage
```go
// cmd/agentctl/cmd/jobs.go:285-298
cfg, err := config.Load(cmd.Context())
store, err := jobs.Open(ctx, cfg.Paths.Jobs)
defer func() { _ = store.Close() }()
job, err := store.Get(ctx, id)
```

**Issues**:
- Direct dependency on `jobs.Open()` and concrete `jobs.Store` type
- Cannot inject test double
- Cannot add caching, logging, or metrics middleware
- Cannot use different storage backend

### Example: Cache Store
```go
// cmd/agentctl/cmd/run.go:93-100
cacheStore, err := cache.Open(ctx, cfg.Paths.Cache)
defer func() { _ = cacheStore.Close() }()
entry, hit, err := cacheStore.Get(ctx, cacheKey)
```

Same issues as Jobs Store above.

## Proposed Solution

### Non-Functional Requirements

All storage interfaces must share the same behavioral guarantees so that middleware
and commands can rely on consistent semantics:

1. **Error Contracts**  
   - Every `Get`/`Head` operation returns `ErrNotFound` (wrapped) when the target does not exist.  
   - Mutations wrap lower-level I/O errors but must never return partial results.  
   - Callers must rely on `errors.Is(err, ErrNotFound)` for miss detection instead of bespoke booleans.
2. **Concurrency**  
   - Implementations must be safe for concurrent use by multiple goroutines without additional locking.
     Long-running operations may take locks internally but cannot race or corrupt shared state.
3. **Context Handling**  
   - All methods accept a `context.Context` and must respect cancellation/timeouts promptly.  
   - Idempotent operations (`Get`, `Head`, `List`) must be safe to retry after cancellation.  
   - Mutations must best-effort roll back when the context is canceled mid-flight and document whether
     retries are safe (e.g., `Put` is idempotent, `Create` is not).

### Phase 1: Define Core Interfaces

Create `internal/storage/interfaces.go`:

```go
package storage

import (
    "context"
    "io"
    "time"
)

// Store is the base interface for all storage implementations
type Store interface {
    Close() error
}

// CacheStore manages execution result caching with TTL support
// All Get operations return ErrNotFound when the cache key is absent.
type CacheStore interface {
    Store
    Get(ctx context.Context, key string) (Entry, error)
    Put(ctx context.Context, entry Entry) error
    Recent(ctx context.Context, workspace string, limit int) ([]Entry, error)
    Delete(ctx context.Context, key string) error
}

// CacheEntry represents a cached execution result
type CacheEntry struct {
    Key           string
    Workspace     string
    Result        []byte
    Digests       []string
    CreatedAt     time.Time
    ExpiresAt     time.Time
    LastAccessed  time.Time
    AccessCount   int
}

// JobStore manages durable job execution and lifecycle
type JobStore interface {
    Store
    Create(ctx context.Context, name string, input []byte) (Job, error)
    Get(ctx context.Context, id string) (Job, error)
    List(ctx context.Context, limit int) ([]Job, error)
    UpdateState(ctx context.Context, id string, state JobState) error
    FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, policy DedupPolicy) (Job, bool, error)
    StreamProgress(ctx context.Context, id string) (io.ReadCloser, error)
}

// Job represents an execution job
type Job struct {
    ID        string
    Name      string
    State     JobState
    CreatedAt time.Time
    UpdatedAt time.Time
    Input     []byte
    Result    []byte
}

type JobState string

const (
    JobStateQueued   JobState = "queued"
    JobStateRunning  JobState = "running"
    JobStateOK       JobState = "ok"
    JobStateError    JobState = "error"
    JobStateCanceled JobState = "canceled"
)

type DedupPolicy string

const (
    DedupNone   DedupPolicy = "none"
    DedupWait   DedupPolicy = "wait"
    DedupReject DedupPolicy = "reject"
)

// ContentAddressableStore manages content-addressed artifacts
// Get/Head must return ErrNotFound when the digest is missing.
type CASStore interface {
    Store
    Put(ctx context.Context, r io.Reader, kind string, tags []string) (Object, error)
    Get(ctx context.Context, digest string) (io.ReadCloser, Metadata, error)
    Head(ctx context.Context, digest string) (Metadata, error)
    Pin(ctx context.Context, digest string) error
    Unpin(ctx context.Context, digest string) error
    AddTags(ctx context.Context, digest string, tags []string) error
}

// Object represents a stored CAS object
type Object struct {
    Digest   string
    Size     int64
    Metadata Metadata
}

// Metadata holds object metadata
type Metadata struct {
    Kind      string
    Tags      []string
    Pinned    bool
    CreatedAt time.Time
}

// MemoryStore manages named, workspace-scoped persistent data.
// Get returns ErrNotFound when the tuple (name, workspace) is missing.
type MemoryStore interface {
    Store
    Get(ctx context.Context, name, workspace string) (NamedEntry, error)
    Save(ctx context.Context, name, typ, workspace, summary string, data []byte) (NamedEntry, error)
    List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error)
    Delete(ctx context.Context, name, workspace string) error
}

// NamedEntry represents a named memory entry
type NamedEntry struct {
    Name      string
    Type      string
    Workspace string
    Summary   string
    Data      []byte
    Digests   []string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Phase 2: Adapter Pattern for Existing Stores

Keep existing concrete implementations but make them implement the interfaces:

```go
// internal/cache/store.go
package cache

import "github.com/jkatigb/agentctl/internal/storage"

// Ensure Store implements storage.CacheStore
var _ storage.CacheStore = (*Store)(nil)

type Store struct {
    db   *sql.DB
    root string
}

// Get implements storage.CacheStore
func (s *Store) Get(ctx context.Context, key string) (storage.CacheEntry, bool, error) {
    // Existing implementation, but return storage.CacheEntry
}

// Legacy type alias for backward compatibility during migration
type Entry = storage.CacheEntry
```

### Phase 3: Factory Functions

Create factory functions that return interfaces:

```go
// internal/cache/factory.go
package cache

import (
    "context"
    "github.com/jkatigb/agentctl/internal/storage"
)

// Open opens a cache store at the given path
func Open(ctx context.Context, path string) (storage.CacheStore, error) {
    // Existing implementation
    return &Store{...}, nil
}
```

### Phase 4: Update Consumers to Use Interfaces

```go
// cmd/agentctl/cmd/jobs.go (BEFORE)
func listJobs(cmd *cobra.Command, args []string) error {
    cfg, _ := config.Load(cmd.Context())
    store, err := jobs.Open(ctx, cfg.Paths.Jobs) // concrete type
    defer store.Close()
}

// cmd/agentctl/cmd/jobs.go (AFTER)
func listJobs(cmd *cobra.Command, args []string) error {
    cfg, _ := config.Load(cmd.Context())
    var store storage.JobStore
    store, err := jobs.Open(ctx, cfg.Paths.Jobs) // returns interface
    defer store.Close()
}
```

## Implementation Plan

### Step 1: Create Storage Package (1-2 hours)
- [ ] Create `internal/storage/interfaces.go`
- [ ] Define all core interfaces
- [ ] Define shared types (Entry, Job, Object, etc.)
- [ ] Add comprehensive godoc comments

### Step 2: Implement Interfaces in Cache Package (2-3 hours)
- [ ] Add `var _ storage.CacheStore = (*Store)(nil)` verification
- [ ] Update method signatures to return `storage.CacheEntry`
- [ ] Create type alias: `type Entry = storage.CacheEntry`
- [ ] Update `Open()` to return `storage.CacheStore`
- [ ] Update tests

### Step 3: Implement Interfaces in Jobs Package (3-4 hours)
- [ ] Add interface verification
- [ ] Update method signatures
- [ ] Create type aliases for backward compatibility
- [ ] Update `Open()` to return `storage.JobStore`
- [ ] Update tests

### Step 4: Implement Interfaces in CAS Package (2-3 hours)
- [ ] Add interface verification
- [ ] Update method signatures
- [ ] Create type aliases
- [ ] Update `Open()` to return `storage.CASStore`
- [ ] Update tests

### Step 5: Implement Interfaces in Memory Package (2-3 hours)
- [ ] Add interface verification
- [ ] Update method signatures
- [ ] Create type aliases
- [ ] Update `Open()` to return `storage.MemoryStore`
- [ ] Update tests

### Step 6: Update All Consumers (4-6 hours)
- [ ] Update all files in `cmd/agentctl/cmd/` to use interfaces
- [ ] Update any internal packages that use stores
- [ ] Remove direct concrete type dependencies
- [ ] Update tests to use interfaces

### Step 7: Remove Type Aliases (1-2 hours)
- [ ] Once all consumers migrated, remove type aliases
- [ ] Consolidate all types in storage package
- [ ] Update all references

## Testing Strategy

### Unit Tests
```go
// internal/storage/mock_test.go
package storage_test

type mockCacheStore struct {
    getCalls []string
    putCalls []CacheEntry
}

func (m *mockCacheStore) Get(ctx context.Context, key string) (CacheEntry, bool, error) {
    m.getCalls = append(m.getCalls, key)
    return CacheEntry{}, false, nil
}

func TestWithMockStore(t *testing.T) {
    mock := &mockCacheStore{}
    // Can now inject mock into functions that accept storage.CacheStore
}
```

### Integration Tests
- Ensure existing tests still pass
- Add tests that verify interface compliance
- Add tests that use mock implementations

## Migration Strategy

### Backward Compatibility
1. **Phase 1-5**: Type aliases ensure existing code continues to work
2. **Phase 6**: Gradual migration of consumers to use interfaces
3. **Phase 7**: Remove aliases only after all migrations complete

### Rollback Plan
- Each step is independently reversible
- Type aliases allow incremental migration
- Can stop at any phase and remain functional

## Benefits

### Immediate
- ✅ Better testability with mock implementations
- ✅ Clear contracts between layers
- ✅ Foundation for dependency injection

### Long-term
- ✅ Can add middleware (logging, metrics, caching)
- ✅ Can swap storage backends (e.g., PostgreSQL, Redis)
- ✅ Easier to add new storage types
- ✅ Better adherence to SOLID principles

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Breaking existing code | High | Use type aliases during migration |
| Interface too broad | Medium | Start minimal, extend as needed |
| Performance overhead | Low | Interfaces in Go have minimal overhead |
| Increased complexity | Medium | Clear documentation, gradual rollout |

## Success Criteria

- [ ] All storage implementations implement their respective interfaces
- [ ] Error contracts documented and enforced (callers use `errors.Is(err, ErrNotFound)` for misses)
- [ ] Concurrency guarantees documented for each store (thread-safe vs caller-synchronized)
- [ ] All consumers use interfaces instead of concrete types
- [ ] Automated or checklist-based verification ensures all alias usages are removed before Phase 7
- [ ] All existing tests pass
- [ ] At least 3 tests added using mock implementations
- [ ] No direct imports of concrete store types in cmd/ package
- [ ] Godoc coverage for all interfaces and types

## Related Specs
- SPEC-004: SkillExecutor Interface (depends on this)
- SPEC-005: Artifact Management Interface (depends on this)

## References
- Go Proverbs: "Accept interfaces, return structs" (exception: factory pattern)
- Effective Go: Interfaces
- SOLID Principles: Dependency Inversion Principle
