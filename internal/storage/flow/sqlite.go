// Package flow provides SQLite-backed persistence for flow engine entities.
//
// The store follows the existing pattern from internal/storage/jobs/persist:
// dbutil.OpenStoreDB with env prefix FLOW and default file flow.db.
// All tables use IF NOT EXISTS for idempotent migrations.
package flow

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	envelopepkg "github.com/joshka0/foxctl/internal/domain/envelope"
	flow "github.com/joshka0/foxctl/internal/runtime/flow"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

type sqlStore struct {
	db    *sql.DB
	close func() error

	// writeMu serializes write operations for SQLite concurrency safety.
	writeMu sync.Mutex

	// logSubs tracks active log subscribers for streaming.
	logSubsMu sync.RWMutex
	logSubs   map[string][]chan flow.RunLog // runID -> subscriber channels
}

// Open creates or opens a SQLite-backed flow store at <root>/flow.db and runs
// migrations. The env prefix is FLOW (e.g. FOXCTL_FLOW_DB overrides the path).
func Open(ctx context.Context, root string) (flow.Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "FLOW", "flow.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("flow: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn, logSubs: make(map[string][]chan flow.RunLog)}, nil
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// ---------------------------------------------------------------------------
// Migration
// ---------------------------------------------------------------------------

func migrate(ctx context.Context, db *sql.DB) error {
	// Add room_id column to existing flows tables (idempotent).
	_, _ = db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN room_id TEXT NOT NULL DEFAULT ''`)

	ddl := `
CREATE TABLE IF NOT EXISTS flows (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    workspace   TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'draft',
    description TEXT NOT NULL DEFAULT '',
    room_id     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE(name, workspace)
);

CREATE TABLE IF NOT EXISTS flow_nodes (
    id       TEXT PRIMARY KEY,
    flow_id  TEXT NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,
    label    TEXT NOT NULL,
    config   TEXT NOT NULL DEFAULT '{}',
    position TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS flow_edges (
    id               TEXT PRIMARY KEY,
    flow_id          TEXT NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    from_node_id     TEXT NOT NULL REFERENCES flow_nodes(id) ON DELETE CASCADE,
    to_node_id       TEXT NOT NULL REFERENCES flow_nodes(id) ON DELETE CASCADE,
    transform        TEXT NOT NULL DEFAULT '',
    transform_config TEXT NOT NULL DEFAULT '',
    trigger          TEXT NOT NULL DEFAULT '',
    trigger_config   TEXT NOT NULL DEFAULT '',
    condition        TEXT NOT NULL DEFAULT '',
    retry_policy     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS flow_runs (
    id           TEXT PRIMARY KEY,
    flow_id      TEXT NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    state        TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_flow_nodes_flow_id ON flow_nodes(flow_id);
CREATE INDEX IF NOT EXISTS idx_flow_edges_flow_id ON flow_edges(flow_id);
CREATE INDEX IF NOT EXISTS idx_flow_edges_from    ON flow_edges(from_node_id);
CREATE INDEX IF NOT EXISTS idx_flow_edges_to      ON flow_edges(to_node_id);
CREATE INDEX IF NOT EXISTS idx_flow_runs_flow_id  ON flow_runs(flow_id);

CREATE TABLE IF NOT EXISTS flow_run_logs (
    id         TEXT PRIMARY KEY,
    run_id     TEXT NOT NULL REFERENCES flow_runs(id) ON DELETE CASCADE,
    node_id    TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    envelope   TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_flow_run_logs_run_seq  ON flow_run_logs(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_flow_run_logs_run_node ON flow_run_logs(run_id, node_id);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("flow: migrate: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// ---------------------------------------------------------------------------
// Flow CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) CreateFlow(ctx context.Context, f flow.Flow) (flow.Flow, error) {
	if err := validateFlowState(f.State); err != nil {
		return flow.Flow{}, err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO flows (id, name, workspace, state, description, room_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		f.ID, f.Name, f.Workspace, string(f.State), f.Description, f.RoomID,
		sqlutil.FormatTimestamp(f.CreatedAt), sqlutil.FormatTimestamp(f.UpdatedAt))
	if err != nil {
		return flow.Flow{}, fmt.Errorf("flow: create flow: %w", err)
	}
	return f, nil
}

func (s *sqlStore) GetFlow(ctx context.Context, id string) (flow.Flow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, workspace, state, description, room_id, created_at, updated_at
		FROM flows WHERE id = $1`, id)
	return scanFlow(row)
}

func (s *sqlStore) GetFlowByName(ctx context.Context, workspace, name string) (flow.Flow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, workspace, state, description, room_id, created_at, updated_at
		FROM flows WHERE name = $1 AND workspace = $2`, name, workspace)
	return scanFlow(row)
}

func (s *sqlStore) ListFlows(ctx context.Context, workspace string) ([]flow.Flow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, workspace, state, description, room_id, created_at, updated_at
		FROM flows WHERE workspace = $1
		ORDER BY created_at DESC`, workspace)
	if err != nil {
		return nil, fmt.Errorf("flow: list flows: %w", err)
	}
	defer rows.Close()

	var flows []flow.Flow
	for rows.Next() {
		f, scanErr := scanFlowRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		flows = append(flows, f)
	}
	if flows == nil {
		flows = []flow.Flow{}
	}
	return flows, rows.Err()
}

func (s *sqlStore) UpdateFlow(ctx context.Context, f flow.Flow) (flow.Flow, error) {
	if err := validateFlowState(f.State); err != nil {
		return flow.Flow{}, err
	}

	now := sqlutil.FormatTimestamp(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
		UPDATE flows SET state = $1, description = $2, room_id = $3, updated_at = $4
		WHERE id = $5`,
		string(f.State), f.Description, f.RoomID, now, f.ID)
	if err != nil {
		return flow.Flow{}, fmt.Errorf("flow: update flow: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return flow.Flow{}, fmt.Errorf("flow: update flow rows: %w", err)
	}
	if n == 0 {
		return flow.Flow{}, flow.ErrNotFound
	}
	return s.GetFlow(ctx, f.ID)
}

func validateFlowState(state flow.FlowState) error {
	if !state.IsValid() {
		return fmt.Errorf("flow: invalid flow state %q", state)
	}
	return nil
}

func (s *sqlStore) DeleteFlow(ctx context.Context, id string) error {
	// Delete in order: runs, edges, nodes, then flow.
	// SQLite FK CASCADE handles child deletions (nodes, edges, runs).
	// RowsAffected=0 means the flow was not found.
	res, err := s.db.ExecContext(ctx, `DELETE FROM flows WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("flow: delete flow: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flow: delete flow rows: %w", err)
	}
	if n == 0 {
		return flow.ErrNotFound
	}
	return nil
}

func scanFlow(row *sql.Row) (flow.Flow, error) {
	var f flow.Flow
	var state string
	var created, updated string
	err := row.Scan(&f.ID, &f.Name, &f.Workspace, &state, &f.Description, &f.RoomID, &created, &updated)
	if err != nil {
		if isNoRows(err) {
			return flow.Flow{}, flow.ErrNotFound
		}
		return flow.Flow{}, fmt.Errorf("flow: scan flow: %w", err)
	}
	f.State = flow.FlowState(state)
	if err := validateFlowState(f.State); err != nil {
		return flow.Flow{}, err
	}
	if err := parseFlowTimestamps(&f, created, updated); err != nil {
		return flow.Flow{}, err
	}
	return f, nil
}

func scanFlowRow(rows *sql.Rows) (flow.Flow, error) {
	var f flow.Flow
	var state string
	var created, updated string
	err := rows.Scan(&f.ID, &f.Name, &f.Workspace, &state, &f.Description, &f.RoomID, &created, &updated)
	if err != nil {
		return flow.Flow{}, fmt.Errorf("flow: scan flow row: %w", err)
	}
	f.State = flow.FlowState(state)
	if err := validateFlowState(f.State); err != nil {
		return flow.Flow{}, err
	}
	if err := parseFlowTimestamps(&f, created, updated); err != nil {
		return flow.Flow{}, err
	}
	return f, nil
}

func parseFlowTimestamps(f *flow.Flow, created, updated string) error {
	var err error
	f.CreatedAt, err = sqlutil.ScanTimestamp(created)
	if err != nil {
		return fmt.Errorf("flow: scan flow created_at: %w", err)
	}
	f.UpdatedAt, err = sqlutil.ScanTimestamp(updated)
	if err != nil {
		return fmt.Errorf("flow: scan flow updated_at: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Node CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) AddNode(ctx context.Context, n flow.FlowNode) (flow.FlowNode, error) {
	if err := validateNodeKind(n.Kind); err != nil {
		return flow.FlowNode{}, err
	}

	configStr, err := normalizeNodeConfig(n.Config)
	if err != nil {
		return flow.FlowNode{}, err
	}

	positionStr, err := encodeNodePosition(n.Position)
	if err != nil {
		return flow.FlowNode{}, err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO flow_nodes (id, flow_id, kind, label, config, position)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		n.ID, n.FlowID, string(n.Kind), n.Label, configStr, positionStr)
	if err != nil {
		return flow.FlowNode{}, fmt.Errorf("flow: add node: %w", err)
	}
	return s.GetNode(ctx, n.ID)
}

func validateNodeKind(kind flow.NodeKind) error {
	if !kind.IsValid() {
		return fmt.Errorf("flow: invalid node kind %q", kind)
	}
	return nil
}

func normalizeNodeConfig(config json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(config)
	if len(trimmed) == 0 {
		return "{}", nil
	}
	if !json.Valid(trimmed) {
		return "", fmt.Errorf("flow: invalid node config JSON")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
		return "", fmt.Errorf("flow: node config must be a JSON object")
	}
	return string(trimmed), nil
}

func encodeNodePosition(position *flow.Position) (string, error) {
	if position == nil {
		return "", nil
	}
	posBytes, err := json.Marshal(position)
	if err != nil {
		return "", fmt.Errorf("flow: invalid node position: %w", err)
	}
	return string(posBytes), nil
}

func (s *sqlStore) GetNode(ctx context.Context, id string) (flow.FlowNode, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, flow_id, kind, label, config, position
		FROM flow_nodes WHERE id = $1`, id)

	var n flow.FlowNode
	var kind, configStr, positionStr string
	err := row.Scan(&n.ID, &n.FlowID, &kind, &n.Label, &configStr, &positionStr)
	if err != nil {
		if isNoRows(err) {
			return flow.FlowNode{}, flow.ErrNotFound
		}
		return flow.FlowNode{}, fmt.Errorf("flow: get node: %w", err)
	}
	n.Kind = flow.NodeKind(kind)
	if err := validateNodeKind(n.Kind); err != nil {
		return flow.FlowNode{}, err
	}
	config, err := parseNodeConfig(configStr)
	if err != nil {
		return flow.FlowNode{}, err
	}
	n.Config = config
	position, err := parseNodePosition(positionStr)
	if err != nil {
		return flow.FlowNode{}, err
	}
	n.Position = position
	return n, nil
}

func (s *sqlStore) RemoveNode(ctx context.Context, id string) error {
	// Delete connected edges first (explicit cascade, though FK CASCADE also covers this).
	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM flow_edges WHERE from_node_id = $1 OR to_node_id = $1`, id)

	res, err := s.db.ExecContext(ctx, `DELETE FROM flow_nodes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("flow: remove node: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flow: remove node rows: %w", err)
	}
	if n == 0 {
		return flow.ErrNotFound
	}
	return nil
}

func (s *sqlStore) ListNodesByFlow(ctx context.Context, flowID string) ([]flow.FlowNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, flow_id, kind, label, config, position
		FROM flow_nodes WHERE flow_id = $1
		ORDER BY label`, flowID)
	if err != nil {
		return nil, fmt.Errorf("flow: list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []flow.FlowNode
	for rows.Next() {
		var n flow.FlowNode
		var kind, configStr, positionStr string
		if err := rows.Scan(&n.ID, &n.FlowID, &kind, &n.Label, &configStr, &positionStr); err != nil {
			return nil, fmt.Errorf("flow: scan node: %w", err)
		}
		n.Kind = flow.NodeKind(kind)
		if err := validateNodeKind(n.Kind); err != nil {
			return nil, err
		}
		config, err := parseNodeConfig(configStr)
		if err != nil {
			return nil, err
		}
		n.Config = config
		position, err := parseNodePosition(positionStr)
		if err != nil {
			return nil, err
		}
		n.Position = position
		nodes = append(nodes, n)
	}
	if nodes == nil {
		nodes = []flow.FlowNode{}
	}
	return nodes, rows.Err()
}

func parseNodeConfig(raw string) (json.RawMessage, error) {
	normalized, err := normalizeNodeConfig(json.RawMessage(raw))
	if err != nil {
		return nil, fmt.Errorf("flow: scan node config: %w", err)
	}
	return json.RawMessage(normalized), nil
}

func parseNodePosition(raw string) (*flow.Position, error) {
	if raw == "" {
		return nil, nil
	}
	var position flow.Position
	if err := json.Unmarshal([]byte(raw), &position); err != nil {
		return nil, fmt.Errorf("flow: scan node position: %w", err)
	}
	return &position, nil
}

// ---------------------------------------------------------------------------
// Edge CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) AddEdge(ctx context.Context, e flow.FlowEdge) (flow.FlowEdge, error) {
	if err := validateEdgeKinds(e); err != nil {
		return flow.FlowEdge{}, err
	}
	if err := validateRetryPolicy(e.RetryPolicy); err != nil {
		return flow.FlowEdge{}, err
	}
	if err := s.validateEdgeEndpoints(ctx, e); err != nil {
		return flow.FlowEdge{}, err
	}

	retryPolicyStr := ""
	if e.RetryPolicy != nil {
		rpBytes, _ := json.Marshal(e.RetryPolicy)
		retryPolicyStr = string(rpBytes)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO flow_edges (id, flow_id, from_node_id, to_node_id,
			transform, transform_config, trigger, trigger_config,
			condition, retry_policy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.ID, e.FlowID, e.FromNodeID, e.ToNodeID,
		string(e.Transform), e.TransformConfig, string(e.Trigger), e.TriggerConfig,
		e.Condition, retryPolicyStr)
	if err != nil {
		return flow.FlowEdge{}, fmt.Errorf("flow: add edge: %w", err)
	}
	return s.GetEdge(ctx, e.ID)
}

func validateEdgeKinds(e flow.FlowEdge) error {
	if !e.Transform.IsValid() {
		return fmt.Errorf("flow: invalid edge transform %q", e.Transform)
	}
	if !e.Trigger.IsValid() {
		return fmt.Errorf("flow: invalid edge trigger %q", e.Trigger)
	}
	return nil
}

func validateRetryPolicy(policy *flow.RetryPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.MaxAttempts < 0 {
		return fmt.Errorf("flow: retry max_attempts must be non-negative")
	}
	if policy.DelayMS < 0 {
		return fmt.Errorf("flow: retry delay_ms must be non-negative")
	}
	return nil
}

func (s *sqlStore) validateEdgeEndpoints(ctx context.Context, e flow.FlowEdge) error {
	from, err := s.GetNode(ctx, e.FromNodeID)
	if err != nil {
		return fmt.Errorf("flow: edge source node: %w", err)
	}
	if from.FlowID != e.FlowID {
		return fmt.Errorf("flow: edge source node %q belongs to flow %q, not %q", e.FromNodeID, from.FlowID, e.FlowID)
	}

	to, err := s.GetNode(ctx, e.ToNodeID)
	if err != nil {
		return fmt.Errorf("flow: edge target node: %w", err)
	}
	if to.FlowID != e.FlowID {
		return fmt.Errorf("flow: edge target node %q belongs to flow %q, not %q", e.ToNodeID, to.FlowID, e.FlowID)
	}
	return nil
}

func (s *sqlStore) GetEdge(ctx context.Context, id string) (flow.FlowEdge, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, flow_id, from_node_id, to_node_id,
			transform, transform_config, trigger, trigger_config,
			condition, retry_policy
		FROM flow_edges WHERE id = $1`, id)

	return scanEdge(row)
}

func (s *sqlStore) RemoveEdge(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM flow_edges WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("flow: remove edge: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flow: remove edge rows: %w", err)
	}
	if n == 0 {
		return flow.ErrNotFound
	}
	return nil
}

func (s *sqlStore) ListEdgesByFlow(ctx context.Context, flowID string) ([]flow.FlowEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, flow_id, from_node_id, to_node_id,
			transform, transform_config, trigger, trigger_config,
			condition, retry_policy
		FROM flow_edges WHERE flow_id = $1
		ORDER BY id`, flowID)
	if err != nil {
		return nil, fmt.Errorf("flow: list edges: %w", err)
	}
	defer rows.Close()

	var edges []flow.FlowEdge
	for rows.Next() {
		var e flow.FlowEdge
		var transform, trigger, retryPolicyStr string
		err := rows.Scan(
			&e.ID, &e.FlowID, &e.FromNodeID, &e.ToNodeID,
			&transform, &e.TransformConfig, &trigger, &e.TriggerConfig,
			&e.Condition, &retryPolicyStr,
		)
		if err != nil {
			return nil, fmt.Errorf("flow: scan edge: %w", err)
		}
		e.Transform = flow.TransformKind(transform)
		e.Trigger = flow.TriggerKind(trigger)
		if err := validateEdgeKinds(e); err != nil {
			return nil, err
		}
		retryPolicy, err := parseEdgeRetryPolicy(retryPolicyStr)
		if err != nil {
			return nil, err
		}
		e.RetryPolicy = retryPolicy
		edges = append(edges, e)
	}
	if edges == nil {
		edges = []flow.FlowEdge{}
	}
	return edges, rows.Err()
}

func scanEdge(row *sql.Row) (flow.FlowEdge, error) {
	var e flow.FlowEdge
	var transform, trigger, retryPolicyStr string
	err := row.Scan(
		&e.ID, &e.FlowID, &e.FromNodeID, &e.ToNodeID,
		&transform, &e.TransformConfig, &trigger, &e.TriggerConfig,
		&e.Condition, &retryPolicyStr,
	)
	if err != nil {
		if isNoRows(err) {
			return flow.FlowEdge{}, flow.ErrNotFound
		}
		return flow.FlowEdge{}, fmt.Errorf("flow: scan edge: %w", err)
	}
	e.Transform = flow.TransformKind(transform)
	e.Trigger = flow.TriggerKind(trigger)
	if err := validateEdgeKinds(e); err != nil {
		return flow.FlowEdge{}, err
	}
	retryPolicy, err := parseEdgeRetryPolicy(retryPolicyStr)
	if err != nil {
		return flow.FlowEdge{}, err
	}
	e.RetryPolicy = retryPolicy
	return e, nil
}

func parseEdgeRetryPolicy(raw string) (*flow.RetryPolicy, error) {
	if raw == "" {
		return nil, nil
	}
	var retryPolicy flow.RetryPolicy
	if err := json.Unmarshal([]byte(raw), &retryPolicy); err != nil {
		return nil, fmt.Errorf("flow: scan edge retry_policy: %w", err)
	}
	if err := validateRetryPolicy(&retryPolicy); err != nil {
		return nil, fmt.Errorf("flow: scan edge retry_policy: %w", err)
	}
	return &retryPolicy, nil
}

// ---------------------------------------------------------------------------
// Run CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) CreateRun(ctx context.Context, r flow.FlowRun) (flow.FlowRun, error) {
	if err := validateRunState(r.State); err != nil {
		return flow.FlowRun{}, err
	}
	if err := validateRunCompletion(r.State, r.StartedAt, r.CompletedAt); err != nil {
		return flow.FlowRun{}, err
	}

	completedAt := ""
	if r.CompletedAt != nil {
		completedAt = sqlutil.FormatTimestamp(*r.CompletedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO flow_runs (id, flow_id, state, started_at, completed_at, error)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		r.ID, r.FlowID, string(r.State),
		sqlutil.FormatTimestamp(r.StartedAt), completedAt, r.Error)
	if err != nil {
		return flow.FlowRun{}, fmt.Errorf("flow: create run: %w", err)
	}
	return s.getRun(ctx, r.ID)
}

func (s *sqlStore) UpdateRun(ctx context.Context, r flow.FlowRun) (flow.FlowRun, error) {
	if err := validateRunState(r.State); err != nil {
		return flow.FlowRun{}, err
	}
	existing, err := s.getRun(ctx, r.ID)
	if err != nil {
		return flow.FlowRun{}, err
	}
	if isFinalizedTerminalRun(existing, r) {
		return flow.FlowRun{}, fmt.Errorf("flow: run %q is terminal in state %q", r.ID, existing.State)
	}
	if err := validateRunCompletion(r.State, existing.StartedAt, r.CompletedAt); err != nil {
		return flow.FlowRun{}, err
	}

	completedAt := ""
	if r.CompletedAt != nil {
		completedAt = sqlutil.FormatTimestamp(*r.CompletedAt)
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE flow_runs SET state = $1, completed_at = $2, error = $3
		WHERE id = $4`,
		string(r.State), completedAt, r.Error, r.ID)
	if err != nil {
		return flow.FlowRun{}, fmt.Errorf("flow: update run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return flow.FlowRun{}, fmt.Errorf("flow: update run rows: %w", err)
	}
	if n == 0 {
		return flow.FlowRun{}, flow.ErrNotFound
	}
	return s.getRun(ctx, r.ID)
}

func isTerminalRunState(state flow.RunState) bool {
	return state == flow.RunCompleted || state == flow.RunFailed
}

func validateRunCompletion(state flow.RunState, startedAt time.Time, completedAt *time.Time) error {
	if state == flow.RunRunning && completedAt != nil {
		return fmt.Errorf("flow: running run cannot have completed_at")
	}
	if completedAt != nil && completedAt.Before(startedAt) {
		return fmt.Errorf("flow: completed_at %s precedes started_at %s",
			completedAt.UTC().Format(time.RFC3339Nano),
			startedAt.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

func isFinalizedTerminalRun(existing, next flow.FlowRun) bool {
	if !isTerminalRunState(existing.State) {
		return false
	}
	return existing.CompletedAt != nil || next.State != existing.State || next.CompletedAt == nil
}

func validateRunState(state flow.RunState) error {
	if !state.IsValid() {
		return fmt.Errorf("flow: invalid run state %q", state)
	}
	return nil
}

// GetRun returns the run with the given ID.
func (s *sqlStore) GetRun(ctx context.Context, id string) (flow.FlowRun, error) {
	return s.getRun(ctx, id)
}

func (s *sqlStore) getRun(ctx context.Context, id string) (flow.FlowRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, flow_id, state, started_at, completed_at, error
		FROM flow_runs WHERE id = $1`, id)

	var r flow.FlowRun
	var state string
	var startedAt, completedAt string
	err := row.Scan(&r.ID, &r.FlowID, &state, &startedAt, &completedAt, &r.Error)
	if err != nil {
		if isNoRows(err) {
			return flow.FlowRun{}, flow.ErrNotFound
		}
		return flow.FlowRun{}, fmt.Errorf("flow: get run: %w", err)
	}
	r.State = flow.RunState(state)
	if err := validateRunState(r.State); err != nil {
		return flow.FlowRun{}, err
	}
	var parseErr error
	r.StartedAt, parseErr = sqlutil.ScanTimestamp(startedAt)
	if parseErr != nil {
		return flow.FlowRun{}, fmt.Errorf("flow: scan run started_at: %w", parseErr)
	}
	if completedAt != "" {
		t, parseErr := sqlutil.ScanTimestamp(completedAt)
		if parseErr != nil {
			return flow.FlowRun{}, fmt.Errorf("flow: scan run completed_at: %w", parseErr)
		}
		if !t.IsZero() {
			r.CompletedAt = &t
		}
	}
	if err := validateRunCompletion(r.State, r.StartedAt, r.CompletedAt); err != nil {
		return flow.FlowRun{}, err
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// Run Logs
// ---------------------------------------------------------------------------

// WriteRunLog inserts a new run log entry. The seq value is auto-assigned
// as the next per-run monotonic integer. The ID is generated if empty.
// After inserting, the log is broadcast to any active stream subscribers.
// Concurrent writes are serialized via a mutex for SQLite safety.
func (s *sqlStore) WriteRunLog(ctx context.Context, log flow.RunLog) (flow.RunLog, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := validateRunLogEnvelope(log.Envelope); err != nil {
		return flow.RunLog{}, err
	}

	// Generate ID if not provided.
	if log.ID == "" {
		log.ID = ulid.Make().String()
	}

	// Assign created_at if not set.
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	run, err := s.getRun(ctx, log.RunID)
	if err != nil {
		return flow.RunLog{}, err
	}
	if err := validateRunLogCreatedAt(log.CreatedAt, run.StartedAt); err != nil {
		return flow.RunLog{}, err
	}

	// Auto-assign seq as next per-run monotonic value.
	var maxSeq sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM flow_run_logs WHERE run_id = $1`, log.RunID).Scan(&maxSeq)
	if err != nil && !isNoRows(err) {
		return flow.RunLog{}, fmt.Errorf("flow: write log seq: %w", err)
	}
	if maxSeq.Valid {
		log.Seq = int(maxSeq.Int64) + 1
	} else {
		log.Seq = 1
	}

	// Serialize envelope to JSON if not already a string.
	envStr := string(log.Envelope)

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO flow_run_logs (id, run_id, node_id, seq, envelope, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		log.ID, log.RunID, log.NodeID, log.Seq, envStr,
		sqlutil.FormatTimestamp(log.CreatedAt))
	if err != nil {
		return flow.RunLog{}, fmt.Errorf("flow: write log: %w", err)
	}

	// Broadcast to stream subscribers (non-blocking).
	s.broadcastLog(log)

	return log, nil
}

func validateRunLogEnvelope(raw json.RawMessage) error {
	var env envelopepkg.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("flow: invalid run log envelope JSON: %w", err)
	}
	if err := envelopepkg.Validate(env); err != nil {
		return fmt.Errorf("flow: invalid run log envelope: %w", err)
	}
	return nil
}

func validateRunLogCreatedAt(createdAt, runStartedAt time.Time) error {
	if createdAt.Before(runStartedAt) {
		return fmt.Errorf("flow: run log created_at %s precedes run started_at %s",
			createdAt.UTC().Format(time.RFC3339Nano),
			runStartedAt.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

// ListRunLogs returns log entries for the given run, ordered by seq ascending.
// Supports functional options: WithNodeID, WithLimit, WithOffset.
// Returns an empty non-nil slice when there are no logs.
func (s *sqlStore) ListRunLogs(ctx context.Context, runID string, opts ...flow.RunLogOption) ([]flow.RunLog, error) {
	f := flow.ApplyRunLogOptions(opts...)

	query := `SELECT id, run_id, node_id, seq, envelope, created_at
		FROM flow_run_logs WHERE run_id = $1`
	args := []any{runID}
	argIdx := 2

	if f.NodeID != "" {
		query += fmt.Sprintf(` AND node_id = $%d`, argIdx)
		args = append(args, f.NodeID)
		argIdx++
	}

	query += ` ORDER BY seq ASC`

	if f.Limit > 0 {
		query += fmt.Sprintf(` LIMIT $%d`, argIdx)
		args = append(args, f.Limit)
		argIdx++
	}

	if f.Offset > 0 {
		query += fmt.Sprintf(` OFFSET $%d`, argIdx)
		args = append(args, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("flow: list run logs: %w", err)
	}
	defer rows.Close()

	var logs []flow.RunLog
	for rows.Next() {
		l, scanErr := scanRunLog(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []flow.RunLog{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.validateListedRunLogs(ctx, runID, logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *sqlStore) validateListedRunLogs(ctx context.Context, runID string, logs []flow.RunLog) error {
	if len(logs) == 0 {
		return nil
	}
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("flow: list run logs run: %w", err)
	}
	prevSeq := 0
	for _, log := range logs {
		if log.Seq <= prevSeq {
			return fmt.Errorf("flow: list run logs seq: seq values must be strictly increasing")
		}
		prevSeq = log.Seq
		if err := validateRunLogCreatedAt(log.CreatedAt, run.StartedAt); err != nil {
			return fmt.Errorf("flow: list run logs created_at: %w", err)
		}
	}
	return nil
}

// StreamRunLogs yields existing logs for the given run in order, then
// subscribes to new logs via the returned channel. The channel is closed
// when the context is cancelled or the run completes.
func (s *sqlStore) StreamRunLogs(ctx context.Context, runID string, opts ...flow.RunLogOption) (<-chan flow.RunLog, error) {
	f := flow.ApplyRunLogOptions(opts...)

	// Check if the run exists and is completed.
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("flow: stream run logs: %w", err)
	}

	ch := make(chan flow.RunLog, 64)

	// Register subscriber before replaying history to avoid race.
	s.logSubsMu.Lock()
	s.logSubs[runID] = append(s.logSubs[runID], ch)
	s.logSubsMu.Unlock()

	go func() {
		defer func() {
			// Unregister subscriber.
			s.logSubsMu.Lock()
			subs := s.logSubs[runID]
			for i, sub := range subs {
				if sub == ch {
					s.logSubs[runID] = append(subs[:i], subs[i+1:]...)
					break
				}
			}
			if len(s.logSubs[runID]) == 0 {
				delete(s.logSubs, runID)
			}
			s.logSubsMu.Unlock()
			close(ch)
		}()

		// Replay existing logs.
		existing, err := s.ListRunLogs(ctx, runID, opts...)
		if err != nil {
			return
		}
		for _, l := range existing {
			if f.NodeID != "" && l.NodeID != f.NodeID {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- l:
			}
		}

		// If run is already completed, close after replay.
		if run.State == flow.RunCompleted || run.State == flow.RunFailed {
			return
		}

		// Wait for context cancellation.
		<-ctx.Done()
	}()

	return ch, nil
}

// broadcastLog sends a log entry to all active subscribers for the given run.
// Non-blocking: drops the entry if the subscriber channel is full.
func (s *sqlStore) broadcastLog(l flow.RunLog) {
	s.logSubsMu.RLock()
	subs := s.logSubs[l.RunID]
	s.logSubsMu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- l:
		default:
			// Drop if subscriber is too slow.
		}
	}
}

// scanRunLog scans a run log row from a sql.Rows cursor.
func scanRunLog(rows *sql.Rows) (flow.RunLog, error) {
	var l flow.RunLog
	var envStr string
	var createdAt string
	err := rows.Scan(&l.ID, &l.RunID, &l.NodeID, &l.Seq, &envStr, &createdAt)
	if err != nil {
		return flow.RunLog{}, fmt.Errorf("flow: scan run log: %w", err)
	}
	if err := validateRunLogSeq(l.Seq); err != nil {
		return flow.RunLog{}, fmt.Errorf("flow: scan run log seq: %w", err)
	}
	l.Envelope = json.RawMessage(envStr)
	if err := validateRunLogEnvelope(l.Envelope); err != nil {
		return flow.RunLog{}, fmt.Errorf("flow: scan run log envelope: %w", err)
	}
	var parseErr error
	l.CreatedAt, parseErr = sqlutil.ScanTimestamp(createdAt)
	if parseErr != nil {
		return flow.RunLog{}, fmt.Errorf("flow: scan run log created_at: %w", parseErr)
	}
	return l, nil
}

func validateRunLogSeq(seq int) error {
	if seq < 1 {
		return fmt.Errorf("seq must be positive")
	}
	return nil
}
