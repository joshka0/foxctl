# Session Continuity: Long-Horizon Agent Memory

## Problem Statement

Claude Code sessions are ephemeral:
- **Compact** → Context summarized, new session starts
- **Resume** → Continues session but loses pre-compact context
- **Fresh start** → No connection to previous work

**Result:** Agents can't see what happened across compactions. Work history is fragmented.

**Goal:** Enable agents to see work across a long time horizon - days, weeks, months of connected sessions.

## Current State

### What We Have
```
sessions.db
├── sessions (id, workspace, summary, embedding, ...)
├── session_turns (id, session_id, content, ...)
└── NO parent/child relationships
```

### What's Missing
- No `parent_session_id` → can't build lineage
- No session identity file → can't track "current session" across hooks
- No chain traversal → can't query "all my ancestor sessions"

## Proposed Solution

### 1. Session Chain Model

```
Session A (root, chain_depth=0)
    │
    ├── compact (context summarized)
    │
    ▼
Session B (parent=A, chain_depth=1)
    │
    ├── compact
    │
    ▼
Session C (parent=B, chain_root=A, chain_depth=2)
    │
    ├── fresh start next day (new chain)
    │
    ▼
Session D (root, chain_depth=0)  ← New chain, but can link via "related"
```

### 2. Schema Changes

```sql
-- Add to sessions table
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT;
ALTER TABLE sessions ADD COLUMN chain_root_id TEXT;
ALTER TABLE sessions ADD COLUMN chain_depth INTEGER DEFAULT 0;
ALTER TABLE sessions ADD COLUMN continuation_type TEXT;  -- 'compact', 'resume', 'branch', 'root'

-- Index for chain traversal
CREATE INDEX idx_sessions_parent ON sessions(parent_session_id);
CREATE INDEX idx_sessions_chain_root ON sessions(chain_root_id);
```

### 3. Session Identity File

Per-workspace persistent file that survives compaction:

**Location:** `~/.agentctl/sessions/active/<workspace_hash>.json`

```json
{
  "session_id": "01HXYZ...",
  "chain_root_id": "01HABC...",
  "chain_depth": 2,
  "workspace": "/Users/user/repos/project",
  "workspace_hash": "a1b2c3d4",
  "started_at": "2024-01-01T10:00:00Z",
  "last_activity": "2024-01-01T12:30:00Z",
  "active_task_id": "01HDEF...",
  "active_task_title": "Implement auth module",
  "files_touched": ["src/auth.go", "src/auth_test.go"],
  "topic_tags": ["auth", "middleware", "tests"]
}
```

**Lifecycle:**
1. **SessionStart(new)** → Check for existing file, create new if none/stale
2. **SessionStart(compact)** → Read parent, create child session
3. **SessionStart(resume)** → Read and continue same session
4. **Stop** → Update last_activity, capture summary
5. **Edit/Read** → Update files_touched

### 4. Hook Integration

#### session-restore.sh (SessionStart)

```bash
#!/usr/bin/env bash
# Enhanced to establish session lineage

trigger="$1"  # compact, resume, startup
workspace_hash=$(echo -n "$CLAUDE_PROJECT_DIR" | sha256sum | cut -c1-16)
identity_file="$HOME/.agentctl/sessions/active/${workspace_hash}.json"

case "$trigger" in
  compact)
    # Read previous session
    prev=$(cat "$identity_file" 2>/dev/null || echo '{}')
    parent_id=$(jq -r '.session_id // empty' <<< "$prev")
    chain_root=$(jq -r '.chain_root_id // .session_id // empty' <<< "$prev")
    chain_depth=$(jq -r '(.chain_depth // 0) + 1' <<< "$prev")

    # Create new session as child
    new_session_id=$(ulid)

    # Write new identity
    cat > "$identity_file" <<EOF
{
  "session_id": "$new_session_id",
  "parent_session_id": "$parent_id",
  "chain_root_id": "${chain_root:-$new_session_id}",
  "chain_depth": $chain_depth,
  "workspace": "$CLAUDE_PROJECT_DIR",
  "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "continuation_type": "compact"
}
EOF

    # Create graph edge
    if [[ -n "$parent_id" ]]; then
      agentctl run graph/add_edge --input "{
        \"from_id\": \"session:$new_session_id\",
        \"to_id\": \"session:$parent_id\",
        \"edge_type\": \"continues\",
        \"workspace\": \"$CLAUDE_PROJECT_DIR\"
      }"
    fi

    # Get ancestor chain summaries for context injection
    agentctl run session/chain --input "{
      \"session_id\": \"$new_session_id\",
      \"include_summaries\": true,
      \"limit\": 5
    }"
    ;;

  resume)
    # Continue existing session, just update activity
    jq '.last_activity = "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"' \
      "$identity_file" > "${identity_file}.tmp" && mv "${identity_file}.tmp" "$identity_file"
    ;;

  startup)
    # Fresh start - check for recent sessions in same workspace
    if [[ -f "$identity_file" ]]; then
      last_activity=$(jq -r '.last_activity // ""' "$identity_file")
      hours_ago=$(( ($(date +%s) - $(date -d "$last_activity" +%s 2>/dev/null || echo 0)) / 3600 ))

      if [[ $hours_ago -lt 24 ]]; then
        # Recent session exists - could prompt to continue
        # For now, create as "related" not "continues"
        prev_id=$(jq -r '.session_id' "$identity_file")
        # ... create new session with "related" edge
      fi
    fi

    # Create new root session
    new_session_id=$(ulid)
    cat > "$identity_file" <<EOF
{
  "session_id": "$new_session_id",
  "chain_root_id": "$new_session_id",
  "chain_depth": 0,
  "workspace": "$CLAUDE_PROJECT_DIR",
  "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "continuation_type": "root"
}
EOF
    ;;
esac
```

#### session-capture.sh (Stop)

```bash
# Enhanced to record parent_session_id
workspace_hash=$(echo -n "$CLAUDE_PROJECT_DIR" | sha256sum | cut -c1-16)
identity_file="$HOME/.agentctl/sessions/active/${workspace_hash}.json"

# Read current session identity
identity=$(cat "$identity_file" 2>/dev/null || echo '{}')
session_id=$(jq -r '.session_id' <<< "$identity")
parent_id=$(jq -r '.parent_session_id // null' <<< "$identity")
chain_root=$(jq -r '.chain_root_id' <<< "$identity")
chain_depth=$(jq -r '.chain_depth // 0' <<< "$identity")

# Pass to session/capture skill
agentctl run session/capture --input "{
  \"session_id\": \"$session_id\",
  \"parent_session_id\": $parent_id,
  \"chain_root_id\": \"$chain_root\",
  \"chain_depth\": $chain_depth,
  \"workspace\": \"$CLAUDE_PROJECT_DIR\"
}"
```

### 5. New Skills

#### session/chain - Get Ancestor Chain

```go
// Input
type Input struct {
    SessionID        string `json:"session_id"`
    Direction        string `json:"direction"`  // "ancestors", "descendants", "both"
    IncludeSummaries bool   `json:"include_summaries"`
    Limit            int    `json:"limit"`
}

// Output
type Output struct {
    Chain []SessionSummary `json:"chain"`
    ChainDepth int         `json:"chain_depth"`
    RootSession string     `json:"root_session"`
    Timeline    string     `json:"timeline"`  // Human-readable
}
```

**SQL for ancestor chain:**
```sql
WITH RECURSIVE ancestors AS (
  SELECT * FROM sessions WHERE id = :current_id
  UNION ALL
  SELECT s.* FROM sessions s
  JOIN ancestors a ON s.id = a.parent_session_id
)
SELECT id, summary, started_at, chain_depth
FROM ancestors
ORDER BY chain_depth DESC
LIMIT :limit;
```

#### session/context - Long-Horizon Context

```go
// Input
type Input struct {
    SessionID string `json:"session_id"`
    Scope     string `json:"scope"`  // "chain", "week", "month", "topic"
    Topic     string `json:"topic"`  // For topic-based retrieval
}

// Output: Aggregated context from multiple sessions
type Output struct {
    Summary        string   `json:"summary"`       // AI-generated summary of work
    Accomplishments []string `json:"accomplishments"`
    KeyDecisions   []string `json:"key_decisions"`
    ActiveWork     []string `json:"active_work"`
    Gotchas        []string `json:"gotchas"`
    Timeline       []TimelineEntry `json:"timeline"`
}
```

### 6. Graph Edges for Sessions

| Edge Type | From | To | Meaning |
|-----------|------|-----|---------|
| `continues` | Session B | Session A | B is compaction of A |
| `resumes` | Session B | Session A | B explicitly resumed A |
| `branches` | Session B | Session A | B is parallel work from A |
| `related` | Session B | Session A | Same workspace, recent timeframe |
| `topic` | Session B | Session A | Share topic tags (weak link) |

### 7. Reconnection Strategy

**Problem:** Fresh start - how do we know to continue previous work?

**Solution: Multi-signal matching**

```go
func findSessionToContinue(workspace string) (*Session, string) {
    // 1. Check identity file
    if identity := readIdentityFile(workspace); identity != nil {
        if hoursAgo(identity.LastActivity) < 24 {
            return loadSession(identity.SessionID), "identity_file"
        }
    }

    // 2. Check for active tasks in workspace
    if task := getActiveTask(workspace); task != nil {
        if session := getLastSessionForTask(task.ID); session != nil {
            return session, "active_task"
        }
    }

    // 3. Check mailbox for handoff messages
    if msg := getHandoffMessage(workspace); msg != nil {
        return loadSession(msg.SessionID), "mailbox_handoff"
    }

    // 4. Check recent sessions by time
    if session := getMostRecentSession(workspace, 24*time.Hour); session != nil {
        return session, "recent_time"
    }

    return nil, "new_chain"
}
```

**Mailbox handoff message:**
```json
{
  "type": "session_handoff",
  "from": "session:01HXYZ",
  "workspace": "/path/to/repo",
  "active_task": "01HDEF",
  "topic_tags": ["auth", "middleware"],
  "summary": "Working on auth middleware, 3 tests remaining",
  "created_at": "2024-01-01T12:00:00Z"
}
```

On SessionStart, check for handoff → inject context → create "continues" edge.

### 8. Long-Horizon Queries

With this system, agents can answer:

**"What have I been working on?"**
```
→ Get current session chain
→ Aggregate summaries from ancestors
→ Show timeline of work
```

**"Did we solve the auth bug before?"**
```
→ Semantic search across session summaries
→ Find sessions mentioning "auth bug"
→ Show resolution from that session
```

**"What decisions did we make last week?"**
```
→ Query sessions by time range + workspace
→ Extract decisions field
→ Aggregate and dedupe
```

**"Continue where I left off"**
```
→ Find most recent session chain
→ Get active task from chain
→ Get last files touched
→ Inject as context
```

## Implementation Phases

### Phase 1: Session Identity (Foundation)
1. Create `~/.agentctl/sessions/active/` directory structure
2. Update `session-restore.sh` to write identity file
3. Update `session-capture.sh` to read and pass identity
4. Add `parent_session_id`, `chain_root_id`, `chain_depth` to sessions table

### Phase 2: Chain Traversal
1. Create `session/chain` skill for ancestor/descendant queries
2. Add `continues` edge type to graph
3. Hook integration for edge creation on compact

### Phase 3: Long-Horizon Context
1. Create `session/context` skill for aggregated context
2. Enhance `session-restore` to inject chain summaries
3. Add topic extraction and tagging

### Phase 4: Smart Reconnection
1. Implement multi-signal matching on fresh start
2. Add mailbox handoff messages
3. Create "related" edges for workspace+time matches

### Phase 5: UI/CLI
1. `agentctl session chain` - Show session lineage
2. `agentctl session context` - Get long-horizon summary
3. `agentctl session handoff` - Create handoff message

## Relationship to Unified Graph

Session continuity integrates with the unified dependency graph:

```
Session Chain (temporal)          Task Graph (logical)
     │                                  │
     ▼                                  ▼
Session A ──continues──► Session B    Task X ──depends_on──► Task Y
     │                      │            │                      │
     │                      │            │                      │
     └─── touched ────► Symbol Z ◄─── modified ────────────────┘
```

- Sessions connect to symbols via `touched`/`modified`
- Sessions connect to tasks via `worked_on`
- Session chains show temporal progression
- Task graph shows logical dependencies
- Combined: full picture of what was done, when, and for what purpose

## Success Metrics

1. **Chain Depth**: Average sessions per chain (target: 5+)
2. **Context Continuity**: % of compactions with successful context injection
3. **Reconnection Rate**: % of fresh starts that find previous session
4. **Query Latency**: Chain traversal under 100ms
5. **Coverage**: % of sessions with parent_session_id set
