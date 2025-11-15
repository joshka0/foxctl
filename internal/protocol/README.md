# Protocol Package

The `protocol` package centralizes wire-level protocol semantics for agentctl, providing a single source of truth for envelope construction, validation, and error codes according to the Core Profile v1 specification.

## Overview

This package wraps `internal/domain/envelope` and provides:

1. **Canonical error codes** - All error codes from Core Profile v1 (EARG, EOPENAPI, EAUTH, etc.)
2. **Helper functions** - Simplified API for building and writing envelopes
3. **Validation** - Centralized envelope validation before emission
4. **Annotation helpers** - Functions for annotating envelopes with run/cache metadata

## Error Codes

The package defines typed error codes matching the Core Profile v1 specification:

```go
const (
    ErrorCodeEARG            // Invalid arguments or validation errors
    ErrorCodeEOpenAPI        // OpenAPI spec parsing/validation errors
    ErrorCodeEAuth           // Authentication or credential problems
    ErrorCodeEPagination     // Pagination-related failures
    ErrorCodeERateLimit      // Rate limit or backoff exhaustion
    ErrorCodeERuntime        // Runtime or generic execution failure
    ErrorCodeEOutputTooLarge // Output exceeded capture limits
    ErrorCodeEPolicy         // Capability or policy violations
    ErrorCodeENotFound       // Resource not found
    ErrorCodeETimeout        // Operation timeout
    ErrorCodeESkillDown      // Skill unavailability
    // ... and more
)
```

## Building Envelopes

### OK Envelopes

```go
// Simple success envelope
env := protocol.OK("agentctl.test", map[string]string{"status": "healthy"})

// With metadata options
env := protocol.OK("agentctl.run", data,
    protocol.WithSource("run"),
    protocol.WithWorkspace("/path/to/workspace"),
    protocol.WithSkillVersion("v1.0.0"),
)
```

### Error Envelopes

```go
env := protocol.Error(
    "agentctl.test",
    protocol.ErrorCodeEAuth,
    "authentication failed",
    map[string]string{"hint": "check credentials"},
)
```

## Writing Envelopes

### Direct Writing

```go
// Validate and write
err := protocol.Write(w, env)

// Build and write in one step
err := protocol.WriteOK(w, "cmd", data, protocol.WithSource("run"))
err := protocol.WriteError(w, "cmd", protocol.ErrorCodeEARG, "bad arg", nil)
```

## Annotation Helpers

### Annotate Run Results

```go
// Annotate an envelope with run metadata
env := protocol.AnnotateRun(env, workspace, skillVersion)

// Or annotate raw JSON bytes
annotated := protocol.AnnotateRunBytes(resultBytes, workspace, skillVersion)
```

### Annotate Cache Hits

```go
// Annotate an envelope with cache metadata
env, err := protocol.AnnotateCacheHit(env, cacheKey, workspace, skillVersion)

// Or annotate raw JSON bytes
annotated, err := protocol.AnnotateCacheHitBytes(resultBytes, cacheKey, workspace, skillVersion)
```

### Memory Summaries

```go
// Create a summary for memory storage
summary := protocol.SummarizeForMemory(env) // "fs/read (project)"

// From raw bytes
summary := protocol.SummarizeForMemoryBytes(resultBytes)
```

## Options

All option functions are composable:

```go
env := protocol.OK("cmd", data,
    protocol.WithSource("cache"),
    protocol.WithWorkspace("/ws"),
    protocol.WithSkillVersion("v2.0.0"),
    protocol.WithJobID("job-123"),
    protocol.WithCacheKey("key-abc"),
    protocol.WithCASDigest("sha256:abc123"),
    protocol.WithRunner("wasi"),
    protocol.WithDuration(150),
    protocol.WithMeta(func(m *envelope.Meta) {
        // Custom meta mutations
    }),
)
```

## Validation

```go
// Validate an envelope
if err := protocol.Validate(env); err != nil {
    // Handle validation error
}

// Panic on invalid envelope (for tests)
protocol.MustValidate(env)
```

## Utility Functions

```go
// Check envelope status
if protocol.IsOK(env) { /* ... */ }
if protocol.IsError(env) { /* ... */ }

// Get error code from error envelope
code := protocol.GetErrorCode(env) // ErrorCodeEAuth

// Check if error is retryable
if protocol.IsRetryable(code) {
    // Retry logic
}
```

## Migration from `envelope` Package

### Before

```go
import "github.com/jkatigb/agentctl/internal/domain/envelope"

data := map[string]any{"config": cfg}
return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.doctor", data))
```

### After

```go
import "github.com/jkatigb/agentctl/internal/protocol"

data := map[string]any{"config": cfg}
return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.doctor", data,
    protocol.WithSource("run"))
```

## Design Principles

1. **No CLI dependencies** - This package is strictly protocol-level
2. **Wraps, doesn't replace** - Uses `envelope` package internally
3. **Validation by default** - All write helpers validate before emission
4. **Composable options** - Option pattern for flexibility
5. **Small and focused** - Only protocol concerns, no business logic

## See Also

- `docs/spec/core_profile_v1.md` - Core Profile v1 specification
- `docs/error-handling.md` - Error handling conventions
- `internal/domain/envelope` - Low-level envelope types and functions
