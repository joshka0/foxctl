---
name: agentctl SWE Grep
description: Extract high-signal code snippets from files based on natural language questions. Smart code retrieval for AI agents.
---

# SWE Grep - Smart Code Extraction

Extract relevant code snippets from candidate files based on natural language
questions.

## Overview

SWE Grep is the final stage in code retrieval:

1. Semantic/symbol indexes provide candidate files
2. SWE Grep reads those files from the live workspace
3. Extracts snippets most relevant to your question

## Usage

```bash
agentctl run code/snippet_extract --input '{
  "workspace_id": "my-project",
  "question": "How does user authentication work?",
  "candidates": [
    {"path": "internal/auth/login.go", "priority": 0.95},
    {"path": "internal/auth/session.go"}
  ]
}'
```

## Parameters

| Parameter      | Type   | Required | Description                                  |
| -------------- | ------ | -------- | -------------------------------------------- |
| `workspace_id` | string | Yes      | Workspace identifier                         |
| `question`     | string | Yes      | Natural language question guiding extraction |
| `candidates`   | array  | Yes      | Files/symbols to process                     |
| `limits`       | object | No       | Performance limits                           |

### Candidate Format

```json
{
  "path": "relative/path/to/file.go",
  "symbol_id": "optional/symbol:identifier",
  "priority": 0.95
}
```

### Limits

```json
{
  "max_files": 10,
  "max_snippets": 20,
  "max_bytes_per_file": 100000
}
```

## Examples

### Basic Query

```bash
agentctl run code/snippet_extract --input '{
  "workspace_id": "agentctl",
  "question": "Where are HTTP handlers defined?",
  "candidates": [
    {"path": "internal/server/routes.go"},
    {"path": "internal/server/handlers.go"},
    {"path": "cmd/server/main.go"}
  ]
}'
```

### With Symbol Hints

```bash
agentctl run code/snippet_extract --input '{
  "workspace_id": "agentctl",
  "question": "How does the cache invalidation logic work?",
  "candidates": [
    {"path": "internal/cache/cache.go", "symbol_id": "internal/cache/cache.go:Invalidate"},
    {"path": "internal/cache/cache.go", "symbol_id": "internal/cache/cache.go:Expire"}
  ]
}'
```

### With Limits

```bash
agentctl run code/snippet_extract --input '{
  "workspace_id": "agentctl",
  "question": "Error handling patterns",
  "candidates": [
    {"path": "internal/errors/errors.go"},
    {"path": "internal/handlers/error.go"},
    {"path": "pkg/errors/wrap.go"}
  ],
  "limits": {
    "max_files": 5,
    "max_snippets": 15,
    "max_bytes_per_file": 50000
  }
}'
```

## Output

### Small Results (Inline)

```json
{
  "data": {
    "summary": {
      "files_considered": 3,
      "files_relevant": 2,
      "snippets_emitted": 5
    },
    "snippets_inline": [
      {
        "path": "internal/auth/login.go",
        "start_line": 42,
        "end_line": 67,
        "content": "func Login(ctx context.Context, ...) {...}"
      }
    ]
  }
}
```

### Large Results (CAS)

```json
{
  "data": {
    "summary": {...},
    "artifact": "sha256:abc123..."
  }
}
```

Retrieve with:

```bash
agentctl cas get sha256:abc123...
```

## Use Cases

- **Code exploration**: Answer "how does X work?"
- **Bug investigation**: Find relevant code for a bug report
- **Feature planning**: Understand existing patterns before adding new code
- **Documentation**: Extract code examples for docs
