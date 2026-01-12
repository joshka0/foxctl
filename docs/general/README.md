# agentctl Documentation

Detailed documentation for agentctl subsystems. For quick reference, see:
- [README.md](../../README.md) — Overview and quick start
- [AGENTS.md](../../AGENTS.md) — AI assistant contribution guide
- [.claude/CLAUDE.md](../../.claude/CLAUDE.md) — Claude Code integration

---

## Topics

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | System design, package structure, data flow |
| [Skills](skills.md) | Skill system, runners, manifest format |
| [Hooks](hooks.md) | Claude Code hook system, events, configuration |
| [Memory](memory.md) | Memory types, storage, vector search |
| [Sessions](sessions.md) | Session lifecycle, lineage, context preservation |
| [Storage](storage.md) | SQLite databases, CAS, vector stores |
| [Search](search.md) | Semantic search, embeddings, hybrid search |
| [Gotchas](gotchas.md) | Common pitfalls and their solutions |
| [v1 Specs](v1-specs.md) | Overview of foundational v1 specifications |

---

## Quick Navigation

### For AI Assistants
1. Read [AGENTS.md](../../AGENTS.md) for contribution conventions
2. Check [Gotchas](gotchas.md) before making changes
3. Reference [Architecture](architecture.md) for code locations

### For Claude Code Users
1. Read [.claude/CLAUDE.md](../../.claude/CLAUDE.md) for hooks and commands
2. Check [Skills](skills.md) for available tools
3. Reference [Memory](memory.md) for knowledge persistence

### For Contributors
1. Understand [Architecture](architecture.md)
2. Review [Skills](skills.md) for adding new skills
3. Check [Storage](storage.md) for database schemas
