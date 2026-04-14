# Phase 5.2 — Named Memory & CLI UX

**Status:** In Progress\
**Last Updated:** 2024-11-30

---

## 1. Scope & Goals

- **Goal:** Make named memories a first-class, ergonomic feature:
  - Stable _storage model_ for named memories.
  - Predictable _CLI UX_ around `foxctl memory …`.
  - Clear _integration_ with `foxctl run --remember …`.
- **Non-goals (for later phases):**
  - Vector/semantic search over memories (embedding column exists but is
    optional).
  - GC policies and CAS compaction.
  - Any change to the JSON envelope wire contract.

---

## 2. Data Model & Invariants

### 2.1 Named Memory Entry

Backed by `internal/storage/memory.Store` (`named_memory` table):

| Field           | Type        | Description                                              |
| --------------- | ----------- | -------------------------------------------------------- |
| `id`            | `string`    | UUID primary key                                         |
| `name`          | `string`    | User/key name; unique per workspace                      |
| `type`          | `string`    | E.g. `result`, `plan`, `spec`, `error`; default `result` |
| `workspace`     | `string`    | Normalized path (see §3)                                 |
| `summary`       | `string`    | Short human-oriented summary                             |
| `result`        | `[]byte`    | Full JSON envelope from skill run                        |
| `digests`       | `[]string`  | CAS digests referenced by `result`                       |
| `created_at`    | `time.Time` | Immutable once set                                       |
| `updated_at`    | `time.Time` | Updated on write                                         |
| `last_accessed` | `time.Time` | Updated on read                                          |
| `access_count`  | `int`       | Incremented on read                                      |
| `embedding`     | `[]byte`    | Reserved for future vector search                        |

### 2.2 Invariants

1. **Uniqueness:** `(name, workspace)` MUST be unique.
2. **Durability:** Named memories have **no TTL**; persistent until deleted.
3. **CAS integrity:**
   - On `Save`/`SaveResult`: digests MUST be pinned via `artifacts.Manager.Pin`.
   - On `Delete`/`DeleteByNamePrefix`: digests MUST be unpinned.
4. **Timestamps:**
   - `created_at` immutable once set.
   - `updated_at` and `last_accessed` updated on write and read respectively.
5. **Types:**
   - `type` MUST default to `"result"` when empty.
   - System MUST NOT silently coerce non-empty types.

---

## 3. Workspace Model

### 3.1 Scoping

All named memory operations are scoped by `workspace` string.

### 3.2 Detection (CLI + runservice)

```
if --workspace flag given:
    workspace = workspace.Normalize(flags.Workspace)
else if cfg.Memory.AutoLoadWorkspace:
    workspace = workspace.Detect("")  // repository root
else:
    workspace = os.Getwd()
```

### 3.3 Behavior

Memory operations MUST NOT silently fall back to another workspace if none is
found.

---

## 4. Creation Flows

### 4.1 `foxctl run --remember`

Primary creation path via `runservice.remember.go`.

**CLI flags:**

- `--remember <name>` → `RunOptions.RememberName`
- `--remember-type <type>` (default `result`)
- `--remember-summary <summary>` (optional)

**Name normalization:**

- Input `"memory:foo/bar"` → stored `name="foo/bar"`.

**When to save:**

- MUST save _final result envelope_ regardless of `status`.
- Call flow:
  1. Skill execution completes.
  2. Cache persistence (Phase 5.1) runs.
  3. `remember(result)` invoked if `RememberName` non-empty.
  4. Opens `memory.Store` at `cfg.Paths.Cache`, `cfg.Paths.CAS`.
  5. Summary: use `RememberSummary` if set, else
     `protocol.SummarizeForMemoryBytes(result)`.
  6. Persist via `Store.SaveResult(SaveOptions{…})`.

**Invariants:**

- Saving MUST NOT mutate the envelope body.
- `digests` derived via `cache.CollectDigests(result)` and pinned.
- On failure: run MUST still succeed; error MAY be logged to stderr.

### 4.2 Direct CLI Save / Put

- `memory save`: Load envelope from `--from-digest` or stdin, save as named
  memory.
- `memory put`: Write memory given name, type, summary, and payload.

---

## 5. Retrieval & Listing Flows

### 5.1 `memory get`

**Inputs:**

- `name` (required)
- `--workspace` (optional)

**Behavior:**

- Fetch via `Store.Get`.
- On success: write `entry.Result` envelope verbatim to stdout.
- On `ErrNotFound`: emit `ENOTFOUND` error envelope with hint.

### 5.2 `memory list` / `memory recent`

**`memory list`:**

- Uses `Store.List(workspace, limit)`.
- Sorted by `updated_at DESC`.
- Outputs envelope with `data.entries: []`.

**`memory recent`:**

- Sugar for `list` with small limit (default 10).

### 5.3 `memory search`

- Uses `Store.Search(workspace, query, limit)`.
- Case-insensitive substring match on `name` and `summary`.
- Returns `data.results: []` of `ScoredEntry`.

### 5.4 `memory relevant`

- Uses `Store.Relevant(workspace, limit)`.
- Scores by recency + frequency via `scoreEntry`.
- Default limit 10.

---

## 6. Mutation Flows

### 6.1 `memory update`

**Inputs:**

- `name` (required)
- `--workspace`, `--summary`, `--type` (optional)

**Behavior:**

- Call `Store.Update(name, workspace, summaryPtr, typePtr)`.
- `UpdatedAt` refreshed; `LastAccess` unchanged.
- Response: envelope with `data.entry`.

### 6.2 `memory delete`

**Inputs:**

- `name` (required)
- `--workspace` (optional)
- `--prefix` (optional; use `DeleteByNamePrefix`)

**Behavior:**

- If `--prefix`: delete all with prefix, return `data.deleted_count`.
- Else: delete single entry, return `data.deleted_count = 1`.
- Digests MUST be unpinned.

---

## 7. Error Handling

| Condition    | Error Code  | Notes                              |
| ------------ | ----------- | ---------------------------------- |
| Not found    | `ENOTFOUND` | Include workspace and name in hint |
| DB/IO errors | `EIO`       | Include cause in data              |
| Invalid name | `EARG`      | Empty or whitespace-only           |

**Remember path (runservice):**

- Memory failures MUST NOT alter primary run envelope.
- Failure MAY be logged to stderr.

---

## 8. Test Plan

### 8.1 Store Tests (unit)

| Test                     | Description                                             |
| ------------------------ | ------------------------------------------------------- |
| `TestSaveGetRoundTrip`   | Save entry with digests → Get → fields match            |
| `TestListOrdering`       | Multiple entries; verify `updated_at DESC`              |
| `TestSearch`             | Mixed matches; verify case-insensitive; limit respected |
| `TestRelevant`           | Vary access patterns; verify scoring order              |
| `TestUpdate`             | Update summary only; ensure type preserved              |
| `TestDelete`             | Entries removed; digests unpinned                       |
| `TestDeleteByNamePrefix` | Prefix delete; digests unpinned                         |

### 8.2 CLI / Runservice Tests

| Test                               | Description                            |
| ---------------------------------- | -------------------------------------- |
| `TestExecutorRememberStoresMemory` | Existing baseline                      |
| `TestRememberWithCustomSummary`    | `--remember-summary` used              |
| `TestRememberWithCustomType`       | `--remember-type=plan`                 |
| `TestMemoryGetReturnsEnvelope`     | `memory get` returns original envelope |
| `TestMemoryGetNotFound`            | `ENOTFOUND` with hint                  |
| `TestMemoryDeleteRemovesEntry`     | Delete works; handles not found        |

---

## 9. Acceptance Criteria

- [ ] `UNIQUE(name, workspace)` enforced at DB level and via tests.
- [ ] `foxctl run … --remember foo` creates named memory in inferred
      workspace.
- [ ] Omitted `--remember-summary` uses `protocol.SummarizeForMemoryBytes`.
- [ ] `foxctl memory get foo` replays same envelope as original result.
- [ ] `foxctl memory list` shows recently updated memories.
- [ ] `foxctl memory relevant` returns high-value memories.
- [ ] `memory get <missing>` → `ENOTFOUND` with hint.
- [ ] `run --remember` still succeeds when memory DB unavailable.
- [ ] Section "Memory" in `core_profile_v1.md` updated.

---

## 10. Implementation Order

1. **Audit existing code** against spec (store, CLI commands, runservice).
2. **Add missing tests** for store operations.
3. **Fix error handling** (proper envelopes for `ENOTFOUND`, `EARG`).
4. **Add CLI tests** for `memory get/list/delete`.
5. **Update `core_profile_v1.md`** with Memory section.
6. **Create changelog** entry.
