---
name: foxctl Data Operations
description: File and data operations with foxctl - read/write files, query JSON/YAML with jq. Use when asked to read files, write content, parse JSON, filter data, or transform YAML.
---

# Data Operations

Use this skill for file I/O and structured data processing with foxctl.

**Trigger phrases**: "read this file", "write to file", "parse JSON", "filter YAML", "query the data", "transform this JSON", "list directory", "find files"

## File Operations

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

Large files (>threshold) stored in CAS: `foxctl cas get sha256:...`

## JSON/YAML Processing

```bash
# Query with jq
foxctl run data/jq --input '{
  "query": ".data.items[] | select(.active)",
  "input": "{\"data\": {\"items\": [...]}}"
}'

# YAML input
foxctl run data/jq --input '{
  "query": ".services[].name",
  "input": "services:\n  - name: web",
  "yaml_input": true
}'
```

### Common JQ Patterns

```bash
# Select fields
".name, .email"

# Filter arrays
".users[] | select(.role == \"admin\")"

# Transform
".items | map({id: .id, title: .name})"

# Aggregate
"[.transactions[].amount] | add"

# Group and count
"group_by(.category) | map({category: .[0].category, count: length})"
```

## Parameters

| fs/read | Description |
|---------|-------------|
| `path` | File path (required) |

| fs/write | Description |
|----------|-------------|
| `path` | File path (required) |
| `content` | Content to write |
| `mode` | `overwrite`, `append`, `create` |

| data/jq | Description |
|---------|-------------|
| `query` | JQ expression (required) |
| `input` | JSON/YAML string (required) |
| `raw_output` | Output raw strings |
| `yaml_input` | Parse as YAML |

Full docs: See `~/.foxctl/share/configs/skills/foxctl-fs/` and `~/.foxctl/share/configs/skills/foxctl-jq/`
