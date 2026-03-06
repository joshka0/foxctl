# agentctl Documentation

Detailed documentation for agentctl subsystems. For quick reference, see:
- [README.md](../../README.md) — Overview and quick start
- [docs/README.md](../README.md) — Documentation map
- [AGENTS.md](../../AGENTS.md) — AI assistant contribution guide
- [.claude/CLAUDE.md](../../.claude/CLAUDE.md) — Claude Code integration

---

## Topics

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Architecture entry point with canonical pointers |
| [Auth & Identity Architecture](../architecture/auth-identity.md) | Canonical identity, authorization, auth broker, and verification architecture |
| [Runtime Orchestration](runtime-orchestration.md) | Agent/runtime execution pipeline and run lifecycle |
| [API Server](api-server.md) | Current `/api` surface, route groups, and transport notes |
| [Agent Policy & Prompts](agent-policy-and-prompts.md) | Capability profiles and role instruction model |
| [Context & Observability](context-and-observability.md) | Context updater and observability primitives |
| [Core Package Coverage](core-package-coverage.md) | Coverage matrix for core `internal/*` concepts |
| [Skills](skills.md) | Skill system, runners, manifest format |
| [Hooks](hooks.md) | Claude Code hook system, events, configuration |
| [Memory](memory.md) | Memory types, storage, vector search |
| [Sessions](sessions.md) | Session lifecycle, lineage, context preservation |
| [Storage](storage.md) | SQLite databases, CAS, vector stores |
| [Search](search.md) | Semantic search, embeddings, hybrid search |
| [Repo Index](repoindex.md) | Repo graph index, navigation, and queries |
| [Gotchas](gotchas.md) | Common pitfalls and their solutions |
| [v1 Specs](v1-specs.md) | Overview of foundational v1 specifications |

---

## Quick Navigation

### For AI Assistants
1. Read [AGENTS.md](../../AGENTS.md) for contribution conventions
2. Check [Gotchas](gotchas.md) before making changes
3. Reference [Architecture](architecture.md) and [Core Package Coverage](core-package-coverage.md) for code locations and doc coverage

### For Claude Code Users
1. Read [.claude/CLAUDE.md](../../.claude/CLAUDE.md) for hooks and commands
2. Check [Skills](skills.md) for available tools
3. Reference [Runtime Orchestration](runtime-orchestration.md) and [Memory](memory.md) for execution + persistence behavior

### For Contributors
1. Start with [Architecture](architecture.md) and canonical docs under `docs/architecture/`
2. Review [Core Package Coverage](core-package-coverage.md) before adding new subsystem docs
3. Check [Storage](storage.md) and [Skills](skills.md) for implementation-facing details
