---
name: agentctl Data Operations
description: File and data operations with agentctl - read/write files, query JSON/YAML with jq. Use when asked to read files, write content, parse JSON, filter data, or transform YAML.
---

# Data Operations

Use this skill for file I/O and structured data processing with agentctl.

**Trigger phrases**: "read this file", "write to file", "parse JSON", "filter YAML", "query the data", "transform this JSON", "list directory", "find files"

## File Operations

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

Large files (>threshold) stored in CAS: `agentctl cas get sha256:...`

## JSON/YAML Processing

```bash
# Query with jq
agentctl run data/jq --input '{
  "query": ".data.items[] | select(.active)",
  "input": "{\"data\": {\"items\": [...]}}"
}'

# YAML input
agentctl run data/jq --input '{
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

Full docs: See `~/.agentctl/share/configs/skills/agentctl-fs/` and `~/.agentctl/share/configs/skills/agentctl-jq/`
