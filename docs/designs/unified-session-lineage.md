# Unified Session Lineage

## Problem Statement

Agents need reliable session identity and lineage tracking to:

1. **Resume correctly** - Pick up where they left off after
   compaction/disconnect
2. **See long-horizon context** - Access work from days/weeks of connected
   sessions
3. **Coordinate safely** - Multiple agents (overseer, claude, subagents) working
   in same workspace
4. **Route artifacts** - Messages, memories, and summaries tagged to correct
   session

## Principles

| Principle               | Implementation                                                                                              |
| ----------------------- | ----------------------------------------------------------------------------------------------------------- |
| **Stable IDs**          | ULIDs for sessions; never derive from temp paths                                                            |
| **Explicit lineage**    | `parent_session_id` + edge_type forms session graph                                                         |
| **Workspace scoping**   | Always scope by `workspace_id` (hashed path)                                                                |
| **Agent identity**      | Tag sessions with `agent_id`; one active per (workspace, agent_id)                                          |
| **Artifact routing**    | All messages/artifacts tagged with `session_id`                                                             |
| **Cross-agent clarity** | Use a registry of `agent_id` values (e.g., `agentctl`, `opencode`, `claude`, `windsurf`, `subagent:<name>`) |

## Data Model

### Sessions Table (Enhanced)

```sql
-- Add to existing sessions table
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT;
ALTER TABLE sessions ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'agentctl';
ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'running';
  -- status: 'queued' | 'running' | 'ok' | 'error' | 'canceled'
ALTER TABLE sessions ADD COLUMN started_at TEXT;
ALTER TABLE sessions ADD COLUMN updated_at TEXT;

-- Indexes for common queries
-- Existing PK on id assumed; ensure UNIQUE(id) or PK(id) remains
CREATE INDEX idx_sessions_workspace_agent_started
  ON sessions(workspace, agent_id, started_at DESC);
CREATE INDEX idx_sessions_workspace_agent_status
  ON sessions(workspace, agent_id, status);
CREATE INDEX idx_sessions_parent
  ON sessions(workspace, parent_session_id);
```

### Session Edges Table (New)

```sql
CREATE TABLE session_edges (
    id TEXT PRIMARY KEY,              -- ULID
    workspace TEXT NOT NULL,
    from_session TEXT NOT NULL,       -- Session ULID
    to_session TEXT NOT NULL,         -- Session ULID (parent/related)
    edge_type TEXT NOT NULL,          -- 'continues' | 'forked_from' | 'relates_to'
    created_at TEXT NOT NULL,
    metadata TEXT,                    -- JSON for additional context

    UNIQUE(from_session, to_session, edge_type, workspace),
    FOREIGN KEY (from_session) REFERENCES sessions(id),
    FOREIGN KEY (to_session) REFERENCES sessions(id)
);

CREATE INDEX idx_session_edges_to
  ON session_edges(workspace, to_session);
CREATE INDEX idx_session_edges_type
  ON session_edges(workspace, edge_type);
```

### Edge Types

| Edge Type     | Meaning                                  | Created When                   |
| ------------- | ---------------------------------------- | ------------------------------ |
| `continues`   | Direct continuation after compact/resume | SessionStart with parent       |
| `forked_from` | Parallel work branch                     | Subagent spawn, explicit fork  |
| `relates_to`  | Weak link (same workspace, recent time)  | Fresh start near prior session |

## Session Lifecycle

### Start Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        Session Start                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Generate session_id (ULID)                                  │
│  2. Set workspace_id = hash(canonical_path)                     │
│  3. Set agent_id (from caller or default "agentctl")            │
│  4. Check for active session in (workspace, agent_id)           │
│     └─► If exists: require --new-session or auto-close          │
│  5. Set parent_session_id (optional)                            │
│  6. Determine edge_type:                                        │
│     ├─► "continues" if resuming                                 │
│     ├─► "forked_from" if branching/subagent                     │
│     └─► NULL if fresh start                                     │
│  7. INSERT session + session_edge                               │
│  8. Export AGENTCTL_SESSION_ID, AGENTCTL_WORKSPACE to env       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Resume Flow

```go
func ResumeSession(workspace, agentID string, explicitSessionID string) (*Session, error) {
    var prior *Session

    if explicitSessionID != "" {
        // Explicit session ID provided
        prior = LoadSession(explicitSessionID)
    } else {
        // Find last session for (workspace, agent_id)
        prior = FindLastSession(workspace, agentID, []string{"running", "ok"})
    }

    if prior == nil {
        return nil, ErrNoSessionToResume
    }

    // Create new session continuing from prior
    newSession := &Session{
        ID:              ulid.Make().String(),
        Workspace:       workspace,
        AgentID:         agentID,
        ParentSessionID: prior.ID,
        Status:          "running",
        StartedAt:       time.Now(),
    }

    // Create continuation edge
    edge := &SessionEdge{
        ID:          ulid.Make().String(),
        Workspace:   workspace,
        FromSession: newSession.ID,
        ToSession:   prior.ID,
        EdgeType:    "continues",
        CreatedAt:   time.Now(),
    }

    SaveSession(newSession)
    SaveEdge(edge)

    return newSession, nil
}
```

### Branch/Subagent Flow

```go
func ForkSession(parentSession *Session, childAgentID string) (*Session, error) {
    child := &Session{
        ID:              ulid.Make().String(),
        Workspace:       parentSession.Workspace,  // Inherit workspace
        AgentID:         childAgentID,             // New agent identity
        ParentSessionID: parentSession.ID,
        Status:          "running",
        StartedAt:       time.Now(),
    }

    edge := &SessionEdge{
        ID:          ulid.Make().String(),
        Workspace:   parentSession.Workspace,
        FromSession: child.ID,
        ToSession:   parentSession.ID,
        EdgeType:    "forked_from",
        CreatedAt:   time.Now(),
    }

    SaveSession(child)
    SaveEdge(edge)

    return child, nil
}
```

### Close Flow

```go
func CloseSession(sessionID string, status string) error {
    // status: "ok" | "error" | "canceled"
    return UpdateSession(sessionID, map[string]any{
        "status":     status,
        "updated_at": time.Now(),
    })
    // Keep session and edges for history; never delete
}
```

## Multi-Agent Safety

### One Active Session Rule

```sql
-- Within a transaction before creating new session
SELECT id, status FROM sessions
WHERE workspace = :workspace
  AND agent_id = :agent_id
  AND status = 'running'
LIMIT 1;

-- If found and not --force/--new-session:
--   Error: "Active session exists for agent X in workspace Y"
--   Suggest: --continue to resume, --new-session to start fresh
```

### Mailbox Routing

```go
// All mailbox messages include session context
type Message struct {
    ID        string `json:"id"`
    SessionID string `json:"session_id"`  // Required
    AgentID   string `json:"agent_id"`    // Required
    Workspace string `json:"workspace"`
    // ... other fields
}

// Read filters by session (optionally include parent chain)
func ReadMailbox(sessionID, agentID string, includeParents bool) ([]Message, error) {
    sessionIDs := []string{sessionID}

    if includeParents {
        ancestors := GetAncestorChain(sessionID, 5) // bounded depth
        for _, a := range ancestors {
            sessionIDs = append(sessionIDs, a.ID)
        }
    }

    return QueryMessages(sessionIDs, agentID, workspace) // workspace filter required
}
```

### Context Fetch

```go
// Prepare context for prompt - only artifacts from session lineage
func GetSessionContext(sessionID string, depth int) (*Context, error) {
    sessions := GetAncestorChain(sessionID, depth)

    ctx := &Context{
        CurrentSession: sessionID,
        Ancestors:      sessions,
    }

    // Fetch artifacts tagged to these sessions
    ctx.Memories = GetMemoriesForSessions(sessionIDs(sessions))
    ctx.Summaries = GetSummariesForSessions(sessionIDs(sessions))
    ctx.Tasks = GetTasksForSessions(sessionIDs(sessions))

    return ctx, nil
}
```

## Environment Propagation

### Runner Context

```go
// runner sets these for all skill executions
func (r *Runner) Execute(skill string, input []byte) (*Result, error) {
    env := os.Environ()
    env = append(env,
        "AGENTCTL_SESSION_ID="+r.sessionID,
        "AGENTCTL_WORKSPACE="+r.workspace,
        "AGENTCTL_AGENT_ID="+r.agentID,
    )

    // Pass through exec/wasi
    return r.exec(skill, input, env)
}
```

### Hook Access

```bash
#!/usr/bin/env bash
# Hooks receive session context via environment

session_id="$AGENTCTL_SESSION_ID"
workspace="$AGENTCTL_WORKSPACE"
agent_id="$AGENTCTL_AGENT_ID"

# Use in skill calls
agentctl run session/capture --input "{
  \"session_id\": \"$session_id\",
  \"workspace\": \"$workspace\"
}"
```

## Session Identity File

For hooks without env access (e.g., some shell contexts), maintain a file:

**Location:** `~/.agentctl/sessions/active/<workspace_hash>.json`

```json
{
  "session_id": "01HXYZ...",
  "agent_id": "agentctl",
  "workspace": "/Users/user/repos/project",
  "workspace_hash": "a1b2c3d4",
  "parent_session_id": "01HABC...",
  "started_at": "2024-01-01T10:00:00Z",
  "last_activity": "2024-01-01T12:30:00Z"
}
```

**Lifecycle:**

- Written on session start
- Updated on activity (edit/read)
- Read on session restore if env not available
- Permissions: chmod 600; remove/rotate on close to avoid stale resumes

## CLI Commands

```bash
# List sessions for workspace
agentctl session list [--agent-id <id>] [--status running|ok|error]

# Start new session
agentctl session new [--agent-id <id>] [--parent <session-id>]

# Resume last session (creates 'continues' edge)
agentctl session resume [--agent-id <id>] [--session <id>]

# Fork from existing session (creates 'forked_from' edge)
agentctl session fork --parent <session-id> [--agent-id <id>]

# Show session lineage
agentctl session chain [--session <id>] [--depth 5]

# Close current session
agentctl session close [--status ok|error|canceled]
```

## Integration Points

### Unified Graph

Session edges integrate with `graph_edges`:

```sql
-- Session → Symbol (touched during session)
INSERT INTO graph_edges (from_id, to_id, edge_type, workspace, ...)
VALUES ('session:01HXYZ', 'symbol:abc123:Login', 'touched', ...);

-- Session → Task (worked on during session)
INSERT INTO graph_edges (from_id, to_id, edge_type, workspace, ...)
VALUES ('session:01HXYZ', 'task:01HDEF', 'worked_on', ...);
```

### Search Scoping

```go
// Search scopes to current session + ancestors for long-horizon context
func SemanticSearch(query string, sessionID string) ([]Result, error) {
    ancestors := GetAncestorChain(sessionID, 10)
    sessionIDs := sessionIDs(ancestors)

    // Filter results to session lineage
    results := VectorSearch(query)
    return FilterBySessionLineage(results, sessionIDs)
}
```

### Summarization

```go
// Per-session summaries with lineage section
type SessionSummary struct {
    SessionID    string   `json:"session_id"`
    Summary      string   `json:"summary"`
    ParentID     string   `json:"parent_id,omitempty"`
    SiblingIDs   []string `json:"sibling_ids,omitempty"`  // Other forks from same parent
    Accomplishments []string `json:"accomplishments"`
    ActiveWork   []string `json:"active_work"`
}
```

## Ancestor Chain Query

```sql
WITH RECURSIVE ancestors AS (
    -- Start with current session
    SELECT s.*, 0 AS depth
    FROM sessions s
    WHERE s.id = :current_session_id

    UNION ALL

    -- Walk parent chain
    SELECT s.*, a.depth + 1
    FROM sessions s
    JOIN session_edges e ON e.to_session = s.id
    JOIN ancestors a ON e.from_session = a.id
    WHERE e.edge_type IN ('continues', 'forked_from')
      AND a.depth < :max_depth
)
SELECT id, workspace, agent_id, status, summary, started_at, depth
FROM ancestors
ORDER BY depth ASC;
```

## Platform Hooks (Claude Code / OpenCode / Windsurf)

- Agent ID mapping:
  - OpenCode: `agent_id=opencode` when AGENT=1/OPENCODE=1 detected.
  - Claude Code: `agent_id=claude`.
  - Windsurf: `agent_id=windsurf`.
  - Subagents: `agent_id=subagent:<name>` when spawned by overseer.
- Hooks export or recover session identity:
  - Prefer env (AGENTCTL_SESSION_ID, AGENTCTL_WORKSPACE, AGENTCTL_AGENT_ID).
  - If missing, read `~/.agentctl/sessions/active/<workspace_hash>.json`; if
    absent, start a new session and write the file.
- Start/resume:
  - On start/resume, call `agentctl session new|resume` with the mapped
    agent_id; create `continues` edge on resume, `forked_from` on subagent
    spawn.
- Close:
  - On stop, close session with status ok/error; remove or rotate identity file.
- Exec/WASI propagation:
  - Runners must pass the env through to child skills so downstream hooks get
    session/agent/workspace.

## Maintenance & Integrity

- Orphan edges: run periodic cleanup to delete session_edges that reference
  missing sessions (SQLite FKs will not cascade across tables).
- Active check: perform the “one active per (workspace, agent_id)” check
  atomically in a transaction to avoid races.
- Resume ordering: prefer (workspace, agent_id, started_at DESC) index for “last
  session” lookup.

## Implementation Phases

### Phase 1: Data Model

1. Add `parent_session_id`, `agent_id`, `status` to sessions table
2. Create `session_edges` table with indexes
3. Migration for existing sessions (set agent_id='agentctl', status='ok')
   - Backfill `started_at`/`updated_at` from existing timestamps if present to
     preserve ordering.
   - Ensure `id` remains PK/UNIQUE; add workspace/agent indexes.
   - CLI: `agentctl session migrate` (idempotent) to apply columns/indexes and
     backfill timestamps.

### Agent ID Registry (convention)

- Reserved: `agentctl`, `opencode`, `claude`, `windsurf`
- Subagents: `subagent:<name>` (spawned children)
- Allow explicit override via `AGENTCTL_AGENT_ID`

### Phase 2: Runner/Environment

1. Export `AGENTCTL_SESSION_ID`, `AGENTCTL_WORKSPACE`, `AGENTCTL_AGENT_ID` in
   runner
2. Pass through exec/wasi contexts
3. Update skill library to read from env

### Phase 3: CLI

1. `agentctl session new|resume|fork|list|chain|close`
2. Enforce one active session per (workspace, agent_id) unless --force
3. Session identity file for fallback

### Phase 4: Mailbox Integration

1. Add `session_id`, `agent_id` columns to messages
2. Filter reads by session (+ optional parent chain)
3. Update mailbox skills

### Phase 5: Hooks

1. session-capture writes with session_id
2. session-summarize creates summaries with lineage
3. Create session_edges on resume/fork

### Phase 6: Search/Context

1. Scope session results to current lineage
2. GetSessionContext for prompt preparation
3. Ancestor chain for long-horizon queries
   - Default max ancestor depth: 5 (configurable)

## Relationship to Other Designs

| Design                        | Relationship                                                                |
| ----------------------------- | --------------------------------------------------------------------------- |
| `unified-dependency-graph.md` | Session edges feed into graph_edges; PageRank flows through session lineage |
| `session-continuity.md`       | Superseded; key concepts merged here                                        |
| Overseer/Multi-agent          | Agent identity enables safe coordination                                    |

## Success Metrics

1. **Lineage coverage**: % of sessions with parent_session_id set
2. **Resume success**: % of resumes finding correct prior session
3. **Multi-agent safety**: 0 conflicts from concurrent sessions per (workspace,
   agent_id)
4. **Context depth**: Average ancestor chain depth for context retrieval
5. **Artifact routing**: % of messages/memories correctly tagged with session_id

## Test Plan (outline)

- Data model migration: run `agentctl session migrate`; verify columns/indexes;
  backfill of started_at/updated_at.
- Start/resume/close: create session, resume (creates continues edge), close
  with status updates.
- Fork/subagent: fork from parent; ensure edge_type forked_from and
  agent_id=subagent:<name>.
- Concurrency: two concurrent starts for same (workspace, agent_id) — second
  should fail without --new-session/--force.
- Env propagation: verify AGENTCTL_SESSION_ID/WORKSPACE/AGENT_ID reach hooks and
  skills (exec/wasi).
- Mailbox routing: writes tagged with session_id/agent_id/workspace; reads
  filtered by lineage + workspace.
- Identity file: created with chmod 600; removed/rotated on close; used when env
  missing.
- Ancestor chain traversal: create chain of 7 sessions (A→B→C→D→E→F→G); query
  from G with max_depth=5; verify returns G,F,E,D,C,B in depth order (A excluded);
  query with max_depth=10; verify full chain returned; verify edge_type filter
  (continues vs forked_from) works correctly.
