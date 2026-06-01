package conversationsettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// Settings contains per-conversation overrides for the web API layer.
//
// Fields are optional. Empty values mean "use default" unless explicitly set via Patch.
type Settings struct {
	ConversationID string `json:"conversation_id"`

	// ToolsAllow is an allowlist of engine tool names.
	// When non-empty, only these tools should be available/executable.
	ToolsAllow []string `json:"tools_allow,omitempty"`

	// LLMProvider and LLMModel are defaults for this conversation.
	LLMProvider string `json:"llm_provider,omitempty"`
	LLMModel    string `json:"llm_model,omitempty"`

	// ExecMode is a default execution mode (reactive|autonomous|proactive|tick|story).
	ExecMode string `json:"exec_mode,omitempty"`

	// StoryGatherModel and StoryDialogueModel are defaults for story mode.
	StoryGatherModel   string `json:"story_gather_model,omitempty"`
	StoryDialogueModel string `json:"story_dialogue_model,omitempty"`

	// PresenceEnabled controls multimodal presence generation.
	// Nil means "enabled by default".
	PresenceEnabled *bool `json:"presence_enabled,omitempty"`

	UpdatedAt string `json:"updated_at,omitempty"`
}

// IsPresenceEnabled returns true when presence is enabled or unset.
func (s *Settings) IsPresenceEnabled() bool {
	if s == nil {
		return true
	}
	if s.PresenceEnabled == nil {
		return true
	}
	return *s.PresenceEnabled
}

// Patch represents a partial update. Nil fields are "no change".
type Patch struct {
	ToolsAllow *[]string `json:"tools_allow,omitempty"`

	LLMProvider *string `json:"llm_provider,omitempty"`
	LLMModel    *string `json:"llm_model,omitempty"`

	ExecMode *string `json:"exec_mode,omitempty"`

	StoryGatherModel   *string `json:"story_gather_model,omitempty"`
	StoryDialogueModel *string `json:"story_dialogue_model,omitempty"`
	PresenceEnabled    *bool   `json:"presence_enabled,omitempty"`
}

// Store persists per-conversation settings.
type Store interface {
	Close() error

	// Get returns stored settings. If none exist, ErrNotFound is returned.
	Get(ctx context.Context, conversationID string) (Settings, error)

	// Patch applies partial updates (upsert).
	Patch(ctx context.Context, conversationID string, patch Patch) (Settings, error)

	// Delete removes settings for a conversation.
	Delete(ctx context.Context, conversationID string) error
}

// ErrNotFound indicates settings were not found.
var ErrNotFound = errors.New("conversationsettings: not found")

// ErrInvalid indicates invalid input (validation) to the store API.
var ErrInvalid = errors.New("conversationsettings: invalid")

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open opens or creates the settings store at storageRoot/conversation_settings.db.
// The database driver is selected via the dbdriver env var conventions (e.g. FOXCTL_CONVERSATION_SETTINGS_DB_DRIVER).
func Open(ctx context.Context, storageRoot string) (Store, error) {
	dbPath := "conversation_settings.db"
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, "CONVERSATION_SETTINGS", dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("conversationsettings: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS conversation_settings (
	conversation_id TEXT PRIMARY KEY,

	tools_allow_json TEXT,

	llm_provider TEXT,
	llm_model TEXT,
	exec_mode TEXT,
	story_gather_model TEXT,
	story_dialogue_model TEXT,
	presence_enabled INTEGER,

	updated_at TEXT NOT NULL
);
`
	_, err := db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("conversationsettings: migrate: %w", err)
	}
	// Best-effort backfill for existing DBs.
	if _, err := db.ExecContext(ctx, `ALTER TABLE conversation_settings ADD COLUMN presence_enabled INTEGER`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column name") &&
			!strings.Contains(msg, "already exists") {
			return fmt.Errorf("conversationsettings: migrate add presence_enabled: %w", err)
		}
	}
	return nil
}

// querier abstracts *sql.DB and *sql.Tx for shared query logic.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *sqlStore) Get(ctx context.Context, conversationID string) (Settings, error) {
	return s.getWithQuerier(ctx, s.db, conversationID)
}

func (s *sqlStore) getWithQuerier(ctx context.Context, q querier, conversationID string) (Settings, error) {
	if conversationID == "" {
		return Settings{}, errors.New("conversationsettings: conversation_id is required")
	}

	var out Settings
	out.ConversationID = conversationID

	var toolsJSON sql.NullString
	var llmProvider, llmModel, execMode, gatherModel, dialogueModel sql.NullString
	var presenceEnabled sql.NullInt64
	var updatedAt string

	err := q.QueryRowContext(ctx, `
		SELECT tools_allow_json, llm_provider, llm_model, exec_mode, story_gather_model, story_dialogue_model, presence_enabled, updated_at
		FROM conversation_settings
		WHERE conversation_id = ?
	`, conversationID).Scan(&toolsJSON, &llmProvider, &llmModel, &execMode, &gatherModel, &dialogueModel, &presenceEnabled, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Settings{}, ErrNotFound
		}
		return Settings{}, fmt.Errorf("conversationsettings: get: %w", err)
	}

	if toolsJSON.Valid && strings.TrimSpace(toolsJSON.String) != "" {
		toolsAllow, err := parseToolsAllowJSON(toolsJSON.String)
		if err != nil {
			return Settings{}, err
		}
		out.ToolsAllow = toolsAllow
	}

	out.LLMProvider = llmProvider.String
	out.LLMModel = llmModel.String
	out.ExecMode = execMode.String
	out.StoryGatherModel = gatherModel.String
	out.StoryDialogueModel = dialogueModel.String
	if presenceEnabled.Valid {
		if presenceEnabled.Int64 != 0 && presenceEnabled.Int64 != 1 {
			return Settings{}, fmt.Errorf("%w: invalid presence_enabled %d", ErrInvalid, presenceEnabled.Int64)
		}
		v := presenceEnabled.Int64 != 0
		out.PresenceEnabled = &v
	}
	out.UpdatedAt = updatedAt

	if err := validate(out); err != nil {
		return Settings{}, fmt.Errorf("conversationsettings: invalid stored settings: %w", err)
	}

	return out, nil
}

func (s *sqlStore) Patch(ctx context.Context, conversationID string, patch Patch) (Settings, error) {
	if conversationID == "" {
		return Settings{}, errors.New("conversationsettings: conversation_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, fmt.Errorf("conversationsettings: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Start from existing settings (if any) — read within the transaction.
	current, err := s.getWithQuerier(ctx, tx, conversationID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return Settings{}, err
		}
		current = Settings{ConversationID: conversationID}
	}

	// Apply patch fields.
	if patch.ToolsAllow != nil {
		current.ToolsAllow = normalizeAllowlist(*patch.ToolsAllow)
	}
	if patch.LLMProvider != nil {
		current.LLMProvider = strings.TrimSpace(*patch.LLMProvider)
	}
	if patch.LLMModel != nil {
		current.LLMModel = strings.TrimSpace(*patch.LLMModel)
	}
	if patch.ExecMode != nil {
		current.ExecMode = strings.TrimSpace(*patch.ExecMode)
	}
	if patch.StoryGatherModel != nil {
		current.StoryGatherModel = strings.TrimSpace(*patch.StoryGatherModel)
	}
	if patch.StoryDialogueModel != nil {
		current.StoryDialogueModel = strings.TrimSpace(*patch.StoryDialogueModel)
	}
	if patch.PresenceEnabled != nil {
		v := *patch.PresenceEnabled
		current.PresenceEnabled = &v
	}

	if err := validate(current); err != nil {
		return Settings{}, err
	}

	updatedAt := sqlutil.FormatTimestamp(time.Now().UTC())
	current.UpdatedAt = updatedAt

	var toolsAllowJSON sql.NullString
	if len(current.ToolsAllow) > 0 {
		b, err := json.Marshal(current.ToolsAllow)
		if err != nil {
			return Settings{}, fmt.Errorf("conversationsettings: marshal tools_allow: %w", err)
		}
		toolsAllowJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err = tx.ExecContext(
		ctx, `
		INSERT INTO conversation_settings (
			conversation_id,
			tools_allow_json,
			llm_provider,
			llm_model,
			exec_mode,
			story_gather_model,
			story_dialogue_model,
			presence_enabled,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			tools_allow_json = excluded.tools_allow_json,
			llm_provider = excluded.llm_provider,
			llm_model = excluded.llm_model,
			exec_mode = excluded.exec_mode,
			story_gather_model = excluded.story_gather_model,
			story_dialogue_model = excluded.story_dialogue_model,
			presence_enabled = excluded.presence_enabled,
			updated_at = excluded.updated_at
	`, conversationID,
		toolsAllowJSON,
		nullString(current.LLMProvider),
		nullString(current.LLMModel),
		nullString(current.ExecMode),
		nullString(current.StoryGatherModel),
		nullString(current.StoryDialogueModel),
		nullBoolInt(current.PresenceEnabled),
		updatedAt,
	)
	if err != nil {
		return Settings{}, fmt.Errorf("conversationsettings: patch: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Settings{}, fmt.Errorf("conversationsettings: commit: %w", err)
	}

	return current, nil
}

func (s *sqlStore) Delete(ctx context.Context, conversationID string) error {
	if conversationID == "" {
		return errors.New("conversationsettings: conversation_id is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversation_settings WHERE conversation_id = ?`, conversationID)
	if err != nil {
		return fmt.Errorf("conversationsettings: delete: %w", err)
	}
	return nil
}

func nullString(s string) sql.NullString {
	if strings.TrimSpace(s) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullBoolInt(v *bool) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	if *v {
		return sql.NullInt64{Int64: 1, Valid: true}
	}
	return sql.NullInt64{Int64: 0, Valid: true}
}

func normalizeAllowlist(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, entry := range in {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseToolsAllowJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if raw == "null" {
		return nil, fmt.Errorf("%w: tools_allow_json must be a JSON array", ErrInvalid)
	}
	var tools []string
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		return nil, fmt.Errorf("conversationsettings: parse tools_allow_json: %w", err)
	}
	return normalizeAllowlist(tools), nil
}

func validate(s Settings) error {
	if s.ConversationID == "" {
		return fmt.Errorf("%w: conversation_id is required", ErrInvalid)
	}
	if s.ExecMode != "" {
		switch s.ExecMode {
		case "reactive", "autonomous", "proactive", "tick", "story":
			// ok
		default:
			return fmt.Errorf("%w: invalid exec_mode %q", ErrInvalid, s.ExecMode)
		}
	}
	return nil
}
