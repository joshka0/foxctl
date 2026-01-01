---
name: agentctl Code Intelligence
description: Research and analyze code with agentctl - symbols, complexity, semantic search, smart grep. Use when asked to understand code, find functions, research how something works, locate definitions, or explore the codebase.
---

# Code Intelligence

Use this skill when you need to understand, research, query, or analyze code in the codebase.

**Trigger phrases**: "research how", "find where", "look for", "locate the", "what does X do", "find usages of", "where is X defined", "show me the code for", "analyze complexity", "query the code", "search for", "understand how", "how does X work", "find all", "explore the code", "investigate"

## Symbols & Structure

```bash
# Extract symbols (functions, types, methods)
agentctl run code/symbols --input '{"path": "src/main.go"}'

# Directory tree structure
agentctl run fs/tree --input '{"path": "src/", "max_depth": 3}'
```

## Code Search

```bash
# Pattern search with ripgrep
agentctl run text/ripgrep --input '{"pattern": "func.*Handler", "path": ".", "file_type": "go"}'

# Context-aware search (returns full function bodies)
agentctl run code/context_ripgrep --input '{"query": "auth", "expand_functions": true}'

# Smart semantic extraction
agentctl run code/swe_grep --input '{"question": "How does auth work?", "candidates": [{"path": "auth/login.go"}]}'

# Semantic code search (vector similarity)
agentctl run code/semantic_search --input '{"query": "error handling middleware", "limit": 10}'
```

## Complexity Analysis

```bash
# Find complexity hotspots
agentctl run code/complexity --input '{"path": ".", "analysis_mode": "hotspots"}'

# Per-function metrics
agentctl run code/complexity --input '{"path": "internal/", "analysis_mode": "function"}'
```

Thresholds: 1-10 simple, 11-20 moderate, 21-50 high (refactor), 51+ critical

## Pre-Implementation Analysis

```bash
# Full analysis before making changes
agentctl run preimpl/analyze --input '{"path": ".", "task": "add authentication"}'
```

## Security & Imports

```bash
# Security scanning
agentctl run code/security --input '{"path": ".", "recursive": true}'

# Import analysis
agentctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

Full docs: See individual skill docs in `configs/skills/`
