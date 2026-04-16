package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
)

// ErrNotFound indicates a missing evolve store row.
var ErrNotFound = errors.New("evolve store: not found")

// Store defines the persistence contract for DB-authoritative evolve state.
type Store interface {
	Close() error

	SaveRun(ctx context.Context, run model.Run) error
	Run(ctx context.Context, id string) (model.Run, error)
	ActiveRun(ctx context.Context, workspacePath string) (model.Run, bool, error)
	ClearActiveRun(ctx context.Context, workspacePath string) error

	SaveNode(ctx context.Context, node model.Node) error
	Node(ctx context.Context, id string) (model.Node, error)
	NodesByRun(ctx context.Context, runID string) ([]model.Node, error)
	ChildNodes(ctx context.Context, runID, parentID string) ([]model.Node, error)
	FrontierNodes(ctx context.Context, runID string) ([]model.Node, error)

	SaveGate(ctx context.Context, gate model.Gate) error
	GatesByRun(ctx context.Context, runID string) ([]model.Gate, error)
	GatesByNode(ctx context.Context, nodeID string) ([]model.Gate, error)

	SaveAttempt(ctx context.Context, attempt model.Attempt) error
	AttemptsByNode(ctx context.Context, nodeID string) ([]model.Attempt, error)

	SaveGateResult(ctx context.Context, result model.GateResult) error
	GateResultsByAttempt(ctx context.Context, attemptID string) ([]model.GateResult, error)

	SaveAnnotation(ctx context.Context, annotation model.Annotation) error
	AnnotationsByNode(ctx context.Context, nodeID string) ([]model.Annotation, error)

	SaveInfraEvent(ctx context.Context, event model.InfraEvent) error
	InfraEventsByRun(ctx context.Context, runID string) ([]model.InfraEvent, error)
}

type SQLStore struct {
	db    *sql.DB
	close func() error
}

// Open opens the evolve store rooted at storageRoot/evolve.db.
func Open(ctx context.Context, storageRoot string) (*SQLStore, error) {
	dbPath := filepath.Join(storageRoot, "evolve.db")
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, "EVOLVE", filepath.Base(dbPath), MigrateSchema)
	if err != nil {
		return nil, fmt.Errorf("evolve store open: %w", err)
	}
	return NewSQLStore(db, closeFn), nil
}

// NewSQLStore constructs a store over an existing sql.DB.
func NewSQLStore(db *sql.DB, closeFn func() error) *SQLStore {
	return &SQLStore{db: db, close: closeFn}
}

// Close releases the underlying store resources.
func (s *SQLStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *SQLStore) ensure() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("evolve store: nil store")
	}
	return nil
}

// SaveRun inserts or updates a run and its active-run marker.
func (s *SQLStore) SaveRun(ctx context.Context, run model.Run) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := run.Validate(); err != nil {
		return fmt.Errorf("evolve store save run: %w", err)
	}
	return sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO evolve_runs (
				id, workspace_path, target_path, benchmark_command, metric, status, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8
			)
			ON CONFLICT(id) DO UPDATE SET
				workspace_path = excluded.workspace_path,
				target_path = excluded.target_path,
				benchmark_command = excluded.benchmark_command,
				metric = excluded.metric,
				status = excluded.status,
				updated_at = excluded.updated_at`,
			run.ID,
			run.WorkspacePath,
			run.TargetPath,
			run.BenchmarkCommand,
			string(run.Metric),
			string(run.Status),
			sqlutil.FormatTimestamp(run.CreatedAt),
			sqlutil.FormatTimestamp(run.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("upsert run: %w", err)
		}
		if run.Active {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO evolve_active_runs (workspace_path, run_id) VALUES ($1, $2)
				ON CONFLICT(workspace_path) DO UPDATE SET run_id = excluded.run_id`,
				run.WorkspacePath,
				run.ID,
			)
			if err != nil {
				return fmt.Errorf("set active run: %w", err)
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM evolve_active_runs WHERE workspace_path = $1 AND run_id = $2`, run.WorkspacePath, run.ID)
		if err != nil {
			return fmt.Errorf("clear active run marker: %w", err)
		}
		return nil
	})
}

// Run loads a run by ID.
func (s *SQLStore) Run(ctx context.Context, id string) (model.Run, error) {
	if err := s.ensure(); err != nil {
		return model.Run{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM evolve_runs r WHERE r.id = $1`, id)
	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Run{}, ErrNotFound
		}
		return model.Run{}, fmt.Errorf("evolve store run: %w", err)
	}
	return run, nil
}

// ActiveRun loads the active run for a workspace.
func (s *SQLStore) ActiveRun(ctx context.Context, workspacePath string) (model.Run, bool, error) {
	if err := s.ensure(); err != nil {
		return model.Run{}, false, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT `+runColumns+`
		FROM evolve_runs r
		JOIN evolve_active_runs a ON a.run_id = r.id
		WHERE a.workspace_path = $1`, workspacePath)
	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Run{}, false, nil
		}
		return model.Run{}, false, fmt.Errorf("evolve store active run: %w", err)
	}
	run.Active = true
	return run, true, nil
}

// ClearActiveRun removes the active run marker for a workspace.
func (s *SQLStore) ClearActiveRun(ctx context.Context, workspacePath string) error {
	if err := s.ensure(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM evolve_active_runs WHERE workspace_path = $1`, workspacePath)
	if err != nil {
		return fmt.Errorf("evolve store clear active run: %w", err)
	}
	return nil
}

// SaveNode inserts or updates a node.
func (s *SQLStore) SaveNode(ctx context.Context, node model.Node) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := node.Validate(); err != nil {
		return fmt.Errorf("evolve store save node: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evolve_nodes (
			id, run_id, parent_id, status, hypothesis, score, eval_epoch, branch, worktree_path,
			commit_sha, pruned_reason, current_attempt, evaluated_attempts, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14, $15
		)
		ON CONFLICT(id) DO UPDATE SET
			run_id = excluded.run_id,
			parent_id = excluded.parent_id,
			status = excluded.status,
			hypothesis = excluded.hypothesis,
			score = excluded.score,
			eval_epoch = excluded.eval_epoch,
			branch = excluded.branch,
			worktree_path = excluded.worktree_path,
			commit_sha = excluded.commit_sha,
			pruned_reason = excluded.pruned_reason,
			current_attempt = excluded.current_attempt,
			evaluated_attempts = excluded.evaluated_attempts,
			updated_at = excluded.updated_at`,
		node.ID,
		node.RunID,
		nullIfEmpty(node.ParentID),
		string(node.Status),
		node.Hypothesis,
		nullableFloat(node.Score),
		node.EvalEpoch,
		node.Branch,
		node.WorktreePath,
		node.CommitSHA,
		node.PrunedReason,
		node.CurrentAttempt,
		node.EvaluatedAttempts,
		sqlutil.FormatTimestamp(node.CreatedAt),
		sqlutil.FormatTimestamp(node.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("evolve store save node: %w", err)
	}
	return nil
}

// Node loads a node by ID.
func (s *SQLStore) Node(ctx context.Context, id string) (model.Node, error) {
	if err := s.ensure(); err != nil {
		return model.Node{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM evolve_nodes WHERE id = $1`, id)
	node, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Node{}, ErrNotFound
		}
		return model.Node{}, fmt.Errorf("evolve store node: %w", err)
	}
	return node, nil
}

// NodesByRun lists all nodes in a run in stable tree order.
func (s *SQLStore) NodesByRun(ctx context.Context, runID string) ([]model.Node, error) {
	return s.queryNodes(ctx, `SELECT `+nodeColumns+` FROM evolve_nodes WHERE run_id = $1 ORDER BY created_at ASC, id ASC`, runID)
}

// ChildNodes lists direct children for a parent node.
func (s *SQLStore) ChildNodes(ctx context.Context, runID, parentID string) ([]model.Node, error) {
	return s.queryNodes(ctx, `SELECT `+nodeColumns+` FROM evolve_nodes WHERE run_id = $1 AND parent_id = $2 ORDER BY created_at ASC, id ASC`, runID, parentID)
}

// FrontierNodes lists nodes ready for execution.
func (s *SQLStore) FrontierNodes(ctx context.Context, runID string) ([]model.Node, error) {
	return s.queryNodes(ctx, `SELECT `+nodeColumns+` FROM evolve_nodes WHERE run_id = $1 AND status IN ($2, $3, $4) ORDER BY created_at ASC, id ASC`,
		runID, string(model.NodeStatusPending), string(model.NodeStatusEvaluated), string(model.NodeStatusFailed))
}

// SaveGate inserts or updates a gate.
func (s *SQLStore) SaveGate(ctx context.Context, gate model.Gate) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := gate.Validate(); err != nil {
		return fmt.Errorf("evolve store save gate: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evolve_gates (id, run_id, node_id, name, command, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(node_id, name) DO UPDATE SET
			command = excluded.command,
			run_id = excluded.run_id`,
		gate.ID,
		gate.RunID,
		gate.NodeID,
		gate.Name,
		gate.Command,
		sqlutil.FormatTimestamp(gate.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("evolve store save gate: %w", err)
	}
	return nil
}

// GatesByRun lists gates for a run.
func (s *SQLStore) GatesByRun(ctx context.Context, runID string) ([]model.Gate, error) {
	return s.queryGates(ctx, `SELECT `+gateColumns+` FROM evolve_gates WHERE run_id = $1 ORDER BY created_at ASC, id ASC`, runID)
}

// GatesByNode lists gates defined on a node.
func (s *SQLStore) GatesByNode(ctx context.Context, nodeID string) ([]model.Gate, error) {
	return s.queryGates(ctx, `SELECT `+gateColumns+` FROM evolve_gates WHERE node_id = $1 ORDER BY created_at ASC, id ASC`, nodeID)
}

// SaveAttempt inserts or updates an attempt.
func (s *SQLStore) SaveAttempt(ctx context.Context, attempt model.Attempt) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := attempt.Validate(); err != nil {
		return fmt.Errorf("evolve store save attempt: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evolve_attempts (
			id, node_id, attempt_no, status, score, benchmark_artifact, trace_artifact,
			diff_artifact, error, started_at, finished_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
		ON CONFLICT(id) DO UPDATE SET
			node_id = excluded.node_id,
			attempt_no = excluded.attempt_no,
			status = excluded.status,
			score = excluded.score,
			benchmark_artifact = excluded.benchmark_artifact,
			trace_artifact = excluded.trace_artifact,
			diff_artifact = excluded.diff_artifact,
			error = excluded.error,
			finished_at = excluded.finished_at`,
		attempt.ID,
		attempt.NodeID,
		attempt.AttemptNo,
		string(attempt.Status),
		nullableFloat(attempt.Score),
		attempt.BenchmarkArtifact,
		attempt.TraceArtifact,
		attempt.DiffArtifact,
		attempt.Error,
		sqlutil.FormatTimestamp(attempt.StartedAt),
		nullableTime(attempt.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("evolve store save attempt: %w", err)
	}
	return nil
}

// AttemptsByNode lists attempts for a node.
func (s *SQLStore) AttemptsByNode(ctx context.Context, nodeID string) ([]model.Attempt, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+attemptColumns+` FROM evolve_attempts WHERE node_id = $1 ORDER BY attempt_no ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("evolve store attempts by node: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close evolve attempt rows") }()

	var attempts []model.Attempt
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolve store attempts rows: %w", err)
	}
	return attempts, nil
}

// SaveGateResult inserts or updates a gate result.
func (s *SQLStore) SaveGateResult(ctx context.Context, result model.GateResult) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return fmt.Errorf("evolve store save gate result: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evolve_gate_results (
			attempt_id, gate_name, source_node_id, passed, return_code, log_artifact
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
		ON CONFLICT(attempt_id, gate_name) DO UPDATE SET
			source_node_id = excluded.source_node_id,
			passed = excluded.passed,
			return_code = excluded.return_code,
			log_artifact = excluded.log_artifact`,
		result.AttemptID,
		result.GateName,
		result.SourceNodeID,
		boolText(result.Passed),
		nullableInt(result.ReturnCode),
		result.LogArtifact,
	)
	if err != nil {
		return fmt.Errorf("evolve store save gate result: %w", err)
	}
	return nil
}

// GateResultsByAttempt lists gate results for an attempt.
func (s *SQLStore) GateResultsByAttempt(ctx context.Context, attemptID string) ([]model.GateResult, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT attempt_id, gate_name, source_node_id, passed, COALESCE(return_code, 0), return_code IS NOT NULL, COALESCE(log_artifact, '') FROM evolve_gate_results WHERE attempt_id = $1 ORDER BY gate_name ASC`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("evolve store gate results by attempt: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close evolve gate result rows") }()

	var results []model.GateResult
	for rows.Next() {
		var (
			result     model.GateResult
			passedText string
			returnCode int
			hasCode    bool
		)
		if err := rows.Scan(&result.AttemptID, &result.GateName, &result.SourceNodeID, &passedText, &returnCode, &hasCode, &result.LogArtifact); err != nil {
			return nil, fmt.Errorf("evolve store scan gate result: %w", err)
		}
		result.Passed = passedText == "true"
		if hasCode {
			result.ReturnCode = &returnCode
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolve store gate result rows: %w", err)
	}
	return results, nil
}

// SaveAnnotation inserts or updates an annotation.
func (s *SQLStore) SaveAnnotation(ctx context.Context, annotation model.Annotation) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := annotation.Validate(); err != nil {
		return fmt.Errorf("evolve store save annotation: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evolve_annotations (id, run_id, node_id, task_id, analysis, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(id) DO UPDATE SET
			run_id = excluded.run_id,
			node_id = excluded.node_id,
			task_id = excluded.task_id,
			analysis = excluded.analysis`,
		annotation.ID,
		annotation.RunID,
		nullIfEmpty(annotation.NodeID),
		nullIfEmpty(annotation.TaskID),
		annotation.Analysis,
		sqlutil.FormatTimestamp(annotation.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("evolve store save annotation: %w", err)
	}
	return nil
}

// AnnotationsByNode lists annotations attached to a node.
func (s *SQLStore) AnnotationsByNode(ctx context.Context, nodeID string) ([]model.Annotation, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, COALESCE(node_id, ''), COALESCE(task_id, ''), analysis, created_at FROM evolve_annotations WHERE node_id = $1 ORDER BY created_at ASC, id ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("evolve store annotations by node: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close evolve annotation rows") }()

	var annotations []model.Annotation
	for rows.Next() {
		var annotation model.Annotation
		var createdAt string
		if err := rows.Scan(&annotation.ID, &annotation.RunID, &annotation.NodeID, &annotation.TaskID, &annotation.Analysis, &createdAt); err != nil {
			return nil, fmt.Errorf("evolve store scan annotation: %w", err)
		}
		t, err := sqlutil.ScanTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("evolve store annotation created_at: %w", err)
		}
		annotation.CreatedAt = t
		annotations = append(annotations, annotation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolve store annotation rows: %w", err)
	}
	return annotations, nil
}

// SaveInfraEvent inserts or updates an infra event.
func (s *SQLStore) SaveInfraEvent(ctx context.Context, event model.InfraEvent) error {
	if err := s.ensure(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("evolve store save infra event: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evolve_infra_events (id, run_id, message, breaking, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(id) DO UPDATE SET
			run_id = excluded.run_id,
			message = excluded.message,
			breaking = excluded.breaking`,
		event.ID,
		event.RunID,
		event.Message,
		boolText(event.Breaking),
		sqlutil.FormatTimestamp(event.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("evolve store save infra event: %w", err)
	}
	return nil
}

// InfraEventsByRun lists infra events for a run.
func (s *SQLStore) InfraEventsByRun(ctx context.Context, runID string) ([]model.InfraEvent, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, message, breaking, created_at FROM evolve_infra_events WHERE run_id = $1 ORDER BY created_at ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("evolve store infra events by run: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close evolve infra event rows") }()

	var events []model.InfraEvent
	for rows.Next() {
		var event model.InfraEvent
		var breaking, createdAt string
		if err := rows.Scan(&event.ID, &event.RunID, &event.Message, &breaking, &createdAt); err != nil {
			return nil, fmt.Errorf("evolve store scan infra event: %w", err)
		}
		t, err := sqlutil.ScanTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("evolve store infra event created_at: %w", err)
		}
		event.Breaking = breaking == "true"
		event.CreatedAt = t
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolve store infra event rows: %w", err)
	}
	return events, nil
}

const runColumns = `
	r.id, r.workspace_path, r.target_path, r.benchmark_command, r.metric, r.status,
	EXISTS(SELECT 1 FROM evolve_active_runs a WHERE a.run_id = r.id), r.created_at, r.updated_at`

func scanRun(scanner interface{ Scan(dest ...any) error }) (model.Run, error) {
	var run model.Run
	var metric, status, createdAt, updatedAt string
	if err := scanner.Scan(&run.ID, &run.WorkspacePath, &run.TargetPath, &run.BenchmarkCommand, &metric, &status, &run.Active, &createdAt, &updatedAt); err != nil {
		return model.Run{}, err
	}
	run.Metric = model.MetricDirection(metric)
	run.Status = model.RunStatus(status)
	var err error
	run.CreatedAt, err = sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return model.Run{}, fmt.Errorf("evolve store run created_at: %w", err)
	}
	run.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAt)
	if err != nil {
		return model.Run{}, fmt.Errorf("evolve store run updated_at: %w", err)
	}
	return run, nil
}

const nodeColumns = `
	id, run_id, COALESCE(parent_id, ''), status, COALESCE(hypothesis, ''), COALESCE(score, 0), score IS NOT NULL,
	COALESCE(eval_epoch, 0), COALESCE(branch, ''), COALESCE(worktree_path, ''), COALESCE(commit_sha, ''),
	COALESCE(pruned_reason, ''), COALESCE(current_attempt, 0), COALESCE(evaluated_attempts, 0), created_at, updated_at`

func (s *SQLStore) queryNodes(ctx context.Context, query string, args ...any) ([]model.Node, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("evolve store query nodes: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close evolve node rows") }()

	var nodes []model.Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolve store node rows: %w", err)
	}
	return nodes, nil
}

func scanNode(scanner interface{ Scan(dest ...any) error }) (model.Node, error) {
	var (
		node                 model.Node
		status               string
		score                float64
		hasScore             bool
		createdAt, updatedAt string
	)
	if err := scanner.Scan(
		&node.ID, &node.RunID, &node.ParentID, &status, &node.Hypothesis, &score, &hasScore,
		&node.EvalEpoch, &node.Branch, &node.WorktreePath, &node.CommitSHA, &node.PrunedReason,
		&node.CurrentAttempt, &node.EvaluatedAttempts, &createdAt, &updatedAt,
	); err != nil {
		return model.Node{}, fmt.Errorf("evolve store scan node: %w", err)
	}
	node.Status = model.NodeStatus(status)
	if hasScore {
		node.Score = &score
	}
	var err error
	node.CreatedAt, err = sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return model.Node{}, fmt.Errorf("evolve store node created_at: %w", err)
	}
	node.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAt)
	if err != nil {
		return model.Node{}, fmt.Errorf("evolve store node updated_at: %w", err)
	}
	return node, nil
}

const gateColumns = `id, run_id, node_id, name, command, created_at`

func (s *SQLStore) queryGates(ctx context.Context, query string, args ...any) ([]model.Gate, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("evolve store query gates: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close evolve gate rows") }()

	var gates []model.Gate
	for rows.Next() {
		var gate model.Gate
		var createdAt string
		if err := rows.Scan(&gate.ID, &gate.RunID, &gate.NodeID, &gate.Name, &gate.Command, &createdAt); err != nil {
			return nil, fmt.Errorf("evolve store scan gate: %w", err)
		}
		t, err := sqlutil.ScanTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("evolve store gate created_at: %w", err)
		}
		gate.CreatedAt = t
		gates = append(gates, gate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolve store gate rows: %w", err)
	}
	return gates, nil
}

const attemptColumns = `
	id, node_id, attempt_no, status, COALESCE(score, 0), score IS NOT NULL,
	COALESCE(benchmark_artifact, ''), COALESCE(trace_artifact, ''), COALESCE(diff_artifact, ''),
	COALESCE(error, ''), started_at, COALESCE(finished_at, '')`

func scanAttempt(scanner interface{ Scan(dest ...any) error }) (model.Attempt, error) {
	var (
		attempt               model.Attempt
		status                string
		score                 float64
		hasScore              bool
		startedAt, finishedAt string
	)
	if err := scanner.Scan(
		&attempt.ID, &attempt.NodeID, &attempt.AttemptNo, &status, &score, &hasScore,
		&attempt.BenchmarkArtifact, &attempt.TraceArtifact, &attempt.DiffArtifact,
		&attempt.Error, &startedAt, &finishedAt,
	); err != nil {
		return model.Attempt{}, fmt.Errorf("evolve store scan attempt: %w", err)
	}
	attempt.Status = model.AttemptStatus(status)
	if hasScore {
		attempt.Score = &score
	}
	var err error
	attempt.StartedAt, err = sqlutil.ScanTimestamp(startedAt)
	if err != nil {
		return model.Attempt{}, fmt.Errorf("evolve store attempt started_at: %w", err)
	}
	attempt.FinishedAt, err = sqlutil.ScanTimestamp(finishedAt)
	if err != nil {
		return model.Attempt{}, fmt.Errorf("evolve store attempt finished_at: %w", err)
	}
	return attempt, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return sqlutil.FormatTimestamp(value)
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
