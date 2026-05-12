---
title: Providers and MCP
description: Configure LLM providers, run the MCP server, and understand the init.sh bootstrap flow.
---

foxctl connects to LLM providers (OpenRouter, Cerebras, Groq, OpenAI, Anthropic)
for agent and companion work, and exposes skills and tools through an MCP server
for editor and IDE integration.

This page covers provider wiring, MCP serving, and the `scripts/init.sh`
bootstrap that ties them together.

## LLM Providers

### Provider Priority

foxctl auto-detects available providers by checking for API keys in the
environment. The detection order is:

1. **OpenRouter** — `OPENROUTER_API_KEY`
2. **Cerebras** — `CEREBRAS_API_KEY`
3. **Groq** — `GROQ_API_KEY`
4. **OpenAI** — `OPENAI_API_KEY`
5. **Anthropic** — `ANTHROPIC_API_KEY`

The first available key wins. You can override the provider and model
explicitly when spawning agents:

```bash
foxctl agent spawn --role researcher \
  --llm-provider openrouter \
  --llm-model openrouter/aurora-alpha \
  --prompt "Research the hook system"
```

### Default Models

| Provider | Default model | Notes |
|----------|--------------|-------|
| OpenRouter | `openrouter/aurora-alpha` | Free tier available |
| Context updater | `qwen/qwen3-coder-next` | Used for session context refresh |

### Environment Variables

Store API keys in `~/.foxctl/.env` or export them in your shell:

```bash
export OPENROUTER_API_KEY="sk-or-..."
export CEREBRAS_API_KEY="csk-..."
export GROQ_API_KEY="gsk_..."
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
```

`scripts/init.sh` copies the repo `.env` to `~/.foxctl/.env` if present and no
global env file exists yet.

### Checking Provider Status

```bash
foxctl auth --help
```

Use `foxctl auth` to inspect which providers are configured and test
connectivity.

## MCP Server

The MCP (Model Context Protocol) server exposes foxctl skills and tools to
editors and IDEs. Agents and skills can be called directly from supported
editors.

### Basic Commands

```bash
# Check MCP server status
foxctl mcp status

# Start MCP server with skills enabled
foxctl mcp serve --skills

# Start as a shared daemon (recommended for local use)
foxctl mcp serve --daemon --skills
```

The `--daemon` flag runs the MCP server as a background process, allowing
multiple editor sessions to share a single foxctl backend.

The `--skills` flag mounts all installed skills as MCP tools, making them
available to connected editors.

### MCP Use Cases

| Use case | How |
|----------|-----|
| Editor integration for code search | Start MCP daemon, connect editor plugin |
| Running skills from IDE | Skills mount as MCP tools via `--skills` |
| Shared agent sessions | Daemon mode allows persistent sessions across editor windows |
| Codex integration | MCP config created by `scripts/init.sh` |

## Provider Bootstrap with `scripts/init.sh`

`scripts/init.sh` is the current source of truth for local provider wiring. It
automates the full setup from a fresh clone to a working development
environment.

### What init.sh Does

1. **Symlinks `foxctl` into `~/.local/bin`** — makes the CLI available on PATH
2. **Creates directory structure** — `~/.foxctl/{storage,cache,cas,skills,jobs,observability,backups}`
3. **Installs provider skill packs** — from `configs/skills-pack`
4. **Configures Claude Code** — hooks and settings in `.claude/settings.json`
5. **Wires OpenCode** — plugin, skills, and agents
6. **Creates Codex MCP config** — skill links and MCP endpoint
7. **Links Gemini skill packs** — connects Gemini to foxctl skills
8. **Starts the shared MCP daemon** — `foxctl mcp serve --daemon --skills`

### Running the Bootstrap

```bash
# Full bootstrap (from repo root)
./scripts/init.sh

# Or via make
make build
make skills-install
./scripts/init.sh
```

The `make build` target compiles the CLI. `make skills-install` (or
`make skills-install-all`) installs all skill binaries. Then `init.sh` wires up
the provider integrations.

### Target Environments

The bootstrap currently targets these environments:

- **Claude Code** — hook registration in `.claude/settings.json`, skill mounting
- **Codex** — MCP config file, skill symlinks
- **OpenCode** — plugin wiring, skill packs, agent configs
- **Gemini** — skill pack linking

### Storage Directories

`init.sh` creates the foxctl storage tree under `~/.foxctl/`:

| Directory | Purpose |
|-----------|---------|
| `storage/` | SQLite/libsql databases |
| `cache/` | Spec caches, temporary files |
| `cas/` | Content-addressable storage for large artifacts |
| `skills/` | Installed skill binaries |
| `jobs/` | Job history and trajectory metadata |
| `observability/` | Event logs and debug traces |
| `backups/` | Database and state backups |

## Auth Wiring

### Secret Sources

Provider credentials are read from, in order of preference:

1. **Environment variables** — `FOXCTL_*` prefixed or provider-specific names
2. **Files under `/run/secrets/<name>`** — for container/daemon deployments
3. **`~/.foxctl/.env`** — local development

### Safety Rules

- Provider credentials must never appear in logs or docs output
- Secret values are redacted as `"***"` in all envelope output, dry-run
  responses, and job metadata
- Prefer typed provider configuration over ad hoc environment reads
- Document whether a provider path is local-only, daemon-backed, or remote

### Production Boundaries

- Do not imply a plugin or provider is production-ready if its current source is
  still plan-backed
- In container deployments, mount secrets at `/run/secrets/<name>` rather than
  baking them into images
- The MCP daemon runs locally by default; remote deployment requires additional
  auth and network configuration

## Cross-References

- [OpenAPI and Plugins](/integrations/openapi-and-plugins) — using API specs as
  skill sources
- [Skills Runtime](/skills/runtime-and-install) — how skills are executed and
  installed
- [Hooks](/integrations/hooks) — editor and CI event integration
- [Agents Lifecycle](/agents/lifecycle) — spawning agents with specific
  providers and models
