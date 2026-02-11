# Chain Middleware Expansion

## Summary

Expand the `skillmain.Chain` middleware system with new middleware types and adopt it across representative skills. The infrastructure exists (`Chain`, `WithTimeout`, `WithRecover`, `WithSkillStep`) but is currently unused. This plan adds 3 new middleware, adopts `Chain` in 5 skills, then adds unit tests for the full middleware suite.

**Estimated Scope:** ~15 files modified, ~200 lines added, ~80 lines removed
**Branch:** `refactor/chain-middleware-expansion`
**Depends on:** PR #209 (design-pattern-standardization) — must be merged first

---

## Background

### What exists today

```
internal/adapters/skillslib/skillmain/middleware.go
```

```go
type Middleware[I any] func(RunFunc[I]) RunFunc[I]

func Chain[I any](run RunFunc[I], mw ...Middleware[I]) RunFunc[I]
func WithTimeout[I any](d time.Duration) Middleware[I]
func WithRecover[I any]() Middleware[I]
func WithSkillStep[I any](name string) Middleware[I]
```

### Current usage

- **0 skills** use `Chain`
- **24 skills** have manual `context.WithTimeout` calls (10 at top-level `run()`)
- **0 skills** use `recover()` — panics propagate unhandled

### Related files

| File | Role |
|------|------|
| `internal/adapters/skillslib/skillmain/middleware.go` | Middleware definitions |
| `internal/adapters/skillslib/skillmain/main.go` | `Main[I]` entrypoint, calls `run(ctx, rc, input)` at line 180 |
| `internal/adapters/skillslib/skillmain/steps.go` | `Step()` used by `WithSkillStep` |
| `internal/adapters/skillslib/skillmain/breakers.go` | `GuardCall` used by skills |

---

## Phase 1: New Middleware (3 additions)

All additions go in `internal/adapters/skillslib/skillmain/middleware.go`.

### 1.1 `WithDynamicTimeout`

**Problem:** Skills like `lsp_gopls` and `lsp_tsserver` derive timeout from user input (e.g., `in.Timeout`). `WithTimeout` only accepts a static `time.Duration`.

**Solution:**

```go
// WithDynamicTimeout extracts a timeout duration from the input struct.
// If getDuration returns 0, no timeout is applied.
func WithDynamicTimeout[I any](getDuration func(I) time.Duration) Middleware[I] {
    return func(next RunFunc[I]) RunFunc[I] {
        return func(ctx context.Context, rc *RunContext, in I) error {
            d := getDuration(in)
            if d > 0 {
                var cancel context.CancelFunc
                ctx, cancel = context.WithTimeout(ctx, d)
                defer cancel()
            }
            return next(ctx, rc, in)
        }
    }
}
```

**Dependencies:** None
**Risk:** Low — additive, generic helper

---

### 1.2 `WithRetry`

**Problem:** LLM-heavy skills sometimes fail on transient errors. Manual retry loops are scattered across skills like `session_summarize` and `calibration_generate`.

**Solution:**

```go
// RetryPolicy configures retry behavior.
type RetryPolicy struct {
    MaxAttempts int           // Total attempts (1 = no retry)
    Backoff     time.Duration // Initial backoff between retries
    Retryable   func(error) bool // Return true to retry; nil = retry all errors
}

// WithRetry retries the skill on transient errors with exponential backoff.
func WithRetry[I any](policy RetryPolicy) Middleware[I] {
    return func(next RunFunc[I]) RunFunc[I] {
        return func(ctx context.Context, rc *RunContext, in I) error {
            maxAttempts := policy.MaxAttempts
            if maxAttempts <= 0 {
                maxAttempts = 1
            }
            backoff := policy.Backoff
            if backoff <= 0 {
                backoff = time.Second
            }
            var lastErr error
            for attempt := 0; attempt < maxAttempts; attempt++ {
                if attempt > 0 {
                    rc.Logger.Warn().Int("attempt", attempt+1).Err(lastErr).Msg("retrying skill")
                    select {
                    case <-ctx.Done():
                        return ctx.Err()
                    case <-time.After(backoff):
                    }
                    backoff *= 2 // exponential
                }
                lastErr = next(ctx, rc, in)
                if lastErr == nil {
                    return nil
                }
                if policy.Retryable != nil && !policy.Retryable(lastErr) {
                    return lastErr
                }
            }
            return lastErr
        }
    }
}
```

**Note:** The `time.After` in retry should use `rc.Now()` only if we add a `rc.Sleep()` or `rc.Timer()` helper for testability. For now, `time.After` is acceptable — retry middleware is inherently I/O-bound. Document this as a known limitation.

**Dependencies:** None
**Risk:** Low — additive, opt-in per skill

---

### 1.3 `WithInputLog`

**Problem:** Many skills manually log their input at the top of `run()`. This is inconsistent — some log all fields, some log none.

**Solution:**

```go
// WithInputLog logs a structured summary of the input on entry.
// The summarize function extracts loggable fields (avoid logging sensitive data).
// If summarize is nil, logs only the input type name.
func WithInputLog[I any](summarize func(I) map[string]any) Middleware[I] {
    return func(next RunFunc[I]) RunFunc[I] {
        return func(ctx context.Context, rc *RunContext, in I) error {
            ev := rc.Logger.Debug().Str("event", "skill_input")
            if summarize != nil {
                for k, v := range summarize(in) {
                    ev = ev.Interface(k, v)
                }
            }
            ev.Msg("input received")
            return next(ctx, rc, in)
        }
    }
}
```

**Dependencies:** None
**Risk:** Low — debug-level logging only

---

## Phase 2: Adopt Chain in 5 Skills

Each skill conversion follows this pattern:
1. Move top-level `context.WithTimeout` into middleware
2. Add `WithRecover` for panic safety
3. Wire through `Chain` in `main()`
4. Remove the manual timeout/cancel boilerplate from `run()`

### 2.1 `code_counsel`

**File:** `skills/code_counsel/main.go`

**Before:**
```go
func main() {
    skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
    // ...defaults...
    ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
    defer cancel()
    // ...rest...
}
```

**After:**
```go
func main() {
    skillmain.Main(command, skillmain.Chain(run,
        skillmain.WithTimeout[Input](DefaultTimeout),
        skillmain.WithRecover[Input](),
    ))
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
    // ...defaults...
    // (timeout removed — handled by middleware)
    // ...rest...
}
```

**Lines removed:** ~2 (timeout + defer cancel)
**Risk:** Low — direct 1:1 replacement

---

### 2.2 `calibration_generate`

**File:** `skills/calibration_generate/main.go`

**Before:** `ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)` at top of `run()`

**After:**
```go
func main() {
    skillmain.Main(command, skillmain.Chain(run,
        skillmain.WithTimeout[Input](10*time.Minute),
        skillmain.WithRecover[Input](),
    ))
}
```

Remove the `ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)` and `defer cancel()` from `run()`.

**Note:** This skill also has inner timeouts on LLM HTTP calls (90s, 2min). Those stay as-is — they are per-request timeouts, not skill-level.

**Risk:** Low

---

### 2.3 `session_summarize`

**File:** `skills/session_summarize/main.go`

**Before:** `ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)` at top of `run()`

**After:**
```go
func main() {
    skillmain.Main(command, skillmain.Chain(run,
        skillmain.WithTimeout[Input](5*time.Minute),
        skillmain.WithRecover[Input](),
    ))
}
```

Remove the timeout boilerplate from `run()`.

**Note:** Same as calibration_generate — inner per-HTTP-call timeouts stay.

**Risk:** Low

---

### 2.4 `lsp_gopls`

**File:** `skills/lsp_gopls/main.go`

This skill uses a **dynamic timeout** from `in.Timeout`:

**Before:**
```go
timeout := defaultTimeout
if in.Timeout > 0 {
    timeout = time.Duration(in.Timeout) * time.Second
}
ctx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()
```

**After:**
```go
func main() {
    skillmain.Main(command, skillmain.Chain(run,
        skillmain.WithDynamicTimeout[Input](func(in Input) time.Duration {
            if in.Timeout > 0 {
                return time.Duration(in.Timeout) * time.Second
            }
            return defaultTimeout
        }),
        skillmain.WithRecover[Input](),
    ))
}
```

Remove the 4-line timeout block from `run()`.

**Risk:** Low — same semantics, just relocated

---

### 2.5 `code_semantic_search`

**File:** `skills/code_semantic_search/main.go`

The timeout is in `search()` (called from `run()`), not directly in `run()`. The conversion requires moving it up to `main()`:

**Before:** `searchCtx, cancel := context.WithTimeout(ctx, DefaultTotalTimeout)` inside `search()` helper

**After:**
```go
func main() {
    skillmain.Main(Command, skillmain.Chain(run,
        skillmain.WithTimeout[Input](DefaultTotalTimeout),
        skillmain.WithRecover[Input](),
    ))
}
```

Remove the `searchCtx, cancel := context.WithTimeout(ctx, DefaultTotalTimeout)` and `defer cancel()` from `search()`, and rename `searchCtx` → `ctx` throughout the function.

**Risk:** Medium — largest skill (~2800 lines), need to verify `searchCtx` is only used for the timeout (not for a separate scope). Grep `searchCtx` to confirm it's a straight alias of the timeout-wrapped context.

---

## Phase 3: Unit Tests

**File:** `internal/adapters/skillslib/skillmain/middleware_test.go` (new)

### Tests to write

| Test | What it verifies |
|------|-----------------|
| `TestChain_OrderMatters` | Outermost middleware executes first on entry, last on exit |
| `TestChain_Empty` | `Chain(run)` with no middleware is identity |
| `TestWithTimeout_Expires` | Context cancels after duration; `run` receives canceled ctx |
| `TestWithTimeout_Completes` | Fast run completes normally |
| `TestWithDynamicTimeout_Zero` | Returns 0 → no timeout applied |
| `TestWithDynamicTimeout_FromInput` | Duration from input applied correctly |
| `TestWithRecover_NoPanic` | Normal execution passes through |
| `TestWithRecover_CatchesPanic` | Panic converted to `"skill panicked: ..."` error |
| `TestWithSkillStep_LogsStartAndEnd` | Step start/end logged with duration |
| `TestWithSkillStep_LogsError` | Failed step logged with error |
| `TestWithRetry_SucceedsFirst` | No retry on success |
| `TestWithRetry_RetriesOnError` | Retries up to MaxAttempts |
| `TestWithRetry_NotRetryable` | Non-retryable error returns immediately |
| `TestWithRetry_RespectsContext` | Cancelled context stops retry loop |
| `TestWithInputLog_LogsSummary` | Summary fields appear in log output |

### Test helper

```go
type testInput struct {
    Timeout int
    Query   string
}

func noopRun(_ context.Context, _ *RunContext, _ testInput) error {
    return nil
}

func failRun(_ context.Context, _ *RunContext, _ testInput) error {
    return errors.New("fail")
}
```

Use `zerolog.New(zerolog.NewTestWriter(t))` for log assertions and a test `RunContext` with `Now: func() time.Time` set to a controllable clock for `Step` timing tests.

**Dependencies:** Phase 1 (new middleware must exist first)
**Risk:** Low — pure test additions

---

## Implementation Order

```
Phase 1.1  WithDynamicTimeout     (middleware.go)
Phase 1.2  WithRetry              (middleware.go)
Phase 1.3  WithInputLog           (middleware.go)
      ↓
Phase 2.1  code_counsel           (simplest, proves pattern)
Phase 2.2  calibration_generate   (long timeout)
Phase 2.3  session_summarize      (long timeout)
Phase 2.4  lsp_gopls              (dynamic timeout — exercises 1.1)
Phase 2.5  code_semantic_search   (largest skill — exercises 1.1)
      ↓
Phase 3    Unit tests             (middleware_test.go)
```

### Verification after each phase

```bash
make check          # fmt, lint, vet, test, coverage, build
make test-race      # Race detection
```

---

## Out of Scope

- **Adopting Chain in all 126 skills** — this plan covers 5 representative skills. Broader adoption can follow once the pattern is validated.
- **Replacing inner per-request timeouts** — HTTP call timeouts (60s, 90s) and CLI execution timeouts (2min) inside helper functions stay as-is. Only top-level `run()` timeouts move to middleware.
- **New middleware beyond the 3 listed** — e.g., rate limiting, circuit breaker middleware, dry-run middleware. These can be planned separately.
- **Refactoring manual retry loops** into `WithRetry` — the retry middleware is defined here but adoption in skills like `session_summarize.summarizeWithFallback` is a follow-up.
