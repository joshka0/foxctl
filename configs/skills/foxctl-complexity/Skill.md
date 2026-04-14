---
name: foxctl Code Complexity
description: Analyze code complexity metrics - cyclomatic, cognitive, nesting depth, function length. Find hotspots and quality issues.
---

# Code Complexity Analysis

Measure and analyze code complexity to identify quality hotspots.

## Quick Start

Find complexity hotspots in your codebase:

```bash
foxctl run code/complexity --input '{
  "path": ".",
  "analysis_mode": "hotspots"
}'
```

## Parameters

| Parameter       | Type    | Required | Default      | Description                                                  |
| --------------- | ------- | -------- | ------------ | ------------------------------------------------------------ |
| `path`          | string  | No       | `.`          | File or directory to analyze                                 |
| `analysis_mode` | string  | No       | `hotspots`   | Mode: `file`, `function`, `aggregate`, `hotspots`            |
| `metric`        | string  | No       | `cyclomatic` | Metric: `cyclomatic`, `cognitive`, `all`                     |
| `threshold`     | integer | No       | `10`         | Complexity threshold for hotspot detection                   |
| `language`      | string  | No       | `auto`       | Language: `go`, `python`, `javascript`, `typescript`, `auto` |
| `include_tests` | boolean | No       | `false`      | Include test files                                           |
| `max_results`   | integer | No       | `100`        | Maximum results to return                                    |

## Analysis Modes

### Hotspots (Default)

Find functions exceeding complexity threshold:

```bash
foxctl run code/complexity --input '{
  "path": "internal/",
  "analysis_mode": "hotspots",
  "threshold": 15
}'
```

### Per-Function

Detailed metrics for each function:

```bash
foxctl run code/complexity --input '{
  "path": "main.go",
  "analysis_mode": "function"
}'
```

### Per-File

Aggregate complexity by file:

```bash
foxctl run code/complexity --input '{
  "path": "src/",
  "analysis_mode": "file"
}'
```

### Aggregate

Project-wide summary statistics:

```bash
foxctl run code/complexity --input '{
  "path": ".",
  "analysis_mode": "aggregate"
}'
```

## Metrics

### Cyclomatic Complexity

Measures independent paths through code:

```bash
foxctl run code/complexity --input '{
  "path": ".",
  "metric": "cyclomatic",
  "threshold": 10
}'
```

- **1-10**: Simple, low risk
- **11-20**: Moderate complexity
- **21-50**: High complexity, consider refactoring
- **51+**: Very high risk, should refactor

### Cognitive Complexity

Measures how hard code is to understand:

```bash
foxctl run code/complexity --input '{
  "path": ".",
  "metric": "cognitive"
}'
```

### All Metrics

Get both cyclomatic and cognitive:

```bash
foxctl run code/complexity --input '{
  "path": ".",
  "metric": "all"
}'
```

## Language-Specific

```bash
# Go
foxctl run code/complexity --input '{"path": ".", "language": "go"}'

# Python
foxctl run code/complexity --input '{"path": ".", "language": "python"}'

# TypeScript
foxctl run code/complexity --input '{"path": ".", "language": "typescript"}'
```

## Use Cases

- **Code review**: Identify complex functions needing attention
- **Refactoring**: Find candidates for simplification
- **Quality gates**: Enforce complexity thresholds in CI
- **Technical debt**: Track complexity trends over time
