package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	"github.com/jkatigb/agentctl/internal/runtime/engine"
	"github.com/rs/zerolog"
)

type episodeSummaryLLMResponse struct {
	Summary          string                         `json:"summary"`
	HardStateEntries []episodeSummaryHardStateEntry `json:"hard_state_entries"`
	EvidenceSnippets []episodeSummaryEvidenceItem   `json:"evidence_snippets"`
}

type episodeSummaryHardStateEntry struct {
	EntryType     string  `json:"entry_type"`
	RawText       string  `json:"raw_text"`
	Value         string  `json:"value,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
	SourceEventID int64   `json:"source_event_id,omitempty"`
}

type episodeSummaryEvidenceItem struct {
	SourceEventID int64   `json:"source_event_id"`
	EventType     string  `json:"event_type,omitempty"`
	FactText      string  `json:"fact_text"`
	Confidence    float64 `json:"confidence,omitempty"`
	Bucket        string  `json:"bucket,omitempty"`
	TTLDays       *int    `json:"ttl_days,omitempty"`
}

// EpisodeSummaryPlan holds the parsed LLM output ready for persistence.
// It separates planning (LLM call + parsing) from application (DB writes),
// enabling dry-run mode and independent testing of the parsing logic.
type EpisodeSummaryPlan struct {
	ConversationID   string                    `json:"conversation_id"`
	EpisodeID        int64                     `json:"episode_id"`
	StartEventID     int64                     `json:"start_event_id"`
	EndEventID       int64                     `json:"end_event_id"`
	Summary          string                    `json:"summary"`
	TokenCount       int                       `json:"token_count"`
	HardStateEntries []HardStateEntryPlanItem  `json:"hard_state_entries"`
	EvidenceSnippets []EvidenceSnippetPlanItem `json:"evidence_snippets"`
}

// HardStateEntryPlanItem is a planned hard-state extraction with its resolved source event.
type HardStateEntryPlanItem struct {
	SourceEventID int64          `json:"source_event_id"`
	Entry         ExtractedEntry `json:"entry"`
}

// EvidenceSnippetPlanItem is a planned evidence snippet ready for persistence.
type EvidenceSnippetPlanItem struct {
	SourceEventID int64   `json:"source_event_id"`
	EventType     string  `json:"event_type"`
	FactText      string  `json:"fact_text"`
	Confidence    float64 `json:"confidence"`
	Bucket        string  `json:"bucket"`
	TTLDays       *int    `json:"ttl_days,omitempty"`
}

type episodeTranscriptEvent struct {
	ID          int64
	EventType   string
	TurnRole    string
	TurnContent string
	PayloadJSON string
}

// SummarizeEpisode runs the full plan+apply pipeline and returns the summary text.
// It satisfies the summarizerA interface used by daemon.go.
func (m *ConversationMemory) SummarizeEpisode(ctx context.Context, conversationID string, episodeID, startEventID, endEventID int64) (string, int, error) {
	plan, err := m.SummarizeEpisodePlan(ctx, conversationID, episodeID, startEventID, endEventID)
	if err != nil {
		return "", 0, err
	}
	if err := m.ApplyEpisodeSummaryPlan(ctx, plan, false); err != nil {
		return "", 0, err
	}
	return plan.Summary, plan.TokenCount, nil
}

// SummarizeEpisodePlan runs the LLM call and parses its output into a plan
// without performing any DB writes. The returned plan can be inspected,
// serialized, or passed to ApplyEpisodeSummaryPlan.
func (m *ConversationMemory) SummarizeEpisodePlan(ctx context.Context, conversationID string, episodeID, startEventID, endEventID int64) (*EpisodeSummaryPlan, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if episodeID <= 0 {
		return nil, fmt.Errorf("episode_id must be positive")
	}
	if startEventID <= 0 {
		return nil, fmt.Errorf("start_event_id must be positive")
	}
	if endEventID <= 0 {
		return nil, fmt.Errorf("end_event_id must be positive")
	}
	if endEventID < startEventID {
		return nil, fmt.Errorf("end_event_id must be >= start_event_id")
	}

	m.mu.RLock()
	llmSummarizer := m.llmSummarizer
	m.mu.RUnlock()
	if llmSummarizer == nil {
		return nil, fmt.Errorf("no llm summarizer configured")
	}

	events, err := loadEpisodeEvents(ctx, m.db, conversationID, startEventID, endEventID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("no episode events found for summary")
	}

	transcript := formatEpisodeTranscript(events)
	prompt := buildEpisodeSummaryPrompt(conversationID, episodeID, startEventID, endEventID, transcript)

	cfg := engine.LLMChatConfig{
		Provider:      llmSummarizer.provider,
		APIKey:        llmSummarizer.apiKey,
		BaseURL:       llmSummarizer.baseURL,
		AuthMode:      llmSummarizer.authMode,
		AuthHeader:    llmSummarizer.authHeader,
		AuthPrefix:    llmSummarizer.authPrefix,
		Model:         llmSummarizer.model,
		MaxIterations: 1,
		Temperature:   0.2,
		MaxTokens:     2000,
	}
	input := engine.EngineInput{
		SystemPrompt: "You summarize conversation episodes and extract structured memory. Return only valid JSON.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	}

	output, err := m.episodeSummaryRunner(ctx, cfg, input)
	if err != nil {
		return nil, fmt.Errorf("run episode summarizer engine: %w", err)
	}
	if output.StopReason == engine.StopReasonError {
		return nil, fmt.Errorf("episode summarizer LLM error: %s", output.Error)
	}

	responseText := strings.TrimSpace(output.AssistantText)
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)

	plan := &EpisodeSummaryPlan{
		ConversationID: conversationID,
		EpisodeID:      episodeID,
		StartEventID:   startEventID,
		EndEventID:     endEventID,
	}

	var parsed episodeSummaryLLMResponse
	if err := json.Unmarshal([]byte(responseText), &parsed); err != nil {
		zerolog.Ctx(ctx).Warn().
			Str("conversation_id", conversationID).
			Int64("episode_id", episodeID).
			Err(err).
			Str("response_preview", previewForLog(responseText, 200)).
			Msg("failed to parse episode summary JSON, using raw response")
		plan.Summary = responseText
		plan.TokenCount = actormemory.EstimateTokens(responseText)
		return plan, nil
	}

	summary := strings.TrimSpace(parsed.Summary)
	if summary == "" {
		summary = responseText
	}
	plan.Summary = summary
	plan.TokenCount = actormemory.EstimateTokens(summary)

	// Build hard state entries with source event ID clamping.
	for _, item := range parsed.HardStateEntries {
		sourceEventID := item.SourceEventID
		if sourceEventID < startEventID || sourceEventID > endEventID {
			sourceEventID = endEventID
		}
		if sourceEventID <= 0 {
			sourceEventID = endEventID
		}

		entry := ExtractedEntry{
			EntryType:  strings.TrimSpace(item.EntryType),
			RawText:    strings.TrimSpace(item.RawText),
			Value:      strings.TrimSpace(item.Value),
			Confidence: item.Confidence,
		}
		if entry.Value == "" {
			entry.Value = entry.RawText
		}

		plan.HardStateEntries = append(plan.HardStateEntries, HardStateEntryPlanItem{
			SourceEventID: sourceEventID,
			Entry:         entry,
		})
	}

	// Build evidence snippets, skipping invalid items.
	eventTypeByID := make(map[int64]string, len(events))
	for _, ev := range events {
		eventTypeByID[ev.ID] = ev.EventType
	}

	for _, item := range parsed.EvidenceSnippets {
		if strings.TrimSpace(item.FactText) == "" {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", conversationID).
				Int64("episode_id", episodeID).
				Msg("skipping empty extracted evidence snippet")
			continue
		}
		if item.SourceEventID <= 0 {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", conversationID).
				Int64("episode_id", episodeID).
				Str("fact_preview", previewForLog(item.FactText, 100)).
				Msg("skipping extracted evidence snippet with invalid source_event_id")
			continue
		}
		if item.SourceEventID < startEventID || item.SourceEventID > endEventID {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", conversationID).
				Int64("episode_id", episodeID).
				Int64("source_event_id", item.SourceEventID).
				Msg("skipping extracted evidence snippet with out-of-range source_event_id")
			continue
		}

		eventType := strings.TrimSpace(item.EventType)
		if eventType == "" {
			eventType = eventTypeByID[item.SourceEventID]
		}

		plan.EvidenceSnippets = append(plan.EvidenceSnippets, EvidenceSnippetPlanItem{
			SourceEventID: item.SourceEventID,
			EventType:     eventType,
			FactText:      strings.TrimSpace(item.FactText),
			Confidence:    item.Confidence,
			Bucket:        strings.TrimSpace(item.Bucket),
			TTLDays:       item.TTLDays,
		})
	}

	return plan, nil
}

// ApplyEpisodeSummaryPlan persists the planned hard-state entries and evidence
// snippets to the database. If dryRun is true, no writes are performed.
func (m *ConversationMemory) ApplyEpisodeSummaryPlan(ctx context.Context, plan *EpisodeSummaryPlan, dryRun bool) error {
	if plan == nil {
		return fmt.Errorf("plan is required")
	}
	if dryRun {
		return nil
	}

	if len(plan.HardStateEntries) > 0 {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin hard state extraction tx: %w", err)
		}
		defer tx.Rollback()

		for _, item := range plan.HardStateEntries {
			if err := m.persistDeterministicExtraction(ctx, tx, plan.ConversationID, item.SourceEventID, item.Entry); err != nil {
				zerolog.Ctx(ctx).Warn().
					Str("conversation_id", plan.ConversationID).
					Int64("episode_id", plan.EpisodeID).
					Int64("source_event_id", item.SourceEventID).
					Str("entry_type", item.Entry.EntryType).
					Err(err).
					Msg("failed to persist extracted hard state entry")
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit hard state extraction tx: %w", err)
		}
	}

	for _, item := range plan.EvidenceSnippets {
		snippet := &EvidenceSnippet{
			ConversationID: plan.ConversationID,
			SourceEventID:  item.SourceEventID,
			EventType:      item.EventType,
			FactText:       item.FactText,
			Confidence:     item.Confidence,
			Bucket:         item.Bucket,
			TTLDays:        item.TTLDays,
		}

		if _, err := m.InsertEvidenceSnippet(ctx, nil, snippet); err != nil {
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", plan.ConversationID).
				Int64("episode_id", plan.EpisodeID).
				Int64("source_event_id", item.SourceEventID).
				Err(err).
				Msg("failed to persist extracted evidence snippet")
		}
	}

	return nil
}

func loadEpisodeEvents(ctx context.Context, db *sql.DB, conversationID string, startEventID, endEventID int64) ([]episodeTranscriptEvent, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if startEventID <= 0 || endEventID <= 0 || endEventID < startEventID {
		return nil, fmt.Errorf("invalid event range")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.event_type, t.role, t.content, e.payload_json
		FROM companion_events e
		LEFT JOIN companion_turns t ON t.id = e.turn_id
		WHERE e.conversation_id = $1
			AND e.id >= $2
			AND e.id <= $3
		ORDER BY e.id
	`, conversationID, startEventID, endEventID)
	if err != nil {
		return nil, fmt.Errorf("query episode events: %w", err)
	}
	defer rows.Close()

	var events []episodeTranscriptEvent
	for rows.Next() {
		var (
			ev         episodeTranscriptEvent
			role       sql.NullString
			turnText   sql.NullString
			payloadRaw sql.NullString
		)

		if err := rows.Scan(&ev.ID, &ev.EventType, &role, &turnText, &payloadRaw); err != nil {
			return nil, fmt.Errorf("scan episode event: %w", err)
		}
		if role.Valid {
			ev.TurnRole = strings.TrimSpace(role.String)
		}
		if turnText.Valid {
			ev.TurnContent = strings.TrimSpace(turnText.String)
		}
		if payloadRaw.Valid {
			ev.PayloadJSON = strings.TrimSpace(payloadRaw.String)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate episode events: %w", err)
	}

	return events, nil
}

func formatEpisodeTranscript(events []episodeTranscriptEvent) string {
	if len(events) == 0 {
		return ""
	}

	lines := make([]string, 0, len(events))
	for _, ev := range events {
		role := strings.TrimSpace(ev.TurnRole)
		if role == "" {
			switch strings.TrimSpace(ev.EventType) {
			case EventTypeUserMessage:
				role = "user"
			case EventTypeAssistantMessage:
				role = "assistant"
			default:
				role = strings.TrimSpace(ev.EventType)
				if role == "" {
					role = "event"
				}
			}
		}

		content := strings.TrimSpace(ev.TurnContent)
		if content == "" {
			content = strings.TrimSpace(ev.PayloadJSON)
		}
		content = strings.Join(strings.Fields(content), " ")
		if content == "" {
			content = "[no content]"
		}

		lines = append(lines, fmt.Sprintf("[%d] %s: %s", ev.ID, role, content))
	}

	return strings.Join(lines, "\n")
}

func buildEpisodeSummaryPrompt(conversationID string, episodeID, startEventID, endEventID int64, transcript string) string {
	return fmt.Sprintf(`Summarize this conversation episode and extract structured memory signals.

Conversation ID: %s
Episode ID: %d
Event Range: [%d, %d]

Transcript:
%s

Return ONLY a valid JSON object that matches this schema exactly:
{
  "summary": "string",
  "hard_state_entries": [
    {
      "entry_type": "one of: %s, %s, %s, %s, %s, %s, %s, %s, %s",
      "raw_text": "directly grounded claim text",
      "value": "normalized value to store",
      "confidence": 0.0,
      "source_event_id": 0
    }
  ],
  "evidence_snippets": [
    {
      "source_event_id": 0,
      "event_type": "one of: %s, %s, %s, %s",
      "fact_text": "short grounded quote or paraphrase from the transcript",
      "confidence": 0.0,
      "bucket": "default",
      "ttl_days": null
    }
  ]
}

Rules:
- Keep summary to 2-4 sentences.
- Use only source_event_id values that appear in the transcript.
- If no hard-state items are present, return [].
- If no evidence snippets are present, return [].
- Do not include markdown fences or extra keys.

Example:
{
  "summary": "The user asked for an implementation matching a plan and requested strict constraints. The assistant inspected companion internals, then prepared code edits for LLM episode summarization and type constants.",
  "hard_state_entries": [
    {
      "entry_type": "technical_context",
      "raw_text": "The method must match the summarizerA interface in daemon.go",
      "value": "SummarizeEpisode must implement the 5-argument signature used by daemon summarizerA",
      "confidence": 0.9,
      "source_event_id": %d
    }
  ],
  "evidence_snippets": [
    {
      "source_event_id": %d,
      "event_type": "user_message",
      "fact_text": "Implement the LLM-based episode summarization plan and add the new constants.",
      "confidence": 0.91,
      "bucket": "default",
      "ttl_days": null
    }
  ]
}
`, conversationID, episodeID, startEventID, endEventID, transcript,
		EntryTypePreference, EntryTypeDecision, EntryTypeGlossary, EntryTypeOpenQuestion, EntryTypeGoal, EntryTypePolicy,
		EntryTypeIdentity, EntryTypeRelationship, EntryTypeTechnicalContext,
		EventTypeUserMessage, EventTypeAssistantMessage, EventTypeToolCall, EventTypeToolResult,
		startEventID, startEventID)
}
