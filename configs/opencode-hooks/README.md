# agentctl OpenCode Plugin

OpenCode plugin for integrating agentctl capabilities.

## Installation

```bash
# Option 1: Copy to project
cp -r configs/opencode-hooks .opencode/plugin/agentctl

# Option 2: Add to opencode.json
{
  "plugin_directory": "configs/opencode-hooks"
}
```

## Architecture

### Critical Limitation: No Context Injection via Tool Hooks

Both Claude Code and OpenCode have the same limitation:
- `tool.execute.before` and `tool.execute.after` return `Promise<void>`
- They **cannot** inject context into the AI conversation
- They can only **block** execution by throwing errors

See [HOOK_ANALYSIS.md](./HOOK_ANALYSIS.md) for detailed research.

### How Context Injection Works

There are **two** ways to inject context in OpenCode:

#### 1. Experimental System Transform (Automatic)

```typescript
"experimental.chat.system.transform": async (input, output) => {
  // Mutate output.system array to add context
  output.system.push("## Custom Context\n...");
}
```

This runs before every AI turn. Use for:
- Active task summary
- Urgent messages
- File-specific gotchas

#### 2. Custom Tools (On-Demand)

```typescript
tool: {
  "agentctl-memory": tool({
    description: "Search memories",
    args: { query: z.string() },
    async execute({ query }) {
      return await searchMemories(query);
    }
  })
}
```

AI calls these explicitly. Use for:
- Detailed context on demand
- Efficient (only when needed)
- Rich data retrieval

## Plugin Features

### Custom Tools

| Tool | Description |
|------|-------------|
| `agentctl-memory` | Query gotchas, decisions, patterns |
| `agentctl-search` | Semantic vector search (format=`tree` for directory tree) |
| `agentctl-inbox` | Check overseer messages |
| `agentctl-symbols` | Get file structure |
| `agentctl-task` | Get active task |
| `agentctl-counsel` | Multi-perspective code analysis with LLM review |
| `agentctl-context` | Quick code context gathering (no LLM) |
| `agentctl-ripgrep` | Search code and return full function bodies |

### Slash Commands

| Command | Description | Example |
|---------|-------------|---------|
| `/anchor <goal>` | Set durable session goal | `/anchor Fix authentication bug` |
| `/todo` | Enable todo check-in mode | `/todo` |
| `/counsel <question>` | Run multi-perspective code analysis | `/counsel review auth flow for security` |
| `/context <query>` | Gather relevant code snippets | `/context database connection handling` |

### Anchors

Set a durable session goal from chat:
- `/anchor Fix stop gating after tasks complete`
- `anchor this: Fix stop gating after tasks complete`

The plugin stores the goal via `session/anchor` and strips the trigger from the message.

Use `/todo` to enable a lightweight todo check-in prompt (no graph analysis). `/todo` persists for ~6 hours per session.

### Code Analysis Commands

**`/counsel <question>`** - Multi-perspective LLM analysis (30-60s)
- Automatically finds relevant files
- Runs security, correctness, performance, and maintainability analyses
- Returns structured findings with severity and location
- Requires: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `CEREBRAS_API_KEY`

**`/context <query>`** - Quick context retrieval (5-15s)
- Finds relevant code snippets without LLM calls
- Returns file paths, line numbers, and code blocks
- Great for understanding how something is implemented

**Counsel Suggestion**: After reading 3+ code files, the plugin suggests using `/counsel` for deeper analysis.

### System Transform Context

Automatically injected every turn:
- Active task (if set)
- Urgent messages (priority >= 2)
- File-specific gotchas (for recently edited files)

### Tool Blocking

The plugin blocks:
- **Task Guard**: Edits without active task (when `AGENTCTL_TASK_GUARD_MODE=strict`)
- **Secret Scanner**: Writes containing AWS keys, private keys, PATs, API keys

### Session Events

| Event | Action |
|-------|--------|
| `session.created` | Warm up daemon |
| `session.idle` | Capture state, flush embeddings, sync plans, compute todo continuation |
| `file.edited` | Link file to active task |
| `experimental.session.compacting` | Save session state |

Note: todo continuation runs only when an anchor goal is set (via `/anchor`) or `/todo` mode is enabled.

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENTCTL_TASK_GUARD_MODE` | `auto` | Set to `strict` to require tasks |
| `AGENTCTL_HOME` | `~/.agentctl` | Storage root |
| `AGENTCTL_OPENCODE_IDLE_CAPTURE_MS` | `60000` | Min interval between idle captures (0 disables) |
| `AGENTCTL_OPENCODE_IDLE_FLUSH_MS` | `300000` | Min interval between embedding flushes (0 disables) |
| `AGENTCTL_OPENCODE_IDLE_PLAN_SYNC_MS` | `60000` | Min interval between plan sync runs (0 disables) |
| `AGENTCTL_OPENCODE_IDLE_TODO_MS` | `60000` | Min interval between todo continuation checks (0 disables) |

## Development

```bash
# Build
cd configs/opencode-hooks
bun install
bun build ./index.ts --outdir dist

# Test
bun test
```

## Files

```
configs/opencode-hooks/
├── index.ts           # Main plugin (custom tools + hooks)
├── lib/
│   └── agentctl.ts    # CLI wrapper utilities
├── HOOK_ANALYSIS.md   # Platform comparison research
└── README.md          # This file
```

## Legacy Shell Hooks

The original shell hooks in `configs/hooks/` are for Claude Code.
Most are non-functional due to Claude Code's PreToolUse limitation.

This OpenCode plugin replaces them with:
1. Custom tools (for context retrieval)
2. System transform (for automatic context)
3. Tool blocking (for security/workflow)
