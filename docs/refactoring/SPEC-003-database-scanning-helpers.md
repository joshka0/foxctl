# SPEC-003: Database Scanning Helpers

## Status
**Draft** | Priority: High | Complexity: Medium

## Problem Statement

Database row scanning code is **repeated 8+ times** across storage packages with identical patterns for:
- JSON unmarshaling from string columns
- RFC3339Nano timestamp parsing
- Row scanning with type conversion
- Error handling (currently ignored silently)

This leads to:
- **Code Duplication**: Same scanning logic copy-pasted
- **Inconsistent Error Handling**: Some errors ignored, some returned
- **Maintenance Burden**: Bug fixes must be applied in 8+ places
- **Increased LOC**: ~15 lines of boilerplate per query

### Affected Files and Lines
- `internal/storage/cache/store.go:158-167` (Get method)
- `internal/storage/cache/store.go:212-219` (Recent method)
- `internal/storage/memory/store.go:127-136` (Get method)
- `internal/storage/memory/store.go:167-173` (List method)
- `internal/storage/jobs/store.go:143-152` (Get method)
- `internal/storage/jobs/store.go:171-180` (List method)
- Similar patterns in CAS store

## Current State Analysis

### Example 1: Cache Store Scanning
```go
// internal/storage/cache/store.go:158-167
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    // ... query ...
    var entry Entry
    var digests string
    var created, expires, last string

    err := row.Scan(&entry.Key, &entry.Workspace, &entry.Result, &digests,
                    &created, &expires, &last, &entry.Count)
    if err == sql.ErrNoRows {
        return Entry{}, false, nil
    }

    // Errors silently ignored!
    _ = json.Unmarshal([]byte(digests), &entry.Digests)
    entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
    entry.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
    entry.LastAccessed, _ = time.Parse(time.RFC3339Nano, last)

    return entry, true, nil
}
```

### Example 2: Memory Store Scanning (Nearly Identical)
```go
// internal/storage/memory/store.go:127-136
func (s *Store) Get(ctx context.Context, name, workspace string) (NamedEntry, error) {
    // ... query ...
    var entry NamedEntry
    var digests string
    var created, updated string

    err := row.Scan(&entry.Name, &entry.Type, &entry.Workspace, &entry.Summary,
                    &entry.Data, &digests, &created, &updated)

    // Same errors ignored!
    _ = json.Unmarshal([]byte(digests), &entry.Digests)
    entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
    entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)

    return entry, nil
}
```

### Problems Identified
1. **Silent Failures**: JSON unmarshal errors ignored - corrupt data undetected
2. **Silent Failures**: Time parse errors ignored - zero times returned
3. **Code Duplication**: Same 4-line pattern repeated 8+ times
4. **No Validation**: No check if timestamps are valid
5. **Inconsistent**: Some places check errors, some don't

## Proposed Solution

Create a shared `internal/sqlutil` package with reusable scanning helpers.

### Architecture

```
internal/
├── sqlutil/
│   ├── scan.go          # Scanning helper functions
│   ├── scan_test.go     # Tests for scanners
│   ├── types.go         # SQL-friendly types (JSONSlice, Timestamp)
│   └── types_test.go    # Tests for types
```

### Implementation

#### 1. Core Scanning Functions

```go
// internal/sqlutil/scan.go
package sqlutil

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "time"
)

// ScanJSON unmarshals a JSON string column into a Go value
func ScanJSON(src string, dest interface{}) error {
    if src == "" {
        return nil // Empty string = null
    }
    if err := json.Unmarshal([]byte(src), dest); err != nil {
        return fmt.Errorf("unmarshal json: %w", err)
    }
    return nil
}

// ScanTimestamp parses an RFC3339Nano timestamp string
func ScanTimestamp(src string) (time.Time, error) {
    if src == "" {
        return time.Time{}, nil // Empty string = null
    }
    t, err := time.Parse(time.RFC3339Nano, src)
    if err != nil {
        return time.Time{}, fmt.Errorf("parse timestamp: %w", err)
    }
    return t, nil
}

// ScanNullableTimestamp parses a timestamp that may be NULL
func ScanNullableTimestamp(src sql.NullString) (time.Time, error) {
    if !src.Valid {
        return time.Time{}, nil
    }
    return ScanTimestamp(src.String)
}

// FormatTimestamp formats a time for SQL storage
func FormatTimestamp(t time.Time) string {
    if t.IsZero() {
        return ""
    }
    return t.UTC().Format(time.RFC3339Nano)
}

// FormatJSON marshals a value to JSON for SQL storage
func FormatJSON(v interface{}) (string, error) {
    if v == nil {
        return "", nil
    }
    data, err := json.Marshal(v)
    if err != nil {
        return "", fmt.Errorf("marshal json: %w", err)
    }
    return string(data), nil
}
```

#### 2. Custom SQL Types

```go
// internal/sqlutil/types.go
package sqlutil

import (
    "database/sql"
    "database/sql/driver"
    "encoding/json"
    "time"
)

// JSONSlice is a []string that marshals to/from JSON in SQL
type JSONSlice []string

// Scan implements sql.Scanner
func (j *JSONSlice) Scan(src interface{}) error {
    if src == nil {
        *j = nil
        return nil
    }

    var data []byte
    switch v := src.(type) {
    case []byte:
        data = v
    case string:
        data = []byte(v)
    default:
        return fmt.Errorf("unsupported type: %T", src)
    }

    return json.Unmarshal(data, j)
}

// Value implements driver.Valuer
func (j JSONSlice) Value() (driver.Value, error) {
    if j == nil {
        return nil, nil
    }
    return json.Marshal(j)
}

// Timestamp wraps time.Time with RFC3339Nano SQL encoding
type Timestamp struct {
    time.Time
}

// Scan implements sql.Scanner
func (t *Timestamp) Scan(src interface{}) error {
    if src == nil {
        t.Time = time.Time{}
        return nil
    }

    var str string
    switch v := src.(type) {
    case []byte:
        str = string(v)
    case string:
        str = v
    default:
        return fmt.Errorf("unsupported type: %T", src)
    }

    parsed, err := time.Parse(time.RFC3339Nano, str)
    if err != nil {
        return err
    }
    t.Time = parsed
    return nil
}

// Value implements driver.Valuer
func (t Timestamp) Value() (driver.Value, error) {
    if t.IsZero() {
        return nil, nil
    }
    return t.UTC().Format(time.RFC3339Nano), nil
}

// NewTimestamp creates a Timestamp from time.Time
func NewTimestamp(t time.Time) Timestamp {
    return Timestamp{Time: t}
}
```

#### 3. Typed Row Scanners

```go
// internal/sqlutil/scan.go (continued)

// ScanRow is a helper for scanning a single row with error handling
type ScanRow struct {
    row *sql.Row
    err error
}

// Scan wraps row.Scan with error accumulation
func (s *ScanRow) Scan(dest ...interface{}) *ScanRow {
    if s.err != nil {
        return s
    }
    s.err = s.row.Scan(dest...)
    return s
}

// Err returns any accumulated errors
func (s *ScanRow) Err() error {
    return s.err
}

// NoRows returns true if error is sql.ErrNoRows
func (s *ScanRow) NoRows() bool {
    return s.err == sql.ErrNoRows
}

// NewScanRow creates a ScanRow wrapper
func NewScanRow(row *sql.Row) *ScanRow {
    return &ScanRow{row: row}
}
```

### Refactored Usage

#### Before: cache.Store.Get()
```go
// internal/storage/cache/store.go:158-167 (BEFORE)
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    row := s.db.QueryRowContext(ctx, `SELECT ...`)

    var entry Entry
    var digests string
    var created, expires, last string

    err := row.Scan(&entry.Key, &entry.Workspace, &entry.Result, &digests,
                    &created, &expires, &last, &entry.Count)
    if err == sql.ErrNoRows {
        return Entry{}, false, nil
    }
    if err != nil {
        return Entry{}, false, err
    }

    _ = json.Unmarshal([]byte(digests), &entry.Digests)
    entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
    entry.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
    entry.LastAccessed, _ = time.Parse(time.RFC3339Nano, last)

    return entry, true, nil
}
```

#### After: Using Helper Functions
```go
// internal/storage/cache/store.go (AFTER - Option 1: Helper functions)
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    row := s.db.QueryRowContext(ctx, `SELECT ...`)

    var entry Entry
    var digests, created, expires, last string

    err := row.Scan(&entry.Key, &entry.Workspace, &entry.Result, &digests,
                    &created, &expires, &last, &entry.Count)
    if err == sql.ErrNoRows {
        return Entry{}, false, nil
    }
    if err != nil {
        return Entry{}, false, err
    }

    // Now with proper error handling!
    if err := sqlutil.ScanJSON(digests, &entry.Digests); err != nil {
        return Entry{}, false, fmt.Errorf("scan digests: %w", err)
    }

    entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
    if err != nil {
        return Entry{}, false, fmt.Errorf("scan created_at: %w", err)
    }

    entry.ExpiresAt, err = sqlutil.ScanTimestamp(expires)
    if err != nil {
        return Entry{}, false, fmt.Errorf("scan expires_at: %w", err)
    }

    entry.LastAccessed, err = sqlutil.ScanTimestamp(last)
    if err != nil {
        return Entry{}, false, fmt.Errorf("scan last_accessed: %w", err)
    }

    return entry, true, nil
}
```

#### After: Using Custom Types (Better)
```go
// internal/storage/cache/store.go (AFTER - Option 2: Custom types)

// Update Entry struct
type Entry struct {
    Key          string
    Workspace    string
    Result       []byte
    Digests      sqlutil.JSONSlice  // Changed from []string
    CreatedAt    sqlutil.Timestamp  // Changed from time.Time
    ExpiresAt    sqlutil.Timestamp  // Changed from time.Time
    LastAccessed sqlutil.Timestamp  // Changed from time.Time
    Count        int
}

func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
    row := s.db.QueryRowContext(ctx, `SELECT ...`)

    var entry Entry

    // Direct scanning with automatic conversion!
    err := row.Scan(&entry.Key, &entry.Workspace, &entry.Result, &entry.Digests,
                    &entry.CreatedAt, &entry.ExpiresAt, &entry.LastAccessed, &entry.Count)
    if err == sql.ErrNoRows {
        return Entry{}, false, nil
    }
    if err != nil {
        return Entry{}, false, fmt.Errorf("scan row: %w", err)
    }

    return entry, true, nil
}
```

## Implementation Plan

### Step 1: Create sqlutil Package (2 hours)
- [ ] Create `internal/sqlutil/` directory
- [ ] Create `scan.go` with helper functions
- [ ] Create `types.go` with JSONSlice and Timestamp
- [ ] Add comprehensive tests

### Step 2: Update Cache Package (1 hour)
- [ ] Update Entry struct to use sqlutil types
- [ ] Update Get() method
- [ ] Update Recent() method
- [ ] Update Put() method (for FormatJSON)
- [ ] Run tests

### Step 3: Update Memory Package (1 hour)
- [ ] Update NamedEntry struct to use sqlutil types
- [ ] Update Get() method
- [ ] Update List() method
- [ ] Update Save() method
- [ ] Run tests

### Step 4: Update Jobs Package (1.5 hours)
- [ ] Update Job struct to use sqlutil types
- [ ] Update Get() method
- [ ] Update List() method
- [ ] Update state transition methods
- [ ] Run tests

### Step 5: Update CAS Package (1 hour)
- [ ] Update Object and Metadata structs
- [ ] Update relevant methods
- [ ] Run tests

### Step 6: Add Integration Tests (1 hour)
- [ ] Test corrupt JSON handling
- [ ] Test invalid timestamp handling
- [ ] Test NULL value handling
- [ ] Test round-trip marshaling

### Step 7: Documentation (0.5 hours)
- [ ] Add godoc comments
- [ ] Add usage examples
- [ ] Update package documentation

**Total Estimated Time**: 8 hours

## Testing Strategy

### Unit Tests for sqlutil Package
```go
// internal/sqlutil/scan_test.go
package sqlutil_test

func TestScanJSON(t *testing.T) {
    tests := []struct {
        name    string
        src     string
        dest    interface{}
        want    interface{}
        wantErr bool
    }{
        {"valid array", `["a","b"]`, &[]string{}, []string{"a", "b"}, false},
        {"empty string", "", &[]string{}, []string(nil), false},
        {"invalid json", `{bad}`, &[]string{}, nil, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ScanJSON(tt.src, tt.dest)
            if (err != nil) != tt.wantErr {
                t.Errorf("wantErr %v, got %v", tt.wantErr, err)
            }
            if !tt.wantErr && !reflect.DeepEqual(tt.dest, tt.want) {
                t.Errorf("want %v, got %v", tt.want, tt.dest)
            }
        })
    }
}

func TestScanTimestamp(t *testing.T) {
    tests := []struct {
        name    string
        src     string
        want    time.Time
        wantErr bool
    }{
        {"valid", "2024-01-01T12:00:00.123456789Z",
         time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC), false},
        {"empty", "", time.Time{}, false},
        {"invalid", "not-a-time", time.Time{}, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ScanTimestamp(tt.src)
            if (err != nil) != tt.wantErr {
                t.Errorf("wantErr %v, got %v", tt.wantErr, err)
            }
            if !tt.wantErr && !got.Equal(tt.want) {
                t.Errorf("want %v, got %v", tt.want, got)
            }
        })
    }
}
```

### Integration Tests
```go
// internal/sqlutil/integration_test.go
func TestRoundTripWithDatabase(t *testing.T) {
    db, _ := sql.Open("sqlite", ":memory:")
    defer db.Close()

    _, err := db.Exec(`
        CREATE TABLE test (
            id INTEGER PRIMARY KEY,
            tags TEXT,
            created TEXT
        )
    `)
    require.NoError(t, err)

    // Insert using custom types
    tags := JSONSlice{"tag1", "tag2"}
    created := NewTimestamp(time.Now())

    _, err = db.Exec("INSERT INTO test (tags, created) VALUES (?, ?)", tags, created)
    require.NoError(t, err)

    // Read back
    var readTags JSONSlice
    var readCreated Timestamp

    err = db.QueryRow("SELECT tags, created FROM test WHERE id = 1").
        Scan(&readTags, &readCreated)
    require.NoError(t, err)

    assert.Equal(t, tags, readTags)
    assert.True(t, created.Equal(readCreated.Time))
}
```

## Benefits

### Before
- 15 lines of boilerplate per query
- Errors silently ignored
- Code duplicated 8+ times
- No validation of data integrity

### After
- 1-2 lines per field with helper functions
- OR direct scanning with custom types
- All errors properly handled
- Data integrity validated
- Centralized, testable utilities

### Metrics
- **Lines of Code Reduced**: ~100 lines (15 lines × 8 locations → 10 lines total)
- **Error Handling Improved**: 306 ignored errors → 0 ignored errors
- **Maintainability**: 1 location to fix bugs vs 8 locations
- **Type Safety**: Custom types provide compile-time safety

## Migration Strategy

### Phase 1: Create sqlutil Package
- Add new package without modifying existing code
- All existing functionality remains working

### Phase 2: Optional Migration
- Packages can adopt incrementally
- Can use helper functions first, custom types later
- No breaking changes

### Phase 3: Standardize
- Once all packages migrated, enforce via linting
- Document as standard practice

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Custom types break existing code | High | Use type aliases initially |
| Performance overhead of validation | Low | Benchmark, minimal overhead expected |
| Learning curve for new developers | Low | Clear documentation and examples |

## Success Criteria

- [ ] sqlutil package created with 95%+ test coverage
- [ ] All storage packages migrated
- [ ] Zero errors silently ignored in scanning code
- [ ] All existing tests pass
- [ ] Code review approval
- [ ] Documentation complete

## Related Specs
- SPEC-001: Storage Interfaces (types should align)
- SPEC-006: Fix Error Handling (addresses same issue)

## References
- Go database/sql documentation
- sql.Scanner and driver.Valuer interfaces
- Go database best practices
