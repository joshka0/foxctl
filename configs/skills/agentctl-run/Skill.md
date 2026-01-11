---
name: agentctl Run
description: Execute agentctl skills with JSON input. Use for file ops, code analysis, text search, and structured workflows.
---

# agentctl Skill Runner

Run any installed agentctl skill with JSON envelope I/O.

## Usage

```bash
agentctl run <skill-name> --input '<json>'
```

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
agentctl run fs/ls --input '{"path": "."}'

# Read a file
agentctl run fs/read --input '{"path": "README.md"}'

# Search for pattern
agentctl run text/ripgrep --input '{"pattern": "TODO", "path": "."}'

# Get code symbols
agentctl run code/symbols --input '{"path": "main.go"}'

# Create a todo
agentctl run todo/manage --input '{"action": "add", "title": "Fix bug"}'
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
