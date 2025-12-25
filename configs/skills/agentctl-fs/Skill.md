---
name: agentctl File System
description: File operations with agentctl - read, write, list, tree. Handles large files via CAS storage.
---

# File System Operations with agentctl

Safe, structured file operations with content-addressable storage.

## Read Files

Read file contents with automatic CAS storage for large files:

```bash
agentctl run fs/read --input '{"path": "README.md"}'
```

Response includes:

- `data.content` - File contents (if small)
- `data.artifact` - CAS digest (if large)
- `data.preview` - First N bytes preview
- `data.size_bytes` - Total file size

## Write Files

Atomic file writes with backup:

```bash
agentctl run fs/write --input '{
  "path": "output.txt",
  "content": "File contents here",
  "mode": "overwrite"
}'
```

Modes:

- `overwrite` - Replace existing file
- `append` - Add to end of file
- `create` - Fail if file exists

## List Directory

```bash
agentctl run fs/ls --input '{
  "path": ".",
  "all": true,
  "long": true
}'
```

Options:

- `all` - Include hidden files
- `long` - Detailed output with sizes/dates
- `recursive` - List subdirectories

## Directory Tree

```bash
agentctl run fs/tree --input '{
  "path": "src/",
  "max_depth": 3,
  "gitignore": true
}'
```

## Find Files

```bash
agentctl run fs/find --input '{
  "path": ".",
  "pattern": "*.go",
  "type": "file",
  "max_depth": 5
}'
```

## Large File Handling

Files exceeding the inline threshold are stored in CAS:

```bash
# Read returns artifact digest
agentctl run fs/read --input '{"path": "large-file.json"}'
# -> data.artifact: "sha256:abc123..."

# Retrieve from CAS
agentctl cas get sha256:abc123...
```

## Path Safety

All paths are validated against workspace boundaries. Attempts to escape via
`../` or symlinks are blocked with `EPOLICY` errors.
