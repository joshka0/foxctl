---
title: DB Migrations v1 (Mailbox + Sessions + Actor Runtime)
status: draft
last_updated: 2026-01-08
---

## 0. Principles

- Additive migrations only (no destructive renames/drop in v1)
- SQLite does NOT reliably support `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` across versions.
- Therefore: migrations should be **idempotent via PRAGMA checks** in Go.

Provide three helper primitives:
- `hasColumn(db, table, col) bool`
- `addColumnIfMissing(db, table, colDDL)`
- `createIndexIfMissing(db, ddl)`
- plus normal `CREATE TABLE IF NOT EXISTS`

---

## 1. mailbox.db changes

### 1.1 mailbox_notify wakeup table

```sql
CREATE TABLE IF NOT EXISTS mailbox_notify (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    to_ns TEXT NOT NULL,
    created_at INTEGER NOT NULL -- unix seconds
);

CREATE INDEX IF NOT EXISTS idx_mailbox_notify_to_ns_created
  ON mailbox_notify(to_ns, created_at);

CREATE TRIGGER IF NOT EXISTS mailbox_notify_trigger
AFTER INSERT ON mailbox
BEGIN
  INSERT INTO mailbox_notify (to_ns, created_at)
  VALUES (NEW.to_ns, strftime('%s','now'));
END;
````

### 1.2 Message metadata columns (for lineage tracing)

Add to `mailbox` table (names may differ; adapt to actual schema):

* `workspace_id TEXT`
* `session_id TEXT`
* `agent_id TEXT`
* `correlation_id TEXT` (optional; if not already in headers)

**Idempotent approach**

* Use PRAGMA table_info(mailbox) and add if missing.

Example DDL fragments:

```sql
ALTER TABLE mailbox ADD COLUMN workspace_id TEXT;
ALTER TABLE mailbox ADD COLUMN session_id TEXT;
ALTER TABLE mailbox ADD COLUMN agent_id TEXT;
ALTER TABLE mailbox ADD COLUMN correlation_id TEXT;
```

Indexes (optional but recommended):

```sql
CREATE INDEX IF NOT EXISTS idx_mailbox_to_ns_visible
  ON mailbox(to_ns, visible_at);

CREATE INDEX IF NOT EXISTS idx_mailbox_session
  ON mailbox(session_id);

CREATE INDEX IF NOT EXISTS idx_mailbox_workspace
  ON mailbox(workspace_id);
```

### 1.3 Backfill strategy

* New writes always set these columns.
* Old rows may remain NULL; readers must tolerate NULL.

---

## 2. sessions.db changes (unified lineage + actor runtime tables)

### 2.1 sessions table lineage columns

If your existing sessions table is Claude-capture oriented, keep it.
Add columns; do NOT change PK.

Columns:

* `workspace_id TEXT` (canonical workspace root path OR existing workspace_path; choose one canonical and fill it)
* `agent_id TEXT NOT NULL DEFAULT 'agentctl'`
* `status TEXT NOT NULL DEFAULT 'ok'` (queued|running|ok|error|canceled)
* `parent_session_id TEXT`
* `started_at TEXT`
* `updated_at TEXT`

DDL fragments:

```sql
ALTER TABLE sessions ADD COLUMN workspace_id TEXT;
ALTER TABLE sessions ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'agentctl';
ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'ok';
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT;
ALTER TABLE sessions ADD COLUMN started_at TEXT;
ALTER TABLE sessions ADD COLUMN updated_at TEXT;
```

Indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_sessions_workspace_agent_started
  ON sessions(workspace_id, agent_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_sessions_workspace_agent_status
  ON sessions(workspace_id, agent_id, status);

CREATE INDEX IF NOT EXISTS idx_sessions_parent
  ON sessions(workspace_id, parent_session_id);
```

Backfill:

* For legacy Claude sessions:

  * `agent_id='claude'` (if you can detect source) else keep 'agentctl'
  * `workspace_id` = existing workspace_path
  * timestamps inferred if available

### 2.2 session_edges table

```sql
CREATE TABLE IF NOT EXISTS session_edges (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  from_session TEXT NOT NULL,
  to_session TEXT NOT NULL,
  edge_type TEXT NOT NULL, -- continues|forked_from|relates_to
  created_at TEXT NOT NULL,
  metadata TEXT,
  UNIQUE(workspace_id, from_session, to_session, edge_type)
);

CREATE INDEX IF NOT EXISTS idx_session_edges_to
  ON session_edges(workspace_id, to_session);

CREATE INDEX IF NOT EXISTS idx_session_edges_type
  ON session_edges(workspace_id, edge_type);
```

---

## 3. Actor runtime tables in sessions.db

These tables are separate from Claude session capture tables to avoid collisions.

### 3.1 actor_turns (durable raw turns for actors)

```sql
CREATE TABLE IF NOT EXISTS actor_turns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,

  turn_index INTEGER NOT NULL,         -- monotonic per (actor_id, session_id)
  role TEXT NOT NULL,                 -- message_received|user|assistant|tool|system
  content TEXT,                       -- assistant/user text
  tool_name TEXT,                     -- for tool turns
  tool_input TEXT,                    -- JSON
  tool_output TEXT,                   -- JSON (may be truncated)
  artifact_digest TEXT,               -- CAS digest if offloaded
  correlation_id TEXT,
  created_at TEXT NOT NULL,

  UNIQUE(actor_id, session_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_actor_turns_actor_session
  ON actor_turns(actor_id, session_id, turn_index);

CREATE INDEX IF NOT EXISTS idx_actor_turns_workspace
  ON actor_turns(workspace_id);
```

### 3.2 actor_context_inbox (hook-injected context)

```sql
CREATE TABLE IF NOT EXISTS actor_context_inbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,

  priority INTEGER NOT NULL DEFAULT 0,
  kind TEXT NOT NULL DEFAULT 'context',  -- context|warning|system|hint
  text TEXT NOT NULL,
  created_at TEXT NOT NULL,
  surfaced_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_actor_inbox_actor_session
  ON actor_context_inbox(actor_id, session_id, surfaced_at, created_at);
```

### 3.3 actor_memory_state (cursor-based progressive memory)

```sql
CREATE TABLE IF NOT EXISTS actor_memory_state (
  actor_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,

  task_context TEXT,
  next_turn_to_summarize INTEGER NOT NULL DEFAULT 0,
  next_summary_to_distill INTEGER NOT NULL DEFAULT 0,

  l1_artifact_id TEXT,
  l2_artifact_id TEXT,

  total_turns INTEGER NOT NULL DEFAULT 0,
  token_estimate INTEGER NOT NULL DEFAULT 0,

  last_summarize_at TEXT,
  last_distill_at TEXT,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_actor_memory_state_session
  ON actor_memory_state(session_id);
```

---

## 4. Go migration helpers (recommended)

Implement these helpers in your sqlite util/migration layer:

```go
// Pseudocode only.
func hasColumn(db *sql.DB, table, col string) (bool, error) {
  rows, err := db.Query("PRAGMA table_info("+table+")")
  ...
}

func addColumnIfMissing(db *sql.DB, table, col, ddl string) error {
  ok, _ := hasColumn(...)
  if ok { return nil }
  _, err := db.Exec("ALTER TABLE "+table+" ADD COLUMN "+ddl)
  return err
}

func createIndexIfMissing(db *sql.DB, ddl string) error {
  _, err := db.Exec(ddl) // relies on IF NOT EXISTS
  return err
}
```

**Important:** Run migrations in a short transaction per DB open, but do NOT hold global locks while calling external services.

---


## 5. Migration sequencing / dependencies

Order:

1. mailbox_notify + indexes
2. mailbox columns (workspace_id/session_id/agent_id)
3. sessions lineage columns + session_edges
4. actor_turns + actor_context_inbox + actor_memory_state

Backfills are optional but recommended for correctness of new queries; keep them bounded and best-effort.



---
