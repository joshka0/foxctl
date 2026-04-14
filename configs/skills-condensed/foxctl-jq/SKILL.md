---
name: foxctl JQ
description: Process JSON and YAML data using jq query language. Filter, transform, and extract data with powerful expressions.
---

# JQ Data Processing

Process JSON/YAML via `foxctl run data/jq`.

## Usage

```bash
foxctl run data/jq --input '{
  "query": ".data.items[] | select(.active)",
  "input": "{\"data\": {\"items\": [...]}}"
}'
```

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `query` | string | JQ expression (required) |
| `input` | string | JSON/YAML data (required) |
| `raw_output` | bool | Output raw strings |
| `compact` | bool | Compact JSON output |
| `yaml_input` | bool | Parse as YAML |

## Common Queries

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

Full docs: `~/.foxctl/share/configs/skills/foxctl-jq/Skill.md`
