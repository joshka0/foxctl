package optimization

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

// Pattern represents a learned tool usage pattern from successful executions.
type Pattern struct {
	// ID uniquely identifies the pattern.
	ID string `json:"id"`

	// AgentRole identifies the agent type (coder, planner, reviewer, overseer).
	AgentRole string `json:"agent_role"`

	// Context describes the situation when this pattern was used.
	Context string `json:"context"`

	// ToolSequence lists the tools used in order.
	ToolSequence []string `json:"tool_sequence"`

	// Outcome indicates the result: success, failure, partial.
	Outcome string `json:"outcome"`

	// Count is the number of times this pattern has been observed.
	Count int `json:"count"`

	// SuccessCount is the number of successful uses.
	SuccessCount int `json:"success_count"`

	// AvgDurationMS is the average execution time in milliseconds.
	AvgDurationMS int64 `json:"avg_duration_ms"`

	// LastSeen is when the pattern was last observed.
	LastSeen time.Time `json:"last_seen"`

	// CreatedAt is when the pattern was first recorded.
	CreatedAt time.Time `json:"created_at"`
}

// SuccessRate returns the success rate of this pattern (0.0 to 1.0).
func (p Pattern) SuccessRate() float64 {
	if p.Count == 0 {
		return 0.0
	}
	return float64(p.SuccessCount) / float64(p.Count)
}

// PatternStore defines the persistence interface for learned patterns.
type PatternStore interface {
	Close() error

	// Record adds or updates a pattern observation.
	Record(ctx context.Context, pattern Pattern) error

	// FindSimilar returns patterns with similar context (using simple text matching).
	// threshold is the minimum similarity score (0.0 to 1.0).
	FindSimilar(ctx context.Context, agentRole, context string, threshold float64) ([]Pattern, error)

	// GetTopPatterns returns the most successful patterns for an agent role.
	GetTopPatterns(ctx context.Context, agentRole string, limit int) ([]Pattern, error)

	// GetByToolSequence returns a pattern matching the exact tool sequence.
	GetByToolSequence(ctx context.Context, agentRole string, tools []string) (Pattern, error)

	// List returns patterns for an agent role, sorted by success rate.
	List(ctx context.Context, agentRole string, limit int) ([]Pattern, error)

	// Delete removes a pattern by ID.
	Delete(ctx context.Context, id string) error

	// Cleanup removes patterns older than the specified duration.
	Cleanup(ctx context.Context, maxAge time.Duration) (int64, error)

	// Clear removes all patterns for an agent role (or all patterns if role is empty).
	Clear(ctx context.Context, agentRole string) error
}

type sqlPatternStore struct {
	db    *sql.DB
	close func() error
}

// OpenPatternStore opens or creates a pattern store backed by a SQLite database
// located at "<root>/patterns.db" and ensures the required schema is present.
// The returned PatternStore provides persistent operations for learned patterns;
// call Close on it when finished. It returns an error if the database cannot be
// opened or migrated.
func OpenPatternStore(ctx context.Context, root string) (PatternStore, error) {
	dbPath := filepath.Join(root, "patterns.db")
	db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, migratePatterns)
	if err != nil {
		return nil, fmt.Errorf("patterns: open db: %w", err)
	}
	return &sqlPatternStore{db: db, close: closeFn}, nil
}

func (s *sqlPatternStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// migratePatterns creates the patterns table and required indexes if they do not exist.
// It ensures the database schema contains the columns and indexes used by the pattern store
// (agent_role, context, tool_sequence, outcome, count, success_count, avg_duration_ms, last_seen, created_at),
// and returns an error if executing the migration DDL fails.
func migratePatterns(ctx context.Context, db *sql.DB) error {
	ddl := `
-- Patterns table stores learned tool usage patterns.
CREATE TABLE IF NOT EXISTS patterns (
	id               TEXT PRIMARY KEY,
	agent_role       TEXT NOT NULL,
	context          TEXT NOT NULL,
	tool_sequence    TEXT NOT NULL,
	outcome          TEXT NOT NULL,
	count            INTEGER NOT NULL DEFAULT 1,
	success_count    INTEGER NOT NULL DEFAULT 0,
	avg_duration_ms  INTEGER NOT NULL DEFAULT 0,
	last_seen        TEXT NOT NULL,
	created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_patterns_agent_role ON patterns(agent_role);
CREATE INDEX IF NOT EXISTS idx_patterns_success_rate ON patterns(agent_role, success_count * 1.0 / count DESC);
CREATE INDEX IF NOT EXISTS idx_patterns_last_seen ON patterns(last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_patterns_tool_sequence ON patterns(agent_role, tool_sequence);
`
	_, err := db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("patterns: migrate: %w", err)
	}
	return nil
}

func generatePatternID() string {
	return ulid.Make().String()
}

// Record adds or updates a pattern observation.
func (s *sqlPatternStore) Record(ctx context.Context, pattern Pattern) error {
	now := timeutil.NowUTC()

	toolsJSON, err := sqlutil.FormatJSON(pattern.ToolSequence)
	if err != nil {
		return fmt.Errorf("patterns: format tools: %w", err)
	}

	// Check if pattern already exists for this agent role and tool sequence
	var existingID string
	var existingCount, existingSuccessCount int
	var existingAvgDuration int64
	err = s.db.QueryRowContext(ctx, `
SELECT id, count, success_count, avg_duration_ms FROM patterns
WHERE agent_role = ? AND tool_sequence = ?
`, pattern.AgentRole, toolsJSON).Scan(&existingID, &existingCount, &existingSuccessCount, &existingAvgDuration)

	if err == nil {
		// Update existing pattern
		newCount := existingCount + 1
		newSuccessCount := existingSuccessCount
		if pattern.Outcome == "success" {
			newSuccessCount++
		}
		// Compute running average for duration
		newAvgDuration := ((existingAvgDuration * int64(existingCount)) + pattern.AvgDurationMS) / int64(newCount)

		_, err = s.db.ExecContext(ctx, `
UPDATE patterns SET count = ?, success_count = ?, avg_duration_ms = ?,
	context = ?, outcome = ?, last_seen = ?
WHERE id = ?
`, newCount, newSuccessCount, newAvgDuration, pattern.Context, pattern.Outcome,
			sqlutil.FormatTimestamp(now), existingID)
		if err != nil {
			return fmt.Errorf("patterns: update: %w", err)
		}
		return nil
	}

	// Insert new pattern
	if pattern.ID == "" {
		pattern.ID = generatePatternID()
	}
	if pattern.CreatedAt.IsZero() {
		pattern.CreatedAt = now
	}
	pattern.LastSeen = now
	pattern.Count = 1
	if pattern.Outcome == "success" {
		pattern.SuccessCount = 1
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO patterns (id, agent_role, context, tool_sequence, outcome, count, success_count, avg_duration_ms, last_seen, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, pattern.ID, pattern.AgentRole, pattern.Context, toolsJSON, pattern.Outcome,
		pattern.Count, pattern.SuccessCount, pattern.AvgDurationMS,
		sqlutil.FormatTimestamp(pattern.LastSeen), sqlutil.FormatTimestamp(pattern.CreatedAt))
	if err != nil {
		return fmt.Errorf("patterns: insert: %w", err)
	}
	return nil
}

// escapeLikePattern escapes SQL LIKE special characters for safe use in patterns.
// It first escapes backslashes, then % and _ wildcards, suitable for use with ESCAPE '\'.
func escapeLikePattern(s string) string {
	// First escape backslashes (must be done first to avoid double-escaping)
	s = strings.ReplaceAll(s, `\`, `\\`)
	// Then escape LIKE wildcards
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// FindSimilar returns patterns with similar context using simple keyword matching.
func (s *sqlPatternStore) FindSimilar(ctx context.Context, agentRole, context string, threshold float64) ([]Pattern, error) {
	// For now, use LIKE matching on context keywords
	// A more sophisticated implementation would use embeddings
	escapedContext := escapeLikePattern(context)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, agent_role, context, tool_sequence, outcome, count, success_count, avg_duration_ms, last_seen, created_at
FROM patterns
WHERE agent_role = ? AND context LIKE ? ESCAPE '\'
ORDER BY success_count * 1.0 / count DESC, count DESC
LIMIT 10
`, agentRole, "%"+escapedContext+"%")
	if err != nil {
		return nil, fmt.Errorf("patterns: find similar: %w", err)
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// GetTopPatterns returns the most successful patterns for an agent role.
func (s *sqlPatternStore) GetTopPatterns(ctx context.Context, agentRole string, limit int) ([]Pattern, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, agent_role, context, tool_sequence, outcome, count, success_count, avg_duration_ms, last_seen, created_at
FROM patterns
WHERE agent_role = ? AND count >= 3
ORDER BY success_count * 1.0 / count DESC, count DESC
LIMIT ?
`, agentRole, limit)
	if err != nil {
		return nil, fmt.Errorf("patterns: get top: %w", err)
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// GetByToolSequence returns a pattern matching the exact tool sequence.
func (s *sqlPatternStore) GetByToolSequence(ctx context.Context, agentRole string, tools []string) (Pattern, error) {
	toolsJSON, err := sqlutil.FormatJSON(tools)
	if err != nil {
		return Pattern{}, fmt.Errorf("patterns: format tools: %w", err)
	}

	row := s.db.QueryRowContext(ctx, `
SELECT id, agent_role, context, tool_sequence, outcome, count, success_count, avg_duration_ms, last_seen, created_at
FROM patterns
WHERE agent_role = ? AND tool_sequence = ?
`, agentRole, toolsJSON)

	return scanPatternRow(row)
}

// List returns patterns for an agent role, sorted by success rate.
func (s *sqlPatternStore) List(ctx context.Context, agentRole string, limit int) ([]Pattern, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, agent_role, context, tool_sequence, outcome, count, success_count, avg_duration_ms, last_seen, created_at
FROM patterns
WHERE agent_role = ?
ORDER BY success_count * 1.0 / count DESC, count DESC
LIMIT ?
`, agentRole, limit)
	if err != nil {
		return nil, fmt.Errorf("patterns: list: %w", err)
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// Delete removes a pattern by ID.
func (s *sqlPatternStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM patterns WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("patterns: delete: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("patterns: rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrPatternNotFound
	}
	return nil
}

// Cleanup removes patterns older than the specified duration.
func (s *sqlPatternStore) Cleanup(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := timeutil.NowUTC().Add(-maxAge)
	result, err := s.db.ExecContext(ctx, `
DELETE FROM patterns WHERE last_seen < ?
`, sqlutil.FormatTimestamp(cutoff))
	if err != nil {
		return 0, fmt.Errorf("patterns: cleanup: %w", err)
	}
	return result.RowsAffected()
}

// Clear removes all patterns for an agent role (or all patterns if role is empty).
func (s *sqlPatternStore) Clear(ctx context.Context, agentRole string) error {
	var err error
	if agentRole == "" {
		_, err = s.db.ExecContext(ctx, `DELETE FROM patterns`)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM patterns WHERE agent_role = ?`, agentRole)
	}
	if err != nil {
		return fmt.Errorf("patterns: clear: %w", err)
	}
	return nil
}

// ErrPatternNotFound indicates a pattern was not found.
var ErrPatternNotFound = fmt.Errorf("pattern not found")

func scanPatternRow(row *sql.Row) (Pattern, error) {
	var p Pattern
	var toolsJSON string
	var lastSeen, createdAt string

	err := row.Scan(
		&p.ID, &p.AgentRole, &p.Context, &toolsJSON, &p.Outcome,
		&p.Count, &p.SuccessCount, &p.AvgDurationMS, &lastSeen, &createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Pattern{}, ErrPatternNotFound
		}
		return Pattern{}, err
	}

	_ = sqlutil.ScanJSON(toolsJSON, &p.ToolSequence)  //nolint:errcheck
	p.LastSeen, _ = sqlutil.ScanTimestamp(lastSeen)   //nolint:errcheck
	p.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt) //nolint:errcheck

	return p, nil
}

func scanPatternRows(rows *sql.Rows) ([]Pattern, error) {
	patterns := make([]Pattern, 0)
	for rows.Next() {
		var p Pattern
		var toolsJSON string
		var lastSeen, createdAt string

		err := rows.Scan(
			&p.ID, &p.AgentRole, &p.Context, &toolsJSON, &p.Outcome,
			&p.Count, &p.SuccessCount, &p.AvgDurationMS, &lastSeen, &createdAt,
		)
		if err != nil {
			return nil, err
		}

		_ = sqlutil.ScanJSON(toolsJSON, &p.ToolSequence)  //nolint:errcheck
		p.LastSeen, _ = sqlutil.ScanTimestamp(lastSeen)   //nolint:errcheck
		p.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt) //nolint:errcheck

		patterns = append(patterns, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return patterns, nil
}
