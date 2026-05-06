package effects

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

// Store persists replayable model and tool effects.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore constructs an effect journal over an existing sql.DB.
func NewStore(db *sql.DB) *Store {
	return &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// SetNowForTest overrides time source for deterministic tests.
func (s *Store) SetNowForTest(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

// GetModelEffect returns a recorded model effect.
func (s *Store) GetModelEffect(ctx context.Context, key run.EffectKey) (run.ModelEffectRecord, error) {
	if s == nil || s.db == nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects get model: nil store")
	}
	key = normalizeKey(key)
	if err := validateModelKey(key); err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects get model: %w", err)
	}
	record, err := getModelEffect(ctx, s.db, key)
	if err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects get model: %w", err)
	}
	return record, nil
}

// BeginModelEffect records a model-call intent row or returns an existing matching row.
func (s *Store) BeginModelEffect(ctx context.Context, record run.ModelEffectRecord) (run.ModelEffectRecord, error) {
	if s == nil || s.db == nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects begin model: nil store")
	}
	record = normalizeModelIntent(record, s.now)
	if err := validateModelKey(record.EffectKey); err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects begin model: %w", err)
	}

	var out run.ModelEffectRecord
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO v2_model_effects (
				run_id, request_id, turn_id, iteration_index, input_json, status, response_json,
				error_message, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, '', '', $7, $8)
			ON CONFLICT(run_id, request_id, turn_id, iteration_index) DO NOTHING
		`,
			record.RunID,
			record.RequestID,
			record.TurnID,
			record.IterationIndex,
			rawText(record.InputJSON),
			string(run.ModelEffectIntent),
			sqlutil.FormatTimestamp(record.CreatedAt),
			sqlutil.FormatTimestamp(record.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("insert model effect: %w", err)
		}
		out, err = getModelEffect(ctx, tx, record.EffectKey)
		if err != nil {
			return err
		}
		if !sameModelIntent(out, record) {
			return fmt.Errorf("%w: model run_id=%s request_id=%s turn_id=%s iteration_index=%d",
				run.ErrEffectConflict, record.RunID, record.RequestID, record.TurnID, record.IterationIndex)
		}
		return nil
	})
	if err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects begin model: %w", err)
	}
	return out, nil
}

// CompleteModelEffect transitions an intent row to a terminal result.
func (s *Store) CompleteModelEffect(ctx context.Context, record run.ModelEffectRecord) (run.ModelEffectRecord, error) {
	if s == nil || s.db == nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects complete model: nil store")
	}
	record = normalizeModelTerminal(record, s.now)
	if err := validateModelKey(record.EffectKey); err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects complete model: %w", err)
	}
	if !record.Status.IsTerminal() {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects complete model: terminal status is required")
	}

	var out run.ModelEffectRecord
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		existing, err := getModelEffect(ctx, tx, record.EffectKey)
		if err != nil {
			return err
		}
		if len(record.InputJSON) != 0 && !sameModelIntent(existing, record) {
			return fmt.Errorf("%w: model run_id=%s request_id=%s turn_id=%s iteration_index=%d",
				run.ErrEffectConflict, record.RunID, record.RequestID, record.TurnID, record.IterationIndex)
		}
		if existing.Status.IsTerminal() {
			if !sameModelTerminal(existing, record) {
				return fmt.Errorf("%w: terminal model run_id=%s request_id=%s turn_id=%s iteration_index=%d",
					run.ErrEffectConflict, record.RunID, record.RequestID, record.TurnID, record.IterationIndex)
			}
			out = existing
			return nil
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE v2_model_effects
			SET status = $1,
				response_json = $2,
				error_message = $3,
				updated_at = $4
			WHERE run_id = $5
				AND request_id = $6
				AND turn_id = $7
				AND iteration_index = $8
				AND status = $9
		`,
			string(record.Status),
			rawText(record.ResponseJSON),
			record.ErrorMessage,
			sqlutil.FormatTimestamp(record.UpdatedAt),
			record.RunID,
			record.RequestID,
			record.TurnID,
			record.IterationIndex,
			string(run.ModelEffectIntent),
		)
		if err != nil {
			return fmt.Errorf("update model effect: %w", err)
		}
		out, err = getModelEffect(ctx, tx, record.EffectKey)
		return err
	})
	if err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects complete model: %w", err)
	}
	return out, nil
}

// SaveModelEffect records a completed model effect or replays an identical prior write.
func (s *Store) SaveModelEffect(ctx context.Context, record run.ModelEffectRecord) (run.ModelEffectRecord, error) {
	if record.Status == "" {
		record.Status = run.ModelEffectSucceeded
	}
	if _, err := s.BeginModelEffect(ctx, record); err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects save model: %w", err)
	}
	out, err := s.CompleteModelEffect(ctx, record)
	if err != nil {
		return run.ModelEffectRecord{}, fmt.Errorf("v2 effects save model: %w", err)
	}
	return out, nil
}

// GetToolEffect returns a recorded tool effect.
func (s *Store) GetToolEffect(ctx context.Context, key run.EffectKey) (run.ToolEffectRecord, error) {
	if s == nil || s.db == nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects get tool: nil store")
	}
	key = normalizeKey(key)
	if err := validateToolKey(key); err != nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects get tool: %w", err)
	}
	record, err := getToolEffect(ctx, s.db, key)
	if err != nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects get tool: %w", err)
	}
	return record, nil
}

// BeginToolEffect records an intent row or returns an existing matching row.
func (s *Store) BeginToolEffect(ctx context.Context, record run.ToolEffectRecord) (run.ToolEffectRecord, error) {
	if s == nil || s.db == nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects begin tool: nil store")
	}
	record = normalizeToolIntent(record, s.now)
	if err := validateToolKey(record.EffectKey); err != nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects begin tool: %w", err)
	}
	if record.ToolName == "" {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects begin tool: tool_name is required")
	}

	var out run.ToolEffectRecord
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO v2_tool_effects (
				run_id, request_id, turn_id, iteration_index, tool_call_id, tool_name, args_json,
				replay_policy, status, result_json, error_message, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', '', $10, $11)
			ON CONFLICT(run_id, request_id, turn_id, iteration_index, tool_call_id) DO NOTHING
		`,
			record.RunID,
			record.RequestID,
			record.TurnID,
			record.IterationIndex,
			record.ToolCallID,
			record.ToolName,
			rawText(record.ArgsJSON),
			record.ReplayPolicy,
			string(run.ToolEffectIntent),
			sqlutil.FormatTimestamp(record.CreatedAt),
			sqlutil.FormatTimestamp(record.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("insert tool effect: %w", err)
		}
		out, err = getToolEffect(ctx, tx, record.EffectKey)
		if err != nil {
			return err
		}
		if !sameToolIntent(out, record) {
			return fmt.Errorf("%w: tool run_id=%s request_id=%s turn_id=%s iteration_index=%d tool_call_id=%s",
				run.ErrEffectConflict, record.RunID, record.RequestID, record.TurnID, record.IterationIndex, record.ToolCallID)
		}
		return nil
	})
	if err != nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects begin tool: %w", err)
	}
	return out, nil
}

// CompleteToolEffect transitions an intent row to a terminal result.
func (s *Store) CompleteToolEffect(ctx context.Context, record run.ToolEffectRecord) (run.ToolEffectRecord, error) {
	if s == nil || s.db == nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects complete tool: nil store")
	}
	record = normalizeToolTerminal(record, s.now)
	if err := validateToolKey(record.EffectKey); err != nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects complete tool: %w", err)
	}
	if !record.Status.IsTerminal() {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects complete tool: terminal status is required")
	}

	var out run.ToolEffectRecord
	err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		existing, err := getToolEffect(ctx, tx, record.EffectKey)
		if err != nil {
			return err
		}
		if record.ToolName != "" || len(record.ArgsJSON) != 0 {
			if !sameToolIntent(existing, record) {
				return fmt.Errorf("%w: tool run_id=%s request_id=%s turn_id=%s iteration_index=%d tool_call_id=%s",
					run.ErrEffectConflict, record.RunID, record.RequestID, record.TurnID, record.IterationIndex, record.ToolCallID)
			}
		}
		if existing.Status.IsTerminal() {
			if !sameToolTerminal(existing, record) {
				return fmt.Errorf("%w: terminal tool run_id=%s request_id=%s turn_id=%s iteration_index=%d tool_call_id=%s",
					run.ErrEffectConflict, record.RunID, record.RequestID, record.TurnID, record.IterationIndex, record.ToolCallID)
			}
			out = existing
			return nil
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE v2_tool_effects
			SET status = $1,
				result_json = $2,
				error_message = $3,
				updated_at = $4
			WHERE run_id = $5
				AND request_id = $6
				AND turn_id = $7
				AND iteration_index = $8
				AND tool_call_id = $9
				AND status = $10
		`,
			string(record.Status),
			rawText(record.ResultJSON),
			record.ErrorMessage,
			sqlutil.FormatTimestamp(record.UpdatedAt),
			record.RunID,
			record.RequestID,
			record.TurnID,
			record.IterationIndex,
			record.ToolCallID,
			string(run.ToolEffectIntent),
		)
		if err != nil {
			return fmt.Errorf("update tool effect: %w", err)
		}
		out, err = getToolEffect(ctx, tx, record.EffectKey)
		return err
	})
	if err != nil {
		return run.ToolEffectRecord{}, fmt.Errorf("v2 effects complete tool: %w", err)
	}
	return out, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getModelEffect(ctx context.Context, db queryer, key run.EffectKey) (run.ModelEffectRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT run_id, request_id, turn_id, iteration_index, COALESCE(input_json, ''), status,
			COALESCE(response_json, ''), COALESCE(error_message, ''), created_at, updated_at
		FROM v2_model_effects
		WHERE run_id = $1 AND request_id = $2 AND turn_id = $3 AND iteration_index = $4
	`, key.RunID, key.RequestID, key.TurnID, key.IterationIndex)
	return scanModel(row)
}

func getToolEffect(ctx context.Context, db queryer, key run.EffectKey) (run.ToolEffectRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT run_id, request_id, turn_id, iteration_index, tool_call_id, tool_name, COALESCE(args_json, ''),
			COALESCE(replay_policy, ''), status, COALESCE(result_json, ''), COALESCE(error_message, ''),
			created_at, updated_at
		FROM v2_tool_effects
		WHERE run_id = $1 AND request_id = $2 AND turn_id = $3 AND iteration_index = $4 AND tool_call_id = $5
	`, key.RunID, key.RequestID, key.TurnID, key.IterationIndex, key.ToolCallID)
	return scanTool(row)
}

func scanModel(scanner interface{ Scan(dest ...any) error }) (run.ModelEffectRecord, error) {
	var record run.ModelEffectRecord
	var inputJSON, status, responseJSON, createdAt, updatedAt string
	if err := scanner.Scan(
		&record.RunID,
		&record.RequestID,
		&record.TurnID,
		&record.IterationIndex,
		&inputJSON,
		&status,
		&responseJSON,
		&record.ErrorMessage,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return run.ModelEffectRecord{}, run.ErrEffectNotFound
		}
		return run.ModelEffectRecord{}, fmt.Errorf("scan model effect: %w", err)
	}
	record.InputJSON = cloneRaw(inputJSON)
	record.Status = run.ModelEffectStatus(strings.TrimSpace(status))
	record.ResponseJSON = cloneRaw(responseJSON)
	if record.Status == "" && len(record.ResponseJSON) > 0 {
		record.Status = run.ModelEffectSucceeded
	}
	var err error
	if record.CreatedAt, err = parseTime(createdAt, "created_at"); err != nil {
		return run.ModelEffectRecord{}, err
	}
	if record.UpdatedAt, err = parseTime(updatedAt, "updated_at"); err != nil {
		return run.ModelEffectRecord{}, err
	}
	return record, nil
}

func scanTool(scanner interface{ Scan(dest ...any) error }) (run.ToolEffectRecord, error) {
	var record run.ToolEffectRecord
	var argsJSON, replayPolicy, status, resultJSON, createdAt, updatedAt string
	if err := scanner.Scan(
		&record.RunID,
		&record.RequestID,
		&record.TurnID,
		&record.IterationIndex,
		&record.ToolCallID,
		&record.ToolName,
		&argsJSON,
		&replayPolicy,
		&status,
		&resultJSON,
		&record.ErrorMessage,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return run.ToolEffectRecord{}, run.ErrEffectNotFound
		}
		return run.ToolEffectRecord{}, fmt.Errorf("scan tool effect: %w", err)
	}
	record.ArgsJSON = cloneRaw(argsJSON)
	record.ReplayPolicy = strings.TrimSpace(replayPolicy)
	record.Status = run.ToolEffectStatus(strings.TrimSpace(status))
	record.ResultJSON = cloneRaw(resultJSON)
	var err error
	if record.CreatedAt, err = parseTime(createdAt, "created_at"); err != nil {
		return run.ToolEffectRecord{}, err
	}
	if record.UpdatedAt, err = parseTime(updatedAt, "updated_at"); err != nil {
		return run.ToolEffectRecord{}, err
	}
	return record, nil
}

func normalizeModelIntent(record run.ModelEffectRecord, now func() time.Time) run.ModelEffectRecord {
	record.EffectKey = normalizeKey(record.EffectKey)
	record.Status = run.ModelEffectIntent
	record.ResponseJSON = nil
	record.ErrorMessage = ""
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	return record
}

func normalizeModelTerminal(record run.ModelEffectRecord, now func() time.Time) run.ModelEffectRecord {
	record.EffectKey = normalizeKey(record.EffectKey)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now().UTC()
	}
	return record
}

func normalizeToolIntent(record run.ToolEffectRecord, now func() time.Time) run.ToolEffectRecord {
	record.EffectKey = normalizeKey(record.EffectKey)
	record.ToolName = strings.TrimSpace(record.ToolName)
	record.ReplayPolicy = strings.TrimSpace(record.ReplayPolicy)
	record.Status = run.ToolEffectIntent
	record.ResultJSON = nil
	record.ErrorMessage = ""
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	return record
}

func normalizeToolTerminal(record run.ToolEffectRecord, now func() time.Time) run.ToolEffectRecord {
	record.EffectKey = normalizeKey(record.EffectKey)
	record.ToolName = strings.TrimSpace(record.ToolName)
	record.ReplayPolicy = strings.TrimSpace(record.ReplayPolicy)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now().UTC()
	}
	return record
}

func normalizeKey(key run.EffectKey) run.EffectKey {
	key.RunID = strings.TrimSpace(key.RunID)
	key.RequestID = strings.TrimSpace(key.RequestID)
	key.TurnID = strings.TrimSpace(key.TurnID)
	key.ToolCallID = strings.TrimSpace(key.ToolCallID)
	return key
}

func validateModelKey(key run.EffectKey) error {
	if err := validateBaseKey(key); err != nil {
		return err
	}
	return nil
}

func validateToolKey(key run.EffectKey) error {
	if err := validateBaseKey(key); err != nil {
		return err
	}
	if key.ToolCallID == "" {
		return fmt.Errorf("tool_call_id is required")
	}
	return nil
}

func validateBaseKey(key run.EffectKey) error {
	switch {
	case key.RunID == "":
		return fmt.Errorf("run_id is required")
	case key.RequestID == "":
		return fmt.Errorf("request_id is required")
	case key.TurnID == "":
		return fmt.Errorf("turn_id is required")
	case key.IterationIndex <= 0:
		return fmt.Errorf("iteration_index must be positive")
	default:
		return nil
	}
}

func sameModelIntent(a, b run.ModelEffectRecord) bool {
	return string(a.InputJSON) == string(b.InputJSON)
}

func sameModelTerminal(a, b run.ModelEffectRecord) bool {
	return a.Status == b.Status &&
		string(a.ResponseJSON) == string(b.ResponseJSON) &&
		a.ErrorMessage == b.ErrorMessage
}

func sameToolIntent(a, b run.ToolEffectRecord) bool {
	return a.ToolName == b.ToolName &&
		string(a.ArgsJSON) == string(b.ArgsJSON) &&
		a.ReplayPolicy == b.ReplayPolicy
}

func sameToolTerminal(a, b run.ToolEffectRecord) bool {
	return a.Status == b.Status &&
		string(a.ResultJSON) == string(b.ResultJSON) &&
		a.ErrorMessage == b.ErrorMessage
}

func rawText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func cloneRaw(raw string) []byte {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return append([]byte(nil), raw...)
}

func parseTime(raw, column string) (time.Time, error) {
	parsed, err := sqlutil.ScanTimestamp(strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("v2 effects parse %s: %w", column, err)
	}
	return parsed, nil
}

var _ run.EffectJournal = (*Store)(nil)
