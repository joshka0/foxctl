---
name: code-retrieval
description: Code retrieval funnel for intelligent code search, context extraction, and editing
---

# Code Retrieval Funnel

## Purpose

This skill describes the retrieval funnel for finding and editing code in
agentctl. The funnel provides a structured approach to code discovery that
balances breadth (semantic search) with precision (symbol-aware extraction).

## When to Use This Skill

Auto-activates when:

- Searching for code to understand or modify
- Finding functions, methods, or classes by intent
- Extracting code context for editing
- Planning code changes across multiple files

## The Retrieval Funnel

```
┌─────────────────────────────────────────────────────────────┐
│                    RETRIEVAL FUNNEL                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. SEMANTIC FILE INDEX (optional, if available)            │
│     └─ Broad file-level search by embedding similarity      │
│        Input: natural language question                     │
│        Output: ranked list of candidate files               │
│                                                             │
│  2. SYMBOL SEARCH (code.symbol_search)                      │
│     └─ Symbol-aware search using indexed AST data           │
│        Input: question + optional symbol_hint               │
│        Output: ranked symbols with file paths, kinds        │
│                                                             │
│  3. SWE GREP (code.swe_grep)                                │
│     └─ Extract high-signal snippets from candidates         │
│        Input: question + candidate_files from step 2        │
│        Output: relevant code snippets with line ranges      │
│                                                             │
│  4. STRUCTURED EDIT                                         │
│     ├─ edit.apply_patch: simple text replacement            │
│     └─ edit.apply_structured_diff: multi-hunk diffs         │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Quick Reference

### Tool Selection Guide

| Task                  | Tool                         | When to Use                             |
| --------------------- | ---------------------------- | --------------------------------------- |
| Find relevant symbols | `code.symbol_search`         | Starting point for any code task        |
| Extract code context  | `code.swe_grep`              | After symbol search, before editing     |
| Pattern matching      | `code.search`                | Known regex patterns, grep-style search |
| Simple edits          | `edit.apply_patch`           | Single-location text replacement        |
| Complex refactors     | `edit.apply_structured_diff` | Multi-hunk changes, structured diffs    |

### Typical Workflow

```
1. code.symbol_search(question="How does user authentication work?")
   → Returns: [{file: "auth/login.go", symbol_id: "Login", kind: "function"}]

2. code.swe_grep(question="authentication flow", candidate_files=[...])
   → Returns: [{file: "auth/login.go", start_line: 10, end_line: 45, preview: "..."}]

3. fs.read_file(path="auth/login.go")
   → Full file content for detailed analysis

4. edit.apply_patch(path="auth/login.go", old_text="...", new_text="...")
   → Apply the change
```

## Resource Files

- [SYMBOL_SEARCH.md](SYMBOL_SEARCH.md) - code.symbol_search tool details
- [SWE_GREP.md](SWE_GREP.md) - code.swe_grep tool details
- [EDIT_TOOLS.md](EDIT_TOOLS.md) - Edit tool comparison and usage
- [GUARDRAILS.md](GUARDRAILS.md) - When NOT to use each tool
