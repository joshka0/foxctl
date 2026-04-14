# Context Buffer Design

## Problem

Current "pending context" approach uses temp files (`~/.foxctl/cache/pending-context/<session>.json`) which creates:
- Race conditions between hooks writing and transforms reading
- No TTL enforcement (manual expiry checks)
- No deduplication (same context can accumulate)
- No priority ordering
- Bespoke logic duplicated in shell scripts and TypeScript

## Solution: Context Buffer Store

A SQLite-based queue that hooks enqueue to, and inject-capable events drain from.

---

## SQL Schema

```sql
-- File: internal/storage/contextbuffer/schema.sql

CREATE TABLE IF NOT EXISTS context_entries (
    id              TEXT PRIMARY KEY,           -- ULID
    workspace_id    TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    agent_id        TEXT NOT NULL DEFAULT '',   -- Optional, for multi-agent

    -- Content
    source          TEXT NOT NULL,              -- e.g., "smart-grep", "overseer-inbox"
    text            TEXT NOT NULL,              -- Markdown content
    priority        INTEGER NOT NULL DEFAULT 2, -- 1=high, 2=normal, 3=low

    -- Deduplication
    dedupe_hash     TEXT,                       -- SHA256(source + text), nullable

    -- Lifecycle
    created_at_ms   INTEGER NOT NULL,
    expires_at_ms   INTEGER NOT NULL,           -- TTL enforcement
    consumed_at_ms  INTEGER,                    -- NULL until drained

    -- Metadata
    metadata        TEXT                        -- JSON for extensibility
);

-- Query patterns
CREATE INDEX idx_context_pending ON context_entries(
    workspace_id, session_id, consumed_at_ms, expires_at_ms, priority, created_at_ms
);
CREATE INDEX idx_context_dedupe ON context_entries(workspace_id, session_id, dedupe_hash)
    WHERE consumed_at_ms IS NULL;
CREATE INDEX idx_context_cleanup ON context_entries(expires_at_ms, consumed_at_ms);
```

---

## Go Store API

```go
// File: internal/storage/contextbuffer/store.go

package contextbuffer

import (
    "context"
    "time"
)

// Entry represents a context buffer entry
type Entry struct {
    ID          string            `json:"id"`
    WorkspaceID string            `json:"workspace_id"`
    SessionID   string            `json:"session_id"`
    AgentID     string            `json:"agent_id,omitempty"`
    Source      string            `json:"source"`
    Text        string            `json:"text"`
    Priority    int               `json:"priority"` // 1=high, 2=normal, 3=low
    CreatedAt   time.Time         `json:"created_at"`
    ExpiresAt   time.Time         `json:"expires_at"`
    ConsumedAt  *time.Time        `json:"consumed_at,omitempty"`
    Metadata    map[string]any    `json:"metadata,omitempty"`
}

// EnqueueParams for adding context
type EnqueueParams struct {
    WorkspaceID string
    SessionID   string
    AgentID     string            // Optional
    Source      string            // Required: identifies the hook/origin
    Text        string            // Required: markdown content
    Priority    int               // 1-3, default 2
    TTL         time.Duration     // Default 60s
    Dedupe      bool              // If true, hash and skip if exists
    Metadata    map[string]any    // Optional extensibility
}

// DrainParams for retrieving context
type DrainParams struct {
    WorkspaceID   string
    SessionID     string
    AgentID       string   // Optional: filter by agent
    Sources       []string // Optional: filter by source
    MinPriority   int      // Optional: only priority <= this (1=high)
    Limit         int      // Max entries to return (default 50)
    MarkConsumed  bool     // If true, mark as consumed (default true)
}

// DrainResult from a drain operation
type DrainResult struct {
    Entries      []Entry `json:"entries"`
    TotalPending int     `json:"total_pending"` // Remaining after drain
    Markdown     string  `json:"markdown"`      // Pre-rendered output
}

// Store interface
type Store interface {
    // Enqueue adds context to the buffer
    // If Dedupe=true and matching hash exists (unconsumed), updates timestamp only
    Enqueue(ctx context.Context, params EnqueueParams) (*Entry, error)

    // Drain retrieves and optionally marks entries as consumed
    // Returns entries ordered by priority ASC, created_at ASC
    Drain(ctx context.Context, params DrainParams) (*DrainResult, error)

    // Peek is like Drain but never marks consumed
    Peek(ctx context.Context, params DrainParams) (*DrainResult, error)

    // PruneExpired removes expired + old consumed entries
    PruneExpired(ctx context.Context, maxAge time.Duration) (int, error)

    // Count returns pending entries for a session
    Count(ctx context.Context, workspaceID, sessionID string) (int, error)
}
```

---

## Dispatcher Output JSON

The `hooks/dispatch` skill produces structured output that callers can act on:

```go
// File: skills/hooks_dispatch/types.go

// DispatchOutput is the unified output from hooks/dispatch
type DispatchOutput struct {
    // Decision for the hook event
    Decision    string `json:"decision"`     // "approve", "block", "skip"
    BlockReason string `json:"block_reason,omitempty"`

    // Context to inject immediately (if provider supports it)
    ImmediateContext string `json:"immediate_context,omitempty"`

    // Actions taken (for observability)
    Actions []Action `json:"actions,omitempty"`

    // Provider capabilities (from input, echoed for clarity)
    Provider ProviderCapabilities `json:"provider"`
}

// Action represents something the dispatcher did
type Action struct {
    Type     string         `json:"type"`     // "enqueue_context", "inject_context", "block", etc.
    Source   string         `json:"source"`   // Which hook module
    Priority int            `json:"priority,omitempty"`
    TTL      string         `json:"ttl,omitempty"` // Duration string
    Preview  string         `json:"preview,omitempty"` // First 100 chars
}

// ProviderCapabilities describes what the calling provider can do
type ProviderCapabilities struct {
    Name              string `json:"name"`               // "claude-code", "opencode"
    Event             string `json:"event"`              // "PreToolUse", "UserPromptSubmit", etc.
    CanInjectContext  bool   `json:"can_inject_context"` // Can this event inject?
    CanBlock          bool   `json:"can_block"`          // Can this event block?
}
```

### Example dispatcher input/output

**Input** (from Claude Code PreToolUse):
```json
{
  "provider": {
    "name": "claude-code",
    "event": "PreToolUse",
    "can_inject_context": true,
    "can_block": true
  },
  "workspace_id": "/Users/dev/project",
  "session_id": "session-abc123",
  "tool": "Read",
  "args": { "file_path": "/src/auth.go" }
}
```

**Output**:
```json
{
  "decision": "approve",
  "immediate_context": "## File Memories: auth.go\n- **gotcha-auth**: Watch out for...",
  "actions": [
    {
      "type": "enqueue_context",
      "source": "file-memory-recall",
      "priority": 2,
      "ttl": "5m",
      "preview": "## File Memories: auth.go..."
    },
    {
      "type": "inject_context",
      "source": "file-memory-recall",
      "preview": "## File Memories: auth.go..."
    }
  ],
  "provider": {
    "name": "claude-code",
    "event": "PreToolUse",
    "can_inject_context": true,
    "can_block": true
  }
}
```

**Input** (from OpenCode PostToolUse - can't inject):
```json
{
  "provider": {
    "name": "opencode",
    "event": "PostToolUse",
    "can_inject_context": false,
    "can_block": false
  },
  "workspace_id": "/Users/dev/project",
  "session_id": "session-abc123",
  "tool": "Read",
  "result": { "file_path": "/src/auth.go" }
}
```

**Output**:
```json
{
  "decision": "approve",
  "actions": [
    {
      "type": "enqueue_context",
      "source": "counsel-suggest",
      "priority": 3,
      "ttl": "1m",
      "preview": "Tip: You've read 3+ code files..."
    }
  ],
  "provider": {
    "name": "opencode",
    "event": "PostToolUse",
    "can_inject_context": false,
    "can_block": false
  }
}
```

---

## Drain Skill

A dedicated skill for draining the buffer (used by inject-capable events):

```go
// File: skills/hooks_context_drain/main.go

// Input
type Input struct {
    WorkspaceID string   `json:"workspace_id"`
    SessionID   string   `json:"session_id"`
    AgentID     string   `json:"agent_id,omitempty"`
    Sources     []string `json:"sources,omitempty"`     // Filter by source
    MinPriority int      `json:"min_priority,omitempty"` // 1=high only
    Peek        bool     `json:"peek,omitempty"`         // Don't consume
    Format      string   `json:"format,omitempty"`       // "markdown" (default), "json"
}

// Output
type Output struct {
    Markdown     string `json:"markdown,omitempty"`
    Entries      []contextbuffer.Entry `json:"entries,omitempty"`
    Count        int    `json:"count"`
    TotalPending int    `json:"total_pending"`
}
```

---

## Integration Points

### Claude Code (`configs/hooks/foxctl-hook.sh`)

```bash
#!/bin/bash
# Unified hook dispatcher for Claude Code

PROVIDER='{"name":"claude-code","event":"'"$HOOK_EVENT"'","can_inject_context":true,"can_block":true}'

# Adjust capabilities by event
case "$HOOK_EVENT" in
  "PostToolUse")
    PROVIDER='{"name":"claude-code","event":"PostToolUse","can_inject_context":true,"can_block":false}'
    ;;
  "Stop")
    PROVIDER='{"name":"claude-code","event":"Stop","can_inject_context":true,"can_block":true}'
    ;;
esac

# Run dispatcher
result=$(cat | jq --argjson provider "$PROVIDER" '. + {provider: $provider}' | \
  foxctl run hooks/dispatch --ephemeral --no-cas)

# Output decision + context
echo "$result" | jq '{
  decision: .decision,
  context: .immediate_context
}'
```

### OpenCode (`configs/opencode-hooks/index.ts`)

```typescript
// In tool.execute.before - enqueue only (can't inject)
"tool.execute.before": async (input, output) => {
  const result = await runSkill("hooks/dispatch", {
    provider: {
      name: "opencode",
      event: "PreToolUse",
      can_inject_context: false, // OpenCode can't inject here
      can_block: true
    },
    workspace_id: workspace,
    session_id: input.sessionID,
    tool: input.tool,
    args: output.args
  });

  if (result.data?.decision === "block") {
    throw new Error(result.data.block_reason);
  }
  // Context was enqueued internally - will be drained later
}

// In system.transform - drain and inject
"experimental.chat.system.transform": async (input, output) => {
  const drained = await runSkill("hooks/context_drain", {
    workspace_id: workspace,
    session_id: input.sessionID,
    format: "markdown"
  });

  if (drained.data?.markdown) {
    output.system.push(`\n\n---\n## Hook Context\n${drained.data.markdown}`);
  }
}
```

---

## Migration Path

1. **Phase 1**: Create `internal/storage/contextbuffer` with schema + store
2. **Phase 2**: Create `hooks/context_drain` skill
3. **Phase 3**: Update `hooks/dispatch` to use Context Buffer (enqueue + conditional drain)
4. **Phase 4**: Update OpenCode plugin to use `hooks/context_drain` in `system.transform`
5. **Phase 5**: Remove temp file logic from OpenCode plugin
6. **Phase 6**: Update Claude Code hooks to use unified dispatcher

---

## Files to Create/Modify

| File | Action | Purpose |
|------|--------|---------|
| `internal/storage/contextbuffer/store.go` | Create | Context Buffer store implementation |
| `internal/storage/contextbuffer/schema.sql` | Create | SQLite schema |
| `skills/hooks_context_drain/main.go` | Create | Drain skill for inject-capable events |
| `skills/hooks_dispatch/main.go` | Modify | Add Context Buffer integration |
| `configs/opencode-hooks/index.ts` | Modify | Use hooks/dispatch + context_drain |
| `configs/hooks/foxctl-hook.sh` | Create | Unified Claude Code hook adapter |

---

## Observability

Emit wide events for:
- `hook.context.enqueue` - source, priority, ttl, text_bytes
- `hook.context.drain` - count, sources, total_bytes, consumer
- `hook.context.prune` - expired_count, consumed_count

---

## Benefits

1. **No temp files** - SQLite handles persistence, TTL, deduplication
2. **Priority ordering** - High-priority context surfaces first
3. **Deduplication** - Same context won't accumulate
4. **Observable** - Wide events for debugging
5. **Multi-agent safe** - Proper scoping by workspace/session/agent
6. **Unified** - Same pattern for Claude Code and OpenCode
