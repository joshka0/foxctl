package contextvar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

// ErrNotFound indicates the variable was not found.
var ErrNotFound = errors.New("contextvar: not found")

// ErrConflict indicates a key already exists.
var ErrConflict = errors.New("contextvar: key conflict")

// Store defines the persistence interface for context variables.
type Store interface {
	Close() error

	// Put stores a context variable.
	// If Upsert=true and key exists, updates the value; otherwise creates new.
	Put(ctx context.Context, params PutParams) (*Variable, error)

	// Get retrieves a single variable by ID.
	Get(ctx context.Context, id string) (*Variable, error)

	// GetByKey retrieves a variable by conversation+scope+key.
	GetByKey(ctx context.Context, conversationID string, scope Scope, key string) (*Variable, error)

	// Query retrieves variables matching the given criteria.
	Query(ctx context.Context, params QueryParams) (*QueryResult, error)

	// QuerySemantic searches by embedding similarity.
	QuerySemantic(ctx context.Context, conversationID string, embedding []float32, limit int) (*SemanticResult, error)

	// ListKeys returns available keys for a conversation.
	ListKeys(ctx context.Context, conversationID string, scope Scope) (*ListKeysResult, error)

	// Delete removes a variable by ID.
	Delete(ctx context.Context, id string) error

	// DeleteByKey removes a variable by conversation+scope+key.
	DeleteByKey(ctx context.Context, conversationID string, scope Scope, key string) error

	// DeleteExpired removes expired variables.
	DeleteExpired(ctx context.Context) (int, error)

	// DeleteByConversation removes all variables for a conversation.
	DeleteByConversation(ctx context.Context, conversationID string) (int, error)

	// IncrementAccess updates access count and last access time.
	IncrementAccess(ctx context.Context, id string) error

	// Stats returns storage statistics.
	Stats(ctx context.Context) (*Stats, error)

	// DB returns the underlying database connection.
	DB() *sql.DB
}

// Open opens or creates a context variable Store backed by a SQLite database at storageRoot/contextvar.db.
// It applies the necessary schema migrations and provides an sql-backed Store; the returned Store should be closed when no longer needed.
func Open(ctx context.Context, storageRoot string) (Store, error) {
	dbPath := filepath.Join(storageRoot, "contextvar.db")
	db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("contextvar: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}


type sqlStore struct {
	db    *sql.DB
	close func() error
}


func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}


func (s *sqlStore) DB() *sql.DB {
	return s.db
}

func migrate(ctx context.Context, db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS context_variables (
		id              TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL DEFAULT '',
		scope           TEXT NOT NULL CHECK (scope IN ('global', 'conversation', 'turn')),
		key             TEXT NOT NULL,

		value_json      TEXT,
		value_cas       TEXT,
		content_type    TEXT NOT NULL DEFAULT 'json',

		sequence_num    INTEGER NOT NULL,

		source          TEXT NOT NULL DEFAULT '',
		created_at_ms   INTEGER NOT NULL,
		updated_at_ms   INTEGER NOT NULL,
		expires_at_ms   INTEGER,
		access_count    INTEGER NOT NULL DEFAULT 0,
		last_access_ms  INTEGER,

		embedding       BLOB,
		embedding_model TEXT,

		UNIQUE (conversation_id, scope, key)
	);

	CREATE INDEX IF NOT EXISTS idx_cv_conversation ON context_variables(conversation_id, scope);
	CREATE INDEX IF NOT EXISTS idx_cv_key ON context_variables(key);
	CREATE INDEX IF NOT EXISTS idx_cv_sequence ON context_variables(conversation_id, sequence_num);
	CREATE INDEX IF NOT EXISTS idx_cv_expires ON context_variables(expires_at_ms) WHERE expires_at_ms IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_cv_scope ON context_variables(scope);

	-- Sequence counter for auto-incrementing sequence_num per conversation
	CREATE TABLE IF NOT EXISTS context_sequences (
		conversation_id TEXT PRIMARY KEY,
		next_seq        INTEGER NOT NULL DEFAULT 1
	);
	`

	_, err := db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("contextvar: migrate: %w", err)
	}
	return nil
}

func (s *sqlStore) Put(ctx context.Context, params PutParams) (*Variable, error) {
	if params.Key == "" {
		return nil, errors.New("contextvar: key is required")
	}
	if params.Scope == "" {
		params.Scope = ScopeConversation
	}
	if params.ContentType == "" {
		params.ContentType = "json"
	}

	// Marshal value to JSON
	valueJSON, err := json.Marshal(params.Value)
	if err != nil {
		return nil, fmt.Errorf("contextvar: marshal value: %w", err)
	}

	now := time.Now()
	nowMS := now.UnixMilli()

	// Get next sequence number
	seqNum, err := s.nextSequence(ctx, params.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("contextvar: get sequence: %w", err)
	}

	var expiresAtMS *int64
	if params.TTL > 0 {
		exp := nowMS + params.TTL.Milliseconds()
		expiresAtMS = &exp
	}

	id := ulid.Make().String()

	if params.Upsert {
		// Try update first
		result, err := s.db.ExecContext(ctx, `
			UPDATE context_variables
			SET value_json = ?, updated_at_ms = ?, expires_at_ms = ?, source = ?
			WHERE conversation_id = ? AND scope = ? AND key = ?
		`, string(valueJSON), nowMS, expiresAtMS, params.Source,
			params.ConversationID, string(params.Scope), params.Key)
		if err != nil {
			return nil, fmt.Errorf("contextvar: upsert update: %w", err)
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected > 0 {
			// Updated existing, fetch and return
			return s.GetByKey(ctx, params.ConversationID, params.Scope, params.Key)
		}
		// Fall through to insert
	}

	// Insert new
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO context_variables (
			id, conversation_id, scope, key, value_json, content_type,
			sequence_num, source, created_at_ms, updated_at_ms, expires_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, params.ConversationID, string(params.Scope), params.Key,
		string(valueJSON), params.ContentType, seqNum, params.Source,
		nowMS, nowMS, expiresAtMS)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, fmt.Errorf("%w: %s", ErrConflict, params.Key)
		}
		return nil, fmt.Errorf("contextvar: insert: %w", err)
	}

	return s.Get(ctx, id)
}

func (s *sqlStore) nextSequence(ctx context.Context, conversationID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback contextvar sequence tx")
	}()

	// Upsert sequence counter
	_, err = tx.ExecContext(ctx, `
		INSERT INTO context_sequences (conversation_id, next_seq)
		VALUES (?, 1)
		ON CONFLICT(conversation_id) DO UPDATE SET next_seq = next_seq + 1
	`, conversationID)
	if err != nil {
		return 0, err
	}

	var seq int
	err = tx.QueryRowContext(ctx, `
		SELECT next_seq FROM context_sequences WHERE conversation_id = ?
	`, conversationID).Scan(&seq)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return seq, nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (*Variable, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, conversation_id, scope, key, value_json, value_cas, content_type,
		       sequence_num, source, created_at_ms, updated_at_ms, expires_at_ms,
		       access_count, last_access_ms, embedding, embedding_model
		FROM context_variables WHERE id = ?
	`, id)

	return s.scanVariable(row)
}

func (s *sqlStore) GetByKey(ctx context.Context, conversationID string, scope Scope, key string) (*Variable, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, conversation_id, scope, key, value_json, value_cas, content_type,
		       sequence_num, source, created_at_ms, updated_at_ms, expires_at_ms,
		       access_count, last_access_ms, embedding, embedding_model
		FROM context_variables
		WHERE conversation_id = ? AND scope = ? AND key = ?
	`, conversationID, string(scope), key)

	return s.scanVariable(row)
}

func (s *sqlStore) scanVariable(row *sql.Row) (*Variable, error) {
	var v Variable
	var scopeStr string
	var valueJSON, valueCAS, source, embeddingModel sql.NullString
	var createdMS, updatedMS int64
	var expiresMS, lastAccessMS sql.NullInt64
	var embedding []byte

	err := row.Scan(
		&v.ID, &v.ConversationID, &scopeStr, &v.Key,
		&valueJSON, &valueCAS, &v.ContentType,
		&v.SequenceNum, &source, &createdMS, &updatedMS, &expiresMS,
		&v.AccessCount, &lastAccessMS, &embedding, &embeddingModel,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("contextvar: scan: %w", err)
	}

	v.Scope = Scope(scopeStr)
	if valueJSON.Valid {
		v.ValueJSON = json.RawMessage(valueJSON.String)
	}
	if valueCAS.Valid {
		v.ValueCAS = valueCAS.String
	}
	if source.Valid {
		v.Source = source.String
	}
	v.CreatedAt = time.UnixMilli(createdMS)
	v.UpdatedAt = time.UnixMilli(updatedMS)
	if expiresMS.Valid {
		t := time.UnixMilli(expiresMS.Int64)
		v.ExpiresAt = &t
	}
	if lastAccessMS.Valid {
		t := time.UnixMilli(lastAccessMS.Int64)
		v.LastAccess = &t
	}
	if embeddingModel.Valid {
		v.EmbeddingModel = embeddingModel.String
	}
	// Note: embedding deserialization would need float32 conversion

	return &v, nil
}

func (s *sqlStore) Query(ctx context.Context, params QueryParams) (*QueryResult, error) {
	var conditions []string
	var args []interface{}

	// Filter by conversation
	if params.ConversationID != "" {
		conditions = append(conditions, "conversation_id = ?")
		args = append(args, params.ConversationID)
	}

	// Filter by scope
	if params.Scope != "" {
		conditions = append(conditions, "scope = ?")
		args = append(args, string(params.Scope))
	}

	// Filter by key
	if params.Key != "" {
		conditions = append(conditions, "key = ?")
		args = append(args, params.Key)
	} else if params.KeyPrefix != "" {
		conditions = append(conditions, "key LIKE ? ESCAPE '\\'")
		args = append(args, escapeLikeLiteral(params.KeyPrefix)+"%")
	} else if params.KeyPattern != "" {
		// Convert glob to SQL LIKE while escaping literal wildcards.
		conditions = append(conditions, "key LIKE ? ESCAPE '\\'")
		args = append(args, buildLikePattern(params.KeyPattern))
	}

	// Filter by sequence range
	if params.SequenceRange != nil {
		conditions = append(conditions, "sequence_num >= ?")
		args = append(args, params.SequenceRange.Start)
		if params.SequenceRange.End >= 0 {
			conditions = append(conditions, "sequence_num <= ?")
			args = append(args, params.SequenceRange.End)
		}
	}

	// Exclude expired by default
	if !params.IncludeExpired {
		nowMS := time.Now().UnixMilli()
		conditions = append(conditions, "(expires_at_ms IS NULL OR expires_at_ms > ?)")
		args = append(args, nowMS)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total
	var totalCount int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM context_variables %s", whereClause)
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("contextvar: count: %w", err)
	}

	// Order by
	orderBy := "created_at_ms"
	if params.OrderBy != "" {
		switch params.OrderBy {
		case "created_at", "created_at_ms":
			orderBy = "created_at_ms"
		case "updated_at", "updated_at_ms":
			orderBy = "updated_at_ms"
		case "sequence_num":
			orderBy = "sequence_num"
		case "access_count":
			orderBy = "access_count"
		case "key":
			orderBy = "key"
		}
	}
	if params.OrderDesc {
		orderBy += " DESC"
	}

	// Limit and offset
	limit := 50
	if params.Limit > 0 {
		limit = params.Limit
	}

	query := fmt.Sprintf(`
		SELECT id, conversation_id, scope, key, value_json, value_cas, content_type,
		       sequence_num, source, created_at_ms, updated_at_ms, expires_at_ms,
		       access_count, last_access_ms, embedding, embedding_model
		FROM context_variables %s
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, whereClause, orderBy)

	args = append(args, limit, params.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextvar: query: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close contextvar query rows")
	}()

	var variables []Variable
	for rows.Next() {
		v, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		variables = append(variables, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contextvar: rows err: %w", err)
	}

	return &QueryResult{
		Variables:  variables,
		TotalCount: totalCount,
		HasMore:    params.Offset+len(variables) < totalCount,
	}, nil
}

func (s *sqlStore) scanRow(rows *sql.Rows) (Variable, error) {
	var v Variable
	var scopeStr string
	var valueJSON, valueCAS, source, embeddingModel sql.NullString
	var createdMS, updatedMS int64
	var expiresMS, lastAccessMS sql.NullInt64
	var embedding []byte

	err := rows.Scan(
		&v.ID, &v.ConversationID, &scopeStr, &v.Key,
		&valueJSON, &valueCAS, &v.ContentType,
		&v.SequenceNum, &source, &createdMS, &updatedMS, &expiresMS,
		&v.AccessCount, &lastAccessMS, &embedding, &embeddingModel,
	)
	if err != nil {
		return Variable{}, fmt.Errorf("contextvar: scan row: %w", err)
	}

	v.Scope = Scope(scopeStr)
	if valueJSON.Valid {
		v.ValueJSON = json.RawMessage(valueJSON.String)
	}
	if valueCAS.Valid {
		v.ValueCAS = valueCAS.String
	}
	if source.Valid {
		v.Source = source.String
	}
	v.CreatedAt = time.UnixMilli(createdMS)
	v.UpdatedAt = time.UnixMilli(updatedMS)
	if expiresMS.Valid {
		t := time.UnixMilli(expiresMS.Int64)
		v.ExpiresAt = &t
	}
	if lastAccessMS.Valid {
		t := time.UnixMilli(lastAccessMS.Int64)
		v.LastAccess = &t
	}

	return v, nil
}

func (s *sqlStore) QuerySemantic(ctx context.Context, conversationID string, embedding []float32, limit int) (*SemanticResult, error) {
	// TODO: Implement semantic search using cosine similarity
	// For now, return empty result - requires vector search capability
	return &SemanticResult{
		Variables:  []ScoredVariable{},
		TotalCount: 0,
	}, nil
}

func (s *sqlStore) ListKeys(ctx context.Context, conversationID string, scope Scope) (*ListKeysResult, error) {
	var conditions []string
	var args []interface{}

	if conversationID != "" {
		conditions = append(conditions, "conversation_id = ?")
		args = append(args, conversationID)
	}
	if scope != "" {
		conditions = append(conditions, "scope = ?")
		args = append(args, string(scope))
	}

	// Exclude expired
	nowMS := time.Now().UnixMilli()
	conditions = append(conditions, "(expires_at_ms IS NULL OR expires_at_ms > ?)")
	args = append(args, nowMS)

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT key, scope, content_type, created_at_ms, updated_at_ms, access_count,
		       LENGTH(COALESCE(value_json, '')) as size_bytes
		FROM context_variables %s
		ORDER BY key
	`, whereClause)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextvar: list keys: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close contextvar list keys rows")
	}()

	var keys []KeyInfo
	for rows.Next() {
		var k KeyInfo
		var scopeStr string
		var createdMS, updatedMS int64

		err := rows.Scan(&k.Key, &scopeStr, &k.ContentType, &createdMS, &updatedMS, &k.AccessCount, &k.SizeBytes)
		if err != nil {
			return nil, fmt.Errorf("contextvar: scan key: %w", err)
		}

		k.Scope = Scope(scopeStr)
		k.CreatedAt = time.UnixMilli(createdMS)
		k.UpdatedAt = time.UnixMilli(updatedMS)
		keys = append(keys, k)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contextvar: list keys rows err: %w", err)
	}

	return &ListKeysResult{
		Keys:       keys,
		TotalCount: len(keys),
	}, nil
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM context_variables WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("contextvar: delete: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *sqlStore) DeleteByKey(ctx context.Context, conversationID string, scope Scope, key string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM context_variables
		WHERE conversation_id = ? AND scope = ? AND key = ?
	`, conversationID, string(scope), key)
	if err != nil {
		return fmt.Errorf("contextvar: delete by key: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *sqlStore) DeleteExpired(ctx context.Context) (int, error) {
	nowMS := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM context_variables
		WHERE expires_at_ms IS NOT NULL AND expires_at_ms <= ?
	`, nowMS)
	if err != nil {
		return 0, fmt.Errorf("contextvar: delete expired: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return int(rowsAffected), nil
}

func (s *sqlStore) DeleteByConversation(ctx context.Context, conversationID string) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM context_variables WHERE conversation_id = ?
	`, conversationID)
	if err != nil {
		return 0, fmt.Errorf("contextvar: delete by conversation: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	// Also clean up sequence counter
	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM context_sequences WHERE conversation_id = ?
	`, conversationID)

	return int(rowsAffected), nil
}

func (s *sqlStore) IncrementAccess(ctx context.Context, id string) error {
	nowMS := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		UPDATE context_variables
		SET access_count = access_count + 1, last_access_ms = ?
		WHERE id = ?
	`, nowMS, id)
	if err != nil {
		return fmt.Errorf("contextvar: increment access: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *sqlStore) Stats(ctx context.Context) (*Stats, error) {
	var stats Stats

	// Total variables
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM context_variables`).Scan(&stats.TotalVariables)
	if err != nil {
		return nil, fmt.Errorf("contextvar: count total: %w", err)
	}

	// Global variables
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM context_variables WHERE scope = 'global'`).Scan(&stats.GlobalVariables)
	if err != nil {
		return nil, fmt.Errorf("contextvar: count global: %w", err)
	}

	// Distinct conversations
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT conversation_id) FROM context_variables WHERE conversation_id != ''`).Scan(&stats.ConversationCount)
	if err != nil {
		return nil, fmt.Errorf("contextvar: count conversations: %w", err)
	}

	// Expired count
	nowMS := time.Now().UnixMilli()
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM context_variables WHERE expires_at_ms IS NOT NULL AND expires_at_ms <= ?`, nowMS).Scan(&stats.ExpiredCount)
	if err != nil {
		return nil, fmt.Errorf("contextvar: count expired: %w", err)
	}

	// Total size
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(LENGTH(COALESCE(value_json, ''))), 0) FROM context_variables`).Scan(&stats.TotalSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("contextvar: sum size: %w", err)
	}

	// Oldest/newest
	var oldestMS, newestMS sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT MIN(created_at_ms), MAX(created_at_ms) FROM context_variables`).Scan(&oldestMS, &newestMS)
	if oldestMS.Valid {
		stats.OldestCreatedAt = time.UnixMilli(oldestMS.Int64)
	}
	if newestMS.Valid {
		stats.NewestCreatedAt = time.UnixMilli(newestMS.Int64)
	}

	return &stats, nil
}

func escapeLikeLiteral(input string) string {
	if input == "" {
		return input
	}
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '%', '_', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(input[i])
	}
	return b.String()
}

func buildLikePattern(pattern string) string {
	if pattern == "" {
		return pattern
	}
	var b strings.Builder
	b.Grow(len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			b.WriteByte('%')
		case '?':
			b.WriteByte('_')
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}