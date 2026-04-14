package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// Store defines the persistence interface for agent metadata.
type Store interface {
	Close() error
	Create(ctx context.Context, a agent.Agent) error
	Get(ctx context.Context, id string) (agent.Agent, error)
	GetByNamespace(ctx context.Context, ns string) (agent.Agent, error)
	GetBySlug(ctx context.Context, slug string) (agent.Agent, error)
	Resolve(ctx context.Context, ref string) (agent.Agent, error) // Resolve slug, name, or ID
	List(ctx context.Context, limit int) ([]agent.Agent, error)
	ListByParent(ctx context.Context, parentID string, limit int) ([]agent.Agent, error)
	UpdateIdentity(ctx context.Context, id, name, slug string) error
	UpdatePrompt(ctx context.Context, id, prompt string) error
	UpdateState(ctx context.Context, id string, state agent.State) error
	UpdateHeartbeat(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	Trash(ctx context.Context, id string) error                                // Soft delete (only stopped agents)
	UpdateConversationID(ctx context.Context, id, conversationID string) error // Link agent to conversation
	UpdateMemoryScope(ctx context.Context, id string, scope agent.MemoryScope) error
	UpdateMemoryRetention(ctx context.Context, id string, retention agent.MemoryRetention) error
	UpdateTerminalBinding(ctx context.Context, id string, binding agent.TerminalBinding) error
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

type rowScanner interface {
	Scan(dest ...any) error
}

const agentSelectColumns = `
		id, parent_id, ns, workspace_root, workspace_source, name, slug, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at,
		llm_provider, llm_model, llm_api_key, llm_base_url, llm_auth_mode, llm_auth_header, llm_auth_prefix, exec_mode, execution_layer,
		max_iterations, max_auto_turns, think_interval, conversation_id, memory_scope, memory_retention, sandbox_provider, sandbox_id, repo_url, repo_ref, terminal_binding`

// Open opens the agents store rooted at the given storage root directory.
// The database driver is selected via the dbdriver env var conventions (e.g., AGENTCTL_AGENTS_DB_DRIVER).
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "agents.db")
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "AGENTS", filepath.Base(dbPath), migrate)
	if err != nil {
		return nil, fmt.Errorf("agents: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *sqlStore) Create(ctx context.Context, a agent.Agent) error {
	// Marshal policy to JSON
	policyJSON, err := json.Marshal(a.Policy)
	if err != nil {
		return fmt.Errorf("agents: marshal policy: %w", err)
	}

	// Marshal skills_allow to JSON
	skillsJSON, err := json.Marshal(a.SkillsAllow)
	if err != nil {
		return fmt.Errorf("agents: marshal skills_allow: %w", err)
	}

	// Convert empty strings to NULL for optional unique fields
	var slugVal interface{}
	if a.Slug != "" {
		slugVal = a.Slug
	}
	memoryScope := agent.NormalizeMemoryScope(a.MemoryScope)
	memoryRetention := a.MemoryRetention
	if strings.TrimSpace(string(memoryRetention)) == "" {
		memoryRetention = agent.DefaultMemoryRetentionForScope(memoryScope)
	} else {
		memoryRetention = agent.NormalizeMemoryRetention(memoryRetention)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agents (
			id, parent_id, ns, workspace_root, workspace_source, name, slug, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at,
			llm_provider, llm_model, llm_api_key, llm_base_url, llm_auth_mode, llm_auth_header, llm_auth_prefix, exec_mode, execution_layer,
			max_iterations, max_auto_turns, think_interval, memory_scope, memory_retention, sandbox_provider, sandbox_id, repo_url, repo_ref, terminal_binding
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21, $22, $23, $24,
			$25, $26, $27, $28, $29, $30, $31, $32, $33, $34
		)`,
		a.ID, a.ParentID, a.Namespace, a.WorkspaceRoot, a.WorkspaceSource, a.Name, slugVal, a.Role, a.Prompt, string(skillsJSON), string(policyJSON), a.ShareBB, a.State,
		sqlutil.FormatTimestamp(a.CreatedAt), sqlutil.FormatTimestamp(a.HeartbeatAt),
		a.LLMProvider, a.LLMModel, a.LLMAPIKey, a.LLMBaseURL, a.LLMAuthMode, a.LLMAuthHeader, a.LLMAuthPrefix,
		string(a.ExecMode), string(agent.NormalizeExecutionLayer(a.ExecutionLayer)), a.MaxIterations, a.MaxAutoTurns, a.ThinkInterval, string(memoryScope), string(memoryRetention),
		a.SandboxProvider, a.SandboxID, a.RepoURL, a.RepoRef, marshalTerminalBinding(a.TerminalBinding))
	if err != nil {
		return fmt.Errorf("agents: create: %w", err)
	}
	return nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (agent.Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+agentSelectColumns+` FROM agents WHERE id = $1 AND deleted_at IS NULL`, id)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Agent{}, ErrNotFound
		}
		return agent.Agent{}, fmt.Errorf("agents: get: %w", err)
	}
	return a, nil
}

func (s *sqlStore) GetByNamespace(ctx context.Context, ns string) (agent.Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+agentSelectColumns+`
		FROM agents
		WHERE ns = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, ns)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Agent{}, ErrNotFound
		}
		return agent.Agent{}, fmt.Errorf("agents: get by ns: %w", err)
	}
	return a, nil
}

func (s *sqlStore) GetBySlug(ctx context.Context, slug string) (agent.Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+agentSelectColumns+` FROM agents WHERE slug = $1 AND deleted_at IS NULL`, slug)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Agent{}, ErrNotFound
		}
		return agent.Agent{}, fmt.Errorf("agents: get by slug: %w", err)
	}
	return a, nil
}

// Resolve looks up an agent by slug, name, or ID (in that order).
func (s *sqlStore) Resolve(ctx context.Context, ref string) (agent.Agent, error) {
	// Try slug first (most common for human-friendly references)
	a, err := s.GetBySlug(ctx, ref)
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return agent.Agent{}, err
	}

	// Try by name (case-insensitive)
	row := s.db.QueryRowContext(ctx, `SELECT `+agentSelectColumns+` FROM agents WHERE LOWER(name) = LOWER($1) AND deleted_at IS NULL LIMIT 1`, ref)
	a, scanErr := scanAgent(row)
	if scanErr == nil {
		return a, nil
	}
	if !errors.Is(scanErr, sql.ErrNoRows) {
		return agent.Agent{}, fmt.Errorf("agents: resolve by name: %w", scanErr)
	}

	// Finally try by ID
	return s.Get(ctx, ref)
}

func (s *sqlStore) List(ctx context.Context, limit int) ([]agent.Agent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentSelectColumns+`
		FROM agents
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("agents: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close agents list rows")
	}()

	var agents []agent.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

func (s *sqlStore) ListByParent(ctx context.Context, parentID string, limit int) ([]agent.Agent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentSelectColumns+`
		FROM agents
		WHERE parent_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2`, parentID, limit)
	if err != nil {
		return nil, fmt.Errorf("agents: list by parent: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close agents list by parent rows")
	}()

	var agents []agent.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

func (s *sqlStore) UpdateState(ctx context.Context, id string, state agent.State) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET state = $1 WHERE id = $2 AND deleted_at IS NULL`, state, id)
	if err != nil {
		return fmt.Errorf("agents: update state: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update state rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) UpdateHeartbeat(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET heartbeat_at = $1 WHERE id = $2 AND deleted_at IS NULL`, sqlutil.FormatTimestamp(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("agents: update heartbeat: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update heartbeat rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateIdentity always writes both name and slug; pass existing values to preserve.
// Empty strings clear the stored values (NULL).
func (s *sqlStore) UpdateIdentity(ctx context.Context, id, name, slug string) error {
	var nameVal interface{}
	if name != "" {
		nameVal = name
	}
	var slugVal interface{}
	if slug != "" {
		slugVal = slug
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET name = $1, slug = $2 WHERE id = $3 AND deleted_at IS NULL`, nameVal, slugVal, id)
	if err != nil {
		return fmt.Errorf("agents: update identity: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update identity rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) UpdatePrompt(ctx context.Context, id, prompt string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET prompt = $1 WHERE id = $2 AND deleted_at IS NULL`, strings.TrimSpace(prompt), id)
	if err != nil {
		return fmt.Errorf("agents: update prompt: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update prompt rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateConversationID links an agent to a companion conversation.
// Pass empty string to clear the link.
func (s *sqlStore) UpdateConversationID(ctx context.Context, id, conversationID string) error {
	var convVal interface{}
	if conversationID != "" {
		convVal = conversationID
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET conversation_id = $1 WHERE id = $2 AND deleted_at IS NULL`, convVal, id)
	if err != nil {
		return fmt.Errorf("agents: update conversation_id: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update conversation_id rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) UpdateMemoryScope(ctx context.Context, id string, scope agent.MemoryScope) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET memory_scope = $1 WHERE id = $2 AND deleted_at IS NULL`, string(agent.NormalizeMemoryScope(scope)), id)
	if err != nil {
		return fmt.Errorf("agents: update memory_scope: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update memory_scope rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) UpdateMemoryRetention(ctx context.Context, id string, retention agent.MemoryRetention) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET memory_retention = $1 WHERE id = $2 AND deleted_at IS NULL`, string(agent.NormalizeMemoryRetention(retention)), id)
	if err != nil {
		return fmt.Errorf("agents: update memory_retention: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update memory_retention rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) UpdateTerminalBinding(ctx context.Context, id string, binding agent.TerminalBinding) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET terminal_binding = $1 WHERE id = $2 AND deleted_at IS NULL`, marshalTerminalBinding(binding), id)
	if err != nil {
		return fmt.Errorf("agents: update terminal binding: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: update terminal binding rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("agents: delete: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: delete rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Trash soft-deletes an agent by setting deleted_at. Only stopped agents can be trashed.
func (s *sqlStore) Trash(ctx context.Context, id string) error {
	// Atomic update: only trash if agent exists, is stopped, and not already trashed
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET deleted_at = $1
		WHERE id = $2 AND state = $3 AND deleted_at IS NULL`,
		sqlutil.FormatTimestamp(time.Now().UTC()), id, string(agent.StateStopped))
	if err != nil {
		return fmt.Errorf("agents: trash: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("agents: trash rows affected: %w", err)
	}
	if rows == 0 {
		// Check why: agent not found vs not stopped
		var state string
		err := s.db.QueryRowContext(ctx, `SELECT state FROM agents WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&state)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("agents: trash check state: %w", err)
		}
		// Agent exists but wasn't stopped
		return ErrNotStopped
	}
	return nil
}

// ErrNotStopped indicates the agent must be stopped before it can be trashed.
var ErrNotStopped = errors.New("agent: must be stopped to trash")

// migrate creates or updates the agents schema in db to the current layout.
// It ensures the agents table and core indexes exist, attempts to add newer
// columns required by newer versions (ignoring errors if those columns already
// exist), and creates slug indexes including a unique partial index for
// non-null slugs. It returns an error if applying the initial DDL fails.
// MigrateSchema runs the agents store DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS agents (
	id           TEXT PRIMARY KEY,
	parent_id    TEXT,
	ns           TEXT NOT NULL,
	workspace_root TEXT,
	workspace_source TEXT,
	name         TEXT,
	slug         TEXT UNIQUE,
	role         TEXT,
	prompt       TEXT,
	skills_allow TEXT NOT NULL,
	policy       TEXT NOT NULL,
	share_bb     TEXT NOT NULL CHECK (share_bb IN ('all','scoped','none')),
	state        TEXT NOT NULL CHECK (state IN ('starting','running','stopped','error')),
	created_at   TEXT NOT NULL,
	heartbeat_at TEXT,
	llm_provider TEXT,
	llm_model    TEXT,
	llm_api_key  TEXT,
	llm_base_url TEXT,
	llm_auth_mode TEXT,
	llm_auth_header TEXT,
	llm_auth_prefix TEXT,
	execution_layer TEXT,
	memory_scope TEXT,
	memory_retention TEXT,
	sandbox_provider TEXT,
	sandbox_id TEXT,
	repo_url TEXT,
	repo_ref TEXT,
	terminal_binding TEXT
);
CREATE INDEX IF NOT EXISTS idx_agents_ns ON agents(ns);
CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_id);
CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("agents: migrate: %w", err)
	}

	// Migration for existing databases: add columns if they don't exist
	// Note: SQLite doesn't support UNIQUE constraint in ALTER TABLE, so we add columns first
	// then create unique indexes separately
	alterStmts := []string{
		"ALTER TABLE agents ADD COLUMN llm_provider TEXT",
		"ALTER TABLE agents ADD COLUMN llm_model TEXT",
		"ALTER TABLE agents ADD COLUMN llm_api_key TEXT",
		"ALTER TABLE agents ADD COLUMN llm_base_url TEXT",
		"ALTER TABLE agents ADD COLUMN llm_auth_mode TEXT",
		"ALTER TABLE agents ADD COLUMN llm_auth_header TEXT",
		"ALTER TABLE agents ADD COLUMN llm_auth_prefix TEXT",
		"ALTER TABLE agents ADD COLUMN name TEXT",
		"ALTER TABLE agents ADD COLUMN slug TEXT",
		"ALTER TABLE agents ADD COLUMN exec_mode TEXT",
		"ALTER TABLE agents ADD COLUMN execution_layer TEXT",
		"ALTER TABLE agents ADD COLUMN max_iterations INTEGER",
		"ALTER TABLE agents ADD COLUMN max_auto_turns INTEGER",
		"ALTER TABLE agents ADD COLUMN think_interval INTEGER",
		"ALTER TABLE agents ADD COLUMN deleted_at TEXT",      // Soft delete timestamp
		"ALTER TABLE agents ADD COLUMN conversation_id TEXT", // Linked companion conversation ID
		"ALTER TABLE agents ADD COLUMN memory_scope TEXT",
		"ALTER TABLE agents ADD COLUMN memory_retention TEXT",
		"ALTER TABLE agents ADD COLUMN workspace_root TEXT",
		"ALTER TABLE agents ADD COLUMN workspace_source TEXT",
		"ALTER TABLE agents ADD COLUMN sandbox_provider TEXT",
		"ALTER TABLE agents ADD COLUMN sandbox_id TEXT",
		"ALTER TABLE agents ADD COLUMN repo_url TEXT",
		"ALTER TABLE agents ADD COLUMN repo_ref TEXT",
		"ALTER TABLE agents ADD COLUMN terminal_binding TEXT",
	}
	for _, stmt := range alterStmts {
		// SQLite will error if column already exists, which is fine
		_, err := db.ExecContext(ctx, stmt)
		errs.Ignore(err, "ALTER TABLE may fail if column exists")
	}

	// Create indexes (unique index for slug enforces uniqueness)
	_, err := db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_agents_slug ON agents(slug)")
	errs.Ignore(err, "CREATE INDEX may fail if index exists")
	_, err = db.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_slug_unique ON agents(slug) WHERE slug IS NOT NULL")
	errs.Ignore(err, "CREATE UNIQUE INDEX may fail if index exists")

	if err := migrateNamespaceUniqueness(ctx, db); err != nil {
		return err
	}

	return nil
}

func migrateNamespaceUniqueness(ctx context.Context, db *sql.DB) error {
	row := db.QueryRowContext(ctx, `
		SELECT sql
		FROM sqlite_master
		WHERE type = 'table' AND name = 'agents'`)
	var ddl sql.NullString
	if err := row.Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("agents: inspect schema for namespace uniqueness: %w", err)
	}
	if !ddl.Valid {
		return nil
	}

	schema := strings.ToLower(ddl.String)
	if !strings.Contains(schema, "ns") || !strings.Contains(schema, "unique") {
		return nil
	}
	if !strings.Contains(schema, "ns           text unique not null") &&
		!strings.Contains(schema, "ns text unique not null") &&
		!strings.Contains(schema, "ns\ttext unique not null") {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("agents: begin namespace uniqueness migration: %w", err)
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback agents namespace uniqueness migration")
	}()

	rebuildDDL := `
CREATE TABLE agents_new (
	id              TEXT PRIMARY KEY,
	parent_id       TEXT,
	ns              TEXT NOT NULL,
	workspace_root  TEXT,
	workspace_source TEXT,
	name            TEXT,
	slug            TEXT,
	role            TEXT,
	prompt          TEXT,
	skills_allow    TEXT NOT NULL,
	policy          TEXT NOT NULL,
	share_bb        TEXT NOT NULL CHECK (share_bb IN ('all','scoped','none')),
	state           TEXT NOT NULL CHECK (state IN ('starting','running','stopped','error')),
	created_at      TEXT NOT NULL,
	heartbeat_at    TEXT,
	llm_provider    TEXT,
	llm_model       TEXT,
	llm_api_key     TEXT,
	llm_base_url    TEXT,
	llm_auth_mode   TEXT,
	llm_auth_header TEXT,
	llm_auth_prefix TEXT,
	exec_mode       TEXT,
	execution_layer TEXT,
	max_iterations  INTEGER,
	max_auto_turns  INTEGER,
	think_interval  INTEGER,
	deleted_at      TEXT,
	conversation_id TEXT,
	memory_scope    TEXT,
	memory_retention TEXT,
	sandbox_provider TEXT,
	sandbox_id      TEXT,
	repo_url        TEXT,
	repo_ref        TEXT,
	terminal_binding TEXT
);
		INSERT INTO agents_new (
	id, parent_id, ns, workspace_root, workspace_source, name, slug, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at,
	llm_provider, llm_model, llm_api_key, llm_base_url, llm_auth_mode, llm_auth_header, llm_auth_prefix, exec_mode, execution_layer, max_iterations, max_auto_turns, think_interval, deleted_at, conversation_id, memory_scope, memory_retention, sandbox_provider, sandbox_id, repo_url, repo_ref, terminal_binding
)
SELECT
	id, parent_id, ns, workspace_root, workspace_source, name, slug, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at,
	llm_provider, llm_model, llm_api_key, llm_base_url, llm_auth_mode, llm_auth_header, llm_auth_prefix, exec_mode, execution_layer, max_iterations, max_auto_turns, think_interval, deleted_at, conversation_id, memory_scope, memory_retention, sandbox_provider, sandbox_id, repo_url, repo_ref, terminal_binding
FROM agents;
DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;
CREATE INDEX idx_agents_ns ON agents(ns);
CREATE INDEX idx_agents_parent ON agents(parent_id);
CREATE INDEX idx_agents_state ON agents(state);
CREATE INDEX idx_agents_slug ON agents(slug);
CREATE UNIQUE INDEX idx_agents_slug_unique ON agents(slug) WHERE slug IS NOT NULL;
`
	if _, err := tx.ExecContext(ctx, rebuildDDL); err != nil {
		return fmt.Errorf("agents: rebuild schema without namespace uniqueness: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("agents: commit namespace uniqueness migration: %w", err)
	}
	return nil
}

func scanAgent(scanner rowScanner) (agent.Agent, error) {
	var a agent.Agent
	var skillsJSON, policyJSON string
	var created string
	var parentID, workspaceRoot, workspaceSource, name, slug, role, prompt, heartbeat sql.NullString
	var llmProvider, llmModel, llmAPIKey sql.NullString
	var llmBaseURL, llmAuthMode, llmAuthHeader, llmAuthPrefix sql.NullString
	var execMode, executionLayer sql.NullString
	var maxIterations, maxAutoTurns, thinkInterval sql.NullInt64
	var conversationID, memoryScope, memoryRetention, sandboxProvider, sandboxID, repoURL, repoRef, terminalBinding sql.NullString
	if err := scanner.Scan(&a.ID, &parentID, &a.Namespace, &workspaceRoot, &workspaceSource, &name, &slug, &role, &prompt, &skillsJSON, &policyJSON, &a.ShareBB, &a.State, &created, &heartbeat, &llmProvider, &llmModel, &llmAPIKey, &llmBaseURL, &llmAuthMode, &llmAuthHeader, &llmAuthPrefix, &execMode, &executionLayer, &maxIterations, &maxAutoTurns, &thinkInterval, &conversationID, &memoryScope, &memoryRetention, &sandboxProvider, &sandboxID, &repoURL, &repoRef, &terminalBinding); err != nil {
		return agent.Agent{}, fmt.Errorf("agents: scan: %w", err)
	}

	a.ParentID = parentID.String
	a.WorkspaceRoot = workspaceRoot.String
	a.WorkspaceSource = workspaceSource.String
	a.Name = name.String
	a.Slug = slug.String
	a.Role = role.String
	a.Prompt = prompt.String

	// Parse skills_allow
	if err := json.Unmarshal([]byte(skillsJSON), &a.SkillsAllow); err != nil {
		return agent.Agent{}, fmt.Errorf("agents: unmarshal skills_allow: %w", err)
	}

	// Parse policy
	if err := json.Unmarshal([]byte(policyJSON), &a.Policy); err != nil {
		return agent.Agent{}, fmt.Errorf("agents: unmarshal policy: %w", err)
	}

	// Parse timestamps
	var err error
	a.CreatedAt, err = sqlutil.ScanTimestamp(created)
	if err != nil {
		return agent.Agent{}, fmt.Errorf("agents: scan created_at: %w", err)
	}
	if heartbeat.Valid && heartbeat.String != "" {
		a.HeartbeatAt, err = sqlutil.ScanTimestamp(heartbeat.String)
		if err != nil {
			return agent.Agent{}, fmt.Errorf("agents: scan heartbeat_at: %w", err)
		}
	}

	// Set LLM fields
	a.LLMProvider = llmProvider.String
	a.LLMModel = llmModel.String
	a.LLMAPIKey = llmAPIKey.String
	a.LLMBaseURL = llmBaseURL.String
	a.LLMAuthMode = llmAuthMode.String
	a.LLMAuthHeader = llmAuthHeader.String
	a.LLMAuthPrefix = llmAuthPrefix.String

	// Set execution mode fields
	a.ExecMode = agent.ExecutionMode(execMode.String)
	a.ExecutionLayer = agent.NormalizeExecutionLayer(agent.ExecutionLayer(executionLayer.String))
	a.MaxIterations = int(maxIterations.Int64)
	a.MaxAutoTurns = int(maxAutoTurns.Int64)
	a.ThinkInterval = int(thinkInterval.Int64)

	// Set conversation ID
	a.ConversationID = conversationID.String
	a.MemoryScope = agent.NormalizeMemoryScope(agent.MemoryScope(memoryScope.String))
	if memoryRetention.Valid && strings.TrimSpace(memoryRetention.String) != "" {
		a.MemoryRetention = agent.NormalizeMemoryRetention(agent.MemoryRetention(memoryRetention.String))
	} else {
		a.MemoryRetention = agent.DefaultMemoryRetentionForScope(a.MemoryScope)
	}
	a.SandboxProvider = sandboxProvider.String
	a.SandboxID = sandboxID.String
	a.RepoURL = repoURL.String
	a.RepoRef = repoRef.String
	a.TerminalBinding = unmarshalTerminalBinding(terminalBinding)

	return a, nil
}

func marshalTerminalBinding(binding agent.TerminalBinding) string {
	binding = agent.NormalizeTerminalBinding(binding)
	if binding == (agent.TerminalBinding{}) {
		return ""
	}
	data, err := json.Marshal(binding)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarshalTerminalBinding(raw sql.NullString) agent.TerminalBinding {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return agent.TerminalBinding{}
	}
	var binding agent.TerminalBinding
	if err := json.Unmarshal([]byte(raw.String), &binding); err != nil {
		return agent.TerminalBinding{}
	}
	return agent.NormalizeTerminalBinding(binding)
}

// ErrNotFound indicates the agent was not found.
var ErrNotFound = errors.New("agent: not found")
