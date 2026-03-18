package companion

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// InsertEvent inserts a new event into companion_events.
// For tool_call/tool_result events, payloadJSON MUST be non-nil (defensive invariant).
// For user_message/assistant_message events, payloadJSON and payloadRef MUST be nil.
func (m *ConversationMemory) InsertEvent(ctx context.Context, event *ConversationEvent) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if strings.TrimSpace(event.ConversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}
	eventType := strings.TrimSpace(strings.ToLower(event.EventType))
	if eventType == "" {
		return fmt.Errorf("event_type is required")
	}

	eventType = strings.ToLower(eventType)
	event.EventType = eventType

	isToolEvent := eventType == "tool_call" || eventType == "tool_result"
	isMessageEvent := eventType == "user_message" || eventType == "assistant_message"

	if isToolEvent && len(event.PayloadJSON) == 0 {
		return fmt.Errorf("tool events must include payload_json")
	}
	if isMessageEvent {
		if len(event.PayloadJSON) != 0 {
			return fmt.Errorf("message events must not include payload_json")
		}
		if strings.TrimSpace(event.PayloadRef) != "" {
			return fmt.Errorf("message events must not include payload_ref")
		}
	}
	if !isToolEvent && !isMessageEvent {
		return fmt.Errorf("invalid event_type %q", eventType)
	}

	var parentToolCallID any
	if event.ParentToolCallID != 0 {
		parentToolCallID = event.ParentToolCallID
	}

	// Convert empty strings to nil so SQL sees NULL (required by CHECK constraints).
	var turnID, toolName, toolRunID, payloadJSON, payloadRef, contentHash any
	if s := strings.TrimSpace(event.TurnID); s != "" {
		turnID = s
	}
	if s := strings.TrimSpace(event.ToolName); s != "" {
		toolName = s
	}
	if s := strings.TrimSpace(event.ToolRunID); s != "" {
		toolRunID = s
	}
	if s := strings.TrimSpace(event.PayloadJSON); s != "" {
		payloadJSON = s
	}
	if s := strings.TrimSpace(event.PayloadRef); s != "" {
		payloadRef = s
	}
	if s := strings.TrimSpace(event.ContentHash); s != "" {
		contentHash = s
	}

	var insertedID int64
	if err := m.db.QueryRowContext(ctx, `
		INSERT INTO companion_events (
			conversation_id,
			event_type,
			turn_id,
			tool_name,
			tool_run_id,
			parent_tool_call_id,
			payload_json,
			payload_ref,
			token_count,
			content_hash,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, CURRENT_TIMESTAMP)
		RETURNING id
	`,
		event.ConversationID,
		eventType,
		turnID,
		toolName,
		toolRunID,
		parentToolCallID,
		payloadJSON,
		payloadRef,
		event.TokenCount,
		contentHash,
	).Scan(&insertedID); err != nil {
		return fmt.Errorf("insert companion event: %w", err)
	}
	event.ID = insertedID

	return nil
}

// claimWork claims a contiguous event range for single-writer updates.
// Returns (oldCursor, latestID] when successfully claimed.
func (m *ConversationMemory) claimWork(ctx context.Context, tx *sql.Tx, convID string) (fromEvent, toEvent int64, claimed bool, err error) {
	if strings.TrimSpace(convID) == "" {
		return 0, 0, false, fmt.Errorf("conversation_id is required")
	}

	if err = tx.QueryRowContext(ctx, `
		SELECT last_processed_event
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, convID).Scan(&fromEvent); err != nil {
		if err != sql.ErrNoRows {
			return 0, 0, false, fmt.Errorf("get cursor: %w", err)
		}

		if _, initErr := tx.ExecContext(ctx, `
			INSERT INTO companion_memory_mode_state
				(conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
			VALUES ($1, 'hybrid', 1, 0, 0, 0, CURRENT_TIMESTAMP)
			ON CONFLICT (conversation_id) DO NOTHING
		`, convID); initErr != nil {
			return 0, 0, false, fmt.Errorf("init cursor: %w", initErr)
		}

		if scanErr := tx.QueryRowContext(ctx, `
			SELECT last_processed_event
			FROM companion_memory_mode_state
			WHERE conversation_id = $1
		`, convID).Scan(&fromEvent); scanErr != nil {
			return 0, 0, false, fmt.Errorf("get cursor after init: %w", scanErr)
		}
	}

	if err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(id), 0)
		FROM companion_events
		WHERE conversation_id = $1 AND id > $2
	`, convID, fromEvent).Scan(&toEvent); err != nil {
		return 0, 0, false, fmt.Errorf("get latest event: %w", err)
	}
	if toEvent == 0 {
		return fromEvent, 0, false, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE companion_memory_mode_state
		SET last_processed_event = $1, updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = $2 AND last_processed_event = $3
	`, toEvent, convID, fromEvent)
	if err != nil {
		return 0, 0, false, fmt.Errorf("claim cursor: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, 0, false, fmt.Errorf("cursor claim rows affected: %w", err)
	}
	if rows == 0 {
		return 0, 0, false, nil
	}

	return fromEvent, toEvent, true, nil
}

func containsPreference(text string) bool {
	for _, signal := range []string{"i prefer", "i always", "i never", "i don't like", "don't like", "i like", "i enjoy"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

// hasMemoryWorthySignals checks if a message should pass Tier 1 extraction.
// tool_result events ALWAYS return true.
func hasMemoryWorthySignals(event ConversationEvent) bool {
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if eventType == "tool_result" {
		return true
	}
	if eventType != "user_message" && eventType != "assistant_message" {
		return false
	}

	text := strings.ToLower(event.Content)
	if containsPreference(text) {
		return true
	}
	if containsDecision(text) {
		return true
	}
	if containsQuestion(text) {
		return true
	}
	if containsDefinition(text) {
		return true
	}
	if containsGoalChange(text) {
		return true
	}
	return containsRetraction(text)
}

func containsDecision(text string) bool {
	for _, signal := range []string{"let's go with", "decided to", "the plan is", "we will", "we're going to", "i've decided", "i decided", "let us", "let's do"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsQuestion(text string) bool {
	if strings.HasSuffix(text, "?") {
		return true
	}
	for _, signal := range []string{"what about", "should we", "could we", "can we", "what if", "how about", "when do", "where should"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsDefinition(text string) bool {
	for _, signal := range []string{" means", " by", " i mean", " is when", "defined as", "is called"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsGoalChange(text string) bool {
	for _, signal := range []string{"the goal is", "we need to", "let's focus", "from now on", "going forward", "new objective", "change the goal", "priority is"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsRetraction(text string) bool {
	for _, signal := range []string{"actually no", "that was wrong", "forget that", "disregard", "scratch that", "nevermind", "i retract", "i take it back"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

// BuildHybridContextLayers is the main hybrid pipeline entry point.
// It claims a range of events, processes Tier 0 and Tier 1 work, and returns.
func (m *ConversationMemory) BuildHybridContextLayers(ctx context.Context, conversationID string) error {
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	if err := m.EnsureHybridMode(ctx, conversationID); err != nil {
		return fmt.Errorf("ensure hybrid mode: %w", err)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pipeline tx: %w", err)
	}
	defer tx.Rollback()

	fromEvent, toEvent, claimed, err := m.claimWork(ctx, tx, conversationID)
	if err != nil {
		return fmt.Errorf("claim work: %w", err)
	}
	if !claimed {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			e.id,
			e.conversation_id,
			e.event_type,
			e.turn_id,
			e.tool_name,
			e.tool_run_id,
			e.parent_tool_call_id,
			e.payload_json,
			e.payload_ref,
			e.token_count,
			e.content_hash,
			t.content
		FROM companion_events e
		LEFT JOIN companion_turns t
			on t.id = e.turn_id
		WHERE e.conversation_id = $1 AND e.id > $2 AND e.id <= $3
		ORDER BY e.id
	`, conversationID, fromEvent, toEvent)
	if err != nil {
		return fmt.Errorf("query event batch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var event ConversationEvent
		var (
			turnID         sql.NullString
			toolName       sql.NullString
			toolRunID      sql.NullString
			parentToolCall sql.NullInt64
			payloadJSON    sql.NullString
			payloadRef     sql.NullString
			contentHash    sql.NullString
			turnContent    sql.NullString
		)

		if err := rows.Scan(
			&event.ID,
			&event.ConversationID,
			&event.EventType,
			&turnID,
			&toolName,
			&toolRunID,
			&parentToolCall,
			&payloadJSON,
			&payloadRef,
			&event.TokenCount,
			&contentHash,
			&turnContent,
		); err != nil {
			return fmt.Errorf("scan event: %w", err)
		}

		if turnID.Valid {
			event.TurnID = turnID.String
		}
		if toolName.Valid {
			event.ToolName = toolName.String
		}
		if toolRunID.Valid {
			event.ToolRunID = toolRunID.String
		}
		if parentToolCall.Valid {
			event.ParentToolCallID = parentToolCall.Int64
		}
		if payloadJSON.Valid {
			event.PayloadJSON = payloadJSON.String
		}
		if payloadRef.Valid {
			event.PayloadRef = payloadRef.String
		}
		if contentHash.Valid {
			event.ContentHash = contentHash.String
		}
		if turnContent.Valid {
			event.Content = turnContent.String
		}

		if err := m.processEventTier0(ctx, tx, conversationID, event); err != nil {
			return fmt.Errorf("process tier0: %w", err)
		}

		if hasMemoryWorthySignals(event) {
			if err := m.processEventTier1(ctx, tx, conversationID, event); err != nil {
				return fmt.Errorf("process tier1: %w", err)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan events: %w", err)
	}

	return tx.Commit()
}

// processEventTier0 handles Tier 0 bookkeeping: open episode state, tool runs, boundary checks.
func (m *ConversationMemory) processEventTier0(ctx context.Context, tx *sql.Tx, convID string, event ConversationEvent) error {
	if err := m.updateOpenEpisodeState(ctx, tx, convID, event); err != nil {
		return fmt.Errorf("update open episode state: %w", err)
	}

	reason, err := m.checkEpisodeBoundary(ctx, tx, convID, event)
	if err != nil {
		return fmt.Errorf("check episode boundary: %w", err)
	}
	if reason != "" {
		if err := m.handleEpisodeSeal(ctx, tx, convID, reason); err != nil {
			return fmt.Errorf("handle episode seal: %w", err)
		}
	}

	return nil
}

// processEventTier1 handles Tier 1 extraction: hard state, evidence, assumptions.
func (m *ConversationMemory) processEventTier1(ctx context.Context, tx *sql.Tx, convID string, event ConversationEvent) error {
	text := strings.TrimSpace(event.Content)
	if text == "" {
		return nil
	}

	extractions := extractProfileClaims(text)
	extractions = append(extractions, extractExplicitFacts(text)...)
	extractions = append(extractions, extractDecisions(text)...)
	extractions = append(extractions, extractOpenQuestions(text)...)
	if goal := extractGoalChange(text); goal != nil {
		extractions = append(extractions, *goal)
	}
	extractions = append(extractions, extractRetractions(text)...)

	for _, entry := range extractions {
		if err := m.persistDeterministicExtraction(ctx, tx, convID, event.ID, entry); err != nil {
			return fmt.Errorf("persist extraction: %w", err)
		}
	}

	return nil
}

// GetMemoryMode returns the current memory mode for a conversation.
// Companion memory is hybrid-only; missing mode state is treated as hybrid.
func (m *ConversationMemory) GetMemoryMode(ctx context.Context, conversationID string) (string, error) {
	if strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation_id is required")
	}

	var mode string
	err := m.db.QueryRowContext(ctx, `
		SELECT mode
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, conversationID).Scan(&mode)
	if err == nil {
		return MemoryModeHybrid, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("get memory mode state: %w", err)
	}

	return MemoryModeHybrid, nil
}

// EnsureHybridMode initializes hybrid mode for a conversation if not already set.
func (m *ConversationMemory) EnsureHybridMode(ctx context.Context, conversationID string) error {
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_memory_mode_state
			(conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
		VALUES ($1, 'hybrid', 1, 0, 0, 0, CURRENT_TIMESTAMP)
		ON CONFLICT (conversation_id) DO UPDATE SET
			mode = 'hybrid',
			updated_at = CURRENT_TIMESTAMP
	`, conversationID)
	if err != nil {
		return fmt.Errorf("ensure hybrid mode: %w", err)
	}

	return nil
}
