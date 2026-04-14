# skill/inspect

A meta-skill for inspecting other foxctl skills. Designed to help LLMs (and humans) understand, troubleshoot, and analyze skill implementations.

## Overview

The `skill/inspect` skill provides multiple views into a skill's implementation:
- **manifest**: Raw skill.yaml configuration
- **types**: Go type definitions (structs with fields and JSON tags)
- **api**: Combined view of parameters and types
- **implementation**: Function signatures and bodies
- **full**: Complete source code
- **examples**: Generated usage examples
- **all**: Everything above in one response

## Use Cases

### For LLMs
- Understand skill capabilities and parameters
- Troubleshoot skill issues by examining implementation
- Learn patterns for implementing new skills
- Generate accurate usage examples

### For Developers
- Quick reference for skill APIs
- Understand parameter types and validation
- Review implementation details
- Debug skill behavior

## Views

### 1. API View (Default)

Shows the skill's interface: parameters from manifest + Go type definitions.

```bash
echo '{"skill_name":"git/worktree","view":"api"}' | skill_inspect
```

**Output:**
```json
{
  "skill_name": "git/worktree",
  "description": "Manage git worktrees...",
  "parameters": [
    {
      "name": "operation",
      "type": "string",
      "required": "true",
      "description": "Operation to perform..."
    }
  ],
  "types": [
    {
      "name": "input",
      "kind": "struct",
      "fields": [
        {"name": "Operation", "type": "string", "json_tag": "operation"}
      ]
    }
  ]
}
```

### 2. Types View

Extracts and shows all Go struct definitions with field types and JSON tags.

```bash
echo '{"skill_name":"fs/write","view":"types"}' | skill_inspect
```

**Use case**: Understand the exact data structure expected by the skill.

### 3. Implementation View

Shows function signatures and source code.

```bash
# All functions
echo '{"skill_name":"json/transform","view":"implementation"}' | skill_inspect

# Specific function
echo '{"skill_name":"json/transform","view":"implementation","function":"extractPath"}' | skill_inspect
```

**Output:**
```json
{
  "functions": [
    {
      "name": "extractPath",
      "params": ["data any", "path string"],
      "returns": ["any"],
      "body": "func extractPath(data any, path string) any {\n..."
    }
  ]
}
```

### 4. Manifest View

Returns the raw skill.yaml file.

```bash
echo '{"skill_name":"test/run","view":"manifest"}' | skill_inspect
```

**Use case**: See the complete skill configuration including capabilities, metadata, and distribution settings.

### 5. Full View

Returns the complete main.go source code.

```bash
echo '{"skill_name":"git/status","view":"full"}' | skill_inspect
```

**Output includes:**
- Complete source code
- File path
- Size and line count

### 6. Examples View

Generates usage examples based on the skill's parameters.

```bash
echo '{"skill_name":"data/jq","view":"examples"}' | skill_inspect
```

**Output:**
```json
{
  "examples": [
    {
      "name": "basic",
      "description": "Basic usage with required parameters",
      "input": "{\n  \"query\": \"example\",\n  \"input\": \"example\"\n}",
      "command": "echo '{...}' | foxctl run data/jq"
    },
    {
      "name": "full",
      "description": "Full usage with all parameters",
      "input": "...",
      "command": "..."
    }
  ]
}
```

### 7. All View

Combines all views into a single comprehensive response.

```bash
echo '{"skill_name":"git/worktree","view":"all"}' | skill_inspect
```

**Use case**: Get complete understanding of a skill in one query.

## Common Workflows

### LLM Troubleshooting

When an LLM encounters an error with a skill:

```bash
# 1. Check the API to understand parameters
echo '{"skill_name":"fs/write","view":"api"}' | skill_inspect

# 2. Look at types to see data structures
echo '{"skill_name":"fs/write","view":"types"}' | skill_inspect

# 3. Check implementation of specific function
echo '{"skill_name":"fs/write","view":"implementation","function":"getContent"}' | skill_inspect
```

### Learning Skill Patterns

To understand how to implement a new skill:

```bash
# Study a similar skill's complete implementation
echo '{"skill_name":"json/transform","view":"all"}' | skill_inspect
```

### Quick Parameter Reference

```bash
# Just need to know what parameters are available
echo '{"skill_name":"test/run","view":"api"}' | skill_inspect | jq '.data.parameters'
```

### Generate Documentation

```bash
# Get examples for documentation
echo '{"skill_name":"git/status","view":"examples"}' | skill_inspect
```

## Implementation Details

The skill uses Go's `go/ast` and `go/parser` packages to analyze source code:

1. **Type Extraction**: Parses AST to find struct definitions and extract field information
2. **Function Analysis**: Identifies function declarations with parameters and return types
3. **Manifest Parsing**: Simple YAML parsing to extract parameter definitions
4. **Example Generation**: Creates realistic examples based on parameter types and requirements

## Limitations

- Only works with skills in the `skills/` directory
- Requires skill to have both `skill.yaml` and `main.go`
- YAML parsing is basic (doesn't handle all edge cases)
- Type formatting handles common Go types but may show "unknown" for complex types

## Future Enhancements

Potential additions:
- Show skill dependencies (imported packages)
- Analyze error handling patterns
- Generate test cases based on parameters
- Show skill performance metrics
- Compare multiple skills side-by-side
- Interactive query builder for complex skills

## Example: Complete LLM Workflow

An LLM needs to use the `test/run` skill but doesn't know the parameters:

```bash
# Step 1: Inspect the API
echo '{"skill_name":"test/run","view":"api"}' | skill_inspect

# Response shows:
# - mode: string (test, race, bench, coverage)
# - path: string (default ./...)
# - pattern: string (test filter)
# etc.

# Step 2: Get examples
echo '{"skill_name":"test/run","view":"examples"}' | skill_inspect

# Step 3: LLM can now construct correct usage:
echo '{"mode":"coverage","path":"./internal/..."}' | test_run
```

## Pro Tips

1. **Use `jq` to filter**: Pipe output through `jq` to extract specific information
2. **Combine with other skills**: Use `fs/write` to save inspection results
3. **Memory integration**: Save frequently-inspected skills with `--remember`
4. **Chain inspections**: Compare multiple skills by running multiple inspections

## Error Handling

If a skill is not found:
```json
{
  "status": "error",
  "error": {
    "code": "ERUNTIME",
    "message": "skill not found: unknown/skill (looked in skills/unknown_skill)"
  }
}
```

Make sure to use the correct skill name format (e.g., `git/worktree`, not `git_worktree`).
