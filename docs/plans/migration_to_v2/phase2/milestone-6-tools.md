# Milestone 6: Smarter Tools (Compound Tools)

**Status:** ~50% Complete

## Overview

Reduce tool spam by providing compound tools that do multiple operations safely. Agents shouldn't need 10 reads + 5 edits when a single tool can do it.

---

## PR 6.1 — fs.apply_patchset (Atomic Multi-Edit)

**Status:** ⚠️ Partial

### Current Implementation

| Component | Location | Status |
|-----------|----------|--------|
| fs/apply_edit | `skills/fs_apply_edit/` | ✅ Done (fuzzy matching) |
| text/replace | `skills/text_replace/` | ✅ Done |
| code/smart_write | `skills/code_smart_write/` | ✅ Done |

### Gap

Current tools work file-by-file. Need true unified diff patchset support.

### Required Tool

`fs.apply_patchset`

```json
{
  "patches": [
    {"file_path": "src/main.go", "unified_diff": "..."},
    {"file_path": "src/util.go", "unified_diff": "..."}
  ],
  "require_reservation": true
}
```

### Behavior

- All-or-nothing application
- Rejects if any patch fails
- Rejects if reservation missing

### Remaining Work

- [ ] Create `skills/fs_patchset/` skill
- [ ] Implement unified diff parsing
- [ ] Implement atomic apply with rollback
- [ ] Integrate with reservation system

---

## PR 6.2 — code.context_bundle (One-Call Context Gathering)

**Status:** ⚠️ Partial

### Current Implementation

| Component | Location | Status |
|-----------|----------|--------|
| code/context_ripgrep | `skills/code_context_ripgrep/` | ✅ Done |
| code/swe_grep | `skills/code_swe_grep/` | ✅ Done |
| code/semantic_search | `skills/code_semantic_search/` | ✅ Done |
| session/recall | `skills/session_recall/` | ✅ Done |
| context/filter | `skills/context_filter/` | ✅ Done |

### Gap

These are separate tools. Need single unified tool.

### Required Tool

`code.context_bundle`

```json
{
  "query": "authentication middleware",
  "workspace_scope": "src/",
  "max_files": 10,
  "include_symbols": true,
  "include_related_sessions": true,
  "include_related_tasks": true
}
```

### Output

Bounded "bundle" ready to paste into prompt:
- Ripgrep blocks
- Symbol index matches
- Session recall snippets
- Task embeddings (if available)

### Remaining Work

- [ ] Create `skills/code_context_bundle/` skill
- [ ] Orchestrate existing skills internally
- [ ] Implement smart deduplication
- [ ] Add token budget enforcement

---

## PR 6.3 — code.search_and_edit (Safe Refactor)

**Status:** ⚠️ Partial

### Current Implementation

| Component | Location | Status |
|-----------|----------|--------|
| text/replace | `skills/text_replace/` | ✅ Done |
| code/smart_write | `skills/code_smart_write/` | ✅ Done |

### Gap

Search and replace are separate operations.

### Required Tool

`code.search_and_edit`

```json
{
  "pattern": "oldFunction",
  "replacement": "newFunction",
  "paths": ["src/"],
  "dry_run": false
}
```

### Output

- Diff preview
- Applied digest (CAS backup)
- Requires reservations for touched files

### Remaining Work

- [ ] Create `skills/code_search_edit/` skill
- [ ] Combine search + edit in single atomic operation
- [ ] Integrate reservation checking
- [ ] Add CAS backup before modification

---

## PR 6.4 — Tool Availability Policy

**Status:** ⚠️ Partial

### Current Implementation

| Component | Location | Status |
|-----------|----------|--------|
| hooks/task_guard | `skills/hooks_task_guard/` | ✅ Done |
| hooks/file_guard | `skills/hooks_file_guard/` | ✅ Done |

### Gap

Guards block operations but don't hide tools from the model.

### Policy Rules (Hard)

**Default tools exposed:** read/search/query only

**Write tools exposed only if:**
1. Active task exists
2. Reservations acquired
3. Hooks allow it

### Remaining Work

- [ ] Implement dynamic tool list filtering
- [ ] Filter based on task state
- [ ] Filter based on reservation state
- [ ] Test tool spam reduction

---

## Acceptance Criteria

- [ ] Multi-file changes in one validated patchset call
- [ ] Coding agent starts with 1 context call instead of 5-15
- [ ] Common refactors become one tool call with safety
- [ ] Tool spam drops; destructive tools hidden unless conditions met
