# Hook Analysis: Port vs Native OpenCode Features

## Critical Finding: BOTH Claude Code AND OpenCode Cannot Inject Context via Tool Hooks

### Claude Code Limitation
**PreToolUse hooks do NOT support `additionalContext` injection.**
- [Open feature request #15345](https://github.com/anthropics/claude-code/issues/15345) (Dec 2025)
- Only PostToolUse, UserPromptSubmit, and SessionStart can inject context
- PreToolUse hooks outputting `{ context: "..." }` are **silently ignored**

### OpenCode Limitation (NEW DISCOVERY)
**`tool.execute.before` and `tool.execute.after` return `Promise<void>` - no context injection!**
- All hook return types are `Promise<void>` per [@opencode-ai/plugin types](https://github.com/sst/opencode)
- [Issue #3384](https://github.com/sst/opencode/issues/3384): "output is formed BEFORE we call tool.execute.after... original output will be returned"
- [Issue #3378](https://github.com/sst/opencode/issues/3378): Feature request for "Silent Message Insertion API" to inject context

### What DOES Work in OpenCode

Only **experimental hooks** can modify context via in-place mutation of the `output` parameter:

| Hook | Purpose | Mutation Target |
|------|---------|-----------------|
| `experimental.chat.system.transform` | Modify system prompt | `output.system: string[]` |
| `experimental.chat.messages.transform` | Transform messages | `output.messages` |
| `experimental.session.compacting` | Add compaction context | `output.context` |

### Recommended Pattern: File-Based Handoff

Since tool hooks can't inject context directly, use a temp file to pass context
from `tool.execute.before` to `experimental.chat.system.transform`:

```
tool.execute.before → write to temp file → system.transform reads → inject
```

**Implementation:**
```typescript
// 1. Tool hook writes context to temp file
"tool.execute.before": async (input, output) => {
  if (input.tool === "Edit") {
    const memories = await getFileMemories(input.args.file_path);
    await writePendingContext(sessionID, "File Memories", memories);
  }
}

// 2. System transform reads and injects
"experimental.chat.system.transform": async (input, output) => {
  const pending = await readAndClearPendingContext(sessionID);
  if (pending.length > 0) {
    output.system.push(`\n\n## Context\n${pending.join("\n")}`);
  }
}
```

**File location:** `~/.foxctl/cache/pending-context/<sessionID>.json`

**Session ID source:** Read from identity file at `~/.foxctl/sessions/active/`
(written by `session-identity.sh` hook or `foxctl session-id` command)

### Alternative: Custom Tools (Recommended)

Since hooks can't inject context, create tools the AI explicitly calls:

```typescript
tool: {
  "foxctl-context": tool({
    description: "Get relevant context for the current task",
    args: { query: z.string() },
    async execute({ query }) {
      const results = await runSkill("context/search", { query });
      return results.data; // AI sees this in tool response
    }
  })
}
```

### Broken Claude Code Hooks (context silently ignored)

| Hook | Event | Issue |
|------|-------|-------|
| `overseer-inbox.sh` | PreToolUse | Context not injected |
| `semantic-search.sh` | PreToolUse | Context not injected |
| `file-memory-recall.sh` | PreToolUse | Context not injected |
| `smart-read.sh` | PreToolUse | Context not injected |
| `knowledge-router.sh` | PreToolUse | Context not injected |
| `task-guard.sh` | PreToolUse | Only blocking works, not context |
| `security-scanner.sh` | PreToolUse | Only blocking works, not context |

**Updated Conclusion**: Both platforms have the same limitation. Focus on:
1. **Experimental hooks** for dynamic system prompt injection
2. **Custom tools** that the AI calls explicitly for context
3. **Tool blocking** (throwing errors) for security/workflow enforcement

---

## Summary

| Category | Port to Plugin | Use Native | Merge |
|----------|----------------|------------|-------|
| Core     | 6              | 0          | 0     |
| LSP/Analysis | 4          | 1          | 0     |
| Memory   | 4              | 1          | 0     |
| Session  | 3              | 2          | 0     |
| Sync     | 2              | 4          | 0     |
| Advisors | 1              | 1          | 2     |

**Total: 20 port, 9 native, 2 merge**

---

## Detailed Analysis

### PreToolUse Hooks

| Hook | Current Trigger | Recommendation | Rationale |
|------|-----------------|----------------|-----------|
| `overseer-inbox.sh` | Read/Bash/Grep/Glob/Task | **Port** | No native equivalent; critical for human-in-the-loop |
| `session-identity.sh` | Edit/Write/Bash | **Port** | No native; sets session env vars for attribution |
| `file-memory-recall.sh` | Edit/Write | **Port** | No native; queries foxctl memory |
| `task-guard.sh` | Edit/Write | **Port** | No native; enforces task-first workflow |
| `security-scanner.sh` | Edit/Write | **Port** | No native; scans for secrets |
| `smart-read.sh` | Read | **Port** | Valuable! Shows file structure (symbols, functions, line numbers) before reading - helps target specific sections |
| `bash-advisor.sh` | Read/Grep/Glob/Bash | **Merge** into skill-advisor | Redundant with skill-advisor |
| `semantic-search.sh` | Grep/Glob | **Port** | No native; augments with vector search |
| `smart-grep.sh` | Grep | **Port** | No native; expands results with context |
| `todo-advisor.sh` | TodoWrite | **Merge** into skill-advisor | Low value standalone |
| `knowledge-router.sh` | (not active) | **Port** | Routes context based on patterns; surfaces relevant knowledge packs |

### PostToolUse Hooks

| Hook | Current Trigger | Recommendation | Rationale |
|------|-----------------|----------------|-----------|
| `overseer-inbox-post.sh` | Read/Bash/Grep/Glob/Task | **Port** | Companion to pre-hook |
| `read-context-suggestions.sh` | Read | **Port** | Suggests context_ripgrep for symbols |
| `todo-sync.sh` | TodoWrite | **Native** `todo.updated` | OpenCode has native todo event |
| `memory-prompt.sh` | TodoWrite | **Port** | Prompts for memory save; no native |
| `lsp-diagnostics.sh` | Edit/Write | **Native** `lsp.client.diagnostics` | OpenCode has native LSP diagnostics |
| `test-feedback.sh` | Edit/Write | **Port** | No native; runs tests on edit |
| `complexity-warning.sh` | Edit/Write | **Port** | No native; uses foxctl skill |
| `impact-analysis.sh` | Edit/Write | **Port (async)** | Valuable! LSP-based dependency analysis. Run in background, surface on next interaction |
| `live-index.sh` | Edit/Write | **Native** `file.edited` | Use file event for indexing |
| `memory-capture.sh` | Edit/Write | **Native** `file.edited` | Trigger on file event |
| `memory-embed.sh` | Edit/Write | **Native** `file.edited` | Queue embeddings on file event |
| `task-file-link.sh` | Edit/Write | **Native** `file.edited` | Link files on edit event |

### PreCompact Hooks

| Hook | Current Trigger | Recommendation | Rationale |
|------|-----------------|----------------|-----------|
| `session-save.sh` | auto/manual | **Native** `experimental.session.compacting` | OpenCode has native compaction hook |

### SessionStart Hooks

| Hook | Current Trigger | Recommendation | Rationale |
|------|-----------------|----------------|-----------|
| `daemon-warmup.sh` | startup | **Port** | No native; warms up foxctl daemon |
| `session-identity.sh` | startup | **Port** | No native; establishes session |
| `session-restore.sh` | compact/resume | **Native** `session.created` | Use session event |

### Stop Hooks

| Hook | Current Trigger | Recommendation | Rationale |
|------|-----------------|----------------|-----------|
| `plan-sync.sh` | stop | **Port** | No native; syncs Claude plans to tasks |
| `embedding-flush.sh` | stop | **Port** | No native; flushes embedding queue |
| `session-capture.sh` | stop | **Native** `session.idle` | Capture on session idle |
| `graph-cleanup.sh` | stop | **Port** | No native; cleans graph data |
| `graph-pagerank.sh` | stop | **Port** | No native; computes PageRank |

### UserPromptSubmit Hooks

| Hook | Current Trigger | Recommendation | Rationale |
|------|-----------------|----------------|-----------|
| `memory-detector.sh` | all prompts | **Native** `message.updated` | Could use message event |
| `skill-advisor.sh` | all prompts | **Port** | Merge bash-advisor, todo-advisor here |

---

## OpenCode Native Events to Use

### `lsp.client.diagnostics`
**Replaces:** `lsp-diagnostics.sh`

```typescript
"lsp.client.diagnostics": async (event) => {
  const errors = event.diagnostics.filter(d => d.severity === "error");
  if (errors.length > 0) {
    const formatted = errors.map(e =>
      `Error [${e.range.start.line}:${e.range.start.character}] ${e.message}`
    ).join("\n");
    return { context: `**LSP Diagnostics:**\n\`\`\`\n${formatted}\n\`\`\`` };
  }
}
```

**Advantages:**
- Faster: No spawning external tools
- Unified: Works for all languages with LSP
- Real-time: Updates as you type, not just on edit

### `file.edited`
**Replaces:** `live-index.sh`, `memory-capture.sh`, `memory-embed.sh`, `task-file-link.sh`

```typescript
"file.edited": async (event) => {
  // Queue indexing
  await runSkill("index/file", { path: event.path });

  // Capture context
  await runSkill("memory/capture", { path: event.path });

  // Link to active task
  await runSkill("task/link_file", { path: event.path });
}
```

**Advantages:**
- Single event handler for all file-related operations
- Runs in background, doesn't slow down edit
- Cleaner architecture

### `todo.updated`
**Replaces:** `todo-sync.sh`

```typescript
"todo.updated": async (event) => {
  await runSkill("todo/sync", { todos: event.todos });
}
```

### `experimental.session.compacting`
**Replaces:** `session-save.sh`

```typescript
"experimental.session.compacting": async (event) => {
  const context = await runSkill("session/save", { session_id: event.session.id });
  return { context: context.data.summary };
}
```

### `session.created` / `session.idle`
**Replaces:** `session-restore.sh`, `session-capture.sh`

```typescript
"session.created": async (event) => {
  const restored = await runSkill("session/restore", { session_id: event.session.id });
  if (restored.data?.context) {
    return { context: restored.data.context };
  }
},

"session.idle": async (event) => {
  await runSkill("session/capture", { session_id: event.session.id });
}
```

### `message.updated`
**Could replace:** `memory-detector.sh`

```typescript
"message.updated": async (event) => {
  if (event.message.role !== "user") return;

  const patterns = {
    save: /remember this|the trick is|TIL|watch out|don't forget/i,
    recall: /how did we|didn't we already|last time we/i,
  };

  const content = event.message.content;
  if (patterns.save.test(content)) {
    return { context: "**Hint:** Use `/remember` to save this insight." };
  }
}
```

---

## Final Hook List for OpenCode

### Must Port (20 plugins)

| Plugin | Priority | Notes |
|--------|----------|-------|
| `core/task-guard.ts` | P0 | Critical for workflow |
| `core/overseer-inbox.ts` | P0 | Human-in-the-loop |
| `core/semantic-search.ts` | P1 | Vector search augmentation |
| `core/smart-grep.ts` | P1 | Context expansion |
| `core/security-scanner.ts` | P1 | Secret detection |
| `core/smart-read.ts` | P1 | File structure preview before reading |
| `core/knowledge-router.ts` | P1 | Route context based on patterns |
| `memory/file-memory-recall.ts` | P0 | Surface memory records |
| `memory/memory-prompt.ts` | P2 | Prompt to save |
| `session/session-identity.ts` | P0 | Attribution |
| `session/daemon-warmup.ts` | P1 | Startup perf |
| `analysis/test-feedback.ts` | P1 | Test on edit |
| `analysis/complexity-warning.ts` | P2 | Complexity alerts |
| `analysis/read-context.ts` | P2 | Symbol suggestions |
| `analysis/impact-analysis.ts` | P2 | **Async** LSP dependency analysis |
| `sync/plan-sync.ts` | P1 | Plans → tasks |
| `sync/embedding-flush.ts` | P1 | Flush queue |
| `graph/graph-cleanup.ts` | P2 | Cleanup |
| `graph/graph-pagerank.ts` | P2 | Analytics |
| `advisors/skill-advisor.ts` | P1 | Merged advisor (bash + todo + skill) |

### Use Native (9 handlers)

| Event | Replaces | Priority |
|-------|----------|----------|
| `lsp.client.diagnostics` | lsp-diagnostics.sh | P0 |
| `file.edited` | live-index, memory-capture, memory-embed, task-file-link | P1 |
| `todo.updated` | todo-sync.sh | P1 |
| `experimental.session.compacting` | session-save.sh | P1 |
| `session.created` | session-restore.sh | P1 |
| `session.idle` | session-capture.sh | P2 |
| `message.updated` | memory-detector.sh (optional) | P2 |

### Merge (2 hooks)

| Hook | Action |
|------|--------|
| `bash-advisor.sh` | Merge into skill-advisor |
| `todo-advisor.sh` | Merge into skill-advisor |

---

## Legacy Python Hooks (~/.claude/hooks/)

These are older experiments that overlap with current shell hooks:

| Hook | Description | Status |
|------|-------------|--------|
| `pre_edit_ai_review.py` | Cerebras AI code review before edits | Superseded by lsp-diagnostics |
| `post_quality_hook.py` | Formatter/linter/type-checker/tests | Superseded by test-feedback |
| `tool_call_logger.py` | Logging tool calls | Keep for debugging |
| `get_context.py` | Context gathering | Superseded by semantic-search |

**Recommendation**: Archive these Python hooks; functionality now in foxctl skills.

---

## Revised Migration Strategy

Given that BOTH platforms cannot inject context via tool hooks, the strategy shifts to:

### Approach 1: Experimental System Prompt Transform (Recommended)

Use `experimental.chat.system.transform` to inject context before EVERY AI turn:

```typescript
"experimental.chat.system.transform": async (input, output) => {
  const sessionID = input.sessionID;

  // 1. Get active task context
  const task = await runSkill("task/active", {});

  // 2. Get relevant memory records for conversation
  const memoryRecords = await runSkill("memory/query", { session_id: sessionID });

  // 3. Get overseer messages
  const messages = await runSkill("mailbox/unread", { recipient: "overseer" });

  // 4. Inject into system prompt
  const context = [];
  if (task.data) context.push(`## Active Task\n${task.data.title}`);
  if (memoryRecords.data?.records?.length) context.push(`## Relevant Memory Records\n${formatMemoryRecords(memoryRecords.data.records)}`);
  if (messages.data?.length) context.push(`## Overseer Messages\n${formatMessages(messages.data)}`);

  if (context.length > 0) {
    output.system.push("\n\n" + context.join("\n\n"));
  }
}
```

**Pros**: Context visible to AI on every turn
**Cons**: Added latency per turn, system prompt grows

### Approach 2: Custom Tools (Alternative)

Create tools the AI calls explicitly:

| Tool | Replaces Hook | Description |
|------|---------------|-------------|
| `foxctl-memory-recall` | file-memory-recall | Get file-specific memories |
| `foxctl-search` | semantic-search | Vector search codebase |
| `foxctl-inbox` | overseer-inbox | Check human messages |
| `foxctl-context` | smart-read | Get file structure/symbols |

**Pros**: AI decides when to call, efficient
**Cons**: Relies on AI remembering to call tools

### Approach 3: Hybrid (Best)

Combine both:
1. **Lightweight system transform**: Just active task + urgent messages
2. **Rich custom tools**: For detailed context on demand
3. **Tool blocking**: For security/workflow enforcement

---

## Migration Order (Revised)

### Phase 1: System Prompt Transform (P0)
Create `context-injector.ts` using `experimental.chat.system.transform`:
- Active task summary
- Urgent overseer messages
- File-specific memory records (when editing)

### Phase 2: Custom Tools (P0)
Create tools that AI calls explicitly:
1. `foxctl-memory` - Query memories
2. `foxctl-search` - Semantic search
3. `foxctl-task` - Task management
4. `foxctl-inbox` - Check messages

### Phase 3: Tool Blocking (P1)
Tool hooks that throw errors to block:
1. `task-guard.ts` - Block edits without active task
2. `security-scanner.ts` - Block secret leaks

### Phase 4: Native Events (P1)
Side-effect only handlers:
1. `file.edited` → Queue indexing, link to task
2. `lsp.client.diagnostics` → Log errors (can't inject)
3. `session.idle` → Capture state

### Phase 5: Session Lifecycle (P2)
1. `session.created` → Restore context via system transform
2. `experimental.session.compacting` → Save state
3. `session.idle` → Flush embeddings, sync plans
