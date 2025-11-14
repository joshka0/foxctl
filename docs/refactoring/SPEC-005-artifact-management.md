# SPEC-005: Artifact Management Extraction

## Status
**Draft** | Priority: Medium | Complexity: Medium

## Problem Statement

Artifact management logic (pinning, unpinning, digest extraction) is **duplicated** across cache, memory, and cmd packages. Each package directly depends on the CAS store, creating tight coupling and code duplication.

### Current State

**Duplicate pin/unpin logic in 3 locations**:
- `internal/cache/store.go:327-343` (pin digests method)
- `internal/cache/store.go:336-343` (unpin digests method)
- `internal/memory/store.go:214-230` (pin/unpin in Save method)
- `cmd/agentctl/cmd/artifacts.go` (artifact commands)

### Problems
1. **Code Duplication**: Same 15-line pattern repeated 3 times
2. **Scattered Responsibility**: No single source of truth for artifact lifecycle
3. **Tight Coupling**: Cache and Memory directly depend on CAS
4. **No Abstraction**: Cannot mock artifact operations in tests
5. **Inconsistent Behavior**: Each location may handle errors differently

### Affected Files
- `internal/cache/store.go` (depends on CAS)
- `internal/memory/store.go` (depends on CAS)
- `internal/artifacts/artifact.go` (only wrapping, no management)
- `cmd/agentctl/cmd/artifacts.go` (CLI commands)
- `cmd/agentctl/cmd/run.go:145-151` (artifact pinning in run command)

## Current State Analysis

### Example: Cache Store Pin Logic
```go
// internal/cache/store.go:327-343
func (s *Store) pinDigests(ctx context.Context, digests []string, casPath string) error {
    if len(digests) == 0 {
        return nil
    }

    casStore, err := cas.Open(ctx, casPath)
    if err != nil {
        return err
    }
    defer casStore.Close()

    for _, digest := range digests {
        if err := casStore.Pin(ctx, digest); err != nil {
            return fmt.Errorf("pin %s: %w", digest, err)
        }
    }
    return nil
}
```

### Example: Memory Store Has Identical Code
```go
// internal/memory/store.go:214-230
func (s *Store) SaveFromResult(ctx context.Context, ...) (NamedEntry, error) {
    // ... save logic ...

    // Extract digests from result
    digests := artifacts.ExtractDigests(result)

    // Pin artifacts - DUPLICATE LOGIC!
    if len(digests) > 0 {
        casStore, err := cas.Open(ctx, casPath)
        if err != nil {
            return NamedEntry{}, err
        }
        defer casStore.Close()

        for _, digest := range digests {
            if err := casStore.Pin(ctx, digest); err != nil {
                return NamedEntry{}, fmt.Errorf("pin %s: %w", digest, err)
            }
        }
    }

    return entry, nil
}
```

### Example: Run Command Also Duplicates
```go
// cmd/agentctl/cmd/run.go:145-151
if cacheMode != "only" {
    casStore, err := cas.Open(ctx, cfg.Paths.CAS)
    if err != nil {
        return err
    }
    defer casStore.Close()

    for _, digest := range entry.Digests {
        casStore.Pin(ctx, digest)  // Error ignored!
    }
}
```

## Proposed Solution

### Create Centralized Artifact Manager

```go
// internal/artifacts/manager.go
package artifacts

import (
    "context"
    "fmt"
    "github.com/jkatigb/agentctl/internal/storage"
)

// Manager handles artifact lifecycle operations
type Manager interface {
    // ExtractDigests extracts artifact digests from an envelope
    ExtractDigests(envelope []byte) ([]string, error)

    // Pin pins artifacts to prevent garbage collection
    Pin(ctx context.Context, digests ...string) error

    // Unpin unpins artifacts, allowing garbage collection
    Unpin(ctx context.Context, digests ...string) error

    // PinFromEnvelope extracts and pins digests from an envelope
    PinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error)

    // UnpinFromEnvelope extracts and unpins digests from an envelope
    UnpinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error)

    // Stage prepares artifacts for a job
    Stage(ctx context.Context, jobID string, digests []string) (string, error)

    // Release cleans up staged artifacts after job completion
    Release(ctx context.Context, jobID string) error
}

// CASManager implements Manager using a CAS store
type CASManager struct {
    store storage.CASStore
}

// NewManager creates a new artifact manager
func NewManager(store storage.CASStore) Manager {
    return &CASManager{store: store}
}

// ExtractDigests extracts artifact digests from an envelope
func (m *CASManager) ExtractDigests(envelope []byte) ([]string, error) {
    // Existing logic from artifacts.ExtractDigests
    var env struct {
        Artifacts []struct {
            Digest string `json:"digest"`
        } `json:"artifacts,omitempty"`
    }

    if err := json.Unmarshal(envelope, &env); err != nil {
        return nil, fmt.Errorf("unmarshal envelope: %w", err)
    }

    digests := make([]string, 0, len(env.Artifacts))
    for _, art := range env.Artifacts {
        if art.Digest != "" {
            digests = append(digests, art.Digest)
        }
    }

    return digests, nil
}

// Pin pins multiple artifacts
func (m *CASManager) Pin(ctx context.Context, digests ...string) error {
    if len(digests) == 0 {
        return nil
    }

    for _, digest := range digests {
        if err := m.store.Pin(ctx, digest); err != nil {
            return fmt.Errorf("pin %s: %w", digest, err)
        }
    }

    return nil
}

// Unpin unpins multiple artifacts
func (m *CASManager) Unpin(ctx context.Context, digests ...string) error {
    if len(digests) == 0 {
        return nil
    }

    for _, digest := range digests {
        if err := m.store.Unpin(ctx, digest); err != nil {
            return fmt.Errorf("unpin %s: %w", digest, err)
        }
    }

    return nil
}

// PinFromEnvelope extracts and pins digests in one operation
func (m *CASManager) PinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
    digests, err := m.ExtractDigests(envelope)
    if err != nil {
        return nil, err
    }

    if err := m.Pin(ctx, digests...); err != nil {
        return nil, err
    }

    return digests, nil
}

// UnpinFromEnvelope extracts and unpins digests in one operation
func (m *CASManager) UnpinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
    digests, err := m.ExtractDigests(envelope)
    if err != nil {
        return nil, err
    }

    if err := m.Unpin(ctx, digests...); err != nil {
        return nil, err
    }

    return digests, nil
}

// Stage creates a staging area for job artifacts
func (m *CASManager) Stage(ctx context.Context, jobID string, digests []string) (string, error) {
    // Future: Copy artifacts to job-specific staging area
    // For now, just pin them
    return "", m.Pin(ctx, digests...)
}

// Release cleans up staged artifacts
func (m *CASManager) Release(ctx context.Context, jobID string) error {
    // Future: Remove from staging area
    // For now, no-op (artifacts remain pinned)
    return nil
}
```

### Add Batch Operations

```go
// internal/artifacts/batch.go
package artifacts

// BatchOperation represents a group of artifact operations
type BatchOperation struct {
    manager Manager
    pins    []string
    unpins  []string
}

// NewBatch creates a new batch operation
func NewBatch(manager Manager) *BatchOperation {
    return &BatchOperation{
        manager: manager,
        pins:    make([]string, 0),
        unpins:  make([]string, 0),
    }
}

// Pin queues digests for pinning
func (b *BatchOperation) Pin(digests ...string) *BatchOperation {
    b.pins = append(b.pins, digests...)
    return b
}

// Unpin queues digests for unpinning
func (b *BatchOperation) Unpin(digests ...string) *BatchOperation {
    b.unpins = append(b.unpins, digests...)
    return b
}

// Execute performs all queued operations
func (b *BatchOperation) Execute(ctx context.Context) error {
    // Pin first
    if err := b.manager.Pin(ctx, b.pins...); err != nil {
        return fmt.Errorf("batch pin: %w", err)
    }

    // Then unpin
    if err := b.manager.Unpin(ctx, b.unpins...); err != nil {
        return fmt.Errorf("batch unpin: %w", err)
    }

    return nil
}
```

### Refactored Cache Store Usage

```go
// internal/cache/store.go (REFACTORED)
package cache

import (
    "github.com/jkatigb/agentctl/internal/artifacts"
    "github.com/jkatigb/agentctl/internal/storage"
)

type Store struct {
    db              *sql.DB
    root            string
    artifactManager artifacts.Manager  // Injected!
}

// Open creates a cache store with default artifact manager
func Open(ctx context.Context, cachePath string, casPath string) (storage.CacheStore, error) {
    casStore, err := cas.Open(ctx, casPath)
    if err != nil {
        return nil, err
    }

    artifactMgr := artifacts.NewManager(casStore)

    return OpenWithArtifactManager(ctx, cachePath, artifactMgr)
}

// OpenWithArtifactManager creates a cache store with custom artifact manager
func OpenWithArtifactManager(ctx context.Context, path string, mgr artifacts.Manager) (*Store, error) {
    db, err := sql.Open("sqlite", filepath.Join(path, "cache.db"))
    if err != nil {
        return nil, err
    }

    return &Store{
        db:              db,
        root:            path,
        artifactManager: mgr,
    }, nil
}

// Get with automatic artifact pinning
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    entry, found, err := s.getEntry(ctx, key)
    if err != nil || !found {
        return Entry{}, found, err
    }

    // Pin artifacts using manager - SIMPLIFIED!
    if err := s.artifactManager.Pin(ctx, entry.Digests...); err != nil {
        // Log warning but return cached result
        log.Warn("failed to pin artifacts", "error", err)
    }

    return entry, true, nil
}

// Put with automatic digest extraction
func (s *Store) Put(ctx context.Context, entry Entry) error {
    // Extract and pin artifacts - SIMPLIFIED!
    digests, err := s.artifactManager.PinFromEnvelope(ctx, entry.Result)
    if err != nil {
        return fmt.Errorf("pin artifacts: %w", err)
    }

    entry.Digests = digests
    return s.putEntry(ctx, entry)
}

// Delete with automatic unpinning
func (s *Store) Delete(ctx context.Context, key string) error {
    entry, found, err := s.getEntry(ctx, key)
    if err != nil || !found {
        return err
    }

    // Unpin before deleting - SIMPLIFIED!
    if err := s.artifactManager.Unpin(ctx, entry.Digests...); err != nil {
        log.Warn("failed to unpin artifacts", "error", err)
    }

    return s.deleteEntry(ctx, key)
}
```

### Refactored Memory Store Usage

```go
// internal/memory/store.go (REFACTORED)
package memory

import (
    "github.com/jkatigb/agentctl/internal/artifacts"
)

type Store struct {
    db              *sql.DB
    root            string
    artifactManager artifacts.Manager  // Injected!
}

// SaveFromResult with automatic artifact management
func (s *Store) SaveFromResult(ctx context.Context, name, typ, workspace, summary string,
    result []byte) (NamedEntry, error) {

    // Extract and pin artifacts - ONE LINE!
    digests, err := s.artifactManager.PinFromEnvelope(ctx, result)
    if err != nil {
        return NamedEntry{}, fmt.Errorf("manage artifacts: %w", err)
    }

    entry := NamedEntry{
        Name:      name,
        Type:      typ,
        Workspace: workspace,
        Summary:   summary,
        Data:      result,
        Digests:   digests,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    return entry, s.saveEntry(ctx, entry)
}
```

### Mock Manager for Testing

```go
// internal/artifacts/mock_manager.go
package artifacts

// MockManager is a test double for Manager
type MockManager struct {
    ExtractDigestsFunc func(envelope []byte) ([]string, error)
    PinFunc            func(ctx context.Context, digests ...string) error
    UnpinFunc          func(ctx context.Context, digests ...string) error

    PinnedDigests   []string
    UnpinnedDigests []string
}

func (m *MockManager) ExtractDigests(envelope []byte) ([]string, error) {
    if m.ExtractDigestsFunc != nil {
        return m.ExtractDigestsFunc(envelope)
    }
    return []string{"sha256:abc123"}, nil
}

func (m *MockManager) Pin(ctx context.Context, digests ...string) error {
    m.PinnedDigests = append(m.PinnedDigests, digests...)
    if m.PinFunc != nil {
        return m.PinFunc(ctx, digests...)
    }
    return nil
}

func (m *MockManager) Unpin(ctx context.Context, digests ...string) error {
    m.UnpinnedDigests = append(m.UnpinnedDigests, digests...)
    if m.UnpinFunc != nil {
        return m.UnpinFunc(ctx, digests...)
    }
    return nil
}

func (m *MockManager) PinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
    digests, err := m.ExtractDigests(envelope)
    if err != nil {
        return nil, err
    }
    return digests, m.Pin(ctx, digests...)
}

// ... other methods ...
```

## Implementation Plan

### Step 1: Enhance Artifacts Package (3 hours)
- [ ] Create `manager.go` with Manager interface
- [ ] Implement CASManager
- [ ] Create `batch.go` with batch operations
- [ ] Add comprehensive tests
- [ ] Add mock implementation

### Step 2: Update Cache Package (2 hours)
- [ ] Add artifactManager field to Store
- [ ] Update Open() to accept CAS path
- [ ] Update Get/Put/Delete to use manager
- [ ] Update tests with mock manager

### Step 3: Update Memory Package (2 hours)
- [ ] Add artifactManager field to Store
- [ ] Update SaveFromResult to use manager
- [ ] Update Delete to use manager
- [ ] Update tests with mock manager

### Step 4: Update Jobs Package (1.5 hours)
- [ ] Consider if jobs should use artifact manager
- [ ] Update if needed
- [ ] Update tests

### Step 5: Update Command Package (2 hours)
- [ ] Update run.go to use manager
- [ ] Update artifacts.go commands
- [ ] Simplify artifact handling code

### Step 6: Integration Tests (1.5 hours)
- [ ] Test cache with real CAS
- [ ] Test memory with real CAS
- [ ] Test artifact lifecycle end-to-end

### Step 7: Documentation (1 hour)
- [ ] Add godoc to all public APIs
- [ ] Add usage examples
- [ ] Document artifact lifecycle

**Total Estimated Time**: 13 hours

## Testing Strategy

### Unit Tests
```go
// internal/artifacts/manager_test.go
func TestManager_Pin(t *testing.T) {
    mockCAS := &mockCASStore{
        pinned: make(map[string]bool),
    }

    mgr := NewManager(mockCAS)

    err := mgr.Pin(context.Background(), "sha256:abc", "sha256:def")

    require.NoError(t, err)
    assert.True(t, mockCAS.pinned["sha256:abc"])
    assert.True(t, mockCAS.pinned["sha256:def"])
}

func TestManager_PinFromEnvelope(t *testing.T) {
    mockCAS := &mockCASStore{pinned: make(map[string]bool)}
    mgr := NewManager(mockCAS)

    envelope := []byte(`{
        "ok": true,
        "artifacts": [
            {"digest": "sha256:abc123"},
            {"digest": "sha256:def456"}
        ]
    }`)

    digests, err := mgr.PinFromEnvelope(context.Background(), envelope)

    require.NoError(t, err)
    assert.Equal(t, []string{"sha256:abc123", "sha256:def456"}, digests)
    assert.Len(t, mockCAS.pinned, 2)
}
```

### Cache Store Tests with Mock
```go
// internal/cache/store_test.go
func TestCache_Put_PinsArtifacts(t *testing.T) {
    mockMgr := &artifacts.MockManager{}

    store := setupCacheWithMockManager(t, mockMgr)
    defer store.Close()

    result := []byte(`{"ok": true, "artifacts": [{"digest": "sha256:test"}]}`)

    err := store.Put(context.Background(), Entry{
        Key:    "key1",
        Result: result,
    })

    require.NoError(t, err)
    assert.Contains(t, mockMgr.PinnedDigests, "sha256:test")
}

func TestCache_Delete_UnpinsArtifacts(t *testing.T) {
    mockMgr := &artifacts.MockManager{}
    store := setupCacheWithMockManager(t, mockMgr)

    // First put an entry
    store.Put(ctx, Entry{Key: "key1", Result: resultWithArtifact})

    // Then delete it
    err := store.Delete(ctx, "key1")

    require.NoError(t, err)
    assert.Contains(t, mockMgr.UnpinnedDigests, "sha256:test")
}
```

## Benefits

### Before: Duplicated in 3 Locations
```go
// 15 lines × 3 locations = 45 lines of duplication
casStore, err := cas.Open(ctx, casPath)
if err != nil {
    return err
}
defer casStore.Close()

for _, digest := range digests {
    if err := casStore.Pin(ctx, digest); err != nil {
        return fmt.Errorf("pin %s: %w", digest, err)
    }
}
```

### After: Centralized in One Place
```go
// 1 line at call site
digests, err := artifactManager.PinFromEnvelope(ctx, result)
```

### Improvements
- ✅ **45 lines of duplication → 1 line per usage**
- ✅ **3 implementations → 1 implementation**
- ✅ **No direct CAS dependency in cache/memory**
- ✅ **Testable with mocks**
- ✅ **Batch operations possible**
- ✅ **Consistent error handling**
- ✅ **Single source of truth**

## Migration Strategy

### Phase 1: Create Manager (No Breaking Changes)
- Add new manager interface and implementation
- Existing code continues to work

### Phase 2: Add to Cache/Memory (Backward Compatible)
- Add manager field (optional initially)
- Keep existing logic as fallback
- No breaking changes

### Phase 3: Migrate Incrementally
- Update cache package
- Update memory package
- Update cmd package
- Verify tests pass at each step

### Phase 4: Remove Duplicates
- Remove old inline implementations
- Enforce manager usage

## Success Criteria

- [ ] Manager interface defined with full API
- [ ] CASManager implementation complete
- [ ] MockManager for testing complete
- [ ] Cache package migrated
- [ ] Memory package migrated
- [ ] Command package migrated
- [ ] 45+ lines of duplication removed
- [ ] All tests pass
- [ ] 90%+ test coverage for manager

## Related Specs
- SPEC-001: Storage Interfaces (Manager uses CASStore interface)
- SPEC-002: Refactor Run Command (will use Manager)

## References
- Facade Pattern (Manager is facade over CAS operations)
- DRY Principle (Don't Repeat Yourself)
- Single Responsibility Principle (artifact management is one concern)
