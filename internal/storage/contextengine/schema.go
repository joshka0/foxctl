package contextengine

import (
	"context"
	"database/sql"
	"fmt"
)

// schemaDDL defines all tables and indexes for the context engine store.
const schemaDDL = `
-- Append-only events stream.
CREATE TABLE IF NOT EXISTS context_events (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	kind         TEXT NOT NULL,
	source       TEXT NOT NULL,
	task_id      TEXT NOT NULL DEFAULT '',
	session_id   TEXT NOT NULL DEFAULT '',
	refs         TEXT NOT NULL DEFAULT '[]',   -- JSON array of EvidenceRef
	data         TEXT NOT NULL DEFAULT '{}',   -- JSON map
	created_at   TEXT NOT NULL
);

-- Retrieval output packs.
CREATE TABLE IF NOT EXISTS evidence_packs (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	query        TEXT NOT NULL,
	lane         TEXT NOT NULL,
	nodes        TEXT NOT NULL DEFAULT '[]',   -- JSON array of EvidenceNode
	telemetry    TEXT NOT NULL DEFAULT '{}',   -- JSON EvidenceTelemetry
	metadata     TEXT NOT NULL DEFAULT '{}',   -- JSON map
	cas_digest   TEXT NOT NULL DEFAULT '',     -- CAS digest for large packs
	created_at   TEXT NOT NULL
);

-- Individual evidence items.
CREATE TABLE IF NOT EXISTS evidence_nodes (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	node_type    TEXT NOT NULL,
	ref_type     TEXT NOT NULL,
	ref_value    TEXT NOT NULL,
	statement    TEXT NOT NULL DEFAULT '',
	confidence   REAL NOT NULL DEFAULT 0,
	grounding    TEXT NOT NULL DEFAULT '',
	count        INTEGER NOT NULL DEFAULT 0,
	first_seen   TEXT NOT NULL DEFAULT '',
	last_seen    TEXT NOT NULL DEFAULT '',
	metadata     TEXT NOT NULL DEFAULT '{}',   -- JSON map
	cas_digest   TEXT NOT NULL DEFAULT ''      -- CAS digest for large statements
);

-- Durable memory assertions with lifecycle.
CREATE TABLE IF NOT EXISTS memory_claims (
	id              TEXT PRIMARY KEY,
	workspace_id    TEXT NOT NULL,
	claim_type      TEXT NOT NULL,
	status          TEXT NOT NULL,
	scope_path      TEXT NOT NULL DEFAULT '',
	scope_task_id   TEXT NOT NULL DEFAULT '',
	scope_session_id TEXT NOT NULL DEFAULT '',
	summary         TEXT NOT NULL DEFAULT '',
	confidence      REAL NOT NULL DEFAULT 0,
	blast_radius    TEXT NOT NULL DEFAULT '',
	source_refs     TEXT NOT NULL DEFAULT '[]', -- JSON array of EvidenceRef
	source_event_id TEXT NOT NULL DEFAULT '',
	superseded_by   TEXT NOT NULL DEFAULT '',
	reason          TEXT NOT NULL DEFAULT '',
	created_at      TEXT NOT NULL,
	updated_at      TEXT NOT NULL
);

-- Directed dependency graph.
CREATE TABLE IF NOT EXISTS impact_edges (
	id              TEXT PRIMARY KEY,
	workspace_id    TEXT NOT NULL,
	from_type       TEXT NOT NULL,
	from_ref        TEXT NOT NULL,
	to_type         TEXT NOT NULL,
	to_ref          TEXT NOT NULL,
	kind            TEXT NOT NULL,
	source_event_id TEXT NOT NULL DEFAULT '',
	created_at      TEXT NOT NULL,
	UNIQUE(workspace_id, from_type, from_ref, to_type, to_ref, kind)
);

-- Reactive invalidation state.
CREATE TABLE IF NOT EXISTS staleness_markers (
	id                TEXT PRIMARY KEY,
	workspace_id      TEXT NOT NULL,
	target_ref_type   TEXT NOT NULL,
	target_ref_value  TEXT NOT NULL,
	status            TEXT NOT NULL,
	caused_by_events  TEXT NOT NULL DEFAULT '[]', -- JSON array of event IDs
	resolved_by_event TEXT NOT NULL DEFAULT '',
	created_at        TEXT NOT NULL,
	updated_at        TEXT NOT NULL,
	UNIQUE(workspace_id, target_ref_type, target_ref_value)
);

-- Versioned working sets and task contexts.
CREATE TABLE IF NOT EXISTS projections (
	id                   TEXT NOT NULL,
	workspace_id         TEXT NOT NULL,
	projection_type      TEXT NOT NULL,
	projection_version   INTEGER NOT NULL,
	task_id              TEXT NOT NULL DEFAULT '',
	generated_from_events TEXT NOT NULL DEFAULT '[]', -- JSON array of event IDs
	payload              TEXT NOT NULL DEFAULT '{}',   -- JSON payload (WorkingSet, TaskContext, etc.)
	generated_at         TEXT NOT NULL,
	expires_at           TEXT NOT NULL DEFAULT '',
	created_at           TEXT NOT NULL,
	PRIMARY KEY (workspace_id, id)
);

-- Retrieval call telemetry (append-only).
CREATE TABLE IF NOT EXISTS retrieval_episodes (
	id             TEXT PRIMARY KEY,
	workspace_id   TEXT NOT NULL,
	query          TEXT NOT NULL,
	lane           TEXT NOT NULL,
	pack_id        TEXT NOT NULL DEFAULT '',
	duration_ms    INTEGER NOT NULL DEFAULT 0,
	tokens_used    INTEGER NOT NULL DEFAULT 0,
	hit_count      INTEGER NOT NULL DEFAULT 0,
	sub_episode_ids TEXT NOT NULL DEFAULT '[]', -- JSON array of episode IDs
	created_at     TEXT NOT NULL
);

-- User/system feedback on retrieval (append-only).
CREATE TABLE IF NOT EXISTS retrieval_feedback (
	id              TEXT PRIMARY KEY,
	workspace_id    TEXT NOT NULL,
	episode_id      TEXT NOT NULL,
	kind            TEXT NOT NULL,
	query           TEXT NOT NULL,
	used_refs       TEXT NOT NULL DEFAULT '[]',  -- JSON array of EvidenceRef
	gap_stmt        TEXT NOT NULL DEFAULT '',
	correction_stmt TEXT NOT NULL DEFAULT '',
	created_at      TEXT NOT NULL
);

-- Indexes for efficient querying.

-- Context events indexes
CREATE INDEX IF NOT EXISTS idx_events_workspace       ON context_events(workspace_id);
CREATE INDEX IF NOT EXISTS idx_events_kind_created     ON context_events(kind, created_at);
CREATE INDEX IF NOT EXISTS idx_events_task             ON context_events(task_id);
CREATE INDEX IF NOT EXISTS idx_events_session          ON context_events(session_id);

-- Evidence nodes indexes
CREATE INDEX IF NOT EXISTS idx_nodes_workspace         ON evidence_nodes(workspace_id);
CREATE INDEX IF NOT EXISTS idx_nodes_ref               ON evidence_nodes(ref_type, ref_value);

-- Memory claims indexes
CREATE INDEX IF NOT EXISTS idx_claims_workspace        ON memory_claims(workspace_id);
CREATE INDEX IF NOT EXISTS idx_claims_status           ON memory_claims(workspace_id, status);

-- Staleness markers indexes
CREATE INDEX IF NOT EXISTS idx_staleness_workspace     ON staleness_markers(workspace_id);
CREATE INDEX IF NOT EXISTS idx_staleness_target        ON staleness_markers(workspace_id, target_ref_type, target_ref_value);

-- Impact edges indexes (forward and reverse traversal)
CREATE INDEX IF NOT EXISTS idx_impact_workspace        ON impact_edges(workspace_id);
CREATE INDEX IF NOT EXISTS idx_impact_from             ON impact_edges(workspace_id, from_type, from_ref);
CREATE INDEX IF NOT EXISTS idx_impact_to               ON impact_edges(workspace_id, to_type, to_ref);

-- Projections indexes
CREATE INDEX IF NOT EXISTS idx_projections_type        ON projections(workspace_id, projection_type);

-- Retrieval episodes indexes
CREATE INDEX IF NOT EXISTS idx_episodes_workspace      ON retrieval_episodes(workspace_id);

-- Retrieval feedback indexes
CREATE INDEX IF NOT EXISTS idx_feedback_workspace      ON retrieval_feedback(workspace_id);
CREATE INDEX IF NOT EXISTS idx_feedback_episode        ON retrieval_feedback(workspace_id, episode_id);
`

// Migrate runs the schema DDL against the database.
// It is safe to call multiple times (all DDL uses IF NOT EXISTS).
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("contextengine: migrate: %w", err)
	}
	return nil
}
