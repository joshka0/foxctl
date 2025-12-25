---
name: agentctl Search
description: Fast code search with agentctl using ripgrep and grep skills. Find patterns, files, and code.
---

# Search with agentctl

High-performance code and file searching.

## Ripgrep Search

Fast regex search with automatic gitignore support:

```bash
agentctl run text/ripgrep --input '{
  "pattern": "func.*Handler",
  "path": ".",
  "file_type": "go"
}'
```

Options:

- `pattern` - Regex pattern to search
- `path` - Directory or file to search
- `file_type` - Filter by file type (go, js, py, etc.)
- `case_sensitive` - Boolean (default: smart case)
- `context_lines` - Lines of context around matches

## Text Grep

Traditional recursive grep with glob patterns:

```bash
agentctl run text/grep --input '{
  "pattern": "TODO|FIXME",
  "path": ".",
  "include": ["*.go", "*.md"],
  "exclude": ["vendor/*"]
}'
```

## Find Files

Find files by name pattern:

```bash
agentctl run fs/find --input '{
  "path": ".",
  "pattern": "*.test.go",
  "type": "file"
}'
```

Options:

- `pattern` - Glob pattern
- `type` - "file", "directory", or "any"
- `max_depth` - Limit search depth

## Directory Tree

Get directory structure:

```bash
agentctl run fs/tree --input '{
  "path": "src/",
  "max_depth": 3,
  "include_hidden": false
}'
```

## Context-Aware Search

Extract code context around matches:

```bash
agentctl run code/context_ripgrep --input '{
  "query": "authentication",
  "path": "internal/",
  "expand_functions": true
}'
```

Expands matches to include full function bodies.
