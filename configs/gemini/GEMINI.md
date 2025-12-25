# Global Gemini Context

## Core Principles
- Context > Speed - understand before acting
- Type-safe > Fast - correctness over velocity
- Research first - check docs, existing patterns, gotchas

## Error Handling
**NEVER dismiss errors as unrelated.** Fix now if quick, otherwise add to TODO.

---

## agentctl Integration

agentctl is available for structured code analysis and automation. Use it for precise, deterministic operations.

### Running Skills
```bash
# Basic usage
bin/agentctl run <skill> --input '<json>'

# Examples
bin/agentctl run code/complexity --input '{"path": "src/"}'
bin/agentctl run code/symbols --input '{"path": "main.go"}'
bin/agentctl run text/grep --input '{"pattern": "TODO", "path": "."}'
```

### Key Skills

| Skill | Purpose | Example Input |
|-------|---------|---------------|
| `code/complexity` | Cyclomatic/cognitive complexity | `{"path": "."}` |
| `code/symbols` | Extract functions, types, methods | `{"path": "file.go"}` |
| `code/security` | Vulnerability detection | `{"path": "src/"}` |
| `code/imports` | Dependency graph analysis | `{"path": "."}` |
| `code/swe_grep` | Smart code retrieval | `{"question": "auth flow", "paths": ["src/"]}` |
| `text/grep` | Regex search | `{"pattern": "func.*Error", "path": "."}` |
| `text/ripgrep` | Fast recursive search | `{"pattern": "TODO", "path": "."}` |
| `fs/tree` | Directory structure | `{"path": ".", "max_depth": 3}` |
| `fs/read` | Read file contents | `{"path": "README.md"}` |
| `test/run` | Run tests with coverage | `{"path": "./...", "coverage": true}` |
| `lsp/gopls` | Go definitions/references | `{"operation": "definition", "file": "main.go", "line": 10}` |
| `git/status` | Git repo status | `{}` |
| `todo/manage` | Task management | `{"action": "list"}` |

### Task Management
```bash
bin/agentctl todo add --title "Task" --description "Details"
bin/agentctl todo list
bin/agentctl todo active
bin/agentctl todo complete --id <id> --notes "Done"
```

### Output Format
All skills return JSON envelope:
```json
{
  "status": "ok",
  "data": { ... },
  "error": {}
}
```
Check `status` field, extract useful info from `data`.

### When to Use agentctl
- **Precise searches**: Use `text/grep` or `code/swe_grep` over manual searching
- **Code analysis**: Use `code/complexity`, `code/symbols` for structured analysis
- **File operations**: Use `fs/tree`, `fs/read` for exploration
- **Tests**: Use `test/run` for running tests with proper output parsing

---

## Custom Commands

### Analysis
- `/pre-impl <target>` - Pre-implementation analysis
- `/complexity <target>` - Code complexity check
- `/review <target>` - Code review
- `/explain <target>` - Detailed explanation
- `/security <path>` - Security vulnerability scan

### agentctl Integration (runs shell commands)
- `/symbols <path>` - Extract code symbols via agentctl
- `/grep <pattern>` - Search with ripgrep via agentctl
- `/tree <path>` - Directory structure via agentctl
- `/imports <path>` - Dependency analysis via agentctl
- `/todos` - Show agentctl tasks
- `/agentctl <skill>` - Run any agentctl skill
- `/test <target>` - Generate tests

## Project Files
- `AGENTS.md` - Protocol, profiles, invariants
- `CLAUDE.md` - Claude Code integration
- `GEMINI.md` - Gemini-specific context

## Memories
- Use NativeWind wherever possible (React Native)
- Refer to CLAUDE.md and AGENTS.md for guidance
