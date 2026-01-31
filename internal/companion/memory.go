// Package companion provides memory management for long-form conversational agents.
package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
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
func NewConversationMemory(db *sql.DB, opts ...MemoryOption) (*ConversationMemory, error) {
	m := &ConversationMemory{
		db:     db,
		config: DefaultMemoryConfig(),
	}

	for _, opt := range opts {
		opt(m)
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
	`

	_, err := m.db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}

	// Migration: Add tool_calls column to existing companion_turns table
	// SQLite doesn't support IF NOT EXISTS for ALTER TABLE, so we check first
	_, _ = m.db.ExecContext(ctx, `ALTER TABLE companion_turns ADD COLUMN tool_calls TEXT`)
	// Ignore error - column may already exist

	return nil
}

// AppendTurn adds a new turn to the conversation.
func (m *ConversationMemory) AppendTurn(ctx context.Context, turn ConversationTurn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if turn.ID == "" {
		turn.ID = generateID()
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now()
	}
	if turn.TokenCount == 0 {
		turn.TokenCount = actormemory.EstimateTokens(turn.Content)
	}

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_turns (id, conversation_id, role, content, token_count, created_at, tool_calls)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, turn.ID, turn.ConversationID, turn.Role, turn.Content, turn.TokenCount, turn.CreatedAt, turn.ToolCalls)
	if err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}

	// Update state
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO companion_memory_state (conversation_id, total_turns, updated_at)
		VALUES (?, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(conversation_id) DO UPDATE SET
			total_turns = total_turns + 1,
			updated_at = CURRENT_TIMESTAMP
	`, turn.ConversationID)

	return err
}

// GetContext builds the memory context for an LLM prompt.
// Returns formatted text combining L0 (vivid) + L1 (recent) + L2 (history).
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
	cutoff := time.Now().Add(-time.Duration(m.config.VividWindowHours) * time.Hour)

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at
		FROM companion_turns
		WHERE conversation_id = ? AND created_at >= ?
		ORDER BY created_at DESC
		LIMIT ?
	`, conversationID, cutoff, m.config.VividMaxTurns)
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
	cutoffDate := time.Now().AddDate(0, 0, -m.config.RecentWindowDays).Format("2006-01-02")
	todayDate := time.Now().Format("2006-01-02")

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, date, turn_count, summary, topics, mood, key_moments, token_count, created_at
		FROM companion_day_summaries
		WHERE conversation_id = ? AND date >= ? AND date < ?
		ORDER BY date DESC
	`, conversationID, cutoffDate, todayDate) // Exclude today (that's in L0)
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
		WHERE conversation_id = ?
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

// RunDailyCompression compresses yesterday's turns into a summary.
// Should be called by a daily cron/daemon.
func (m *ConversationMemory) RunDailyCompression(ctx context.Context, conversationID string) error {
	// Check summarizer without holding lock for the whole operation
	m.mu.Lock()
	if m.summarizer == nil {
		m.mu.Unlock()
		return fmt.Errorf("no summarizer configured")
	}
	summarizer := m.summarizer
	db := m.db
	memoryStore := m.memoryStore
	embedder := m.embedder
	workspace := m.workspace
	m.mu.Unlock()

	now := time.Now()
	loc := now.Location()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	startOfYesterday := startOfToday.AddDate(0, 0, -1)
	endOfYesterday := startOfToday
	// Get yesterday's date in local time
	yesterday := startOfYesterday.Format("2006-01-02")

	// Check if already summarized (DB query doesn't need lock)
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_day_summaries
		WHERE conversation_id = ? AND date = ?
	`, conversationID, yesterday).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // Already summarized
	}

	// Get yesterday's turns (local midnight boundary) - DB query doesn't need lock
	rows, err := db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at
		FROM companion_turns
		WHERE conversation_id = ? AND created_at >= ? AND created_at < ?
		ORDER BY created_at ASC
	`, conversationID, startOfYesterday, endOfYesterday)
	if err != nil {
		return err
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

	if len(turns) == 0 {
		return nil // No turns to summarize
	}

	// Create summary - this is the slow LLM call, done without holding lock
	summary, err := summarizer.SummarizeDay(ctx, turns)
	if err != nil {
		return fmt.Errorf("summarize day: %w", err)
	}

	summary.ConversationID = conversationID
	summary.Date = yesterday
	summary.ID = generateID()
	summary.TurnCount = len(turns)

	// Store summary - DB operations don't need the memory lock
	_, err = db.ExecContext(ctx, `
		INSERT INTO companion_day_summaries
		(id, conversation_id, date, turn_count, summary, topics, mood, key_moments, token_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, summary.ID, summary.ConversationID, summary.Date, summary.TurnCount,
		summary.Summary, joinList(summary.Topics), summary.Mood,
		joinList(summary.KeyMoments), summary.TokenCount)
	if err != nil {
		return fmt.Errorf("store summary: %w", err)
	}

	// Store in named_memory for semantic search (if memory store configured)
	// These operations use local copies captured earlier, no lock needed
	if memoryStore != nil && workspace != "" {
		// Build rich summary text for embedding
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

		// Create result JSON for full data preservation
		resultData, _ := json.Marshal(summary)

		// Memory name for storage and embedding update
		memoryName := fmt.Sprintf("companion:summary:%s:%s", conversationID, summary.Date)

		_, saveErr := memoryStore.SaveResult(ctx, storage.MemorySaveOptions{
			Name:      memoryName,
			Type:      "companion_summary",
			Workspace: workspace,
			SessionID: conversationID, // Use conversation_id as session for filtering
			Summary:   summaryText,
			Result:    resultData,
		})
		if saveErr != nil {
			// Log but don't fail - companion memory table is primary
			// The semantic search is supplementary
			_ = saveErr
		}

		// Generate and store embedding for vector search (if embedder configured)
		// Use dated summary format for better temporal search context
		if embedder != nil && saveErr == nil {
			// Parse date for display (e.g., "[Jan 2, 2006]")
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

	// Update cursor - DB operation doesn't need lock
	_, err = db.ExecContext(ctx, `
		UPDATE companion_memory_state
		SET last_summarized_date = ?, updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = ?
	`, yesterday, conversationID)

	return err
}

// RunWeeklyDistillation distills old summaries into long-term history.
// Should be called weekly or when summaries exceed the recent window.
func (m *ConversationMemory) RunWeeklyDistillation(ctx context.Context, conversationID string) error {
	if m.summarizer == nil {
		return fmt.Errorf("no summarizer configured")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Get summaries older than the recent window
	cutoffDate := time.Now().AddDate(0, 0, -m.config.RecentWindowDays).Format("2006-01-02")

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, date, turn_count, summary, topics, mood, key_moments, token_count, created_at
		FROM companion_day_summaries
		WHERE conversation_id = ? AND date < ?
		ORDER BY date ASC
	`, conversationID, cutoffDate)
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

	// Get existing history
	existing, _ := m.getDistilledHistory(ctx, conversationID)

	// Distill
	history, err := m.summarizer.DistillHistory(ctx, existing, summaries)
	if err != nil {
		return fmt.Errorf("distill history: %w", err)
	}

	history.ConversationID = conversationID
	if history.ID == "" {
		history.ID = generateID()
	}
	history.LastDistilledAt = time.Now()

	// Upsert history
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO companion_history
		(id, conversation_id, relationship_note, recurring_topics, user_preferences,
		 shared_memories, token_count, last_distilled_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
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
			dateStr := time.Now().Format("Jan 2, 2006")
			datedSummary := fmt.Sprintf("[%s] Distilled history: %s", dateStr, summaryText)

			if embedding, embErr := m.embedder.Embed(ctx, datedSummary); embErr == nil && len(embedding.Vec) > 0 {
				_ = m.memoryStore.UpdateEmbedding(ctx, memoryName, m.workspace, embedding.Vec)
			}
		}
	}

	// Delete distilled summaries (they're now in L2)
	var oldestDistilledDate string
	for _, s := range summaries {
		if oldestDistilledDate == "" || s.Date < oldestDistilledDate {
			oldestDistilledDate = s.Date
		}
	}

	_, err = m.db.ExecContext(ctx, `
		DELETE FROM companion_day_summaries
		WHERE conversation_id = ? AND date < ?
	`, conversationID, cutoffDate)
	if err != nil {
		return fmt.Errorf("cleanup summaries: %w", err)
	}

	// Update cursor
	_, err = m.db.ExecContext(ctx, `
		UPDATE companion_memory_state
		SET last_distilled_date = ?, updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = ?
	`, cutoffDate, conversationID)

	return err
}

// GetStats returns memory statistics for a conversation.
func (m *ConversationMemory) GetStats(ctx context.Context, conversationID string) (*MemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &MemoryStats{ConversationID: conversationID}

	// Count turns
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_turns WHERE conversation_id = ?
	`, conversationID).Scan(&stats.TotalTurns); err != nil {
		return nil, fmt.Errorf("count turns: %w", err)
	}

	// Count summaries
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_day_summaries WHERE conversation_id = ?
	`, conversationID).Scan(&stats.DaySummaries); err != nil {
		return nil, fmt.Errorf("count summaries: %w", err)
	}

	// Check if has history
	var historyCount int
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_history WHERE conversation_id = ?
	`, conversationID).Scan(&historyCount); err != nil {
		return nil, fmt.Errorf("count history: %w", err)
	}
	stats.HasDistilledHistory = historyCount > 0

	// Get state (may not exist, so sql.ErrNoRows is acceptable)
	var lastSummarized, lastDistilled sql.NullString
	err := m.db.QueryRowContext(ctx, `
		SELECT last_summarized_date, last_distilled_date
		FROM companion_memory_state WHERE conversation_id = ?
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_turns WHERE conversation_id = ?`, conversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_day_summaries WHERE conversation_id = ?`, conversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_history WHERE conversation_id = ?`, conversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM companion_memory_state WHERE conversation_id = ?`, conversationID); err != nil {
		return err
	}

	return tx.Commit()
}

// Helper functions

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
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
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	MessageCount int    `json:"message_count"`
	LastMessage  string `json:"last_message,omitempty"`
}

// ListConversations returns all conversations with their summaries.
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
			COALESCE(titles.title, '') as title
		FROM companion_turns t
		LEFT JOIN companion_conversation_titles titles ON t.conversation_id = titles.conversation_id
		WHERE t.conversation_id NOT IN (SELECT conversation_id FROM companion_deleted_conversations)
		GROUP BY t.conversation_id
		ORDER BY MAX(t.created_at) DESC
		LIMIT ?
	`

	rows, err := m.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []ConversationSummary
	for rows.Next() {
		var conv ConversationSummary
		var createdAt, updatedAt, title string
		if err := rows.Scan(&conv.ID, &conv.MessageCount, &createdAt, &updatedAt, &title); err != nil {
			return nil, err
		}
		conv.Name = title
		// Parse timestamps and convert to ISO 8601 for frontend compatibility
		conv.CreatedAt = parseAndFormatTimestamp(createdAt)
		conv.UpdatedAt = parseAndFormatTimestamp(updatedAt)
		conversations = append(conversations, conv)
	}

	// Get last message for each conversation
	for i := range conversations {
		lastMsgQuery := `
			SELECT content FROM companion_turns
			WHERE conversation_id = ?
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

// GetConversationMessages retrieves all messages for a specific conversation.
// Returns messages in chronological order (oldest first).
func (m *ConversationMemory) GetConversationMessages(ctx context.Context, conversationID string, limit int) ([]ConversationTurn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at, tool_calls
		FROM companion_turns
		WHERE conversation_id = ?
		ORDER BY created_at ASC
		LIMIT ?
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

// SoftDeleteConversation marks a conversation as deleted without removing the data.
func (m *ConversationMemory) SoftDeleteConversation(ctx context.Context, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO companion_deleted_conversations (conversation_id, deleted_at)
		VALUES (?, CURRENT_TIMESTAMP)
	`, conversationID)
	if err != nil {
		return fmt.Errorf("soft delete conversation: %w", err)
	}

	return nil
}

// RenameConversation sets or updates the custom title for a conversation.
func (m *ConversationMemory) RenameConversation(ctx context.Context, conversationID, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if title == "" {
		// If empty title, delete the custom title entry
		_, err := m.db.ExecContext(ctx, `
			DELETE FROM companion_conversation_titles WHERE conversation_id = ?
		`, conversationID)
		if err != nil {
			return fmt.Errorf("remove conversation title: %w", err)
		}
		return nil
	}

	_, err := m.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO companion_conversation_titles (conversation_id, title, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, conversationID, title)
	if err != nil {
		return fmt.Errorf("rename conversation: %w", err)
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