---
name: agentctl Test Runner
description: Run tests with coverage, benchmarks, and race detection. Structured test results for Go projects.
---

# Test Runner

Run tests with various modes and get structured results.

## Quick Start

```bash
agentctl run test/run --input '{"path": "./..."}'
```

## Parameters

| Parameter | Type    | Required | Default | Description                               |
| --------- | ------- | -------- | ------- | ----------------------------------------- |
| `path`    | string  | No       | `./...` | Path to test                              |
| `mode`    | string  | No       | `test`  | Mode: `test`, `race`, `bench`, `coverage` |
| `short`   | boolean | No       | `false` | Run in short mode                         |
| `verbose` | boolean | No       | `false` | Verbose output                            |
| `pattern` | string  | No       | -       | Run tests matching pattern (`-run`)       |
| `timeout` | string  | No       | `10m`   | Test timeout                              |

## Test Modes

### Standard Tests

```bash
agentctl run test/run --input '{
  "path": "./internal/...",
  "mode": "test"
}'
```

### Race Detection

Find data races:

```bash
agentctl run test/run --input '{
  "path": "./...",
  "mode": "race"
}'
```

### Coverage

Get code coverage metrics:

```bash
agentctl run test/run --input '{
  "path": "./...",
  "mode": "coverage"
}'
```

### Benchmarks

Run performance benchmarks:

```bash
agentctl run test/run --input '{
  "path": "./...",
  "mode": "bench"
}'
```

## Filtering

### By Pattern

Run specific tests:

```bash
agentctl run test/run --input '{
  "path": "./internal/cache/...",
  "pattern": "TestCache.*"
}'
```

### Short Mode

Skip long-running tests:

```bash
agentctl run test/run --input '{
  "path": "./...",
  "short": true
}'
```

### With Timeout

Custom timeout for slow tests:

```bash
agentctl run test/run --input '{
  "path": "./integration/...",
  "timeout": "30m"
}'
```

## Examples

### Quick Validation

```bash
agentctl run test/run --input '{
  "path": "./...",
  "short": true,
  "timeout": "5m"
}'
```

### CI Pipeline

```bash
agentctl run test/run --input '{
  "path": "./...",
  "mode": "race",
  "verbose": true
}'
```

### Focused Testing

```bash
agentctl run test/run --input '{
  "path": "./internal/auth/...",
  "pattern": "TestLogin",
  "verbose": true
}'
```

### Coverage Report

```bash
agentctl run test/run --input '{
  "path": "./...",
  "mode": "coverage"
}'
```

## Output

Returns structured test results:

```json
{
	"data": {
		"passed": 42,
		"failed": 1,
		"skipped": 3,
		"duration_ms": 5230,
		"coverage_pct": 78.5,
		"failures": [
			{
				"test": "TestLogin/invalid_password",
				"package": "internal/auth",
				"message": "expected error, got nil"
			}
		]
	}
}
```

## Use Cases

- **Pre-commit**: Quick validation before committing
- **CI integration**: Structured results for pipelines
- **Debugging**: Run specific failing tests with verbose output
- **Performance**: Track benchmark results over time
