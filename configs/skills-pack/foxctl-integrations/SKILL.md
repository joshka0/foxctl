---
name: foxctl-integrations
description: "Provider and integration glue for MCP setup, provider config sync, and OpenAPI-backed requests."
---

## What I do
- Keep Claude/Codex/OpenCode configs in sync.
- Provide a universal REST client via OpenAPI (dry-run first).

## Providers (sync config across tools)
```bash
foxctl run providers/config --input '{"operation":"list"}'
foxctl run providers/config --input '{"operation":"sync","sync_config":{"from":"claude","to":["codex","opencode"],"what":["mcp"]}}'
```

## MCP server (foxctl as MCP)
```bash
foxctl mcp serve --daemon --skills
foxctl mcp status
foxctl mcp stop
```

## OpenAPI (dry-run)
```bash
foxctl run http/openapi --input '{"spec":"memory:github","operationId":"listReposForUser","params":{"path":{"username":"octocat"}},"dry_run":true}'
```
