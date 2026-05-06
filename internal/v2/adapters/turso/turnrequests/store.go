package turnrequests

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

// Store persists run-scoped turn request idempotency records.
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

// BeginTurnRequest inserts a running request row or returns the existing row.
func (s *Store) BeginTurnRequest(ctx context.Context, record run.TurnRequestRecord) (run.TurnRequestRecord, bool, error) {
	if s == nil || s.db == nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests begin: nil store")
	}
	record = normalizeBegin(record, s.now)
	if err := validateIdentity(record); err != nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests begin: %w", err)
	}

	var out run.TurnRequestRecord
	inserted := false
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO v2_turn_requests (
				run_id, request_id, turn_id, status, output_json, error_json,
				started_at, completed_at, updated_at
			) VALUES (
				$1, $2, $3, $4, '', '', $5, '', $6
			)
			ON CONFLICT(run_id, request_id) DO NOTHING
		`,
			record.RunID,
			record.RequestID,
			record.TurnID,
			string(run.TurnRequestStatusRunning),
			sqlutil.FormatTimestamp(record.StartedAt),
			sqlutil.FormatTimestamp(record.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("insert rows affected: %w", err)
		}
		inserted = affected == 1

		out, err = getTurnRequest(ctx, tx, record.RunID, record.RequestID)
		if err != nil {
			return err
		}
		if !inserted && out.TurnID != record.TurnID {
			return fmt.Errorf("%w: run_id=%s request_id=%s existing_turn_id=%s new_turn_id=%s",
				run.ErrTurnRequestConflict, record.RunID, record.RequestID, out.TurnID, record.TurnID)
		}
		return nil
	})
	if err != nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests begin: %w", err)
	}
	return out, inserted, nil
}

// RecoverStaleTurnRequest atomically reclaims an old running row for retry.
func (s *Store) RecoverStaleTurnRequest(ctx context.Context, record run.TurnRequestRecord, staleBefore time.Time) (run.TurnRequestRecord, bool, error) {
	if s == nil || s.db == nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests recover stale: nil store")
	}
	record = normalizeBegin(record, s.now)
	if err := validateIdentity(record); err != nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests recover stale: %w", err)
	}
	if staleBefore.IsZero() {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests recover stale: stale_before is required")
	}
	staleBefore = staleBefore.UTC()

	var out run.TurnRequestRecord
	recovered := false
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE v2_turn_requests
			SET status = $1,
				output_json = '',
				error_json = '',
				started_at = $2,
				completed_at = '',
				updated_at = $3
			WHERE run_id = $4
				AND request_id = $5
				AND turn_id = $6
				AND status = $7
				AND updated_at < $8
		`,
			string(run.TurnRequestStatusRunning),
			sqlutil.FormatTimestamp(record.StartedAt),
			sqlutil.FormatTimestamp(record.UpdatedAt),
			record.RunID,
			record.RequestID,
			record.TurnID,
			string(run.TurnRequestStatusRunning),
			sqlutil.FormatTimestamp(staleBefore),
		)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("update rows affected: %w", err)
		}
		recovered = affected == 1

		out, err = getTurnRequest(ctx, tx, record.RunID, record.RequestID)
		if err != nil {
			return err
		}
		if out.TurnID != record.TurnID {
			return fmt.Errorf("%w: run_id=%s request_id=%s existing_turn_id=%s new_turn_id=%s",
				run.ErrTurnRequestConflict, record.RunID, record.RequestID, out.TurnID, record.TurnID)
		}
		return nil
	})
	if err != nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests recover stale: %w", err)
	}
	return out, recovered, nil
}

// TouchTurnRequest refreshes updated_at for an active running request.
func (s *Store) TouchTurnRequest(ctx context.Context, runID, requestID, turnID string, now time.Time) (run.TurnRequestRecord, bool, error) {
	if s == nil || s.db == nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests touch: nil store")
	}
	record := run.TurnRequestRecord{
		RunID:     strings.TrimSpace(runID),
		RequestID: strings.TrimSpace(requestID),
		TurnID:    strings.TrimSpace(turnID),
	}
	if err := validateIdentity(record); err != nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests touch: %w", err)
	}
	if now.IsZero() {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests touch: now is required")
	}
	now = now.UTC()

	var out run.TurnRequestRecord
	touched := false
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE v2_turn_requests
			SET updated_at = $1
			WHERE run_id = $2
				AND request_id = $3
				AND turn_id = $4
				AND status = $5
		`,
			sqlutil.FormatTimestamp(now),
			record.RunID,
			record.RequestID,
			record.TurnID,
			string(run.TurnRequestStatusRunning),
		)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("update rows affected: %w", err)
		}
		touched = affected == 1

		out, err = getTurnRequest(ctx, tx, record.RunID, record.RequestID)
		if err != nil {
			return err
		}
		if out.TurnID != record.TurnID {
			return fmt.Errorf("%w: run_id=%s request_id=%s existing_turn_id=%s new_turn_id=%s",
				run.ErrTurnRequestConflict, record.RunID, record.RequestID, out.TurnID, record.TurnID)
		}
		return nil
	})
	if err != nil {
		return run.TurnRequestRecord{}, false, fmt.Errorf("v2 turn requests touch: %w", err)
	}
	return out, touched, nil
}

// CompleteTurnRequest transitions a running row to a terminal state.
func (s *Store) CompleteTurnRequest(ctx context.Context, record run.TurnRequestRecord) (run.TurnRequestRecord, error) {
	if s == nil || s.db == nil {
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests complete: nil store")
	}
	record = normalizeComplete(record, s.now)
	if err := validateIdentity(record); err != nil {
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests complete: %w", err)
	}
	if !record.Status.IsTerminal() {
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests complete: terminal status is required")
	}

	var out run.TurnRequestRecord
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE v2_turn_requests
			SET status = $1,
				output_json = $2,
				error_json = $3,
				completed_at = $4,
				updated_at = $5
			WHERE run_id = $6
				AND request_id = $7
				AND turn_id = $8
				AND status = $9
		`,
			string(record.Status),
			nullableRawMessage(record.OutputJSON),
			nullableRawMessage(record.ErrorJSON),
			sqlutil.FormatTimestamp(record.CompletedAt),
			sqlutil.FormatTimestamp(record.UpdatedAt),
			record.RunID,
			record.RequestID,
			record.TurnID,
			string(run.TurnRequestStatusRunning),
		)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("update rows affected: %w", err)
		}

		out, err = getTurnRequest(ctx, tx, record.RunID, record.RequestID)
		if err != nil {
			return err
		}
		if out.TurnID != record.TurnID {
			return fmt.Errorf("%w: run_id=%s request_id=%s existing_turn_id=%s new_turn_id=%s",
				run.ErrTurnRequestConflict, record.RunID, record.RequestID, out.TurnID, record.TurnID)
		}
		if affected == 0 && !out.Status.IsTerminal() {
			return fmt.Errorf("v2 turn requests complete: running row was not updated")
		}
		return nil
	})
	if err != nil {
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests complete: %w", err)
	}
	return out, nil
}

func (s *Store) GetTurnRequest(ctx context.Context, runID, requestID string) (run.TurnRequestRecord, error) {
	if s == nil || s.db == nil {
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests get: nil store")
	}
	record, err := getTurnRequest(ctx, s.db, strings.TrimSpace(runID), strings.TrimSpace(requestID))
	if err != nil {
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests get: %w", err)
	}
	return record, nil
}

const selectColumns = `
	run_id, request_id, turn_id, status, COALESCE(output_json, ''), COALESCE(error_json, ''),
	started_at, COALESCE(completed_at, ''), updated_at`

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getTurnRequest(ctx context.Context, db queryer, runID, requestID string) (run.TurnRequestRecord, error) {
	row := db.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM v2_turn_requests WHERE run_id = $1 AND request_id = $2`, runID, requestID)
	record, err := scanRecord(row)
	if err != nil {
		return run.TurnRequestRecord{}, err
	}
	return record, nil
}

func scanRecord(scanner interface{ Scan(dest ...any) error }) (run.TurnRequestRecord, error) {
	var (
		record                            run.TurnRequestRecord
		status, outputJSON, errorJSON     string
		startedAt, completedAt, updatedAt string
	)
	if err := scanner.Scan(
		&record.RunID,
		&record.RequestID,
		&record.TurnID,
		&status,
		&outputJSON,
		&errorJSON,
		&startedAt,
		&completedAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return run.TurnRequestRecord{}, run.ErrTurnRequestNotFound
		}
		return run.TurnRequestRecord{}, fmt.Errorf("v2 turn requests scan: %w", err)
	}
	record.Status = run.TurnRequestStatus(strings.TrimSpace(status))
	if strings.TrimSpace(outputJSON) != "" {
		record.OutputJSON = append([]byte(nil), outputJSON...)
	}
	if strings.TrimSpace(errorJSON) != "" {
		record.ErrorJSON = append([]byte(nil), errorJSON...)
	}
	var err error
	if record.StartedAt, err = parseTime(startedAt, "started_at"); err != nil {
		return run.TurnRequestRecord{}, err
	}
	if record.CompletedAt, err = parseTime(completedAt, "completed_at"); err != nil {
		return run.TurnRequestRecord{}, err
	}
	if record.UpdatedAt, err = parseTime(updatedAt, "updated_at"); err != nil {
		return run.TurnRequestRecord{}, err
	}
	return record, nil
}

func normalizeBegin(record run.TurnRequestRecord, now func() time.Time) run.TurnRequestRecord {
	record.RunID = strings.TrimSpace(record.RunID)
	record.RequestID = strings.TrimSpace(record.RequestID)
	record.TurnID = strings.TrimSpace(record.TurnID)
	record.Status = run.TurnRequestStatusRunning
	record.OutputJSON = nil
	record.ErrorJSON = nil
	record.CompletedAt = time.Time{}
	if record.StartedAt.IsZero() {
		record.StartedAt = now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.StartedAt
	}
	return record
}

func normalizeComplete(record run.TurnRequestRecord, now func() time.Time) run.TurnRequestRecord {
	record.RunID = strings.TrimSpace(record.RunID)
	record.RequestID = strings.TrimSpace(record.RequestID)
	record.TurnID = strings.TrimSpace(record.TurnID)
	if record.CompletedAt.IsZero() {
		record.CompletedAt = now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CompletedAt
	}
	return record
}

func validateIdentity(record run.TurnRequestRecord) error {
	switch {
	case record.RunID == "":
		return fmt.Errorf("run_id is required")
	case record.RequestID == "":
		return fmt.Errorf("request_id is required")
	case record.TurnID == "":
		return fmt.Errorf("turn_id is required")
	default:
		return nil
	}
}

func parseTime(raw, column string) (time.Time, error) {
	parsed, err := sqlutil.ScanTimestamp(strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("v2 turn requests parse %s: %w", column, err)
	}
	return parsed, nil
}

func nullableRawMessage(raw []byte) any {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}
