---
name: agentctl OpenAPI
description: HTTP request planner using OpenAPI specs. Build and validate API requests with dry-run mode.
---

# OpenAPI Request Planner

Plan and validate HTTP requests against OpenAPI specs.

## Usage

```bash
agentctl run http/openapi --input '{
  "base_url": "https://api.example.com",
  "path": "/users/123",
  "method": "GET"
}'
```

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `base_url` | string | API base URL (required) |
| `path` | string | API endpoint (required) |
| `method` | string | HTTP method (default: GET) |
| `query` | object | Query parameters |
| `headers` | object | Request headers |
| `body` | object | Request body (POST/PUT) |
| `dry_run` | bool | Plan only (default: true) |

## Examples

```bash
# GET with query
agentctl run http/openapi --input '{
  "base_url": "https://api.github.com",
  "path": "/repos/owner/repo/issues",
  "query": {"state": "open", "per_page": 10}
}'

# POST with body
agentctl run http/openapi --input '{
  "base_url": "https://api.example.com",
  "path": "/users",
  "method": "POST",
  "body": {"name": "John", "email": "john@example.com"}
}'
```

Full docs: `~/.agentctl/share/configs/skills/agentctl-openapi/Skill.md`
