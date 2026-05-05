package turns

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/joshka0/foxctl/internal/storage/vector"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

const (
	ArtifactTypeEmbedding      = "embedding"
	ArtifactTypeAnnotation     = "annotation"
	ArtifactTypeClassification = "classification"
	ArtifactTypeLearning       = "learning"
	ArtifactTypeNarrative      = "narrative"

	// defaultV2TurnsVectorDims is the local-testing default for v2 turns
	// embeddings. Override with FOXCTL_V2_TURNS_VECTOR_DIMS (preferred) or
	// FOXCTL_VECTOR_DIMS for provider-specific dimensions (e.g. Voyage).
	defaultV2TurnsVectorDims = 768

	defaultArtifactVersion        = "v1"
	artifactVectorIndexName       = "idx_v2_turn_artifacts_embedding_vec"
	artifactVectorCandidateFactor = 12
	artifactVectorCandidateCap    = 500
)

var (
	// ErrArtifactNotFound indicates a requested artifact row is missing.
	ErrArtifactNotFound = errors.New("v2 turns: artifact not found")
	// ErrEpisodeNotFound indicates a requested episode row is missing.
	ErrEpisodeNotFound = errors.New("v2 turns: episode not found")
	// ErrInvalidArtifactType indicates an unsupported artifact type.
	ErrInvalidArtifactType = errors.New("v2 turns: invalid artifact type")
	// ErrInvalidArtifactRef indicates malformed artifact references.
	ErrInvalidArtifactRef = errors.New("v2 turns: invalid artifact ref")
	// ErrInvalidEmbeddingDimensions indicates embeddings do not match
	// configured vector dimensions when vector mode is active.
	ErrInvalidEmbeddingDimensions = errors.New("v2 turns: invalid embedding dimensions")
	// ErrInvalidNarrativeClaims indicates narrative artifacts without evidence.
	ErrInvalidNarrativeClaims = errors.New("v2 turns: invalid narrative claims")

	artifactRefPattern = regexp.MustCompile(`^turn/([^/#]+)/artifact/([^/#]+)/([^/#]+)$`)
	vectorDimsPattern  = regexp.MustCompile(`(?i)f32_blob\s*\(\s*(\d+)\s*\)`)

	allowedArtifactTypes = map[string]struct{}{
		ArtifactTypeEmbedding:      {},
		ArtifactTypeAnnotation:     {},
		ArtifactTypeClassification: {},
		ArtifactTypeLearning:       {},
		ArtifactTypeNarrative:      {},
	}
)

type narrativeContent struct {
	Summary         string               `json:"summary,omitempty"`
	Claims          []run.NarrativeClaim `json:"claims,omitempty"`
	AnchorRefs      []string             `json:"anchor_refs,omitempty"`
	SourceTurnID    string               `json:"source_turn_id,omitempty"`
	SourceTurnIndex int                  `json:"source_turn_index,omitempty"`
	SourceTurnCount int                  `json:"source_turn_count,omitempty"`
}

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
		vectorDimensions: defaultV2TurnsVectorDimensions(),
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
		return defaultV2TurnsVectorDimensions()
	}
	return s.vectorDimensions
}

// Open opens a Turso-first v2 turns store.
func Open(ctx context.Context, storageRoot string) (*Store, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, fmt.Errorf("v2 turns open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultTursoLocalConfig(filepath.Join(storageRoot, "v2_turns.turso"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_TURNS", "v2_turns.db")
	}
	if !hasVectorDimsOverride() {
		v2DefaultDims := defaultV2TurnsVectorDimensions()
		switch cfg.Driver {
		case dbdriver.DriverTurso:
			cfg.Turso.VectorDimensions = v2DefaultDims
		case dbdriver.DriverPostgres:
			cfg.Postgres.VectorDimensions = v2DefaultDims
		}
	}

	driverType := cfg.Driver
	vectorDims := resolveVectorDimensions(cfg)
	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil {
		return nil, fmt.Errorf("v2 turns open: %w", err)
	}

	store := NewStore(db, closeFn)
	store.vectorEnabled.Store(driverType == dbdriver.DriverTurso)
	store.vectorDimensions = detectArtifactVectorDimensions(ctx, db, vectorDims)
	return store, nil
}

func hasDriverOverride() bool {
	return os.Getenv("FOXCTL_V2_TURNS_DB_DRIVER") != "" || os.Getenv("FOXCTL_DB_DRIVER") != ""
}

func hasVectorDimsOverride() bool {
	return strings.TrimSpace(os.Getenv("FOXCTL_V2_TURNS_VECTOR_DIMS")) != "" ||
		strings.TrimSpace(os.Getenv("FOXCTL_VECTOR_DIMS")) != ""
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

// SaveEpisode upserts one semantic episode record using boundary-key idempotency.
func (s *Store) SaveEpisode(ctx context.Context, episode run.EpisodeRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 turns save episode: nil store")
	}

	normalized, err := s.normalizeEpisode(episode)
	if err != nil {
		return err
	}

	anchorRefsJSON, err := sqlutil.FormatJSON(normalized.AnchorRefs)
	if err != nil {
		return fmt.Errorf("format episode anchor refs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO v2_episodes (
			id, session_id, episode_version, boundary_key,
			start_turn_id, end_turn_id, start_turn_index, end_turn_index,
			topic, summary, salience_score, is_landmark, anchor_refs_json,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15
		)
		ON CONFLICT(session_id, episode_version, boundary_key) DO UPDATE SET
			start_turn_id = excluded.start_turn_id,
			end_turn_id = excluded.end_turn_id,
			start_turn_index = excluded.start_turn_index,
			end_turn_index = excluded.end_turn_index,
			topic = excluded.topic,
			summary = excluded.summary,
			salience_score = excluded.salience_score,
			is_landmark = excluded.is_landmark,
			anchor_refs_json = excluded.anchor_refs_json,
			updated_at = excluded.updated_at
	`,
		normalized.ID,
		normalized.SessionID,
		normalized.EpisodeVersion,
		normalized.BoundaryKey,
		normalized.StartTurnID,
		normalized.EndTurnID,
		normalized.StartTurnIndex,
		normalized.EndTurnIndex,
		normalized.Topic,
		normalized.Summary,
		normalized.SalienceScore,
		boolToInt(normalized.IsLandmark),
		anchorRefsJSON,
		sqlutil.FormatTimestamp(normalized.CreatedAt),
		sqlutil.FormatTimestamp(normalized.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert episode: %w", err)
	}
	return nil
}

// GetEpisode loads one semantic episode by ID.
func (s *Store) GetEpisode(ctx context.Context, episodeID string) (run.EpisodeRecord, error) {
	if s == nil || s.db == nil {
		return run.EpisodeRecord{}, fmt.Errorf("v2 turns get episode: nil store")
	}

	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return run.EpisodeRecord{}, run.ErrEpisodeNotFound
	}

	var (
		out            run.EpisodeRecord
		isLandmarkInt  int
		anchorRefsJSON string
		createdAtRaw   string
		updatedAtRaw   string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			COALESCE(session_id, ''),
			COALESCE(episode_version, ''),
			COALESCE(boundary_key, ''),
			COALESCE(start_turn_id, ''),
			COALESCE(end_turn_id, ''),
			COALESCE(start_turn_index, 0),
			COALESCE(end_turn_index, 0),
			COALESCE(topic, ''),
			COALESCE(summary, ''),
			COALESCE(salience_score, 0),
			COALESCE(is_landmark, 0),
			COALESCE(anchor_refs_json, '[]'),
			created_at,
			updated_at
		FROM v2_episodes
		WHERE id = $1
	`, episodeID).Scan(
		&out.ID,
		&out.SessionID,
		&out.EpisodeVersion,
		&out.BoundaryKey,
		&out.StartTurnID,
		&out.EndTurnID,
		&out.StartTurnIndex,
		&out.EndTurnIndex,
		&out.Topic,
		&out.Summary,
		&out.SalienceScore,
		&isLandmarkInt,
		&anchorRefsJSON,
		&createdAtRaw,
		&updatedAtRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return run.EpisodeRecord{}, run.ErrEpisodeNotFound
	}
	if err != nil {
		return run.EpisodeRecord{}, fmt.Errorf("query episode: %w", err)
	}

	out.IsLandmark = isLandmarkInt > 0
	out.SalienceScore = clampUnitFloat(out.SalienceScore)
	if strings.TrimSpace(anchorRefsJSON) != "" {
		if unmarshalErr := json.Unmarshal([]byte(anchorRefsJSON), &out.AnchorRefs); unmarshalErr != nil {
			out.AnchorRefs = nil
		}
	}
	out.AnchorRefs = normalizeAnchorRefs(out.AnchorRefs)

	if out.CreatedAt, err = sqlutil.ScanTimestamp(createdAtRaw); err != nil {
		return run.EpisodeRecord{}, fmt.Errorf("parse episode.created_at: %w", err)
	}
	if out.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAtRaw); err != nil {
		return run.EpisodeRecord{}, fmt.Errorf("parse episode.updated_at: %w", err)
	}

	return out.Clone(), nil
}

// ListEpisodes returns semantic episodes for one session ordered by creation time.
func (s *Store) ListEpisodes(ctx context.Context, sessionID string, opts run.EpisodeListOptions) ([]run.EpisodeRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 turns list episodes: nil store")
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

	since := ""
	until := ""
	if !opts.Since.IsZero() {
		since = sqlutil.FormatTimestamp(opts.Since.UTC())
	}
	if !opts.Until.IsZero() {
		until = sqlutil.FormatTimestamp(opts.Until.UTC())
	}

	query := fmt.Sprintf(`
		SELECT id
		FROM v2_episodes
		WHERE session_id = $1
		  AND ($2 = '' OR created_at >= $2)
		  AND ($3 = '' OR created_at <= $3)
		  AND ($4 = 0 OR is_landmark = 1)
		ORDER BY created_at %s, id %s
		LIMIT $5
	`, order, order)

	rows, err := s.db.QueryContext(ctx, query, sessionID, since, until, boolToInt(opts.LandmarkOnly), limit)
	if err != nil {
		return nil, fmt.Errorf("query episode ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	episodeIDs := make([]string, 0, limit)
	for rows.Next() {
		var episodeID string
		if err := rows.Scan(&episodeID); err != nil {
			return nil, fmt.Errorf("scan episode id: %w", err)
		}
		episodeIDs = append(episodeIDs, strings.TrimSpace(episodeID))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate episode ids: %w", err)
	}

	out := make([]run.EpisodeRecord, 0, len(episodeIDs))
	for _, episodeID := range episodeIDs {
		episode, err := s.GetEpisode(ctx, episodeID)
		if err != nil {
			return nil, err
		}
		out = append(out, episode)
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

// SaveNarrative upserts one session-scoped narrative artifact.
// Idempotency is keyed by (session first turn id, artifact_type=narrative, artifact_version).
func (s *Store) SaveNarrative(ctx context.Context, narrative run.NarrativeRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 turns save narrative: nil store")
	}
	normalized, err := normalizeNarrativeRecord(narrative)
	if err != nil {
		return err
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = s.now().UTC()
	}
	turnID, err := s.resolveNarrativeTurnID(ctx, normalized.SessionID, normalized.TurnID)
	if err != nil {
		return err
	}

	content := narrativeContent{
		Summary:         strings.TrimSpace(normalized.Summary),
		Claims:          cloneNarrativeClaims(normalized.Claims),
		AnchorRefs:      append([]string(nil), normalized.AnchorRefs...),
		SourceTurnID:    strings.TrimSpace(normalized.SourceTurnID),
		SourceTurnIndex: normalized.SourceTurnIndex,
		SourceTurnCount: normalized.SourceTurnCount,
	}
	if content.Summary == "" {
		content.Summary = summaryFromNarrativeClaims(content.Claims)
	}
	if strings.TrimSpace(normalized.Summary) == "" {
		normalized.Summary = content.Summary
	}

	metadata := map[string]any{
		"session_id":      normalized.SessionID,
		"artifact_scope":  "session",
		"artifact_kind":   "narrative",
		"claim_count":     len(content.Claims),
		"anchor_refs":     append([]string(nil), content.AnchorRefs...),
		"source_turn_id":  content.SourceTurnID,
		"source_turn_ix":  content.SourceTurnIndex,
		"source_turn_cnt": content.SourceTurnCount,
	}

	artifact := Artifact{
		TurnID:          turnID,
		ArtifactType:    ArtifactTypeNarrative,
		ArtifactVersion: normalized.ArtifactVersion,
		Summary:         strings.TrimSpace(normalized.Summary),
		ContentJSON:     mustMarshalJSON(content),
		MetadataJSON:    mustMarshalJSON(metadata),
		CreatedAt:       normalized.UpdatedAt,
		UpdatedAt:       normalized.UpdatedAt,
	}
	return s.SaveArtifact(ctx, artifact)
}

// GetNarrative loads the latest narrative artifact for one session/version.
func (s *Store) GetNarrative(ctx context.Context, sessionID, artifactVersion string) (run.NarrativeRecord, error) {
	if s == nil || s.db == nil {
		return run.NarrativeRecord{}, fmt.Errorf("v2 turns get narrative: nil store")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return run.NarrativeRecord{}, run.ErrNarrativeNotFound
	}
	artifactVersion = strings.TrimSpace(artifactVersion)

	var (
		turnID      string
		version     string
		ref         string
		summary     string
		contentJSON []byte
		updatedAt   string
	)
	err := s.db.QueryRowContext(ctx, `
			SELECT
				a.turn_id,
				a.artifact_version,
			a.ref,
			COALESCE(a.summary, ''),
			COALESCE(a.content_json, '{}'),
			COALESCE(a.updated_at, '')
		FROM v2_turn_artifacts a
		JOIN v2_turns t ON t.id = a.turn_id
			WHERE
				t.session_id = $1
				AND a.artifact_type = $2
				AND ($3 = '' OR a.artifact_version = $3)
			ORDER BY
				CASE
					WHEN a.turn_id = (
						SELECT t0.id
						FROM v2_turns t0
						WHERE t0.session_id = $1
						ORDER BY t0.turn_index ASC, t0.created_at ASC, t0.id ASC
						LIMIT 1
					) THEN 0
					ELSE 1
				END ASC,
				a.updated_at DESC,
				a.turn_id ASC
			LIMIT 1
		`, sessionID, ArtifactTypeNarrative, artifactVersion).Scan(
		&turnID,
		&version,
		&ref,
		&summary,
		&contentJSON,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return run.NarrativeRecord{}, run.ErrNarrativeNotFound
	}
	if err != nil {
		return run.NarrativeRecord{}, fmt.Errorf("query narrative artifact: %w", err)
	}

	var content narrativeContent
	if len(contentJSON) > 0 {
		if err := json.Unmarshal(contentJSON, &content); err != nil {
			return run.NarrativeRecord{}, fmt.Errorf("parse narrative content: %w", err)
		}
	}
	content.Summary = strings.TrimSpace(content.Summary)
	if content.Summary == "" {
		content.Summary = strings.TrimSpace(summary)
	}
	content.SourceTurnID = strings.TrimSpace(content.SourceTurnID)
	content.AnchorRefs = normalizeAnchorRefs(content.AnchorRefs)
	if len(content.AnchorRefs) == 0 {
		content.AnchorRefs = anchorsFromClaims(content.Claims)
	}
	if err := validateNarrativeContent(content); err != nil {
		return run.NarrativeRecord{}, err
	}

	out := run.NarrativeRecord{
		SessionID:       sessionID,
		TurnID:          strings.TrimSpace(turnID),
		Ref:             strings.TrimSpace(ref),
		ArtifactVersion: strings.TrimSpace(version),
		Summary:         content.Summary,
		Claims:          cloneNarrativeClaims(content.Claims),
		AnchorRefs:      append([]string(nil), content.AnchorRefs...),
		SourceTurnID:    content.SourceTurnID,
		SourceTurnIndex: content.SourceTurnIndex,
		SourceTurnCount: content.SourceTurnCount,
	}
	if ts, parseErr := sqlutil.ScanTimestamp(updatedAt); parseErr == nil {
		out.UpdatedAt = ts
	}
	return out.Clone(), nil
}

// SearchArtifactsByEmbedding returns top artifacts by embedding similarity.
// It prefers native Turso vector functions and falls back to in-process cosine
// scoring when vector SQL is unavailable.
func (s *Store) SearchArtifactsByEmbedding(
	ctx context.Context,
	queryEmbedding []float32,
	opts run.ArtifactSearchOptions,
) (run.ArtifactSearchResult, error) {
	if s == nil || s.db == nil {
		return run.ArtifactSearchResult{}, fmt.Errorf("v2 turns search artifacts: nil store")
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
	workingAttempts := workingContextAttempts(normalized.Working)
	if len(workingAttempts) == 0 {
		return s.searchArtifactsByEmbeddingOnce(ctx, queryEmbedding, normalized, false, 0)
	}

	last := run.ArtifactSearchResult{
		SearchPath:       run.ArtifactSearchPathDisabled,
		VectorCapability: s.vectorCapability(),
		WorkingApplied:   true,
		FallbackLevel:    3,
	}
	for level, attempt := range workingAttempts {
		attemptOpts := normalized
		attemptOpts.Working = attempt
		result, err := s.searchArtifactsByEmbeddingOnce(ctx, queryEmbedding, attemptOpts, true, level)
		if err != nil {
			return run.ArtifactSearchResult{}, err
		}
		last = result
		if len(result.Hits) > 0 {
			return result, nil
		}
	}

	// Level 3: temporal-only fallback. Semantic layer intentionally returns no
	// hits so callers can fall through to temporal refs.
	last.Hits = nil
	last.WorkingApplied = true
	last.FallbackLevel = 3
	return last, nil
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

func (s *Store) searchArtifactsByEmbeddingOnce(
	ctx context.Context,
	queryEmbedding []float32,
	opts run.ArtifactSearchOptions,
	workingApplied bool,
	fallbackLevel int,
) (run.ArtifactSearchResult, error) {
	eligibleCount := 0
	if workingApplied {
		count, err := s.countEligibleArtifacts(ctx, opts)
		if err != nil {
			return run.ArtifactSearchResult{}, err
		}
		eligibleCount = count
	}
	if len(queryEmbedding) == 0 {
		return run.ArtifactSearchResult{
			SearchPath:       run.ArtifactSearchPathDisabled,
			VectorCapability: s.vectorCapability(),
			WorkingApplied:   workingApplied,
			FallbackLevel:    fallbackLevel,
			EligibleCount:    eligibleCount,
		}, nil
	}

	if s.vectorEnabled.Load() {
		candidates, err := s.searchArtifactCandidatesVector(ctx, queryEmbedding, opts)
		if err == nil {
			hits, loadErr := s.loadScoredArtifacts(ctx, candidates)
			if loadErr != nil {
				return run.ArtifactSearchResult{}, loadErr
			}
			if workingApplied {
				hits = rerankWorkingContextHits(hits, opts.Working)
			}
			return run.ArtifactSearchResult{
				Hits:             hits,
				SearchPath:       run.ArtifactSearchPathVector,
				VectorCapability: run.ArtifactVectorCapabilityEnabled,
				WorkingApplied:   workingApplied,
				FallbackLevel:    fallbackLevel,
				EligibleCount:    eligibleCount,
			}, nil
		}
		if !isVectorUnsupported(err) {
			return run.ArtifactSearchResult{}, err
		}
		// Disable vector search after first unsupported query to avoid repeated
		// expensive failures on fallback drivers.
		s.vectorEnabled.Store(false)
	}

	candidates, err := s.searchArtifactCandidatesFallback(ctx, queryEmbedding, opts)
	if err != nil {
		return run.ArtifactSearchResult{}, err
	}
	hits, err := s.loadScoredArtifacts(ctx, candidates)
	if err != nil {
		return run.ArtifactSearchResult{}, err
	}
	if workingApplied {
		hits = rerankWorkingContextHits(hits, opts.Working)
	}
	return run.ArtifactSearchResult{
		Hits:             hits,
		SearchPath:       run.ArtifactSearchPathFallback,
		VectorCapability: s.vectorCapability(),
		WorkingApplied:   workingApplied,
		FallbackLevel:    fallbackLevel,
		EligibleCount:    eligibleCount,
	}, nil
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
	opts.Working = normalizeWorkingContext(opts.Working)
	if opts.SessionID == "" && opts.Working.SessionID != "" {
		opts.SessionID = opts.Working.SessionID
	}
	if opts.Working.SessionID == "" {
		opts.Working.SessionID = opts.SessionID
	}
	if opts.SessionID != "" && opts.Working.SessionID != "" && opts.SessionID != opts.Working.SessionID {
		opts.Working.SessionID = opts.SessionID
	}
	return opts, nil
}

func normalizeWorkingContext(ctx run.WorkingContext) run.WorkingContext {
	ctx.SessionID = strings.TrimSpace(ctx.SessionID)
	ctx.WorkspaceID = strings.TrimSpace(ctx.WorkspaceID)
	ctx.ActiveFiles = uniqueSortedStrings(ctx.ActiveFiles, false)
	ctx.RequiredLabels = uniqueSortedStrings(ctx.RequiredLabels, true)
	if ctx.MinSalience < 0 {
		ctx.MinSalience = 0
	}
	if ctx.MinSalience > 1 {
		ctx.MinSalience = 1
	}
	return ctx
}

func workingContextAttempts(ctx run.WorkingContext) []run.WorkingContext {
	if !workingContextEnabled(ctx) {
		return nil
	}
	strict := normalizeWorkingContext(ctx)
	level1 := strict
	level1.RequiredLabels = nil
	level2 := level1
	level2.MinSalience = 0
	return []run.WorkingContext{strict, level1, level2}
}

func workingContextEnabled(ctx run.WorkingContext) bool {
	ctx = normalizeWorkingContext(ctx)
	return ctx.SessionID != "" ||
		ctx.WorkspaceID != "" ||
		len(ctx.ActiveFiles) > 0 ||
		len(ctx.RequiredLabels) > 0 ||
		ctx.MinSalience > 0
}

func (s *Store) searchArtifactCandidatesVector(
	ctx context.Context,
	queryEmbedding []float32,
	opts run.ArtifactSearchOptions,
) ([]artifactSimilarityCandidate, error) {
	vectorStr := float32sToVectorString(queryEmbedding)
	vectorTopKExpr := vectorTopKExpression(artifactVectorIndexName, vectorStr, vectorCandidateLimit(opts))

	where, args, err := buildArtifactSearchWhere(opts, true)
	if err != nil {
		return nil, err
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
	where, args, err := buildArtifactSearchWhere(opts, false)
	if err != nil {
		return nil, err
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

func (s *Store) countEligibleArtifacts(ctx context.Context, opts run.ArtifactSearchOptions) (int, error) {
	where, args, err := buildArtifactSearchWhere(opts, false)
	if err != nil {
		return 0, err
	}
	// Eligible means "has embedding payload available for semantic retrieval".
	where = append(where, "(a.embedding IS NOT NULL OR COALESCE(a.embedding_json, '[]') <> '[]')")

	query := fmt.Sprintf(`
		SELECT COUNT(1)
		FROM v2_turn_artifacts a
		JOIN v2_turns t ON t.id = a.turn_id
		WHERE %s
	`, strings.Join(where, " AND "))

	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count eligible artifacts: %w", err)
	}
	return count, nil
}

func buildArtifactSearchWhere(opts run.ArtifactSearchOptions, vectorOnly bool) ([]string, []any, error) {
	where := make([]string, 0, 16)
	if vectorOnly {
		where = append(where, "a.embedding IS NOT NULL")
	} else {
		where = append(where, "COALESCE(a.embedding_json, '[]') <> '[]'")
	}

	args := make([]any, 0, 24)
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
	where, args = appendWorkingContextFilters(where, args, opts.Working)
	return where, args, nil
}

func appendWorkingContextFilters(where []string, args []any, working run.WorkingContext) ([]string, []any) {
	if !workingContextEnabled(working) {
		return where, args
	}
	working = normalizeWorkingContext(working)
	if working.WorkspaceID != "" {
		args = append(args, working.WorkspaceID, working.WorkspaceID)
		where = append(where, fmt.Sprintf(
			"(COALESCE(json_extract(a.metadata_json, '$.workspace_id'), '') = $%d OR COALESCE(json_extract(a.metadata_json, '$.workspace'), '') = $%d)",
			len(args)-1,
			len(args),
		))
	}
	if len(working.ActiveFiles) > 0 {
		filePredicates := make([]string, 0, len(working.ActiveFiles))
		for _, file := range working.ActiveFiles {
			args = append(args, file)
			filePredicates = append(filePredicates, fmt.Sprintf("$%d", len(args)))
		}
		where = append(where, fmt.Sprintf(`
			EXISTS (
				SELECT 1
				FROM json_each(a.metadata_json, '$.active_files') af
				WHERE af.value IN (%s)
			)
		`, strings.Join(filePredicates, ", ")))
	}
	for _, label := range working.RequiredLabels {
		args = append(args, label, label)
		where = append(where, fmt.Sprintf(`
			(
				EXISTS (
					SELECT 1 FROM json_each(a.metadata_json, '$.labels') ml
					WHERE LOWER(CAST(ml.value AS TEXT)) = $%d
				)
				OR EXISTS (
					SELECT 1 FROM json_each(a.content_json, '$.labels') cl
					WHERE LOWER(CAST(cl.value AS TEXT)) = $%d
				)
			)
		`, len(args)-1, len(args)))
	}
	if working.MinSalience > 0 {
		args = append(args, working.MinSalience)
		where = append(where, fmt.Sprintf(`
			COALESCE(
				CAST(json_extract(a.metadata_json, '$.salience') AS REAL),
				CAST(json_extract(a.metadata_json, '$.salience_score') AS REAL),
				CAST(json_extract(a.content_json, '$.salience') AS REAL),
				CAST(json_extract(a.content_json, '$.salience_score') AS REAL),
				0
			) >= $%d
		`, len(args)))
	}
	return where, args
}

func rerankWorkingContextHits(hits []run.ScoredArtifact, working run.WorkingContext) []run.ScoredArtifact {
	if len(hits) == 0 {
		return nil
	}
	working = normalizeWorkingContext(working)
	required := make(map[string]struct{}, len(working.RequiredLabels))
	for _, label := range working.RequiredLabels {
		required[label] = struct{}{}
	}

	type scored struct {
		hit          run.ScoredArtifact
		composite    float64
		salience     float64
		labelOverlap float64
	}

	out := make([]scored, 0, len(hits))
	for _, hit := range hits {
		meta := decodeArtifactMetadata(hit.MetadataJSON)
		labels := metadataLabels(meta)
		salience := metadataSalience(meta)
		overlap := labelOverlapRatio(required, labels)
		composite := (hit.Similarity * 0.70) + (salience * 0.20) + (overlap * 0.10)
		out = append(out, scored{
			hit:          hit.Clone(),
			composite:    composite,
			salience:     salience,
			labelOverlap: overlap,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.composite != right.composite {
			return left.composite > right.composite
		}
		if left.hit.Similarity != right.hit.Similarity {
			return left.hit.Similarity > right.hit.Similarity
		}
		if left.salience != right.salience {
			return left.salience > right.salience
		}
		if left.labelOverlap != right.labelOverlap {
			return left.labelOverlap > right.labelOverlap
		}
		if left.hit.TurnID != right.hit.TurnID {
			return left.hit.TurnID < right.hit.TurnID
		}
		if left.hit.ArtifactType != right.hit.ArtifactType {
			return left.hit.ArtifactType < right.hit.ArtifactType
		}
		return left.hit.ArtifactVersion < right.hit.ArtifactVersion
	})

	reranked := make([]run.ScoredArtifact, 0, len(out))
	for _, candidate := range out {
		reranked = append(reranked, candidate.hit.Clone())
	}
	return reranked
}

func decodeArtifactMetadata(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}

func metadataLabels(meta map[string]any) []string {
	if len(meta) == 0 {
		return nil
	}
	raw, ok := meta["labels"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if str, ok := value.(string); ok {
				out = append(out, str)
			}
		}
		return uniqueSortedStrings(out, true)
	case []string:
		return uniqueSortedStrings(typed, true)
	default:
		return nil
	}
}

func metadataSalience(meta map[string]any) float64 {
	if len(meta) == 0 {
		return 0
	}
	for _, key := range []string{"salience", "salience_score"} {
		raw, ok := meta[key]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case float64:
			return clampUnitFloat(typed)
		case float32:
			return clampUnitFloat(float64(typed))
		case int:
			return clampUnitFloat(float64(typed))
		case int64:
			return clampUnitFloat(float64(typed))
		case json.Number:
			f, err := typed.Float64()
			if err == nil {
				return clampUnitFloat(f)
			}
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
				return clampUnitFloat(parsed)
			}
		}
	}
	return 0
}

func labelOverlapRatio(required map[string]struct{}, labels []string) float64 {
	if len(required) == 0 || len(labels) == 0 {
		return 0
	}
	unique := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		normalized := strings.ToLower(strings.TrimSpace(label))
		if normalized == "" {
			continue
		}
		unique[normalized] = struct{}{}
	}
	if len(unique) == 0 {
		return 0
	}
	overlap := 0
	for label := range required {
		if _, ok := unique[label]; ok {
			overlap++
		}
	}
	return float64(overlap) / float64(len(required))
}

func uniqueSortedStrings(items []string, lowercase bool) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if lowercase {
			item = strings.ToLower(item)
		}
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
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

func (s *Store) normalizeEpisode(episode run.EpisodeRecord) (run.EpisodeRecord, error) {
	episode = episode.Clone()
	episode.ID = strings.TrimSpace(episode.ID)
	episode.SessionID = strings.TrimSpace(episode.SessionID)
	episode.EpisodeVersion = strings.TrimSpace(episode.EpisodeVersion)
	episode.BoundaryKey = strings.TrimSpace(episode.BoundaryKey)
	episode.StartTurnID = strings.TrimSpace(episode.StartTurnID)
	episode.EndTurnID = strings.TrimSpace(episode.EndTurnID)
	episode.Topic = strings.TrimSpace(episode.Topic)
	episode.Summary = strings.TrimSpace(episode.Summary)
	episode.AnchorRefs = normalizeAnchorRefs(episode.AnchorRefs)
	episode.SalienceScore = clampUnitFloat(episode.SalienceScore)

	if episode.SessionID == "" {
		return run.EpisodeRecord{}, fmt.Errorf("v2 turns save episode: session_id is required")
	}
	if episode.EpisodeVersion == "" {
		episode.EpisodeVersion = defaultArtifactVersion
	}
	if episode.BoundaryKey == "" {
		return run.EpisodeRecord{}, fmt.Errorf("v2 turns save episode: boundary_key is required")
	}
	if episode.StartTurnID == "" || episode.EndTurnID == "" {
		return run.EpisodeRecord{}, fmt.Errorf("v2 turns save episode: start_turn_id and end_turn_id are required")
	}
	if episode.StartTurnIndex > 0 && episode.EndTurnIndex > 0 && episode.EndTurnIndex < episode.StartTurnIndex {
		return run.EpisodeRecord{}, fmt.Errorf("v2 turns save episode: end_turn_index must be >= start_turn_index")
	}
	if episode.ID == "" {
		episode.ID = buildEpisodeID(episode.SessionID, episode.EpisodeVersion, episode.BoundaryKey)
	}

	now := s.now().UTC()
	if episode.CreatedAt.IsZero() {
		episode.CreatedAt = now
	}
	if episode.UpdatedAt.IsZero() {
		episode.UpdatedAt = now
	}
	if episode.UpdatedAt.Before(episode.CreatedAt) {
		episode.UpdatedAt = episode.CreatedAt
	}

	return episode, nil
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
	if artifact.ArtifactType == ArtifactTypeNarrative {
		if err := validateNarrativeArtifact(artifact.ContentJSON); err != nil {
			return Artifact{}, err
		}
	}
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

func normalizeNarrativeRecord(narrative run.NarrativeRecord) (run.NarrativeRecord, error) {
	narrative = narrative.Clone()
	narrative.SessionID = strings.TrimSpace(narrative.SessionID)
	narrative.TurnID = strings.TrimSpace(narrative.TurnID)
	narrative.SourceTurnID = strings.TrimSpace(narrative.SourceTurnID)
	narrative.ArtifactVersion = strings.TrimSpace(narrative.ArtifactVersion)
	narrative.Summary = strings.TrimSpace(narrative.Summary)
	narrative.Ref = strings.TrimSpace(narrative.Ref)
	if narrative.SourceTurnIndex < 0 {
		narrative.SourceTurnIndex = 0
	}
	if narrative.SourceTurnCount < 0 {
		narrative.SourceTurnCount = 0
	}
	narrative.AnchorRefs = normalizeAnchorRefs(narrative.AnchorRefs)
	for i := range narrative.Claims {
		narrative.Claims[i].Text = strings.TrimSpace(narrative.Claims[i].Text)
		narrative.Claims[i].AnchorRefs = normalizeAnchorRefs(narrative.Claims[i].AnchorRefs)
	}
	if narrative.SessionID == "" {
		return run.NarrativeRecord{}, fmt.Errorf("v2 turns save narrative: session_id is required")
	}
	if narrative.ArtifactVersion == "" {
		narrative.ArtifactVersion = defaultArtifactVersion
	}
	if len(narrative.AnchorRefs) == 0 {
		narrative.AnchorRefs = anchorsFromClaims(narrative.Claims)
	}
	content := narrativeContent{
		Summary:         narrative.Summary,
		Claims:          cloneNarrativeClaims(narrative.Claims),
		AnchorRefs:      append([]string(nil), narrative.AnchorRefs...),
		SourceTurnID:    narrative.SourceTurnID,
		SourceTurnIndex: narrative.SourceTurnIndex,
		SourceTurnCount: narrative.SourceTurnCount,
	}
	if err := validateNarrativeContent(content); err != nil {
		return run.NarrativeRecord{}, err
	}
	if narrative.Summary == "" {
		narrative.Summary = summaryFromNarrativeClaims(narrative.Claims)
	}
	if !narrative.UpdatedAt.IsZero() {
		narrative.UpdatedAt = narrative.UpdatedAt.UTC()
	}
	return narrative, nil
}

func (s *Store) resolveNarrativeTurnID(ctx context.Context, sessionID, candidateTurnID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	candidateTurnID = strings.TrimSpace(candidateTurnID)
	if candidateTurnID != "" {
		turn, err := s.GetTurn(ctx, candidateTurnID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(turn.SessionID) != sessionID {
			return "", fmt.Errorf("v2 turns save narrative: turn %s not in session %s", candidateTurnID, sessionID)
		}
	}

	turns, err := s.ListTurns(ctx, sessionID, run.TurnListOptions{
		Limit: 1,
		Asc:   true,
	})
	if err != nil {
		return "", err
	}
	if len(turns) == 0 {
		return "", fmt.Errorf("v2 turns save narrative: no turns found for session %s", sessionID)
	}
	return strings.TrimSpace(turns[0].ID), nil
}

func validateNarrativeArtifact(contentJSON json.RawMessage) error {
	var content narrativeContent
	if len(contentJSON) > 0 {
		if err := json.Unmarshal(contentJSON, &content); err != nil {
			return fmt.Errorf("%w: parse content: %v", ErrInvalidNarrativeClaims, err)
		}
	}
	return validateNarrativeContent(content)
}

func validateNarrativeContent(content narrativeContent) error {
	content.Claims = cloneNarrativeClaims(content.Claims)
	content.AnchorRefs = normalizeAnchorRefs(content.AnchorRefs)
	if len(content.Claims) == 0 {
		return fmt.Errorf("%w: narrative claims are required", ErrInvalidNarrativeClaims)
	}
	for i, claim := range content.Claims {
		if strings.TrimSpace(claim.Text) == "" {
			return fmt.Errorf("%w: claim[%d] text is required", ErrInvalidNarrativeClaims, i)
		}
		claimRefs := normalizeAnchorRefs(claim.AnchorRefs)
		if len(claimRefs) == 0 {
			return fmt.Errorf("%w: claim[%d] missing anchor refs", ErrInvalidNarrativeClaims, i)
		}
		for _, ref := range claimRefs {
			if !strings.HasPrefix(ref, "turn/") {
				return fmt.Errorf("%w: claim[%d] invalid anchor ref %q", ErrInvalidNarrativeClaims, i, ref)
			}
		}
	}
	if len(content.AnchorRefs) > 0 {
		for _, ref := range content.AnchorRefs {
			if !strings.HasPrefix(ref, "turn/") {
				return fmt.Errorf("%w: invalid anchor ref %q", ErrInvalidNarrativeClaims, ref)
			}
		}
	}
	return nil
}

func anchorsFromClaims(claims []run.NarrativeClaim) []string {
	refs := make([]string, 0, len(claims)*2)
	for _, claim := range claims {
		refs = append(refs, claim.AnchorRefs...)
	}
	return normalizeAnchorRefs(refs)
}

func cloneNarrativeClaims(claims []run.NarrativeClaim) []run.NarrativeClaim {
	if len(claims) == 0 {
		return nil
	}
	out := make([]run.NarrativeClaim, len(claims))
	for i := range claims {
		out[i] = claims[i].Clone()
	}
	return out
}

func summaryFromNarrativeClaims(claims []run.NarrativeClaim) string {
	if len(claims) == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	for _, claim := range claims {
		text := strings.TrimSpace(claim.Text)
		if text == "" {
			continue
		}
		parts = append(parts, truncate(text, 120))
		if len(parts) >= 2 {
			break
		}
	}
	return strings.Join(parts, " ")
}

func truncate(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
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

func mustMarshalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(encoded)
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

func normalizeAnchorRefs(refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	out := make([]string, 0, len(refs))
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func clampUnitFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func buildEpisodeID(sessionID, episodeVersion, boundaryKey string) string {
	normalizedSession := strings.TrimSpace(sessionID)
	normalizedVersion := strings.TrimSpace(episodeVersion)
	raw := normalizedSession + "|" + normalizedVersion + "|" + strings.TrimSpace(boundaryKey)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("episode|%s|%s|%s", normalizedSession, normalizedVersion, hex.EncodeToString(sum[:]))
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
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
	dims := defaultV2TurnsVectorDimensions()
	switch cfg.Driver {
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

func defaultV2TurnsVectorDimensions() int {
	if dims, ok := envPositiveInt("FOXCTL_V2_TURNS_VECTOR_DIMS"); ok {
		return dims
	}
	if dims, ok := envPositiveInt("FOXCTL_VECTOR_DIMS"); ok {
		return dims
	}
	return defaultV2TurnsVectorDims
}

func envPositiveInt(name string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func detectArtifactVectorDimensions(ctx context.Context, db *sql.DB, fallback int) int {
	if db == nil {
		return fallback
	}

	var tableSQL string
	err := db.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='v2_turn_artifacts'`,
	).Scan(&tableSQL)
	if err != nil {
		return fallback
	}

	match := vectorDimsPattern.FindStringSubmatch(tableSQL)
	if len(match) != 2 {
		return fallback
	}
	dims, err := strconv.Atoi(match[1])
	if err != nil || dims <= 0 {
		return fallback
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
	_ run.EpisodeWriter             = (*Store)(nil)
	_ run.EpisodeReader             = (*Store)(nil)
	_ run.NarrativeWriter           = (*Store)(nil)
	_ run.NarrativeReader           = (*Store)(nil)
)
