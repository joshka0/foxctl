# Storage

agentctl uses SQLite databases and content-addressable storage.

---

## Overview

```mermaid
flowchart TD
    subgraph Storage["~/.agentctl/storage/"]
        MEM[(memory.db)]
        TASK[(tasks.db)]
        SESS[(sessions.db)]
        TRAJ[(trajectory.db)]
        AGENT[(agents.db)]
        COMP[(companion.db)]
        CTXVAR[(contextvar.db)]
        MBOX[(mailbox.db)]
        RGI[(repoindex/)]
    end

    subgraph Cache["~/.agentctl/cache/"]
        EMB[(embedding_queue.db)]
        CACHE[(cache.db)]
    end

    subgraph CAS["~/.agentctl/cas/"]
        SHA[sha256/]
    end

    subgraph Other
        JOBS[jobs/]
        OBS[observability/]
        BACK[backups/]
    end
```

---

## Database Files

### Persistent Storage (`~/.agentctl/storage/`)

| Database | Purpose | Key Tables |
|----------|---------|------------|
| `memory.db` | Memories, codemaps, symbols | `memories`, `memory_embeddings` |
| `tasks.db` | Task management | `tasks`, `task_dependencies` |
| `sessions.db` | Session lineage | `sessions`, `context_windows` |
| `trajectory.db` | Agent audit trail | `trajectories`, `trajectory_events` |
| `agents.db` | Agent registry | `agents` |
| `companion.db` | Companion conversation memory | `companion_turns`, `companion_day_summaries`, `companion_history` |
| `contextvar.db` | RLM context variables | `context_vars` |
| `mailbox.db` | Inter-agent messaging | `mailbox` |
| `repoindex/<repo>-repoindex-<hash>.db` | Repo graph index | `nodes`, `edges`, `index_meta` |

### Cache (`~/.agentctl/cache/`)

| Database | Purpose | Notes |
|----------|---------|-------|
| `embedding_queue.db` | Symbol embedding jobs | Regenerable |
| `cache.db` | Skill result cache | TTL-based |

---

## Content-Addressable Storage

Large artifacts stored by SHA-256 digest:

```
~/.agentctl/cas/
└── sha256/
    ├── abc123...
    ├── def456...
    └── ...
```

### Operations

```bash
# Store content
DIGEST=$(agentctl cas put < large-file.json)

# Retrieve by digest
agentctl cas get $DIGEST

# Pin (prevent GC)
agentctl cas pin $DIGEST

# Garbage collect
agentctl cas gc --older-than=168h --dry-run
```

### Integrity
Every `get` recomputes the SHA-256 and fails with `EIO` on mismatch.

---

## Schema Details

### memory.db

```sql
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL,  -- gotcha, decision, pattern, learning, reference
    summary TEXT NOT NULL,
    data TEXT,  -- JSON blob
    workspace TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE memory_embeddings (
    memory_id TEXT PRIMARY KEY,
    embedding BLOB NOT NULL,  -- 1024-dim float32
    model TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (memory_id) REFERENCES memories(id)
);

CREATE INDEX idx_memories_type ON memories(type);
CREATE INDEX idx_memories_workspace ON memories(workspace);
```

### tasks.db

```sql
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT DEFAULT 'pending',  -- pending, in_progress, completed, blocked
    priority INTEGER DEFAULT 0,
    workspace TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

CREATE TABLE task_dependencies (
    task_id TEXT NOT NULL,
    depends_on TEXT NOT NULL,
    PRIMARY KEY (task_id, depends_on),
    FOREIGN KEY (task_id) REFERENCES tasks(id),
    FOREIGN KEY (depends_on) REFERENCES tasks(id)
);

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_workspace ON tasks(workspace);
```

### sessions.db

See [sessions.md](sessions.md) for full schema details.

Key tables:
- `sessions` - Session records with lineage, agent execution context
- `session_turns` - Per-turn persistence for agent sessions
- `context_windows` - Compaction window records
- `session_edges` - Session lineage graph (continues, forked_from)

Notable fields for daemon agents:
- `prompt`, `prompt_hash` - Original task for correlation with wide events
- `llm_provider`, `llm_model` - LLM configuration
- Turn content stored in CAS via `content_cas_digest`

```sql
-- Core session query
SELECT id, status, prompt, llm_provider, started_at
FROM sessions
WHERE workspace_path = ?
ORDER BY started_at DESC;

-- Session turns with CAS content
SELECT turn_index, role, content_preview, content_cas_digest, tool_calls
FROM session_turns
WHERE session_id = ?
ORDER BY turn_index;
```

### companion.db

```sql
-- L0: Raw conversation turns
CREATE TABLE companion_turns (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    role TEXT NOT NULL,              -- 'user', 'assistant'
    content TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- L1: Daily compressed summaries
CREATE TABLE companion_day_summaries (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    date TEXT NOT NULL,              -- YYYY-MM-DD
    summary TEXT NOT NULL,
    topics TEXT,                     -- JSON array
    mood TEXT,
    message_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(conversation_id, date)
);

-- L2: Distilled relationship history
CREATE TABLE companion_history (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL UNIQUE,
    history TEXT NOT NULL,           -- Distilled context
    topics TEXT,                     -- JSON array
    preferences TEXT,                -- JSON object
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Compression cursor tracking
CREATE TABLE companion_memory_state (
    conversation_id TEXT PRIMARY KEY,
    last_summarized_at DATETIME,
    last_distilled_at DATETIME
);

CREATE INDEX idx_turns_conv ON companion_turns(conversation_id);
CREATE INDEX idx_turns_created ON companion_turns(created_at);
CREATE INDEX idx_summaries_conv ON companion_day_summaries(conversation_id);
CREATE INDEX idx_summaries_date ON companion_day_summaries(date);
```

### contextvar.db

```sql
CREATE TABLE context_vars (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    scope TEXT NOT NULL,             -- 'global', 'conversation', 'turn'
    key TEXT NOT NULL,
    value_json TEXT,                 -- Inline JSON for small values
    value_cas TEXT,                  -- CAS digest for large values (>64KB)
    content_type TEXT DEFAULT 'json',
    sequence_num INTEGER,
    source TEXT,                     -- Producer (tool, skill, user)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,             -- TTL expiration
    access_count INTEGER DEFAULT 0,
    last_access DATETIME,
    embedding BLOB,                  -- Optional semantic embedding
    embedding_model TEXT,
    UNIQUE(conversation_id, scope, key)
);

CREATE INDEX idx_context_vars_conv ON context_vars(conversation_id);
CREATE INDEX idx_context_vars_scope ON context_vars(scope);
CREATE INDEX idx_context_vars_expires ON context_vars(expires_at);
CREATE INDEX idx_context_vars_key ON context_vars(key);
```

---

## Vector Storage

Embeddings stored as binary blobs (float32 arrays):

```go
// Store embedding
embedding := make([]float32, 1024)
// ... populate embedding
blob := float32ToBytes(embedding)
db.Exec("INSERT INTO memory_embeddings (memory_id, embedding, model) VALUES (?, ?, ?)",
    memoryID, blob, "voyage-3-large")

// Query with cosine similarity
// (Typically done in application code with vector library)
```

### Turso Sync (Optional)

For cross-workspace search, embeddings can sync to Turso:

```bash
export TURSO_DATABASE_URL=libsql://your-db.turso.io
export TURSO_AUTH_TOKEN=your-token

# Push local embeddings
agentctl index sync push --scope memory

# Query globally
agentctl index sync query --query "..." --global
```

Requires CGO build: `make build-cgo`

---

## Jobs Storage

Async job execution stored in `~/.agentctl/jobs/`:

```
jobs/
├── jobs.db           # Job state
└── <ulid>/
    ├── input.json    # Job input
    ├── output.json   # Job output
    └── logs/         # Execution logs
```

---

## Observability

Wide events stored in `~/.agentctl/observability/events/`:

```
observability/
└── events/
    ├── 2026-01-12.ndjson
    ├── 2026-01-11.ndjson
    └── ...
```

Enable with:
```bash
export AGENTCTL_OBS_DIR=$HOME/.agentctl/observability
```

---

## Backups

```bash
# Create backup
agentctl backup create

# List backups
agentctl backup list

# Restore
agentctl backup restore <backup-id>
```

Backups stored in `~/.agentctl/backups/`.

---

## Gotchas

### Memory Path
Memory is in `storage/memory.db`, NOT `cache/memory.db`:

```go
// Correct
store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)

// Wrong
store, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
```

### CGO for Turso
Vector search with Turso requires CGO build:

```bash
# Wrong - causes duplicate SQLite symbols
CGO_ENABLED=1 go build ./...

# Correct
make build-cgo  # Uses -tags=libsqlite3
```

See [gotchas.md](gotchas.md) for more common pitfalls.
