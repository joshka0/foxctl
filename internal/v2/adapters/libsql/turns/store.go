package turns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	"github.com/jkatigb/agentctl/internal/storage/vector"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

const (
	ArtifactTypeEmbedding      = "embedding"
	ArtifactTypeAnnotation     = "annotation"
	ArtifactTypeClassification = "classification"
	ArtifactTypeLearning       = "learning"

	defaultArtifactVersion        = "v1"
	artifactVectorIndexName       = "idx_v2_turn_artifacts_embedding_vec"
	artifactVectorCandidateFactor = 12
	artifactVectorCandidateCap    = 500
)

var (
	// ErrArtifactNotFound indicates a requested artifact row is missing.
	ErrArtifactNotFound = errors.New("v2 turns: artifact not found")
	// ErrInvalidArtifactType indicates an unsupported artifact type.
	ErrInvalidArtifactType = errors.New("v2 turns: invalid artifact type")
	// ErrInvalidArtifactRef indicates malformed artifact references.
	ErrInvalidArtifactRef = errors.New("v2 turns: invalid artifact ref")
	// ErrInvalidEmbeddingDimensions indicates embeddings do not match
	// configured vector dimensions when vector mode is active.
	ErrInvalidEmbeddingDimensions = errors.New("v2 turns: invalid embedding dimensions")

	artifactRefPattern = regexp.MustCompile(`^turn/([^/#]+)/artifact/([^/#]+)/([^/#]+)$`)

	allowedArtifactTypes = map[string]struct{}{
		ArtifactTypeEmbedding:      {},
		ArtifactTypeAnnotation:     {},
		ArtifactTypeClassification: {},
		ArtifactTypeLearning:       {},
	}
)

// Artifact is one derived, versioned turn artifact.
type Artifact struct {
	TurnID          string          `json:"turn_id"`
	ArtifactType    string          `json:"artifact_type"`
	ArtifactVersion string          `json:"artifact_version"`
	Ref             string          `json:"ref"`
	Summary         string          `json:"summary,omitempty"`
	ContentJSON     json.RawMessage `json:"content_json,omitempty"`
	MetadataJSON    json.RawMessage `json:"metadata_json,omitempty"`
	Embedding       []float32       `json:"embedding,omitempty"`
	EmbeddingModel  string          `json:"embedding_model,omitempty"`
	CreatedAt       time.Time       `json:"created_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine reads.
func (a Artifact) Clone() Artifact {
	out := a
	if len(a.ContentJSON) > 0 {
		out.ContentJSON = append(json.RawMessage(nil), a.ContentJSON...)
	}
	if len(a.MetadataJSON) > 0 {
		out.MetadataJSON = append(json.RawMessage(nil), a.MetadataJSON...)
	}
	if len(a.Embedding) > 0 {
		out.Embedding = append([]float32(nil), a.Embedding...)
	}
	return out
}

// Store persists turn lineage and derived artifacts.
type Store struct {
	db            *sql.DB
	closeFn       func() error
	now           func() time.Time
	vectorEnabled atomic.Bool
	// vectorDimensions is the configured embedding width for vector mode.
	vectorDimensions int
}

// NewStore constructs a turns store over an existing sql.DB.
func NewStore(db *sql.DB, closeFn func() error) *Store {
	return &Store{
		db:      db,
		closeFn: closeFn,
		now: func() time.Time {
			return time.Now().UTC()
		},
		vectorDimensions: dbdriver.GetDefaultVectorDimensions(),
	}
}

// SetNowForTest overrides time source for deterministic tests.
func (s *Store) SetNowForTest(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

// VectorDimensions returns the configured embedding width for this store.
func (s *Store) VectorDimensions() int {
	if s == nil || s.vectorDimensions <= 0 {
		return dbdriver.GetDefaultVectorDimensions()
	}
	return s.vectorDimensions
}

// Open opens a libsql-first v2 turns store.
func Open(ctx context.Context, storageRoot string) (*Store, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, fmt.Errorf("v2 turns open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultLibSQLConfig(filepath.Join(storageRoot, "v2_turns.libsql"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_TURNS", "v2_turns.db")
	}

	driverType := cfg.Driver
	vectorDims := resolveVectorDimensions(cfg)
	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil && cfg.Driver == dbdriver.DriverLibSQL {
		fallback := dbdriver.DefaultSQLiteConfig(filepath.Join(storageRoot, "v2_turns.db"))
		driverType = fallback.Driver
		db, closeFn, err = dbdriver.OpenDBCompatWithCloser(ctx, fallback, MigrateSchema)
	}
	if err != nil {
		return nil, fmt.Errorf("v2 turns open: %w", err)
	}

	store := NewStore(db, closeFn)
	store.vectorEnabled.Store(driverType == dbdriver.DriverLibSQL || driverType == dbdriver.DriverTurso)
	store.vectorDimensions = vectorDims
	return store, nil
}

func hasDriverOverride() bool {
	return os.Getenv("AGENTCTL_V2_TURNS_DB_DRIVER") != "" || os.Getenv("AGENTCTL_DB_DRIVER") != ""
}

// Close releases database resources.
func (s *Store) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// SaveTurn persists one canonical turn hierarchy.
func (s *Store) SaveTurn(ctx context.Context, turn run.TurnRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 turns save turn: nil store")
	}

	turn = turn.Clone()
	turn.ID = strings.TrimSpace(turn.ID)
	if turn.ID == "" {
		return fmt.Errorf("v2 turns save turn: turn id is required")
	}
	turn.SessionID = strings.TrimSpace(turn.SessionID)
	turn.TraceID = strings.TrimSpace(turn.TraceID)
	turn.RootSpanID = strings.TrimSpace(turn.RootSpanID)
	turn.CorrelationID = strings.TrimSpace(turn.CorrelationID)
	turn.CausationID = strings.TrimSpace(turn.CausationID)
	turn.RequestID = strings.TrimSpace(turn.RequestID)
	turn.ActorID = strings.TrimSpace(turn.ActorID)
	turn.Command = strings.TrimSpace(turn.Command)
	turn.Prompt = strings.TrimSpace(turn.Prompt)
	turn.FinalOutput.ID = strings.TrimSpace(turn.FinalOutput.ID)
	turn.FinalOutput.Role = strings.TrimSpace(turn.FinalOutput.Role)
	turn.FinalOutput.Text = strings.TrimSpace(turn.FinalOutput.Text)

	now := s.now().UTC()
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = now
	}
	if turn.UpdatedAt.IsZero() {
		turn.UpdatedAt = now
	}
	if turn.UpdatedAt.Before(turn.CreatedAt) {
		turn.UpdatedAt = turn.CreatedAt
	}

	return sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO v2_turns (
				id, session_id, turn_index, trace_id, root_span_id, correlation_id, causation_id,
				request_id, actor_id, command, prompt, final_output_id, final_output_role,
				final_output_text, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT(id) DO UPDATE SET
				session_id = excluded.session_id,
				turn_index = excluded.turn_index,
				trace_id = excluded.trace_id,
				root_span_id = excluded.root_span_id,
				correlation_id = excluded.correlation_id,
				causation_id = excluded.causation_id,
				request_id = excluded.request_id,
				actor_id = excluded.actor_id,
				command = excluded.command,
				prompt = excluded.prompt,
				final_output_id = excluded.final_output_id,
				final_output_role = excluded.final_output_role,
				final_output_text = excluded.final_output_text,
				updated_at = excluded.updated_at
		`,
			turn.ID,
			turn.SessionID,
			turn.TurnIndex,
			turn.TraceID,
			turn.RootSpanID,
			turn.CorrelationID,
			turn.CausationID,
			turn.RequestID,
			turn.ActorID,
			turn.Command,
			turn.Prompt,
			turn.FinalOutput.ID,
			turn.FinalOutput.Role,
			turn.FinalOutput.Text,
			sqlutil.FormatTimestamp(turn.CreatedAt),
			sqlutil.FormatTimestamp(turn.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("upsert turn: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM v2_turn_tool_calls WHERE turn_id = $1`, turn.ID); err != nil {
			return fmt.Errorf("delete tool_calls: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM v2_turn_iterations WHERE turn_id = $1`, turn.ID); err != nil {
			return fmt.Errorf("delete iterations: %w", err)
		}

		for i, iter := range turn.Iterations {
			iter = iter.Clone()
			if iter.IterationIndex <= 0 {
				iter.IterationIndex = i + 1
			}
			iter.TurnID = turn.ID
			iter.TraceID = strings.TrimSpace(iter.TraceID)
			iter.SpanID = strings.TrimSpace(iter.SpanID)
			iter.ParentSpanID = strings.TrimSpace(iter.ParentSpanID)
			iter.Message.ID = strings.TrimSpace(iter.Message.ID)
			iter.Message.Role = strings.TrimSpace(iter.Message.Role)
			iter.Message.Text = strings.TrimSpace(iter.Message.Text)

			_, err := tx.ExecContext(ctx, `
				INSERT INTO v2_turn_iterations (
					turn_id, iteration_index, trace_id, span_id, parent_span_id,
					message_id, message_role, message_text, created_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			`,
				iter.TurnID,
				iter.IterationIndex,
				iter.TraceID,
				iter.SpanID,
				iter.ParentSpanID,
				iter.Message.ID,
				iter.Message.Role,
				iter.Message.Text,
				sqlutil.FormatTimestamp(now),
			)
			if err != nil {
				return fmt.Errorf("insert iteration %d: %w", iter.IterationIndex, err)
			}

			for j, call := range iter.ToolCalls {
				call = call.Clone()
				call.CallID = strings.TrimSpace(call.CallID)
				if call.CallID == "" {
					call.CallID = fmt.Sprintf("tc-%d-%d", iter.IterationIndex, j+1)
				}
				call.Name = strings.TrimSpace(call.Name)
				if call.Name == "" {
					call.Name = "unknown"
				}
				call.Status = strings.TrimSpace(call.Status)
				call.TraceID = strings.TrimSpace(call.TraceID)
				call.SpanID = strings.TrimSpace(call.SpanID)
				call.ParentSpanID = strings.TrimSpace(call.ParentSpanID)
				call.ResultRef.ID = strings.TrimSpace(call.ResultRef.ID)
				call.ResultRef.Kind = strings.TrimSpace(call.ResultRef.Kind)
				call.ResultRef.Text = strings.TrimSpace(call.ResultRef.Text)

				argsJSON := normalizeJSON(call.ArgsJSON, "{}")
				_, err := tx.ExecContext(ctx, `
					INSERT INTO v2_turn_tool_calls (
						turn_id, iteration_index, call_id, trace_id, span_id, parent_span_id,
						name, args_json, status, result_ref_id, result_ref_kind, result_ref_text, created_at
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
				`,
					turn.ID,
					iter.IterationIndex,
					call.CallID,
					call.TraceID,
					call.SpanID,
					call.ParentSpanID,
					call.Name,
					argsJSON,
					call.Status,
					call.ResultRef.ID,
					call.ResultRef.Kind,
					call.ResultRef.Text,
					sqlutil.FormatTimestamp(now),
				)
				if err != nil {
					return fmt.Errorf("insert tool call %s: %w", call.CallID, err)
				}
			}
		}

		return nil
	})
}

// GetTurn loads one canonical turn hierarchy by turn ID.
func (s *Store) GetTurn(ctx context.Context, turnID string) (run.TurnRecord, error) {
	if s == nil || s.db == nil {
		return run.TurnRecord{}, fmt.Errorf("v2 turns get turn: nil store")
	}

	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}

	var (
		turn            run.TurnRecord
		createdAtRaw    string
		updatedAtRaw    string
		finalOutputID   string
		finalOutputRole string
		finalOutputText string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			COALESCE(session_id, ''),
			COALESCE(turn_index, 0),
			COALESCE(trace_id, ''),
			COALESCE(root_span_id, ''),
			COALESCE(correlation_id, ''),
			COALESCE(causation_id, ''),
			COALESCE(request_id, ''),
			COALESCE(actor_id, ''),
			COALESCE(command, ''),
			COALESCE(prompt, ''),
			COALESCE(final_output_id, ''),
			COALESCE(final_output_role, ''),
			COALESCE(final_output_text, ''),
			created_at,
			updated_at
		FROM v2_turns
		WHERE id = $1
	`, turnID).Scan(
		&turn.ID,
		&turn.SessionID,
		&turn.TurnIndex,
		&turn.TraceID,
		&turn.RootSpanID,
		&turn.CorrelationID,
		&turn.CausationID,
		&turn.RequestID,
		&turn.ActorID,
		&turn.Command,
		&turn.Prompt,
		&finalOutputID,
		&finalOutputRole,
		&finalOutputText,
		&createdAtRaw,
		&updatedAtRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}
	if err != nil {
		return run.TurnRecord{}, fmt.Errorf("query turn: %w", err)
	}

	turn.FinalOutput = run.MessageRef{
		ID:   strings.TrimSpace(finalOutputID),
		Role: strings.TrimSpace(finalOutputRole),
		Text: strings.TrimSpace(finalOutputText),
	}
	if turn.CreatedAt, err = sqlutil.ScanTimestamp(createdAtRaw); err != nil {
		return run.TurnRecord{}, fmt.Errorf("parse turn.created_at: %w", err)
	}
	if turn.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAtRaw); err != nil {
		return run.TurnRecord{}, fmt.Errorf("parse turn.updated_at: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(iteration_index, 0),
			COALESCE(trace_id, ''),
			COALESCE(span_id, ''),
			COALESCE(parent_span_id, ''),
			COALESCE(message_id, ''),
			COALESCE(message_role, ''),
			COALESCE(message_text, '')
		FROM v2_turn_iterations
		WHERE turn_id = $1
		ORDER BY iteration_index ASC
	`, turnID)
	if err != nil {
		return run.TurnRecord{}, fmt.Errorf("query iterations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	iterationToIndex := make(map[int]int)
	for rows.Next() {
		iter := run.IterationRecord{TurnID: turnID}
		if err := rows.Scan(
			&iter.IterationIndex,
			&iter.TraceID,
			&iter.SpanID,
			&iter.ParentSpanID,
			&iter.Message.ID,
			&iter.Message.Role,
			&iter.Message.Text,
		); err != nil {
			return run.TurnRecord{}, fmt.Errorf("scan iteration: %w", err)
		}
		iterationToIndex[iter.IterationIndex] = len(turn.Iterations)
		turn.Iterations = append(turn.Iterations, iter)
	}
	if err := rows.Err(); err != nil {
		return run.TurnRecord{}, fmt.Errorf("iterate iterations: %w", err)
	}

	callRows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(iteration_index, 0),
			COALESCE(call_id, ''),
			COALESCE(trace_id, ''),
			COALESCE(span_id, ''),
			COALESCE(parent_span_id, ''),
			COALESCE(name, ''),
			COALESCE(args_json, '{}'),
			COALESCE(status, ''),
			COALESCE(result_ref_id, ''),
			COALESCE(result_ref_kind, ''),
			COALESCE(result_ref_text, '')
		FROM v2_turn_tool_calls
		WHERE turn_id = $1
		ORDER BY iteration_index ASC, rowid ASC
	`, turnID)
	if err != nil {
		return run.TurnRecord{}, fmt.Errorf("query tool calls: %w", err)
	}
	defer func() { _ = callRows.Close() }()

	for callRows.Next() {
		var (
			call    run.ToolCallRecord
			argsRaw string
		)
		if err := callRows.Scan(
			&call.IterationIndex,
			&call.CallID,
			&call.TraceID,
			&call.SpanID,
			&call.ParentSpanID,
			&call.Name,
			&argsRaw,
			&call.Status,
			&call.ResultRef.ID,
			&call.ResultRef.Kind,
			&call.ResultRef.Text,
		); err != nil {
			return run.TurnRecord{}, fmt.Errorf("scan tool call: %w", err)
		}
		argsRaw = strings.TrimSpace(argsRaw)
		if argsRaw == "" {
			argsRaw = "{}"
		}
		if !json.Valid([]byte(argsRaw)) {
			argsRaw = "{}"
		}
		call.ArgsJSON = json.RawMessage(argsRaw)

		pos, ok := iterationToIndex[call.IterationIndex]
		if !ok {
			continue
		}
		turn.Iterations[pos].ToolCalls = append(turn.Iterations[pos].ToolCalls, call)
	}
	if err := callRows.Err(); err != nil {
		return run.TurnRecord{}, fmt.Errorf("iterate tool calls: %w", err)
	}

	return turn.Clone(), nil
}

// ListTurns returns turn records for one session ordered by created time.
func (s *Store) ListTurns(ctx context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 turns list turns: nil store")
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}

	order := "DESC"
	if opts.Asc {
		order = "ASC"
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 64
	}

	query := fmt.Sprintf(`
		SELECT id
		FROM v2_turns
		WHERE session_id = $1
		  AND ($2 = '' OR created_at >= $2)
		  AND ($3 = '' OR created_at <= $3)
		ORDER BY created_at %s, id %s
		LIMIT $4
	`, order, order)

	since := ""
	until := ""
	if !opts.Since.IsZero() {
		since = sqlutil.FormatTimestamp(opts.Since.UTC())
	}
	if !opts.Until.IsZero() {
		until = sqlutil.FormatTimestamp(opts.Until.UTC())
	}

	rows, err := s.db.QueryContext(ctx, query, sessionID, since, until, limit)
	if err != nil {
		return nil, fmt.Errorf("query turn ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	turnIDs := make([]string, 0, limit)
	for rows.Next() {
		var turnID string
		if err := rows.Scan(&turnID); err != nil {
			return nil, fmt.Errorf("scan turn id: %w", err)
		}
		turnIDs = append(turnIDs, strings.TrimSpace(turnID))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turn ids: %w", err)
	}

	out := make([]run.TurnRecord, 0, len(turnIDs))
	for _, turnID := range turnIDs {
		turn, err := s.GetTurn(ctx, turnID)
		if err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, nil
}

// SaveArtifact upserts one versioned artifact under its idempotency key.
func (s *Store) SaveArtifact(ctx context.Context, artifact Artifact) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 turns save artifact: nil store")
	}
	normalized, err := s.normalizeArtifact(artifact)
	if err != nil {
		return err
	}
	if s.vectorEnabled.Load() && len(normalized.Embedding) > 0 {
		if err := s.insertArtifactWithVector(ctx, normalized); err == nil {
			return nil
		} else if !isVectorUnsupported(err) {
			return err
		}
		// Disable vector writes after first unsupported error to avoid repeated
		// failing vector() attempts on fallback drivers.
		s.vectorEnabled.Store(false)
	}
	return s.insertArtifactWithoutVector(ctx, normalized)
}

// GetArtifact loads one artifact row by idempotency key.
func (s *Store) GetArtifact(ctx context.Context, turnID, artifactType, artifactVersion string) (Artifact, error) {
	if s == nil || s.db == nil {
		return Artifact{}, fmt.Errorf("v2 turns get artifact: nil store")
	}
	turnID = strings.TrimSpace(turnID)
	artifactType = strings.TrimSpace(strings.ToLower(artifactType))
	artifactVersion = strings.TrimSpace(artifactVersion)
	if turnID == "" || artifactType == "" || artifactVersion == "" {
		return Artifact{}, ErrArtifactNotFound
	}

	var (
		out             Artifact
		contentJSONRaw  string
		metadataJSONRaw string
		embeddingJSON   string
		createdAtRaw    string
		updatedAtRaw    string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			turn_id,
			artifact_type,
			artifact_version,
			ref,
			COALESCE(summary, ''),
			COALESCE(content_json, '{}'),
			COALESCE(metadata_json, '{}'),
			COALESCE(embedding_json, '[]'),
			COALESCE(embedding_model, ''),
			created_at,
			updated_at
		FROM v2_turn_artifacts
		WHERE turn_id = $1 AND artifact_type = $2 AND artifact_version = $3
	`, turnID, artifactType, artifactVersion).Scan(
		&out.TurnID,
		&out.ArtifactType,
		&out.ArtifactVersion,
		&out.Ref,
		&out.Summary,
		&contentJSONRaw,
		&metadataJSONRaw,
		&embeddingJSON,
		&out.EmbeddingModel,
		&createdAtRaw,
		&updatedAtRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("query artifact: %w", err)
	}

	out.ContentJSON = json.RawMessage(normalizeJSON(json.RawMessage(contentJSONRaw), "{}"))
	out.MetadataJSON = json.RawMessage(normalizeJSON(json.RawMessage(metadataJSONRaw), "{}"))
	if strings.TrimSpace(embeddingJSON) != "" {
		if unmarshalErr := json.Unmarshal([]byte(embeddingJSON), &out.Embedding); unmarshalErr != nil {
			out.Embedding = nil
		}
	}
	if out.CreatedAt, err = sqlutil.ScanTimestamp(createdAtRaw); err != nil {
		return Artifact{}, fmt.Errorf("parse artifact.created_at: %w", err)
	}
	if out.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAtRaw); err != nil {
		return Artifact{}, fmt.Errorf("parse artifact.updated_at: %w", err)
	}

	return out.Clone(), nil
}

// GetArtifactByRef resolves a stable artifact reference.
func (s *Store) GetArtifactByRef(ctx context.Context, ref string) (Artifact, error) {
	turnID, artifactType, artifactVersion, err := ParseArtifactRef(ref)
	if err != nil {
		return Artifact{}, err
	}
	return s.GetArtifact(ctx, turnID, artifactType, artifactVersion)
}

// ListArtifacts returns all artifacts for one turn ordered by update time.
func (s *Store) ListArtifacts(ctx context.Context, turnID string) ([]Artifact, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 turns list artifacts: nil store")
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT artifact_type, artifact_version
		FROM v2_turn_artifacts
		WHERE turn_id = $1
		ORDER BY updated_at DESC, artifact_type ASC
	`, turnID)
	if err != nil {
		return nil, fmt.Errorf("query artifact keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]Artifact, 0, 8)
	for rows.Next() {
		var (
			artifactType    string
			artifactVersion string
		)
		if err := rows.Scan(&artifactType, &artifactVersion); err != nil {
			return nil, fmt.Errorf("scan artifact key: %w", err)
		}
		artifact, err := s.GetArtifact(ctx, turnID, artifactType, artifactVersion)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact keys: %w", err)
	}
	return out, nil
}

// SearchArtifactsByEmbedding returns top artifacts by embedding similarity.
// It prefers native libsql vector functions and falls back to in-process cosine
// scoring when vector SQL is unavailable.
func (s *Store) SearchArtifactsByEmbedding(
	ctx context.Context,
	queryEmbedding []float32,
	opts run.ArtifactSearchOptions,
) (run.ArtifactSearchResult, error) {
	if s == nil || s.db == nil {
		return run.ArtifactSearchResult{}, fmt.Errorf("v2 turns search artifacts: nil store")
	}
	if len(queryEmbedding) == 0 {
		return run.ArtifactSearchResult{
			SearchPath:       run.ArtifactSearchPathDisabled,
			VectorCapability: s.vectorCapability(),
		}, nil
	}
	for i, value := range queryEmbedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return run.ArtifactSearchResult{}, fmt.Errorf("v2 turns search artifacts: embedding contains invalid value at index %d", i)
		}
	}
	if s.vectorEnabled.Load() && s.vectorDimensions > 0 && len(queryEmbedding) != s.vectorDimensions {
		return run.ArtifactSearchResult{}, fmt.Errorf(
			"%w: query=%d configured=%d",
			ErrInvalidEmbeddingDimensions,
			len(queryEmbedding),
			s.vectorDimensions,
		)
	}

	normalized, err := normalizeArtifactSearchOptions(opts)
	if err != nil {
		return run.ArtifactSearchResult{}, err
	}

	if s.vectorEnabled.Load() {
		candidates, err := s.searchArtifactCandidatesVector(ctx, queryEmbedding, normalized)
		if err == nil {
			hits, loadErr := s.loadScoredArtifacts(ctx, candidates)
			if loadErr != nil {
				return run.ArtifactSearchResult{}, loadErr
			}
			return run.ArtifactSearchResult{
				Hits:             hits,
				SearchPath:       run.ArtifactSearchPathVector,
				VectorCapability: run.ArtifactVectorCapabilityEnabled,
			}, nil
		}
		if !isVectorUnsupported(err) {
			return run.ArtifactSearchResult{}, err
		}
		// Disable vector search after first unsupported query to avoid repeated
		// expensive failures on fallback drivers.
		s.vectorEnabled.Store(false)
	}

	candidates, err := s.searchArtifactCandidatesFallback(ctx, queryEmbedding, normalized)
	if err != nil {
		return run.ArtifactSearchResult{}, err
	}
	hits, err := s.loadScoredArtifacts(ctx, candidates)
	if err != nil {
		return run.ArtifactSearchResult{}, err
	}
	return run.ArtifactSearchResult{
		Hits:             hits,
		SearchPath:       run.ArtifactSearchPathFallback,
		VectorCapability: s.vectorCapability(),
	}, nil
}

func (s *Store) vectorCapability() run.ArtifactVectorCapability {
	if s == nil {
		return run.ArtifactVectorCapabilityUnknown
	}
	if s.vectorEnabled.Load() {
		return run.ArtifactVectorCapabilityEnabled
	}
	return run.ArtifactVectorCapabilityDisabled
}

type artifactSimilarityCandidate struct {
	TurnID          string
	ArtifactType    string
	ArtifactVersion string
	Similarity      float64
	Distance        float64
	UpdatedAt       time.Time
}

func normalizeArtifactSearchOptions(opts run.ArtifactSearchOptions) (run.ArtifactSearchOptions, error) {
	opts.SessionID = strings.TrimSpace(opts.SessionID)

	normalizedTypes := make([]string, 0, len(opts.ArtifactTypes))
	typeSet := make(map[string]struct{}, len(opts.ArtifactTypes))
	for _, rawType := range opts.ArtifactTypes {
		artifactType := strings.TrimSpace(strings.ToLower(rawType))
		if artifactType == "" {
			continue
		}
		if _, ok := allowedArtifactTypes[artifactType]; !ok {
			return run.ArtifactSearchOptions{}, ErrInvalidArtifactType
		}
		if _, dup := typeSet[artifactType]; dup {
			continue
		}
		typeSet[artifactType] = struct{}{}
		normalizedTypes = append(normalizedTypes, artifactType)
	}
	sort.Strings(normalizedTypes)
	opts.ArtifactTypes = normalizedTypes

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}
	if opts.MinSimilarity < 0 {
		opts.MinSimilarity = 0
	}
	if opts.MinSimilarity > 1 {
		opts.MinSimilarity = 1
	}
	return opts, nil
}

func (s *Store) searchArtifactCandidatesVector(
	ctx context.Context,
	queryEmbedding []float32,
	opts run.ArtifactSearchOptions,
) ([]artifactSimilarityCandidate, error) {
	vectorStr := float32sToVectorString(queryEmbedding)
	vectorTopKExpr := vectorTopKExpression(artifactVectorIndexName, vectorStr, vectorCandidateLimit(opts))

	where := []string{"a.embedding IS NOT NULL"}
	args := make([]any, 0, 8)
	if opts.SessionID != "" {
		args = append(args, opts.SessionID)
		where = append(where, fmt.Sprintf("t.session_id = $%d", len(args)))
	}
	if len(opts.ArtifactTypes) > 0 {
		typePredicates := make([]string, 0, len(opts.ArtifactTypes))
		for _, artifactType := range opts.ArtifactTypes {
			args = append(args, artifactType)
			typePredicates = append(typePredicates, fmt.Sprintf("$%d", len(args)))
		}
		where = append(where, fmt.Sprintf("a.artifact_type IN (%s)", strings.Join(typePredicates, ", ")))
	}
	args = append(args, opts.Limit)

	query := fmt.Sprintf(`
		SELECT
			a.turn_id,
			a.artifact_type,
			a.artifact_version,
			COALESCE(a.updated_at, ''),
			vector_distance_cos(a.embedding, vector('%s')) AS distance
		FROM %s vt
		JOIN v2_turn_artifacts a ON a.rowid = vt.id
		JOIN v2_turns t ON t.id = a.turn_id
		WHERE %s
		ORDER BY distance ASC, a.updated_at DESC, a.turn_id ASC, a.artifact_type ASC, a.artifact_version ASC
		LIMIT $%d
	`, vectorStr, vectorTopKExpr, strings.Join(where, " AND "), len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query artifact vector similarity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]artifactSimilarityCandidate, 0, opts.Limit)
	for rows.Next() {
		var (
			candidate   artifactSimilarityCandidate
			updatedAt   string
			distanceRaw float64
		)
		if err := rows.Scan(
			&candidate.TurnID,
			&candidate.ArtifactType,
			&candidate.ArtifactVersion,
			&updatedAt,
			&distanceRaw,
		); err != nil {
			return nil, fmt.Errorf("scan artifact vector candidate: %w", err)
		}
		candidate.Distance = distanceRaw
		candidate.Similarity = 1.0 - distanceRaw
		if candidate.Similarity < opts.MinSimilarity {
			continue
		}
		candidate.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact vector candidates: %w", err)
	}
	if len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (s *Store) searchArtifactCandidatesFallback(
	ctx context.Context,
	queryEmbedding []float32,
	opts run.ArtifactSearchOptions,
) ([]artifactSimilarityCandidate, error) {
	where := []string{"COALESCE(a.embedding_json, '[]') <> '[]'"}
	args := make([]any, 0, 8)
	if opts.SessionID != "" {
		args = append(args, opts.SessionID)
		where = append(where, fmt.Sprintf("t.session_id = $%d", len(args)))
	}
	if len(opts.ArtifactTypes) > 0 {
		typePredicates := make([]string, 0, len(opts.ArtifactTypes))
		for _, artifactType := range opts.ArtifactTypes {
			args = append(args, artifactType)
			typePredicates = append(typePredicates, fmt.Sprintf("$%d", len(args)))
		}
		where = append(where, fmt.Sprintf("a.artifact_type IN (%s)", strings.Join(typePredicates, ", ")))
	}
	candidateLimit := opts.Limit * 12
	if candidateLimit < opts.Limit {
		candidateLimit = opts.Limit
	}
	if candidateLimit > 500 {
		candidateLimit = 500
	}
	args = append(args, candidateLimit)

	query := fmt.Sprintf(`
		SELECT
			a.turn_id,
			a.artifact_type,
			a.artifact_version,
			COALESCE(a.embedding_json, '[]'),
			COALESCE(a.updated_at, '')
		FROM v2_turn_artifacts a
		JOIN v2_turns t ON t.id = a.turn_id
		WHERE %s
		ORDER BY a.updated_at DESC, a.turn_id ASC, a.artifact_type ASC, a.artifact_version ASC
		LIMIT $%d
	`, strings.Join(where, " AND "), len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query artifact fallback candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]artifactSimilarityCandidate, 0, candidateLimit)
	for rows.Next() {
		var (
			candidate   artifactSimilarityCandidate
			embeddingJS string
			updatedAt   string
			embedding   []float32
		)
		if err := rows.Scan(
			&candidate.TurnID,
			&candidate.ArtifactType,
			&candidate.ArtifactVersion,
			&embeddingJS,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan artifact fallback candidate: %w", err)
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(embeddingJS)), &embedding); err != nil {
			continue
		}
		if len(embedding) == 0 || len(embedding) != len(queryEmbedding) {
			continue
		}
		candidate.Similarity = vector.Cosine(queryEmbedding, embedding)
		if candidate.Similarity < opts.MinSimilarity {
			continue
		}
		candidate.Distance = 1.0 - candidate.Similarity
		candidate.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact fallback candidates: %w", err)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.Similarity != right.Similarity {
			return left.Similarity > right.Similarity
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		if left.TurnID != right.TurnID {
			return left.TurnID < right.TurnID
		}
		if left.ArtifactType != right.ArtifactType {
			return left.ArtifactType < right.ArtifactType
		}
		return left.ArtifactVersion < right.ArtifactVersion
	})

	if len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}
	return candidates, nil
}

func (s *Store) loadScoredArtifacts(
	ctx context.Context,
	candidates []artifactSimilarityCandidate,
) ([]run.ScoredArtifact, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	out := make([]run.ScoredArtifact, 0, len(candidates))
	for _, candidate := range candidates {
		artifact, err := s.GetArtifact(ctx, candidate.TurnID, candidate.ArtifactType, candidate.ArtifactVersion)
		if errors.Is(err, ErrArtifactNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, run.ScoredArtifact{
			Ref:             artifact.Ref,
			TurnID:          artifact.TurnID,
			ArtifactType:    artifact.ArtifactType,
			ArtifactVersion: artifact.ArtifactVersion,
			Similarity:      candidate.Similarity,
			Distance:        candidate.Distance,
			Summary:         artifact.Summary,
			MetadataJSON:    append(json.RawMessage(nil), artifact.MetadataJSON...),
		})
	}
	return out, nil
}

func (s *Store) normalizeArtifact(artifact Artifact) (Artifact, error) {
	artifact = artifact.Clone()
	artifact.TurnID = strings.TrimSpace(artifact.TurnID)
	artifact.ArtifactType = strings.TrimSpace(strings.ToLower(artifact.ArtifactType))
	artifact.ArtifactVersion = strings.TrimSpace(artifact.ArtifactVersion)
	artifact.Ref = strings.TrimSpace(artifact.Ref)
	artifact.Summary = strings.TrimSpace(artifact.Summary)
	artifact.EmbeddingModel = strings.TrimSpace(artifact.EmbeddingModel)

	if artifact.TurnID == "" {
		return Artifact{}, fmt.Errorf("v2 turns save artifact: turn_id is required")
	}
	if _, ok := allowedArtifactTypes[artifact.ArtifactType]; !ok {
		return Artifact{}, ErrInvalidArtifactType
	}
	if artifact.ArtifactVersion == "" {
		artifact.ArtifactVersion = defaultArtifactVersion
	}
	if artifact.Ref == "" {
		artifact.Ref = BuildArtifactRef(artifact.TurnID, artifact.ArtifactType, artifact.ArtifactVersion)
	}
	artifact.ContentJSON = json.RawMessage(normalizeJSON(artifact.ContentJSON, "{}"))
	artifact.MetadataJSON = json.RawMessage(normalizeJSON(artifact.MetadataJSON, "{}"))
	for i, value := range artifact.Embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return Artifact{}, fmt.Errorf("v2 turns save artifact: embedding contains invalid value at index %d", i)
		}
	}
	now := s.now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	if artifact.UpdatedAt.IsZero() {
		artifact.UpdatedAt = now
	}
	if artifact.UpdatedAt.Before(artifact.CreatedAt) {
		artifact.UpdatedAt = artifact.CreatedAt
	}
	return artifact, nil
}

func (s *Store) insertArtifactWithoutVector(ctx context.Context, artifact Artifact) error {
	embeddingJSON, err := sqlutil.FormatJSON(artifact.Embedding)
	if err != nil {
		return fmt.Errorf("format embedding json: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO v2_turn_artifacts (
			turn_id, artifact_type, artifact_version, ref, summary,
			content_json, metadata_json, embedding, embedding_json, embedding_model, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, $8, $9, $10, $11)
		ON CONFLICT(turn_id, artifact_type, artifact_version) DO UPDATE SET
			ref = excluded.ref,
			summary = excluded.summary,
			content_json = excluded.content_json,
			metadata_json = excluded.metadata_json,
			embedding = excluded.embedding,
			embedding_json = excluded.embedding_json,
			embedding_model = excluded.embedding_model,
			updated_at = excluded.updated_at
	`,
		artifact.TurnID,
		artifact.ArtifactType,
		artifact.ArtifactVersion,
		artifact.Ref,
		artifact.Summary,
		string(artifact.ContentJSON),
		string(artifact.MetadataJSON),
		embeddingJSON,
		artifact.EmbeddingModel,
		sqlutil.FormatTimestamp(artifact.CreatedAt),
		sqlutil.FormatTimestamp(artifact.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert artifact: %w", err)
	}
	return nil
}

func (s *Store) insertArtifactWithVector(ctx context.Context, artifact Artifact) error {
	if s.vectorDimensions > 0 && len(artifact.Embedding) > 0 && len(artifact.Embedding) != s.vectorDimensions {
		return fmt.Errorf(
			"%w: artifact=%d configured=%d",
			ErrInvalidEmbeddingDimensions,
			len(artifact.Embedding),
			s.vectorDimensions,
		)
	}

	embeddingJSON, err := sqlutil.FormatJSON(artifact.Embedding)
	if err != nil {
		return fmt.Errorf("format embedding json: %w", err)
	}
	vectorStr := float32sToVectorString(artifact.Embedding)
	query := fmt.Sprintf(`
		INSERT INTO v2_turn_artifacts (
			turn_id, artifact_type, artifact_version, ref, summary,
			content_json, metadata_json, embedding, embedding_json, embedding_model, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, vector('%s'), $8, $9, $10, $11)
		ON CONFLICT(turn_id, artifact_type, artifact_version) DO UPDATE SET
			ref = excluded.ref,
			summary = excluded.summary,
			content_json = excluded.content_json,
			metadata_json = excluded.metadata_json,
			embedding = excluded.embedding,
			embedding_json = excluded.embedding_json,
			embedding_model = excluded.embedding_model,
			updated_at = excluded.updated_at
	`, vectorStr)

	_, err = s.db.ExecContext(ctx, query,
		artifact.TurnID,
		artifact.ArtifactType,
		artifact.ArtifactVersion,
		artifact.Ref,
		artifact.Summary,
		string(artifact.ContentJSON),
		string(artifact.MetadataJSON),
		embeddingJSON,
		artifact.EmbeddingModel,
		sqlutil.FormatTimestamp(artifact.CreatedAt),
		sqlutil.FormatTimestamp(artifact.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert artifact with vector: %w", err)
	}
	return nil
}

// BuildArtifactRef renders the canonical artifact reference form.
func BuildArtifactRef(turnID, artifactType, artifactVersion string) string {
	turnID = strings.TrimSpace(turnID)
	artifactType = strings.TrimSpace(strings.ToLower(artifactType))
	artifactVersion = strings.TrimSpace(artifactVersion)
	return fmt.Sprintf("turn/%s/artifact/%s/%s", turnID, artifactType, artifactVersion)
}

// ParseArtifactRef parses canonical artifact references.
func ParseArtifactRef(ref string) (turnID, artifactType, artifactVersion string, err error) {
	ref = strings.TrimSpace(ref)
	m := artifactRefPattern.FindStringSubmatch(ref)
	if len(m) != 4 {
		return "", "", "", ErrInvalidArtifactRef
	}
	turnID = strings.TrimSpace(m[1])
	artifactType = strings.TrimSpace(strings.ToLower(m[2]))
	artifactVersion = strings.TrimSpace(m[3])
	if turnID == "" || artifactType == "" || artifactVersion == "" {
		return "", "", "", ErrInvalidArtifactRef
	}
	return turnID, artifactType, artifactVersion, nil
}

func normalizeJSON(raw json.RawMessage, fallback string) string {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return fallback
	}
	if !json.Valid([]byte(value)) {
		return fallback
	}
	return value
}

func float32sToVectorString(values []float32) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%f", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func vectorTopKExpression(indexName string, vectorString string, k int) string {
	return fmt.Sprintf("vector_top_k('%s', '%s', %d)", indexName, vectorString, k)
}

func vectorCandidateLimit(opts run.ArtifactSearchOptions) int {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	// Use a wider candidate pool when post-filtering is likely (session/type filters
	// or similarity threshold) to reduce false negatives from preselection.
	if opts.MinSimilarity > 0 || opts.SessionID != "" || len(opts.ArtifactTypes) > 0 {
		limit *= artifactVectorCandidateFactor
	}
	if limit > artifactVectorCandidateCap {
		limit = artifactVectorCandidateCap
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

func resolveVectorDimensions(cfg dbdriver.Config) int {
	dims := dbdriver.GetDefaultVectorDimensions()
	switch cfg.Driver {
	case dbdriver.DriverLibSQL:
		if cfg.LibSQL.VectorDimensions > 0 {
			dims = cfg.LibSQL.VectorDimensions
		}
	case dbdriver.DriverTurso:
		if cfg.Turso.VectorDimensions > 0 {
			dims = cfg.Turso.VectorDimensions
		}
	case dbdriver.DriverPostgres:
		if cfg.Postgres.VectorDimensions > 0 {
			dims = cfg.Postgres.VectorDimensions
		}
	}
	return dims
}

func isVectorUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such function") ||
		strings.Contains(msg, "unknown function") ||
		strings.Contains(msg, "vector(") ||
		strings.Contains(msg, "libsql_vector_idx") ||
		strings.Contains(msg, "vector_top_k") ||
		strings.Contains(msg, artifactVectorIndexName)
}

var (
	_ run.TurnRecorder              = (*Store)(nil)
	_ run.TurnReader                = (*Store)(nil)
	_ run.TurnTimelineReader        = (*Store)(nil)
	_ run.ArtifactSemanticRetriever = (*Store)(nil)
)
