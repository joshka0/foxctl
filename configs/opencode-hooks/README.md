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
| `agentctl-search` | Semantic vector search |
| `agentctl-inbox` | Check overseer messages |
| `agentctl-symbols` | Get file structure |
| `agentctl-task` | Get active task |

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
| `session.idle` | Capture state, flush embeddings, sync plans |
| `file.edited` | Link file to active task |
| `experimental.session.compacting` | Save session state |

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENTCTL_TASK_GUARD_MODE` | `auto` | Set to `strict` to require tasks |
| `AGENTCTL_HOME` | `~/.agentctl` | Storage root |

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
