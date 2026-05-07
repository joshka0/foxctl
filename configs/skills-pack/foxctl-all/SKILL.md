---
name: foxctl-all
description: "Single entrypoint for foxctl workflows: core, code, dev, orchestration, room collaboration, integrations, and mobile."
---

## Quick Reference

Run: `foxctl run <skill> --input '<json>'` for job-tracked JSON execution, or
`foxctl skills run <skill> --flag value` for direct parameter flags.
Help: `foxctl run <skill> --help` | Examples: `foxctl run <skill> --examples`
List: `foxctl skills list`

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
| `code/dag_grep` | Repo graph explanation subgraph (DAG view) |
| `repo/index_build` | Build or refresh repo graph index (incremental by default) |
| `repo/index_enrich_summaries` | Attach stored file/symbol summaries to repo graph nodes |
| `repo/index_search`, `repo/index_expand`, `repo/index_open` | Search, traverse, and inspect repo graph nodes |
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

### Room Coordination
| Skill | Purpose |
|-------|---------|
| `configs/skills-pack/foxctl-room/SKILL.md` | Durable shared room chat, room relay, room loop, room tasks |
| `configs/skills-pack/foxctl-room-agent/SKILL.md` | Participant-agent startup and day-to-day room behavior: membership checks, inbox, tasks, durable replies, reminders |
| `configs/skills-pack/foxctl-room-operator/SKILL.md` | How to operate inside an active room: status, inbox, task claim/touch/block/complete, coordinator escalation, review notes |
| `configs/skills-pack/foxctl-room-view/SKILL.md` | Viewer/presentation layer for room participants: tmux/zellij/gui PTY setup, pane reads, manual pokes |
| `configs/skills-pack/foxctl-epic-pipeline/SKILL.md` | Import Factory missions or plan files into canonical room epics, shape them through status/scout/grade/frontier, and stop before implementation |

### Context Architecture & History
For the bundled ACA/context-history workflow pack, see:

- `configs/skills-pack/foxctl-context/SKILL.md`

That pack covers:

- `foxctl context task-history-summary`
- `foxctl context family-history-summary`
- transcript-history persistence via `sessions derive-memory --memory-lane insight --persist-history`
- ACA / Obsidian bridge and graph flows
- `foxctl refactor scout` / `foxctl refactor advisor` when refactor discovery is part of the same context-workstream

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
| `memory/query` | Query canonical memory records by kind, file, or semantic search |
| `memory/curator_report` | Generate or explicitly apply memory lifecycle maintenance reports |

### Observability
| Skill | Purpose |
|-------|---------|
| `obs/logs` | Query foxcular events (filter by operation, command, status, component) |

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
| `optimize/bootstrap`, `optimize/reflect` | Agent optimization |

### Agent Orchestration
| Command | Purpose |
|---------|---------|
| `foxctl agent spawn` | Spawn persistent daemon agents |
| `foxctl agent list` | List all agents |
| `foxctl agent info <id>` | Agent details and status |
| `foxctl agent ask <id> --question "..." --wait` | Query an agent and wait for reply |
| `foxctl agent kill <id>` | Stop an agent |
| `foxctl agent resume <id> --prompt "..."` | Continue a previous session |
| `foxctl agent hierarchy` | Show agent tree structure |
| `foxctl agent watch <id>` | Live activity stream |

**Roles:** `overseer` (coordinator), `researcher`, `coder`, `planner`, `reviewer`

**Exec modes:** `reactive` (wait for messages), `autonomous` (run and exit), `autonomous_reactive` (research then stay alive), `proactive` (think cycles)

**Key spawn flags:**
- `--prompt "..."` - Inline prompt text
- `--role overseer|researcher|coder|planner|reviewer` - Agent role
- `--exec-mode autonomous_reactive` - Research then queryable
- `--max-iterations 20` - Tool call limit per engine run
- `--max-auto-turns 3` - Autonomous continuation turns
- `--max-context-tokens 30000` - Context budget (stops when exceeded)
- `--llm-provider openrouter` - LLM provider (auto-detect: openrouter→cerebras→groq→openai)
- `--llm-model openrouter/aurora-alpha` - Model name

**Research agent (replaces /foxctl-research for persistent use):**
```bash
foxctl agent spawn --role researcher \
  --prompt "Research the hook system architecture" \
  --exec-mode autonomous_reactive \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20
# Wait for research, then query:
foxctl agent ask <id> --question "What did you find?" --wait --timeout 120s
```

**Overseer with subagents:**
```bash
foxctl agent spawn --role overseer --prompt "Coordinate analysis of storage layer" \
  --exec-mode autonomous --max-auto-turns 5
```

**Session continuation:**
```bash
foxctl agent resume <session-id> --prompt "Based on your findings, explain X"
```

## Direct CLI Shortcuts

```bash
foxctl todo list|add|complete    # Task management
foxctl ci status --pr 123        # CI + comments + merge status
foxctl search "pattern"          # Quick ripgrep
foxctl memory list|get|put       # Named memories
foxctl index git-diff            # Index changed files
```

## Common Patterns

```bash
# Understand before editing
foxctl run code/symbols --input '{"path":"internal/agent/daemon/daemon.go"}'
foxctl run code/semantic_search --input '{"query":"task guard","format":"tree","limit":10}'

# Verify changes
foxctl run git/status
foxctl skills run test/run --path ./...
foxctl skills run ci/checks --pr 123

# Task workflow
foxctl todo add --title "Implement feature X"
foxctl todo list -f table
foxctl todo complete --id <id> --notes "Done"

# Debug recent errors
foxctl run obs/logs --input '{"errors_only": true, "since": "1h"}'
```
