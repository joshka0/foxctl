// Package agents implements SQLite-backed persistence for agent metadata and lifecycle.
package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
)

// Store defines the persistence interface for agent metadata.
type Store interface {
	Close() error
	Create(ctx context.Context, a agent.Agent) error
	Get(ctx context.Context, id string) (agent.Agent, error)
	GetByNamespace(ctx context.Context, ns string) (agent.Agent, error)
	List(ctx context.Context, limit int) ([]agent.Agent, error)
	ListByParent(ctx context.Context, parentID string, limit int) ([]agent.Agent, error)
	UpdateState(ctx context.Context, id string, state agent.State) error
	UpdateHeartbeat(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

type sqlStore struct {
	db *sql.DB
}

// Open initializes the agent store rooted at the provided path.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "agents.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("agents: open db: %w", err)
	}
	return &sqlStore{db: db}, nil
}

func (s *sqlStore) Close() error {
	return s.db.Close()
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

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agents (id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ParentID, a.Namespace, a.Role, a.Prompt, string(skillsJSON), string(policyJSON), a.ShareBB, a.State,
		sqlutil.FormatTimestamp(a.CreatedAt), sqlutil.FormatTimestamp(a.HeartbeatAt))
	if err != nil {
		return fmt.Errorf("agents: create: %w", err)
	}
	return nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (agent.Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at
		FROM agents WHERE id = ?`, id)

	var a agent.Agent
	var skillsJSON, policyJSON string
	var created, heartbeat string
	if err := row.Scan(&a.ID, &a.ParentID, &a.Namespace, &a.Role, &a.Prompt, &skillsJSON, &policyJSON, &a.ShareBB, &a.State, &created, &heartbeat); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Agent{}, ErrNotFound
		}
		return agent.Agent{}, fmt.Errorf("agents: get: %w", err)
	}

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
	if heartbeat != "" {
		a.HeartbeatAt, err = sqlutil.ScanTimestamp(heartbeat)
		if err != nil {
			return agent.Agent{}, fmt.Errorf("agents: scan heartbeat_at: %w", err)
		}
	}

	return a, nil
}

func (s *sqlStore) GetByNamespace(ctx context.Context, ns string) (agent.Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at
		FROM agents WHERE ns = ?`, ns)

	var a agent.Agent
	var skillsJSON, policyJSON string
	var created, heartbeat string
	if err := row.Scan(&a.ID, &a.ParentID, &a.Namespace, &a.Role, &a.Prompt, &skillsJSON, &policyJSON, &a.ShareBB, &a.State, &created, &heartbeat); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Agent{}, ErrNotFound
		}
		return agent.Agent{}, fmt.Errorf("agents: get by ns: %w", err)
	}

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
	if heartbeat != "" {
		a.HeartbeatAt, err = sqlutil.ScanTimestamp(heartbeat)
		if err != nil {
			return agent.Agent{}, fmt.Errorf("agents: scan heartbeat_at: %w", err)
		}
	}

	return a, nil
}

func (s *sqlStore) List(ctx context.Context, limit int) ([]agent.Agent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at
		FROM agents
		ORDER BY created_at DESC
		LIMIT ?`, limit)
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at
		FROM agents
		WHERE parent_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, parentID, limit)
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
		UPDATE agents SET state = ? WHERE id = ?`, state, id)
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
		UPDATE agents SET heartbeat_at = ? WHERE id = ?`, sqlutil.FormatTimestamp(time.Now().UTC()), id)
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

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
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

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS agents (
	id           TEXT PRIMARY KEY,
	parent_id    TEXT,
	ns           TEXT UNIQUE NOT NULL,
	role         TEXT,
	prompt       TEXT,
	skills_allow TEXT NOT NULL,
	policy       TEXT NOT NULL,
	share_bb     TEXT NOT NULL CHECK (share_bb IN ('all','scoped','none')),
	state        TEXT NOT NULL CHECK (state IN ('starting','running','stopped','error')),
	created_at   TEXT NOT NULL,
	heartbeat_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_agents_ns ON agents(ns);
CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_id);
CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("agents: migrate: %w", err)
	}
	return nil
}

func scanAgent(rows *sql.Rows) (agent.Agent, error) {
	var a agent.Agent
	var skillsJSON, policyJSON string
	var created, heartbeat string
	if err := rows.Scan(&a.ID, &a.ParentID, &a.Namespace, &a.Role, &a.Prompt, &skillsJSON, &policyJSON, &a.ShareBB, &a.State, &created, &heartbeat); err != nil {
		return agent.Agent{}, fmt.Errorf("agents: scan: %w", err)
	}

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
	if heartbeat != "" {
		a.HeartbeatAt, err = sqlutil.ScanTimestamp(heartbeat)
		if err != nil {
			return agent.Agent{}, fmt.Errorf("agents: scan heartbeat_at: %w", err)
		}
	}

	return a, nil
}

// ErrNotFound indicates the agent was not found.
var ErrNotFound = errors.New("agent: not found")
