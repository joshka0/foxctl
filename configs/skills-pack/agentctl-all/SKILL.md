---
name: agentctl-all
description: "Single condensed entrypoint for all agentctl workflows (core, code, dev, orchestrate, integrations, mobile)."
---

## Quick Reference

Run: `agentctl run <skill> --input '<json>'` or `agentctl run <skill> --flag value`
Help: `agentctl run <skill> --help` | Examples: `agentctl run <skill> --examples`
List: `agentctl skills list`

## Skills by Category

### Files & Search
| Skill | Purpose |
|-------|---------|
| `fs/tree` | Directory tree (gitignore-aware) |
| `fs/ls`, `fs/read`, `fs/write`, `fs/find` | File operations |
| `text/grep`, `text/replace` | Text ops |
| `code/context_grep` | Search + expand to full functions |
| `code/smart_search` | Smart code search |

### Code Intelligence
| Skill | Purpose |
|-------|---------|
| `code/symbols` | Extract functions/types/vars |
| `code/imports` | Dependency graph |
| `code/security` | Security scan |
| `code/semantic_search` | Vector search across symbols/codemaps/memories |
| `code/smart_write` | Symbol-based editing with diff preview |
| `code/diff` | Staged/unstaged/branch diffs |
| `code/git` | Blame, hotspots, history |

### LSP
| Skill | Purpose |
|-------|---------|
| `lsp/gopls` | Go: definitions, references, hover |
| `lsp/tsserver` | TypeScript/JS LSP |
| `lsp/pylsp` | Python LSP |

### Testing & CI
| Skill | Purpose |
|-------|---------|
| `test/run` | Run tests with coverage |
| `ci/checks` | CI status, failed checks |
| `ci/prcomments` | PR review comments (coderabbit, greptile, human) |
| `git/status` | Working tree status |
| `verification/cove_verify` | Chain-of-Verification for claims |

### Tasks & Sessions
| Skill | Purpose |
|-------|---------|
| `todo/manage` | Create/list/complete tasks |
| `todo/continuation` | Task gating for stop hooks |
| `session/save`, `session/restore` | Context preservation |
| `session/capture` | Snapshot current state |
| `session/summarize` | Generate session summary |
| `session/recall` | Search past sessions |
| `session/anchor` | Set durable session goal |
| `mailbox/manage` | Actor messaging (overseer inbox) |
| `graph/manage`, `graph/pagerank` | Task dependency graph |

### Codemaps & Indexing
| Skill | Purpose |
|-------|---------|
| `codemap/generate` | Generate semantic codemap |
| `codemap/get`, `codemap/list` | Retrieve codemaps |
| `embedding/queue` | Background embedding jobs |
| `code/incremental_index` | Index changed files |

### Memory
| Skill | Purpose |
|-------|---------|
| `memory/query` | Query gotchas/decisions/patterns by file or semantic search |

### Observability
| Skill | Purpose |
|-------|---------|
| `obs/logs` | Query wide events (filter by operation, command, status, component) |

### Integrations
| Skill | Purpose |
|-------|---------|
| `http/openapi` | Call OpenAPI endpoints (use `dry_run: true` first) |
| `mcp/bridge`, `mcp/install` | MCP server management |
| `data/jq`, `json/transform` | JSON manipulation |

### Mobile
| Skill | Purpose |
|-------|---------|
| `mobile/ios` | iOS Simulator automation |
| `mobile/android` | Android emulator automation |
| `mobile/expo` | Expo dev tools |

### Plans & Optimization
| Skill | Purpose |
|-------|---------|
| `plan/sync`, `plan/analyze_deps` | Plan file management |
| `optimize/bootstrap`, `optimize/reflect` | DSPy optimization |

### Agent Orchestration
| Command | Purpose |
|---------|---------|
| `agentctl agent spawn` | Spawn autonomous agents |
| `agentctl agent list` | List running agents |
| `agentctl agent kill <id>` | Terminate an agent |
| `agentctl agent resume <id>` | Continue a previous session |
| `agentctl agent hierarchy` | Show agent tree structure |
| `agentctl sessions list` | List agent sessions |

**Roles:** `overseer` (coordinator), `researcher`, `coder`, `planner`, `reviewer`

**Key spawn flags:**
- `--prompt "..."` - Inline prompt text
- `--role overseer|researcher|coder|planner|reviewer` - Agent role
- `--max-iterations 20` - Tool call limit
- `--max-context-tokens 30000` - Context budget (stops when exceeded)
- `--exec-mode autonomous` - Enable tool calling loop

**Multi-agent with overseer:**
```bash
# Overseer coordinates subagents
agentctl agent spawn --role overseer --prompt "Spawn researcher and coder to analyze X"
```

**Session continuation:**
```bash
# Resume with follow-up
agentctl agent resume <session-id> --prompt "Based on your findings, explain X"
```

## Direct CLI Shortcuts

```bash
agentctl todo list|add|complete    # Task management
agentctl ci status --pr 123        # CI + comments + merge status
agentctl search "pattern"          # Quick ripgrep
agentctl memory list|get|put       # Named memories
agentctl index git-diff            # Index changed files
```

## Common Patterns

```bash
# Understand before editing
agentctl run code/symbols --path internal/agent/daemon/daemon.go
agentctl run code/semantic_search --query "task guard" --format tree --limit 10

# Verify changes
agentctl run git/status
agentctl run test/run --path ./...
agentctl run ci/checks --pr 123

# Task workflow
agentctl todo add --title "Implement feature X"
agentctl todo list -f table
agentctl todo complete --id <id> --notes "Done"

# Debug recent errors
agentctl run obs/logs --input '{"errors_only": true, "since": "1h"}'
```
