---
name: foxctl JQ
description: Process JSON and YAML data using jq query language. Filter, transform, and extract data with powerful expressions.
---

# JQ Data Processing

Process JSON and YAML using the jq query language.

## Quick Start

```bash
foxctl run data/jq --input '{
  "query": ".data.items[] | select(.active)",
  "input": "{\"data\": {\"items\": [{\"name\": \"a\", \"active\": true}]}}"
}'
```

## Parameters

| Parameter    | Type    | Required | Default | Description                  |
| ------------ | ------- | -------- | ------- | ---------------------------- |
| `query`      | string  | Yes      | -       | JQ query expression          |
| `input`      | string  | Yes      | -       | JSON/YAML input data         |
| `raw_output` | boolean | No       | `false` | Output raw strings, not JSON |
| `compact`    | boolean | No       | `false` | Compact output               |
| `slurp`      | boolean | No       | `false` | Read entire input into array |
| `sort_keys`  | boolean | No       | `false` | Sort object keys             |
| `yaml_input` | boolean | No       | `false` | Parse input as YAML          |

## Common Queries

### Select Fields

```bash
foxctl run data/jq --input '{
  "query": ".name, .email",
  "input": "{\"name\": \"John\", \"email\": \"john@example.com\", \"age\": 30}"
}'
```

### Filter Arrays

```bash
foxctl run data/jq --input '{
  "query": ".users[] | select(.role == \"admin\")",
  "input": "{\"users\": [{\"name\": \"Alice\", \"role\": \"admin\"}, {\"name\": \"Bob\", \"role\": \"user\"}]}"
}'
```

### Transform Data

```bash
foxctl run data/jq --input '{
  "query": ".items | map({id: .id, title: .name})",
  "input": "{\"items\": [{\"id\": 1, \"name\": \"Item 1\"}, {\"id\": 2, \"name\": \"Item 2\"}]}"
}'
```

### Aggregate

```bash
foxctl run data/jq --input '{
  "query": "[.transactions[].amount] | add",
  "input": "{\"transactions\": [{\"amount\": 100}, {\"amount\": 250}, {\"amount\": 50}]}"
}'
```

## Output Options

### Raw Strings

Get unquoted string output:

```bash
foxctl run data/jq --input '{
  "query": ".message",
  "input": "{\"message\": \"Hello, World!\"}",
  "raw_output": true
}'
```

### Compact JSON

Single-line output:

```bash
foxctl run data/jq --input '{
  "query": ".",
  "input": "{\"a\": 1, \"b\": 2}",
  "compact": true
}'
```

### Sorted Keys

Consistent key ordering:

```bash
foxctl run data/jq --input '{
  "query": ".",
  "input": "{\"z\": 1, \"a\": 2}",
  "sort_keys": true
}'
```

## YAML Input

Parse YAML data:

```bash
foxctl run data/jq --input '{
  "query": ".services | keys",
  "input": "services:\n  web:\n    port: 8080\n  api:\n    port: 3000",
  "yaml_input": true
}'
```

## Advanced Examples

### Nested Selection

```bash
foxctl run data/jq --input '{
  "query": ".data.results[] | select(.score > 0.8) | {name, score}",
  "input": "{\"data\": {\"results\": [{\"name\": \"a\", \"score\": 0.9}, {\"name\": \"b\", \"score\": 0.5}]}}"
}'
```

### Group and Count

```bash
foxctl run data/jq --input '{
  "query": "group_by(.category) | map({category: .[0].category, count: length})",
  "input": "[{\"category\": \"A\"}, {\"category\": \"B\"}, {\"category\": \"A\"}]"
}'
```

### Recursive Descent

```bash
foxctl run data/jq --input '{
  "query": ".. | .id? // empty",
  "input": "{\"items\": [{\"id\": 1, \"children\": [{\"id\": 2}]}]}"
}'
```

## Use Cases

- **API response processing**: Extract relevant fields from API responses
- **Configuration transformation**: Convert between config formats
- **Log analysis**: Parse and filter JSON logs
- **Data pipelines**: Transform data between processing stages
