# SPEC-019: Root README & Documentation

## Status
**Not Started** | Priority: Medium | Complexity: Low

## Problem Statement

No `README.md` at repository root. New users see only `AGENTS.md` which is for AI assistants.

## Proposed Solution

### README.md Structure

```markdown
# agentctl

**Bash for LLMs** - A single-binary CLI for structured, deterministic LLM workflows.

## Features
- 📦 **Structured I/O**: JSON envelopes (canonical, version 1)
- 🔧 **Skills**: Discoverable, sandboxed tools (WASI + exec)
- 💾 **Content-Addressable Storage**: SHA-256 integrity for large outputs
- 📋 **Jobs**: Async execution with durable state
- 🧠 **Memory**: Auto-cache (24h) + named persistent memories
- 🌐 **OpenAPI**: Universal REST API client (no codegen)

## Quick Start

\`\`\`bash
# Install
curl -sSL https://install.agentctl.dev | sh

# Run a skill
agentctl run fs/ls --path ./src

# Call an API
agentctl openapi import https://api.github.com/openapi.yaml --as github
agentctl run http/openapi --spec memory:github --operationId listRepos --params '{...}'

# Async job
agentctl jobs submit http/openapi --spec memory:github --operationId search
agentctl jobs tail <job-id>
\`\`\`

## Documentation
- [Core Profile v1 Spec](docs/spec/core_profile_v1.md)
- [OpenAPI Skill Guide](docs/spec/openapi_skill.md)
- [Skill Development](docs/skills/)
- [Contributing](CONTRIBUTING.md)

## Architecture
- **Language**: Go 1.22+
- **Storage**: SQLite + filesystem CAS
- **Runners**: WASI (wazero) preferred, exec fallback
- **CLI**: Cobra + Viper
```

### Additional Docs

- `CONTRIBUTING.md` - Development guide
- `docs/SECURITY.md` - Security policy
- `docs/TROUBLESHOOTING.md` - Common issues
- `docs/examples/` - Usage examples

## Implementation Plan

1. **Create README.md** (2h)
2. **Create CONTRIBUTING.md** (1h)
3. **Create SECURITY.md** (1h)
4. **Create TROUBLESHOOTING.md** (1h)

## Effort Estimate
**Total: 5 hours**
