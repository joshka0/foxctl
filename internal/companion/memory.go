package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/storage"
)

// ConversationMemory manages progressive context for companion conversations.
//
// Unlike code-focused ShortTermMemory, this is optimized for:
// - Short conversational turns (50-200 tokens)
// - Time-based decay (today vivid, yesterday summarized, last week distilled)
// - Relationship context (tone, topics, emotional continuity)
//
// Layers:
// - L0 (Vivid): Recent turns from today, kept in full
// - L1 (Recent): Yesterday's conversation, summarized
// - L2 (History): Older conversations, distilled to key facts/topics
type ConversationMemory struct {
	db          *sql.DB
	summarizer  ConversationSummarizer
	memoryStore storage.MemoryStore // Optional: for semantic search integration
	workspace   string              // Workspace for semantic search scoping
	embedder    *semantic.Embedder  // Optional: for generating embeddings on summaries
	config      MemoryConfig
	clock       skilltest.Clock
	idGenerator func() string
	idSeq       uint64
	mu          sync.RWMutex
}

// MemoryConfig configures conversation memory behavior.
type MemoryConfig struct {
	// L0: Vivid (today's turns)
	VividWindowHours int // Hours to keep turns vivid (default: 24)
	VividMaxTurns    int // Max vivid turns to include (default: 50)
	VividTokenBudget int // Token budget for vivid layer (default: 24000)

	// L1: Recent (yesterday-ish)
	RecentWindowDays  int // Days to keep as "recent" summaries (default: 7)
	RecentTokenBudget int // Token budget for recent summaries (default: 10000)

	// L2: History (older, distilled)
	HistoryTokenBudget int // Token budget for distilled history (default: 6000)

	// Total budget for conversation context
	TotalTokenBudget int // Total tokens for memory context (default: 40000)
}

// DefaultMemoryConfig returns sensible defaults for companion conversations.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		VividWindowHours:   24,    // Last 24 hours vivid
		VividMaxTurns:      50,    // Up to 50 recent turns
		VividTokenBudget:   24000, // 24K for vivid turns
		RecentWindowDays:   7,     // Last week as "recent"
		RecentTokenBudget:  10000, // 10K for recent summaries
		HistoryTokenBudget: 6000,  // 6K for distilled history
		TotalTokenBudget:   40000, // 40K total for memory
	}
}

// RoleplayMemoryConfig returns optimized defaults for roleplay/chat companion agents.
// Optimized for:
// - Extended conversations with rich context
// - Character consistency across turns
// - Natural dialogue flow without tool interruption
// - Long-term relationship memory
func RoleplayMemoryConfig() MemoryConfig {
	return MemoryConfig{
		VividWindowHours:   48,    // 2 days vivid (extended conversation window)
		VividMaxTurns:      100,   // More turns for active roleplay sessions
		VividTokenBudget:   30000, // 30K for vivid (roleplay needs context)
		RecentWindowDays:   14,    // 2 weeks of summaries for character continuity
		RecentTokenBudget:  12000, // More for relationship consistency
		HistoryTokenBudget: 8000,  // Relationship history matters more
		TotalTokenBudget:   50000, // ~50K total for rich context
	}
}

// ConversationTurn represents a single message in the conversation.
type ConversationTurn struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"` // user, assistant
	Content        string          `json:"content"`
	TokenCount     int             `json:"token_count"`
	CreatedAt      time.Time       `json:"created_at"`
	ToolCalls      json.RawMessage `json:"tool_calls,omitempty"` // JSON array of tool calls made during this turn
}

// DaySummary represents a summarized day of conversation.
type DaySummary struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Date           string    `json:"date"` // YYYY-MM-DD
	TurnCount      int       `json:"turn_count"`
	Summary        string    `json:"summary"`
	Topics         []string  `json:"topics"`
	Mood           string    `json:"mood,omitempty"` // Overall emotional tone
	KeyMoments     []string  `json:"key_moments,omitempty"`
	TokenCount     int       `json:"token_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// DistilledHistory represents the compressed long-term memory.
type DistilledHistory struct {
	ID               string    `json:"id"`
	ConversationID   string    `json:"conversation_id"`
	RelationshipNote string    `json:"relationship_note"` // How do we relate?
	RecurringTopics  []string  `json:"recurring_topics"`
	UserPreferences  []string  `json:"user_preferences"`
	SharedMemories   []string  `json:"shared_memories"` // Key moments we've shared
	LastDistilledAt  time.Time `json:"last_distilled_at"`
	TokenCount       int       `json:"token_count"`
}

// ConversationSummarizer creates summaries from turns.
type ConversationSummarizer interface {
	// SummarizeDay creates a day summary from turns
	SummarizeDay(ctx context.Context, turns []ConversationTurn) (*DaySummary, error)

	// DistillHistory compresses multiple day summaries into distilled history
	DistillHistory(ctx context.Context, existing *DistilledHistory, summaries []DaySummary) (*DistilledHistory, error)
}

// NewConversationMemory creates a new conversation memory store.
// NewConversationMemory initializes conversation memory storage and schema.
//
// Index:
// - Purpose: Create a conversation memory store with configured options
// - Flow: apply options → ensure schema → return memory
// - SideEffects: creates/updates SQLite schema
// - FailureModes: schema creation errors
// - Related: DefaultMemoryConfig, ConversationMemory.ensureSchema
// - Keywords: conversation_memory, schema, options, sqlite, summaries
func NewConversationMemory(db *sql.DB, opts ...MemoryOption) (*ConversationMemory, error) {
	m := &ConversationMemory{
		db:     db,
		config: DefaultMemoryConfig(),
		clock:  skilltest.RealClock{},
	}
	m.idGenerator = func() string {
		seq := atomic.AddUint64(&m.idSeq, 1)
		return fmt.Sprintf("%d-%d", m.clock.Now().UnixNano(), seq)
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.clock == nil {
		m.clock = skilltest.RealClock{}
	}
	if m.idGenerator == nil {
		m.idGenerator = func() string {
			seq := atomic.AddUint64(&m.idSeq, 1)
			return fmt.Sprintf("%d-%d", m.clock.Now().UnixNano(), seq)
		}
	}

	if err := m.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	return m, nil
}

// MemoryOption configures ConversationMemory.
type MemoryOption func(*ConversationMemory)

// WithMemoryConfig sets the memory configuration.
func WithMemoryConfig(cfg MemoryConfig) MemoryOption {
	return func(m *ConversationMemory) {
		m.config = cfg
	}
}

// WithSummarizer sets the conversation summarizer.
func WithSummarizer(s ConversationSummarizer) MemoryOption {
	return func(m *ConversationMemory) {
		m.summarizer = s
	}
}

// WithMemoryStore sets the memory store for semantic search integration.
// When set, day summaries and distilled history will also be stored as
// named memories, enabling semantic search across companion conversations.
func WithMemoryStore(store storage.MemoryStore, workspace string) MemoryOption {
	return func(m *ConversationMemory) {
		m.memoryStore = store
		m.workspace = workspace
	}
}

// WithEmbedder sets the embedder for generating semantic embeddings on summaries.
// When set along with a MemoryStore, companion summaries become vector-searchable.
// The embedder should use semantic.ScopeMemory for consistency with other memories.
func WithEmbedder(embedder *semantic.Embedder) MemoryOption {
	return func(m *ConversationMemory) {
		m.embedder = embedder
	}
}

// WithClock sets the clock used for time-based behavior (useful for deterministic tests).
func WithClock(clock skilltest.Clock) MemoryOption {
	return func(m *ConversationMemory) {
		if clock != nil {
			m.clock = clock
		}
	}
}

// WithIDGenerator sets the ID generator used for new memory rows (useful for deterministic tests).
func WithIDGenerator(gen func() string) MemoryOption {
	return func(m *ConversationMemory) {
		if gen != nil {
			m.idGenerator = gen
		}
	}
}

// MigrateSchema runs the companion memory DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	m := &ConversationMemory{db: db}
	return m.ensureSchema(ctx)
}

// ensureSchema creates the conversation memory tables.
func (m *ConversationMemory) ensureSchema(ctx context.Context) error {
	schema := `
		-- Conversation turns (L0 storage)
		CREATE TABLE IF NOT EXISTS companion_turns (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			token_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			tool_calls TEXT -- JSON array of tool calls made during this turn
		);
		CREATE INDEX IF NOT EXISTS idx_companion_turns_conv_time
		ON companion_turns(conversation_id, created_at DESC);

		-- Migration: Add tool_calls column if it doesn't exist
		-- SQLite doesn't have IF NOT EXISTS for ALTER, so we use a pragma check
		-- This is safe because SQLite ignores duplicate column additions silently with this pattern

		-- Day summaries (L1 storage)
		CREATE TABLE IF NOT EXISTS companion_day_summaries (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			date TEXT NOT NULL,
			turn_count INTEGER DEFAULT 0,
			summary TEXT NOT NULL,
			topics TEXT,
			mood TEXT,
			key_moments TEXT,
			token_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(conversation_id, date)
		);
		CREATE INDEX IF NOT EXISTS idx_companion_summaries_conv_date
		ON companion_day_summaries(conversation_id, date DESC);

		-- Distilled history (L2 storage)
		CREATE TABLE IF NOT EXISTS companion_history (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL UNIQUE,
			relationship_note TEXT,
			recurring_topics TEXT,
			user_preferences TEXT,
			shared_memories TEXT,
			token_count INTEGER DEFAULT 0,
			last_distilled_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Soft-deleted conversations tracking
		CREATE TABLE IF NOT EXISTS companion_deleted_conversations (
			conversation_id TEXT PRIMARY KEY,
			deleted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Custom conversation titles
		CREATE TABLE IF NOT EXISTS companion_conversation_titles (
			conversation_id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			agent_id TEXT,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Compression cursor tracking
		CREATE TABLE IF NOT EXISTS companion_memory_state (
			conversation_id TEXT PRIMARY KEY,
			last_summarized_date TEXT,
			last_distilled_date TEXT,
			total_turns INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Character definitions for presence system
		CREATE TABLE IF NOT EXISTS companion_characters (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			name TEXT NOT NULL,
			avatar_digest TEXT,
			voice_id TEXT,
			base_mood TEXT DEFAULT 'neutral',
			backstory TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(conversation_id, name)
		);
		CREATE INDEX IF NOT EXISTS idx_companion_characters_conv
		ON companion_characters(conversation_id);

		-- Emotion overlays per character
		CREATE TABLE IF NOT EXISTS companion_character_overlays (
			id TEXT PRIMARY KEY,
			character_id TEXT NOT NULL,
			emotion TEXT NOT NULL,
			overlay_digest TEXT NOT NULL,
			intensity_low_digest TEXT,
			intensity_high_digest TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(character_id, emotion)
		);
		CREATE INDEX IF NOT EXISTS idx_companion_overlays_char
		ON companion_character_overlays(character_id);

		-- Generated background cache
		CREATE TABLE IF NOT EXISTS companion_generated_backgrounds (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			prompt_hash TEXT NOT NULL,
			image_digest TEXT NOT NULL,
			emotion TEXT,
			scene TEXT,
			model TEXT NOT NULL,
			latency_ms INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			UNIQUE(conversation_id, prompt_hash)
		);
		CREATE INDEX IF NOT EXISTS idx_backgrounds_conv
		ON companion_generated_backgrounds(conversation_id);

		-- Generated voice cache
		CREATE TABLE IF NOT EXISTS companion_generated_voices (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			text_hash TEXT NOT NULL,
			voice_id TEXT NOT NULL,
			audio_digest TEXT NOT NULL,
			format TEXT DEFAULT 'mp3',
			duration_ms INTEGER,
			model TEXT NOT NULL,
			latency_ms INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			UNIQUE(conversation_id, text_hash, voice_id)
		);
		CREATE INDEX IF NOT EXISTS idx_voices_conv
		ON companion_generated_voices(conversation_id);

		-- Presence bundle cache (latest presence data per conversation)
		CREATE TABLE IF NOT EXISTS companion_presence_bundles (
			conversation_id TEXT PRIMARY KEY,
			emotion TEXT,
			intensity REAL,
			display_text TEXT,
			character_id TEXT,
			overlay_digest TEXT,
			background_digest TEXT,
			audio_digest TEXT,
			audio_duration_ms INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Unified event log (messages + tool calls/results)
		CREATE TABLE IF NOT EXISTS companion_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			turn_id TEXT,
			tool_name TEXT,
			tool_run_id TEXT,
			parent_tool_call_id INTEGER,
			payload_json TEXT,
			payload_ref TEXT,
			token_count INTEGER,
			content_hash TEXT,
			created_at TEXT NOT NULL,
			CHECK (
				(event_type IN ('tool_call', 'tool_result') AND payload_json IS NOT NULL)
				OR
				(event_type IN ('user_message', 'assistant_message') AND
				 payload_json IS NULL AND payload_ref IS NULL)
			)
		);
		CREATE INDEX IF NOT EXISTS idx_events_conv ON companion_events(conversation_id, id);
		CREATE INDEX IF NOT EXISTS idx_events_tool_run ON companion_events(conversation_id, tool_run_id)
			WHERE tool_run_id IS NOT NULL;

		-- Hard state entries (append-only, immutable)
		CREATE TABLE IF NOT EXISTS companion_hard_state_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			entry_type TEXT NOT NULL,
			key TEXT NOT NULL,
			value_json TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			source_event_id INTEGER NOT NULL,
			confidence REAL NOT NULL DEFAULT 0.8,
			metadata_json TEXT,
			supersedes INTEGER,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_hard_entries_conv
		ON companion_hard_state_entries(conversation_id, entry_type, key, status);
		CREATE INDEX IF NOT EXISTS idx_hard_entries_max
		ON companion_hard_state_entries(conversation_id, id DESC);

		-- Episodic narrative summaries (abstractive)
		CREATE TABLE IF NOT EXISTS companion_soft_episodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			episode_type TEXT NOT NULL,
			start_event_id INTEGER NOT NULL,
			end_event_id INTEGER NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			needs_summary INTEGER NOT NULL DEFAULT 0,
			assumption_ids TEXT NOT NULL DEFAULT '[]',
			token_count INTEGER DEFAULT 0,
			boundary_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			deleted_at TEXT,
			UNIQUE(conversation_id, boundary_hash)
		);
		CREATE INDEX IF NOT EXISTS idx_soft_episodes_conv
		ON companion_soft_episodes(conversation_id, end_event_id DESC);

		-- Extractive evidence snippets (canonical quotes)
		CREATE TABLE IF NOT EXISTS companion_evidence_snippets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			source_event_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			fact_text TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			confidence REAL NOT NULL DEFAULT 0.5,
			bucket TEXT NOT NULL DEFAULT 'default',
			ttl_days INTEGER,
			created_at TEXT NOT NULL,
			expires_at TEXT,
			UNIQUE(conversation_id, content_hash)
		);
		CREATE INDEX IF NOT EXISTS idx_evidence_conv
		ON companion_evidence_snippets(conversation_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_evidence_expires
		ON companion_evidence_snippets(expires_at)
		WHERE expires_at IS NOT NULL;

		CREATE VIRTUAL TABLE IF NOT EXISTS companion_evidence_fts USING fts5(
			conversation_id UNINDEXED,
			fact_text,
			content='companion_evidence_snippets',
			content_rowid='id'
		);
		CREATE TRIGGER IF NOT EXISTS companion_evidence_fts_insert
		AFTER INSERT ON companion_evidence_snippets
		BEGIN
			INSERT INTO companion_evidence_fts(rowid, conversation_id, fact_text)
			VALUES (new.id, new.conversation_id, new.fact_text);
		END;
		CREATE TRIGGER IF NOT EXISTS companion_evidence_fts_delete
		AFTER DELETE ON companion_evidence_snippets
		BEGIN
			INSERT INTO companion_evidence_fts(companion_evidence_fts, rowid, conversation_id, fact_text)
			VALUES ('delete', old.id, old.conversation_id, old.fact_text);
		END;

		-- Assumptions ledger (canonical source)
		CREATE TABLE IF NOT EXISTS companion_assumptions_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			assumption TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			reason TEXT,
			source_event_id INTEGER NOT NULL,
			confidence REAL NOT NULL DEFAULT 0.5,
			created_at TEXT NOT NULL,
			retracted_at TEXT,
			retracted_by_event_id INTEGER,
			retraction_reason TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_assumptions_conv
		ON companion_assumptions_ledger(conversation_id, status);

		-- Per-conversation mode tracking
		CREATE TABLE IF NOT EXISTS companion_memory_mode_state (
			conversation_id TEXT PRIMARY KEY,
			mode TEXT NOT NULL DEFAULT 'legacy',
			schema_version INTEGER NOT NULL DEFAULT 1,
			last_processed_event INTEGER NOT NULL DEFAULT 0,
			last_soft_event INTEGER NOT NULL DEFAULT 0,
			last_evidence_event INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);

		-- Open episode state (active episode tracking)
		CREATE TABLE IF NOT EXISTS companion_open_episode (
			conversation_id TEXT PRIMARY KEY,
			start_event_id INTEGER NOT NULL,
			episode_type TEXT NOT NULL DEFAULT 'exploration',
			event_count INTEGER NOT NULL DEFAULT 0,
			topic_sig TEXT,
			pending_seal_reason TEXT,
			updated_at TEXT NOT NULL
		);

		-- Active tool runs for open episodes
		CREATE TABLE IF NOT EXISTS companion_open_tool_runs (
			conversation_id TEXT NOT NULL,
			tool_run_id TEXT NOT NULL,
			start_event_id INTEGER NOT NULL,
			parent_call_event_id INTEGER,
			created_at TEXT NOT NULL,
			PRIMARY KEY (conversation_id, tool_run_id)
		);

		-- Staging queue for ambiguous extractions
		CREATE TABLE IF NOT EXISTS companion_extraction_staging (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			source_event_id INTEGER NOT NULL,
			proposed_entry_type TEXT NOT NULL,
			raw_text TEXT NOT NULL,
			reason TEXT NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			resolved_at TEXT,
			discarded_at TEXT,
			discard_reason TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_staging_pending
		ON companion_extraction_staging(conversation_id)
		WHERE resolved_at IS NULL AND discarded_at IS NULL;

		-- Materialized hard-state cache
		CREATE TABLE IF NOT EXISTS companion_hard_state_cache (
			conversation_id TEXT PRIMARY KEY,
			compact_json TEXT NOT NULL,
			last_entry_id INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);
	`

	_, err := m.db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}

	// Migration: Add tool_calls column to existing companion_turns table
	// SQLite doesn't support IF NOT EXISTS for ALTER TABLE, so we check first
	_, _ = m.db.ExecContext(ctx, `ALTER TABLE companion_turns ADD COLUMN tool_calls TEXT`)
	// Ignore error - column may already exist

	// Migration: Add agent_id column to companion_conversation_titles
	_, _ = m.db.ExecContext(ctx, `ALTER TABLE companion_conversation_titles ADD COLUMN agent_id TEXT`)
	// Ignore error - column may already exist

	// Migration: Fix malformed Go time.Time.String() timestamps in companion_turns.
	// Older versions stored created_at as e.g. "2026-01-24 19:15:42.503658 +0200 EET m=+102.170251751"
	// instead of the clean "2006-01-02 15:04:05.000000" UTC format. These break ORDER BY.
	if err := m.migrateTimestamps(ctx); err != nil {
		// Non-fatal: don't block startup
		_ = err
	}

	return nil
}

// migrateTimestamps fixes malformed Go time.Time.String() timestamps in companion_turns.
// These contain timezone names and monotonic clock readings that break SQLite ordering.
func (m *ConversationMemory) migrateTimestamps(ctx context.Context) error {
	// Select rows whose created_at contains a timezone offset (not clean UTC format).
	// The libsql driver auto-parses stored timestamps, so we read them as-is and check
	// in Go whether they need conversion to clean UTC format.
	rows, err := m.db.QueryContext(ctx, `SELECT id, created_at FROM companion_turns`)
	if err != nil {
		return err
	}
	defer rows.Close()

	// The libsql driver may auto-parse stored timestamps into RFC3339 format on read.
	// We detect any timestamp not already in clean "YYYY-MM-DD HH:MM:SS.ffffff" UTC format
	// and convert it.
	const cleanFormat = "2006-01-02 15:04:05.000000"
	type fix struct {
		id        string
		cleanedAt string
	}
	var fixes []fix
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		// Skip rows already in clean UTC format (26 chars, no timezone suffix).
		if len(raw) == 26 {
			if _, err := time.Parse(cleanFormat, raw); err == nil {
				continue
			}
		}
		// Try RFC3339 (libsql driver format): "2026-01-20T10:14:42.397221+02:00"
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			// Try Go's time.Time.String() format (raw SQLite): "2026-01-20 10:14:42.503658 +0200 EET m=+29.73"
			cleaned := raw
			if idx := strings.Index(raw, " m="); idx > 0 {
				cleaned = raw[:idx]
			}
			t, err = time.Parse("2006-01-02 15:04:05.999999 -0700 MST", cleaned)
			if err != nil {
				t, err = time.Parse("2006-01-02 15:04:05.999999 -0700", cleaned)
			}
		}
		if err != nil {
			continue
		}
		utcStr := t.UTC().Format(cleanFormat)
		if utcStr != raw {
			fixes = append(fixes, fix{id: id, cleanedAt: utcStr})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(fixes) == 0 {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `UPDATE companion_turns SET created_at = $1 WHERE id = $2`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range fixes {
		_, _ = stmt.ExecContext(ctx, f.cleanedAt, f.id)
	}

	return tx.Commit()
}

// AppendTurn adds a new turn to the conversation.
// AppendTurn stores a conversation turn and updates memory state.
//
// Index:
// - Purpose: Persist a conversation turn and update totals
// - Flow: normalize turn → insert turn → update memory state
// - SideEffects: writes to companion_turns and companion_memory_state
// - FailureModes: insert errors, state update errors
// - Related: ConversationMemory.GetContext
// - Keywords: append_turn, conversation_id, token_count, memory_state, tool_calls
func (m *ConversationMemory) AppendTurn(ctx context.Context, turn ConversationTurn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if turn.ID == "" {
		turn.ID = m.newID()
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = m.clock.Now().UTC()
	}
	turn.CreatedAt = turn.CreatedAt.UTC()
	if turn.TokenCount == 0 {
		turn.TokenCount = actormemory.EstimateTokens(turn.Content)
	}

	// Format timestamp consistently for SQLite text comparison
	createdAtStr := turn.CreatedAt.UTC().Format("2006-01-02 15:04:05.000000")
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_turns (id, conversation_id, role, content, token_count, created_at, tool_calls)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, turn.ID, turn.ConversationID, turn.Role, turn.Content, turn.TokenCount, createdAtStr, turn.ToolCalls)
	if err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}

	// Update state
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO companion_memory_state (conversation_id, total_turns, updated_at)
		VALUES ($1, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET
			total_turns = total_turns + 1,
			updated_at = CURRENT_TIMESTAMP
	`, turn.ConversationID)

	return err
}

// GetContext builds the memory context for an LLM prompt.
// Returns formatted text combining L0 (vivid) + L1 (recent) + L2 (history).
// GetContext builds layered context for a conversation.
//
// Index:
// - Purpose: Assemble vivid turns, summaries, and distilled history into context
// - Flow: load history → load summaries → load turns → apply budgets → format sections
// - SideEffects: reads conversation data
// - FailureModes: query errors yield partial context
// - Related: getDistilledHistory, getRecentSummaries, getVividTurns
// - Keywords: conversation_context, vivid_turns, summaries, history, token_budget
func (m *ConversationMemory) GetContext(ctx context.Context, conversationID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var historyText string
	var summaryText string
	var turnText string

	// L2: Distilled history (oldest context, most compressed)
	history, err := m.getDistilledHistory(ctx, conversationID)
	if err == nil && history != nil {
		historyText = trimToTokenBudget(m.formatHistory(history), m.config.HistoryTokenBudget, false)
	}

	// L1: Recent summaries (last week, summarized by day)
	summaries, err := m.getRecentSummaries(ctx, conversationID)
	if err == nil && len(summaries) > 0 {
		summaryText = trimToTokenBudget(m.formatSummaries(summaries), m.config.RecentTokenBudget, false)
	}

	// L0: Vivid turns (today's conversation, full content)
	turns, err := m.getVividTurns(ctx, conversationID)
	if err == nil && len(turns) > 0 {
		trimmedTurns := trimTurnsToTokenBudget(turns, m.config.VividTokenBudget)
		turnText = trimToTokenBudget(m.formatTurns(trimmedTurns), m.config.VividTokenBudget, true)
	}

	historyText, summaryText, turnText = applyTotalTokenBudget(historyText, summaryText, turnText, m.config.TotalTokenBudget)

	var parts []string
	if historyText != "" {
		parts = append(parts, "## Our History\n"+historyText)
	}
	if summaryText != "" {
		parts = append(parts, "## Recent Conversations\n"+summaryText)
	}
	if turnText != "" {
		parts = append(parts, "## Today's Conversation\n"+turnText)
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// getVividTurns retrieves recent turns within the vivid window.
func (m *ConversationMemory) getVividTurns(ctx context.Context, conversationID string) ([]ConversationTurn, error) {
	cutoff := m.clock.Now().UTC().Add(-time.Duration(m.config.VividWindowHours) * time.Hour)
	// Format as string matching the stored Go time.Time format for correct SQLite text comparison
	cutoffStr := cutoff.Format("2006-01-02 15:04:05")

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at
		FROM companion_turns
		WHERE conversation_id = $1 AND created_at >= $2
		ORDER BY created_at DESC
		LIMIT $3
	`, conversationID, cutoffStr, m.config.VividMaxTurns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []ConversationTurn
	for rows.Next() {
		var t ConversationTurn
		if err := rows.Scan(&t.ID, &t.ConversationID, &t.Role, &t.Content, &t.TokenCount, &t.CreatedAt); err != nil {
			// Log but continue - partial results are acceptable for memory context
			continue
		}
		turns = append(turns, t)
	}

	// Reverse to chronological order
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}

	return turns, rows.Err()
}

// getRecentSummaries retrieves day summaries from the recent window.
func (m *ConversationMemory) getRecentSummaries(ctx context.Context, conversationID string) ([]DaySummary, error) {
	now := m.clock.Now().UTC()
	cutoffDate := now.AddDate(0, 0, -m.config.RecentWindowDays).Format("2006-01-02")
	todayDate := now.Format("2006-01-02")

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, date, turn_count, summary, topics, mood, key_moments, token_count, created_at
		FROM companion_day_summaries
		WHERE conversation_id = $1 AND date >= $2 AND date <= $3
		ORDER BY date DESC
	`, conversationID, cutoffDate, todayDate) // Include today if a rolling summary exists.
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []DaySummary
	for rows.Next() {
		var s DaySummary
		var topics, keyMoments sql.NullString
		var mood sql.NullString
		if err := rows.Scan(&s.ID, &s.ConversationID, &s.Date, &s.TurnCount, &s.Summary,
			&topics, &mood, &keyMoments, &s.TokenCount, &s.CreatedAt); err != nil {
			continue
		}
		if topics.Valid {
			s.Topics = splitList(topics.String)
		}
		if mood.Valid {
			s.Mood = mood.String
		}
		if keyMoments.Valid {
			s.KeyMoments = splitList(keyMoments.String)
		}
		summaries = append(summaries, s)
	}

	return summaries, rows.Err()
}

// getDistilledHistory retrieves the distilled long-term history.
func (m *ConversationMemory) getDistilledHistory(ctx context.Context, conversationID string) (*DistilledHistory, error) {
	var h DistilledHistory
	var recurringTopics, userPrefs, sharedMemories sql.NullString
	var relNote sql.NullString
	var lastDistilled sql.NullTime

	err := m.db.QueryRowContext(ctx, `
		SELECT id, conversation_id, relationship_note, recurring_topics, user_preferences,
		       shared_memories, token_count, last_distilled_at
		FROM companion_history
		WHERE conversation_id = $1
	`, conversationID).Scan(
		&h.ID, &h.ConversationID, &relNote, &recurringTopics, &userPrefs,
		&sharedMemories, &h.TokenCount, &lastDistilled,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if relNote.Valid {
		h.RelationshipNote = relNote.String
	}
	if recurringTopics.Valid {
		h.RecurringTopics = splitList(recurringTopics.String)
	}
	if userPrefs.Valid {
		h.UserPreferences = splitList(userPrefs.String)
	}
	if sharedMemories.Valid {
		h.SharedMemories = splitList(sharedMemories.String)
	}
	if lastDistilled.Valid {
		h.LastDistilledAt = lastDistilled.Time
	}

	return &h, nil
}

// formatTurns formats vivid turns for the prompt.
func (m *ConversationMemory) formatTurns(turns []ConversationTurn) string {
	if len(turns) == 0 {
		return ""
	}

	var parts []string
	for _, t := range turns {
		timeStr := t.CreatedAt.Format("3:04 PM")
		role := "You"
		if t.Role == "user" {
			role = "Human"
		}
		parts = append(parts, fmt.Sprintf("[%s] %s: %s", timeStr, role, t.Content))
	}

	return strings.Join(parts, "\n\n")
}

// formatSummaries formats day summaries for the prompt.
func (m *ConversationMemory) formatSummaries(summaries []DaySummary) string {
	if len(summaries) == 0 {
		return ""
	}

	var parts []string
	for _, s := range summaries {
		// Parse date for friendly display
		date, _ := time.Parse("2006-01-02", s.Date)
		dateStr := date.Format("Monday, Jan 2")

		part := fmt.Sprintf("**%s** (%d messages)", dateStr, s.TurnCount)
		if s.Summary != "" {
			part += "\n" + s.Summary
		}
		if len(s.Topics) > 0 {
			part += "\nTopics: " + strings.Join(s.Topics, ", ")
		}
		if s.Mood != "" {
			part += "\nMood: " + s.Mood
		}

		parts = append(parts, part)
	}

	return strings.Join(parts, "\n\n")
}

// formatHistory formats distilled history for the prompt.
func (m *ConversationMemory) formatHistory(h *DistilledHistory) string {
	if h == nil {
		return ""
	}

	var parts []string

	if h.RelationshipNote != "" {
		parts = append(parts, h.RelationshipNote)
	}
	if len(h.RecurringTopics) > 0 {
		parts = append(parts, "We often discuss: "+strings.Join(h.RecurringTopics, ", "))
	}
	if len(h.UserPreferences) > 0 {
		parts = append(parts, "They prefer: "+strings.Join(h.UserPreferences, "; "))
	}
	if len(h.SharedMemories) > 0 {
		parts = append(parts, "Key moments: "+strings.Join(h.SharedMemories, "; "))
	}

	return strings.Join(parts, "\n")
}

// CompressionOptions configures how conversation turns are compressed into summaries and history.
type CompressionOptions struct {
	// IncludeToday enables summarization for today's date as well. This is useful
	// when a conversation grows long within a single day and L0 history injection
	// is truncated.
	IncludeToday bool

	// MaxDays bounds how many distinct dates will be processed. 0 means unlimited.
	MaxDays int

	// Force recomputes summaries even if an up-to-date summary already exists.
	Force bool

	// Distill triggers L2 distillation after generating/updating day summaries.
	Distill bool
}

// CompressionResult reports what a compression run did.
type CompressionResult struct {
	ConversationID string   `json:"conversation_id"`
	ProcessedDates []string `json:"processed_dates,omitempty"`
	Summarized     int      `json:"summarized"`
	Skipped        int      `json:"skipped"`
	Distilled      bool     `json:"distilled"`
}

// RunDayCompression summarizes all turns for a specific date (YYYY-MM-DD) into a day summary.
// If a summary already exists, it is recomputed only when the day has new turns or when force is true.
//
// Index:
// - Purpose: Produce an L1 day summary for a specific day of a conversation
// - Flow: validate → resolve day boundaries → detect if update needed → load turns → (optionally trim) → summarize → upsert summary → update cursor
// - SideEffects: may invoke LLM summarizer; writes companion_day_summaries and companion_memory_state
// - FailureModes: missing summarizer, DB errors, LLM errors
// - Related: ConversationMemory.RunPendingDailyCompression, ConversationMemory.CompressConversation
// - Keywords: day_summary, L1, compression, backfill, conversation_memory
func (m *ConversationMemory) RunDayCompression(ctx context.Context, conversationID, date string, force bool) (bool, error) {
	if strings.TrimSpace(conversationID) == "" {
		return false, fmt.Errorf("conversation_id is required")
	}
	date = strings.TrimSpace(date)
	if date == "" {
		return false, fmt.Errorf("date is required")
	}

	// Check summarizer without holding lock for the whole operation.
	m.mu.Lock()
	if m.summarizer == nil {
		m.mu.Unlock()
		return false, fmt.Errorf("no summarizer configured")
	}
	summarizer := m.summarizer
	db := m.db
	memoryStore := m.memoryStore
	embedder := m.embedder
	workspace := m.workspace
	// Reuse RecentTokenBudget as a safe bound for summarizer input size.
	inputBudget := m.config.RecentTokenBudget
	m.mu.Unlock()

	dayStart, err := time.ParseInLocation("2006-01-02", date, time.UTC)
	if err != nil {
		return false, fmt.Errorf("parse date %q: %w", date, err)
	}
	dayEnd := dayStart.AddDate(0, 0, 1)

	startStr := dayStart.Format("2006-01-02 15:04:05")
	endStr := dayEnd.Format("2006-01-02 15:04:05")

	// Count turns for the day.
	var totalTurns int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_turns
		WHERE conversation_id = $1 AND created_at >= $2 AND created_at < $3
	`, conversationID, startStr, endStr).Scan(&totalTurns); err != nil {
		return false, fmt.Errorf("count turns: %w", err)
	}
	if totalTurns == 0 {
		return false, nil
	}

	// Check if existing summary is up-to-date.
	var existingTurnCount int
	err = db.QueryRowContext(ctx, `
		SELECT turn_count FROM companion_day_summaries
		WHERE conversation_id = $1 AND date = $2
	`, conversationID, date).Scan(&existingTurnCount)
	switch err {
	case nil:
		if !force && existingTurnCount >= totalTurns {
			return false, nil
		}
	case sql.ErrNoRows:
		// No summary yet - proceed.
	default:
		return false, fmt.Errorf("get existing summary: %w", err)
	}

	// Load turns (chronological).
	rows, err := db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at
		FROM companion_turns
		WHERE conversation_id = $1 AND created_at >= $2 AND created_at < $3
		ORDER BY created_at ASC
	`, conversationID, startStr, endStr)
	if err != nil {
		return false, fmt.Errorf("query turns: %w", err)
	}
	defer rows.Close()

	var turns []ConversationTurn
	for rows.Next() {
		var t ConversationTurn
		if err := rows.Scan(&t.ID, &t.ConversationID, &t.Role, &t.Content, &t.TokenCount, &t.CreatedAt); err != nil {
			continue
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("scan turns: %w", err)
	}
	if len(turns) == 0 {
		return false, nil
	}

	// Bound summarizer input size for long conversations by selecting a representative subset.
	turnsForSummary := selectTurnsForSummaryBudget(turns, inputBudget)

	// Create summary - this is the slow LLM call, done without holding lock.
	summary, err := summarizer.SummarizeDay(ctx, turnsForSummary)
	if err != nil {
		return false, fmt.Errorf("summarize day: %w", err)
	}

	summary.ConversationID = conversationID
	summary.Date = date
	summary.ID = m.newID()
	summary.TurnCount = totalTurns

	// Upsert summary.
	_, err = db.ExecContext(ctx, `
		INSERT INTO companion_day_summaries
		(id, conversation_id, date, turn_count, summary, topics, mood, key_moments, token_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id, date) DO UPDATE SET
			turn_count = excluded.turn_count,
			summary = excluded.summary,
			topics = excluded.topics,
			mood = excluded.mood,
			key_moments = excluded.key_moments,
			token_count = excluded.token_count
	`, summary.ID, summary.ConversationID, summary.Date, summary.TurnCount,
		summary.Summary, joinList(summary.Topics), summary.Mood,
		joinList(summary.KeyMoments), summary.TokenCount)
	if err != nil {
		return false, fmt.Errorf("store summary: %w", err)
	}

	// Store in named_memory for semantic search (if configured).
	if memoryStore != nil && workspace != "" {
		// Build rich summary text for embedding.
		summaryText := summary.Summary
		if len(summary.Topics) > 0 {
			summaryText += "\nTopics: " + strings.Join(summary.Topics, ", ")
		}
		if summary.Mood != "" {
			summaryText += "\nMood: " + summary.Mood
		}
		if len(summary.KeyMoments) > 0 {
			summaryText += "\nKey moments: " + strings.Join(summary.KeyMoments, "; ")
		}

		// Create result JSON for full data preservation.
		resultData, _ := json.Marshal(summary)

		// Memory name for storage and embedding update.
		memoryName := fmt.Sprintf("companion:summary:%s:%s", conversationID, summary.Date)

		_, saveErr := memoryStore.SaveResult(ctx, storage.MemorySaveOptions{
			Name:      memoryName,
			Type:      "companion_summary",
			Workspace: workspace,
			SessionID: conversationID, // Use conversation_id as session for filtering.
			Summary:   summaryText,
			Result:    resultData,
		})
		if saveErr != nil {
			// Log but don't fail - companion memory table is primary; semantic search is supplementary.
			_ = saveErr
		}

		// Generate and store embedding for vector search (if embedder configured).
		if embedder != nil && saveErr == nil {
			dateForDisplay := summary.Date
			if parsedDate, parseErr := time.Parse("2006-01-02", summary.Date); parseErr == nil {
				dateForDisplay = parsedDate.Format("Jan 2, 2006")
			}
			datedSummary := fmt.Sprintf("[%s] %s", dateForDisplay, summaryText)

			if embedding, embErr := embedder.Embed(ctx, datedSummary); embErr == nil && len(embedding.Vec) > 0 {
				_ = memoryStore.UpdateEmbedding(ctx, memoryName, workspace, embedding.Vec)
			}
		}
	}

	// Update cursor.
	_, err = db.ExecContext(ctx, `
		UPDATE companion_memory_state
		SET last_summarized_date = $1, updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = $2
	`, date, conversationID)
	if err != nil {
		return true, err
	}

	return true, nil
}

// RunDailyCompression summarizes yesterday's turns into a day summary (best-effort).
// It is safe to call repeatedly; it only recomputes when yesterday has new turns.
func (m *ConversationMemory) RunDailyCompression(ctx context.Context, conversationID string) error {
	yesterday := m.clock.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	_, err := m.RunDayCompression(ctx, conversationID, yesterday, false)
	return err
}

// RunPendingDailyCompression summarizes the most recent day prior to today that has turns but
// no up-to-date day summary. This is useful for catch-up when a conversation spans days
// without continuous daily compression runs.
//
// Index:
// - Purpose: Backfill L1 day summaries opportunistically without unbounded work
// - Flow: find most recent unsummarized day (< today) → run day compression
// - SideEffects: may invoke LLM summarizer; writes day summary rows
// - FailureModes: missing summarizer, DB errors, LLM errors
// - Related: ConversationMemory.RunDayCompression, ConversationMemory.RunDailyCompression
// - Keywords: pending_compression, backfill, day_summary, L1
func (m *ConversationMemory) RunPendingDailyCompression(ctx context.Context, conversationID string) error {
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	// Check summarizer (fast path) without holding lock for the whole operation.
	m.mu.RLock()
	hasSummarizer := m.summarizer != nil
	db := m.db
	m.mu.RUnlock()
	if !hasSummarizer {
		return fmt.Errorf("no summarizer configured")
	}

	todayDate := m.clock.Now().UTC().Format("2006-01-02")
	var pendingDate string
	err := db.QueryRowContext(ctx, `
		SELECT d.date
		FROM (
			SELECT substr(created_at, 1, 10) AS date, COUNT(*) AS turns
			FROM companion_turns
			WHERE conversation_id = $1 AND substr(created_at, 1, 10) < $2
			GROUP BY date
		) d
		LEFT JOIN companion_day_summaries s
			ON s.conversation_id = $3 AND s.date = d.date
		WHERE s.id IS NULL OR s.turn_count < d.turns
		ORDER BY d.date DESC
		LIMIT 1
	`, conversationID, todayDate, conversationID).Scan(&pendingDate)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query pending date: %w", err)
	}

	_, err = m.RunDayCompression(ctx, conversationID, pendingDate, false)
	return err
}

// CompressConversation runs multi-day compression for a conversation, optionally including today,
// and then runs weekly distillation (L2) when requested.
//
// Index:
// - Purpose: Trigger L0→L1→L2 compaction on demand (manual or UI-triggered)
// - Flow: list distinct turn dates → select up to MaxDays most-recent dates → run day compression per date → optionally distill
// - SideEffects: may invoke LLM summarizer; writes summaries/history; keeps all rows (no pruning)
// - FailureModes: missing summarizer, DB errors, LLM errors
// - Related: ConversationMemory.RunDayCompression, ConversationMemory.RunWeeklyDistillation
// - Keywords: compress, L0, L1, L2, summaries, distillation
func (m *ConversationMemory) CompressConversation(ctx context.Context, conversationID string, opts CompressionOptions) (*CompressionResult, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	// Check summarizer (fast path).
	m.mu.RLock()
	hasSummarizer := m.summarizer != nil
	db := m.db
	m.mu.RUnlock()
	if !hasSummarizer {
		return nil, fmt.Errorf("no summarizer configured")
	}

	today := m.clock.Now().UTC().Format("2006-01-02")

	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT substr(created_at, 1, 10) AS date
		FROM companion_turns
		WHERE conversation_id = $1
		ORDER BY date DESC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list turn dates: %w", err)
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil && d != "" {
			dates = append(dates, d)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan dates: %w", err)
	}

	res := &CompressionResult{ConversationID: conversationID}

	// Select up to MaxDays most-recent distinct dates (optionally excluding today).
	var selected []string
	for _, d := range dates {
		if !opts.IncludeToday && d == today {
			continue
		}
		selected = append(selected, d)
	}
	if opts.MaxDays > 0 && len(selected) > opts.MaxDays {
		selected = selected[:opts.MaxDays]
	}

	for _, d := range selected {

		ran, err := m.RunDayCompression(ctx, conversationID, d, opts.Force)
		if err != nil {
			return res, err
		}
		res.ProcessedDates = append(res.ProcessedDates, d)
		if ran {
			res.Summarized++
		} else {
			res.Skipped++
		}
	}

	if opts.Distill {
		if err := m.RunWeeklyDistillation(ctx, conversationID); err != nil {
			return res, err
		}
		res.Distilled = true
	}

	return res, nil
}

// RunWeeklyDistillation distills old summaries into long-term history.
// Should be called weekly or when summaries exceed the recent window.
func (m *ConversationMemory) RunWeeklyDistillation(ctx context.Context, conversationID string) error {
	if m.summarizer == nil {
		return fmt.Errorf("no summarizer configured")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	nowUTC := m.clock.Now().UTC()

	// Distill day summaries that have moved out of the recent window. We keep all L1 summaries
	// (and all turns) indefinitely; distillation is additive, so we only distill summaries that
	// haven't been distilled yet.
	cutoffDate := nowUTC.AddDate(0, 0, -m.config.RecentWindowDays).Format("2006-01-02")

	// Read last distilled cursor (YYYY-MM-DD). Empty means "never distilled".
	lastDistilledDate := "0000-00-00"
	var lastDistilled sql.NullString
	err := m.db.QueryRowContext(ctx, `
		SELECT last_distilled_date
		FROM companion_memory_state
		WHERE conversation_id = $1
	`, conversationID).Scan(&lastDistilled)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("get last distilled cursor: %w", err)
	}
	if lastDistilled.Valid && strings.TrimSpace(lastDistilled.String) != "" {
		lastDistilledDate = strings.TrimSpace(lastDistilled.String)
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, date, turn_count, summary, topics, mood, key_moments, token_count, created_at
		FROM companion_day_summaries
		WHERE conversation_id = $1 AND date > $2 AND date < $3
		ORDER BY date ASC
	`, conversationID, lastDistilledDate, cutoffDate)
	if err != nil {
		return err
	}
	defer rows.Close()

	var summaries []DaySummary
	for rows.Next() {
		var s DaySummary
		var topics, keyMoments, mood sql.NullString
		if err := rows.Scan(&s.ID, &s.ConversationID, &s.Date, &s.TurnCount, &s.Summary,
			&topics, &mood, &keyMoments, &s.TokenCount, &s.CreatedAt); err != nil {
			continue
		}
		if topics.Valid {
			s.Topics = splitList(topics.String)
		}
		if mood.Valid {
			s.Mood = mood.String
		}
		if keyMoments.Valid {
			s.KeyMoments = splitList(keyMoments.String)
		}
		summaries = append(summaries, s)
	}

	if len(summaries) == 0 {
		return nil // Nothing to distill
	}

	// Track cursor for incremental distillation (we keep summaries, so we only advance by date).
	maxDistilledDate := summaries[len(summaries)-1].Date

	// Get existing history
	existing, _ := m.getDistilledHistory(ctx, conversationID)

	// Distill
	history, err := m.summarizer.DistillHistory(ctx, existing, summaries)
	if err != nil {
		return fmt.Errorf("distill history: %w", err)
	}

	history.ConversationID = conversationID
	if history.ID == "" {
		history.ID = m.newID()
	}
	history.LastDistilledAt = nowUTC

	// Upsert history
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO companion_history
		(id, conversation_id, relationship_note, recurring_topics, user_preferences,
		 shared_memories, token_count, last_distilled_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET
			relationship_note = excluded.relationship_note,
			recurring_topics = excluded.recurring_topics,
			user_preferences = excluded.user_preferences,
			shared_memories = excluded.shared_memories,
			token_count = excluded.token_count,
			last_distilled_at = excluded.last_distilled_at,
			updated_at = CURRENT_TIMESTAMP
	`, history.ID, history.ConversationID, history.RelationshipNote,
		joinList(history.RecurringTopics), joinList(history.UserPreferences),
		joinList(history.SharedMemories), history.TokenCount, history.LastDistilledAt)
	if err != nil {
		return fmt.Errorf("store history: %w", err)
	}

	// Store in named_memory for semantic search (if memory store configured)
	if m.memoryStore != nil && m.workspace != "" {
		// Build rich summary text for embedding
		var historyParts []string
		if history.RelationshipNote != "" {
			historyParts = append(historyParts, history.RelationshipNote)
		}
		if len(history.RecurringTopics) > 0 {
			historyParts = append(historyParts, "Recurring topics: "+strings.Join(history.RecurringTopics, ", "))
		}
		if len(history.UserPreferences) > 0 {
			historyParts = append(historyParts, "User preferences: "+strings.Join(history.UserPreferences, "; "))
		}
		if len(history.SharedMemories) > 0 {
			historyParts = append(historyParts, "Key memories: "+strings.Join(history.SharedMemories, "; "))
		}

		summaryText := strings.Join(historyParts, "\n")

		// Create result JSON for full data preservation
		resultData, _ := json.Marshal(history)

		// Memory name for storage and embedding update
		memoryName := fmt.Sprintf("companion:history:%s", conversationID)

		_, saveErr := m.memoryStore.SaveResult(ctx, storage.MemorySaveOptions{
			Name:      memoryName,
			Type:      "companion_history",
			Workspace: m.workspace,
			SessionID: conversationID, // Use conversation_id as session for filtering
			Summary:   summaryText,
			Result:    resultData,
		})
		if saveErr != nil {
			// Log but don't fail - companion memory table is primary
			_ = saveErr
		}

		// Generate and store embedding for vector search (if embedder configured)
		// Use dated format for temporal context in searches
		if m.embedder != nil && saveErr == nil {
			dateStr := nowUTC.Format("Jan 2, 2006")
			datedSummary := fmt.Sprintf("[%s] Distilled history: %s", dateStr, summaryText)

			if embedding, embErr := m.embedder.Embed(ctx, datedSummary); embErr == nil && len(embedding.Vec) > 0 {
				_ = m.memoryStore.UpdateEmbedding(ctx, memoryName, m.workspace, embedding.Vec)
			}
		}
	}

	// Update cursor for incremental distillation (keep summaries/turns indefinitely).
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO companion_memory_state (conversation_id, last_distilled_date, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET
			last_distilled_date = excluded.last_distilled_date,
			updated_at = CURRENT_TIMESTAMP
	`, conversationID, maxDistilledDate)

	return err
}

// GetStats returns memory statistics for a conversation.
func (m *ConversationMemory) GetStats(ctx context.Context, conversationID string) (*MemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &MemoryStats{ConversationID: conversationID}

	// Count turns
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_turns WHERE conversation_id = $1
	`, conversationID).Scan(&stats.TotalTurns); err != nil {
		return nil, fmt.Errorf("count turns: %w", err)
	}

	// Count summaries
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_day_summaries WHERE conversation_id = $1
	`, conversationID).Scan(&stats.DaySummaries); err != nil {
		return nil, fmt.Errorf("count summaries: %w", err)
	}

	// Check if has history
	var historyCount int
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_history WHERE conversation_id = $1
	`, conversationID).Scan(&historyCount); err != nil {
		return nil, fmt.Errorf("count history: %w", err)
	}
	stats.HasDistilledHistory = historyCount > 0

	// Get state (may not exist, so sql.ErrNoRows is acceptable)
	var lastSummarized, lastDistilled sql.NullString
	err := m.db.QueryRowContext(ctx, `
		SELECT last_summarized_date, last_distilled_date
		FROM companion_memory_state WHERE conversation_id = $1
	`, conversationID).Scan(&lastSummarized, &lastDistilled)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get memory state: %w", err)
	}
	if lastSummarized.Valid {
		stats.LastSummarizedDate = lastSummarized.String
	}
	if lastDistilled.Valid {
		stats.LastDistilledDate = lastDistilled.String
	}

	return stats, nil
}

// MemoryStats contains statistics about conversation memory.
type MemoryStats struct {
	ConversationID      string `json:"conversation_id"`
	TotalTurns          int    `json:"total_turns"`
	DaySummaries        int    `json:"day_summaries"`
	HasDistilledHistory bool   `json:"has_distilled_history"`
	LastSummarizedDate  string `json:"last_summarized_date,omitempty"`
	LastDistilledDate   string `json:"last_distilled_date,omitempty"`
}

// Clear removes all memory for a conversation.
func (m *ConversationMemory) Clear(ctx context.Context, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_turns WHERE conversation_id = $1`, conversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_day_summaries WHERE conversation_id = $1`, conversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_history WHERE conversation_id = $1`, conversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_memory_state WHERE conversation_id = $1`, conversationID); err != nil {
		return err
	}

	// Clean up hybrid memory tables (best-effort; tables may not exist if hybrid mode was never activated)
	hybridTables := []string{
		"companion_events",
		"companion_hard_state_entries",
		"companion_hard_state_cache",
		"companion_soft_episodes",
		"companion_evidence_snippets",
		"companion_assumptions_ledger",
		"companion_open_episode",
		"companion_open_tool_runs",
		"companion_extraction_staging",
		"companion_memory_mode_state",
	}
	for _, table := range hybridTables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE conversation_id = $1`, table), conversationID); err != nil {
			if !strings.Contains(err.Error(), "no such table") {
				return fmt.Errorf("clear %s: %w", table, err)
			}
		}
	}

	return tx.Commit()
}

// Helper functions

// newID returns a new stable identifier for companion memory rows.
//
// Index:
// - Purpose: Centralize ID generation for deterministic tests and consistent formatting
// - Flow: call injected idGenerator
// - SideEffects: increments internal sequence when using default generator
// - FailureModes: none
// - Related: WithIDGenerator, ConversationMemory.AppendTurn
// - Keywords: id, deterministic, testing
func (m *ConversationMemory) newID() string {
	return m.idGenerator()
}

func trimTurnsToTokenBudget(turns []ConversationTurn, budget int) []ConversationTurn {
	if len(turns) == 0 || budget <= 0 {
		return turns
	}

	var kept []ConversationTurn
	total := 0
	for i := len(turns) - 1; i >= 0; i-- {
		tokens := turns[i].TokenCount
		if tokens == 0 {
			tokens = actormemory.EstimateTokens(turns[i].Content)
		}
		if total+tokens > budget && total > 0 {
			break
		}
		total += tokens
		kept = append(kept, turns[i])
	}

	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	return kept
}

// selectTurnsForSummaryBudget returns a representative subset of turns that fits within a token budget.
//
// For long days, we prefer keeping a slice from the beginning and end of the day so the summary
// can capture both how the conversation started and where it ended.
func selectTurnsForSummaryBudget(turns []ConversationTurn, budget int) []ConversationTurn {
	if len(turns) == 0 || budget <= 0 {
		return turns
	}

	// Fast path: fits as-is.
	total := 0
	for _, t := range turns {
		tokens := t.TokenCount
		if tokens == 0 {
			tokens = actormemory.EstimateTokens(t.Content)
		}
		total += tokens
		if total > budget {
			break
		}
	}
	if total <= budget {
		return turns
	}

	headBudget := budget / 2
	tailBudget := budget - headBudget

	var head []ConversationTurn
	headTokens := 0
	i := 0
	for i < len(turns) {
		tokens := turns[i].TokenCount
		if tokens == 0 {
			tokens = actormemory.EstimateTokens(turns[i].Content)
		}
		if headTokens+tokens > headBudget && headTokens > 0 {
			break
		}
		headTokens += tokens
		head = append(head, turns[i])
		i++
		if headTokens >= headBudget {
			break
		}
	}

	var tail []ConversationTurn
	tailTokens := 0
	j := len(turns) - 1
	for j >= i {
		tokens := turns[j].TokenCount
		if tokens == 0 {
			tokens = actormemory.EstimateTokens(turns[j].Content)
		}
		if tailTokens+tokens > tailBudget && tailTokens > 0 {
			break
		}
		tailTokens += tokens
		tail = append(tail, turns[j])
		j--
		if tailTokens >= tailBudget {
			break
		}
	}

	// Restore chronological order for the tail slice.
	for a, b := 0, len(tail)-1; a < b; a, b = a+1, b-1 {
		tail[a], tail[b] = tail[b], tail[a]
	}

	// Fallback: always return something.
	if len(head) == 0 && len(tail) == 0 {
		return nil
	}
	if len(head) == 0 {
		return tail
	}
	if len(tail) == 0 {
		return head
	}

	out := append([]ConversationTurn{}, head...)
	out = append(out, tail...)
	return out
}

func trimToTokenBudget(text string, budget int, keepTail bool) string {
	if text == "" || budget <= 0 {
		return text
	}
	trimmed, truncated := actormemory.TruncateToFitWithMargin(text, budget, 1.0, keepTail)
	if !truncated {
		return trimmed
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func applyTotalTokenBudget(historyText, summaryText, turnText string, totalBudget int) (string, string, string) {
	if totalBudget <= 0 {
		return historyText, summaryText, turnText
	}

	budget := actormemory.NewTokenBudgetWithMargin(totalBudget, 1.0)
	turnText = trimTextWithBudget(turnText, budget, true)
	summaryText = trimTextWithBudget(summaryText, budget, false)
	historyText = trimTextWithBudget(historyText, budget, false)

	return historyText, summaryText, turnText
}

func trimTextWithBudget(text string, budget *actormemory.TokenBudget, keepTail bool) string {
	if text == "" {
		return text
	}
	if budget.Remaining <= 0 {
		return ""
	}
	if budget.CanFitText(text) {
		budget.AddText(text)
		return text
	}
	trimmed, _ := actormemory.TruncateToFitWithMargin(text, budget.Remaining, 1.0, keepTail)
	budget.AddText(trimmed)
	return trimmed
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "|||")
}

func joinList(items []string) string {
	return strings.Join(items, "|||")
}

// Export returns the full memory state as JSON for debugging.
func (m *ConversationMemory) Export(ctx context.Context, conversationID string) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	export := map[string]interface{}{
		"conversation_id": conversationID,
	}

	// Get turns
	turns, _ := m.getVividTurns(ctx, conversationID)
	export["vivid_turns"] = turns

	// Get summaries
	summaries, _ := m.getRecentSummaries(ctx, conversationID)
	export["day_summaries"] = summaries

	// Get history
	history, _ := m.getDistilledHistory(ctx, conversationID)
	export["distilled_history"] = history

	// Get stats
	stats, _ := m.GetStats(ctx, conversationID)
	export["stats"] = stats

	return json.MarshalIndent(export, "", "  ")
}

// ConversationSummary represents a conversation for listing.
type ConversationSummary struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	MessageCount int    `json:"message_count"`
	LastMessage  string `json:"last_message,omitempty"`
}

// ListConversations returns all non-deleted conversations with titles, agent links, and summaries.
func (m *ConversationMemory) ListConversations(ctx context.Context, limit int) ([]ConversationSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	// Get distinct conversation IDs with stats (excluding soft-deleted), including custom titles
	query := `
		SELECT
			t.conversation_id,
			COUNT(*) as message_count,
			MIN(t.created_at) as created_at,
			MAX(t.created_at) as updated_at,
			COALESCE(titles.title, '') as title,
			COALESCE(titles.agent_id, '') as agent_id
		FROM companion_turns t
		LEFT JOIN companion_conversation_titles titles ON t.conversation_id = titles.conversation_id
		WHERE t.conversation_id NOT IN (SELECT conversation_id FROM companion_deleted_conversations)
		GROUP BY t.conversation_id
		ORDER BY MAX(t.created_at) DESC
		LIMIT $1
	`

	rows, err := m.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []ConversationSummary
	for rows.Next() {
		var conv ConversationSummary
		var createdAt, updatedAt, title, agentID string
		if err := rows.Scan(&conv.ID, &conv.MessageCount, &createdAt, &updatedAt, &title, &agentID); err != nil {
			return nil, err
		}
		conv.Name = title
		conv.AgentID = agentID
		// Parse timestamps and convert to ISO 8601 for frontend compatibility
		conv.CreatedAt = parseAndFormatTimestamp(createdAt)
		conv.UpdatedAt = parseAndFormatTimestamp(updatedAt)
		conversations = append(conversations, conv)
	}

	// Get last message for each conversation
	for i := range conversations {
		lastMsgQuery := `
			SELECT content FROM companion_turns
			WHERE conversation_id = $1
			ORDER BY created_at DESC
			LIMIT 1
		`
		var lastMsg string
		if err := m.db.QueryRowContext(ctx, lastMsgQuery, conversations[i].ID).Scan(&lastMsg); err == nil {
			// Truncate to first 100 chars
			if len(lastMsg) > 100 {
				lastMsg = lastMsg[:100] + "..."
			}
			conversations[i].LastMessage = lastMsg
		}
	}

	return conversations, nil
}

// GetConversationMessages retrieves the most recent messages for a specific conversation.
// Returns messages in chronological order (oldest first).
func (m *ConversationMemory) GetConversationMessages(ctx context.Context, conversationID string, limit int) ([]ConversationTurn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	// Fetch the newest N rows, then reorder oldest-first so clients can render
	// message history naturally.
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at, tool_calls
		FROM (
			SELECT id, conversation_id, role, content, token_count, created_at, tool_calls
			FROM companion_turns
			WHERE conversation_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		)
		ORDER BY created_at ASC
	`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var turns []ConversationTurn
	for rows.Next() {
		var t ConversationTurn
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&t.ID, &t.ConversationID, &t.Role, &t.Content, &t.TokenCount, &t.CreatedAt, &toolCallsJSON); err != nil {
			observability.Emit(ctx, observability.NewEvent("companion.turn_scan_failed").
				WithComponent("companion").
				WithData("conversation_id", conversationID).
				WithData("error", err.Error()).
				Error(err, 0))
			continue
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			t.ToolCalls = json.RawMessage(toolCallsJSON.String)
		}
		turns = append(turns, t)
	}

	return turns, rows.Err()
}

// DeleteMessage removes a single message (turn) from a conversation.
func (m *ConversationMemory) DeleteMessage(ctx context.Context, conversationID, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.db.ExecContext(ctx, `
		DELETE FROM companion_turns WHERE id = $1 AND conversation_id = $2
	`, messageID, conversationID)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete message rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("message not found")
	}

	return nil
}

// SoftDeleteConversation marks a conversation as deleted without removing the data.
func (m *ConversationMemory) SoftDeleteConversation(ctx context.Context, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_deleted_conversations (conversation_id, deleted_at)
		VALUES ($1, CURRENT_TIMESTAMP)
		ON CONFLICT (conversation_id) DO UPDATE SET deleted_at = EXCLUDED.deleted_at
	`, conversationID)
	if err != nil {
		return fmt.Errorf("soft delete conversation: %w", err)
	}

	return nil
}

// RenameConversation sets or updates the custom title for a conversation.
// Empty title clears the title but preserves agent_id (UPDATE, not DELETE).
func (m *ConversationMemory) RenameConversation(ctx context.Context, conversationID, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if title == "" {
		// If empty title, clear the title but preserve agent_id
		_, err := m.db.ExecContext(ctx, `
			UPDATE companion_conversation_titles SET title = '', updated_at = CURRENT_TIMESTAMP WHERE conversation_id = $1
		`, conversationID)
		if err != nil {
			return fmt.Errorf("remove conversation title: %w", err)
		}
		return nil
	}

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_conversation_titles (conversation_id, title, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET title = $3, updated_at = CURRENT_TIMESTAMP
	`, conversationID, title, title)
	if err != nil {
		return fmt.Errorf("rename conversation: %w", err)
	}

	return nil
}

// LinkConversationAgent associates a conversation with an agent ID.
// Supports many-to-one: multiple conversations can link to the same agent.
//
// Index:
// - Purpose: Set agent_id on companion_conversation_titles for many-to-one linking
// - Flow: upsert conversation_titles row with agent_id
// - SideEffects: writes companion_conversation_titles
// - FailureModes: DB errors
// - Related: ConversationMemory.RenameConversation, ConversationMemory.ListConversations
// - Keywords: agent_id, conversation_titles, link, many_to_one
func (m *ConversationMemory) LinkConversationAgent(ctx context.Context, conversationID, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_conversation_titles (conversation_id, title, agent_id, updated_at)
		VALUES ($1, '', $2, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET agent_id = $3, updated_at = CURRENT_TIMESTAMP
	`, conversationID, agentID, agentID)
	if err != nil {
		return fmt.Errorf("link conversation agent: %w", err)
	}
	return nil
}

// parseAndFormatTimestamp attempts to parse various timestamp formats and returns ISO 8601.
// parseAndFormatTimestamp parses a timestamp string in several common formats and returns it formatted as RFC3339 in UTC.
// It accepts Go's default time format (including values that contain a monotonic clock suffix like " m=+..."), RFC3339/RFC3339Nano, and a few common date-time layouts.
// If parsing fails, it returns the original input unchanged.
func parseAndFormatTimestamp(ts string) string {
	if ts == "" {
		return ""
	}

	// Try various formats
	formats := []string{
		// Go default format with monotonic clock (remove m=+... suffix first)
		"2006-01-02 15:04:05.999999 -0700 MST",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
	}

	// Strip monotonic clock reading if present (e.g., "m=+123.456")
	if idx := strings.Index(ts, " m="); idx > 0 {
		ts = ts[:idx]
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}

	// If all parsing fails, return original (better than empty)
	return ts
}
