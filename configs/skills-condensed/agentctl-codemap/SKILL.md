---
name: agentctl Codemap
description: Generate semantic code relationship maps using AI-driven analysis. Creates structured traces of code paths with annotations.
---

# Codemaps

AI-driven code relationship mapping.

```bash
agentctl run codemap/generate --input '{"query": "how does auth work", "depth": 1}' --timeout 10m
```

## Parameters

| Param | Description |
|-------|-------------|
| `query` | Natural language question about code |
| `depth` | Trace depth (1-3, higher = more detail) |
| `path` | Starting directory |

## Timeout

- Simple query: 5-10 min
- Complex query: 10-20 min

## Search Codemaps

```bash
agentctl run code/semantic_search --input '{"query": "authentication", "scope": ["codemaps"]}'
```

## Import Codemaps

```bash
agentctl run codemap/import --input '{"path": "docs/codemaps", "recursive": false}'
```

## Output

- Traces with numbered sections
- ASCII trees of code relationships
- Annotated references with file:line

Full docs: `~/.agentctl/share/configs/skills/agentctl-codemap/Skill.md`
