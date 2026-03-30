package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AnchoredInteractionResolution describes the interaction outcome after follow-up.
type AnchoredInteractionResolution string

const (
	InteractionResolutionResolved   AnchoredInteractionResolution = "resolved"
	InteractionResolutionCorrected  AnchoredInteractionResolution = "corrected"
	InteractionResolutionContinues  AnchoredInteractionResolution = "continues"
	InteractionResolutionUnresolved AnchoredInteractionResolution = "unresolved"
)

// FollowUpReactionOutcome captures the observed follow-up posture.
type FollowUpReactionOutcome string

const (
	ReactionOutcomeAccepted   FollowUpReactionOutcome = "accepted"
	ReactionOutcomeCorrected  FollowUpReactionOutcome = "corrected"
	ReactionOutcomeFrustrated FollowUpReactionOutcome = "frustrated"
	ReactionOutcomeConfused   FollowUpReactionOutcome = "confused"
	ReactionOutcomeNeutral    FollowUpReactionOutcome = "neutral"
	ReactionOutcomeUnresolved FollowUpReactionOutcome = "unresolved"
)

// AnchorStateFact is a compact hard-state fact visible before an interaction.
type AnchorStateFact struct {
	EntryType     string  `json:"entry_type"`
	Key           string  `json:"key"`
	Value         string  `json:"value"`
	SourceEventID int64   `json:"source_event_id"`
	Confidence    float64 `json:"confidence"`
}

// AnchorStateSnapshot captures what was known before the user event.
type AnchorStateSnapshot struct {
	BeforeEventID      int64             `json:"before_event_id"`
	HardState          []AnchorStateFact `json:"hard_state,omitempty"`
	ActiveAssumptions  []string          `json:"active_assumptions,omitempty"`
	OpenQuestions      []string          `json:"open_questions,omitempty"`
	Goals              []string          `json:"goals,omitempty"`
	RecentToolReceipts []string          `json:"recent_tool_receipts,omitempty"`
}

// FollowUpReaction summarizes observable follow-up cues and a conservative inferred affect.
type FollowUpReaction struct {
	Outcome        FollowUpReactionOutcome `json:"outcome"`
	ObservedCues   []string                `json:"observed_cues,omitempty"`
	InferredAffect string                  `json:"inferred_affect,omitempty"`
	Confidence     float64                 `json:"confidence,omitempty"`
}

// AnchoredInteractionFrame is the primary evaluation unit for auto-memory derivation.
type AnchoredInteractionFrame struct {
	ConversationID string                        `json:"conversation_id"`
	AnchorState    AnchorStateSnapshot           `json:"anchor_state"`
	UserEvent      ConversationEvent             `json:"user_event"`
	AssistantEvent ConversationEvent             `json:"assistant_event"`
	ToolReceipts   []string                      `json:"tool_receipts,omitempty"`
	FollowUpUser   *ConversationEvent            `json:"followup_user_event,omitempty"`
	Resolution     AnchoredInteractionResolution `json:"resolution"`
	Reaction       FollowUpReaction              `json:"reaction"`
}

// BuildAnchoredInteractionFrames compiles ordered interaction triples anchored to prior state.
func (m *ConversationMemory) BuildAnchoredInteractionFrames(ctx context.Context, conversationID string, limit int) ([]AnchoredInteractionFrame, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	events, err := m.loadMessageEvents(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if len(events) < 2 {
		return nil, nil
	}

	frames := make([]AnchoredInteractionFrame, 0, len(events)/2)
	for i := 0; i+1 < len(events); i++ {
		if events[i].EventType != EventTypeUserMessage || events[i+1].EventType != EventTypeAssistantMessage {
			continue
		}

		anchorState, err := m.buildAnchorStateSnapshot(ctx, conversationID, events[i].ID)
		if err != nil {
			return nil, err
		}
		toolReceipts, err := m.getToolReceiptsBetweenEvents(ctx, conversationID, events[i].ID, events[i+1].ID, 4)
		if err != nil {
			return nil, err
		}

		var followUp *ConversationEvent
		if i+2 < len(events) && events[i+2].EventType == EventTypeUserMessage {
			followUp = &events[i+2]
		}
		reaction := inferFollowUpReaction(followUp)

		frame := AnchoredInteractionFrame{
			ConversationID: conversationID,
			AnchorState:    anchorState,
			UserEvent:      events[i],
			AssistantEvent: events[i+1],
			ToolReceipts:   toolReceipts,
			FollowUpUser:   followUp,
			Resolution:     classifyInteractionResolution(followUp, reaction),
			Reaction:       reaction,
		}
		frames = append(frames, frame)
	}

	if limit > 0 && len(frames) > limit {
		frames = frames[len(frames)-limit:]
	}
	return frames, nil
}

func (m *ConversationMemory) loadMessageEvents(ctx context.Context, conversationID string) ([]ConversationEvent, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT e.id, e.conversation_id, e.event_type, e.turn_id, t.content, e.created_at
		FROM companion_events e
		LEFT JOIN companion_turns t ON t.id = e.turn_id
		WHERE e.conversation_id = $1
			AND e.event_type IN ('user_message', 'assistant_message')
		ORDER BY e.id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("query message events: %w", err)
	}
	defer rows.Close()

	var events []ConversationEvent
	for rows.Next() {
		var event ConversationEvent
		var turnID sql.NullString
		var content sql.NullString
		if err := rows.Scan(&event.ID, &event.ConversationID, &event.EventType, &turnID, &content, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message event: %w", err)
		}
		if turnID.Valid {
			event.TurnID = turnID.String
		}
		if content.Valid {
			event.Content = strings.TrimSpace(content.String)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message events: %w", err)
	}
	return events, nil
}

func (m *ConversationMemory) buildAnchorStateSnapshot(ctx context.Context, conversationID string, beforeEventID int64) (AnchorStateSnapshot, error) {
	hardState, err := m.materializeHardStateBeforeEvent(ctx, conversationID, beforeEventID)
	if err != nil {
		return AnchorStateSnapshot{}, err
	}
	assumptions, err := m.getAssumptionsBeforeEvent(ctx, conversationID, beforeEventID)
	if err != nil {
		return AnchorStateSnapshot{}, err
	}
	toolReceipts, err := m.getRecentToolReceiptsBeforeEvent(ctx, conversationID, beforeEventID, 3)
	if err != nil {
		return AnchorStateSnapshot{}, err
	}

	snapshot := AnchorStateSnapshot{
		BeforeEventID:      beforeEventID,
		ActiveAssumptions:  assumptions,
		RecentToolReceipts: toolReceipts,
	}
	for _, fact := range hardState {
		snapshot.HardState = append(snapshot.HardState, fact)
		switch fact.EntryType {
		case EntryTypeOpenQuestion:
			snapshot.OpenQuestions = append(snapshot.OpenQuestions, fact.Value)
		case EntryTypeGoal:
			snapshot.Goals = append(snapshot.Goals, fact.Value)
		}
	}
	return snapshot, nil
}

func (m *ConversationMemory) materializeHardStateBeforeEvent(ctx context.Context, conversationID string, beforeEventID int64) ([]AnchorStateFact, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT entry_type, key, value_json, source_event_id, confidence
		FROM (
			SELECT entry_type, key, value_json, status, source_event_id, confidence,
			       ROW_NUMBER() OVER (PARTITION BY entry_type, key ORDER BY id DESC) AS rn
			FROM companion_hard_state_entries
			WHERE conversation_id = $1
				AND source_event_id < $2
		) sub
		WHERE rn = 1 AND status = 'active'
	`, conversationID, beforeEventID)
	if err != nil {
		return nil, fmt.Errorf("query hard state snapshot: %w", err)
	}
	defer rows.Close()

	var facts []AnchorStateFact
	for rows.Next() {
		var (
			fact       AnchorStateFact
			valueJSON  sql.NullString
			sourceID   int64
			confidence float64
		)
		if err := rows.Scan(&fact.EntryType, &fact.Key, &valueJSON, &sourceID, &confidence); err != nil {
			return nil, fmt.Errorf("scan hard state snapshot: %w", err)
		}
		fact.Value = decodeJSONStringValue(valueJSON.String)
		fact.SourceEventID = sourceID
		fact.Confidence = confidence
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hard state snapshot: %w", err)
	}

	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].EntryType == facts[j].EntryType {
			return facts[i].Key < facts[j].Key
		}
		return facts[i].EntryType < facts[j].EntryType
	})
	return facts, nil
}

func (m *ConversationMemory) getAssumptionsBeforeEvent(ctx context.Context, conversationID string, beforeEventID int64) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT assumption
		FROM companion_assumptions_ledger
		WHERE conversation_id = $1
			AND source_event_id < $2
			AND (retracted_by_event_id IS NULL OR retracted_by_event_id >= $2)
		ORDER BY created_at DESC, id DESC
	`, conversationID, beforeEventID)
	if err != nil {
		return nil, fmt.Errorf("query assumptions snapshot: %w", err)
	}
	defer rows.Close()

	var assumptions []string
	for rows.Next() {
		var assumption string
		if err := rows.Scan(&assumption); err != nil {
			return nil, fmt.Errorf("scan assumption snapshot: %w", err)
		}
		assumption = strings.TrimSpace(assumption)
		if assumption != "" {
			assumptions = append(assumptions, assumption)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assumptions snapshot: %w", err)
	}
	return assumptions, nil
}

func (m *ConversationMemory) getRecentToolReceiptsBeforeEvent(ctx context.Context, conversationID string, beforeEventID int64, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT event_type, tool_name, payload_json
		FROM companion_events
		WHERE conversation_id = $1
			AND id < $2
			AND event_type IN ('tool_call', 'tool_result')
		ORDER BY id DESC
		LIMIT $3
	`, conversationID, beforeEventID, limit)
	if err != nil {
		return nil, fmt.Errorf("query tool receipts snapshot: %w", err)
	}
	defer rows.Close()

	var receipts []string
	for rows.Next() {
		var eventType sql.NullString
		var toolName sql.NullString
		var payloadJSON sql.NullString
		if err := rows.Scan(&eventType, &toolName, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan tool receipt snapshot: %w", err)
		}
		receipt := summarizeToolReceipt(strings.TrimSpace(eventType.String), strings.TrimSpace(toolName.String), strings.TrimSpace(payloadJSON.String))
		if receipt != "" {
			receipts = append(receipts, receipt)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool receipt snapshot: %w", err)
	}
	return receipts, nil
}

func (m *ConversationMemory) getToolReceiptsBetweenEvents(ctx context.Context, conversationID string, afterEventID, beforeEventID int64, limit int) ([]string, error) {
	if limit <= 0 || beforeEventID <= afterEventID {
		return nil, nil
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT event_type, tool_name, payload_json
		FROM companion_events
		WHERE conversation_id = $1
			AND id > $2
			AND id < $3
			AND event_type IN ('tool_call', 'tool_result')
		ORDER BY id ASC
		LIMIT $4
	`, conversationID, afterEventID, beforeEventID, limit)
	if err != nil {
		return nil, fmt.Errorf("query tool receipts between events: %w", err)
	}
	defer rows.Close()

	var receipts []string
	for rows.Next() {
		var eventType sql.NullString
		var toolName sql.NullString
		var payloadJSON sql.NullString
		if err := rows.Scan(&eventType, &toolName, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan tool receipt between events: %w", err)
		}
		receipt := summarizeToolReceipt(strings.TrimSpace(eventType.String), strings.TrimSpace(toolName.String), strings.TrimSpace(payloadJSON.String))
		if receipt != "" {
			receipts = append(receipts, receipt)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool receipts between events: %w", err)
	}
	return receipts, nil
}

func decodeJSONStringValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err == nil {
		return strings.TrimSpace(s)
	}
	return raw
}

func summarizeToolReceipt(eventType, toolName, payloadJSON string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" && payloadJSON == "" {
		return ""
	}

	parts := make([]string, 0, 2)
	if eventType != "" {
		parts = append(parts, eventType)
	}
	if toolName != "" {
		parts = append(parts, toolName)
	}

	preview := summarizeJSONPreview(payloadJSON)
	if preview != "" {
		parts = append(parts, preview)
	}
	return strings.Join(parts, ": ")
}

func summarizeJSONPreview(payloadJSON string) string {
	payloadJSON = strings.TrimSpace(payloadJSON)
	if payloadJSON == "" {
		return ""
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &obj); err == nil {
		for _, key := range []string{"summary", "status", "message", "result", "note"} {
			if v, ok := obj[key]; ok {
				if text := strings.TrimSpace(fmt.Sprint(v)); text != "" {
					return truncateInline(text, 80)
				}
			}
		}
	}
	return truncateInline(payloadJSON, 80)
}

func truncateInline(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
}

func classifyInteractionResolution(followUp *ConversationEvent, reaction FollowUpReaction) AnchoredInteractionResolution {
	if followUp == nil {
		return InteractionResolutionUnresolved
	}
	switch reaction.Outcome {
	case ReactionOutcomeAccepted:
		return InteractionResolutionResolved
	case ReactionOutcomeCorrected:
		return InteractionResolutionCorrected
	case ReactionOutcomeFrustrated, ReactionOutcomeConfused:
		return InteractionResolutionUnresolved
	default:
		return InteractionResolutionContinues
	}
}

func inferFollowUpReaction(followUp *ConversationEvent) FollowUpReaction {
	if followUp == nil {
		return FollowUpReaction{Outcome: ReactionOutcomeUnresolved}
	}

	raw := strings.TrimSpace(followUp.Content)
	if raw == "" {
		return FollowUpReaction{Outcome: ReactionOutcomeNeutral}
	}
	if strings.Contains(raw, "<article>") || len(raw) > 1500 {
		return FollowUpReaction{
			Outcome:    ReactionOutcomeNeutral,
			Confidence: 0.25,
		}
	}
	text := strings.ToLower(truncateInline(raw, 320))

	cues := make([]string, 0, 3)
	if containsAny(text, "no,", "no.", "that's wrong", "that is wrong", "actually", "instead", "i meant", "not what i asked", "still broken", "still not", "already told") {
		cues = append(cues, "correction")
	}
	if containsAny(text, "frustrated", "annoyed", "irritated", "come on", "again", "still broken", "already told", "why are you", "this is taking too long") {
		cues = append(cues, "frustration")
	}
	if containsAny(text, "i don't understand", "what do you mean", "unclear", "confused", "why?", "huh") {
		cues = append(cues, "confusion")
	}
	if containsAny(text, "thanks", "that works", "this works", "perfect", "great", "exactly", "looks good", "nice", "let's do it", "lets do it", "let's try it", "lets try it", "go ahead", "works for me") {
		cues = append(cues, "acceptance")
	}
	if strings.HasPrefix(text, "okay yeah this works") {
		cues = append(cues, "acceptance")
	}

	switch {
	case hasCue(cues, "correction") && hasCue(cues, "frustration"):
		return FollowUpReaction{
			Outcome:        ReactionOutcomeFrustrated,
			ObservedCues:   cues,
			InferredAffect: "frustration",
			Confidence:     0.82,
		}
	case hasCue(cues, "correction"):
		return FollowUpReaction{
			Outcome:      ReactionOutcomeCorrected,
			ObservedCues: cues,
			Confidence:   0.8,
		}
	case hasCue(cues, "confusion"):
		return FollowUpReaction{
			Outcome:        ReactionOutcomeConfused,
			ObservedCues:   cues,
			InferredAffect: "confusion",
			Confidence:     0.72,
		}
	case hasCue(cues, "acceptance"):
		return FollowUpReaction{
			Outcome:      ReactionOutcomeAccepted,
			ObservedCues: cues,
			Confidence:   0.82,
		}
	default:
		return FollowUpReaction{
			Outcome:      ReactionOutcomeNeutral,
			ObservedCues: cues,
			Confidence:   0.4,
		}
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func hasCue(cues []string, target string) bool {
	for _, cue := range cues {
		if cue == target {
			return true
		}
	}
	return false
}
