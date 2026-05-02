// Package flow provides SQLite-backed persistence for flow engine entities.
//
// The store follows the existing pattern from internal/storage/jobs/persist:
// dbutil.OpenStoreDB with env prefix FLOW and default file flow.db.
// All tables use IF NOT EXISTS for idempotent migrations.
package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	flow "github.com/joshka0/foxctl/internal/runtime/flow"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open creates or opens a SQLite-backed flow store at <root>/flow.db and runs
// migrations. The env prefix is FLOW (e.g. FOXCTL_FLOW_DB overrides the path).
func Open(ctx context.Context, root string) (flow.Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "FLOW", "flow.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("flow: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
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
	ddl := `
CREATE TABLE IF NOT EXISTS flows (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    workspace   TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'draft',
    description TEXT NOT NULL DEFAULT '',
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO flows (id, name, workspace, state, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		f.ID, f.Name, f.Workspace, string(f.State), f.Description,
		sqlutil.FormatTimestamp(f.CreatedAt), sqlutil.FormatTimestamp(f.UpdatedAt))
	if err != nil {
		return flow.Flow{}, fmt.Errorf("flow: create flow: %w", err)
	}
	return f, nil
}

func (s *sqlStore) GetFlow(ctx context.Context, id string) (flow.Flow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, workspace, state, description, created_at, updated_at
		FROM flows WHERE id = $1`, id)
	return scanFlow(row)
}

func (s *sqlStore) GetFlowByName(ctx context.Context, workspace, name string) (flow.Flow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, workspace, state, description, created_at, updated_at
		FROM flows WHERE name = $1 AND workspace = $2`, name, workspace)
	return scanFlow(row)
}

func (s *sqlStore) ListFlows(ctx context.Context, workspace string) ([]flow.Flow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, workspace, state, description, created_at, updated_at
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
	now := sqlutil.FormatTimestamp(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
		UPDATE flows SET state = $1, description = $2, updated_at = $3
		WHERE id = $4`,
		string(f.State), f.Description, now, f.ID)
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
	err := row.Scan(&f.ID, &f.Name, &f.Workspace, &state, &f.Description, &created, &updated)
	if err != nil {
		if isNoRows(err) {
			return flow.Flow{}, flow.ErrNotFound
		}
		return flow.Flow{}, fmt.Errorf("flow: scan flow: %w", err)
	}
	f.State = flow.FlowState(state)
	f.CreatedAt, _ = sqlutil.ScanTimestamp(created)
	f.UpdatedAt, _ = sqlutil.ScanTimestamp(updated)
	return f, nil
}

func scanFlowRow(rows *sql.Rows) (flow.Flow, error) {
	var f flow.Flow
	var state string
	var created, updated string
	err := rows.Scan(&f.ID, &f.Name, &f.Workspace, &state, &f.Description, &created, &updated)
	if err != nil {
		return flow.Flow{}, fmt.Errorf("flow: scan flow row: %w", err)
	}
	f.State = flow.FlowState(state)
	f.CreatedAt, _ = sqlutil.ScanTimestamp(created)
	f.UpdatedAt, _ = sqlutil.ScanTimestamp(updated)
	return f, nil
}

// ---------------------------------------------------------------------------
// Node CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) AddNode(ctx context.Context, n flow.FlowNode) (flow.FlowNode, error) {
	configStr := string(n.Config)
	positionStr := ""
	if n.Position != nil {
		posBytes, _ := json.Marshal(n.Position)
		positionStr = string(posBytes)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO flow_nodes (id, flow_id, kind, label, config, position)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		n.ID, n.FlowID, string(n.Kind), n.Label, configStr, positionStr)
	if err != nil {
		return flow.FlowNode{}, fmt.Errorf("flow: add node: %w", err)
	}
	return s.GetNode(ctx, n.ID)
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
	n.Config = json.RawMessage(configStr)
	if positionStr != "" {
		var pos flow.Position
		if jsonErr := json.Unmarshal([]byte(positionStr), &pos); jsonErr == nil {
			n.Position = &pos
		}
	}
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
		n.Config = json.RawMessage(configStr)
		if positionStr != "" {
			var pos flow.Position
			if jsonErr := json.Unmarshal([]byte(positionStr), &pos); jsonErr == nil {
				n.Position = &pos
			}
		}
		nodes = append(nodes, n)
	}
	if nodes == nil {
		nodes = []flow.FlowNode{}
	}
	return nodes, rows.Err()
}

// ---------------------------------------------------------------------------
// Edge CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) AddEdge(ctx context.Context, e flow.FlowEdge) (flow.FlowEdge, error) {
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
			&e.Condition, &retryPolicyStr)
		if err != nil {
			return nil, fmt.Errorf("flow: scan edge: %w", err)
		}
		e.Transform = flow.TransformKind(transform)
		e.Trigger = flow.TriggerKind(trigger)
		if retryPolicyStr != "" {
			var rp flow.RetryPolicy
			if jsonErr := json.Unmarshal([]byte(retryPolicyStr), &rp); jsonErr == nil {
				e.RetryPolicy = &rp
			}
		}
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
		&e.Condition, &retryPolicyStr)
	if err != nil {
		if isNoRows(err) {
			return flow.FlowEdge{}, flow.ErrNotFound
		}
		return flow.FlowEdge{}, fmt.Errorf("flow: scan edge: %w", err)
	}
	e.Transform = flow.TransformKind(transform)
	e.Trigger = flow.TriggerKind(trigger)
	if retryPolicyStr != "" {
		var rp flow.RetryPolicy
		if jsonErr := json.Unmarshal([]byte(retryPolicyStr), &rp); jsonErr == nil {
			e.RetryPolicy = &rp
		}
	}
	return e, nil
}

// ---------------------------------------------------------------------------
// Run CRUD
// ---------------------------------------------------------------------------

func (s *sqlStore) CreateRun(ctx context.Context, r flow.FlowRun) (flow.FlowRun, error) {
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
	r.StartedAt, _ = sqlutil.ScanTimestamp(startedAt)
	if completedAt != "" {
		t, _ := sqlutil.ScanTimestamp(completedAt)
		if !t.IsZero() {
			r.CompletedAt = &t
		}
	}
	return r, nil
}
