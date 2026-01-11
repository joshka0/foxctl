---
name: agentctl-integrations
description: "Provider + integration glue: MCP server setup, provider config sync, and OpenAPI requests."
---

## What I do
- Keep Claude/Codex/OpenCode configs in sync.
- Provide a universal REST client via OpenAPI (dry-run first).

## Providers (sync config across tools)
```bash
agentctl run providers/config --input '{"operation":"list"}'
agentctl run providers/config --input '{"operation":"sync","sync_config":{"from":"claude","to":["codex","opencode"],"what":["mcp"]}}'
```

## MCP server (agentctl as MCP)
```bash
agentctl mcp serve --daemon --skills
agentctl mcp status
agentctl mcp stop
```

## OpenAPI (dry-run)
```bash
agentctl run http/openapi --input '{"spec":"memory:github","operationId":"listReposForUser","params":{"path":{"username":"octocat"}},"dry_run":true}'
```
