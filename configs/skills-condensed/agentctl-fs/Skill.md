---
name: agentctl File System
description: File operations with agentctl - read, write, list, tree. Handles large files via CAS storage.
---

# File System Operations

Safe, structured file operations with CAS for large files.

## Skills

```bash
# Read file
agentctl run fs/read --input '{"path": "README.md"}'

# Write file (modes: overwrite, append, create)
agentctl run fs/write --input '{"path": "out.txt", "content": "...", "mode": "overwrite"}'

# List directory
agentctl run fs/ls --input '{"path": ".", "all": true, "long": true}'

# Directory tree
agentctl run fs/tree --input '{"path": "src/", "max_depth": 3, "gitignore": true}'

# Find files
agentctl run fs/find --input '{"path": ".", "pattern": "*.go", "type": "file"}'
```

## Large Files

Files exceeding threshold stored in CAS:
```bash
# Returns data.artifact: "sha256:abc..."
agentctl run fs/read --input '{"path": "large.json"}'
agentctl cas get sha256:abc...
```

Path safety: Escapes via `../` or symlinks are blocked.

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-fs/Skill.md`
