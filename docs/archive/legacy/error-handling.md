# Error Handling Guidelines

This document establishes best practices for error handling in agentctl.

## Table of Contents

1. [General Principles](#general-principles)
2. [When to Return Errors](#when-to-return-errors)
3. [When to Log Errors](#when-to-log-errors)
4. [When to Ignore Errors](#when-to-ignore-errors)
5. [Error Utilities](#error-utilities)
6. [Common Patterns](#common-patterns)

## General Principles

### Never Silently Ignore Errors

**Bad:**
```go
_ = json.Unmarshal(data, &v)  // Silent data corruption!
```

**Good:**
```go
if err := json.Unmarshal(data, &v); err != nil {
    return fmt.Errorf("unmarshal data: %w", err)
}
```

### Always Handle Critical Errors

Critical errors that can lead to data corruption or silent failures must always be handled:

- **JSON unmarshal errors** - Can cause silent data corruption
- **Time parsing errors** - Can result in zero values being used incorrectly
- **Database scan errors** - Can lead to incomplete or corrupt data
- **File I/O errors** - Can result in data loss

### Wrap Errors with Context

Always add context when wrapping errors:

```go
if err := db.QueryRow(query).Scan(&result); err != nil {
    return fmt.Errorf("scan user data: %w", err)
}
```

## When to Return Errors

Return errors when:

1. **Data integrity is at risk**
   ```go
   if err := json.Unmarshal(data, &entry); err != nil {
       return Entry{}, fmt.Errorf("unmarshal entry: %w", err)
   }
   ```

2. **The operation cannot be completed**
   ```go
   if err := os.WriteFile(path, data, 0644); err != nil {
       return fmt.Errorf("write file: %w", err)
   }
   ```

3. **The caller needs to know about the failure**
   ```go
   if err := validate(input); err != nil {
       return fmt.Errorf("validation failed: %w", err)
   }
   ```

## When to Log Errors

Log errors when:

1. **In defer statements where you can't return**
   ```go
   defer errors.MustClose(file, logger)
   ```

2. **For non-critical background operations**
   ```go
   if err := updateAccessTime(id); err != nil {
       logger.Warn("failed to update access time", "error", err, "id", id)
   }
   ```

3. **For metrics or monitoring purposes**
   ```go
   if err := metrics.Record(stat); err != nil {
       logger.Debug("failed to record metric", "error", err)
   }
   ```

## When to Ignore Errors

Only ignore errors when:

1. **The error is truly expected and safe to ignore**
2. **You document WHY it's safe to ignore**
3. **You use the `errors.Ignore()` utility**

### Examples of Acceptable Ignores

**Test cleanup:**
```go
defer func() { _ = store.Close() }()  // Test cleanup
```

**Non-critical operations with fallback:**
```go
errors.Ignore(os.Remove(tempFile), "temp file cleanup failure is not critical")
```

**Optional update operations:**
```go
// Update access metadata - failure is non-critical
_, _ = db.Exec(`UPDATE ... SET last_accessed = ? ...`, time.Now())
```

## Error Utilities

The `internal/errors` package provides utilities for common error handling patterns.

### MustClose

Use for defer cleanup when you have a logger:

```go
func process(logger *slog.Logger) error {
    file, err := os.Open("data.txt")
    if err != nil {
        return err
    }
    defer errors.MustClose(file, logger)
    
    // ... process file ...
    return nil
}
```

### CloseOnErr

Use to clean up resources only on error:

```go
func transaction() (err error) {
    tx, err := db.Begin()
    if err != nil {
        return err
    }
    defer errors.CloseOnErr(tx, &err)
    
    // ... do work ...
    
    return tx.Commit()
}
```

### Must

Use for initialization-time failures:

```go
var config = errors.Must(loadConfig())
```

### Ignore

Use to document intentionally ignored errors:

```go
errors.Ignore(os.Remove(tempFile), "temp file cleanup failure is non-critical")
```

### LogOnErr

Use to log errors while passing them through:

```go
defer errors.LogOnErr(file.Close(), logger, "failed to close file")
```

### MultiClose

Use to close multiple resources:

```go
return errors.MultiClose(file1, file2, conn)
```

## Common Patterns

### Database Scanning

**Bad:**
```go
var entry Entry
var created string
row.Scan(&entry.ID, &created)
entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)  // Zero time on error!
```

**Good:**
```go
var entry Entry
var created string
if err := row.Scan(&entry.ID, &created); err != nil {
    return Entry{}, fmt.Errorf("scan row: %w", err)
}
entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
if err != nil {
    return Entry{}, fmt.Errorf("scan created_at: %w", err)
}
```

### JSON Unmarshaling

**Bad:**
```go
var data MyStruct
_ = json.Unmarshal(input, &data)  // Silent corruption!
```

**Good:**
```go
var data MyStruct
if err := json.Unmarshal(input, &data); err != nil {
    return fmt.Errorf("unmarshal input: %w", err)
}
```

For database JSON fields:
```go
if err := sqlutil.ScanJSON(jsonStr, &entry.Digests); err != nil {
    return Entry{}, fmt.Errorf("scan digests: %w", err)
}
```

### Resource Cleanup

**Without logger (tests):**
```go
defer func() { _ = store.Close() }()  // Test cleanup
```

**With logger (production):**
```go
defer errors.MustClose(store, logger)
```

**With error propagation:**
```go
func process() (err error) {
    db, err := sql.Open(...)
    if err != nil {
        return err
    }
    defer errors.CloseOnErr(db, &err)
    
    // ... do work ...
    return nil
}
```

### File I/O

**Bad:**
```go
_ = os.WriteFile(path, data, 0644)  // Data loss!
```

**Good:**
```go
if err := os.WriteFile(path, data, 0644); err != nil {
    return fmt.Errorf("write file %s: %w", path, err)
}
```

### Optional Operations

When an operation is truly optional and failure doesn't affect correctness:

```go
// Update access metadata - failure is non-critical for correctness
_, _ = db.Exec(`UPDATE entries SET last_accessed = ? WHERE id = ?`, time.Now(), id)
```

Or better:
```go
// Update access metadata - logged but non-fatal
if err := updateAccessMetadata(id); err != nil {
    logger.Debug("failed to update access metadata", "error", err, "id", id)
}
```

## Linter Configuration

The `.golangci.yml` configuration enforces these patterns:

- `errcheck` with `check-blank: true` catches ignored errors
- `revive` with `unhandled-error` rule for additional checks

Run linter before committing:
```bash
golangci-lint run
```

## Summary

1. **Never** silently ignore critical errors (JSON, time parsing, DB scans, file I/O)
2. **Always** add context when wrapping errors
3. **Use** error utilities from `internal/errors` for common patterns
4. **Document** why errors are ignored using `errors.Ignore()` or comments
5. **Log** errors in defer statements using `errors.MustClose()`
6. **Test** error paths to ensure they're handled correctly

When in doubt, return the error. It's better to be explicit than to silently fail.
