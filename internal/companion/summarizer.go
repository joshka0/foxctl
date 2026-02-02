package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/rs/zerolog"
)

// LLMSummarizer uses an LLM to create conversation summaries.
type LLMSummarizer struct {
	provider string
	apiKey   string
	model    string
	logger   zerolog.Logger
}

// LLMSummarizerConfig configures the LLM summarizer.
type LLMSummarizerConfig struct {
	Provider string
	APIKey   string
	Model    string
	Logger   zerolog.Logger
}

// NewLLMSummarizer creates a new LLM-based summarizer.
// NewLLMSummarizer creates an LLM-based summarizer.
//
// Index:
// - Purpose: Build a summarizer configured with provider credentials
// - Flow: store config → return summarizer
// - Related: LLMSummarizer.SummarizeDay, LLMSummarizer.DistillHistory
// - Keywords: llm_summarizer, provider, model, api_key, summaries
func NewLLMSummarizer(cfg LLMSummarizerConfig) *LLMSummarizer {
	return &LLMSummarizer{
		provider: cfg.Provider,
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		logger:   cfg.Logger,
	}
}

// SummarizeDay creates a day summary from conversation turns.
// SummarizeDay creates a day summary from conversation turns.
//
// Index:
// - Purpose: Summarize a day's conversation into structured metadata
// - Flow: format turns → prompt LLM → parse JSON → build summary
// - SideEffects: LLM API call
// - FailureModes: missing turns, engine errors, parse errors (falls back to raw)
// - Related: LLMSummarizer.DistillHistory
// - Keywords: summarize_day, day_summary, mood, topics, key_moments
func (s *LLMSummarizer) SummarizeDay(ctx context.Context, turns []ConversationTurn) (*DaySummary, error) {
	if len(turns) == 0 {
		return nil, fmt.Errorf("no turns to summarize")
	}

	// Format turns for the prompt
	var turnLines []string
	for _, t := range turns {
		role := "Human"
		if t.Role == "assistant" {
			role = "Assistant"
		}
		turnLines = append(turnLines, fmt.Sprintf("[%s] %s: %s",
			t.CreatedAt.Format("3:04 PM"), role, t.Content))
	}
	transcript := strings.Join(turnLines, "\n\n")

	prompt := fmt.Sprintf(`Summarize this day's conversation between a human and their AI companion.

CONVERSATION:
%s

Respond with a JSON object containing:
{
  "summary": "A 1-2 sentence summary of what was discussed and the overall vibe",
  "topics": ["topic1", "topic2"],
  "mood": "The emotional tone (e.g., 'casual and friendly', 'deep and reflective', 'playful')",
  "key_moments": ["Any memorable or important exchanges"]
}

Keep the summary warm and personal - this is for the companion to remember the conversation.`, transcript)

	// Call LLM
	engineCfg := engine.LLMChatConfig{
		Provider:      s.provider,
		APIKey:        s.apiKey,
		Model:         s.model,
		MaxIterations: 1,
		Temperature:   0.3,
		MaxTokens:     1000,
	}

	llm, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	input := engine.EngineInput{
		SystemPrompt: "You are a helpful assistant that summarizes conversations. Respond only with valid JSON.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	}

	output, err := llm.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("run engine: %w", err)
	}

	if output.StopReason == engine.StopReasonError {
		return nil, fmt.Errorf("LLM error: %s", output.Error)
	}

	// Parse response
	responseText := strings.TrimSpace(output.AssistantText)
	// Strip markdown code blocks if present
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)

	var result struct {
		Summary    string   `json:"summary"`
		Topics     []string `json:"topics"`
		Mood       string   `json:"mood"`
		KeyMoments []string `json:"key_moments"`
	}

	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		s.logger.Warn().
			Int("response_chars", len(responseText)).
			Str("response_preview", previewForLog(responseText, 100)).
			Err(err).
			Msg("Failed to parse summary JSON, using raw response")
		// Fallback: use the raw response as summary
		return &DaySummary{
			Summary:    responseText,
			TokenCount: actormemory.EstimateTokens(responseText),
		}, nil
	}

	summary := &DaySummary{
		Summary:    result.Summary,
		Topics:     result.Topics,
		Mood:       result.Mood,
		KeyMoments: result.KeyMoments,
		TokenCount: actormemory.EstimateTokens(result.Summary),
	}

	return summary, nil
}

// DistillHistory compresses multiple day summaries into distilled history.
// DistillHistory compresses multiple summaries into long-term memory.
//
// Index:
// - Purpose: Distill recent summaries into relationship history
// - Flow: format summaries → prompt LLM → parse JSON → build distilled history
// - SideEffects: LLM API call
// - FailureModes: engine errors, parse errors (returns existing/empty)
// - Related: LLMSummarizer.SummarizeDay
// - Keywords: distill_history, relationship_note, recurring_topics, user_preferences, shared_memories
func (s *LLMSummarizer) DistillHistory(ctx context.Context, existing *DistilledHistory, summaries []DaySummary) (*DistilledHistory, error) {
	if len(summaries) == 0 {
		return existing, nil
	}

	// Format summaries for the prompt
	var summaryLines []string
	for _, sum := range summaries {
		line := fmt.Sprintf("**%s**: %s", sum.Date, sum.Summary)
		if len(sum.Topics) > 0 {
			line += " [Topics: " + strings.Join(sum.Topics, ", ") + "]"
		}
		if sum.Mood != "" {
			line += " [Mood: " + sum.Mood + "]"
		}
		summaryLines = append(summaryLines, line)
	}
	summaryText := strings.Join(summaryLines, "\n")

	// Include existing history if available
	existingContext := ""
	if existing != nil {
		var parts []string
		if existing.RelationshipNote != "" {
			parts = append(parts, "Current relationship: "+existing.RelationshipNote)
		}
		if len(existing.RecurringTopics) > 0 {
			parts = append(parts, "Recurring topics: "+strings.Join(existing.RecurringTopics, ", "))
		}
		if len(existing.UserPreferences) > 0 {
			parts = append(parts, "User preferences: "+strings.Join(existing.UserPreferences, ", "))
		}
		if len(existing.SharedMemories) > 0 {
			parts = append(parts, "Shared memories: "+strings.Join(existing.SharedMemories, ", "))
		}
		if len(parts) > 0 {
			existingContext = "\n\nEXISTING HISTORY:\n" + strings.Join(parts, "\n")
		}
	}

	prompt := fmt.Sprintf(`Distill these conversation summaries into long-term memory for an AI companion.
%s
RECENT SUMMARIES TO INTEGRATE:
%s

Create an updated long-term memory that captures:
1. The nature of the relationship (warm, professional, playful, etc.)
2. Recurring topics they discuss
3. User preferences and communication style
4. Key shared memories or meaningful moments

Respond with a JSON object:
{
  "relationship_note": "A brief description of the relationship dynamic",
  "recurring_topics": ["topic1", "topic2"],
  "user_preferences": ["preference1", "preference2"],
  "shared_memories": ["memory1", "memory2"]
}

Keep it concise but meaningful - this shapes how the companion remembers and relates to the user.`, existingContext, summaryText)

	// Call LLM
	engineCfg := engine.LLMChatConfig{
		Provider:      s.provider,
		APIKey:        s.apiKey,
		Model:         s.model,
		MaxIterations: 1,
		Temperature:   0.3,
		MaxTokens:     1000,
	}

	llm, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	input := engine.EngineInput{
		SystemPrompt: "You are a helpful assistant that distills conversation history. Respond only with valid JSON.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	}

	output, err := llm.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("run engine: %w", err)
	}

	if output.StopReason == engine.StopReasonError {
		return nil, fmt.Errorf("LLM error: %s", output.Error)
	}

	// Parse response
	responseText := strings.TrimSpace(output.AssistantText)
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)

	var result struct {
		RelationshipNote string   `json:"relationship_note"`
		RecurringTopics  []string `json:"recurring_topics"`
		UserPreferences  []string `json:"user_preferences"`
		SharedMemories   []string `json:"shared_memories"`
	}

	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		s.logger.Warn().
			Int("response_chars", len(responseText)).
			Str("response_preview", previewForLog(responseText, 100)).
			Err(err).
			Msg("Failed to parse distill JSON")
		// Return existing or empty
		if existing != nil {
			return existing, nil
		}
		return &DistilledHistory{}, nil
	}

	history := &DistilledHistory{
		RelationshipNote: result.RelationshipNote,
		RecurringTopics:  result.RecurringTopics,
		UserPreferences:  result.UserPreferences,
		SharedMemories:   result.SharedMemories,
		TokenCount:       actormemory.EstimateTokens(result.RelationshipNote),
	}

	// Preserve ID from existing if updating
	if existing != nil {
		history.ID = existing.ID
	}

	return history, nil
}

func previewForLog(text string, maxRunes int) string {
	if text == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}
