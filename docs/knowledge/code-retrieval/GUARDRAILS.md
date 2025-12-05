# Retrieval Funnel Guardrails

## Overview

This document describes when to use—and when NOT to use—each tool in the
retrieval funnel. Following these guardrails prevents common mistakes and
improves efficiency.

---

## code.symbol_search Guardrails

### Use When

- Starting a new code understanding task
- Finding functions/methods by intent
- Building candidate list for swe_grep
- Exploring unfamiliar codebase

### Do NOT Use When

| Situation                | Better Alternative             |
| ------------------------ | ------------------------------ |
| Known exact pattern      | `code.search` (ripgrep)        |
| Need file content        | `fs.read_file`                 |
| Symbol index unavailable | `code.search` fallback         |
| Searching non-code files | `code.search` or `fs.list_dir` |

### Anti-Patterns

```
❌ Calling symbol_search repeatedly with same question
❌ Ignoring low-score results (they may be relevant)
❌ Using without workspace_id
❌ Skipping to grep without checking symbol index first
```

---

## code.swe_grep Guardrails

### Use When

- Have candidate files from symbol_search
- Need code snippets, not full files
- Preparing context for edits
- Multiple files to search

### Do NOT Use When

| Situation          | Better Alternative             |
| ------------------ | ------------------------------ |
| No candidates yet  | Run `code.symbol_search` first |
| Need entire file   | `fs.read_file`                 |
| Very simple lookup | `code.search` may be faster    |
| Files > 10MB       | May hit resource limits        |

### Anti-Patterns

```
❌ Calling without candidate_files
❌ Too many candidates (>20 files)
❌ Empty candidate paths
❌ Skipping symbol_id when available
```

---

## code.search (ripgrep) Guardrails

### Use When

- Known regex pattern
- Searching for literal strings
- Grep-style pattern matching
- Quick content search

### Do NOT Use When

| Situation              | Better Alternative            |
| ---------------------- | ----------------------------- |
| Semantic/intent search | `code.symbol_search`          |
| Need ranked results    | `code.symbol_search`          |
| Want code snippets     | `code.swe_grep`               |
| Binary files           | Skip or use specialized tools |

### Anti-Patterns

```
❌ Overly broad patterns matching too much
❌ Not limiting max_results
❌ Searching in node_modules/vendor
❌ Using when symbol search would be more precise
```

---

## edit.apply_patch Guardrails

### Use When

- Single text replacement
- Simple function signature change
- Adding/removing a few lines
- Quick fixes

### Do NOT Use When

| Situation                       | Better Alternative           |
| ------------------------------- | ---------------------------- |
| Multi-location changes          | `edit.apply_structured_diff` |
| Complex refactors               | `edit.apply_structured_diff` |
| old_text appears multiple times | Be more specific or use diff |
| Creating new file               | `edit.create_file`           |

### Anti-Patterns

```
❌ old_text doesn't match exactly (whitespace matters!)
❌ Calling multiple times when structured diff is cleaner
❌ Not reading file first to verify content
❌ Partial line matches (always include full lines)
```

---

## edit.apply_structured_diff Guardrails

### Use When

- Multiple hunks in same file
- Complex refactors
- Diffs from code/diff skill
- Changes spanning multiple locations

### Do NOT Use When

| Situation                 | Better Alternative        |
| ------------------------- | ------------------------- |
| Simple single-line change | `edit.apply_patch`        |
| Creating new file         | `edit.create_file`        |
| Unsure about changes      | Use `dry_run: true` first |

### Anti-Patterns

```
❌ Incorrect old_lines/new_lines counts
❌ Missing context lines for verification
❌ Wrong line prefix (space vs - vs +)
❌ Not using dry_run for risky changes
```

---

## Funnel Flow Guardrails

### Correct Flow

```
1. symbol_search → Get candidates with scores
2. swe_grep → Extract relevant snippets
3. read_file → Full context if needed
4. apply_patch OR apply_structured_diff → Make the edit
5. tests.run → Verify the change
```

### Common Mistakes

| Mistake                  | Fix                               |
| ------------------------ | --------------------------------- |
| Skipping symbol_search   | Always start with semantic search |
| Going straight to edit   | Read context first                |
| Not verifying after edit | Run tests                         |
| Over-relying on ripgrep  | Symbol search is more precise     |

### Resource Limits

| Resource                  | Limit       | Impact                    |
| ------------------------- | ----------- | ------------------------- |
| symbol_search max_results | 20 default  | May miss relevant symbols |
| swe_grep candidates       | ~20 files   | Performance degrades      |
| File size                 | 1MB default | Large files may truncate  |
| Diff hunks                | Reasonable  | Very large diffs may fail |

---

## Summary Decision Matrix

| Goal                | First Tool           | Then                         |
| ------------------- | -------------------- | ---------------------------- |
| Find code by intent | `code.symbol_search` | `code.swe_grep`              |
| Find by pattern     | `code.search`        | `fs.read_file`               |
| Simple edit         | Read file            | `edit.apply_patch`           |
| Complex refactor    | Read file            | `edit.apply_structured_diff` |
| Explore codebase    | `fs.list_dir`        | `code.symbol_search`         |
