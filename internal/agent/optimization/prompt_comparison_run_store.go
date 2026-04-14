package optimization

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

// PromptComparisonRun is one persisted prompt-comparison execution.
type PromptComparisonRun struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	ArtifactDigest string    `json:"artifact_digest"`
	Provider       string    `json:"provider"`
	BaseURL        string    `json:"base_url,omitempty"`
	Question       string    `json:"question"`
	Context        string    `json:"context,omitempty"`
	ModelCount     int       `json:"model_count"`
	VariantCount   int       `json:"variant_count"`
	SuccessCount   int       `json:"success_count"`
	FailureCount   int       `json:"failure_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// PromptComparisonRunStore persists prompt comparison metadata.
type PromptComparisonRunStore interface {
	Close() error
	Save(ctx context.Context, run PromptComparisonRun) (PromptComparisonRun, error)
	Get(ctx context.Context, workspaceID, id string) (PromptComparisonRun, error)
	List(ctx context.Context, workspaceID string, limit int) ([]PromptComparisonRun, error)
}

type sqlPromptComparisonRunStore struct {
	db    *sql.DB
	close func() error
}

// OpenPromptComparisonRunStore opens the store backing prompt comparison runs.
func OpenPromptComparisonRunStore(ctx context.Context, root string) (PromptComparisonRunStore, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "PROMPT_COMPARISONS", "prompt_comparisons.db", migratePromptComparisonRuns)
	if err != nil {
		return nil, fmt.Errorf("prompt_comparisons: open db: %w", err)
	}
	return &sqlPromptComparisonRunStore{db: db, close: closeFn}, nil
}

func (s *sqlPromptComparisonRunStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func migratePromptComparisonRuns(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS prompt_comparisons (
	id TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	artifact_digest TEXT NOT NULL,
	provider TEXT NOT NULL,
	base_url TEXT,
	question TEXT NOT NULL,
	context TEXT,
	model_count INTEGER NOT NULL DEFAULT 0,
	variant_count INTEGER NOT NULL DEFAULT 0,
	success_count INTEGER NOT NULL DEFAULT 0,
	failure_count INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prompt_comparisons_workspace_created
	ON prompt_comparisons(workspace_id, created_at DESC, id DESC);
`
	_, err := db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("prompt_comparisons: migrate: %w", err)
	}
	return nil
}

func generatePromptComparisonRunID() string {
	return ulid.Make().String()
}

func (s *sqlPromptComparisonRunStore) Save(ctx context.Context, run PromptComparisonRun) (PromptComparisonRun, error) {
	if strings.TrimSpace(run.WorkspaceID) == "" {
		return PromptComparisonRun{}, fmt.Errorf("prompt_comparisons: workspace_id is required")
	}
	if strings.TrimSpace(run.ArtifactDigest) == "" {
		return PromptComparisonRun{}, fmt.Errorf("prompt_comparisons: artifact_digest is required")
	}
	if strings.TrimSpace(run.Provider) == "" {
		return PromptComparisonRun{}, fmt.Errorf("prompt_comparisons: provider is required")
	}
	if strings.TrimSpace(run.Question) == "" {
		return PromptComparisonRun{}, fmt.Errorf("prompt_comparisons: question is required")
	}
	if run.ID == "" {
		run.ID = generatePromptComparisonRunID()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = timeutil.NowUTC()
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO prompt_comparisons (
	id, workspace_id, artifact_digest, provider, base_url, question, context,
	model_count, variant_count, success_count, failure_count, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		run.ID,
		strings.TrimSpace(run.WorkspaceID),
		strings.TrimSpace(run.ArtifactDigest),
		strings.TrimSpace(run.Provider),
		strings.TrimSpace(run.BaseURL),
		strings.TrimSpace(run.Question),
		strings.TrimSpace(run.Context),
		run.ModelCount,
		run.VariantCount,
		run.SuccessCount,
		run.FailureCount,
		sqlutil.FormatTimestamp(run.CreatedAt),
	)
	if err != nil {
		return PromptComparisonRun{}, fmt.Errorf("prompt_comparisons: save: %w", err)
	}
	return run, nil
}

func (s *sqlPromptComparisonRunStore) Get(ctx context.Context, workspaceID, id string) (PromptComparisonRun, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, artifact_digest, provider, base_url, question, context,
       model_count, variant_count, success_count, failure_count, created_at
FROM prompt_comparisons
WHERE workspace_id = ? AND id = ?
`, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
	return scanPromptComparisonRun(row)
}

func (s *sqlPromptComparisonRunStore) List(ctx context.Context, workspaceID string, limit int) ([]PromptComparisonRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, artifact_digest, provider, base_url, question, context,
       model_count, variant_count, success_count, failure_count, created_at
FROM prompt_comparisons
WHERE workspace_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?
`, strings.TrimSpace(workspaceID), limit)
	if err != nil {
		return nil, fmt.Errorf("prompt_comparisons: list: %w", err)
	}
	defer rows.Close()

	var runs []PromptComparisonRun
	for rows.Next() {
		run, err := scanPromptComparisonRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

type promptComparisonRunScanner interface {
	Scan(dest ...any) error
}

func scanPromptComparisonRun(scanner promptComparisonRunScanner) (PromptComparisonRun, error) {
	var (
		run       PromptComparisonRun
		baseURL   sql.NullString
		reqCtx    sql.NullString
		createdAt string
	)
	if err := scanner.Scan(
		&run.ID,
		&run.WorkspaceID,
		&run.ArtifactDigest,
		&run.Provider,
		&baseURL,
		&run.Question,
		&reqCtx,
		&run.ModelCount,
		&run.VariantCount,
		&run.SuccessCount,
		&run.FailureCount,
		&createdAt,
	); err != nil {
		return PromptComparisonRun{}, err
	}
	if baseURL.Valid {
		run.BaseURL = baseURL.String
	}
	if reqCtx.Valid {
		run.Context = reqCtx.String
	}
	parsedCreatedAt, err := sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return PromptComparisonRun{}, fmt.Errorf("prompt_comparisons: decode created_at: %w", err)
	}
	run.CreatedAt = parsedCreatedAt
	return run, nil
}
