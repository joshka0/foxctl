# Phase 5.1 — Auto-Cache Implementation

**Date:** 2024-11-30\
**PR:** #89\
**Branch:** `codex/phase5-1-auto-cache`

---

## Summary

Implemented Phase 5.1 of the foxctl spec, focusing on deterministic
memoization and proper error handling for the auto-cache system.

---

## Changes

### New Error Codes

- `ECACHE_MISS` — cache-only mode with no cached result
- `ECACHE_UNAVAILABLE` — cache storage unavailable

### Cache Mode Behavior

| Mode   | Behavior                                                                         |
| ------ | -------------------------------------------------------------------------------- |
| `auto` | Read-through + write-through. Cache errors are **non-fatal** (log and continue). |
| `off`  | Skip all cache I/O.                                                              |
| `only` | Emit `ECACHE_MISS` error envelope on miss.                                       |

### Cache Hit Annotation

On cache hit, envelope includes:

- `meta.source = "cache"`
- `meta.cache_key = "<cache_key>"`

---

## Files Changed

### Code

- `internal/protocol/errors.go` — Add `ECACHE_MISS`, `ECACHE_UNAVAILABLE`
- `internal/runtime/runservice/cache.go` — Non-fatal errors in auto mode, proper
  envelope in only mode
- `internal/storage/cache/store_test.go` — BuildKey variation tests

### Tests

- `internal/runtime/runservice/executor_test.go` — Cache mode tests
- `test/integration/cache_test.go` — Full integration tests for hit/miss
  scenarios

### Documentation

- `docs/spec/phase5_1_auto_cache.md` — Detailed spec for Phase 5.1
- `docs/spec/core_profile_v1.md` — Updated sections 8.2, 8.3, 13

---

## Test Coverage

### Unit Tests (new)

- `TestBuildKey_VariesOnSkillName`
- `TestBuildKey_VariesOnVersion`
- `TestBuildKey_VariesOnArgs`
- `TestBuildKey_VariesOnDigests`
- `TestBuildKey_EmptyInput`
- `TestExecutorTryServeCacheModeOnlyMiss`
- `TestExecutorTryServeCacheAutoModeErrorsNonFatal`

### Integration Tests (new)

- `TestCacheHitSameInput`
- `TestCacheMissDifferentInput`
- `TestCacheModeOff`
- `TestCacheModeOnlyMiss`
- `TestCacheKeyDeterminism`

---

## Acceptance Criteria

- [x] Same skill + args + input digests → same cache key (deterministic)
- [x] Second identical run returns `meta.source:"cache"` + `meta.cache_key`
- [x] `--cache=off` skips all cache I/O
- [x] `--cache=only` on miss returns error envelope with `ECACHE_MISS`
- [x] Cache errors in `auto` mode don't fail the run
- [x] All unit and integration tests pass
