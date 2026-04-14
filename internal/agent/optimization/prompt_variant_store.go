package optimization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

// PromptVariant is one persisted optimized prompt candidate for a role.
type PromptVariant struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	AgentRole      string         `json:"agent_role"`
	TargetProfile  string         `json:"target_profile,omitempty"`
	Mode           string         `json:"mode"`
	OriginalPrompt string         `json:"original_prompt"`
	Prompt         string         `json:"prompt"`
	OriginalScore  float64        `json:"original_score"`
	OptimizedScore float64        `json:"optimized_score"`
	Improvement    float64        `json:"improvement"`
	CandidateCount int            `json:"candidate_count"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// PromptVariantStore persists optimized prompt variants for later reuse.
type PromptVariantStore interface {
	Close() error
	Save(ctx context.Context, variant PromptVariant) (PromptVariant, error)
	Get(ctx context.Context, workspaceID, id string) (PromptVariant, error)
	List(ctx context.Context, workspaceID, agentRole string, limit int) ([]PromptVariant, error)
	ListByTargetProfile(ctx context.Context, workspaceID, agentRole, targetProfile string, limit int) ([]PromptVariant, error)
	ResolveLatestCompatible(ctx context.Context, workspaceID, agentRole, targetProfile string) (PromptVariant, error)
}

type sqlPromptVariantStore struct {
	db    *sql.DB
	close func() error
}

// OpenPromptVariantStore opens the store backing optimized prompt variants.
func OpenPromptVariantStore(ctx context.Context, root string) (PromptVariantStore, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "PROMPT_VARIANTS", "prompt_variants.db", migratePromptVariants)
	if err != nil {
		return nil, fmt.Errorf("prompt_variants: open db: %w", err)
	}
	return &sqlPromptVariantStore{db: db, close: closeFn}, nil
}

func (s *sqlPromptVariantStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func migratePromptVariants(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS prompt_variants (
	id TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	agent_role TEXT NOT NULL,
	target_profile TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL,
	original_prompt TEXT NOT NULL,
	prompt TEXT NOT NULL,
	original_score REAL NOT NULL DEFAULT 0,
	optimized_score REAL NOT NULL DEFAULT 0,
	improvement REAL NOT NULL DEFAULT 0,
	candidate_count INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prompt_variants_workspace_role_created
	ON prompt_variants(workspace_id, agent_role, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_variants_workspace_created
	ON prompt_variants(workspace_id, created_at DESC, id DESC);
`
	_, err := db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("prompt_variants: migrate: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE prompt_variants ADD COLUMN target_profile TEXT NOT NULL DEFAULT ''`); err != nil {
		errMsg := strings.ToLower(strings.TrimSpace(err.Error()))
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			return fmt.Errorf("prompt_variants: add target_profile: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_prompt_variants_workspace_role_target_created ON prompt_variants(workspace_id, agent_role, target_profile, created_at DESC, id DESC)`); err != nil {
		return fmt.Errorf("prompt_variants: add target_profile index: %w", err)
	}
	return nil
}

func generatePromptVariantID() string {
	return ulid.Make().String()
}

func (s *sqlPromptVariantStore) Save(ctx context.Context, variant PromptVariant) (PromptVariant, error) {
	if strings.TrimSpace(variant.WorkspaceID) == "" {
		return PromptVariant{}, fmt.Errorf("prompt_variants: workspace_id is required")
	}
	if strings.TrimSpace(variant.AgentRole) == "" {
		return PromptVariant{}, fmt.Errorf("prompt_variants: agent_role is required")
	}
	if strings.TrimSpace(variant.Prompt) == "" {
		return PromptVariant{}, fmt.Errorf("prompt_variants: prompt is required")
	}
	if strings.TrimSpace(variant.OriginalPrompt) == "" {
		return PromptVariant{}, fmt.Errorf("prompt_variants: original_prompt is required")
	}
	variant.TargetProfile = NormalizePromptTargetProfile(variant.TargetProfile)

	if variant.ID == "" {
		variant.ID = generatePromptVariantID()
	}
	if variant.CreatedAt.IsZero() {
		variant.CreatedAt = timeutil.NowUTC()
	}

	metadataJSON, err := sqlutil.FormatJSON(variant.Metadata)
	if err != nil {
		return PromptVariant{}, fmt.Errorf("prompt_variants: format metadata: %w", err)
	}
	metadataArg := any(metadataJSON)
	if metadataJSON == "" {
		metadataArg = nil
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO prompt_variants (
	id, workspace_id, agent_role, target_profile, mode, original_prompt, prompt,
	original_score, optimized_score, improvement, candidate_count, metadata_json, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		variant.ID,
		strings.TrimSpace(variant.WorkspaceID),
		strings.TrimSpace(variant.AgentRole),
		variant.TargetProfile,
		strings.TrimSpace(variant.Mode),
		variant.OriginalPrompt,
		variant.Prompt,
		variant.OriginalScore,
		variant.OptimizedScore,
		variant.Improvement,
		variant.CandidateCount,
		metadataArg,
		sqlutil.FormatTimestamp(variant.CreatedAt),
	)
	if err != nil {
		return PromptVariant{}, fmt.Errorf("prompt_variants: save: %w", err)
	}
	return variant, nil
}

func (s *sqlPromptVariantStore) Get(ctx context.Context, workspaceID, id string) (PromptVariant, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, agent_role, target_profile, mode, original_prompt, prompt,
       original_score, optimized_score, improvement, candidate_count, metadata_json, created_at
FROM prompt_variants
WHERE workspace_id = ? AND id = ?
`, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
	return scanPromptVariant(row)
}

func (s *sqlPromptVariantStore) List(ctx context.Context, workspaceID, agentRole string, limit int) ([]PromptVariant, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
SELECT id, workspace_id, agent_role, target_profile, mode, original_prompt, prompt,
       original_score, optimized_score, improvement, candidate_count, metadata_json, created_at
FROM prompt_variants
WHERE workspace_id = ?
`
	args := []any{strings.TrimSpace(workspaceID)}
	if strings.TrimSpace(agentRole) != "" {
		query += ` AND agent_role = ?`
		args = append(args, strings.TrimSpace(agentRole))
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("prompt_variants: list: %w", err)
	}
	defer rows.Close()

	var variants []PromptVariant
	for rows.Next() {
		variant, err := scanPromptVariant(rows)
		if err != nil {
			return nil, err
		}
		variants = append(variants, variant)
	}
	return variants, rows.Err()
}

func (s *sqlPromptVariantStore) ListByTargetProfile(ctx context.Context, workspaceID, agentRole, targetProfile string, limit int) ([]PromptVariant, error) {
	targetProfile = NormalizePromptTargetProfile(targetProfile)
	if targetProfile == "" {
		return s.List(ctx, workspaceID, agentRole, limit)
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, agent_role, target_profile, mode, original_prompt, prompt,
       original_score, optimized_score, improvement, candidate_count, metadata_json, created_at
FROM prompt_variants
WHERE workspace_id = ? AND agent_role = ? AND target_profile = ?
ORDER BY created_at DESC, id DESC LIMIT ?
`, strings.TrimSpace(workspaceID), strings.TrimSpace(agentRole), targetProfile, limit)
	if err != nil {
		return nil, fmt.Errorf("prompt_variants: list by target_profile: %w", err)
	}
	defer rows.Close()

	var variants []PromptVariant
	for rows.Next() {
		variant, err := scanPromptVariant(rows)
		if err != nil {
			return nil, err
		}
		variants = append(variants, variant)
	}
	return variants, rows.Err()
}

func (s *sqlPromptVariantStore) ResolveLatestCompatible(ctx context.Context, workspaceID, agentRole, targetProfile string) (PromptVariant, error) {
	targetProfile = NormalizePromptTargetProfile(targetProfile)
	if targetProfile != "" {
		variants, err := s.ListByTargetProfile(ctx, workspaceID, agentRole, targetProfile, 1)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return PromptVariant{}, err
		}
		if len(variants) > 0 {
			return variants[0], nil
		}
	}
	variants, err := s.ListByTargetProfile(ctx, workspaceID, agentRole, "generic", 1)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PromptVariant{}, err
	}
	if len(variants) > 0 {
		return variants[0], nil
	}
	return PromptVariant{}, sql.ErrNoRows
}

type promptVariantScanner interface {
	Scan(dest ...any) error
}

func scanPromptVariant(scanner promptVariantScanner) (PromptVariant, error) {
	var (
		variant      PromptVariant
		metadataJSON sql.NullString
		createdAt    string
	)

	if err := scanner.Scan(
		&variant.ID,
		&variant.WorkspaceID,
		&variant.AgentRole,
		&variant.TargetProfile,
		&variant.Mode,
		&variant.OriginalPrompt,
		&variant.Prompt,
		&variant.OriginalScore,
		&variant.OptimizedScore,
		&variant.Improvement,
		&variant.CandidateCount,
		&metadataJSON,
		&createdAt,
	); err != nil {
		return PromptVariant{}, err
	}

	if metadataJSON.Valid && strings.TrimSpace(metadataJSON.String) != "" {
		variant.Metadata = make(map[string]any)
		if err := json.Unmarshal([]byte(metadataJSON.String), &variant.Metadata); err != nil {
			return PromptVariant{}, fmt.Errorf("prompt_variants: decode metadata: %w", err)
		}
	}
	parsedCreatedAt, err := sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return PromptVariant{}, fmt.Errorf("prompt_variants: decode created_at: %w", err)
	}
	variant.CreatedAt = parsedCreatedAt
	variant.TargetProfile = NormalizePromptTargetProfile(variant.TargetProfile)

	return variant, nil
}
