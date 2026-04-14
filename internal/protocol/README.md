# Protocol Package

The `protocol` package centralizes wire-level protocol semantics for foxctl,
providing a single source of truth for envelope construction, validation, and
error codes according to the Core Profile v1 specification.

## Overview

This package wraps `internal/domain/envelope` and provides:

1. **Canonical error codes** - All error codes from Core Profile v1 (EARG,
   EOPENAPI, EAUTH, etc.)
2. **Helper functions** - Simplified API for building and writing envelopes
3. **Extended validation** - Protocol-level checks for CAS digests, cache
   metadata, and status codes
4. **Typed error helpers** - Specialized constructors for common error scenarios
   (HTTP, validation, auth, etc.)
5. **Annotation helpers** - Functions for annotating envelopes with run/cache
   metadata

## Error Codes

The package defines typed error codes matching the Core Profile v1
specification:

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
env := protocol.OK("foxctl.test", map[string]string{"status": "healthy"})

// With metadata options
env := protocol.OK("foxctl.run", data,
    protocol.WithSource("run"),
    protocol.WithWorkspace("/path/to/workspace"),
    protocol.WithSkillVersion("v1.0.0"),
)
```

### Error Envelopes

```go
env := protocol.Error(
    "foxctl.test",
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

## Typed Error Helpers

The protocol package provides specialized error envelope constructors with typed
data payloads for common error scenarios:

### Validation Errors

```go
env := protocol.ValidationError("user.create", "invalid email", protocol.ValidationErrorData{
    Field:  "email",
    Value:  "not-an-email",
    Reason: "must be a valid email address",
    Hint:   "use format: user@example.com",
})
```

### HTTP Errors

```go
env := protocol.HTTPError("api.call", "request failed", protocol.HTTPErrorData{
    Summary: protocol.HTTPSummary{
        StatusCode: 401,
        Method:     "GET",
        URL:        "https://api.example.com/users",
        Headers:    map[string]string{"content-type": "application/json"},
    },
    Hint: "check your API key",
})
// Error code is automatically mapped: 401 -> EAUTH, 404 -> ENOTFOUND, etc.
```

### Auth Errors

```go
env := protocol.AuthError("auth.login", "authentication failed", "check your credentials")
```

### Not Found Errors

```go
env := protocol.NotFoundError("skill.get", "skill", "foo/bar")
// Generates: "skill not found" with context {"resource": "skill", "identifier": "foo/bar"}
```

### Timeout Errors

```go
env := protocol.TimeoutError("job.wait", "skill execution", "30s")
```

### Rate Limit Errors

```go
env := protocol.RateLimitError("api.call", "rate limit exceeded", "60s")
```

### Policy Errors

```go
env := protocol.PolicyError("skill.run", "network.egress", "egress to example.com not allowed")
```

### Generic Error with Data

```go
env := protocol.ErrorWithData("op.fail", protocol.ErrorCodeEIO, "I/O error", protocol.ErrorData{
    Detail: "failed to write file",
    Hint:   "check disk space",
    Context: map[string]any{
        "path": "/tmp/file.txt",
    },
})
```

## Validation

The protocol package extends base envelope validation with protocol-level
checks:

### Base Validation

```go
// Validate an envelope (includes extended checks)
if err := protocol.Validate(env); err != nil {
    // Handle validation error
}

// Panic on invalid envelope (for tests)
protocol.MustValidate(env)
```

### Extended Validation Rules

The `Validate` function performs these protocol-level checks:

#### 1. CAS Digest Matching

- `meta.cas_digest` is optional; if set it MUST match `data.artifact` and MUST
  be omitted when `data.artifact` is absent
- Artifact field must use `sha256:` prefix

```go
// Valid
env := protocol.OK("cmd", map[string]any{
    "artifact": "sha256:abc123",
})

// Valid
env := protocol.OK("cmd", map[string]any{
    "artifact": "sha256:abc123",
}, protocol.WithCASDigest("sha256:abc123"))

// Invalid - digest mismatch
env := protocol.OK("cmd", map[string]any{
    "artifact": "sha256:abc123",
}, protocol.WithCASDigest("sha256:different"))
```

#### 2. Cache Metadata Consistency

- If `meta.source == "cache"`, `meta.cache_key` must be set

```go
// Valid
env := protocol.OK("cmd", nil,
    protocol.WithSource("cache"),
    protocol.WithCacheKey("key123"),
)

// Invalid - missing cache_key
env := protocol.OK("cmd", nil, protocol.WithSource("cache"))
```

#### 3. Error Status Code Validation

- Error envelopes with `data.summary.status_code` must have codes in 400-599
  range

```go
// Valid
env := protocol.Error("cmd", protocol.ErrorCodeERuntime, "error", map[string]any{
    "summary": map[string]any{
        "status_code": 500,
    },
})

// Invalid - 2xx status in error envelope
env := protocol.Error("cmd", protocol.ErrorCodeERuntime, "error", map[string]any{
    "summary": map[string]any{
        "status_code": 200,
    },
})
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
import "github.com/joshka0/foxctl/internal/domain/envelope"

data := map[string]any{"config": cfg}
return envelope.Write(cmd.OutOrStdout(), envelope.OK("foxctl.doctor", data))
```

### After

```go
import "github.com/joshka0/foxctl/internal/protocol"

data := map[string]any{"config": cfg}
return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.doctor", data,
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
