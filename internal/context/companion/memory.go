package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	actormemory "github.com/joshka0/foxctl/internal/runtime/actor/memory"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
)

// ConversationMemory manages hybrid companion context for conversations.
//
// Unlike code-focused ShortTermMemory, this is optimized for:
// - Short conversational turns (50-200 tokens)
// - Event-derived, trust-labeled context layers
// - Relationship continuity (preferences, assumptions, episodic summaries)
type ConversationMemory struct {
	db                   *sql.DB
	llmSummarizer        *LLMSummarizer
	memoryStore          storage.MemoryStore // Optional: for semantic search integration
	workspace            string              // Workspace for semantic search scoping
	embedder             *semantic.Embedder  // Optional: for generating embeddings on summaries
	tokenCounter         TokenCounter
	episodeSummaryRunner func(ctx context.Context, cfg engine.LLMChatConfig, input engine.EngineInput) (engine.EngineOutput, error)
	config               MemoryConfig
	clock                skilltest.Clock
	idGenerator          func() string
	idSeq                uint64
	mu                   sync.RWMutex
	extractionPolicy     ExtractionPolicy // Typed extraction policy (replaces keyword heuristics)
	signalExtractor      SignalExtractor  // Typed episode boundary signals (replaces isRetractionSignal/isUserRedirectSignal)
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

// ContextLayerBudget captures token budgets for each memory layer.
// This keeps layered context assembly explicit and easy to reuse by dynamic
// context builders that may choose to pull only a subset of layers.
type ContextLayerBudget struct {
	L0Vivid   int
	L1Recent  int
	L2History int
	Total     int
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

// LayerBudget returns per-layer and total token budgets for context assembly.
func (m *ConversationMemory) LayerBudget() ContextLayerBudget {
	return ContextLayerBudget{
		L0Vivid:   m.config.VividTokenBudget,
		L1Recent:  m.config.RecentTokenBudget,
		L2History: m.config.HistoryTokenBudget,
		Total:     m.config.TotalTokenBudget,
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
		db:           db,
		config:       DefaultMemoryConfig(),
		clock:        skilltest.RealClock{},
		tokenCounter: NewTikTokenCounter(""),
	}
	m.idGenerator = func() string {
		seq := atomic.AddUint64(&m.idSeq, 1)
		return fmt.Sprintf("%d-%d", m.clock.Now().UnixNano(), seq)
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.episodeSummaryRunner == nil {
		m.episodeSummaryRunner = defaultEpisodeSummaryRunner
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
	if m.tokenCounter == nil {
		m.tokenCounter = NewHeuristicTokenCounter()
	}
	if m.extractionPolicy == nil {
		m.extractionPolicy = NewCompositeExtractionPolicy(
			ToolResultBypassPolicy{},
			NewDefaultPatternExtractionPolicy(),
		)
	}
	if m.signalExtractor == nil {
		m.signalExtractor = NewDefaultTypedSignalExtractor()
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

// WithSummarizer sets the LLM summarizer used by hybrid episode summarization.
func WithSummarizer(s *LLMSummarizer) MemoryOption {
	return func(m *ConversationMemory) {
		m.llmSummarizer = s
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

// WithTokenCounter sets the token counter used for companion memory budgeting.
func WithTokenCounter(counter TokenCounter) MemoryOption {
	return func(m *ConversationMemory) {
		if counter != nil {
			m.tokenCounter = counter
		}
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

func defaultEpisodeSummaryRunner(ctx context.Context, cfg engine.LLMChatConfig, input engine.EngineInput) (engine.EngineOutput, error) {
	llm, err := engine.NewLLMChatEngine(cfg)
	if err != nil {
		return engine.EngineOutput{}, err
	}
	return llm.Run(ctx, input)
}

// WithEpisodeSummaryRunner sets the function used to run LLM calls for episode summarization.
// Useful for injecting test doubles.
func WithEpisodeSummaryRunner(fn func(context.Context, engine.LLMChatConfig, engine.EngineInput) (engine.EngineOutput, error)) MemoryOption {
	return func(m *ConversationMemory) { m.episodeSummaryRunner = fn }
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
				mode TEXT NOT NULL DEFAULT 'hybrid',
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

	// Migration: hard-cut legacy mode rows to hybrid.
	_, _ = m.db.ExecContext(ctx, `
		UPDATE companion_memory_mode_state
		SET mode = 'hybrid', updated_at = CURRENT_TIMESTAMP
		WHERE mode IS NULL OR lower(trim(mode)) <> 'hybrid'
	`)

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
// AppendTurn stores a conversation turn and emits a hybrid event.
//
// Index:
// - Purpose: Persist a conversation turn and update totals
// - Flow: normalize turn → insert turn → insert hybrid event
// - SideEffects: writes to companion_turns + companion_events(+derived hybrid tables)
// - FailureModes: insert errors, hybrid bridge errors (logged)
// - Related: ConversationMemory.GetHybridContext
// - Keywords: append_turn, conversation_id, token_count, hybrid_event, tool_calls
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
		turn.TokenCount = m.countTokens(turn.Content)
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

	// Bridge every turn to the hybrid event pipeline (v2 hard-cut: hybrid is the only mode).
	eventType := EventTypeUserMessage
	if turn.Role == "assistant" {
		eventType = EventTypeAssistantMessage
	}
	event := &ConversationEvent{
		ConversationID: turn.ConversationID,
		EventType:      eventType,
		TurnID:         turn.ID,
		Content:        turn.Content,
		TokenCount:     turn.TokenCount,
	}
	if evErr := m.InsertEvent(ctx, event); evErr != nil {
		zerolog.Ctx(ctx).Warn().
			Str("conversation_id", turn.ConversationID).
			Str("turn_id", turn.ID).
			Err(evErr).
			Msg("hybrid bridge: failed to insert event")
	} else if turn.Role == "user" && event.ID > 0 {
		// Capture deterministic user facts immediately so they are available
		// even if async pipeline work is delayed.
		if prefErr := m.extractUserFactsFromTurn(ctx, turn.ConversationID, event.ID, turn.Content); prefErr != nil {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", turn.ConversationID).
				Str("turn_id", turn.ID).
				Err(prefErr).
				Msg("hybrid bridge: failed to extract deterministic facts from turn")
		}
	}

	return nil
}

func (m *ConversationMemory) extractUserFactsFromTurn(ctx context.Context, conversationID string, sourceEventID int64, content string) error {
	content = strings.TrimSpace(content)
	if conversationID == "" || sourceEventID <= 0 || content == "" {
		return nil
	}

	extractions := m.extractionPolicy.ExtractEntries(content, []string{ExtractionCategoryPreference})
	extractions = append(extractions, extractExplicitFacts(content)...)
	if len(extractions) == 0 {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin preference extraction tx: %w", err)
	}
	defer tx.Rollback()

	for _, entry := range extractions {
		switch entry.EntryType {
		case EntryTypePreference, EntryTypeTechnicalContext, EntryTypeIdentity:
			if err := m.persistDeterministicExtraction(ctx, tx, conversationID, sourceEventID, entry); err != nil {
				return fmt.Errorf("persist deterministic extraction: %w", err)
			}
		default:
			continue
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_hard_state_cache WHERE conversation_id = $1`, conversationID); err != nil {
		return fmt.Errorf("invalidate hard state cache: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deterministic extraction tx: %w", err)
	}

	return nil
}

func (m *ConversationMemory) getAllTurns(ctx context.Context, conversationID string) ([]ConversationTurn, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at, tool_calls
		FROM companion_turns
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []ConversationTurn
	for rows.Next() {
		var t ConversationTurn
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&t.ID, &t.ConversationID, &t.Role, &t.Content, &t.TokenCount, &t.CreatedAt, &toolCallsJSON); err != nil {
			continue
		}
		if toolCallsJSON.Valid && strings.TrimSpace(toolCallsJSON.String) != "" {
			t.ToolCalls = json.RawMessage(toolCallsJSON.String)
		}
		turns = append(turns, t)
	}

	return turns, rows.Err()
}

func (m *ConversationMemory) getMemoryModeState(ctx context.Context, conversationID string) (*MemoryModeState, error) {
	var state MemoryModeState
	err := m.db.QueryRowContext(ctx, `
		SELECT conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, conversationID).Scan(
		&state.ConversationID,
		&state.Mode,
		&state.SchemaVersion,
		&state.LastProcessedEvent,
		&state.LastSoftEvent,
		&state.LastEvidenceEvent,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	state.Mode = MemoryModeHybrid
	return &state, nil
}

func (m *ConversationMemory) countTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	if m.tokenCounter == nil {
		return actormemory.EstimateTokens(text)
	}
	if count := m.tokenCounter.Count(text); count > 0 {
		return count
	}
	return actormemory.EstimateTokens(text)
}

// CompressionOptions configures hybrid maintenance behavior.
type CompressionOptions struct {
	// IncludeToday is retained for request-shape compatibility.
	IncludeToday bool

	// MaxDays is retained for request-shape compatibility.
	MaxDays int

	// Force is retained for request-shape compatibility.
	Force bool

	// Distill triggers episode summary generation for pending soft episodes.
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

// RunDayCompression executes hybrid event processing for a conversation.
func (m *ConversationMemory) RunDayCompression(ctx context.Context, conversationID, _ string, _ bool) (bool, error) {
	if strings.TrimSpace(conversationID) == "" {
		return false, fmt.Errorf("conversation_id is required")
	}
	if err := m.EnsureHybridMode(ctx, conversationID); err != nil {
		return false, err
	}
	if err := m.BuildHybridContextLayers(ctx, conversationID); err != nil {
		return false, err
	}
	return true, nil
}

// CompressConversation executes v2 hybrid processing for a conversation.
func (m *ConversationMemory) CompressConversation(ctx context.Context, conversationID string, opts CompressionOptions) (*CompressionResult, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	res := &CompressionResult{
		ConversationID: conversationID,
		ProcessedDates: []string{m.clock.Now().UTC().Format("2006-01-02")},
	}
	ran, err := m.RunDayCompression(ctx, conversationID, "", false)
	if err != nil {
		return res, err
	}
	if ran {
		res.Summarized = 1
	}
	if opts.Distill {
		if err := m.RunEpisodeSummaryPass(ctx, conversationID); err != nil {
			return res, err
		}
		res.Distilled = true
	}
	return res, nil
}

// RunEpisodeSummaryPass performs a hybrid episode-summary pass on pending episodes.
func (m *ConversationMemory) RunEpisodeSummaryPass(ctx context.Context, conversationID string) error {
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}
	if err := m.EnsureHybridMode(ctx, conversationID); err != nil {
		return err
	}
	if err := m.BuildHybridContextLayers(ctx, conversationID); err != nil {
		return err
	}

	m.mu.RLock()
	llmSummarizer := m.llmSummarizer
	m.mu.RUnlock()
	if llmSummarizer == nil {
		return nil
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, start_event_id, end_event_id
		FROM companion_soft_episodes
		WHERE conversation_id = $1 AND needs_summary = 1
		ORDER BY id ASC
		LIMIT 50
	`, conversationID)
	if err != nil {
		return fmt.Errorf("query pending episode summaries: %w", err)
	}
	defer rows.Close()

	type episodeRow struct {
		id           int64
		startEventID int64
		endEventID   int64
	}
	var episodes []episodeRow
	for rows.Next() {
		var e episodeRow
		if scanErr := rows.Scan(&e.id, &e.startEventID, &e.endEventID); scanErr != nil {
			return fmt.Errorf("scan pending episode summaries: %w", scanErr)
		}
		episodes = append(episodes, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pending episode summaries: %w", err)
	}

	for _, ep := range episodes {
		summary, tokenCount, sumErr := m.SummarizeEpisode(ctx, conversationID, ep.id, ep.startEventID, ep.endEventID)
		if sumErr != nil {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", conversationID).
				Int64("episode_id", ep.id).
				Err(sumErr).
				Msg("hybrid episode summary pass: episode summary failed")
			continue
		}
		if _, updateErr := m.db.ExecContext(ctx, `
			UPDATE companion_soft_episodes
			SET summary = $1, needs_summary = 0, token_count = $2
			WHERE id = $3
		`, summary, tokenCount, ep.id); updateErr != nil {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", conversationID).
				Int64("episode_id", ep.id).
				Err(updateErr).
				Msg("hybrid episode summary pass: failed to persist episode summary")
		}
	}

	return nil
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

	// Count events
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_events WHERE conversation_id = $1
	`, conversationID).Scan(&stats.EventCount); err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}

	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = $1
	`, conversationID).Scan(&stats.HardStateCount); err != nil {
		return nil, fmt.Errorf("count hard state entries: %w", err)
	}

	err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_soft_episodes WHERE conversation_id = $1 AND deleted_at IS NULL
	`, conversationID).Scan(&stats.EpisodeCount)
	if err != nil {
		return nil, fmt.Errorf("count episodes: %w", err)
	}

	err = m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_evidence_snippets WHERE conversation_id = $1
	`, conversationID).Scan(&stats.EvidenceCount)
	if err != nil {
		return nil, fmt.Errorf("count evidence snippets: %w", err)
	}

	err = m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_assumptions_ledger WHERE conversation_id = $1
	`, conversationID).Scan(&stats.AssumptionCount)
	if err != nil {
		return nil, fmt.Errorf("count assumptions: %w", err)
	}

	// Mode state is optional for brand new conversations.
	var state MemoryModeState
	err = m.db.QueryRowContext(ctx, `
		SELECT conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, conversationID).Scan(
		&state.ConversationID,
		&state.Mode,
		&state.SchemaVersion,
		&state.LastProcessedEvent,
		&state.LastSoftEvent,
		&state.LastEvidenceEvent,
		&state.UpdatedAt,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get memory mode state: %w", err)
	}
	if err == nil {
		stats.Mode = MemoryModeHybrid
		stats.SchemaVersion = state.SchemaVersion
		stats.LastProcessedEvent = state.LastProcessedEvent
		stats.LastSoftEvent = state.LastSoftEvent
		stats.LastEvidenceEvent = state.LastEvidenceEvent
		stats.UpdatedAt = state.UpdatedAt
	}

	return stats, nil
}

// MemoryStats contains statistics about conversation memory.
type MemoryStats struct {
	ConversationID     string `json:"conversation_id"`
	Mode               string `json:"mode,omitempty"`
	SchemaVersion      int    `json:"schema_version,omitempty"`
	TotalTurns         int    `json:"total_turns"`
	EventCount         int    `json:"event_count"`
	HardStateCount     int    `json:"hard_state_count"`
	EpisodeCount       int    `json:"episode_count"`
	EvidenceCount      int    `json:"evidence_count"`
	AssumptionCount    int    `json:"assumption_count"`
	LastProcessedEvent int64  `json:"last_processed_event,omitempty"`
	LastSoftEvent      int64  `json:"last_soft_event,omitempty"`
	LastEvidenceEvent  int64  `json:"last_evidence_event,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
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

	// Clean up hybrid memory tables.
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
			return fmt.Errorf("clear %s: %w", table, err)
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

// MemoryBackupPayload is a portable backup format for companion memory state.
type MemoryBackupPayload struct {
	ConversationID   string                   `json:"conversation_id"`
	Turns            []ConversationTurn       `json:"turns"`
	Events           []ConversationEvent      `json:"events,omitempty"`
	HardStateEntries []HardStateEntry         `json:"hard_state_entries,omitempty"`
	SoftEpisodes     []SoftEpisode            `json:"soft_episodes,omitempty"`
	EvidenceSnippets []EvidenceSnippet        `json:"evidence_snippets,omitempty"`
	Assumptions      []Assumption             `json:"assumptions,omitempty"`
	ModeState        *MemoryModeState         `json:"mode_state,omitempty"`
	OpenEpisode      *OpenEpisodeState        `json:"open_episode,omitempty"`
	OpenToolRuns     []OpenToolRun            `json:"open_tool_runs,omitempty"`
	ExtractionStage  []ExtractionStagingEntry `json:"extraction_staging,omitempty"`
	HardStateCache   *HardStateCache          `json:"hard_state_cache,omitempty"`
}

// Export returns the full conversation memory state as JSON for backup/debugging.
func (m *ConversationMemory) Export(ctx context.Context, conversationID string) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backup := MemoryBackupPayload{
		ConversationID: conversationID,
	}

	turns, err := m.getAllTurns(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export turns: %w", err)
	}
	backup.Turns = turns

	eventRows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, event_type, turn_id, tool_name, tool_run_id, parent_tool_call_id,
		       payload_json, payload_ref, token_count, content_hash, created_at
		FROM companion_events
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export events: %w", err)
	}
	for eventRows.Next() {
		var (
			e                ConversationEvent
			turnID           sql.NullString
			toolName         sql.NullString
			toolRunID        sql.NullString
			parentToolCallID sql.NullInt64
			payloadJSON      sql.NullString
			payloadRef       sql.NullString
			tokenCount       sql.NullInt64
			contentHash      sql.NullString
		)
		if err := eventRows.Scan(
			&e.ID,
			&e.ConversationID,
			&e.EventType,
			&turnID,
			&toolName,
			&toolRunID,
			&parentToolCallID,
			&payloadJSON,
			&payloadRef,
			&tokenCount,
			&contentHash,
			&e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan export event: %w", err)
		}
		if turnID.Valid {
			e.TurnID = turnID.String
		}
		if toolName.Valid {
			e.ToolName = toolName.String
		}
		if toolRunID.Valid {
			e.ToolRunID = toolRunID.String
		}
		if parentToolCallID.Valid {
			e.ParentToolCallID = parentToolCallID.Int64
		}
		if payloadJSON.Valid {
			e.PayloadJSON = payloadJSON.String
		}
		if payloadRef.Valid {
			e.PayloadRef = payloadRef.String
		}
		if tokenCount.Valid {
			e.TokenCount = int(tokenCount.Int64)
		}
		if contentHash.Valid {
			e.ContentHash = contentHash.String
		}
		backup.Events = append(backup.Events, e)
	}
	if err := eventRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export events: %w", err)
	}
	_ = eventRows.Close()

	hardRows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, entry_type, key, value_json, status, source_event_id, confidence, metadata_json, supersedes, created_at
		FROM companion_hard_state_entries
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export hard state entries: %w", err)
	}
	for hardRows.Next() {
		var (
			entry      HardStateEntry
			metadata   sql.NullString
			supersedes sql.NullInt64
		)
		if err := hardRows.Scan(
			&entry.ID,
			&entry.ConversationID,
			&entry.EntryType,
			&entry.Key,
			&entry.ValueJSON,
			&entry.Status,
			&entry.SourceEventID,
			&entry.Confidence,
			&metadata,
			&supersedes,
			&entry.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan export hard state entry: %w", err)
		}
		if metadata.Valid {
			entry.MetadataJSON = strPtr(metadata.String)
		}
		if supersedes.Valid {
			entry.Supersedes = int64Ptr(supersedes.Int64)
		}
		backup.HardStateEntries = append(backup.HardStateEntries, entry)
	}
	if err := hardRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export hard state entries: %w", err)
	}
	_ = hardRows.Close()

	episodeRows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, episode_type, start_event_id, end_event_id, summary, needs_summary,
		       assumption_ids, token_count, boundary_hash, created_at, deleted_at
		FROM companion_soft_episodes
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export soft episodes: %w", err)
	}
	for episodeRows.Next() {
		var (
			episode   SoftEpisode
			deletedAt sql.NullString
		)
		if err := episodeRows.Scan(
			&episode.ID,
			&episode.ConversationID,
			&episode.EpisodeType,
			&episode.StartEventID,
			&episode.EndEventID,
			&episode.Summary,
			&episode.NeedsSummary,
			&episode.AssumptionIDs,
			&episode.TokenCount,
			&episode.BoundaryHash,
			&episode.CreatedAt,
			&deletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan export soft episode: %w", err)
		}
		if deletedAt.Valid {
			episode.DeletedAt = strPtr(deletedAt.String)
		}
		backup.SoftEpisodes = append(backup.SoftEpisodes, episode)
	}
	if err := episodeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export soft episodes: %w", err)
	}
	_ = episodeRows.Close()

	evidenceRows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, source_event_id, event_type, fact_text, content_hash, confidence,
		       bucket, ttl_days, created_at, expires_at
		FROM companion_evidence_snippets
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export evidence snippets: %w", err)
	}
	for evidenceRows.Next() {
		var (
			snippet   EvidenceSnippet
			ttlDays   sql.NullInt64
			expiresAt sql.NullString
		)
		if err := evidenceRows.Scan(
			&snippet.ID,
			&snippet.ConversationID,
			&snippet.SourceEventID,
			&snippet.EventType,
			&snippet.FactText,
			&snippet.ContentHash,
			&snippet.Confidence,
			&snippet.Bucket,
			&ttlDays,
			&snippet.CreatedAt,
			&expiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan export evidence snippet: %w", err)
		}
		if ttlDays.Valid {
			ttl := int(ttlDays.Int64)
			snippet.TTLDays = &ttl
		}
		if expiresAt.Valid {
			snippet.ExpiresAt = strPtr(expiresAt.String)
		}
		backup.EvidenceSnippets = append(backup.EvidenceSnippets, snippet)
	}
	if err := evidenceRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export evidence snippets: %w", err)
	}
	_ = evidenceRows.Close()

	assumptionRows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, assumption, status, reason, source_event_id, confidence,
		       created_at, retracted_at, retracted_by_event_id, retraction_reason
		FROM companion_assumptions_ledger
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export assumptions: %w", err)
	}
	for assumptionRows.Next() {
		var (
			assumption       Assumption
			reason           sql.NullString
			retractedAt      sql.NullString
			retractedByEvent sql.NullInt64
			retractionReason sql.NullString
		)
		if err := assumptionRows.Scan(
			&assumption.ID,
			&assumption.ConversationID,
			&assumption.Assumption,
			&assumption.Status,
			&reason,
			&assumption.SourceEventID,
			&assumption.Confidence,
			&assumption.CreatedAt,
			&retractedAt,
			&retractedByEvent,
			&retractionReason,
		); err != nil {
			return nil, fmt.Errorf("scan export assumption: %w", err)
		}
		if reason.Valid {
			assumption.Reason = strPtr(reason.String)
		}
		if retractedAt.Valid {
			assumption.RetractedAt = strPtr(retractedAt.String)
		}
		if retractedByEvent.Valid {
			assumption.RetractedByEventID = int64Ptr(retractedByEvent.Int64)
		}
		if retractionReason.Valid {
			assumption.RetractionReason = strPtr(retractionReason.String)
		}
		backup.Assumptions = append(backup.Assumptions, assumption)
	}
	if err := assumptionRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export assumptions: %w", err)
	}
	_ = assumptionRows.Close()

	modeState, err := m.getMemoryModeState(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export mode state: %w", err)
	}
	backup.ModeState = modeState

	var openEpisode OpenEpisodeState
	var topicSig sql.NullString
	var pendingSealReason sql.NullString
	err = m.db.QueryRowContext(ctx, `
		SELECT conversation_id, start_event_id, episode_type, event_count, topic_sig, pending_seal_reason, updated_at
		FROM companion_open_episode
		WHERE conversation_id = $1
	`, conversationID).Scan(
		&openEpisode.ConversationID,
		&openEpisode.StartEventID,
		&openEpisode.EpisodeType,
		&openEpisode.EventCount,
		&topicSig,
		&pendingSealReason,
		&openEpisode.UpdatedAt,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("export open episode: %w", err)
	}
	if err == nil {
		if topicSig.Valid {
			openEpisode.TopicSig = strPtr(topicSig.String)
		}
		if pendingSealReason.Valid {
			openEpisode.PendingSealReason = strPtr(pendingSealReason.String)
		}
		backup.OpenEpisode = &openEpisode
	}

	openToolRows, err := m.db.QueryContext(ctx, `
		SELECT conversation_id, tool_run_id, start_event_id, parent_call_event_id, created_at
		FROM companion_open_tool_runs
		WHERE conversation_id = $1
		ORDER BY start_event_id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export open tool runs: %w", err)
	}
	for openToolRows.Next() {
		var (
			run             OpenToolRun
			parentCallEvent sql.NullInt64
		)
		if err := openToolRows.Scan(
			&run.ConversationID,
			&run.ToolRunID,
			&run.StartEventID,
			&parentCallEvent,
			&run.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan export open tool run: %w", err)
		}
		if parentCallEvent.Valid {
			run.ParentCallEventID = int64Ptr(parentCallEvent.Int64)
		}
		backup.OpenToolRuns = append(backup.OpenToolRuns, run)
	}
	if err := openToolRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export open tool runs: %w", err)
	}
	_ = openToolRows.Close()

	stageRows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, source_event_id, proposed_entry_type, raw_text, reason, attempt_count,
		       created_at, resolved_at, discarded_at, discard_reason
		FROM companion_extraction_staging
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("export extraction staging: %w", err)
	}
	for stageRows.Next() {
		var (
			entry         ExtractionStagingEntry
			resolvedAt    sql.NullString
			discardedAt   sql.NullString
			discardReason sql.NullString
		)
		if err := stageRows.Scan(
			&entry.ID,
			&entry.ConversationID,
			&entry.SourceEventID,
			&entry.ProposedEntryType,
			&entry.RawText,
			&entry.Reason,
			&entry.AttemptCount,
			&entry.CreatedAt,
			&resolvedAt,
			&discardedAt,
			&discardReason,
		); err != nil {
			return nil, fmt.Errorf("scan export extraction staging entry: %w", err)
		}
		if resolvedAt.Valid {
			entry.ResolvedAt = strPtr(resolvedAt.String)
		}
		if discardedAt.Valid {
			entry.DiscardedAt = strPtr(discardedAt.String)
		}
		if discardReason.Valid {
			entry.DiscardReason = strPtr(discardReason.String)
		}
		backup.ExtractionStage = append(backup.ExtractionStage, entry)
	}
	if err := stageRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export extraction staging: %w", err)
	}
	_ = stageRows.Close()

	var cache HardStateCache
	err = m.db.QueryRowContext(ctx, `
		SELECT conversation_id, compact_json, last_entry_id, updated_at
		FROM companion_hard_state_cache
		WHERE conversation_id = $1
	`, conversationID).Scan(&cache.ConversationID, &cache.CompactJSON, &cache.LastEntryID, &cache.UpdatedAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("export hard state cache: %w", err)
	}
	if err == nil {
		backup.HardStateCache = &cache
	}

	return json.MarshalIndent(backup, "", "  ")
}

// Import restores conversation memory from a backup payload.
// Import is idempotent: existing rows are upserted by primary/unique keys.
func (m *ConversationMemory) Import(ctx context.Context, payload json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var backup MemoryBackupPayload
	if err := json.Unmarshal(payload, &backup); err != nil {
		return fmt.Errorf("decode backup payload: %w", err)
	}
	if strings.TrimSpace(backup.ConversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()

	for _, turn := range backup.Turns {
		if strings.TrimSpace(turn.ID) == "" {
			continue
		}
		createdAt := turn.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = m.clock.Now().UTC()
		}
		createdAtStr := createdAt.Format("2006-01-02 15:04:05.000000")
		if turn.TokenCount <= 0 {
			turn.TokenCount = m.countTokens(turn.Content)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_turns (id, conversation_id, role, content, token_count, created_at, tool_calls)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT(id) DO UPDATE SET
				conversation_id = excluded.conversation_id,
				role = excluded.role,
				content = excluded.content,
				token_count = excluded.token_count,
				created_at = excluded.created_at,
				tool_calls = excluded.tool_calls
		`, turn.ID, backup.ConversationID, turn.Role, turn.Content, turn.TokenCount, createdAtStr, turn.ToolCalls); err != nil {
			return fmt.Errorf("import turn %s: %w", turn.ID, err)
		}
	}

	for _, event := range backup.Events {
		if strings.TrimSpace(event.EventType) == "" {
			continue
		}
		createdAt := strings.TrimSpace(event.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO companion_events
				(id, conversation_id, event_type, turn_id, tool_name, tool_run_id, parent_tool_call_id,
				 payload_json, payload_ref, token_count, content_hash, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT(id) DO UPDATE SET
				conversation_id = excluded.conversation_id,
				event_type = excluded.event_type,
				turn_id = excluded.turn_id,
				tool_name = excluded.tool_name,
				tool_run_id = excluded.tool_run_id,
				parent_tool_call_id = excluded.parent_tool_call_id,
				payload_json = excluded.payload_json,
				payload_ref = excluded.payload_ref,
				token_count = excluded.token_count,
				content_hash = excluded.content_hash,
				created_at = excluded.created_at
		`, event.ID, backup.ConversationID, event.EventType, nullableString(event.TurnID), nullableString(event.ToolName),
			nullableString(event.ToolRunID), nullableInt64(event.ParentToolCallID), nullableString(event.PayloadJSON),
			nullableString(event.PayloadRef), nullableInt(event.TokenCount), nullableString(event.ContentHash), createdAt)
		if err != nil {
			return fmt.Errorf("import event %d: %w", event.ID, err)
		}
	}

	for _, entry := range backup.HardStateEntries {
		createdAt := strings.TrimSpace(entry.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_hard_state_entries
				(id, conversation_id, entry_type, key, value_json, status, source_event_id, confidence, metadata_json, supersedes, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT(id) DO UPDATE SET
				conversation_id = excluded.conversation_id,
				entry_type = excluded.entry_type,
				key = excluded.key,
				value_json = excluded.value_json,
				status = excluded.status,
				source_event_id = excluded.source_event_id,
				confidence = excluded.confidence,
				metadata_json = excluded.metadata_json,
				supersedes = excluded.supersedes,
				created_at = excluded.created_at
		`, entry.ID, backup.ConversationID, entry.EntryType, entry.Key, entry.ValueJSON, entry.Status, entry.SourceEventID, entry.Confidence,
			entry.MetadataJSON, entry.Supersedes, createdAt); err != nil {
			return fmt.Errorf("import hard state entry %d: %w", entry.ID, err)
		}
	}

	for _, episode := range backup.SoftEpisodes {
		createdAt := strings.TrimSpace(episode.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_soft_episodes
				(id, conversation_id, episode_type, start_event_id, end_event_id, summary, needs_summary, assumption_ids, token_count, boundary_hash, created_at, deleted_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT(id) DO UPDATE SET
				conversation_id = excluded.conversation_id,
				episode_type = excluded.episode_type,
				start_event_id = excluded.start_event_id,
				end_event_id = excluded.end_event_id,
				summary = excluded.summary,
				needs_summary = excluded.needs_summary,
				assumption_ids = excluded.assumption_ids,
				token_count = excluded.token_count,
				boundary_hash = excluded.boundary_hash,
				created_at = excluded.created_at,
				deleted_at = excluded.deleted_at
		`, episode.ID, backup.ConversationID, episode.EpisodeType, episode.StartEventID, episode.EndEventID, episode.Summary,
			episode.NeedsSummary, episode.AssumptionIDs, episode.TokenCount, episode.BoundaryHash, createdAt, episode.DeletedAt); err != nil {
			return fmt.Errorf("import soft episode %d: %w", episode.ID, err)
		}
	}

	for _, snippet := range backup.EvidenceSnippets {
		createdAt := strings.TrimSpace(snippet.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_evidence_snippets
				(id, conversation_id, source_event_id, event_type, fact_text, content_hash, confidence, bucket, ttl_days, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT(conversation_id, content_hash) DO UPDATE SET
				source_event_id = excluded.source_event_id,
				event_type = excluded.event_type,
				fact_text = excluded.fact_text,
				confidence = excluded.confidence,
				bucket = excluded.bucket,
				ttl_days = excluded.ttl_days,
				created_at = excluded.created_at,
				expires_at = excluded.expires_at
		`, snippet.ID, backup.ConversationID, snippet.SourceEventID, snippet.EventType, snippet.FactText, snippet.ContentHash, snippet.Confidence,
			snippet.Bucket, snippet.TTLDays, createdAt, snippet.ExpiresAt); err != nil {
			return fmt.Errorf("import evidence snippet %d: %w", snippet.ID, err)
		}
	}

	for _, assumption := range backup.Assumptions {
		createdAt := strings.TrimSpace(assumption.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_assumptions_ledger
				(id, conversation_id, assumption, status, reason, source_event_id, confidence, created_at, retracted_at, retracted_by_event_id, retraction_reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT(id) DO UPDATE SET
				conversation_id = excluded.conversation_id,
				assumption = excluded.assumption,
				status = excluded.status,
				reason = excluded.reason,
				source_event_id = excluded.source_event_id,
				confidence = excluded.confidence,
				created_at = excluded.created_at,
				retracted_at = excluded.retracted_at,
				retracted_by_event_id = excluded.retracted_by_event_id,
				retraction_reason = excluded.retraction_reason
		`, assumption.ID, backup.ConversationID, assumption.Assumption, assumption.Status, assumption.Reason, assumption.SourceEventID,
			assumption.Confidence, createdAt, assumption.RetractedAt, assumption.RetractedByEventID, assumption.RetractionReason); err != nil {
			return fmt.Errorf("import assumption %d: %w", assumption.ID, err)
		}
	}

	if backup.OpenEpisode != nil {
		openEpisode := backup.OpenEpisode
		updatedAt := strings.TrimSpace(openEpisode.UpdatedAt)
		if updatedAt == "" {
			updatedAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_open_episode
				(conversation_id, start_event_id, episode_type, event_count, topic_sig, pending_seal_reason, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT(conversation_id) DO UPDATE SET
				start_event_id = excluded.start_event_id,
				episode_type = excluded.episode_type,
				event_count = excluded.event_count,
				topic_sig = excluded.topic_sig,
				pending_seal_reason = excluded.pending_seal_reason,
				updated_at = excluded.updated_at
		`, backup.ConversationID, openEpisode.StartEventID, openEpisode.EpisodeType, openEpisode.EventCount,
			openEpisode.TopicSig, openEpisode.PendingSealReason, updatedAt); err != nil {
			return fmt.Errorf("import open episode: %w", err)
		}
	}

	for _, run := range backup.OpenToolRuns {
		createdAt := strings.TrimSpace(run.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_open_tool_runs
				(conversation_id, tool_run_id, start_event_id, parent_call_event_id, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT(conversation_id, tool_run_id) DO UPDATE SET
				start_event_id = excluded.start_event_id,
				parent_call_event_id = excluded.parent_call_event_id,
				created_at = excluded.created_at
		`, backup.ConversationID, run.ToolRunID, run.StartEventID, run.ParentCallEventID, createdAt); err != nil {
			return fmt.Errorf("import open tool run %q: %w", run.ToolRunID, err)
		}
	}

	for _, entry := range backup.ExtractionStage {
		createdAt := strings.TrimSpace(entry.CreatedAt)
		if createdAt == "" {
			createdAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_extraction_staging
				(id, conversation_id, source_event_id, proposed_entry_type, raw_text, reason, attempt_count, created_at, resolved_at, discarded_at, discard_reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT(id) DO UPDATE SET
				conversation_id = excluded.conversation_id,
				source_event_id = excluded.source_event_id,
				proposed_entry_type = excluded.proposed_entry_type,
				raw_text = excluded.raw_text,
				reason = excluded.reason,
				attempt_count = excluded.attempt_count,
				created_at = excluded.created_at,
				resolved_at = excluded.resolved_at,
				discarded_at = excluded.discarded_at,
				discard_reason = excluded.discard_reason
		`, entry.ID, backup.ConversationID, entry.SourceEventID, entry.ProposedEntryType, entry.RawText, entry.Reason, entry.AttemptCount,
			createdAt, entry.ResolvedAt, entry.DiscardedAt, entry.DiscardReason); err != nil {
			return fmt.Errorf("import extraction staging %d: %w", entry.ID, err)
		}
	}

	if backup.HardStateCache != nil {
		cache := backup.HardStateCache
		updatedAt := strings.TrimSpace(cache.UpdatedAt)
		if updatedAt == "" {
			updatedAt = m.clock.Now().UTC().Format("2006-01-02 15:04:05.000000")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_hard_state_cache
				(conversation_id, compact_json, last_entry_id, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT(conversation_id) DO UPDATE SET
				compact_json = excluded.compact_json,
				last_entry_id = excluded.last_entry_id,
				updated_at = excluded.updated_at
		`, backup.ConversationID, cache.CompactJSON, cache.LastEntryID, updatedAt); err != nil {
			return fmt.Errorf("import hard state cache: %w", err)
		}
	}

	modeState := backup.ModeState
	if modeState == nil {
		modeState = &MemoryModeState{
			ConversationID: backup.ConversationID,
			Mode:           MemoryModeHybrid,
			SchemaVersion:  1,
		}
	}
	if modeState.SchemaVersion <= 0 {
		modeState.SchemaVersion = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO companion_memory_mode_state
			(conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
		VALUES ($1, 'hybrid', $2, $3, $4, $5, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET
			mode = 'hybrid',
			schema_version = excluded.schema_version,
			last_processed_event = excluded.last_processed_event,
			last_soft_event = excluded.last_soft_event,
			last_evidence_event = excluded.last_evidence_event,
			updated_at = CURRENT_TIMESTAMP
	`, backup.ConversationID, modeState.SchemaVersion, modeState.LastProcessedEvent, modeState.LastSoftEvent, modeState.LastEvidenceEvent); err != nil {
		return fmt.Errorf("import mode state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory import: %w", err)
	}

	return nil
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

// SearchCompanionMemories searches v2 hybrid companion memory artifacts for a conversation.
func (m *ConversationMemory) SearchCompanionMemories(ctx context.Context, conversationID, query string, limit int) ([]storage.ScoredEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 10
	}
	like := "%" + query + "%"
	queryTokens := tokenizeForGrounding(query)

	resultByName := map[string]storage.ScoredEntry{}
	addResult := func(r storage.ScoredEntry) {
		existing, ok := resultByName[r.Entry.Name]
		if !ok || r.Score > existing.Score {
			resultByName[r.Entry.Name] = r
		}
	}

	// 1) Hard state entries (facts/preferences/decisions).
	hardRows, err := m.db.QueryContext(ctx, `
		SELECT id, entry_type, key, value_json, confidence
		FROM companion_hard_state_entries
		WHERE conversation_id = $1 AND status = 'active'
			AND (
				lower(key) LIKE lower($2)
				OR lower(value_json) LIKE lower($2)
			)
		ORDER BY confidence DESC, created_at DESC
		LIMIT $3
	`, conversationID, like, limit*3)
	if err != nil {
		return nil, fmt.Errorf("query hard state search: %w", err)
	}
	for hardRows.Next() {
		var (
			id         int64
			entryType  string
			key        string
			valueJSON  string
			confidence float64
		)
		if scanErr := hardRows.Scan(&id, &entryType, &key, &valueJSON, &confidence); scanErr != nil {
			continue
		}
		summary := strings.TrimSpace(valueJSON)
		var asString string
		if err := json.Unmarshal([]byte(valueJSON), &asString); err == nil && strings.TrimSpace(asString) != "" {
			summary = strings.TrimSpace(asString)
		}
		if summary == "" {
			continue
		}
		addResult(storage.ScoredEntry{
			Entry: storage.NamedEntry{
				Name:      fmt.Sprintf("companion:v2:hard_state:%s:%d", conversationID, id),
				Type:      "companion_hard_state",
				Workspace: m.workspace,
				Summary:   summary,
				SessionID: conversationID,
			},
			Score: maxFloat(0.1, confidence),
		})
	}
	_ = hardRows.Close()

	// 2) Episode summaries.
	episodeRows, err := m.db.QueryContext(ctx, `
		SELECT id, summary
		FROM companion_soft_episodes
		WHERE conversation_id = $1
			AND deleted_at IS NULL
			AND needs_summary = 0
			AND summary IS NOT NULL
			AND trim(summary) <> ''
			AND lower(summary) LIKE lower($2)
		ORDER BY end_event_id DESC
		LIMIT $3
	`, conversationID, like, limit*3)
	if err != nil {
		return nil, fmt.Errorf("query episode summary search: %w", err)
	}
	for episodeRows.Next() {
		var (
			id      int64
			summary string
		)
		if scanErr := episodeRows.Scan(&id, &summary); scanErr != nil {
			continue
		}
		if strings.TrimSpace(summary) == "" {
			continue
		}
		addResult(storage.ScoredEntry{
			Entry: storage.NamedEntry{
				Name:      fmt.Sprintf("companion:v2:episode:%s:%d", conversationID, id),
				Type:      "companion_episode",
				Workspace: m.workspace,
				Summary:   summary,
				SessionID: conversationID,
			},
			Score: 0.85,
		})
	}
	_ = episodeRows.Close()

	// 3) Evidence snippets.
	evidenceRows, err := m.db.QueryContext(ctx, `
		SELECT id, fact_text, confidence
		FROM companion_evidence_snippets
		WHERE conversation_id = $1
			AND lower(fact_text) LIKE lower($2)
		ORDER BY confidence DESC, created_at DESC
		LIMIT $3
	`, conversationID, like, limit*3)
	if err != nil {
		return nil, fmt.Errorf("query evidence search: %w", err)
	}
	for evidenceRows.Next() {
		var (
			id         int64
			factText   string
			confidence float64
		)
		if scanErr := evidenceRows.Scan(&id, &factText, &confidence); scanErr != nil {
			continue
		}
		if strings.TrimSpace(factText) == "" {
			continue
		}
		addResult(storage.ScoredEntry{
			Entry: storage.NamedEntry{
				Name:      fmt.Sprintf("companion:v2:evidence:%s:%d", conversationID, id),
				Type:      "companion_evidence",
				Workspace: m.workspace,
				Summary:   factText,
				SessionID: conversationID,
			},
			Score: maxFloat(0.2, confidence),
		})
	}
	_ = evidenceRows.Close()

	if len(resultByName) == 0 && len(queryTokens) > 0 {
		candidateLimit := limit * 10
		if candidateLimit < 50 {
			candidateLimit = 50
		}

		hardRows, err := m.db.QueryContext(ctx, `
			SELECT id, entry_type, key, value_json, confidence
			FROM companion_hard_state_entries
			WHERE conversation_id = $1 AND status = 'active'
			ORDER BY confidence DESC, created_at DESC
			LIMIT $2
		`, conversationID, candidateLimit)
		if err != nil {
			return nil, fmt.Errorf("query hard state token fallback: %w", err)
		}
		for hardRows.Next() {
			var (
				id         int64
				entryType  string
				key        string
				valueJSON  string
				confidence float64
			)
			if scanErr := hardRows.Scan(&id, &entryType, &key, &valueJSON, &confidence); scanErr != nil {
				continue
			}
			summary := decodeCompanionValueSummary(valueJSON)
			if summary == "" {
				continue
			}
			overlap := tokenOverlapCount(queryTokens, tokenizeForGrounding(key+" "+summary))
			if overlap == 0 {
				continue
			}
			ratio := normalizedTokenOverlap(overlap, len(queryTokens))
			addResult(storage.ScoredEntry{
				Entry: storage.NamedEntry{
					Name:      fmt.Sprintf("companion:v2:hard_state:%s:%d", conversationID, id),
					Type:      "companion_hard_state",
					Workspace: m.workspace,
					Summary:   summary,
					SessionID: conversationID,
				},
				Score: minFloat(1.0, maxFloat(0.1, confidence*0.6+ratio*0.4)),
			})
		}
		_ = hardRows.Close()

		episodeRows, err := m.db.QueryContext(ctx, `
			SELECT id, summary
			FROM companion_soft_episodes
			WHERE conversation_id = $1
				AND deleted_at IS NULL
				AND needs_summary = 0
				AND summary IS NOT NULL
				AND trim(summary) <> ''
			ORDER BY end_event_id DESC
			LIMIT $2
		`, conversationID, candidateLimit)
		if err != nil {
			return nil, fmt.Errorf("query episode token fallback: %w", err)
		}
		for episodeRows.Next() {
			var (
				id      int64
				summary string
			)
			if scanErr := episodeRows.Scan(&id, &summary); scanErr != nil {
				continue
			}
			summary = strings.TrimSpace(summary)
			if summary == "" {
				continue
			}
			overlap := tokenOverlapCount(queryTokens, tokenizeForGrounding(summary))
			if overlap == 0 {
				continue
			}
			ratio := normalizedTokenOverlap(overlap, len(queryTokens))
			addResult(storage.ScoredEntry{
				Entry: storage.NamedEntry{
					Name:      fmt.Sprintf("companion:v2:episode:%s:%d", conversationID, id),
					Type:      "companion_episode",
					Workspace: m.workspace,
					Summary:   summary,
					SessionID: conversationID,
				},
				Score: minFloat(0.9, 0.45+ratio*0.4),
			})
		}
		_ = episodeRows.Close()

		evidenceRows, err := m.db.QueryContext(ctx, `
			SELECT id, fact_text, confidence
			FROM companion_evidence_snippets
			WHERE conversation_id = $1
			ORDER BY confidence DESC, created_at DESC
			LIMIT $2
		`, conversationID, candidateLimit)
		if err != nil {
			return nil, fmt.Errorf("query evidence token fallback: %w", err)
		}
		for evidenceRows.Next() {
			var (
				id         int64
				factText   string
				confidence float64
			)
			if scanErr := evidenceRows.Scan(&id, &factText, &confidence); scanErr != nil {
				continue
			}
			factText = strings.TrimSpace(factText)
			if factText == "" {
				continue
			}
			overlap := tokenOverlapCount(queryTokens, tokenizeForGrounding(factText))
			if overlap == 0 {
				continue
			}
			ratio := normalizedTokenOverlap(overlap, len(queryTokens))
			addResult(storage.ScoredEntry{
				Entry: storage.NamedEntry{
					Name:      fmt.Sprintf("companion:v2:evidence:%s:%d", conversationID, id),
					Type:      "companion_evidence",
					Workspace: m.workspace,
					Summary:   factText,
					SessionID: conversationID,
				},
				Score: minFloat(1.0, maxFloat(0.2, confidence*0.4+ratio*0.6)),
			})
		}
		_ = evidenceRows.Close()
	}

	if len(resultByName) == 0 {
		return []storage.ScoredEntry{}, nil
	}

	merged := make([]storage.ScoredEntry, 0, len(resultByName))
	for _, r := range resultByName {
		merged = append(merged, r)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Score == merged[j].Score {
			return merged[i].Entry.Name < merged[j].Entry.Name
		}
		return merged[i].Score > merged[j].Score
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}

	return merged, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func decodeCompanionValueSummary(valueJSON string) string {
	summary := strings.TrimSpace(valueJSON)
	var asString string
	if err := json.Unmarshal([]byte(valueJSON), &asString); err == nil && strings.TrimSpace(asString) != "" {
		summary = strings.TrimSpace(asString)
	}
	return summary
}

func tokenOverlapCount(queryTokens, candidateTokens map[string]struct{}) int {
	if len(queryTokens) == 0 || len(candidateTokens) == 0 {
		return 0
	}
	count := 0
	for token := range queryTokens {
		if _, ok := candidateTokens[token]; ok {
			count++
		}
	}
	return count
}

func normalizedTokenOverlap(overlap, total int) float64 {
	if overlap <= 0 || total <= 0 {
		return 0
	}
	return float64(overlap) / float64(total)
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
