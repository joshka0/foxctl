---
name: agentctl Codemap
description: Generate semantic code relationship maps using AI-driven analysis. Creates structured traces of code paths with annotations.
---

# Codemaps with agentctl

Agent-driven semantic code mapping that generates structured traces of how code flows through a codebase.

## Quick Usage

```bash
agentctl run codemap/generate --input '{
  "query": "how does authentication work",
  "depth": 2
}' --timeout 15m
```

## What are Codemaps?

Codemaps are structured documents that trace code paths through a codebase:

- **Traces**: Numbered sections following code flow
- **Trees**: ASCII visualization of code relationships
- **Annotations**: Labeled references to specific code locations

Example output structure:

```
Trace 1: Request Authentication Flow
├── internal/api/middleware.go [1a]
│   └── authMiddleware function
├── internal/auth/jwt.go [1b]
│   └── ValidateToken function
└── internal/user/service.go [1c]
    └── GetUserFromToken function
```

## Parameters

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `query` | string | required | Natural language question about code |
| `depth` | int | 1 | Trace depth (1-3, higher = more detail) |
| `path` | string | workspace | Starting directory for analysis |

## Timeout Guidance

Codemap generation uses AI agents and may take 5-20 minutes depending on query complexity:

```bash
# Simple query: ~5 minutes
agentctl run codemap/generate --input '{"query": "where is the config loaded"}' --timeout 5m

# Complex query: ~15 minutes
agentctl run codemap/generate --input '{"query": "trace the full auth flow", "depth": 2}' --timeout 15m
```

## Embedding Storage

Codemaps are stored with chunked embeddings (overview + per-trace) for semantic search:

```bash
# Search codemaps by semantic similarity
agentctl run code/semantic_search --input '{
  "query": "authentication",
  "scope": ["codemaps"]
}'
```

## Import Codemaps

Import existing `.codemap` files (including Windsurf-style codemaps) into the memory store:

```bash
# Import a single codemap file
agentctl run codemap/import --input '{
  "path": "docs/codemaps/Skill_Resolution__Input_Loading__and_Run_Execution_Flow_20260115_003130.codemap"
}'

# Import all codemaps in a directory
agentctl run codemap/import --input '{
  "path": "docs/codemaps",
  "recursive": false,
  "skip_existing": true
}'
```

## Output Format

```json
{
  "id": "01KDTG584CHNRE9H0WHNRWBX75",
  "title": "Authentication Flow",
  "description": "How user authentication works in the API",
  "query": "how does authentication work",
  "traces": [
    {
      "number": 1,
      "title": "JWT Token Validation",
      "summary": "Validates incoming JWT tokens...",
      "tree": "internal/auth/jwt.go\n├── ValidateToken [1a]\n...",
      "annotations": [
        {
          "label": "1a",
          "title": "ValidateToken function",
          "description": "Validates JWT signature and claims",
          "path": "@internal/auth/jwt.go:45"
        }
      ]
    }
  ],
  "file_count": 5,
  "symbol_count": 12
}
```

## Web UI

Browse codemaps via the web interface:

```bash
# Start web UI
cd web-ui && npm run dev
# Navigate to /codemaps
```

## Best Practices

1. **Start broad**: Use depth 1 for initial exploration
2. **Increase depth**: Use depth 2-3 for detailed traces
3. **Be specific**: Focused queries produce better results
4. **Allow time**: Complex queries may need 15+ minutes

## Examples

### Explore Data Flow

```bash
agentctl run codemap/generate --input '{
  "query": "how does data flow from API to database",
  "depth": 2
}' --timeout 10m
```

### Understand a Feature

```bash
agentctl run codemap/generate --input '{
  "query": "how does rate limiting work",
  "depth": 1
}' --timeout 5m
```

### Trace Error Handling

```bash
agentctl run codemap/generate --input '{
  "query": "how are errors handled in the API layer",
  "depth": 2
}' --timeout 10m
```
