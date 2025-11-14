# SPEC-006: Fix Error Handling

## Status
**Draft** | Priority: High | Complexity: Medium

## Problem Statement

There are **306 instances** of ignored errors across the codebase (using `_ =` or `_,` to discard errors). Many of these are critical errors that should be handled, logged, or at minimum documented why they're safe to ignore.

### Critical Examples

1. **JSON Unmarshaling Errors** - Silent data corruption
2. **Time Parsing Errors** - Invalid timestamps become zero values
3. **File I/O Errors** - Failed writes go unnoticed
4. **Resource Cleanup Errors** - Failed Close() calls hidden

### Affected Files (Top Offenders)
- `internal/cache/store.go` - 12 ignored errors
- `internal/memory/store.go` - 10 ignored errors
- `internal/jobs/store.go` - 8 ignored errors
- `cmd/agentctl/cmd/*.go` - 30+ ignored errors across all command files

## Current State Analysis

### Category 1: Data Integrity Errors (CRITICAL)

#### Example 1: JSON Unmarshal Errors
```go
// internal/cache/store.go:164-167
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    // ... query database ...
    var digests string
    row.Scan(&entry.Key, &entry.Workspace, &entry.Result, &digests, ...)

    // CRITICAL: Corrupt JSON data will be silently ignored!
    _ = json.Unmarshal([]byte(digests), &entry.Digests)

    return entry, true, nil
}
```

**Impact**: Corrupt data in database → empty Digests slice → artifacts not pinned → data loss

**Fix**:
```go
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    // ... query database ...
    if err := json.Unmarshal([]byte(digests), &entry.Digests); err != nil {
        return Entry{}, false, fmt.Errorf("unmarshal digests: %w", err)
    }
    return entry, true, nil
}
```

#### Example 2: Time Parsing Errors
```go
// internal/cache/store.go:165-167
entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
entry.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
entry.LastAccessed, _ = time.Parse(time.RFC3339Nano, last)
```

**Impact**: Invalid timestamps → zero time values → cache expiration broken

**Fix** (with SPEC-003):
```go
var err error
entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
if err != nil {
    return Entry{}, false, fmt.Errorf("scan created_at: %w", err)
}
```

### Category 2: Resource Cleanup Errors (MEDIUM PRIORITY)

#### Example 3: Deferred Close Errors
```go
// cmd/agentctl/cmd/jobs.go:285-298
func listJobs(cmd *cobra.Command, args []string) error {
    store, err := jobs.Open(ctx, cfg.Paths.Jobs)
    if err != nil {
        return err
    }
    defer func() { _ = store.Close() }()  // Error ignored
}
```

**Impact**: Resource leaks, file descriptor exhaustion (rare but possible)

**Fix**:
```go
func listJobs(cmd *cobra.Command, args []string) error {
    store, err := jobs.Open(ctx, cfg.Paths.Jobs)
    if err != nil {
        return err
    }
    defer func() {
        if err := store.Close(); err != nil {
            // Log but don't fail - cleanup is best-effort
            log.Warn("failed to close store", "error", err)
        }
    }()
}
```

#### Example 4: Progress Write Errors
```go
// internal/jobs/store.go:560-565
func (s *Store) executeSkill(...) {
    progressFile, _ := os.OpenFile(progressPath, ...)
    defer progressFile.Close()

    // CRITICAL: Progress events might not be written!
    _ = json.NewEncoder(progressFile).Encode(startEvent)
    _ = json.NewEncoder(progressFile).Encode(resultEvent)
}
```

**Impact**: Lost progress tracking, users can't monitor jobs

**Fix**:
```go
func (s *Store) executeSkill(...) {
    progressFile, err := os.OpenFile(progressPath, ...)
    if err != nil {
        return fmt.Errorf("open progress file: %w", err)
    }
    defer progressFile.Close()

    if err := s.writeProgressEvent(progressFile, startEvent); err != nil {
        return fmt.Errorf("write start event: %w", err)
    }
}

func (s *Store) writeProgressEvent(w io.Writer, event ProgressEvent) error {
    if err := json.NewEncoder(w).Encode(event); err != nil {
        return fmt.Errorf("encode progress event: %w", err)
    }
    return nil
}
```

### Category 3: Legitimate Ignores (ACCEPTABLE BUT SHOULD BE DOCUMENTED)

#### Example 5: Context Cancellation
```go
// Some error ignores are acceptable but should be commented
defer func() {
    _ = store.Close() // Cleanup error is non-fatal
}()
```

**Fix**: Add comments explaining why it's safe
```go
defer func() {
    // Cleanup error is non-fatal - we're already returning the main error
    _ = store.Close()
}()
```

## Proposed Solution

### 1. Error Handling Guidelines

Create `docs/error-handling.md`:

```markdown
# Error Handling Guidelines

## When to Return Errors

1. **Data Integrity**: Always return errors for:
   - JSON marshal/unmarshal
   - Time parsing
   - Database operations
   - File I/O

2. **User Operations**: Always return errors for:
   - Invalid input
   - Missing resources
   - Permission issues

## When to Log Errors

1. **Cleanup Operations**: Log but don't fail for:
   - Close() on defer
   - Cleanup of temporary files
   - Best-effort operations

2. **Background Operations**: Log errors in:
   - Async tasks
   - Periodic cleanup
   - Non-critical updates

## When to Ignore Errors (Rare!)

Only ignore errors when:
1. You've thoroughly analyzed the failure mode
2. Failure is truly harmless
3. You've added a comment explaining why

## Examples

### ❌ Bad
```go
_ = json.Unmarshal(data, &v)  // Silent data corruption!
```

### ✅ Good
```go
if err := json.Unmarshal(data, &v); err != nil {
    return fmt.Errorf("unmarshal config: %w", err)
}
```

### ✅ Acceptable
```go
defer func() {
    // Cleanup is best-effort; main operation already succeeded
    _ = tmpFile.Close()
}()
```
```

### 2. Create Error Utilities

```go
// internal/errors/errors.go
package errors

import (
    "fmt"
    "log/slog"
)

// MustClose logs an error if closing fails, but doesn't panic
// Use in defer statements where cleanup failure is non-fatal
func MustClose(closer io.Closer, logger *slog.Logger) {
    if err := closer.Close(); err != nil {
        logger.Warn("close failed", "error", err)
    }
}

// CloseOnErr closes a resource only if err is non-nil
// Useful for cleanup in error paths
func CloseOnErr(err *error, closer io.Closer) {
    if *err != nil {
        _ = closer.Close() // Already failing, ignore cleanup errors
    }
}

// Must panics if err is non-nil
// Only use in init() or main() for must-succeed operations
func Must(err error) {
    if err != nil {
        panic(err)
    }
}

// Ignore returns a function that ignores an error
// Forces explicit acknowledgment of ignored errors
func Ignore() func(error) {
    return func(error) {}
}
```

Usage:
```go
// Before
defer func() { _ = store.Close() }()

// After
defer errors.MustClose(store, logger)
```

### 3. Linter Configuration

Update `.golangci.yml`:

```yaml
linters-settings:
  errcheck:
    # Check all errors by default
    check-blank: true
    check-type-assertions: true

    # Allowed ignored errors (very limited!)
    exclude-functions:
      - (io.Closer).Close  # Only in defer with comment
      - (*os.File).Close   # Only in defer with comment

  govet:
    enable:
      - errorf  # Check error formatting

  revive:
    rules:
      - name: unhandled-error
        severity: error
        arguments:
          - "fmt.Printf"
          - "fmt.Println"

      - name: defer
        severity: warning
        arguments:
          - ["call-chain", "loop"]
```

## Implementation Plan

### Step 1: Create Error Utilities (1 hour)
- [ ] Create `internal/errors/` package
- [ ] Implement MustClose, CloseOnErr utilities
- [ ] Add tests
- [ ] Add documentation

### Step 2: Fix Critical Data Integrity Errors (4 hours)
- [ ] Fix all JSON unmarshal errors in cache/memory/jobs
- [ ] Fix all time parsing errors
- [ ] Fix all database scan errors
- [ ] Add tests verifying error handling

### Step 3: Fix Progress Writing Errors (2 hours)
- [ ] Fix progress event writing in jobs
- [ ] Extract writeProgressEvent helper
- [ ] Add error propagation
- [ ] Test error paths

### Step 4: Improve Resource Cleanup (3 hours)
- [ ] Replace `_ = x.Close()` with `errors.MustClose(x, logger)`
- [ ] Add logging infrastructure where missing
- [ ] Document cleanup patterns

### Step 5: Add Comments to Legitimate Ignores (2 hours)
- [ ] Audit all remaining ignored errors
- [ ] Add comments explaining why safe
- [ ] Or fix if not actually safe

### Step 6: Update Linter Configuration (1 hour)
- [ ] Update .golangci.yml with strict error checking
- [ ] Run linter and fix issues
- [ ] Add to CI pipeline

### Step 7: Documentation (1 hour)
- [ ] Create error-handling.md guide
- [ ] Add examples to contributing docs
- [ ] Update PR template to mention error handling

**Total Estimated Time**: 14 hours

## Testing Strategy

### Error Injection Tests
```go
// internal/cache/store_test.go
func TestGet_CorruptJSON(t *testing.T) {
    store := setupTestStore(t)

    // Manually corrupt JSON in database
    _, err := store.db.Exec(`
        UPDATE cache_entries
        SET digests = '{invalid json}'
        WHERE key = ?
    `, "key1")
    require.NoError(t, err)

    // Should return error, not corrupt data
    _, _, err = store.Get(context.Background(), "key1")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "unmarshal")
}

func TestGet_InvalidTimestamp(t *testing.T) {
    store := setupTestStore(t)

    // Corrupt timestamp
    _, err := store.db.Exec(`
        UPDATE cache_entries
        SET created_at = 'not-a-timestamp'
        WHERE key = ?
    `, "key1")
    require.NoError(t, err)

    _, _, err = store.Get(context.Background(), "key1")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "parse")
}
```

### Resource Cleanup Tests
```go
func TestClose_ErrorLogged(t *testing.T) {
    var logBuffer bytes.Buffer
    logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

    // Create closer that fails
    failingCloser := &failCloser{err: errors.New("close failed")}

    errors.MustClose(failingCloser, logger)

    assert.Contains(t, logBuffer.String(), "close failed")
}
```

## Audit Results

### Errors by Category

| Category | Count | Priority | Fix Strategy |
|----------|-------|----------|-------------|
| JSON unmarshal | 24 | Critical | Return error |
| Time parsing | 18 | Critical | Return error |
| Database scan | 12 | Critical | Return error (SPEC-003) |
| File I/O | 45 | High | Return error |
| Resource cleanup | 150 | Medium | Log error |
| Already documented | 57 | Low | Keep as-is |

### Files Requiring Most Changes

| File | Ignored Errors | Priority |
|------|---------------|----------|
| internal/cache/store.go | 12 | Critical |
| internal/memory/store.go | 10 | Critical |
| internal/jobs/store.go | 8 | Critical |
| cmd/agentctl/cmd/run.go | 6 | High |
| cmd/agentctl/cmd/jobs.go | 5 | High |

## Benefits

### Before
```go
// Silent failures
_ = json.Unmarshal(data, &v)  // Corrupt data
entry.CreatedAt, _ = time.Parse(...)  // Zero time
_ = encoder.Encode(event)  // Lost progress
defer func() { _ = store.Close() }()  // Resource leak
```

### After
```go
// Proper error handling
if err := json.Unmarshal(data, &v); err != nil {
    return fmt.Errorf("unmarshal: %w", err)
}

entry.CreatedAt, err = sqlutil.ScanTimestamp(...)
if err != nil {
    return fmt.Errorf("scan timestamp: %w", err)
}

if err := s.writeProgressEvent(event); err != nil {
    return fmt.Errorf("write progress: %w", err)
}

defer errors.MustClose(store, logger)
```

### Improvements
- ✅ **306 ignored errors → <60 documented ignores**
- ✅ **Data integrity protected**
- ✅ **Clear error messages for debugging**
- ✅ **Resource cleanup logged**
- ✅ **Linter enforcement**

## Migration Strategy

### Phase 1: Critical Fixes (Cannot Break)
- Fix data integrity errors (JSON, time, DB)
- Add tests for error paths
- These changes catch real bugs

### Phase 2: Improvements (May Break)
- Fix file I/O errors
- Improve error messages
- May uncover edge cases

### Phase 3: Cleanup (Low Risk)
- Add logging for cleanup
- Document remaining ignores
- Cosmetic improvements

### Phase 4: Enforcement
- Enable strict linting
- Add to CI checks
- Update contributing docs

## Success Criteria

- [ ] All JSON unmarshal errors handled
- [ ] All time parsing errors handled
- [ ] All database scan errors handled
- [ ] All file I/O errors handled
- [ ] Resource cleanup uses MustClose or has comments
- [ ] Ignored errors < 60 (down from 306)
- [ ] All remaining ignores have comments
- [ ] Linter enforces error handling
- [ ] Error handling guide documented
- [ ] All tests pass

## Related Specs
- SPEC-003: Database Scanning Helpers (fixes scan errors)
- SPEC-001: Storage Interfaces (cleaner error signatures)

## References
- Go Error Handling Best Practices
- errcheck linter documentation
- Rob Pike: "Errors are values"
