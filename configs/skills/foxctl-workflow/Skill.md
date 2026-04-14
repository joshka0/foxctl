---
name: foxctl-workflow
description: Execute and manage workflow pipelines that chain multiple foxctl skills
---

# Workflow Engine

Chain multiple foxctl skills into automated pipelines with dependencies, parallel execution, loops, and conditionals.

## CLI Commands

```bash
# Execute a workflow
foxctl workflow run <name-or-path> --input '{"key": "value"}'

# List available workflows
foxctl workflow list

# Validate without executing
foxctl workflow validate <name>

# Show workflow details
foxctl workflow show <name>
```

## Workflow Definition

Workflows are YAML files in `workflows/` or `~/.foxctl/workflows/`:

```yaml
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: my-workflow
  description: "What this workflow does"

inputs:
  - name: path
    type: string
    required: true
  - name: pattern
    default: "*.go"

steps:
  - id: find
    skill: fs/find
    input:
      path: "{{.inputs.path}}"
      pattern: "{{.inputs.pattern}}"

  - id: analyze
    skill: code/symbols
    loop:
      over: "{{.find.data.files}}"
      as: file
    parallel: 4
    input:
      path: "{{.file}}"

  - id: report
    skill: workflow/aggregate
    depends_on: [analyze]
    input:
      data: "{{.analyze.data}}"

outputs:
  - name: file_count
    value: "{{len .find.data.files}}"
```

## Key Features

### Template Expressions

Access inputs and step results:
```yaml
input:
  path: "{{.inputs.path}}"           # Workflow input
  files: "{{.find.data.files}}"      # Previous step's data
  status: "{{.find.status}}"         # Step status
```

### Loops

Iterate over arrays with optional parallelism:
```yaml
- id: process
  skill: code/symbols
  loop:
    over: "{{.find.data.files}}"
    as: file
  parallel: 4  # Max concurrent executions
  input:
    path: "{{.file}}"
```

### Conditionals

Skip steps based on conditions:
```yaml
- id: deep_analysis
  skill: lsp/gopls
  if: "{{.inputs.deep}}"
  input:
    operation: call_hierarchy
```

### Dependencies

Explicit or inferred from template references:
```yaml
- id: report
  depends_on: [step1, step2]  # Explicit
  # Or automatic via {{.step1.data}}
```

### Error Handling

```yaml
- id: risky_step
  skill: external/api
  on_error: continue    # fail (default), continue, retry
  timeout: "30s"
  max_retries: 3
  retry_delay: "1s"
```

## Template Functions

### Collections
- `len`, `first`, `last`, `index`, `slice`
- `keys`, `values`, `filter`, `map`, `flatMap`
- `flatten`, `unique`, `sort`, `reverse`
- `append`, `concat`

### Strings
- `upper`, `lower`, `trim`, `replace`, `split`, `join`
- `contains`, `hasPrefix`, `hasSuffix`

### Paths
- `base`, `dir`, `ext`, `clean`, `joinPath`

### Data Access
- `get` (nested path access), `pluck`, `pick`, `omit`, `merge`, `hasKey`

### Conditionals
- `default`, `coalesce`, `ternary`, `empty`
- `eq`, `ne`, `lt`, `le`, `gt`, `ge`
- `and`, `or`, `not`

### Math
- `add`, `sub`, `mul`, `div`, `mod`, `max`, `min`

### Conversion
- `toJSON`, `fromJSON`, `toString`, `toInt`, `toBool`

## Built-in Workflows

| Workflow | Description |
|----------|-------------|
| `pre-impl-analysis` | Find files, extract symbols, check complexity |
| `code-review` | Complexity analysis + TODO scanning |
| `lsp-analysis` | Definition, references, call hierarchy |
| `batch-process` | Parallel file processing with retries |

## Examples

### Run pre-implementation analysis
```bash
foxctl workflow run pre-impl-analysis --input '{"path": "./internal", "pattern": "*.go"}'
```

### Run with verbose output
```bash
foxctl workflow run code-review --input '{"path": ".", "lang": "go"}' -v
```

### Dry-run validation
```bash
foxctl workflow run my-workflow --input '{}' --dry-run
```

## Programmatic API (Go)

```go
import "github.com/joshka0/foxctl/internal/runtime/orchestration/workflow"

// Using the engine
engine := workflow.NewEngine()
result, err := engine.Run(ctx, "my-workflow", map[string]any{
    "path": "./src",
})

// Using the builder
wf, _ := workflow.NewBuilder("my-workflow").
    Input("path", "string", workflow.Required()).
    Step("find", "fs/find", map[string]any{
        "path": "{{.inputs.path}}",
    }).
    Step("analyze", "code/symbols", map[string]any{
        "path": "{{.find.data.files}}",
    }).Loop("{{.find.data.files}}", "file").Parallel(4).
    Build()
```
