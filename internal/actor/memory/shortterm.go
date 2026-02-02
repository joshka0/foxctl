package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Config holds memory configuration.
type Config struct {
	// L0 Configuration
	RawBufferSize  int // Turns before L0→L1 summarization
	RawTokenBudget int // Max tokens for L0

	// L1 Configuration
	RecentSummarySize int // Summaries before L1→L2 distillation
	L1TokenBudget     int // Max tokens for L1

	// L2 Configuration
	L2TokenBudget int // Max tokens for L2

	// Total budget
	TotalTokenBudget int // Total short-term memory budget

	// Summarization
	SummarizerModel string // LLM for summarization (e.g., "gemini-flash")
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		RawBufferSize:     5,     // 5 turns before summarization
		RawTokenBudget:    8000,  // 8K tokens for raw turns
		RecentSummarySize: 3,     // 3 summaries before distillation
		L1TokenBudget:     6000,  // 6K tokens for recent summaries
		L2TokenBudget:     4000,  // 4K tokens for distilled history
		TotalTokenBudget:  18000, // 18K total for short-term memory
		SummarizerModel:   "gemini-flash",
	}
}

// ShortTermMemory manages progressive context for actors.
//
// Contract: Durability
// - Raw turns are durable: persisted to sessions.db before processing
// - Compaction is cursor-based: resumable on failure
// - Never delete before persist: summaries must be durably written first
type ShortTermMemory struct {
	config     Config
	db         *sql.DB
	summarizer Summarizer
	redactor   *SecretRedactor
	mu         sync.RWMutex

	// Shutdown coordination
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	wg             sync.WaitGroup
}

// State holds the memory state for an actor.
// This is loaded from DB, not held in memory long-term.
type State struct {
	ActorID   string
	SessionID string
	Task      string // Current task context

	// Cursors (durable, drive compaction)
	NextTurnToSummarize  int
	NextSummaryToDistill int

	// L1/L2 artifact references (CAS)
	L1ArtifactID string
	L2ArtifactID string

	// Metadata
	TotalTurns      int
	TokenEstimate   int
	LastSummarizeAt time.Time
	LastDistillAt   time.Time
	UpdatedAt       time.Time
}

// Turn represents a conversation turn.
type Turn struct {
	Index      int
	Role       string // user, assistant, tool
	Content    string
	ToolCalls  []ToolCall
	Timestamp  time.Time
	TokenCount int
}

// ToolCall represents a tool invocation within a turn.
type ToolCall struct {
	Name   string
	Input  string
	Output string
}

// Summary represents a summarized batch of turns.
type Summary struct {
	TurnRange  TurnRange
	Content    string
	KeyPoints  []string
	Decisions  []string
	TokenCount int
	CreatedAt  time.Time
}

// TurnRange represents a range of turn indices.
type TurnRange struct {
	Start int
	End   int
}

// Summarizer creates summaries from turns.
type Summarizer interface {
	// SummarizeTurns creates a summary from raw turns
	SummarizeTurns(ctx context.Context, task string, turns []Turn) (*Summary, error)

	// DistillSummaries compresses multiple summaries into one
	DistillSummaries(ctx context.Context, task string, summaries []Summary) (string, error)

	// FilterByRelevance scores and filters items by relevance
	FilterByRelevance(ctx context.Context, task string, items []string) ([]string, error)
}

// Option configures ShortTermMemory.
type Option func(*ShortTermMemory)

// WithConfig sets the memory configuration.
func WithConfig(cfg Config) Option {
	return func(m *ShortTermMemory) {
		m.config = cfg
	}
}

// WithSummarizer sets the summarizer.
func WithSummarizer(s Summarizer) Option {
	return func(m *ShortTermMemory) {
		m.summarizer = s
	}
}

// WithRedactor sets the secret redactor.
func WithRedactor(r *SecretRedactor) Option {
	return func(m *ShortTermMemory) {
		m.redactor = r
	}
}

// New creates a new ShortTermMemory.
func New(ctx context.Context, db *sql.DB, opts ...Option) (*ShortTermMemory, error) {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	m := &ShortTermMemory{
		config:         DefaultConfig(),
		db:             db,
		redactor:       NewSecretRedactor(),
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Ensure schema exists
	if err := m.ensureSchema(ctx); err != nil {
		shutdownCancel()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	return m, nil
}

// Close cancels background operations and waits for in-flight compactions to finish.
func (m *ShortTermMemory) Close() {
	if m.shutdownCancel != nil {
		m.shutdownCancel()
	}
	m.wg.Wait()
}

// ensureSchema creates the memory tables if they don't exist.
func (m *ShortTermMemory) ensureSchema(ctx context.Context) error {
	// Memory state table
	stateSchema := `
		CREATE TABLE IF NOT EXISTS actor_memory_state (
			actor_id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL DEFAULT '',
			task_context TEXT,
			next_turn_to_summarize INTEGER DEFAULT 0,
			next_summary_to_distill INTEGER DEFAULT 0,
			l1_artifact_id TEXT,
			l2_artifact_id TEXT,
			total_turns INTEGER DEFAULT 0,
			token_estimate INTEGER DEFAULT 0,
			last_summarize_at TIMESTAMP,
			last_distill_at TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_actor_memory_state_session
		ON actor_memory_state(session_id);

		CREATE INDEX IF NOT EXISTS idx_actor_memory_state_workspace
		ON actor_memory_state(workspace_id);
	`
	if _, err := m.db.ExecContext(ctx, stateSchema); err != nil {
		return fmt.Errorf("create actor_memory_state: %w", err)
	}

	// Turns table (L0 storage)
	turnsSchema := `
		CREATE TABLE IF NOT EXISTS actor_turns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL DEFAULT '',
			turn_index INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT,
			tool_name TEXT,
			tool_input TEXT,
			tool_output TEXT,
			tool_calls TEXT,
			artifact_digest TEXT,
			correlation_id TEXT,
			token_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(actor_id, session_id, turn_index)
		);

		CREATE INDEX IF NOT EXISTS idx_actor_turns_actor_session
		ON actor_turns(actor_id, session_id, turn_index);

		CREATE INDEX IF NOT EXISTS idx_actor_turns_workspace
		ON actor_turns(workspace_id);
	`
	if _, err := m.db.ExecContext(ctx, turnsSchema); err != nil {
		return fmt.Errorf("create actor_turns: %w", err)
	}

	// Summaries table (L1 storage)
	summariesSchema := `
		CREATE TABLE IF NOT EXISTS actor_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			summary_index INTEGER NOT NULL,
			turn_start INTEGER NOT NULL,
			turn_end INTEGER NOT NULL,
			content TEXT NOT NULL,
			key_points TEXT,
			decisions TEXT,
			token_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(actor_id, summary_index)
		);

		CREATE INDEX IF NOT EXISTS idx_actor_summaries_actor
		ON actor_summaries(actor_id);
	`
	if _, err := m.db.ExecContext(ctx, summariesSchema); err != nil {
		return fmt.Errorf("create actor_summaries: %w", err)
	}

	// Context inbox table for hook-injected context
	// Hooks can inject context that will be surfaced in the next actor turn
	inboxSchema := `
		CREATE TABLE IF NOT EXISTS actor_context_inbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			kind TEXT NOT NULL DEFAULT 'context',
			text TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			surfaced_at TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_actor_inbox_actor_session
		ON actor_context_inbox(actor_id, session_id, surfaced_at, created_at);

		CREATE INDEX IF NOT EXISTS idx_actor_inbox_workspace
		ON actor_context_inbox(workspace_id);
	`
	if _, err := m.db.ExecContext(ctx, inboxSchema); err != nil {
		return fmt.Errorf("create actor_context_inbox: %w", err)
	}

	return nil
}

// GetState loads the memory state for an actor.
func (m *ShortTermMemory) GetState(ctx context.Context, actorID string) (*State, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var state State
	var task, l1ID, l2ID sql.NullString
	var lastSummarize, lastDistill sql.NullTime

	err := m.db.QueryRowContext(ctx, `
		SELECT actor_id, session_id, task_context,
		       next_turn_to_summarize, next_summary_to_distill,
		       l1_artifact_id, l2_artifact_id,
		       total_turns, token_estimate,
		       last_summarize_at, last_distill_at, updated_at
		FROM actor_memory_state
		WHERE actor_id = ?
	`, actorID).Scan(
		&state.ActorID, &state.SessionID, &task,
		&state.NextTurnToSummarize, &state.NextSummaryToDistill,
		&l1ID, &l2ID,
		&state.TotalTurns, &state.TokenEstimate,
		&lastSummarize, &lastDistill, &state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Convert NullString/NullTime to values
	state.Task = task.String
	state.L1ArtifactID = l1ID.String
	state.L2ArtifactID = l2ID.String
	state.LastSummarizeAt = lastSummarize.Time
	state.LastDistillAt = lastDistill.Time

	return &state, nil
}

// InitState initializes memory state for a new actor session.
func (m *ShortTermMemory) InitState(ctx context.Context, actorID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO actor_memory_state (actor_id, session_id, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(actor_id) DO UPDATE SET
			session_id = excluded.session_id,
			updated_at = CURRENT_TIMESTAMP
	`, actorID, sessionID)
	return err
}

// SetTaskContext updates the current task context.
func (m *ShortTermMemory) SetTaskContext(ctx context.Context, actorID, task string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx, `
		UPDATE actor_memory_state
		SET task_context = ?, updated_at = CURRENT_TIMESTAMP
		WHERE actor_id = ?
	`, task, actorID)
	return err
}

// AppendTurn adds a new turn and triggers distillation if needed.
//
// Contract: Turn is persisted before any processing.
func (m *ShortTermMemory) AppendTurn(ctx context.Context, actorID string, turn Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get current state to determine turn index
	state, err := m.getStateUnlocked(ctx, actorID)
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("actor %s not initialized", actorID)
	}

	// Redact secrets from content before persistence
	redactedContent := m.redactor.Redact(turn.Content)

	// Serialize tool calls to JSON
	var toolCallsJSON []byte
	if len(turn.ToolCalls) > 0 {
		toolCallsJSON, err = serializeToolCalls(turn.ToolCalls)
		if err != nil {
			return fmt.Errorf("serialize tool calls: %w", err)
		}
	}

	// Persist the turn
	turnIndex := state.TotalTurns
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO actor_turns (actor_id, session_id, turn_index, role, content, tool_calls, token_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, actorID, state.SessionID, turnIndex, turn.Role, redactedContent, toolCallsJSON, turn.TokenCount, turn.Timestamp)
	if err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}

	// Update state
	_, err = m.db.ExecContext(ctx, `
		UPDATE actor_memory_state
		SET total_turns = total_turns + 1,
		    token_estimate = token_estimate + ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE actor_id = ?
	`, turn.TokenCount, actorID)
	if err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	// Check if compaction is needed
	newTotal := state.TotalTurns + 1
	pendingTurns := newTotal - state.NextTurnToSummarize
	if pendingTurns >= m.config.RawBufferSize && m.summarizer != nil {
		// Trigger summarization in background (non-blocking)
		// Use shutdown context so in-flight compactions can be cancelled on shutdown
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.compactL0(m.shutdownCtx, actorID)
		}()
	}

	return nil
}

// getStateUnlocked gets state without locking (caller must hold lock).
func (m *ShortTermMemory) getStateUnlocked(ctx context.Context, actorID string) (*State, error) {
	var state State
	var task, l1ID, l2ID sql.NullString
	var lastSummarize, lastDistill sql.NullTime

	err := m.db.QueryRowContext(ctx, `
		SELECT actor_id, session_id, task_context,
		       next_turn_to_summarize, next_summary_to_distill,
		       l1_artifact_id, l2_artifact_id,
		       total_turns, token_estimate,
		       last_summarize_at, last_distill_at, updated_at
		FROM actor_memory_state
		WHERE actor_id = ?
	`, actorID).Scan(
		&state.ActorID, &state.SessionID, &task,
		&state.NextTurnToSummarize, &state.NextSummaryToDistill,
		&l1ID, &l2ID,
		&state.TotalTurns, &state.TokenEstimate,
		&lastSummarize, &lastDistill, &state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	state.Task = task.String
	state.L1ArtifactID = l1ID.String
	state.L2ArtifactID = l2ID.String
	state.LastSummarizeAt = lastSummarize.Time
	state.LastDistillAt = lastDistill.Time

	return &state, nil
}

// GetTurns retrieves turns in a range for an actor.
func (m *ShortTermMemory) GetTurns(ctx context.Context, actorID string, startIndex, endIndex int) ([]Turn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.QueryContext(ctx, `
		SELECT turn_index, role, content, tool_calls, token_count, created_at
		FROM actor_turns
		WHERE actor_id = ? AND turn_index >= ? AND turn_index < ?
		ORDER BY turn_index ASC
	`, actorID, startIndex, endIndex)
	if err != nil {
		return nil, fmt.Errorf("query turns: %w", err)
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var t Turn
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&t.Index, &t.Role, &t.Content, &toolCallsJSON, &t.TokenCount, &t.Timestamp); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			t.ToolCalls, err = deserializeToolCalls([]byte(toolCallsJSON.String))
			if err != nil {
				return nil, fmt.Errorf("deserialize tool calls: %w", err)
			}
		}
		turns = append(turns, t)
	}

	return turns, rows.Err()
}

// GetRecentTurns retrieves the most recent N turns for an actor.
func (m *ShortTermMemory) GetRecentTurns(ctx context.Context, actorID string, count int) ([]Turn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.QueryContext(ctx, `
		SELECT turn_index, role, content, tool_calls, token_count, created_at
		FROM actor_turns
		WHERE actor_id = ?
		ORDER BY turn_index DESC
		LIMIT ?
	`, actorID, count)
	if err != nil {
		return nil, fmt.Errorf("query recent turns: %w", err)
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var t Turn
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&t.Index, &t.Role, &t.Content, &toolCallsJSON, &t.TokenCount, &t.Timestamp); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			t.ToolCalls, err = deserializeToolCalls([]byte(toolCallsJSON.String))
			if err != nil {
				return nil, fmt.Errorf("deserialize tool calls: %w", err)
			}
		}
		turns = append(turns, t)
	}

	// Reverse to get oldest-first order
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}

	return turns, rows.Err()
}

// compactL0 summarizes pending turns into L1.
// Uses snapshot-based locking to avoid holding the lock during slow operations.
// Retries on optimistic lock failure to handle concurrent compaction races.
func (m *ShortTermMemory) compactL0(ctx context.Context, actorID string) {
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if m.compactL0Once(ctx, actorID) {
			return // Success or no work needed
		}
		// Optimistic lock failed, retry with fresh state
	}
}

// compactL0Once attempts a single compaction pass.
// Returns true if successful or no work needed, false if retry is needed.
func (m *ShortTermMemory) compactL0Once(ctx context.Context, actorID string) bool {
	state, err := m.GetState(ctx, actorID)
	if err != nil || state == nil {
		return true // Can't proceed, don't retry
	}

	startIdx := state.NextTurnToSummarize
	endIdx := state.TotalTurns
	if endIdx-startIdx < m.config.RawBufferSize {
		return true // Not enough turns yet
	}

	// Query turns (no global lock held to avoid blocking Append/GetContext).
	rows, err := m.db.QueryContext(ctx, `
		SELECT turn_index, role, content, tool_calls, token_count, created_at
		FROM actor_turns
		WHERE actor_id = ? AND turn_index >= ? AND turn_index < ?
		ORDER BY turn_index ASC
	`, actorID, startIdx, endIdx)
	if err != nil {
		return true // Query error, don't retry
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var t Turn
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&t.Index, &t.Role, &t.Content, &toolCallsJSON, &t.TokenCount, &t.Timestamp); err != nil {
			continue
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			t.ToolCalls, _ = deserializeToolCalls([]byte(toolCallsJSON.String))
		}
		turns = append(turns, t)
	}

	if len(turns) == 0 {
		return true // No turns to process
	}

	// Summarize turns outside of DB tx
	summary, err := m.summarizer.SummarizeTurns(ctx, state.Task, turns)
	if err != nil {
		return true // Will retry on next turn
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return true // TX error, don't retry
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var summaryIdx int
	if err := tx.QueryRowContext(ctx, `
		SELECT IFNULL(MAX(summary_index), -1) + 1
		FROM actor_summaries
		WHERE actor_id = ?
	`, actorID).Scan(&summaryIdx); err != nil {
		return true // Query error, don't retry
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE actor_memory_state
		SET next_turn_to_summarize = ?,
		    last_summarize_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE actor_id = ? AND next_turn_to_summarize = ?
	`, endIdx, actorID, startIdx)
	if err != nil {
		return true // Update error, don't retry
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil || rowsAffected == 0 {
		return false // Another compaction raced - retry with fresh state
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO actor_summaries (actor_id, session_id, summary_index, turn_start, turn_end, content, key_points, decisions, token_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, actorID, state.SessionID, summaryIdx, startIdx, endIdx-1,
		summary.Content, serializeStrings(summary.KeyPoints), serializeStrings(summary.Decisions),
		summary.TokenCount, summary.CreatedAt)
	if err != nil {
		return true // Insert error, don't retry
	}

	if err := tx.Commit(); err != nil {
		return true // Commit error, don't retry
	}

	// Update local view and consider L1→L2 compaction.
	state.NextTurnToSummarize = endIdx
	// Initialize cursor if it was unset (-1). Do not move it backwards.
	if state.NextSummaryToDistill < 0 {
		state.NextSummaryToDistill = summaryIdx
	}
	m.checkL1Compaction(ctx, actorID, state)
	return true // Success
}

// checkL1Compaction checks if L1→L2 distillation is needed.
func (m *ShortTermMemory) checkL1Compaction(ctx context.Context, actorID string, state *State) {
	if m.summarizer == nil {
		return
	}

	// Count pending summaries
	var count int
	err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM actor_summaries
		WHERE actor_id = ? AND summary_index >= ?
	`, actorID, state.NextSummaryToDistill).Scan(&count)
	if err != nil || count < m.config.RecentSummarySize {
		return
	}

	// Query summaries to distill
	rows, err := m.db.QueryContext(ctx, `
		SELECT summary_index, turn_start, turn_end, content, key_points, decisions, token_count, created_at
		FROM actor_summaries
		WHERE actor_id = ? AND summary_index >= ?
		ORDER BY summary_index ASC
	`, actorID, state.NextSummaryToDistill)
	if err != nil {
		return
	}
	defer rows.Close()

	var summaries []Summary
	var maxSummaryIndex int
	for rows.Next() {
		var s Summary
		var idx int
		var keyPointsStr, decisionsStr sql.NullString
		if err := rows.Scan(&idx, &s.TurnRange.Start, &s.TurnRange.End, &s.Content,
			&keyPointsStr, &decisionsStr, &s.TokenCount, &s.CreatedAt); err != nil {
			continue
		}
		if keyPointsStr.Valid {
			s.KeyPoints = deserializeStrings(keyPointsStr.String)
		}
		if decisionsStr.Valid {
			s.Decisions = deserializeStrings(decisionsStr.String)
		}
		summaries = append(summaries, s)
		maxSummaryIndex = idx
	}

	if len(summaries) == 0 {
		return
	}

	// Distill summaries
	distilled, err := m.summarizer.DistillSummaries(ctx, state.Task, summaries)
	if err != nil {
		return // Will retry on next compaction check
	}

	// Store distilled summary as L2 artifact
	// For MVP, we update l2_artifact_id with the distilled content
	// In a full implementation, this would be stored in CAS
	_, err = m.db.ExecContext(ctx, `
		UPDATE actor_memory_state
		SET l2_artifact_id = ?,
		    next_summary_to_distill = ?,
		    last_distill_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE actor_id = ?
	`, distilled, maxSummaryIndex+1, actorID)
	if err != nil {
		return
	}

	// Clean up old summaries (keep only the most recent one)
	// The distilled content is now in L2, so older summaries can be removed
	if maxSummaryIndex > state.NextSummaryToDistill {
		_, _ = m.db.ExecContext(ctx, `
			DELETE FROM actor_summaries
			WHERE actor_id = ? AND summary_index < ?
		`, actorID, maxSummaryIndex)
	}
}

// GetContext returns formatted short-term memory for LLM.
// Builds context from L2 (distilled) + L1 (recent summaries) + L0 (raw turns).
func (m *ShortTermMemory) GetContext(ctx context.Context, actorID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, err := m.getStateUnlocked(ctx, actorID)
	if err != nil {
		return "", fmt.Errorf("get state: %w", err)
	}
	if state == nil {
		return "", nil // No state initialized
	}

	var parts []string

	// L2: Distilled history
	if state.L2ArtifactID != "" {
		parts = append(parts, "## Session History (Distilled)\n"+state.L2ArtifactID)
	}

	// L1: Recent summaries
	rows, err := m.db.QueryContext(ctx, `
		SELECT turn_start, turn_end, content
		FROM actor_summaries
		WHERE actor_id = ?
		ORDER BY summary_index ASC
	`, actorID)
	if err == nil {
		defer rows.Close()
		var summaryParts []string
		for rows.Next() {
			var start, end int
			var content string
			if err := rows.Scan(&start, &end, &content); err == nil {
				summaryParts = append(summaryParts, fmt.Sprintf("[Turns %d-%d] %s", start, end, content))
			}
		}
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("iterate summaries: %w", err)
		}
		if len(summaryParts) > 0 {
			parts = append(parts, "## Recent Summaries\n"+strings.Join(summaryParts, "\n\n"))
		}
	}

	// L0: Raw turns (most recent)
	recentTurns, err := m.getRecentTurnsUnlocked(ctx, actorID, m.config.RawBufferSize)
	if err == nil && len(recentTurns) > 0 {
		var turnParts []string
		for _, t := range recentTurns {
			turnParts = append(turnParts, fmt.Sprintf("[%s] %s", t.Role, t.Content))
		}
		parts = append(parts, "## Recent Conversation\n"+strings.Join(turnParts, "\n\n"))
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// getRecentTurnsUnlocked retrieves recent turns without locking (caller must hold lock).
func (m *ShortTermMemory) getRecentTurnsUnlocked(ctx context.Context, actorID string, count int) ([]Turn, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT turn_index, role, content, tool_calls, token_count, created_at
		FROM actor_turns
		WHERE actor_id = ?
		ORDER BY turn_index DESC
		LIMIT ?
	`, actorID, count)
	if err != nil {
		return nil, fmt.Errorf("query recent turns: %w", err)
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var t Turn
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&t.Index, &t.Role, &t.Content, &toolCallsJSON, &t.TokenCount, &t.Timestamp); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			t.ToolCalls, err = deserializeToolCalls([]byte(toolCallsJSON.String))
			if err != nil {
				return nil, fmt.Errorf("deserialize tool calls: %w", err)
			}
		}
		turns = append(turns, t)
	}

	// Reverse to get oldest-first order
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}

	return turns, rows.Err()
}

// CompactionRunner runs periodic compaction for all actors.
type CompactionRunner struct {
	memory   *ShortTermMemory
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewCompactionRunner creates a new background compaction runner.
func NewCompactionRunner(memory *ShortTermMemory, interval time.Duration) *CompactionRunner {
	if interval <= 0 {
		interval = 30 * time.Second // Default interval
	}
	return &CompactionRunner{
		memory:   memory,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the background compaction loop.
func (r *CompactionRunner) Start(ctx context.Context) {
	go r.run(ctx)
}

// Stop stops the compaction runner and waits for it to finish.
func (r *CompactionRunner) Stop() {
	close(r.stopCh)
	<-r.doneCh
}

// run is the main compaction loop.
func (r *CompactionRunner) run(ctx context.Context) {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.compactAll(ctx)
		}
	}
}

// compactAll runs compaction for all actors with pending work.
func (r *CompactionRunner) compactAll(ctx context.Context) {
	// Query actors that might need compaction
	rows, err := r.memory.db.QueryContext(ctx, `
		SELECT actor_id FROM actor_memory_state
		WHERE total_turns > next_turn_to_summarize + ?
	`, r.memory.config.RawBufferSize-1)
	if err != nil {
		return
	}
	defer rows.Close()

	var actorIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			actorIDs = append(actorIDs, id)
		}
	}

	// Run compaction for each actor
	for _, actorID := range actorIDs {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		default:
			r.memory.compactL0(ctx, actorID)
		}
	}
}

// RunCompaction manually triggers compaction for a specific actor.
func (m *ShortTermMemory) RunCompaction(ctx context.Context, actorID string) error {
	if m.summarizer == nil {
		return fmt.Errorf("no summarizer configured")
	}

	m.compactL0(ctx, actorID)
	return nil
}

// Clear resets memory state for an actor.
func (m *ShortTermMemory) Clear(ctx context.Context, actorID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Use a transaction to ensure atomic deletion
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Delete turns
	if _, err := tx.ExecContext(ctx, `DELETE FROM actor_turns WHERE actor_id = ?`, actorID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete turns: %w", err)
	}

	// Delete summaries
	if _, err := tx.ExecContext(ctx, `DELETE FROM actor_summaries WHERE actor_id = ?`, actorID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete summaries: %w", err)
	}

	// Delete state
	if _, err := tx.ExecContext(ctx, `DELETE FROM actor_memory_state WHERE actor_id = ?`, actorID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// Export returns full memory state for debugging/inspection.
func (m *ShortTermMemory) Export(ctx context.Context, actorID string) (*State, error) {
	return m.GetState(ctx, actorID)
}

// serializeToolCalls converts tool calls to JSON.
func serializeToolCalls(calls []ToolCall) ([]byte, error) {
	return json.Marshal(calls)
}

// deserializeToolCalls converts JSON back to tool calls.
func deserializeToolCalls(data []byte) ([]ToolCall, error) {
	var calls []ToolCall
	if err := json.Unmarshal(data, &calls); err != nil {
		return nil, err
	}
	return calls, nil
}

// serializeStrings converts a string slice to a comma-separated string.
func serializeStrings(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, "|||")
}

// deserializeStrings converts a comma-separated string back to a slice.
func deserializeStrings(data string) []string {
	if data == "" {
		return nil
	}
	return strings.Split(data, "|||")
}
