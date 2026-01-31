package teams

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

type Store interface {
	Close() error
	UpsertTeam(ctx context.Context, team Team) (Team, error)
	GetTeam(ctx context.Context, workspaceID, teamID string) (Team, error)
	ListTeams(ctx context.Context, workspaceID string, limit int) ([]Team, error)
	AddMember(ctx context.Context, member TeamMember) error
	RemoveMember(ctx context.Context, workspaceID, teamID, actorID string) error
	ListMembers(ctx context.Context, workspaceID, teamID string, limit int) ([]TeamMember, error)
}

type Team struct {
	WorkspaceID  string
	TeamID       string
	Name         string
	Description  string
	PrimaryEpics []string
	Tags         []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type TeamMember struct {
	WorkspaceID string
	TeamID      string
	ActorID     string
	Role        string
	CreatedAt   time.Time
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}


// Open opens or creates the teams SQLite database in the provided root directory and returns a Store backed by it.
// It constructs the path root/teams.db, opens a shared SQLite database (applying migrations), and returns a Store
// that manages the database connection or an error if opening fails.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "teams.db")
	db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("teams: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}


func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}


// migrate creates the database schema for teams and team_members and their indexes.
 // It executes the required DDL against the provided DB and returns an error wrapping the underlying failure if execution does not succeed.
func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS teams (
	workspace_id   TEXT NOT NULL,
	team_id        TEXT NOT NULL,
	name           TEXT NOT NULL,
	description    TEXT,
	primary_epics  TEXT NOT NULL DEFAULT '[]',
	tags           TEXT NOT NULL DEFAULT '[]',
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL,
	PRIMARY KEY (workspace_id, team_id)
);
CREATE INDEX IF NOT EXISTS idx_teams_workspace ON teams(workspace_id);

CREATE TABLE IF NOT EXISTS team_members (
	workspace_id   TEXT NOT NULL,
	team_id        TEXT NOT NULL,
	actor_id       TEXT NOT NULL,
	role           TEXT,
	created_at     TEXT NOT NULL,
	PRIMARY KEY (workspace_id, team_id, actor_id),
	FOREIGN KEY (workspace_id, team_id) REFERENCES teams(workspace_id, team_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_team_members_workspace_team ON team_members(workspace_id, team_id);
CREATE INDEX IF NOT EXISTS idx_team_members_workspace_actor ON team_members(workspace_id, actor_id);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("teams: migrate: %w", err)
	}
	return nil
}

func (s *sqlStore) UpsertTeam(ctx context.Context, team Team) (Team, error) {
	team.WorkspaceID = strings.TrimSpace(team.WorkspaceID)
	team.TeamID = strings.TrimSpace(team.TeamID)
	team.Name = strings.TrimSpace(team.Name)
	team.Description = strings.TrimSpace(team.Description)

	if team.WorkspaceID == "" {
		return Team{}, fmt.Errorf("teams: workspace_id required")
	}
	if team.TeamID == "" {
		return Team{}, fmt.Errorf("teams: team_id required")
	}
	if team.Name == "" {
		team.Name = team.TeamID
	}
	if team.PrimaryEpics == nil {
		team.PrimaryEpics = []string{}
	}
	if team.Tags == nil {
		team.Tags = []string{}
	}

	primaryJSON, err := json.Marshal(team.PrimaryEpics)
	if err != nil {
		return Team{}, fmt.Errorf("teams: marshal primary_epics: %w", err)
	}
	tagsJSON, err := json.Marshal(team.Tags)
	if err != nil {
		return Team{}, fmt.Errorf("teams: marshal tags: %w", err)
	}

	now := timeutil.NowUTC()
	createdAt := team.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	createdAt = createdAt.UTC()
	updatedAt := now

	descArg := any(team.Description)
	if team.Description == "" {
		descArg = nil
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO teams (workspace_id, team_id, name, description, primary_epics, tags, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, team_id) DO UPDATE SET
	name = excluded.name,
	description = excluded.description,
	primary_epics = excluded.primary_epics,
	tags = excluded.tags,
	updated_at = excluded.updated_at
`,
		team.WorkspaceID, team.TeamID, team.Name, descArg, string(primaryJSON), string(tagsJSON),
		timeutil.FormatRFC3339Nano(createdAt), timeutil.FormatRFC3339Nano(updatedAt),
	)
	if err != nil {
		return Team{}, fmt.Errorf("teams: upsert: %w", err)
	}

	return s.GetTeam(ctx, team.WorkspaceID, team.TeamID)
}

func (s *sqlStore) GetTeam(ctx context.Context, workspaceID, teamID string) (Team, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	teamID = strings.TrimSpace(teamID)
	if workspaceID == "" {
		return Team{}, fmt.Errorf("teams: workspace_id required")
	}
	if teamID == "" {
		return Team{}, fmt.Errorf("teams: team_id required")
	}

	row := s.db.QueryRowContext(ctx, `
SELECT workspace_id, team_id, name, description, primary_epics, tags, created_at, updated_at
FROM teams
WHERE workspace_id = ? AND team_id = ?
`, workspaceID, teamID)

	team, err := scanTeam(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return Team{}, ErrNotFound
		}
		return Team{}, err
	}
	return team, nil
}

func (s *sqlStore) ListTeams(ctx context.Context, workspaceID string, limit int) ([]Team, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("teams: workspace_id required")
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT workspace_id, team_id, name, description, primary_epics, tags, created_at, updated_at
FROM teams
WHERE workspace_id = ?
ORDER BY team_id ASC
LIMIT ?
`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("teams: list teams: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	out := make([]Team, 0)
	for rows.Next() {
		t, err := scanTeamRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("teams: list teams rows: %w", err)
	}
	return out, nil
}

func (s *sqlStore) AddMember(ctx context.Context, member TeamMember) error {
	member.WorkspaceID = strings.TrimSpace(member.WorkspaceID)
	member.TeamID = strings.TrimSpace(member.TeamID)
	member.ActorID = strings.TrimSpace(member.ActorID)
	member.Role = strings.TrimSpace(member.Role)

	if member.WorkspaceID == "" {
		return fmt.Errorf("teams: workspace_id required")
	}
	if member.TeamID == "" {
		return fmt.Errorf("teams: team_id required")
	}
	if member.ActorID == "" {
		return fmt.Errorf("teams: actor_id required")
	}

	now := timeutil.NowUTC()
	createdAt := member.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	createdAt = createdAt.UTC()

	roleArg := any(member.Role)
	if member.Role == "" {
		roleArg = nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO team_members (workspace_id, team_id, actor_id, role, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, team_id, actor_id) DO UPDATE SET
	role = excluded.role
`, member.WorkspaceID, member.TeamID, member.ActorID, roleArg, timeutil.FormatRFC3339Nano(createdAt))
	if err != nil {
		return fmt.Errorf("teams: add member: %w", err)
	}
	return nil
}

func (s *sqlStore) RemoveMember(ctx context.Context, workspaceID, teamID, actorID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	teamID = strings.TrimSpace(teamID)
	actorID = strings.TrimSpace(actorID)
	if workspaceID == "" {
		return fmt.Errorf("teams: workspace_id required")
	}
	if teamID == "" {
		return fmt.Errorf("teams: team_id required")
	}
	if actorID == "" {
		return fmt.Errorf("teams: actor_id required")
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM team_members WHERE workspace_id = ? AND team_id = ? AND actor_id = ?`, workspaceID, teamID, actorID)
	if err != nil {
		return fmt.Errorf("teams: remove member: %w", err)
	}
	return nil
}

func (s *sqlStore) ListMembers(ctx context.Context, workspaceID, teamID string, limit int) ([]TeamMember, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	teamID = strings.TrimSpace(teamID)
	if workspaceID == "" {
		return nil, fmt.Errorf("teams: workspace_id required")
	}
	if teamID == "" {
		return nil, fmt.Errorf("teams: team_id required")
	}
	if limit <= 0 {
		limit = 500
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT workspace_id, team_id, actor_id, role, created_at
FROM team_members
WHERE workspace_id = ? AND team_id = ?
ORDER BY actor_id ASC
LIMIT ?
`, workspaceID, teamID, limit)
	if err != nil {
		return nil, fmt.Errorf("teams: list members: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	out := make([]TeamMember, 0)
	for rows.Next() {
		m, err := scanTeamMemberRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("teams: list members rows: %w", err)
	}
	return out, nil
}

func scanTeam(row *sql.Row) (Team, error) {
	var t Team
	var desc sql.NullString
	var primaryJSON, tagsJSON string
	var createdAtStr, updatedAtStr string
	if err := row.Scan(&t.WorkspaceID, &t.TeamID, &t.Name, &desc, &primaryJSON, &tagsJSON, &createdAtStr, &updatedAtStr); err != nil {
		return Team{}, fmt.Errorf("teams: scan team: %w", err)
	}
	if desc.Valid {
		t.Description = desc.String
	}

	primary, err := parseJSONStringSlice(primaryJSON)
	if err != nil {
		return Team{}, fmt.Errorf("teams: decode primary_epics: %w", err)
	}
	tags, err := parseJSONStringSlice(tagsJSON)
	if err != nil {
		return Team{}, fmt.Errorf("teams: decode tags: %w", err)
	}
	t.PrimaryEpics = primary
	t.Tags = tags

	createdAt, err := timeutil.ParseRFC3339Nano(createdAtStr)
	if err != nil {
		return Team{}, fmt.Errorf("teams: parse created_at: %w", err)
	}
	updatedAt, err := timeutil.ParseRFC3339Nano(updatedAtStr)
	if err != nil {
		return Team{}, fmt.Errorf("teams: parse updated_at: %w", err)
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return t, nil
}

func scanTeamRow(rows *sql.Rows) (Team, error) {
	var t Team
	var desc sql.NullString
	var primaryJSON, tagsJSON string
	var createdAtStr, updatedAtStr string
	if err := rows.Scan(&t.WorkspaceID, &t.TeamID, &t.Name, &desc, &primaryJSON, &tagsJSON, &createdAtStr, &updatedAtStr); err != nil {
		return Team{}, fmt.Errorf("teams: scan team row: %w", err)
	}
	if desc.Valid {
		t.Description = desc.String
	}

	primary, err := parseJSONStringSlice(primaryJSON)
	if err != nil {
		return Team{}, fmt.Errorf("teams: decode primary_epics: %w", err)
	}
	tags, err := parseJSONStringSlice(tagsJSON)
	if err != nil {
		return Team{}, fmt.Errorf("teams: decode tags: %w", err)
	}
	t.PrimaryEpics = primary
	t.Tags = tags

	createdAt, err := timeutil.ParseRFC3339Nano(createdAtStr)
	if err != nil {
		return Team{}, fmt.Errorf("teams: parse created_at: %w", err)
	}
	updatedAt, err := timeutil.ParseRFC3339Nano(updatedAtStr)
	if err != nil {
		return Team{}, fmt.Errorf("teams: parse updated_at: %w", err)
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return t, nil
}

func scanTeamMemberRow(rows *sql.Rows) (TeamMember, error) {
	var m TeamMember
	var role sql.NullString
	var createdAtStr string
	if err := rows.Scan(&m.WorkspaceID, &m.TeamID, &m.ActorID, &role, &createdAtStr); err != nil {
		return TeamMember{}, fmt.Errorf("teams: scan member row: %w", err)
	}
	if role.Valid {
		m.Role = role.String
	}
	createdAt, err := timeutil.ParseRFC3339Nano(createdAtStr)
	if err != nil {
		return TeamMember{}, fmt.Errorf("teams: parse member created_at: %w", err)
	}
	m.CreatedAt = createdAt
	return m, nil
}

func parseJSONStringSlice(src string) ([]string, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(src), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

var ErrNotFound = fmt.Errorf("teams: not found")