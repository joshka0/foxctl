---
name: foxctl Run
description: Execute foxctl skills with JSON input. Use for file ops, code analysis, text search, and structured workflows.
---

# foxctl Skill Runner

Run any installed foxctl skill with JSON envelope I/O.

## Usage

```bash
foxctl run <skill-name> --input '<json>'
```

Use `foxctl run` for job-tracked execution of installed skills. Use
`foxctl run <skill-name> --ephemeral --input '<json>'` when the jobs store is
not writable or you do not need history. Use `foxctl skills run <skill-name>`
for direct skill execution with manifest-derived flags.

Input modes:

| Mode | Use |
| ---- | --- |
| `--input '<json>'` | Pass raw JSON inline |
| `--input-file input.json` | Read raw JSON from a file |
| `--input-file -` | Pipe raw JSON from stdin |
| `--input stdin` | Read an envelope from stdin and pass its `data` field |
| `--input sha256:<hex>` | Read raw JSON from CAS |

## Available Skills

### File System

- `fs/ls` - List directory contents
- `fs/read` - Read file with CAS storage for large files
- `fs/write` - Write file with atomic operations
- `fs/tree` - Directory tree with filtering
- `fs/find` - Find files by pattern

### Code Analysis

- `code/diff` - Generate unified diffs between files
- `code/symbols` - Extract symbols (functions, classes, types)
- `code/complexity` - Analyze code complexity metrics
- `code/imports` - Analyze import dependencies
- `code/security` - Security pattern scanning
- `code/smart_search` - High-signal code snippet extraction

### Text Operations

- `text/grep` - Recursive regex search
- `text/ripgrep` - Fast ripgrep wrapper
- `text/replace` - Find and replace in files

### Git Operations

- `git/status` - Repository status
- `code/git` - Git operations (log, diff, blame)

### Data Processing

- `data/jq` - JQ queries on JSON data
- `json/transform` - JSON transformations

### Task Management

- `todo/manage` - Nested TODO management

## Examples

```bash
# List files
foxctl run fs/ls --input '{"path": "."}'

# List files without job persistence
foxctl run fs/ls --ephemeral --input '{"path": "."}'

# List files with direct parameter flags
foxctl skills run fs/ls --path .

# Read a file
foxctl run fs/read --input '{"path": "README.md"}'

# Search for pattern
foxctl run text/ripgrep --input '{"pattern": "TODO", "path": "."}'

# Get code symbols
foxctl run code/symbols --input '{"path": "main.go"}'

# Create a todo
foxctl run todo/manage --input '{"action": "add", "title": "Fix bug"}'
```

## Output Format

All skills return JSON envelopes:

```json
{
  "version": 1,
  "status": "ok",
  "command": "<skill-name>",
  "data": { ... },
  "meta": { "ts": "...", "source": "run" }
}
```

Large outputs are stored in CAS with `data.artifact` containing the digest.
