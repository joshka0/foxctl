# Phase 5.2 — Named Memory & CLI UX

**Date:** 2024-11-30\
**PR:** #89 (continuation)\
**Branch:** `codex/phase5-1-auto-cache`

---

## Summary

Implemented Phase 5.2 of the foxctl spec, focusing on named memory ergonomics
and proper error handling for memory CLI commands.

---

## Changes

### Error Handling Improvements

Memory commands now emit proper error envelopes instead of returning raw errors:

| Command         | Error Case   | Envelope              |
| --------------- | ------------ | --------------------- |
| `memory get`    | Not found    | `ENOTFOUND` with hint |
| `memory delete` | Not found    | `ENOTFOUND` with hint |
| `memory update` | Not found    | `ENOTFOUND` with hint |
| `memory update` | Missing args | `EARG` with hint      |

### New Helpers

- `memorycmd.WriteNotFound()` — emits `ENOTFOUND` envelope with name, workspace,
  hint
- `memorycmd.WriteArgError()` — emits `EARG` envelope with message and hint

### CLI Enhancements

- `memory delete` response now includes `deleted_count` field

---

## Files Changed

### Code

- `cmd/foxctl/cmd/memorycmd/helper.go` — Add error envelope helpers
- `cmd/foxctl/cmd/memory_named.go` — Use error envelopes for not found/invalid
  args

### Tests

- `cmd/foxctl/cmd/memory_test.go` — 5 new tests for error cases

### Documentation

- `docs/spec/phase5_2_named_memory.md` — Full Phase 5.2 spec
- `docs/spec/core_profile_v1.md` — Expanded section 12 (Memory)

---

## Test Coverage

### New Tests

| Test                          | Description                           |
| ----------------------------- | ------------------------------------- |
| `TestMemoryGetNotFound`       | ENOTFOUND envelope with hint and name |
| `TestMemoryDeleteNotFound`    | ENOTFOUND envelope                    |
| `TestMemoryUpdateNotFound`    | ENOTFOUND envelope                    |
| `TestMemoryUpdateMissingArgs` | EARG envelope                         |
| `TestMemoryDeleteSuccess`     | Successful delete with deleted_count  |

---

## Acceptance Criteria

- [x] `UNIQUE(name, workspace)` enforced at DB level
- [x] `foxctl run … --remember foo` creates named memory
- [x] `foxctl memory get foo` replays same envelope as original result
- [x] `foxctl memory list` shows recently updated memories
- [x] `foxctl memory relevant` returns high-value memories
- [x] `memory get <missing>` → `ENOTFOUND` with hint
- [x] `memory update` without flags → `EARG` envelope
- [x] Section "Memory" in `core_profile_v1.md` updated

---

## What's Next

Phase 5 complete. Next phases:

- **Phase 6:** OpenAPI Tier-1 skill (generic)
- **Phase 7:** Plugin SPI (auth & pagination)
