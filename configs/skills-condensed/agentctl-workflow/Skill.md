---
name: agentctl-workflow
description: Execute and manage workflow pipelines that chain multiple agentctl skills
---

# Workflow Engine

Chain skills into pipelines with dependencies, parallel execution, loops, and conditionals.

## CLI

```bash
agentctl workflow run <name> --input '{"key": "value"}'
agentctl workflow list
agentctl workflow validate <name>
```

## Definition (YAML)

```yaml
apiVersion: agentctl/v1
kind: Workflow
metadata:
  name: my-workflow
inputs:
  - name: path
    type: string
    required: true
steps:
  - id: find
    skill: fs/find
    input: {path: "{{.inputs.path}}", pattern: "*.go"}
  - id: analyze
    skill: code/symbols
    loop: {over: "{{.find.data.files}}", as: file}
    parallel: 4
    input: {path: "{{.file}}"}
```

## Features

- **Templates**: `{{.inputs.path}}`, `{{.stepId.data.field}}`
- **Loops**: `loop: {over: "{{.list}}", as: item}` with `parallel: N`
- **Conditionals**: `if: "{{.inputs.deep}}"`
- **Dependencies**: `depends_on: [step1, step2]`
- **Error handling**: `on_error: continue|fail|retry`, `max_retries: 3`

## Built-in Workflows

| Workflow | Description |
|----------|-------------|
| `pre-impl-analysis` | Find files, symbols, complexity |
| `code-review` | Complexity + TODO scanning |
| `lsp-analysis` | Definition, references, call hierarchy |

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-workflow/Skill.md`
