---
name: agentctl Pre-Implementation Analysis
description: Pre-implementation checklist to understand codebase before writing code. Combines code analysis skills for thorough preparation.
---

# Pre-Implementation Analysis with agentctl

Systematic codebase analysis before implementation. Use these skills to understand
existing patterns, identify relevant code, and validate assumptions.

## Quick Start

Run the `/pre-impl` command with your task description:

```
/pre-impl add retry logic to HTTP client
```

This invokes the full checklist workflow.

## Individual Analysis Skills

### Find Related Files

Locate files relevant to your task:

```bash
# By filename pattern
agentctl run fs/find --input '{"path": ".", "pattern": "*client*"}'

# By content (ripgrep)
agentctl run text/ripgrep --input '{"pattern": "http.*request", "path": "internal/"}'

# Tree view for structure
agentctl run fs/tree --input '{"path": "internal/", "depth": 3}'
```

### Extract Code Structure

Understand what exists before adding:

```bash
# Get all symbols (functions, types, methods)
agentctl run code/symbols --input '{"path": "internal/client/http.go"}'

# Example output:
# {
#   "functions": ["NewClient", "Do", "Get", "Post"],
#   "types": ["Client", "Options", "Response"],
#   "methods": ["Client.Do", "Client.Get"]
# }
```

### Assess Complexity

Identify hotspots to avoid making worse:

```bash
agentctl run code/complexity --input '{"path": "internal/", "recursive": true}'

# High complexity files may need refactoring, not more code
```

### Analyze Dependencies

Understand import relationships:

```bash
agentctl run code/imports --input '{"path": "internal/client/", "recursive": true}'

# Shows what depends on what - helps identify impact
```

### Security Check (before touching sensitive code)

```bash
agentctl run code/security --input '{"path": "internal/auth/", "recursive": true}'
```

### SWE-Grep for Semantic Search

Find code snippets relevant to a natural language query:

```bash
agentctl run code/swe_grep --input '{
  "query": "how errors are handled in HTTP responses",
  "files": ["internal/client/http.go", "internal/client/errors.go"]
}'
```

## LSP-Powered Analysis (Go)

For Go projects, use the `lsp/gopls` skill for semantic understanding:

### Find All References

See everywhere a function/type is used:

```bash
agentctl run lsp/gopls --input '{"operation": "references", "file": "internal/client/http.go", "line": 25, "column": 6}'
```

Use this to understand:
- How widely used is this code?
- Will my changes break other callers?
- What patterns do other callers expect?

### Call Hierarchy

Understand the call chain:

```bash
agentctl run lsp/gopls --input '{"operation": "call_hierarchy", "file": "internal/client/http.go", "line": 25, "column": 6}'
```

Returns:
- **Callers**: Who calls this function
- **Callees**: What this function calls

### Workspace Symbol Search

Find types/functions across the entire codebase:

```bash
agentctl run lsp/gopls --input '{"operation": "workspace_symbol", "query": "Handler"}'
```

### Find Implementations

For interfaces, find all implementations:

```bash
agentctl run lsp/gopls --input '{"operation": "implementation", "file": "internal/domain/executor.go", "line": 15, "column": 6}'
```

### Check for Errors

Get diagnostics before making changes:

```bash
agentctl run lsp/gopls --input '{"operation": "check", "file": "internal/client/http.go"}'
```

## Workflow Integration

### With Task Guard

The task guard hook ensures you have an active task before writing code.
Pre-impl analysis naturally leads to task creation:

```bash
# After pre-impl analysis, create a task
agentctl todo add --title "Add retry logic to HTTP client" \
  --description "Based on pre-impl: modify internal/client/http.go, follow existing error patterns"
```

### With Git History

Check how similar features were implemented:

```bash
# Recent commits touching relevant files
agentctl run code/git --input '{"action": "log", "path": "internal/client/", "count": 10}'

# Who last modified the file and why
agentctl run code/git --input '{"action": "blame", "path": "internal/client/http.go"}'
```

## Output: Pre-Impl Summary

After analysis, produce a summary block:

```markdown
## Pre-Impl Summary: Add retry logic to HTTP client

**Problem:** HTTP requests fail silently on transient errors
**Approach:** Add retry wrapper using existing backoff pattern from internal/retry
**Key files:** internal/client/http.go, internal/retry/backoff.go
**Assumptions:**
- Only retry on 5xx and network errors
- Max 3 retries with exponential backoff
**Risk:** Could mask permanent failures if retry logic too aggressive
**Steps:** 4 steps identified

Ready to implement: YES
```

## Anti-Patterns

**Don't:**
- Skip reading existing code and guess at patterns
- Assume you need new files (usually you don't)
- Start coding before listing assumptions
- Ignore high-complexity warnings

**Do:**
- Read at least 3 related files before proposing changes
- Identify and reuse existing abstractions
- Question whether the simplest solution was chosen
- Document what's explicitly out of scope
