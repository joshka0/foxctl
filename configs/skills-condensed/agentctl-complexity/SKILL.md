---
name: agentctl Code Complexity
description: Analyze code complexity metrics - cyclomatic, cognitive, nesting depth, function length. Find hotspots and quality issues.
---

# Code Complexity Analysis

Measure complexity to identify quality hotspots via `agentctl run code/complexity`.

## Usage

```bash
agentctl run code/complexity --input '{"path": ".", "analysis_mode": "hotspots"}'
```

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `path` | string | File or directory (default: `.`) |
| `analysis_mode` | string | `file`, `function`, `aggregate`, `hotspots` |
| `metric` | string | `cyclomatic`, `cognitive`, `all` |
| `threshold` | int | Hotspot threshold (default: 10) |
| `language` | string | `go`, `python`, `javascript`, `typescript`, `auto` |
| `include_tests` | bool | Include test files |

## Thresholds

- **1-10**: Simple, low risk
- **11-20**: Moderate complexity
- **21-50**: High complexity, consider refactoring
- **51+**: Very high risk, should refactor

Full docs: `~/.agentctl/share/configs/skills/agentctl-complexity/Skill.md`
