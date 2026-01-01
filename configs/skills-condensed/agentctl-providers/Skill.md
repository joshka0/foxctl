---
name: agentctl Providers
description: Manage AI coding assistant configs across Claude, Codex, OpenCode, Factory, Gemini. Sync MCP servers, skills, and settings between providers.
---

# Provider Configuration

Unified config management for AI coding assistants.

## Providers

| Provider | Config | Skills | MCP Key |
|----------|--------|--------|---------|
| claude | `~/.claude.json` | `~/.claude/skills/` | `mcpServers` |
| codex | `~/.codex/config.toml` | `~/.codex/skills/` | `mcp_servers` |
| opencode | `~/.config/opencode/opencode.json` | `~/.config/opencode/agent/` | `mcpServers` |
| factory | `~/.factory/settings.json` | `~/.factory/droids/` | in `mcp.json` |
| gemini | `~/.gemini/settings.json` | N/A | `mcpServers` |

## Operations

```bash
# List all providers and their status
agentctl run providers/config --input '{"operation": "list"}'

# Add MCP server to Claude
agentctl run providers/config --input '{
  "operation": "add-mcp",
  "provider": "claude",
  "mcp": {"name": "github", "command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"]}
}'

# Add MCP to ALL providers
agentctl run providers/config --input '{
  "operation": "add-mcp",
  "provider": "all",
  "mcp": {"name": "filesystem", "command": "..."}
}'

# Sync MCP servers from Claude to others
agentctl run providers/config --input '{
  "operation": "sync",
  "sync_config": {"from": "claude", "to": ["codex", "factory", "gemini"], "what": ["mcp"]}
}'

# Add skill symlink
agentctl run providers/config --input '{
  "operation": "add-skill",
  "provider": "claude",
  "skill": {"name": "my-skill", "source": "/path/to/skill"}
}'

# Export config
agentctl run providers/config --input '{"operation": "export", "provider": "claude", "file": "backup.json"}'

# Set a config value
agentctl run providers/config --input '{
  "operation": "set",
  "provider": "claude",
  "setting": {"key": "permissions.deny", "value": [".env", "*.key"]}
}'
```

## Options

| Option | Description |
|--------|-------------|
| `dry_run` | Preview changes without applying |
| `provider` | Target: `claude`, `codex`, `opencode`, `factory`, `gemini`, or `all` |

Full docs: `~/repos/personal/agentctl/skills/providers/skill.yaml`
