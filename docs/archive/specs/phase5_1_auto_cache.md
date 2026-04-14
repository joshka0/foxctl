# Phase 5.1 — Auto-Cache & Cache Keys

**Status:** Disabled (cache is currently off-only)\
**Last Updated:** 2025-11-30

---

## 1. Summary

This document specifies the auto-cache system for `foxctl run`, including
cache key computation, read/write semantics, and envelope annotation on hits.

---

## 2. Cache Key Formula

### Spec

Cache key = `sha256:` + hex of:

```text
skill_name || NUL || skill_version || NUL || RFC8785(args_json) || (NUL || digest)*
```

Where:

- `skill_name`: `manifest.Metadata.Name`
- `skill_version`: `manifest.Metadata.Version`
- `args_json`: skill input JSON, canonicalized via RFC 8785 (deterministic)
- `digests`: sorted `sha256:<hex>` strings for CAS inputs (optional)

### Implementation Status

**`internal/storage/cache/store.go:BuildKey`** ✅ **Matches spec**

```go
func BuildKey(manifest skill.Manifest, input []byte, extraDigests []string) (string, error)
```

- Uses `canonicaljson.Marshal` (RFC 8785) for args ✅
- Sorts digests lexicographically ✅
- Uses NUL separators between components ✅
- Returns `sha256:` + hex ✅

### Test Coverage

**`internal/storage/cache/store_test.go:TestBuildKeyDeterministic`** ✅ Exists

- Verifies reordered JSON keys produce same key
- Verifies reordered digests produce same key

**Missing tests:**

- [ ] `TestBuildKey_VariesOnSkillName`
- [ ] `TestBuildKey_VariesOnVersion`
- [ ] `TestBuildKey_VariesOnArgs`
- [ ] `TestBuildKey_VariesOnDigests`
- [ ] `TestBuildKey_EmptyInput` (edge case: empty `{}` vs `""`)

---

## 3. Cache Modes

Cache is currently disabled in the reference implementation. Only `off` is
supported.

### Spec

| Mode   | Behavior                                    |
| ------ | ------------------------------------------- |
| `auto` | Read-through + write-through (default)      |
| `off`  | Skip cache entirely (no read, no write)     |
| `only` | Read-only; fail with error envelope on miss |

### Implementation Status

**`internal/storage/cache/store.go`** ✅ Defines `ModeAuto`, `ModeOff`,
`ModeOnly`

**`internal/runtime/runservice/cache.go:TryServeCache`** ✅ Implements read path

- Skips lookup if `Async` or `CacheMode == ModeOff` ✅
- Returns cached entry with annotation on hit ✅
- Returns error (simple `fmt.Errorf`) on miss when `ModeOnly` ⚠️ (see gap below)

**`internal/runtime/runservice/result.go:HandleResult`** → **`PersistCache`** ✅
Implements write path

- Only writes if `ModeAuto` ✅
- Collects digests from envelope ✅
- Pins digests via artifact manager ✅

### Gaps

1. **`ModeOnly` error format**: Currently returns
   `fmt.Errorf("cache miss for key %s", key)`.
   - **Should be:** A proper error envelope with `error.code = "ECACHE_MISS"`
     and `data.hint`.
   - **Fix:** Return structured error that can be caught and converted to
     envelope by caller, or emit envelope directly.

---

## 4. Envelope Annotation on Cache Hit

### Spec

On cache hit, the returned envelope must have:

- `meta.source = "cache"`
- `meta.cache_key = <cache_key>`
- All other fields unchanged from original result

### Implementation Status

**`internal/storage/cache/store.go:AnnotateHit`** ✅ Implements correctly

```go
env.Meta.Source = "cache"
env.Meta.CacheKey = cacheKey
```

Also sets `Meta.Workspace` and `Meta.SkillVer` if provided ✅

### No gaps here.

---

## 5. CAS Integration

### Spec

- Cached envelopes may reference CAS artifacts via `data.artifact` / optional
  `meta.cas_digest` (if set MUST match `data.artifact`).
- On cache hit, these references remain valid.
- Cache store pins artifacts on `Put`, unpins on `Delete`/expiry.

### Implementation Status

**`internal/storage/cache/store.go`**

- `Put` calls `pinDigests(ctx, entry.Digests)` ✅
- `Delete` calls `unpinDigests(ctx, digests)` ✅
- `evictExpired` unpins digests before deleting rows ✅
- Digests collected via `artifacts.Digests(result)` ✅

### Test Coverage

- Basic put/get/eviction tested ✅
- **Missing:** Integration test verifying CAS artifact remains accessible after
  cache hit

---

## 6. Runtime Flow

### Current Flow (working correctly)

```
cmd/run.go
├── buildRunOptions() → RunOptions with CacheMode
├── runservice.NewExecutor()
├── executor.TryServeCache(input)           // READ
│   ├── cache.BuildKey()
│   ├── cacheStore.Get()
│   │   └── on hit: AnnotateHitBytes() + write to stdout + return (true, nil)
│   │   └── on miss + ModeOnly: return (false, error)
│   │   └── on miss + ModeAuto: return (false, nil)
│   └── stores cacheStore + cacheKey on Executor
├── executor.PrepareJob()
├── executor.ExecuteSync()
│   └── jobStore.ExecutePreparedSkill()
│       └── returns result bytes
├── executor.HandleResult(jobID, result)    // WRITE
│   └── PersistCache(annotated)
│       └── cacheStore.Put(entry)
└── return
```

### Identified Issues

1. **Cache key not recomputed if TryServeCache misses and input changes**
   - Currently: key is computed in `TryServeCache` and stored on `Executor`.
   - The same key is reused in `PersistCache`.
   - This is **correct** as long as input doesn't change between `TryServeCache`
     and skill execution (which it shouldn't).
   - ✅ No issue.

2. **Cache store opened lazily in TryServeCache**
   - Currently only opens cache store when `TryServeCache` is called.
   - If `CacheMode == ModeOff`, store is never opened (correct).
   - If `CacheMode == ModeAuto` and `TryServeCache` misses, store remains open
     for `PersistCache` (correct).
   - ✅ No issue.

3. **Error handling in TryServeCache**
   - On cache store open error: returns error (stops run) ⚠️
   - **Spec says:** Cache errors in `auto` mode should log and continue (treat
     as miss).
   - **Fix needed:** Wrap cache errors in non-fatal wrapper in `auto` mode.

---

## 7. Test Plan for 5.1

### Unit Tests (to add/extend)

```
internal/storage/cache/store_test.go
├── TestBuildKey_Deterministic           ✅ exists
├── TestBuildKey_VariesOnSkillName       ❌ missing
├── TestBuildKey_VariesOnVersion         ❌ missing
├── TestBuildKey_VariesOnArgs            ❌ missing
├── TestBuildKey_VariesOnDigests         ❌ missing
├── TestBuildKey_EmptyInput              ❌ missing
├── TestStorePutAndGet                   ✅ exists
├── TestStoreEvictsExpired               ✅ exists
├── TestStore_TTL_Expiry                 ✅ exists (same as above)
└── TestStore_PinUnpinDigests            ❌ missing (CAS integration)
```

### Integration/Golden Tests (to add)

```
test/integration/cache_hit_test.go (or similar)
├── TestCacheHit_SameInputProducesHit
│   - Run skill twice with identical input
│   - Assert: 2nd run has meta.source="cache", meta.cache_key matches
│   - Assert: data identical; if set, meta.cas_digest identical (if CAS-backed)
├── TestCacheMiss_DifferentInputProducesMiss
│   - Run skill twice with different input
│   - Assert: both runs execute (no meta.source="cache")
├── TestCacheMode_Off
│   - Run skill twice with --cache=off
│   - Assert: neither run has meta.source="cache"
├── TestCacheMode_Only_Miss
│   - Run with --cache=only before any cache exists
│   - Assert: error envelope with ECACHE_MISS
└── TestCacheHit_CASArtifactValid
    - Run skill that produces CAS artifact
    - Cache hit should still have valid CAS reference
```

---

## 8. Gaps Summary

| # | Gap                                                          | Severity | Fix                                  |
| - | ------------------------------------------------------------ | -------- | ------------------------------------ |
| 1 | `ModeOnly` miss returns plain error, not structured envelope | Medium   | Emit `ECACHE_MISS` envelope          |
| 2 | Cache open errors in `auto` mode are fatal                   | Low      | Log and treat as miss in `auto` mode |
| 3 | Missing unit tests for key variation                         | Low      | Add tests                            |
| 4 | Missing integration test for cache hit flow                  | Medium   | Add test                             |
| 5 | Missing integration test for CAS + cache                     | Low      | Add test                             |

---

## 9. Implementation Order

1. **Add missing unit tests** for `BuildKey` variations (quick win, increases
   confidence)
2. **Fix `ModeOnly` error format** to emit proper envelope
3. **Make cache errors non-fatal in `auto` mode** (log + continue)
4. **Add integration tests** for cache hit/miss scenarios
5. **Add CAS integration test** (optional, can defer to 5.2)
6. **Document** in `docs/spec/core_profile_v1.md` (section on caching)

---

## 10. Acceptance Criteria

- [ ] Same skill + args + input digests → same cache key (deterministic)
- [ ] Second identical run returns `meta.source:"cache"` + `meta.cache_key`
- [ ] `--cache=off` skips all cache I/O
- [ ] `--cache=only` on miss returns error envelope with `ECACHE_MISS`
- [ ] Cache errors in `auto` mode don't fail the run
- [ ] CAS artifacts remain valid after cache hit
- [ ] All unit and integration tests pass
