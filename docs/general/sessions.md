# Sessions

Session management for context preservation across compaction boundaries.

---

## Overview

```mermaid
flowchart TD
    subgraph Session["Session Lifecycle"]
        START[Session Start]
        WORK[Working]
        COMPACT[Compaction]
        RESUME[Resume]
        END[Session End]
    end

    subgraph Windows["Context Windows"]
        W1[Window 1]
        W2[Window 2]
        W3[Window 3]
    end

    subgraph Learnings["Extracted Learnings"]
        L1[Gotchas]
        L2[Decisions]
        L3[Patterns]
    end

    START --> WORK
    WORK --> COMPACT
    COMPACT --> W1
    COMPACT --> W2
    WORK --> END

    RESUME --> WORK
    W1 --> L1
    W2 --> L2
    W3 --> L3
```

---

## Session Lineage

Sessions track their ancestry for context chain:

```mermaid
flowchart LR
    S1[Session 1<br/>parent: null]
    S2[Session 2<br/>parent: S1]
    S3[Session 3<br/>parent: S2]
    S4[Session 4<br/>parent: S1]

    S1 --> S2
    S2 --> S3
    S1 --> S4
```

```bash
# View session chain
agentctl sessions chain

# Output:
# Session 3 (current)
#   └── Session 2
#       └── Session 1 (root)
```

---

## Context Windows

Each compaction creates a new context window within a session:

| Field | Description |
|-------|-------------|
| `session_id` | Parent session |
| `window_number` | Sequential within session |
| `chunk_count` | Number of message chunks |
| `raw_jsonl_path` | Path to captured JSONL |
| `summary` | LLM-generated summary |

---

## Session Skills

### session/restore

Restore context when resuming or after compaction:

```bash
agentctl run session/restore --input '{"session_id": "..."}'
```

Returns:
- Recent session context
- Relevant past windows (via vector search)
- Active anchor goal
- Pending tasks

### session/summarize

Extract learnings from session windows:

```bash
agentctl run session/summarize --input '{"session_id": "..."}'
```

Extracts:
- Gotchas discovered
- Decisions made
- Patterns identified
- Technical learnings

### session/recall

Search past sessions:

```bash
agentctl run session/recall --input '{"query": "authentication bug"}'
```

---

## Session Hooks

| Hook | Event | Purpose |
|------|-------|---------|
| `session-restore` | SessionStart | Restore context on resume |
| `session-save` | PreCompact | Capture session state |
| `session-summarize` | PreCompact | Extract learnings via LLM |

### Hook Flow

```mermaid
sequenceDiagram
    participant CC as Claude Code
    participant Save as session-save
    participant Sum as session-summarize
    participant Restore as session-restore

    Note over CC: Compaction triggered
    CC->>Save: PreCompact
    Save->>Save: Capture JSONL
    CC->>Sum: PreCompact
    Sum->>Sum: LLM extraction
    Sum->>Sum: Store learnings

    Note over CC: Session resume
    CC->>Restore: SessionStart
    Restore->>Restore: Load context
    Restore-->>CC: Inject context
```

---

## Identity Fallback

For hooks without environment access, a fallback file stores session identity:

```
~/.agentctl/sessions/active/<workspace_hash>.json
```

```json
{
  "session_id": "abc123",
  "agent_id": "claude",
  "parent_session_id": "xyz789",
  "workspace": "/path/to/project"
}
```

---

## Environment Variables

Skills receive session context via environment:

| Variable | Description |
|----------|-------------|
| `AGENTCTL_SESSION_ID` | Current session ID |
| `AGENTCTL_AGENT_ID` | Agent identifier |
| `CLAUDE_SESSION_ID` | Claude Code session ID |

Fallback chain:
1. `AGENTCTL_SESSION_ID`
2. `CLAUDE_SESSION_ID`
3. `OPENCODE_SESSION_ID`
4. `CURSOR_SESSION_ID`
5. `TERM_SESSION_ID`

---

## Anchor Goals

Sessions can have an anchor goal that persists across compaction:

```bash
# Set anchor (in Claude Code chat)
/anchor Fix the authentication timeout bug

# Anchor is restored on resume via session/restore
```

---

## Storage Schema

Sessions stored in `~/.agentctl/storage/sessions.db`:

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    agent_id TEXT,
    workspace TEXT,
    parent_session_id TEXT,
    status TEXT,  -- running, ok, error, canceled
    started_at DATETIME,
    ended_at DATETIME,
    anchor TEXT,
    FOREIGN KEY (parent_session_id) REFERENCES sessions(id)
);

CREATE TABLE context_windows (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    window_number INTEGER NOT NULL,
    chunk_count INTEGER,
    raw_jsonl_path TEXT,
    summary TEXT,
    created_at DATETIME,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
```

---

## Gotchas

### Session Archives are Gzipped
Session JSONL files are archived as `.jsonl.gz`. Skills reading
`session.RawJSONLPath` must handle gzip:

```go
if strings.HasSuffix(path, ".gz") {
    gzReader, err := gzip.NewReader(file)
    // ...
}
```

### Session Learnings are Idempotent
`session/summarize` uses content-hash naming and checks for existing
embeddings before re-processing. Safe to run multiple times.

### Context Window vs Session Summaries
Summaries are at the **context window** level, not session level.
A session may have multiple windows, each with its own summary.

See [gotchas.md](gotchas.md) for more common pitfalls.
