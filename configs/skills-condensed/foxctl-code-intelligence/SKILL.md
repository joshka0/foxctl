---
name: foxctl Code Intelligence
description: Research and analyze code with foxctl - symbols, semantic search, smart search. Use when asked to understand code, find functions, research how something works, locate definitions, or explore the codebase.
---

# Code Intelligence

Use this skill when you need to understand, research, query, or analyze code in the codebase.

**Trigger phrases**: "research how", "find where", "look for", "locate the", "what does X do", "find usages of", "where is X defined", "show me the code for", "analyze complexity", "query the code", "search for", "understand how", "how does X work", "find all", "explore the code", "investigate"

## Symbols & Structure

```bash
# Extract symbols (functions, types, methods)
foxctl run code/symbols --input '{"path": "src/main.go"}'

# Directory tree structure
foxctl run fs/tree --input '{"path": "src/", "max_depth": 3}'
```

## Code Search

```bash
# Pattern search
foxctl run text/grep --input '{"pattern": "func.*Handler", "path": "."}'

# Context-aware search (returns full function bodies)
foxctl run code/context_grep --input '{"query": "auth", "expand_functions": true}'

# Smart semantic extraction
foxctl run code/snippet_extract --input '{"question": "How does auth work?", "candidates": [{"path": "auth/login.go"}]}'

# Semantic code search (vector similarity)
foxctl run code/semantic_search --input '{"query": "error handling middleware", "limit": 10}'
```

## Pre-Implementation Analysis

```bash
# Full analysis before making changes
foxctl run preimpl/analyze --input '{"path": ".", "task": "add authentication"}'
```

## Security & Imports

```bash
# Security scanning
foxctl run code/security --input '{"path": ".", "recursive": true}'

# Import analysis
foxctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

Full docs: See individual skill docs in `~/.foxctl/share/configs/skills/`
