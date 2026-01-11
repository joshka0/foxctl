---
name: agentctl Test Runner
description: Run tests with coverage, benchmarks, and race detection. Structured test results for Go projects.
---

# Test Runner

Run Go tests with structured results via `agentctl run test/run`.

## Usage

```bash
agentctl run test/run --input '{"path": "./...", "mode": "test"}'
```

## Parameters

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | `./...` | Path to test |
| `mode` | string | `test` | `test`, `race`, `bench`, `coverage` |
| `short` | bool | false | Skip long tests |
| `verbose` | bool | false | Verbose output |
| `pattern` | string | - | Test name pattern (`-run`) |
| `timeout` | string | `10m` | Test timeout |

## Output

```json
{"data": {"passed": 42, "failed": 1, "skipped": 3, "duration_ms": 5230, "coverage_pct": 78.5, "failures": [...]}}
```

Full docs: `~/.agentctl/share/configs/skills/agentctl-test/Skill.md`
