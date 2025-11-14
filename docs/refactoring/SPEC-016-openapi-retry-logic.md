# SPEC-016: OpenAPI Retry Logic

## Status
**Not Started** | Priority: High | Complexity: Low

## Problem Statement

APIs may return transient errors (429 rate limit, 5xx server errors). Need retry with exponential backoff.

## Proposed Solution

```go
// internal/openapi/retry/retry.go
package retry

type Config struct {
    MaxAttempts   int           `json:"max_attempts"`    // Default: 3
    InitialDelay  time.Duration `json:"initial_delay"`   // Default: 1s
    MaxDelay      time.Duration `json:"max_delay"`       // Default: 30s
    Multiplier    float64       `json:"multiplier"`      // Default: 2.0
    Jitter        bool          `json:"jitter"`          // Default: true
}

type Retryer struct {
    config Config
}

func (r *Retryer) Execute(ctx context.Context, fn func() (*http.Response, error)) (*http.Response, error) {
    // Exponential backoff with jitter
    // Respect Retry-After header
    // Only retry 429, 500, 502, 503, 504
}
```

## Implementation Plan

1. **Exponential backoff** (2h)
2. **Retry-After parsing** (1h)
3. **Retryable error detection** (1h)
4. **Context cancellation** (1h)
5. **Tests** (3h)

## Effort Estimate
**Total: 8 hours**

## Dependencies
- **Depends on:** SPEC-014 (HTTP Client)
