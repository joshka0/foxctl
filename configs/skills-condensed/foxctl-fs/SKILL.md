---
name: foxctl File System
description: File operations with foxctl - read, write, list, tree. Handles large files via CAS storage.
---

# File System Operations

Safe, structured file operations with CAS for large files.

## Skills

```bash
# Read file
foxctl run fs/read --input '{"path": "README.md"}'

# Write file (modes: overwrite, append, create)
foxctl run fs/write --input '{"path": "out.txt", "content": "...", "mode": "overwrite"}'

# List directory
foxctl run fs/ls --input '{"path": ".", "all": true, "long": true}'

# Directory tree
foxctl run fs/tree --input '{"path": "src/", "max_depth": 3, "gitignore": true}'

# Find files
foxctl run fs/find --input '{"path": ".", "pattern": "*.go", "type": "file"}'
```

## Large Files

Files exceeding threshold stored in CAS:
```bash
# Returns data.artifact: "sha256:abc..."
foxctl run fs/read --input '{"path": "large.json"}'
foxctl cas get sha256:abc...
```

Path safety: Escapes via `../` or symlinks are blocked.

Full docs: `~/.foxctl/share/configs/skills/foxctl-fs/Skill.md`
