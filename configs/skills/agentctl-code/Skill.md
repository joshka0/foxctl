---
name: agentctl Code Analysis
description: Analyze code with agentctl skills - symbols, complexity, diffs, security scanning, and imports.
---

# Code Analysis with agentctl

Extract insights from codebases using specialized analysis skills.

## Symbols Extraction

Extract functions, classes, types, and their signatures:

```bash
agentctl run code/symbols --input '{"path": "src/main.go"}'
```

Output includes:

- Function names and signatures
- Class/struct definitions
- Type declarations
- Method receivers

## Code Complexity

Analyze cyclomatic complexity and other metrics:

```bash
agentctl run code/complexity --input '{"path": "src/", "recursive": true}'
```

## Unified Diffs

Generate diffs between files or versions:

```bash
agentctl run code/diff --input '{"old": "file.go.bak", "new": "file.go", "format": "unified"}'
```

## Security Scanning

Detect potential security issues:

```bash
agentctl run code/security --input '{"path": ".", "recursive": true}'
```

Checks for:

- Hardcoded secrets
- SQL injection patterns
- Unsafe function calls
- Path traversal risks

## Import Analysis

Map dependencies and import relationships:

```bash
agentctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

## SWE Grep

Extract high-signal code snippets from candidates:

```bash
agentctl run code/swe_grep --input '{"query": "error handling", "files": ["handler.go", "service.go"]}'
```

## Git Operations

```bash
# Recent commits
agentctl run code/git --input '{"action": "log", "count": 10}'

# File blame
agentctl run code/git --input '{"action": "blame", "path": "main.go"}'

# Diff against branch
agentctl run code/git --input '{"action": "diff", "ref": "main"}'
```
