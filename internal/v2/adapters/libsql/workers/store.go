package workers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
)

// ErrNotFound indicates missing worker rows.
var ErrNotFound = errors.New("v2 workers: not found")

// Store persists runtime worker records.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Store) Upsert(ctx context.Context, record coreworker.Record) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 workers upsert: nil store")
	}
	record = normalizeRecord(record, s.now)
	if strings.TrimSpace(record.WorkerID) == "" {
		return fmt.Errorf("v2 workers upsert: worker_id is required")
	}

	metadataJSON, err := marshalJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("v2 workers upsert metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO v2_runtime_workers (
			worker_id, backend_kind, backend_worker_ref, agent_id, run_id, session_id,
			parent_agent_id, parent_worker_id, workspace_id, role, status, tag, pid,
			started_at, updated_at, heartbeat_at, stop_reason, exit_code, metadata_json, raw_state
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20
		)
		ON CONFLICT(worker_id) DO UPDATE SET
			backend_kind = excluded.backend_kind,
			backend_worker_ref = excluded.backend_worker_ref,
			agent_id = excluded.agent_id,
			run_id = excluded.run_id,
			session_id = excluded.session_id,
			parent_agent_id = excluded.parent_agent_id,
			parent_worker_id = excluded.parent_worker_id,
			workspace_id = excluded.workspace_id,
			role = excluded.role,
			status = excluded.status,
			tag = excluded.tag,
			pid = excluded.pid,
			started_at = COALESCE(excluded.started_at, v2_runtime_workers.started_at),
			updated_at = excluded.updated_at,
			heartbeat_at = COALESCE(excluded.heartbeat_at, v2_runtime_workers.heartbeat_at),
			stop_reason = COALESCE(excluded.stop_reason, v2_runtime_workers.stop_reason),
			exit_code = excluded.exit_code,
			metadata_json = excluded.metadata_json,
			raw_state = COALESCE(excluded.raw_state, v2_runtime_workers.raw_state)
	`,
		record.WorkerID,
		string(record.BackendKind),
		record.BackendWorkerRef,
		record.AgentID,
		record.RunID,
		record.SessionID,
		record.ParentAgentID,
		record.ParentWorkerID,
		record.WorkspaceID,
		record.Role,
		string(record.Status),
		record.Tag,
		record.PID,
		formatNullableTime(record.StartedAt),
		sqlutil.FormatTimestamp(record.UpdatedAt),
		formatNullableTime(record.HeartbeatAt),
		nullIfEmpty(record.StopReason),
		record.ExitCode,
		metadataJSON,
		nullableBytes(record.RawState),
	)
	if err != nil {
		return fmt.Errorf("v2 workers upsert: %w", err)
	}
	return nil
}

func (s *Store) Worker(ctx context.Context, req coreworker.LookupRequest) (coreworker.Record, error) {
	if s == nil || s.db == nil {
		return coreworker.Record{}, fmt.Errorf("v2 workers get: nil store")
	}
	switch {
	case strings.TrimSpace(req.WorkerID) != "":
		return s.queryOne(ctx, `SELECT `+selectColumns+` FROM v2_runtime_workers WHERE worker_id = $1`, strings.TrimSpace(req.WorkerID))
	case strings.TrimSpace(req.AgentID) != "":
		return s.queryOne(ctx, `SELECT `+selectColumns+` FROM v2_runtime_workers WHERE agent_id = $1 ORDER BY updated_at DESC LIMIT 1`, strings.TrimSpace(req.AgentID))
	default:
		return coreworker.Record{}, ErrNotFound
	}
}

func (s *Store) Children(ctx context.Context, req coreworker.ChildrenRequest) ([]coreworker.Record, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 workers children: nil store")
	}
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case strings.TrimSpace(req.ParentWorkerID) != "":
		rows, err = s.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM v2_runtime_workers WHERE parent_worker_id = $1 ORDER BY worker_id ASC`, strings.TrimSpace(req.ParentWorkerID))
	case strings.TrimSpace(req.ParentAgentID) != "":
		rows, err = s.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM v2_runtime_workers WHERE parent_agent_id = $1 ORDER BY worker_id ASC`, strings.TrimSpace(req.ParentAgentID))
	default:
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("v2 workers children query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []coreworker.Record
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("v2 workers children rows: %w", err)
	}
	return out, nil
}

func (s *Store) Active(ctx context.Context, backend coreworker.BackendKind) ([]coreworker.Record, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 workers active: nil store")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM v2_runtime_workers WHERE backend_kind = $1 AND status IN ($2, $3, $4) ORDER BY updated_at DESC, worker_id ASC`,
		string(backend),
		string(coreworker.StatusStarting),
		string(coreworker.StatusRunning),
		string(coreworker.StatusStopping),
	)
	if err != nil {
		return nil, fmt.Errorf("v2 workers active query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []coreworker.Record
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("v2 workers active rows: %w", err)
	}
	return out, nil
}

const selectColumns = `
	worker_id, backend_kind, COALESCE(backend_worker_ref, ''), COALESCE(agent_id, ''),
	COALESCE(run_id, ''), COALESCE(session_id, ''), COALESCE(parent_agent_id, ''),
	COALESCE(parent_worker_id, ''), COALESCE(workspace_id, ''), COALESCE(role, ''),
	status, COALESCE(tag, ''), COALESCE(pid, ''), COALESCE(started_at, ''),
	updated_at, COALESCE(heartbeat_at, ''), COALESCE(stop_reason, ''), COALESCE(exit_code, 0),
	COALESCE(metadata_json, ''), raw_state`

func (s *Store) queryOne(ctx context.Context, query string, arg string) (coreworker.Record, error) {
	rows, err := s.db.QueryContext(ctx, query, arg)
	if err != nil {
		return coreworker.Record{}, fmt.Errorf("v2 workers query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return coreworker.Record{}, ErrNotFound
	}
	record, err := scanRecord(rows)
	if err != nil {
		return coreworker.Record{}, err
	}
	return record, nil
}

func scanRecord(scanner interface{ Scan(dest ...any) error }) (coreworker.Record, error) {
	var (
		record                                         coreworker.Record
		backendKind, startedAt, updatedAt, heartbeatAt string
		metadataJSON                                   string
		rawState                                       []byte
	)
	if err := scanner.Scan(
		&record.WorkerID,
		&backendKind,
		&record.BackendWorkerRef,
		&record.AgentID,
		&record.RunID,
		&record.SessionID,
		&record.ParentAgentID,
		&record.ParentWorkerID,
		&record.WorkspaceID,
		&record.Role,
		&record.Status,
		&record.Tag,
		&record.PID,
		&startedAt,
		&updatedAt,
		&heartbeatAt,
		&record.StopReason,
		&record.ExitCode,
		&metadataJSON,
		&rawState,
	); err != nil {
		return coreworker.Record{}, fmt.Errorf("v2 workers scan: %w", err)
	}
	record.BackendKind = coreworker.BackendKind(strings.TrimSpace(backendKind))
	if ts, err := parseNullableTime(startedAt); err != nil {
		return coreworker.Record{}, err
	} else {
		record.StartedAt = ts
	}
	if ts, err := parseNullableTime(updatedAt); err != nil {
		return coreworker.Record{}, err
	} else {
		record.UpdatedAt = ts
	}
	if ts, err := parseNullableTime(heartbeatAt); err != nil {
		return coreworker.Record{}, err
	} else {
		record.HeartbeatAt = ts
	}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &record.Metadata); err != nil {
			return coreworker.Record{}, fmt.Errorf("v2 workers decode metadata: %w", err)
		}
	}
	if len(rawState) > 0 {
		record.RawState = append([]byte(nil), rawState...)
	}
	return record, nil
}

func normalizeRecord(record coreworker.Record, now func() time.Time) coreworker.Record {
	record.WorkerID = strings.TrimSpace(record.WorkerID)
	record.BackendWorkerRef = strings.TrimSpace(record.BackendWorkerRef)
	record.AgentID = strings.TrimSpace(record.AgentID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.ParentAgentID = strings.TrimSpace(record.ParentAgentID)
	record.ParentWorkerID = strings.TrimSpace(record.ParentWorkerID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.Role = strings.TrimSpace(record.Role)
	record.Tag = strings.TrimSpace(record.Tag)
	record.PID = strings.TrimSpace(record.PID)
	record.StopReason = strings.TrimSpace(record.StopReason)
	if record.BackendKind == "" {
		record.BackendKind = coreworker.BackendUnknown
	}
	if record.Status == "" {
		record.Status = coreworker.StatusUnknown
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now().UTC()
	}
	return record
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func nullableBytes(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func formatNullableTime(ts time.Time) any {
	if ts.IsZero() {
		return nil
	}
	return sqlutil.FormatTimestamp(ts.UTC())
}

func parseNullableTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	parsed, err := sqlutil.ScanTimestamp(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("v2 workers parse timestamp: %w", err)
	}
	return parsed, nil
}
