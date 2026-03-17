// Package main implements the session/summarize skill for generating structured summaries.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hashutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/obs"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/sliceutil"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/queue"
	"github.com/jkatigb/agentctl/internal/sessionkit/codexjsonl"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/vector"
)

const (
	summaryQueueDBName = "summary_queue.db"
	summaryQueueTable  = "summary_queue_jobs"
)

// logger is the package-level observability logger, initialized in run().
var logger *obs.Logger

// Input defines the skill input parameters for session summarization with multiple modes and options.
type Input struct {
	SessionID string `json:"session_id"`
	Force     bool   `json:"force,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`

	// Mode selects the output format.
	// - "summary" (default): structured summary fields for the session
	// - "windows": generate summaries for each context window
	// - "seed": generate a pasteable seed prompt for the next context window
	Mode string `json:"mode,omitempty"`

	// Query provides an optional relevance query for seed mode.
	// If empty, seed mode falls back to the session summary/decisions.
	Query string `json:"query,omitempty"`

	// SeedMaxChars bounds seed_prompt size to keep it inline.
	SeedMaxChars int `json:"seed_max_chars,omitempty"`

	// SeedTopKWindows selects additional relevant context windows (besides the latest).
	SeedTopKWindows int `json:"seed_top_k_windows,omitempty"`

	// SeedChunksPerWindow caps chunk previews per window.
	SeedChunksPerWindow int `json:"seed_chunks_per_window,omitempty"`

	// BatchSize limits windows processed per batch (mode=windows). Default: 5.
	BatchSize int `json:"batch_size,omitempty"`

	// ProcessAll loops internally until all windows are done (mode=windows).
	// When false, returns after one batch with windows_remaining count.
	ProcessAll bool `json:"process_all,omitempty"`

	// Queue enables queue-backed processing for window summaries.
	Queue bool `json:"queue,omitempty"`

	// QueueOnly enqueues windows without processing them.
	QueueOnly bool `json:"queue_only,omitempty"`
}

// SummarizeStats tracks token usage, costs, and skip reasons for summarization with detailed metrics.
type SummarizeStats struct {
	// Token usage
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`

	// Cost estimation (USD)
	InputCost  float64 `json:"input_cost,omitempty"`
	OutputCost float64 `json:"output_cost,omitempty"`
	TotalCost  float64 `json:"total_cost,omitempty"`

	// Provider info
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`

	// Skip tracking
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"` // "content_hash_dedup", "already_summarized", "no_jsonl"

	// Dedup info (when skipped due to content hash match)
	DedupFromSession string `json:"dedup_from_session,omitempty"`

	// Learnings persisted to memory
	LearningsPersisted int `json:"learnings_persisted,omitempty"`
}

// Output defines the skill output with comprehensive session summary data and metadata.
type Output struct {
	SessionID       string   `json:"session_id"`
	Summary         string   `json:"summary"`
	Accomplished    []string `json:"accomplished"`
	Decisions       []string `json:"decisions"`
	Gotchas         []string `json:"gotchas"`
	UserInsights    []string `json:"user_insights,omitempty"`
	UserPreferences []string `json:"user_preferences,omitempty"`
	TimeSinks       []string `json:"time_sinks,omitempty"`
	KeyQuestions    []string `json:"key_questions,omitempty"` // Semantic search queries for context restoration
	Tags            []string `json:"tags"`
	KeyFiles        []string `json:"key_files"`
	ToolsPattern    string   `json:"tools_pattern"`
	HasEmbedding    bool     `json:"has_embedding"`
	EmbeddingModel  string   `json:"embedding_model,omitempty"`
	EmbeddingDims   int      `json:"embedding_dims,omitempty"`

	// SeedPrompt is populated when mode=seed.
	SeedPrompt string `json:"seed_prompt,omitempty"`

	// WindowsSummarized is the count of windows that were summarized (mode=windows).
	WindowsSummarized int `json:"windows_summarized,omitempty"`
	// WindowsQueued is the count of windows enqueued (mode=windows with queue).
	WindowsQueued int `json:"windows_queued,omitempty"`
	// WindowsSkipped is the count of windows that already had summaries (mode=windows).
	WindowsSkipped int `json:"windows_skipped,omitempty"`
	// WindowsRemaining is the count of windows still needing summarization (mode=windows with batch_size).
	WindowsRemaining int `json:"windows_remaining,omitempty"`

	// SessionsReembedded is the count of sessions re-embedded (mode=reembed).
	SessionsReembedded int `json:"sessions_reembedded,omitempty"`
	// SessionsSkipped is the count of sessions skipped (already correct, mode=reembed).
	SessionsSkipped int `json:"sessions_skipped,omitempty"`
	// WindowsReembedded is the count of context windows re-embedded (mode=reembed).
	WindowsReembedded int `json:"windows_reembedded,omitempty"`

	// Statistics for tracking costs and deduplication
	Stats *SummarizeStats `json:"stats,omitempty"`

	Status  string `json:"status"`
	Message string `json:"message"`
}

// TokenUsage captures API response token usage for cost tracking and usage analytics.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ProviderCost defines per-million-token costs for a provider with input/output pricing.
type ProviderCost struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// providerCosts maps provider names to their per-million-token costs (USD).
// Prices as of January 2026 - update as needed.
// Sources: cerebras.ai/pricing, groq.com/pricing, openrouter.ai/pricing
var providerCosts = map[string]ProviderCost{
	// Primary providers used by agentctl
	"cerebras":       {InputPerMillion: 0.60, OutputPerMillion: 2.20}, // GLM-4.7 (Z.ai pricing)
	"cerebras:llama": {InputPerMillion: 0.60, OutputPerMillion: 0.60}, // Llama 3.3 70B
	"groq":           {InputPerMillion: 0.59, OutputPerMillion: 0.79}, // Llama 3.3 70B Versatile

	// OpenRouter models (name format: "openrouter:{provider}/{model}")
	// Models from .env OPENROUTER_MODELS config
	"openrouter:mistralai":      {InputPerMillion: 0.15, OutputPerMillion: 0.60},  // Devstral 2512 (paid tier)
	"openrouter:arcee-ai":       {InputPerMillion: 0.045, OutputPerMillion: 0.15}, // Trinity Mini (paid tier)
	"openrouter:bytedance-seed": {InputPerMillion: 0.075, OutputPerMillion: 0.30}, // Seed 1.6 Flash
	"openrouter:x-ai":           {InputPerMillion: 0.20, OutputPerMillion: 0.50},  // Grok 4.1 Fast
	"openrouter:openai":         {InputPerMillion: 0.25, OutputPerMillion: 2.00},  // GPT-5.1 Codex Mini
	// Other OpenRouter providers
	"openrouter:minimax":    {InputPerMillion: 0.27, OutputPerMillion: 1.12},   // MiniMax M2.1
	"openrouter:anthropic":  {InputPerMillion: 0.25, OutputPerMillion: 1.25},   // Claude 3 Haiku
	"openrouter:meta-llama": {InputPerMillion: 0.055, OutputPerMillion: 0.055}, // Llama 3.3 70B
	"openrouter:google":     {InputPerMillion: 0.075, OutputPerMillion: 0.30},  // Gemini Flash
	"openrouter:deepseek":   {InputPerMillion: 0.14, OutputPerMillion: 0.28},   // DeepSeek V3

	// Direct API providers (less commonly used)
	"anthropic": {InputPerMillion: 3.00, OutputPerMillion: 15.00}, // Claude 3.5 Sonnet
	"openai":    {InputPerMillion: 0.15, OutputPerMillion: 0.60},  // GPT-4o-mini
	"gemini":    {InputPerMillion: 0.075, OutputPerMillion: 0.30}, // Gemini 1.5 Flash
}

// calculateCost returns input cost, output cost, total cost in USD with provider-specific pricing.
func calculateCost(provider string, usage TokenUsage) (inputCost, outputCost, totalCost float64) {
	cost, ok := providerCosts[provider]
	if !ok {
		// Try prefix match for openrouter variants
		for prefix, c := range providerCosts {
			if strings.HasPrefix(provider, prefix) {
				cost = c
				ok = true
				break
			}
		}
	}
	if !ok {
		return 0, 0, 0 // Unknown provider
	}

	inputCost = float64(usage.InputTokens) * cost.InputPerMillion / 1_000_000
	outputCost = float64(usage.OutputTokens) * cost.OutputPerMillion / 1_000_000
	totalCost = inputCost + outputCost
	return
}

// SummaryResponse is the expected JSON response from the LLM with structured summary fields.
type SummaryResponse struct {
	Summary         string   `json:"summary"`
	Accomplished    []string `json:"accomplished"`
	Decisions       []string `json:"decisions"`
	Gotchas         []string `json:"gotchas"`
	UserInsights    []string `json:"user_insights,omitempty"`
	UserPreferences []string `json:"user_preferences,omitempty"`
	TimeSinks       []string `json:"time_sinks,omitempty"`
	Tags            []string `json:"tags"`
	KeyFiles        []string `json:"key_files"`
	ToolsPattern    string   `json:"tools_pattern"`
	KeyQuestions    []string `json:"key_questions,omitempty"` // Semantic search questions for context restoration
}

// ClaudeMessage represents a message from Claude Code's JSONL format with comprehensive metadata.
type ClaudeMessage struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid,omitempty"`
	ParentUUID string          `json:"parentUuid,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	Timestamp  string          `json:"timestamp,omitempty"`
	CWD        string          `json:"cwd,omitempty"`
	GitBranch  string          `json:"gitBranch,omitempty"`
	Version    string          `json:"version,omitempty"`
	Message    *MessageContent `json:"message,omitempty"`
	Summary    string          `json:"summary,omitempty"`
}

// MessageContent represents the content of a message with role and structured data.
type MessageContent struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Model   string          `json:"model,omitempty"`
}

// ContentBlock represents a block in assistant message content with various content types.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"` // Note: NOT used for summarization (can be huge)
	ToolUseID string          `json:"id,omitempty"`
}

// UserContentBlock represents a block in user message content (text or tool_result) with error handling.
type UserContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"` // tool result content (often large)
}

// codexEventPayload models Codex event_msg payloads for summarization with event type detection.
type codexEventPayload struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Text    string `json:"text,omitempty"`
}

// FilteredMessage is a high-signal message for summarization with role, content, and tool tracking.
type FilteredMessage struct {
	Role       string   `json:"role"`
	Content    string   `json:"content"`
	ToolsUsed  []string `json:"tools_used,omitempty"`
	Error      string   `json:"error,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
}

const (
	command          = "session/summarize"
	defaultMaxTokens = 2000 // ~8k chars, conservative limit for free tier models
	geminiBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
)

// LLMProvider represents a chat completion API provider.
type LLMProvider = llmproviders.Provider

// main is the skill entry point for session/summarize with multi-mode summarization capabilities.
func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithTimeout[Input](5*time.Minute),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates session summarization with multiple modes, deduplication, and embedding generation.
//
// Index:
// - Purpose: Generate structured summaries for sessions with multiple output modes and cost optimization
// - Flow: validate input → resolve mode → open session store → route to handler → generate summary → persist learnings → emit results
// - SideEffects: updates session summaries; stores learnings in memory; generates embeddings; manages queue jobs
// - FailureModes: invalid sessions, LLM provider failures, file access errors, embedding generation failures
// - Observability: emits summary results, token usage, cost tracking, deduplication status, and processing statistics
// - Related: persistSessionLearnings, filterJSONL, summarizeWithFallback, buildSeedPrompt, reembedAll
// - Keywords: session/summarize, session_summary, llm_summarization, deduplication, embeddings, cost_tracking
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Initialize package logger
	logger = obs.NewLogger(
		obs.WithLogCommand("session/summarize"),
	)

	if in.MaxTokens <= 0 {
		in.MaxTokens = defaultMaxTokens
	}

	// Get available LLM providers
	providers := llmproviders.SummarizationProviders()
	if len(providers) == 0 {
		return skillerr.Arg("no LLM provider configured (set GROQ_API_KEY, CEREBRAS_API_KEY, or OPENROUTER_API_KEY)")
	}
	windowProviders := windowSummaryProviders(providers)

	// Parse mode early - some modes don't need session_id
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "summary"
	}
	if mode != "summary" && mode != "seed" && mode != "windows" && mode != "reembed" {
		return skillerr.Arg(fmt.Sprintf("invalid mode %q (expected summary, windows, seed, or reembed)", in.Mode))
	}

	// Open sessions store
	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

	// Handle reembed mode early - doesn't need a specific session
	if mode == "reembed" {
		output := reembedAll(ctx, sessionStore, in, rc.Config, skillmain.EmbeddingGuard(rc))
		return skillout.Emit(rc, command, output)
	}

	// All other modes require session_id
	if in.SessionID == "" {
		return skillerr.Arg("session_id is required")
	}

	// Get session
	session, err := sessionStore.Get(ctx, in.SessionID)
	if err != nil {
		return skillerr.Arg("session not found", skillerr.WithCause(err))
	}
	if in.SeedMaxChars <= 0 {
		in.SeedMaxChars = 12000
	}
	if in.SeedTopKWindows <= 0 {
		in.SeedTopKWindows = 2
	}
	if in.SeedChunksPerWindow <= 0 {
		in.SeedChunksPerWindow = 10
	}

	// Handle windows mode - summarize context windows with LLM
	if mode == "windows" {
		var output Output
		if in.Queue || in.QueueOnly {
			output = summarizeWindowsQueued(ctx, sessionStore, session, windowProviders, in, rc.Config, skillmain.EmbeddingGuard(rc))
		} else {
			output = summarizeWindows(ctx, sessionStore, session, windowProviders, in, rc.Config, skillmain.EmbeddingGuard(rc))
		}
		return skillout.Emit(rc, command, output)
	}

	needsSummarize := mode == "summary" && (in.Force || strings.TrimSpace(session.Summary) == "")

	// Default behavior: return existing summary quickly.
	if mode == "summary" && !needsSummarize {
		output := Output{
			SessionID:    session.ID,
			Summary:      session.Summary,
			Accomplished: sliceutil.Clone(session.Accomplished),
			Decisions:    sliceutil.Clone(session.Decisions),
			Gotchas:      sliceutil.Clone(session.Gotchas),
			Tags:         sliceutil.Clone(session.Tags),
			KeyFiles:     sliceutil.Clone(session.KeyFiles),
			ToolsPattern: session.ToolsPattern,
			Stats: &SummarizeStats{
				Skipped:    true,
				SkipReason: "already_summarized",
			},
			Status:  "exists",
			Message: fmt.Sprintf("Session %s already summarized (use force=true to re-summarize)", session.ID),
		}
		return skillout.Emit(rc, command, output)
	}

	var (
		summaryResp        *SummaryResponse
		contentHash        string
		usedProvider       string
		tokenUsage         *TokenUsage
		persistedLearnings int
		persistErr         error
		deduped            bool
		dedupFromSession   string
	)

	if needsSummarize {
		// Compute content hash for deduplication check
		// Use the same max tokens logic as summarizeWithFallback to ensure consistent hashing
		firstProviderMaxTokens := providers[0].MaxTokens
		if in.MaxTokens > 0 && in.MaxTokens < firstProviderMaxTokens {
			firstProviderMaxTokens = in.MaxTokens // Apply user cap (same as summarizeWithFallback)
		}
		if firstProviderMaxTokens <= 0 {
			firstProviderMaxTokens = defaultMaxTokens
		}
		_, preHash, err := filterJSONL(ctx, session.RawJSONLPath, firstProviderMaxTokens)
		if err == nil && preHash != "" {
			// Check if another session with same content already has a summary
			existingSession, _ := sessionStore.FindByContentHash(ctx, preHash)
			if existingSession != nil && existingSession.ID != session.ID && existingSession.Summary != "" {
				// Determine if we should dedupe:
				// - force=false: always dedupe from any matching session
				// - force=true: only dedupe from sessions updated in last 5 minutes
				//   (handles batch re-summarize: first one calls LLM, rest dedupe)
				recentThreshold := time.Now().Add(-5 * time.Minute)
				shouldDedupe := !in.Force || existingSession.UpdatedAt.After(recentThreshold)

				if shouldDedupe {
					// Reuse existing summary from session with identical content
					summaryResp = &SummaryResponse{
						Summary:      existingSession.Summary,
						Accomplished: sliceutil.Clone(existingSession.Accomplished),
						Decisions:    sliceutil.Clone(existingSession.Decisions),
						Gotchas:      sliceutil.Clone(existingSession.Gotchas),
						UserInsights: sliceutil.Clone(existingSession.UserInsights),
						Tags:         sliceutil.Clone(existingSession.Tags),
						KeyFiles:     sliceutil.Clone(existingSession.KeyFiles),
						ToolsPattern: existingSession.ToolsPattern,
						KeyQuestions: sliceutil.Clone(existingSession.KeyQuestions),
					}
					contentHash = preHash
					deduped = true
					dedupFromSession = existingSession.ID
				}
			}
		}

		if !deduped {
			// Call LLM for summarization (with fallback, filtering per-provider)
			summaryResp, contentHash, usedProvider, tokenUsage, err = summarizeWithFallback(ctx, rc, providers, session.RawJSONLPath, in.MaxTokens)
			if err != nil {
				return skillerr.Runtime(fmt.Sprintf("summarization failed (tried %d providers)", len(providers)), skillerr.WithCause(err))
			}
		}

		// Update session with summary (including key_questions for session restore)
		err = sessionStore.UpdateSummaryWithQuestions(ctx, session.ID,
			summaryResp.Summary,
			summaryResp.Accomplished,
			summaryResp.Decisions,
			summaryResp.Gotchas,
			summaryResp.UserInsights,
			summaryResp.Tags,
			summaryResp.KeyFiles,
			summaryResp.ToolsPattern,
			summaryResp.KeyQuestions,
		)
		if err != nil {
			return skillerr.IO("save summary", skillerr.WithCause(err))
		}

		// Store the content hash for future deduplication
		if contentHash != "" {
			_ = sessionStore.SetContentHash(ctx, session.ID, contentHash)
		}

		persistedLearnings, persistErr = persistSessionLearnings(ctx, rc, session, summaryResp)
	} else {
		// Use persisted session fields.
		summaryResp = &SummaryResponse{
			Summary:         session.Summary,
			Accomplished:    sliceutil.Clone(session.Accomplished),
			Decisions:       sliceutil.Clone(session.Decisions),
			Gotchas:         sliceutil.Clone(session.Gotchas),
			UserInsights:    sliceutil.Clone(session.UserInsights),
			UserPreferences: []string{},
			TimeSinks:       []string{},
			Tags:            sliceutil.Clone(session.Tags),
			KeyFiles:        sliceutil.Clone(session.KeyFiles),
			ToolsPattern:    session.ToolsPattern,
			KeyQuestions:    sliceutil.Clone(session.KeyQuestions),
		}
	}

	status := "summarized"
	message := fmt.Sprintf("Summarized session %s: %s", session.ID, skillout.TruncateSingleLine(summaryResp.Summary, 100))
	if deduped {
		status = "deduped"
		message = fmt.Sprintf("Reused summary from session with identical content: %s", skillout.TruncateSingleLine(summaryResp.Summary, 100))
	} else if !needsSummarize {
		status = "exists"
		message = fmt.Sprintf("Loaded existing summary for session %s", session.ID)
	}

	// Build stats for cost tracking and deduplication insight
	stats := &SummarizeStats{
		LearningsPersisted: persistedLearnings,
	}
	if deduped {
		stats.Skipped = true
		stats.SkipReason = "content_hash_dedup"
		stats.DedupFromSession = dedupFromSession
	} else if !needsSummarize {
		stats.Skipped = true
		stats.SkipReason = "already_summarized"
	} else if tokenUsage != nil {
		// LLM was called - populate token and cost info
		stats.InputTokens = tokenUsage.InputTokens
		stats.OutputTokens = tokenUsage.OutputTokens
		stats.TotalTokens = tokenUsage.TotalTokens
		stats.Provider = usedProvider
		// Calculate cost
		stats.InputCost, stats.OutputCost, stats.TotalCost = calculateCost(usedProvider, *tokenUsage)
	}

	output := Output{
		SessionID:       session.ID,
		Summary:         summaryResp.Summary,
		Accomplished:    sliceutil.Clone(summaryResp.Accomplished),
		Decisions:       sliceutil.Clone(summaryResp.Decisions),
		Gotchas:         sliceutil.Clone(summaryResp.Gotchas),
		UserInsights:    sliceutil.Clone(summaryResp.UserInsights),
		UserPreferences: sliceutil.Clone(summaryResp.UserPreferences),
		TimeSinks:       sliceutil.Clone(summaryResp.TimeSinks),
		KeyQuestions:    sliceutil.Clone(summaryResp.KeyQuestions),
		Tags:            sliceutil.Clone(summaryResp.Tags),
		KeyFiles:        sliceutil.Clone(summaryResp.KeyFiles),
		ToolsPattern:    summaryResp.ToolsPattern,
		Stats:           stats,
		Status:          status,
		Message:         message,
	}
	if needsSummarize {
		if persistErr != nil {
			output.Message += fmt.Sprintf(" (persist learnings failed: %v)", persistErr)
		} else if persistedLearnings > 0 {
			output.Message += fmt.Sprintf(" (persisted %d learnings)", persistedLearnings)
		}
	}

	if mode == "seed" {
		seedPrompt, err := buildSeedPrompt(ctx, sessionStore, session, summaryResp, in, rc.Config, skillmain.EmbeddingGuard(rc))
		if err != nil {
			output.Message += fmt.Sprintf(" (seed failed: %v)", err)
		} else {
			output.SeedPrompt = seedPrompt
			output.Status = "seeded"
			if needsSummarize {
				output.Message = fmt.Sprintf("Summarized and generated seed for session %s", session.ID)
			} else if strings.TrimSpace(summaryResp.Summary) != "" {
				output.Message = fmt.Sprintf("Generated seed from existing summary for session %s", session.ID)
			} else {
				output.Message = fmt.Sprintf("Generated seed from session context for session %s", session.ID)
			}
		}
	}

	if needsSummarize {
		// Generate embedding using unified Embedder (handles provider selection and fallback)
		embeddingText := buildEmbeddingText(summaryResp, session.StartedAt, session.KeyFiles)
		var embeddingResult semantic.EmbedResult
		var embeddingErr error

		embedder, err := semantic.NewEmbedderFromConfig(
			semantic.ScopeSessions,
			rc.Config,
			semantic.WithAllowFallback(true),
			skillmain.EmbeddingGuard(rc),
		)
		if err != nil {
			embeddingErr = err
		} else {
			embeddingResult, embeddingErr = embedder.Embed(ctx, embeddingText)
		}

		if embeddingErr != nil {
			// Log but don't fail - embedding is optional
			output.Message += fmt.Sprintf(" (embedding failed: %v)", embeddingErr)
		} else if len(embeddingResult.Vec) > 0 {
			// Serialize embedding as binary float32
			embeddingBytes := vector.SerializeF32(embeddingResult.Vec)

			if err := sessionStore.SetEmbedding(ctx, session.ID, embeddingBytes, embeddingResult.Model); err != nil {
				output.Message += fmt.Sprintf(" (save embedding failed: %v)", err)
			} else {
				output.HasEmbedding = true
				output.EmbeddingModel = embeddingResult.Model
				output.EmbeddingDims = embeddingResult.Dims
				output.Message += fmt.Sprintf(" (embedded: %s/%d dims)", embeddingResult.Provider, embeddingResult.Dims)

				// Record embedding metadata for dimension validation on future opens
				meta := sessions.EmbeddingMetadata{
					WorkspaceID:   session.WorkspaceID,
					WorkspacePath: session.WorkspacePath,
					TableName:     "sessions",
					ColumnName:    "embedding",
					Provider:      embeddingResult.Provider,
					Model:         embeddingResult.Model,
					Dimensions:    embeddingResult.Dims,
				}
				if err := sessionStore.SetEmbeddingMetadata(ctx, meta); err != nil {
					logger.Warn("failed to set embedding metadata", obs.Err(err))
				}
			}
		}
	}

	return skillout.Emit(rc, command, output)
}

// persistSessionLearnings extracts and persists session learnings to memory with embedding support.
func persistSessionLearnings(ctx context.Context, rc *skillmain.RunContext, session sessions.Session, resp *SummaryResponse) (int, error) {
	workspace := session.WorkspacePath
	if strings.TrimSpace(workspace) == "" {
		return 0, skillerr.Validation("missing session workspace_path")
	}

	store, err := rc.Stores.Memory(ctx)
	if err != nil {
		return 0, skillerr.WrapIO("open memory store", err)
	}

	// Initialize embedding provider (optional - learnings work without embeddings)
	var embedProvider semantic.EmbeddingProvider
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if voyageKey != "" || geminiKey != "" {
		waitOnLimit := true
		embedProvider, _ = semantic.NewProviderForScope(
			semantic.ScopeMemory,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			semantic.WithRateLimitWait(waitOnLimit),
		)
	}

	count := 0
	if n, err := persistLearnings(ctx, store, embedProvider, session.ID, workspace, "gotcha", resp.Gotchas); err != nil {
		return count, err
	} else {
		count += n
	}
	if n, err := persistLearnings(ctx, store, embedProvider, session.ID, workspace, "decision", resp.Decisions); err != nil {
		return count, err
	} else {
		count += n
	}
	if n, err := persistLearnings(ctx, store, embedProvider, session.ID, workspace, "user_pref", resp.UserPreferences); err != nil {
		return count, err
	} else {
		count += n
	}
	if n, err := persistLearnings(ctx, store, embedProvider, session.ID, workspace, "time_sink", resp.TimeSinks); err != nil {
		return count, err
	} else {
		count += n
	}

	return count, nil
}

// persistLearnings stores individual learning items with deduplication and embedding generation.
func persistLearnings(ctx context.Context, store *memory.Store, embedProvider semantic.EmbeddingProvider, sessionID, workspace, typ string, items []string) (int, error) {
	count := 0
	for _, raw := range items {
		text := normalizeLearning(raw)
		if text == "" {
			continue
		}
		digest := hashutil.ShortHash(text)
		name := fmt.Sprintf("session:%s:%s:%s", sessionID, typ, digest)

		// Content-hash deduplication: check if ANY entry with this content exists
		// (handles forked sessions that share identical learnings)
		suffix := fmt.Sprintf(":%s:%s", typ, digest)
		if exists, err := store.ExistsByNameSuffix(ctx, workspace, suffix); err == nil && exists {
			// Content already stored (possibly from another session), skip
			continue
		}

		// Idempotency: check if entry already exists with embedding
		if existing, err := store.GetEmbedding(ctx, name, workspace); err == nil && len(existing) > 0 {
			// Already has embedding, skip
			continue
		}

		payload, err := json.Marshal(map[string]any{
			"session_id":   sessionID,
			"type":         typ,
			"text":         text,
			"content_hash": digest, // Store hash for future idempotency checks
		})
		if err != nil {
			return count, skillerr.WrapRuntime(fmt.Sprintf("marshal %s", typ), err)
		}

		_, err = store.SaveResult(ctx, memory.SaveOptions{
			Name:      name,
			Type:      typ,
			Workspace: workspace,
			Summary:   text,
			Result:    payload,
			SessionID: sessionID,
		})
		if err != nil {
			return count, skillerr.WrapIO(fmt.Sprintf("save %s", typ), err)
		}

		// Generate embedding if provider available
		if embedProvider != nil {
			if embedding, err := embedProvider.Embed(ctx, text); err == nil && len(embedding) > 0 {
				_ = store.UpdateEmbedding(ctx, name, workspace, embedding)
			}
		}

		count++
	}
	return count, nil
}

// normalizeLearning normalizes learning text by trimming whitespace and collapsing multiple spaces.
func normalizeLearning(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// filterJSONL reads and filters raw JSONL session data to extract high-signal content with compression support.
// Aggressively filters to reduce 35MB+ session files to a few hundred KB.
// Handles both plain .jsonl and gzip-compressed .jsonl.gz files.
// Returns the filtered messages and a content hash for deduplication.
func filterJSONL(ctx context.Context, path string, maxTokens int) ([]FilteredMessage, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", skillerr.WrapIO("open JSONL", err)
	}
	defer func() { _ = file.Close() }()

	// Handle gzip-compressed files
	var reader io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, "", skillerr.WrapIO("open gzip", err)
		}
		defer func() { _ = gzReader.Close() }()
		reader = gzReader
	}

	var filtered []FilteredMessage
	scanner := bufio.NewScanner(reader)

	// Increase buffer size for large lines (still need to read them to skip)
	const maxCapacity = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	estimatedTokens := 0
	tokensPerChar := 0.25 // Rough estimate: 4 chars per token
	format := ""

	// Pre-filter patterns for quick rejection
	const maxLineSize = 50 * 1024 // Skip lines >50KB (likely tool_result or huge tool_use)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, "", skillerr.WrapRuntime("context canceled", err)
		}

		line := scanner.Bytes()
		lineLen := len(line)
		if lineLen == 0 {
			continue
		}

		// OPTIMIZATION 1: Skip very large lines entirely
		// These are typically tool_result with file contents or tool_use with Write/Edit inputs
		if lineLen > maxLineSize {
			continue
		}

		lineStr := string(line)
		if format == "" && detectCodexLine(lineStr) {
			format = "codex"
		}

		if format == "codex" {
			fm := filterCodexLine(line)
			if fm == nil {
				continue
			}

			// Estimate tokens for this message
			msgTokens := int(float64(len(fm.Content)) * tokensPerChar)
			if estimatedTokens+msgTokens > maxTokens {
				// Truncate remaining messages
				break
			}

			filtered = append(filtered, *fm)
			estimatedTokens += msgTokens
			continue
		}

		// OPTIMIZATION 2: Quick type check before full JSON parse
		// Skip known noise types without parsing
		if strings.Contains(lineStr, `"type":"file-history-snapshot"`) ||
			strings.Contains(lineStr, `"type":"queue-operation"`) ||
			strings.Contains(lineStr, `"type":"system"`) {
			continue
		}

		// OPTIMIZATION 3: Skip lines that are mostly tool_result content
		// These contain file contents, bash output, etc.
		if strings.Contains(lineStr, `"type":"tool_result"`) {
			continue
		}

		var msg ClaudeMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		fm := filterMessage(msg)
		if fm == nil {
			continue
		}

		// Estimate tokens for this message
		msgTokens := int(float64(len(fm.Content)) * tokensPerChar)
		if estimatedTokens+msgTokens > maxTokens {
			// Truncate remaining messages
			break
		}

		filtered = append(filtered, *fm)
		estimatedTokens += msgTokens
	}

	if err := scanner.Err(); err != nil {
		return nil, "", skillerr.WrapIO("scan JSONL", err)
	}

	// Compute content hash for deduplication
	filteredJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, "", skillerr.WrapRuntime("marshal filtered for hash", err)
	}
	contentHash := hashutil.FullHash(string(filteredJSON))

	return filtered, contentHash, nil
}

// filterMessage extracts high-signal content from a message with aggressive filtering of noise.
// Drops: file-history-snapshot, queue-operation, thinking blocks, tool_result blocks
func filterMessage(msg ClaudeMessage) *FilteredMessage {
	switch msg.Type {
	case "user":
		if msg.Message == nil {
			return nil
		}
		// Try to parse as string first (direct user input)
		var content string
		if err := json.Unmarshal(msg.Message.Content, &content); err == nil && content != "" {
			return &FilteredMessage{
				Role:    "user",
				Content: skillout.TruncateSingleLine(content, 1000), // Increased limit for user requests
			}
		}

		// Try to parse as array (may contain text blocks and tool_result blocks)
		var blocks []UserContentBlock
		if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
			return nil
		}

		// Extract only text blocks, skip tool_result blocks (they're huge and not useful)
		var textParts []string
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				textParts = append(textParts, skillout.TruncateSingleLine(block.Text, 1000))
			}
			// Skip tool_result blocks entirely - they contain file contents, command output, etc.
		}

		if len(textParts) == 0 {
			return nil
		}

		return &FilteredMessage{
			Role:    "user",
			Content: strings.Join(textParts, "\n"),
		}

	case "assistant":
		if msg.Message == nil {
			return nil
		}
		// Extract text and tool uses, skip thinking blocks
		var blocks []ContentBlock
		if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
			return nil
		}

		var textParts []string
		var toolsUsed []string

		for _, block := range blocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					textParts = append(textParts, skillout.TruncateSingleLine(block.Text, 300))
				}
			case "tool_use":
				if block.Name != "" {
					toolsUsed = append(toolsUsed, block.Name)
				}
				// Skip "thinking" blocks - they're Claude's internal reasoning and very large
			}
		}

		if len(textParts) == 0 && len(toolsUsed) == 0 {
			return nil
		}

		return &FilteredMessage{
			Role:      "assistant",
			Content:   strings.Join(textParts, "\n"),
			ToolsUsed: toolsUsed,
		}

	case "summary":
		// Claude's auto-generated summaries are valuable
		if msg.Summary != "" {
			return &FilteredMessage{
				Role:    "summary",
				Content: msg.Summary,
			}
		}
		return nil

	// Explicitly drop these types (they're noise for summarization):
	// - file-history-snapshot: file state snapshots (2MB+)
	// - queue-operation: internal operations
	// - system: system messages (could add back selectively if needed)
	default:
		return nil
	}
}

// summarizeWithFallback tries providers in order until one succeeds.
// Each provider gets filtered content based on its MaxTokens limit.
func detectCodexLine(line string) bool {
	return strings.Contains(line, `"type":"event_msg"`) ||
		strings.Contains(line, `"type":"response_item"`) ||
		strings.Contains(line, `"type":"session_meta"`) ||
		strings.Contains(line, `"type":"turn_context"`) ||
		strings.Contains(line, `"type":"compacted"`)
}

func filterCodexLine(line []byte) *FilteredMessage {
	var msg codexjsonl.Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil
	}

	switch msg.Type {
	case "event_msg":
		var payload codexEventPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return nil
		}
		return filterCodexEvent(payload)
	case "response_item":
		var item codexjsonl.ResponseItem
		if err := json.Unmarshal(msg.Payload, &item); err != nil {
			return nil
		}
		return filterCodexResponseItem(item)
	default:
		return nil
	}
}

func filterCodexEvent(payload codexEventPayload) *FilteredMessage {
	text := codexEventText(payload)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	switch payload.Type {
	case "user_message":
		return &FilteredMessage{
			Role:    "user",
			Content: skillout.TruncateSingleLine(text, 1000),
		}
	case "agent_message":
		return &FilteredMessage{
			Role:    "assistant",
			Content: skillout.TruncateSingleLine(text, 300),
		}
	default:
		return nil
	}
}

func codexEventText(payload codexEventPayload) string {
	if payload.Message != "" {
		return payload.Message
	}
	return payload.Text
}

func filterCodexResponseItem(item codexjsonl.ResponseItem) *FilteredMessage {
	switch item.Type {
	case "function_call", "custom_tool_call":
		if item.Name == "" {
			return nil
		}
		return &FilteredMessage{
			Role:      "assistant",
			ToolsUsed: []string{item.Name},
		}
	default:
		return nil
	}
}

func summarizeWithFallback(ctx context.Context, rc *skillmain.RunContext, providers []LLMProvider, jsonlPath string, userMaxTokens int) (*SummaryResponse, string, string, *TokenUsage, error) {
	var lastErr error
	var contentHash string
	for _, p := range providers {
		// Determine max tokens: user override > provider limit > default
		maxTokens := p.MaxTokens
		if userMaxTokens > 0 && userMaxTokens < maxTokens {
			maxTokens = userMaxTokens
		}
		if maxTokens <= 0 {
			maxTokens = defaultMaxTokens
		}

		// Filter JSONL for this provider's context limit
		filtered, hash, err := filterJSONL(ctx, jsonlPath, maxTokens)
		if err != nil {
			lastErr = skillerr.Runtimef("%s: filter error: %v", p.Name, err)
			continue
		}
		// Keep the hash from the first successful filter (largest context)
		if contentHash == "" {
			contentHash = hash
		}

		var resp *SummaryResponse
		var usage *TokenUsage
		err = skillmain.GuardCall(rc, skillmain.BreakerLLMProvider, ctx, func(ctx context.Context) error {
			var e error
			if p.IsCLI {
				resp, e = summarizeWithCLI(ctx, p, filtered)
			} else {
				resp, usage, e = summarizeWithProvider(ctx, p, filtered)
			}
			return e
		})

		if err == nil {
			return resp, contentHash, p.Name, usage, nil
		}
		lastErr = skillerr.Runtimef("%s: %v", p.Name, err)
		// Continue to next provider on rate limit or server errors
		if !isRetryableError(err) {
			return nil, "", "", nil, lastErr
		}
	}
	return nil, "", "", nil, lastErr
}

// summarizeWithCLI calls a local CLI tool (gemini or claude) to generate a summary.
// CLI providers don't return token usage info.
func summarizeWithCLI(ctx context.Context, provider LLMProvider, filtered []FilteredMessage) (*SummaryResponse, error) {
	// Build the conversation content (compact JSON)
	filteredJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, skillerr.WrapRuntime("marshal filtered", err)
	}

	prompt := buildSummarizationPrompt(string(filteredJSON))

	// Enforce an upper bound for CLI calls to avoid hanging processes.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var cmdName string
	var args []string
	switch provider.Name {
	case "gemini-cli":
		// gemini -m <model> -p "<prompt>"
		cmdName = "gemini"
		args = []string{"-m", provider.Model, "-p", prompt}
	case "claude-cli":
		// claude -p "<prompt>" --model <model> --output-format text
		cmdName = "claude"
		args = []string{"-p", prompt, "--model", provider.Model, "--output-format", "text"}
	default:
		return nil, skillerr.Runtimef("unknown CLI provider: %s", provider.Name)
	}

	result := executil.Run(ctx, "", cmdName, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("CLI error: %v (stderr: %s)", result.Err, string(result.Stderr))
	}

	return parseSummaryResponse(string(result.Stdout))
}

// isRetryableError returns true if we should try the next provider.
func isRetryableError(err error) bool {
	errStr := err.Error()
	// Rate limits, server errors, and timeouts are retryable
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "quota")
}

// summarizeWithProvider calls an OpenAI-compatible API to generate a summary.
// Returns the parsed summary, token usage from the API, and any error.
func summarizeWithProvider(ctx context.Context, provider LLMProvider, filtered []FilteredMessage) (*SummaryResponse, *TokenUsage, error) {
	// Build the conversation content (compact JSON to save tokens)
	filteredJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, nil, skillerr.WrapRuntime("marshal filtered", err)
	}

	prompt := buildSummarizationPrompt(string(filteredJSON))

	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": 8192,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, skillerr.WrapRuntime("marshal request", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, skillerr.WrapRuntime("create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	// OpenRouter requires additional headers
	if strings.HasPrefix(provider.Name, "openrouter:") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
		req.Header.Set("X-Title", "agentctl")
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, nil, skillerr.Runtimef("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, skillerr.WrapParse("decode response", err)
	}

	if len(result.Choices) == 0 {
		return nil, nil, skillerr.Runtimef("empty response from %s", provider.Name)
	}

	// Extract token usage
	usage := &TokenUsage{
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
		TotalTokens:  result.Usage.TotalTokens,
	}

	// Parse the JSON response
	summaryResp, err := parseSummaryResponse(result.Choices[0].Message.Content)
	if err != nil {
		return nil, usage, err // Return usage even on parse error for cost tracking
	}
	return summaryResp, usage, nil
}

func buildSummarizationPrompt(filteredJSON string) string {
	return fmt.Sprintf(`You are summarizing a coding session between a developer and an AI assistant.
Extract the following in JSON format:

{
  "summary": "2-3 sentence narrative of what happened",
  "accomplished": ["list of things completed", "2-5 items"],
  "decisions": ["key technical decisions made", "2-5 items"],
  "gotchas": ["problems encountered and solutions", "0-5 items"],
  "user_insights": ["user feedback or corrections (not preferences)", "0-5 items"],
  "user_preferences": ["explicit preferences or constraints from the user", "0-5 items"],
  "time_sinks": ["things that took unusually long or required repeated attempts", "0-5 items"],
  "tags": ["topic", "tags", "3-7 items"],
  "key_files": ["important/files/modified.go", "up to 10"],
  "tools_pattern": "Common sequence like Read->Edit->Bash(test)",
  "key_questions": ["semantic search queries to quickly re-understand this work area", "3-5 items"]
}

Be concise and specific. Focus on:
- What was the main goal and outcome?
- What important technical decisions were made?
- What problems were encountered and how were they solved?
- What explicit user preferences or constraints were stated?
- What dragged on or required repeated back-and-forth?
- What files were most important?
- What did the user explicitly ask for, correct, criticize, or provide feedback on?
  Look for phrases like: "that's not right", "I meant", "don't do", "please", "actually", "no,", "yes,", "let's", "can you"

For key_questions: Generate 3-5 semantic search queries that would help someone resuming this session quickly understand where key code/concepts are in the codebase. Examples:
- "where is the authentication middleware implemented"
- "how does the todo continuation hook work"
- "what handles session state persistence"
These should be natural language questions that target important concepts touched in this session.

<conversation>
%s
</conversation>

Respond ONLY with the JSON object, no other text.`, filteredJSON)
}

func parseSummaryResponse(response string) (*SummaryResponse, error) {
	response = strings.TrimSpace(response)

	// Remove markdown code blocks if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		response = strings.Join(jsonLines, "\n")
	}

	// Find JSON object boundaries
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || start >= end {
		return nil, skillerr.Parse("no JSON object found in response")
	}
	response = response[start : end+1]

	// Clean up common LLM JSON issues
	response = cleanJSONResponse(response)

	var summary SummaryResponse
	if err := json.Unmarshal([]byte(response), &summary); err != nil {
		return nil, skillerr.Parsef("parse summary JSON: %v (response: %s)", err, skillout.TruncateSingleLine(response, 200))
	}

	return &summary, nil
}

// cleanJSONResponse removes common issues LLMs add to JSON output.
func cleanJSONResponse(s string) string {
	// Remove single-line comments (// ...)
	// Must be careful not to remove // inside strings
	lines := strings.Split(s, "\n")
	var cleanedLines []string
	for _, line := range lines {
		// Find // that's not inside a string
		cleaned := removeLineComment(line)
		cleanedLines = append(cleanedLines, cleaned)
	}
	s = strings.Join(cleanedLines, "\n")

	// Remove multi-line comments (/* ... */)
	for {
		start := strings.Index(s, "/*")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "*/")
		if end == -1 {
			// Unterminated comment, remove to end
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+2:]
	}

	// Remove trailing commas before ] or }
	// Pattern: comma followed by optional whitespace and ] or }
	s = removeTrailingCommas(s)

	return s
}

// removeLineComment removes // comments that are not inside strings.
func removeLineComment(line string) string {
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if !inString && i+1 < len(line) && c == '/' && line[i+1] == '/' {
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}

// removeTrailingCommas removes commas before ] or }.
func removeTrailingCommas(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	inString := false
	escaped := false
	lastCommaPos := -1

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			escaped = false
			_ = result.WriteByte(c)
			continue
		}

		if c == '\\' && inString {
			escaped = true
			_ = result.WriteByte(c)
			continue
		}

		if c == '"' {
			inString = !inString
			_ = result.WriteByte(c)
			lastCommaPos = -1
			continue
		}

		if !inString {
			if c == ',' {
				lastCommaPos = result.Len()
				_ = result.WriteByte(c)
				continue
			}

			// Skip whitespace tracking
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				_ = result.WriteByte(c)
				continue
			}

			// If we hit ] or } and had a trailing comma, remove it
			if (c == ']' || c == '}') && lastCommaPos >= 0 {
				// Rebuild without the comma
				current := result.String()
				result.Reset()
				result.WriteString(current[:lastCommaPos])
				result.WriteString(current[lastCommaPos+1:])
			}
			lastCommaPos = -1
		}

		_ = result.WriteByte(c)
	}

	return result.String()
}

type scoredSeedWindow struct {
	Window     sessions.ContextWindow
	Similarity float64
}

func buildSeedPrompt(ctx context.Context, sessionStore *sessions.Store, session sessions.Session, summary *SummaryResponse, in Input, cfg config.Config, embedOpts ...semantic.EmbedderOption) (string, error) {
	maxChars := in.SeedMaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		query = buildSeedQuery(session, summary)
	}

	windows, err := sessionStore.GetContextWindows(ctx, session.ID)
	if err != nil {
		return "", skillerr.WrapIO("get context windows", err)
	}

	var latest *sessions.ContextWindow
	if len(windows) > 0 {
		latest = &windows[len(windows)-1]
	}

	var queryEmbedding []float32
	if query != "" {
		if emb, _, err := embedSeedQuery(ctx, query, cfg, embedOpts...); err == nil {
			queryEmbedding = emb
		}
	}

	selected := selectSeedWindows(windows, latest, queryEmbedding, in.SeedTopKWindows)

	var b strings.Builder
	appendLine := func(s string) bool {
		if s == "" {
			s = ""
		}
		need := len(s) + 1
		if b.Len()+need > maxChars {
			return false
		}
		b.WriteString(s)
		b.WriteString("\n")
		return true
	}

	appendLine("## Session Seed")
	appendLine("")
	if summary != nil && strings.TrimSpace(summary.Summary) != "" {
		appendLine("### Summary")
		appendLine(summary.Summary)
		appendLine("")
	}
	if summary != nil && len(summary.Decisions) > 0 {
		appendLine("### Decisions")
		for _, d := range summary.Decisions {
			if d = strings.TrimSpace(d); d == "" {
				continue
			}
			if !appendLine("- " + d) {
				break
			}
		}
		appendLine("")
	}
	if summary != nil && len(summary.Gotchas) > 0 {
		appendLine("### Gotchas")
		for _, g := range summary.Gotchas {
			if g = strings.TrimSpace(g); g == "" {
				continue
			}
			if !appendLine("- " + g) {
				break
			}
		}
		appendLine("")
	}

	if len(selected) > 0 {
		appendLine("### Relevant Context")
		for _, w := range selected {
			window := w.Window
			header := fmt.Sprintf("#### Window %d (%s)", window.WindowIndex, strings.TrimSpace(window.Trigger))
			if strings.TrimSpace(window.Trigger) == "" {
				header = fmt.Sprintf("#### Window %d", window.WindowIndex)
			}
			if !appendLine(header) {
				break
			}
			if window.Summary != "" {
				if !appendLine(skillout.TruncateSingleLine(window.Summary, 800)) {
					break
				}
			}

			maxChunks := in.SeedChunksPerWindow
			if maxChunks <= 0 {
				maxChunks = 10
			}
			indices := sampleChunkIndices(window.ChunkStart, window.ChunkEnd, maxChunks)
			for _, idx := range indices {
				chunk, err := sessionStore.GetChunk(ctx, session.ID, idx)
				if err != nil {
					continue
				}
				line := fmt.Sprintf("- [%s #%d] %s", chunk.ChunkType, chunk.ChunkIndex, skillout.TruncateSingleLine(chunk.ContentPreview, 240))
				if !appendLine(line) {
					break
				}
			}
			appendLine("")
		}
	}

	// Fallback: if we couldn't include any windows/chunks, include recent filtered messages.
	if len(selected) == 0 && session.RawJSONLPath != "" {
		filtered, _, err := filterJSONL(ctx, session.RawJSONLPath, 800)
		if err == nil && len(filtered) > 0 {
			appendLine("### Recent Messages")
			start := 0
			if len(filtered) > 8 {
				start = len(filtered) - 8
			}
			for _, fm := range filtered[start:] {
				role := fm.Role
				if role == "" {
					role = "msg"
				}
				text := skillout.TruncateSingleLine(fm.Content, 240)
				if !appendLine(fmt.Sprintf("- [%s] %s", role, text)) {
					break
				}
			}
		}
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", skillerr.Validation("seed prompt empty")
	}
	return out, nil
}

func buildSeedQuery(session sessions.Session, summary *SummaryResponse) string {
	var parts []string
	if summary != nil {
		if s := strings.TrimSpace(summary.Summary); s != "" {
			parts = append(parts, s)
		}
		if len(summary.Decisions) > 0 {
			parts = append(parts, "Decisions: "+strings.Join(summary.Decisions, "; "))
		}
		if len(summary.Gotchas) > 0 {
			parts = append(parts, "Gotchas: "+strings.Join(summary.Gotchas, "; "))
		}
	}
	if len(parts) == 0 {
		if s := strings.TrimSpace(session.Summary); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func embedSeedQuery(ctx context.Context, text string, cfg config.Config, embedOpts ...semantic.EmbedderOption) ([]float32, string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, "", nil
	}

	// Use Embedder with Gemini fallback for query embedding
	opts := append([]semantic.EmbedderOption{semantic.WithAllowFallback(true)}, embedOpts...)
	embedder, err := semantic.NewEmbedderFromConfig(
		semantic.ScopeSessions,
		cfg,
		opts...,
	)
	if err != nil {
		return nil, "", nil
	}

	result, err := embedder.Embed(ctx, text)
	if err != nil {
		return nil, "", err
	}
	return result.Vec, result.Model, nil
}

func selectSeedWindows(all []sessions.ContextWindow, latest *sessions.ContextWindow, queryEmbedding []float32, topK int) []scoredSeedWindow {
	var out []scoredSeedWindow
	seen := map[int]bool{}
	if latest != nil {
		out = append(out, scoredSeedWindow{Window: *latest, Similarity: 1})
		seen[latest.WindowIndex] = true
	}

	if topK <= 0 {
		return out
	}
	if len(queryEmbedding) == 0 {
		// No embedding query; include up to topK previous windows.
		for i := len(all) - 2; i >= 0 && len(out) < topK+1; i-- {
			w := all[i]
			if seen[w.WindowIndex] {
				continue
			}
			out = append(out, scoredSeedWindow{Window: w, Similarity: 0})
			seen[w.WindowIndex] = true
		}
		return out
	}

	var scored []scoredSeedWindow
	for _, w := range all {
		if seen[w.WindowIndex] {
			continue
		}
		if len(w.Embedding) == 0 {
			continue
		}
		we := vector.DeserializeF32(w.Embedding)
		if len(we) == 0 || len(we) != len(queryEmbedding) {
			continue
		}
		sim := vector.Cosine(queryEmbedding, we)
		scored = append(scored, scoredSeedWindow{Window: w, Similarity: sim})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Similarity > scored[j].Similarity })
	if len(scored) > topK {
		scored = scored[:topK]
	}
	out = append(out, scored...)
	return out
}

func sampleChunkIndices(start, end, max int) []int {
	if start < 0 || end < start {
		return nil
	}
	count := end - start + 1
	if max <= 0 || count <= max {
		idx := make([]int, 0, count)
		for i := start; i <= end; i++ {
			idx = append(idx, i)
		}
		return idx
	}
	// Take a balanced prefix+suffix sample.
	prefix := max / 2
	suffix := max - prefix
	idx := make([]int, 0, max)
	for i := 0; i < prefix; i++ {
		idx = append(idx, start+i)
	}
	for i := suffix - 1; i >= 0; i-- {
		idx = append(idx, end-i)
	}
	return idx
}

// reembedAll re-embeds sessions and context windows that have wrong embedding model/dimensions.
// This is a no-LLM operation - it only calls the embedding API using existing summaries.
// Skips items that already have correct embeddings (voyage-3.5, 1024 dims = 4096 bytes).
func reembedAll(ctx context.Context, sessionStore *sessions.Store, input Input, cfg config.Config, embedOpts ...semantic.EmbedderOption) Output {
	output := Output{
		Status: "reembed_complete",
	}

	// Create embedder for sessions scope
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeSessions, cfg, embedOpts...)
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("no embedding provider: %v", err)
		return output
	}

	expectedModel := embedder.Model()
	const expectedEmbeddingBytes = 4096 // 1024 float32s = 4096 bytes

	// needsReembed checks if an item needs re-embedding
	needsReembed := func(embeddingModel string, embeddingLen int) bool {
		if input.Force {
			return true
		}
		// Skip if already correct model and dimensions
		if embeddingModel == expectedModel && embeddingLen == expectedEmbeddingBytes {
			return false
		}
		// Re-embed if wrong model or wrong dimensions
		return embeddingLen > 0 || embeddingModel != ""
	}

	// Re-embed sessions
	allSessions, err := sessionStore.List(ctx, sessions.ListOptions{Limit: 1000})
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("failed to list sessions: %v", err)
		return output
	}

	for _, sess := range allSessions {
		// Skip sessions without summaries
		if strings.TrimSpace(sess.Summary) == "" {
			continue
		}

		// Check if needs re-embedding
		if !needsReembed(sess.EmbeddingModel, len(sess.Embedding)) {
			output.SessionsSkipped++
			continue
		}

		// Build embedding text from session
		embeddingText := buildEmbeddingTextFromSession(sess)
		if embeddingText == "" {
			output.SessionsSkipped++
			continue
		}

		// Generate new embedding
		result, err := embedder.Embed(ctx, embeddingText)
		if err != nil {
			logger.Warn("failed to embed session", obs.Str("session_id", sess.ID), obs.Err(err))
			continue
		}

		// Serialize and update session with new embedding
		embeddingBytes := vector.SerializeF32(result.Vec)
		if err := sessionStore.SetEmbedding(ctx, sess.ID, embeddingBytes, expectedModel); err != nil {
			logger.Warn("failed to update session embedding", obs.Str("session_id", sess.ID), obs.Err(err))
			continue
		}
		output.SessionsReembedded++
	}

	// Re-embed context windows
	for _, sess := range allSessions {
		windows, err := sessionStore.GetContextWindows(ctx, sess.ID)
		if err != nil {
			continue
		}

		for _, win := range windows {
			// Skip windows without summaries
			if strings.TrimSpace(win.Summary) == "" {
				continue
			}

			// Check if needs re-embedding
			if !needsReembed(win.EmbeddingModel, len(win.Embedding)) {
				continue
			}

			// Generate new embedding with date prefix
			windowText := buildWindowEmbeddingTextFromWindow(win)
			result, err := embedder.Embed(ctx, windowText)
			if err != nil {
				logger.Warn("failed to embed window", obs.Str("window_id", win.ID), obs.Err(err))
				continue
			}

			// Serialize embedding
			embeddingBytes := vector.SerializeF32(result.Vec)

			// Update window embedding only (summary unchanged)
			if err := sessionStore.SetContextWindowEmbedding(ctx, win.ID, embeddingBytes, expectedModel); err != nil {
				logger.Warn("failed to update window embedding", obs.Str("window_id", win.ID), obs.Err(err))
				continue
			}
			output.WindowsReembedded++
		}
	}

	output.Message = fmt.Sprintf("Re-embedded %d sessions (%d skipped) and %d windows",
		output.SessionsReembedded, output.SessionsSkipped, output.WindowsReembedded)
	return output
}

// buildEmbeddingTextFromSession creates embedding text from an existing session's fields.
// Format: [Jan 2, 2026] [activity] Summary\nAccomplished: ...\nFiles: ...\nTopics: ...
func buildEmbeddingTextFromSession(sess sessions.Session) string {
	var parts []string

	// Date and activity type prefix
	dateStr := sess.StartedAt.Format("Jan 2, 2006")
	activity := inferActivityType(sess.Tags)
	parts = append(parts, fmt.Sprintf("[%s] [%s]", dateStr, activity))

	if s := strings.TrimSpace(sess.Summary); s != "" {
		parts = append(parts, s)
	}
	if len(sess.Accomplished) > 0 {
		parts = append(parts, "Accomplished: "+strings.Join(sess.Accomplished, "; "))
	}
	if len(sess.Decisions) > 0 {
		parts = append(parts, "Decisions: "+strings.Join(sess.Decisions, "; "))
	}
	if len(sess.Gotchas) > 0 {
		parts = append(parts, "Gotchas: "+strings.Join(sess.Gotchas, "; "))
	}
	if len(sess.KeyFiles) > 0 {
		parts = append(parts, "Files: "+strings.Join(sess.KeyFiles, ", "))
	}
	if len(sess.Tags) > 0 {
		parts = append(parts, "Topics: "+strings.Join(sess.Tags, ", "))
	}
	return strings.Join(parts, "\n")
}

// buildWindowEmbeddingTextFromWindow creates embedding text for a context window.
// Format: [Jan 2, 2026 15:04] [auto] Summary text
func buildWindowEmbeddingTextFromWindow(win sessions.ContextWindow) string {
	dateStr := win.StartedAt.Format("Jan 2, 2006 15:04")
	trigger := win.Trigger
	if trigger == "" {
		trigger = "context"
	}
	return fmt.Sprintf("[%s] [%s] %s", dateStr, trigger, win.Summary)
}

// inferActivityType extracts activity type from session tags.
func inferActivityType(tags []string) string {
	for _, tag := range tags {
		lower := strings.ToLower(tag)
		switch {
		case strings.Contains(lower, "debug"):
			return "debugging"
		case strings.Contains(lower, "fix") || strings.Contains(lower, "bug"):
			return "bug-fix"
		case strings.Contains(lower, "feature") || strings.Contains(lower, "implement"):
			return "feature"
		case strings.Contains(lower, "refactor"):
			return "refactoring"
		case strings.Contains(lower, "test"):
			return "testing"
		case strings.Contains(lower, "doc"):
			return "documentation"
		case strings.Contains(lower, "review"):
			return "code-review"
		case strings.Contains(lower, "setup") || strings.Contains(lower, "config"):
			return "setup"
		}
	}
	return "development"
}

// summarizeWindowsQueued processes window summaries via the queue.
func summarizeWindowsQueued(ctx context.Context, sessionStore *sessions.Store, session sessions.Session, providers []LLMProvider, input Input, cfg config.Config, embedOpts ...semantic.EmbedderOption) Output {
	output := Output{
		SessionID: session.ID,
		Status:    "windows_queued",
	}

	windows, err := sessionStore.GetContextWindows(ctx, session.ID)
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("failed to get windows: %v", err)
		return output
	}

	if len(windows) == 0 {
		output.Status = "no_windows"
		output.Message = "no context windows found for session"
		return output
	}

	tasks, alreadyDone, skippedWindows := buildWindowTasks(windows, input.Force)

	queueStore, err := queue.OpenInRoot(ctx, cfg.Storage.Root, summaryQueueDBName, queue.Options{Table: summaryQueueTable})
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("open summary queue: %v", err)
		return output
	}
	defer queueStore.Close()

	enqueueResult, err := enqueueWindowTasks(ctx, queueStore, session.ID, tasks, input.Force)
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("enqueue windows: %v", err)
		return output
	}
	output.WindowsQueued = enqueueResult.Queued

	totalSkipped := alreadyDone + skippedWindows + enqueueResult.Skipped

	if input.QueueOnly {
		remaining := queueRemaining(ctx, queueStore, session.ID)
		output.WindowsSkipped = totalSkipped
		output.WindowsRemaining = remaining
		if remaining > 0 {
			output.Message = fmt.Sprintf("Queued %d windows (%d skipped), %d remaining in queue for session %s",
				output.WindowsQueued, totalSkipped, remaining, session.ID)
		} else {
			output.Message = fmt.Sprintf("Queued %d windows (%d skipped) for session %s",
				output.WindowsQueued, totalSkipped, session.ID)
		}
		return output
	}

	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = 5
	}

	chunkProviders := chunkSummaryProviders(providers)
	embedder, embedderErr := semantic.NewEmbedderFromConfig(
		semantic.ScopeSessions,
		cfg,
		append([]semantic.EmbedderOption{semantic.WithAllowFallback(true)}, embedOpts...)...,
	)

	totalSummarized := 0
	totalEmbedded := 0
	batchCount := 0

	for {
		var batchTasks []windowTask
		var batchTasksForced []windowTask
		var jobIDs []string
		var jobIDsForced []string
		batchSkipped := 0

		for len(batchTasks)+len(batchTasksForced) < batchSize {
			job, err := queueStore.ClaimNext(ctx, queue.ClaimOptions{GroupID: session.ID})
			if err != nil {
				output.Status = "error"
				output.Message = fmt.Sprintf("claim queue job: %v", err)
				return output
			}
			if job == nil {
				break
			}

			payload, err := decodeWindowQueuePayload(job.Payload)
			if err != nil {
				logger.Warn("invalid queue payload", obs.Str("job_id", job.ID), obs.Err(err))
				_ = queueStore.Fail(ctx, job.ID, fmt.Sprintf("decode payload: %v", err))
				continue
			}
			if payload.SessionID != "" && payload.SessionID != session.ID {
				logger.Warn("queue payload session mismatch", obs.Str("payload_session", payload.SessionID), obs.Str("expected_session", session.ID))
				_ = queueStore.Fail(ctx, job.ID, "session mismatch")
				continue
			}

			window, err := sessionStore.GetContextWindow(ctx, session.ID, payload.WindowIndex)
			if err != nil {
				logger.Warn("missing window", obs.Int("window_index", payload.WindowIndex), obs.Err(err))
				_ = queueStore.Fail(ctx, job.ID, fmt.Sprintf("get window %d: %v", payload.WindowIndex, err))
				continue
			}

			effectiveForce := input.Force || payload.Force
			task, ok := buildWindowTaskFromWindow(window, effectiveForce)
			if !ok {
				batchSkipped++
				_ = queueStore.Complete(ctx, job.ID)
				continue
			}

			if effectiveForce {
				batchTasksForced = append(batchTasksForced, task)
				jobIDsForced = append(jobIDsForced, job.ID)
			} else {
				batchTasks = append(batchTasks, task)
				jobIDs = append(jobIDs, job.ID)
			}
		}

		if len(batchTasks) == 0 && len(batchTasksForced) == 0 {
			remaining := queueRemaining(ctx, queueStore, session.ID)
			output.WindowsSummarized = totalSummarized
			output.WindowsReembedded = totalEmbedded
			output.WindowsSkipped = totalSkipped + batchSkipped
			output.WindowsRemaining = remaining

			if remaining == 0 {
				output.Status = "windows_summarized"
				output.Message = fmt.Sprintf("Summarized %d windows (%d embedded, %d skipped) for session %s",
					totalSummarized, totalEmbedded, output.WindowsSkipped, session.ID)
				return output
			}
			if !input.ProcessAll {
				output.Status = "windows_partial"
				output.Message = fmt.Sprintf("Processed %d windows (%d embedded), %d remaining in queue for session %s",
					totalSummarized, totalEmbedded, remaining, session.ID)
				return output
			}

			output.Status = "windows_partial"
			output.Message = fmt.Sprintf("No eligible jobs to claim; %d remaining in queue for session %s",
				remaining, session.ID)
			return output
		}

		if len(batchTasks) > 0 {
			batchSummarized, batchEmbedded, batchSkippedBatch := processBatch(ctx, sessionStore, session.ID, batchTasks, providers, chunkProviders, embedder, embedderErr, false)
			totalSummarized += batchSummarized
			totalEmbedded += batchEmbedded
			totalSkipped += batchSkippedBatch
			batchCount++
			for _, jobID := range jobIDs {
				if err := queueStore.Complete(ctx, jobID); err != nil {
					logger.Warn("failed to complete queue job", obs.Str("job_id", jobID), obs.Err(err))
				}
			}
		}

		if len(batchTasksForced) > 0 {
			batchSummarized, batchEmbedded, batchSkippedBatch := processBatch(ctx, sessionStore, session.ID, batchTasksForced, providers, chunkProviders, embedder, embedderErr, true)
			totalSummarized += batchSummarized
			totalEmbedded += batchEmbedded
			totalSkipped += batchSkippedBatch
			batchCount++
			for _, jobID := range jobIDsForced {
				if err := queueStore.Complete(ctx, jobID); err != nil {
					logger.Warn("failed to complete queue job", obs.Str("job_id", jobID), obs.Err(err))
				}
			}
		}

		totalSkipped += batchSkipped

		remaining := queueRemaining(ctx, queueStore, session.ID)
		if !input.ProcessAll {
			output.WindowsSummarized = totalSummarized
			output.WindowsReembedded = totalEmbedded
			output.WindowsSkipped = totalSkipped
			output.WindowsRemaining = remaining
			if remaining > 0 {
				output.Status = "windows_partial"
				output.Message = fmt.Sprintf("Processed %d windows (%d embedded), %d remaining in queue for session %s",
					totalSummarized, totalEmbedded, remaining, session.ID)
			} else {
				output.Status = "windows_summarized"
				output.Message = fmt.Sprintf("Summarized %d windows (%d embedded, %d skipped) for session %s",
					totalSummarized, totalEmbedded, totalSkipped, session.ID)
			}
			return output
		}

		if remaining == 0 {
			output.WindowsSummarized = totalSummarized
			output.WindowsReembedded = totalEmbedded
			output.WindowsSkipped = totalSkipped
			output.WindowsRemaining = 0
			output.Status = "windows_summarized"
			output.Message = fmt.Sprintf("Summarized %d windows (%d embedded, %d skipped) in %d batches for session %s",
				totalSummarized, totalEmbedded, totalSkipped, batchCount, session.ID)
			return output
		}

		if ctx.Err() != nil {
			output.Status = "interrupted"
			output.WindowsSummarized = totalSummarized
			output.WindowsReembedded = totalEmbedded
			output.WindowsSkipped = totalSkipped
			output.WindowsRemaining = remaining
			output.Message = fmt.Sprintf("Interrupted after %d batches (%d windows), %d remaining",
				batchCount, totalSummarized, remaining)
			return output
		}
	}
}

// summarizeWindows generates LLM-based summaries for context windows with batch processing.
// When process_all=true, loops until all windows are done. Otherwise returns after one batch.
func summarizeWindows(ctx context.Context, sessionStore *sessions.Store, session sessions.Session, providers []LLMProvider, input Input, cfg config.Config, embedOpts ...semantic.EmbedderOption) Output {
	output := Output{
		SessionID: session.ID,
		Status:    "windows_summarized",
	}

	if input.Queue {
		return summarizeWindowsQueued(ctx, sessionStore, session, providers, input, cfg, embedOpts...)
	}

	// Default batch size: 5 windows per batch
	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = 5
	}

	chunkProviders := chunkSummaryProviders(providers)

	// Create embedder once for reuse across all batches
	embedder, embedderErr := semantic.NewEmbedderFromConfig(
		semantic.ScopeSessions,
		cfg,
		append([]semantic.EmbedderOption{semantic.WithAllowFallback(true)}, embedOpts...)...,
	)

	// Totals across all batches
	totalSummarized := 0
	totalEmbedded := 0
	totalSkipped := 0
	batchCount := 0

	for {
		// Get fresh window list each iteration (to see newly summarized ones)
		windows, err := sessionStore.GetContextWindows(ctx, session.ID)
		if err != nil {
			output.Status = "error"
			output.Message = fmt.Sprintf("failed to get windows: %v", err)
			return output
		}

		if len(windows) == 0 {
			if batchCount == 0 {
				output.Status = "no_windows"
				output.Message = "no context windows found for session"
			}
			return output
		}

		// Build work list (summarize or embed-only)
		tasks, alreadyDone, skippedWindows := buildWindowTasks(windows, input.Force)

		// Nothing left to process
		if len(tasks) == 0 {
			output.WindowsSummarized = totalSummarized
			output.WindowsSkipped = alreadyDone + totalSkipped
			output.WindowsReembedded = totalEmbedded
			output.WindowsRemaining = 0
			output.Message = fmt.Sprintf("Summarized %d windows (%d embedded, %d skipped) for session %s",
				totalSummarized, totalEmbedded, alreadyDone+totalSkipped, session.ID)
			return output
		}

		// Apply batch limit
		batch := tasks
		remaining := 0
		if len(batch) > batchSize {
			batch = batch[:batchSize]
			remaining = len(tasks) - batchSize
		}

		// Process this batch
		batchSummarized, batchEmbedded, batchSkipped := processBatch(ctx, sessionStore, session.ID, batch, providers, chunkProviders, embedder, embedderErr, input.Force)
		totalSummarized += batchSummarized
		totalEmbedded += batchEmbedded
		totalSkipped += batchSkipped + skippedWindows
		batchCount++

		// If not process_all mode, return after one batch
		if !input.ProcessAll {
			output.WindowsSummarized = totalSummarized
			output.WindowsSkipped = alreadyDone + totalSkipped
			output.WindowsReembedded = totalEmbedded
			output.WindowsRemaining = remaining

			if remaining > 0 {
				output.Status = "windows_partial"
				output.Message = fmt.Sprintf("Processed %d windows (%d embedded), %d remaining. Run again to continue.",
					totalSummarized, totalEmbedded, remaining)
			} else {
				output.Message = fmt.Sprintf("Summarized %d windows (%d embedded, %d skipped) for session %s",
					totalSummarized, totalEmbedded, alreadyDone+totalSkipped, session.ID)
			}
			return output
		}

		// process_all mode: continue if there are more windows
		if remaining == 0 {
			output.WindowsSummarized = totalSummarized
			output.WindowsSkipped = alreadyDone + totalSkipped
			output.WindowsReembedded = totalEmbedded
			output.WindowsRemaining = 0
			output.Message = fmt.Sprintf("Summarized %d windows (%d embedded, %d skipped) in %d batches for session %s",
				totalSummarized, totalEmbedded, alreadyDone+totalSkipped, batchCount, session.ID)
			return output
		}

		// Check context cancellation between batches
		if ctx.Err() != nil {
			output.Status = "interrupted"
			output.WindowsSummarized = totalSummarized
			output.WindowsSkipped = totalSkipped
			output.WindowsReembedded = totalEmbedded
			output.WindowsRemaining = remaining
			output.Message = fmt.Sprintf("Interrupted after %d batches (%d windows), %d remaining",
				batchCount, totalSummarized, remaining)
			return output
		}
	}
}

// processBatch summarizes a batch of windows, returning counts.
const (
	windowSummaryHintMaxChars  = 1200
	windowContentTokensReserve = 1200
	windowContentTokensMin     = 1500
	windowContentTokensMax     = 8000
	windowContentHeadChunks    = 4
	windowContentTailChunks    = 4

	windowChunkSummaryTokensReserve = 800
	windowChunkSummaryTokensMin     = 2400
	windowChunkSummaryTokensMax     = 8000

	windowSummaryMaxTokens         = 500
	windowChunkTokensMax           = 1800
	windowChunkSummaryMaxTokens    = 300
	windowChunkSummaryLineMaxChars = 600
	windowChunkSummariesMaxChars   = 6000
	windowChunkIndicesDisplayMax   = 12
	windowChunkEmbeddingMaxChars   = 6000

	chunkSummaryModelEnv      = "CEREBRAS_CHUNK_MODEL"
	chunkSummaryDefaultModel  = "zai-glm-4.7"
	windowSummaryModelEnv     = "CEREBRAS_MODEL"
	windowSummaryDefaultModel = "zai-glm-4.7"
)

type windowTask struct {
	Window    sessions.ContextWindow
	Summarize bool
}

type windowQueuePayload struct {
	SessionID   string `json:"session_id"`
	WindowIndex int    `json:"window_index"`
	Force       bool   `json:"force,omitempty"`
}

func buildWindowTaskFromWindow(window sessions.ContextWindow, force bool) (windowTask, bool) {
	if shouldSkipWindow(window) {
		return windowTask{}, false
	}
	summary := strings.TrimSpace(window.Summary)
	if isPlaceholderSummary(summary) {
		summary = ""
	}
	if force || summary == "" {
		return windowTask{Window: window, Summarize: true}, true
	}
	if len(window.Embedding) == 0 {
		return windowTask{Window: window, Summarize: false}, true
	}
	return windowTask{}, false
}

func enqueueWindowTasks(ctx context.Context, store *queue.Store, sessionID string, tasks []windowTask, force bool) (*queue.EnqueueResult, error) {
	if len(tasks) == 0 {
		return &queue.EnqueueResult{}, nil
	}
	requests := make([]queue.EnqueueRequest, 0, len(tasks))
	for _, task := range tasks {
		payload, err := json.Marshal(windowQueuePayload{
			SessionID:   sessionID,
			WindowIndex: task.Window.WindowIndex,
			Force:       force,
		})
		if err != nil {
			return nil, err
		}
		requests = append(requests, queue.EnqueueRequest{
			GroupID:   sessionID,
			Payload:   payload,
			DedupeKey: windowQueueDedupeKey(task.Window.WindowIndex, force),
			Priority:  queue.PriorityNormal,
		})
	}
	return store.EnqueueBatch(ctx, requests)
}

func windowQueueDedupeKey(windowIndex int, force bool) string {
	if force {
		return fmt.Sprintf("window:%d:force", windowIndex)
	}
	return fmt.Sprintf("window:%d", windowIndex)
}

func decodeWindowQueuePayload(payload []byte) (windowQueuePayload, error) {
	var decoded windowQueuePayload
	if len(payload) == 0 {
		return decoded, fmt.Errorf("empty payload")
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return decoded, err
	}
	if decoded.WindowIndex < 0 {
		return decoded, fmt.Errorf("invalid window_index: %d", decoded.WindowIndex)
	}
	return decoded, nil
}

func queueRemaining(ctx context.Context, store *queue.Store, sessionID string) int {
	stats, err := store.Stats(ctx, sessionID)
	if err != nil || stats == nil {
		return 0
	}
	return stats.QueuedCount + stats.RunningCount
}

func buildWindowTasks(windows []sessions.ContextWindow, force bool) (tasks []windowTask, alreadyDone int, skipped int) {
	for _, window := range windows {
		if shouldSkipWindow(window) {
			skipped++
			continue
		}
		summary := strings.TrimSpace(window.Summary)
		if isPlaceholderSummary(summary) {
			summary = ""
		}
		if force || summary == "" {
			tasks = append(tasks, windowTask{Window: window, Summarize: true})
			continue
		}
		if len(window.Embedding) == 0 {
			tasks = append(tasks, windowTask{Window: window, Summarize: false})
			continue
		}
		alreadyDone++
	}
	return
}

type windowChunkCandidate struct {
	Chunk    sessions.SessionChunk
	Preview  string
	Tokens   int
	HasTools bool
	HasFiles bool
	IsError  bool
}

type windowSummaryMeta struct {
	WindowIndex int
	Trigger     string
	Tools       []string
	Files       []string
	Errors      []string
}

type windowChunkSummaryMeta struct {
	WindowIndex  int
	Trigger      string
	ChunkIndices []int
	Tools        []string
	Files        []string
	Errors       []string
}

type windowChunkGroup struct {
	Candidates []windowChunkCandidate
	Meta       windowChunkSummaryMeta
	Content    string
}

type windowChunkSummary struct {
	Meta       windowChunkSummaryMeta
	Summary    string
	Model      string
	Candidates []windowChunkCandidate
}

var placeholderSummaryValues = map[string]struct{}{
	"none":                {},
	"n/a":                 {},
	"na":                  {},
	"no details":          {},
	"no specific content": {},
	"no information":      {},
	"no content":          {},
	"not provided":        {},
	"not mentioned":       {},
	"unknown":             {},
}

func isPlaceholderSummary(summary string) bool {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return true
	}
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		value := line
		if idx := strings.Index(line, ":"); idx != -1 {
			value = strings.TrimSpace(line[idx+1:])
		}
		value = strings.ToLower(strings.Trim(value, " \t-()[].{}"))
		if value == "" {
			continue
		}
		if _, ok := placeholderSummaryValues[value]; ok {
			continue
		}
		return false
	}
	return true
}

func shouldSkipWindow(window sessions.ContextWindow) bool {
	trigger := strings.ToLower(strings.TrimSpace(window.Trigger))
	if trigger == "" {
		return false
	}
	if trigger == "auto" || trigger == "manual" {
		if window.MessageCount <= 4 || window.ChunkStart == window.ChunkEnd {
			return true
		}
	}
	return false
}

func windowContentBudget(providers []LLMProvider) int {
	minTokens := 0
	for _, p := range providers {
		if p.MaxTokens <= 0 {
			continue
		}
		if minTokens == 0 || p.MaxTokens < minTokens {
			minTokens = p.MaxTokens
		}
	}
	if minTokens == 0 {
		minTokens = windowContentTokensMax
	}
	budget := minTokens - windowContentTokensReserve
	if budget < windowContentTokensMin {
		budget = windowContentTokensMin
	}
	if budget > windowContentTokensMax {
		budget = windowContentTokensMax
	}
	return budget
}

func windowChunkSummaryBudget(providers []LLMProvider) int {
	minTokens := 0
	for _, p := range providers {
		if p.MaxTokens <= 0 {
			continue
		}
		if minTokens == 0 || p.MaxTokens < minTokens {
			minTokens = p.MaxTokens
		}
	}
	if minTokens == 0 {
		minTokens = windowChunkSummaryTokensMax
	}
	budget := minTokens - windowChunkSummaryTokensReserve
	if budget < windowChunkSummaryTokensMin {
		budget = windowChunkSummaryTokensMin
	}
	if budget > windowChunkSummaryTokensMax {
		budget = windowChunkSummaryTokensMax
	}
	return budget
}

func selectWindowChunks(candidates []windowChunkCandidate, maxTokens int) []windowChunkCandidate {
	if len(candidates) == 0 || maxTokens <= 0 {
		return nil
	}
	selected := make(map[int]windowChunkCandidate, len(candidates))
	tokensUsed := 0
	add := func(candidate windowChunkCandidate) bool {
		if candidate.Tokens <= 0 || candidate.Preview == "" {
			return true
		}
		index := candidate.Chunk.ChunkIndex
		if _, ok := selected[index]; ok {
			return true
		}
		if tokensUsed+candidate.Tokens > maxTokens {
			return false
		}
		selected[index] = candidate
		tokensUsed += candidate.Tokens
		return true
	}

	for _, candidate := range candidates {
		if candidate.IsError {
			if !add(candidate) {
				break
			}
		}
	}

	for _, candidate := range candidates {
		if candidate.IsError || (!candidate.HasFiles && !candidate.HasTools) {
			continue
		}
		if !add(candidate) {
			break
		}
	}

	head := windowContentHeadChunks
	if head > len(candidates) {
		head = len(candidates)
	}
	for i := 0; i < head; i++ {
		if !add(candidates[i]) {
			break
		}
	}

	tail := windowContentTailChunks
	if tail > len(candidates) {
		tail = len(candidates)
	}
	for i := len(candidates) - tail; i < len(candidates); i++ {
		if i < 0 {
			continue
		}
		if !add(candidates[i]) {
			break
		}
	}

	i := 0
	j := len(candidates) - 1
	for tokensUsed < maxTokens && i <= j {
		if !add(candidates[i]) {
			break
		}
		i++
		if i > j {
			break
		}
		if !add(candidates[j]) {
			break
		}
		j--
	}

	out := make([]windowChunkCandidate, 0, len(selected))
	for _, candidate := range candidates {
		if _, ok := selected[candidate.Chunk.ChunkIndex]; ok {
			out = append(out, candidate)
		}
	}
	return out
}

func chunkSummaryProviders(providers []LLMProvider) []LLMProvider {
	if len(providers) == 0 {
		return nil
	}
	model := strings.TrimSpace(os.Getenv(chunkSummaryModelEnv))
	if model == "" {
		model = chunkSummaryDefaultModel
	}
	out := make([]LLMProvider, 0, len(providers))
	for _, p := range providers {
		if p.Name == "cerebras" {
			p.Model = model
			out = append(out, p)
		}
	}
	for _, p := range providers {
		if p.Name != "cerebras" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return providers
	}
	return out
}

func windowSummaryProviders(providers []LLMProvider) []LLMProvider {
	if len(providers) == 0 {
		return nil
	}
	preferred := strings.TrimSpace(os.Getenv(windowSummaryModelEnv))
	if preferred == "" {
		preferred = windowSummaryDefaultModel
	}
	preferred = strings.TrimPrefix(preferred, "openrouter:")
	if preferred == "" {
		return providers
	}
	preferred = strings.ToLower(preferred)

	var prioritized []LLMProvider
	var rest []LLMProvider
	for _, p := range providers {
		if strings.Contains(strings.ToLower(p.Model), preferred) {
			prioritized = append(prioritized, p)
		} else {
			rest = append(rest, p)
		}
	}
	if len(prioritized) == 0 {
		return providers
	}
	return append(prioritized, rest...)
}

func buildWindowChunkGroups(windowIndex int, trigger string, candidates []windowChunkCandidate, maxTokens int) []windowChunkGroup {
	if len(candidates) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		maxTokens = windowChunkTokensMax
	}
	var groups []windowChunkGroup
	var current []windowChunkCandidate
	tokens := 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		groups = append(groups, windowChunkGroup{
			Candidates: current,
			Meta:       buildWindowChunkSummaryMeta(windowIndex, trigger, current),
			Content:    buildWindowChunkContent(current),
		})
		current = nil
		tokens = 0
	}

	for _, candidate := range candidates {
		if candidate.Preview == "" {
			continue
		}
		tokenCount := candidate.Tokens
		if tokenCount <= 0 {
			tokenCount = 1
		}
		if tokens+tokenCount > maxTokens && len(current) > 0 {
			flush()
		}
		current = append(current, candidate)
		tokens += tokenCount
	}
	flush()
	return groups
}

func buildWindowChunkSummaryMeta(windowIndex int, trigger string, candidates []windowChunkCandidate) windowChunkSummaryMeta {
	var indices []int
	toolsSeen := make(map[string]struct{})
	filesSeen := make(map[string]struct{})
	var errorSnippets []string

	for _, candidate := range candidates {
		indices = append(indices, candidate.Chunk.ChunkIndex)
		for _, tool := range candidate.Chunk.ToolsUsed {
			if tool != "" {
				toolsSeen[tool] = struct{}{}
			}
		}
		for _, file := range candidate.Chunk.FilesTouched {
			if file != "" {
				filesSeen[file] = struct{}{}
			}
		}
		if candidate.IsError {
			snippet := skillout.TruncateSingleLine(strings.TrimSpace(candidate.Preview), 200)
			if snippet != "" {
				errorSnippets = append(errorSnippets, snippet)
			}
		}
	}

	return windowChunkSummaryMeta{
		WindowIndex:  windowIndex,
		Trigger:      trigger,
		ChunkIndices: sortedUniqueInts(indices),
		Tools:        sortedSet(toolsSeen, 8),
		Files:        sortedSet(filesSeen, 12),
		Errors:       uniqueLimited(errorSnippets, 3),
	}
}

func buildWindowChunkContent(candidates []windowChunkCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Preview == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s #%d] %s", candidate.Chunk.ChunkType, candidate.Chunk.ChunkIndex, candidate.Preview))
	}
	return strings.Join(parts, "\n")
}

func summarizeWindowChunkGroups(ctx context.Context, providers []LLMProvider, groups []windowChunkGroup) []windowChunkSummary {
	if len(groups) == 0 {
		return nil
	}
	var summaries []windowChunkSummary
	for _, group := range groups {
		summary, model, err := summarizeWindowChunk(ctx, providers, group.Meta, group.Content)
		if err != nil {
			logger.Warn("chunk summary failed", obs.Int("window_index", group.Meta.WindowIndex), obs.Err(err))
			continue
		}
		summary = strings.TrimSpace(summary)
		if isPlaceholderSummary(summary) {
			continue
		}
		summaries = append(summaries, windowChunkSummary{
			Summary:    summary,
			Meta:       group.Meta,
			Model:      model,
			Candidates: group.Candidates,
		})
	}
	return summaries
}

func summarizeWindowChunk(ctx context.Context, providers []LLMProvider, meta windowChunkSummaryMeta, content string) (string, string, error) {
	metaBlock := formatWindowChunkMeta(meta)
	prompt := fmt.Sprintf(`Summarize this context window segment for retrieval.

Output format: 2-4 short lines using labels:
Focus: ...
Work: ...
Artifacts: ... (files, tools, configs, errors)
Issues: ... (only if mentioned)

Requirements:
- Include exact identifiers from the content (file paths, function names, config keys, error messages, tool names).
- Be compact but information-dense. No filler.
- Output only the labeled lines. No JSON, no markdown.

Chunk metadata:
%s

<content>
%s
</content>`, metaBlock, content)

	for _, p := range providers {
		var summary string
		var err error

		if p.IsCLI {
			summary, err = callCLIForWindowSummary(ctx, p, prompt)
		} else {
			summary, err = callAPIForWindowSummary(ctx, p, prompt, windowChunkSummaryMaxTokens)
		}

		if err == nil && summary != "" {
			return strings.TrimSpace(summary), p.Model, nil
		}

		if err != nil && !isRetryableError(err) {
			return "", p.Model, err
		}
	}

	return "", "", skillerr.Runtime("all providers failed")
}

func summarizeWindowFromSummaries(ctx context.Context, providers []LLMProvider, windowIndex int, summaries []windowChunkSummary, meta windowSummaryMeta, compactSummary string) (string, error) {
	if len(summaries) == 0 {
		return "", nil
	}
	summariesBlock := condenseChunkSummaries(summaries, windowChunkSummariesMaxChars)
	if summariesBlock == "" {
		return "", nil
	}
	metaBlock := formatWindowMeta(meta)
	summaryHint := strings.TrimSpace(compactSummary)
	summaryBlock := "None."
	if summaryHint != "" {
		summaryBlock = skillout.TruncateSingleLine(summaryHint, windowSummaryHintMaxChars)
	}
	prompt := fmt.Sprintf(`Summarize this coding session context window (window #%d) for retrieval + embedding.

Output format: 4-6 short lines using labels:
Goal: ...
Work: ...
Decisions/Issues (include gotchas + fixes when present): ...
Artifacts: ... (files, tools, commands, configs, errors)
Next: ... (only if mentioned)
Preferences: ... (only if mentioned)
Tags: ... (1-3, only if mentioned)

Requirements:
- Include exact identifiers from the chunk summaries and metadata.
- If a compact summary is provided, use it as a hint and reconcile with the summaries.
- Be compact but information-dense. No filler.
- Output only the labeled lines. No JSON, no markdown.

Compact summary (Claude Code, if present):
%s

Window metadata:
%s

Chunk summaries:
%s`, windowIndex, summaryBlock, metaBlock, summariesBlock)

	for _, p := range providers {
		var summary string
		var err error

		if p.IsCLI {
			summary, err = callCLIForWindowSummary(ctx, p, prompt)
		} else {
			summary, err = callAPIForWindowSummary(ctx, p, prompt, windowSummaryMaxTokens)
		}

		if err == nil && summary != "" {
			return strings.TrimSpace(summary), nil
		}

		if err != nil && !isRetryableError(err) {
			return "", err
		}
	}

	return "", skillerr.Runtime("all providers failed")
}

func condenseChunkSummaries(summaries []windowChunkSummary, maxChars int) string {
	if len(summaries) == 0 {
		return ""
	}
	var b strings.Builder
	appendLine := func(line string) bool {
		if line == "" {
			return true
		}
		if maxChars > 0 && b.Len()+len(line)+1 > maxChars {
			return false
		}
		b.WriteString(line)
		b.WriteString("\n")
		return true
	}
	for _, summary := range summaries {
		chunkLabel := formatChunkIndices(summary.Meta.ChunkIndices, windowChunkIndicesDisplayMax)
		text := skillout.TruncateSingleLine(summary.Summary, windowChunkSummaryLineMaxChars)
		if !appendLine(fmt.Sprintf("Chunks %s: %s", chunkLabel, text)) {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func chunkEmbeddingsMissing(expectedModel string, candidates []windowChunkCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if len(candidate.Chunk.Embedding) == 0 {
			return true
		}
		if expectedModel != "" && candidate.Chunk.EmbeddingModel != expectedModel {
			return true
		}
	}
	return false
}

func embedChunkSummaries(ctx context.Context, sessionStore *sessions.Store, embedder *semantic.Embedder, embedderErr error, summaries []windowChunkSummary, force bool) {
	if embedderErr != nil || embedder == nil || len(summaries) == 0 {
		return
	}

	for _, summary := range summaries {
		embeddingText := buildChunkEmbeddingText(summary.Summary, summary.Meta)
		if embeddingText == "" {
			continue
		}
		result, err := embedder.Embed(ctx, embeddingText)
		if err != nil || len(result.Vec) == 0 {
			continue
		}
		embeddingBytes := vector.SerializeF32(result.Vec)

		for _, candidate := range summary.Candidates {
			chunk := candidate.Chunk
			if !force && len(chunk.Embedding) > 0 && chunk.EmbeddingModel == result.Model {
				continue
			}
			chunk.Embedding = embeddingBytes
			chunk.EmbeddingModel = result.Model
			if _, err := sessionStore.SaveChunk(ctx, chunk); err != nil {
				logger.Warn("failed to update chunk embedding", obs.Int("chunk_index", chunk.ChunkIndex), obs.Err(err))
			}
		}
	}
}

func chunkCandidateMap(candidates []windowChunkCandidate) map[int]windowChunkCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make(map[int]windowChunkCandidate, len(candidates))
	for _, candidate := range candidates {
		out[candidate.Chunk.ChunkIndex] = candidate
	}
	return out
}

func chunkSummaryIndexSet(summaries []windowChunkSummary) map[int]struct{} {
	if len(summaries) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	for _, summary := range summaries {
		for _, idx := range summary.Meta.ChunkIndices {
			seen[idx] = struct{}{}
		}
	}
	return seen
}

func missingChunkSummaryCandidates(candidates []windowChunkCandidate, summaries []windowChunkSummary) []windowChunkCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(summaries) == 0 {
		return candidates
	}
	covered := chunkSummaryIndexSet(summaries)
	var missing []windowChunkCandidate
	for _, candidate := range candidates {
		if _, ok := covered[candidate.Chunk.ChunkIndex]; ok {
			continue
		}
		missing = append(missing, candidate)
	}
	return missing
}

func windowChunkSummariesFromRecords(records []sessions.SessionChunkSummary, candidates map[int]windowChunkCandidate) []windowChunkSummary {
	if len(records) == 0 {
		return nil
	}
	out := make([]windowChunkSummary, 0, len(records))
	for _, record := range records {
		if isPlaceholderSummary(record.Summary) {
			continue
		}
		meta := windowChunkSummaryMeta{
			WindowIndex:  record.WindowIndex,
			Trigger:      record.Trigger,
			ChunkIndices: sortedUniqueInts(record.ChunkIndices),
			Tools:        record.Tools,
			Files:        record.Files,
			Errors:       record.Errors,
		}
		summary := windowChunkSummary{
			Meta:    meta,
			Summary: record.Summary,
			Model:   record.SummaryModel,
		}
		for _, idx := range meta.ChunkIndices {
			if candidate, ok := candidates[idx]; ok {
				summary.Candidates = append(summary.Candidates, candidate)
			}
		}
		out = append(out, summary)
	}
	return out
}

func buildChunkSummaryRecords(sessionID string, summaries []windowChunkSummary) []sessions.SessionChunkSummary {
	if len(summaries) == 0 {
		return nil
	}
	out := make([]sessions.SessionChunkSummary, 0, len(summaries))
	for _, summary := range summaries {
		if isPlaceholderSummary(summary.Summary) {
			continue
		}
		meta := summary.Meta
		indices := sortedUniqueInts(meta.ChunkIndices)
		chunkMin := 0
		chunkMax := 0
		if len(indices) > 0 {
			chunkMin = indices[0]
			chunkMax = indices[len(indices)-1]
		}
		out = append(out, sessions.SessionChunkSummary{
			ID:            chunkSummaryRecordID(sessionID, meta),
			SessionID:     sessionID,
			WindowIndex:   meta.WindowIndex,
			Trigger:       meta.Trigger,
			ChunkIndices:  indices,
			ChunkIndexMin: chunkMin,
			ChunkIndexMax: chunkMax,
			Tools:         meta.Tools,
			Files:         meta.Files,
			Errors:        meta.Errors,
			Summary:       summary.Summary,
			SummaryModel:  summary.Model,
		})
	}
	return out
}

func chunkSummaryRecordID(sessionID string, meta windowChunkSummaryMeta) string {
	key := fmt.Sprintf("%s:%d:%s", sessionID, meta.WindowIndex, formatChunkIndices(meta.ChunkIndices, 0))
	if meta.Trigger != "" {
		key = fmt.Sprintf("%s:%s", key, meta.Trigger)
	}
	return "chunk_summary_" + hashutil.ShortHash(key)
}

func processBatch(ctx context.Context, sessionStore *sessions.Store, sessionID string, batch []windowTask, providers []LLMProvider, chunkProviders []LLMProvider, embedder *semantic.Embedder, embedderErr error, force bool) (summarized, embedded, skipped int) {
	contentBudget := windowContentBudget(providers)
	chunkSummaryBudget := windowChunkSummaryBudget(chunkProviders)
	expectedEmbedModel := ""
	if embedder != nil {
		expectedEmbedModel = embedder.Model()
	}
	for _, task := range batch {
		window := task.Window
		// Build content from chunks
		var contentParts []string
		var candidates []windowChunkCandidate
		toolsSeen := make(map[string]struct{})
		filesSeen := make(map[string]struct{})
		var errorSnippets []string

		for chunkIdx := window.ChunkStart; chunkIdx <= window.ChunkEnd; chunkIdx++ {
			chunk, err := sessionStore.GetChunk(ctx, sessionID, chunkIdx)
			if err != nil {
				continue
			}
			for _, tool := range chunk.ToolsUsed {
				if tool != "" {
					toolsSeen[tool] = struct{}{}
				}
			}
			for _, file := range chunk.FilesTouched {
				if file != "" {
					filesSeen[file] = struct{}{}
				}
			}
			isError := chunk.HasError || chunk.ChunkType == "error"
			if isError {
				snippet := skillout.TruncateSingleLine(strings.TrimSpace(chunk.ContentPreview), 200)
				if snippet == "" && chunk.ErrorType != "" {
					snippet = chunk.ErrorType
				}
				if snippet != "" {
					errorSnippets = append(errorSnippets, snippet)
				}
			}
			preview := strings.TrimSpace(chunk.ContentPreview)
			if preview == "" {
				continue
			}
			chunkTokens := len(preview) / 4
			if chunkTokens == 0 {
				chunkTokens = 1
			}
			candidates = append(candidates, windowChunkCandidate{
				Chunk:    chunk,
				Preview:  preview,
				Tokens:   chunkTokens,
				HasTools: len(chunk.ToolsUsed) > 0,
				HasFiles: len(chunk.FilesTouched) > 0,
				IsError:  isError,
			})
		}

		selected := selectWindowChunks(candidates, contentBudget)
		summaryCandidates := selected
		if chunkSummaryBudget > contentBudget {
			summaryCandidates = selectWindowChunks(candidates, chunkSummaryBudget)
		}
		for _, candidate := range selected {
			contentParts = append(contentParts, fmt.Sprintf("[%s #%d] %s", candidate.Chunk.ChunkType, candidate.Chunk.ChunkIndex, candidate.Preview))
		}

		meta := windowSummaryMeta{
			WindowIndex: window.WindowIndex,
			Trigger:     window.Trigger,
			Tools:       sortedSet(toolsSeen, 8),
			Files:       sortedSet(filesSeen, 12),
			Errors:      uniqueLimited(errorSnippets, 3),
		}
		summaryHint := strings.TrimSpace(window.Summary)

		needsChunkSummaries := task.Summarize || force
		if !needsChunkSummaries && embedderErr == nil && embedder != nil {
			needsChunkSummaries = chunkEmbeddingsMissing(expectedEmbedModel, summaryCandidates)
		}
		var chunkSummaries []windowChunkSummary
		candidateMap := chunkCandidateMap(candidates)
		if !force {
			storedSummaries, err := sessionStore.GetChunkSummaries(ctx, sessionID, window.WindowIndex)
			if err == nil && len(storedSummaries) > 0 {
				chunkSummaries = windowChunkSummariesFromRecords(storedSummaries, candidateMap)
			}
		}

		missingCandidates := missingChunkSummaryCandidates(summaryCandidates, chunkSummaries)
		if needsChunkSummaries && len(missingCandidates) > 0 {
			missingGroups := buildWindowChunkGroups(window.WindowIndex, window.Trigger, missingCandidates, windowChunkTokensMax)
			newSummaries := summarizeWindowChunkGroups(ctx, chunkProviders, missingGroups)
			if len(newSummaries) > 0 {
				if err := sessionStore.SaveChunkSummaries(ctx, buildChunkSummaryRecords(sessionID, newSummaries)); err != nil {
					logger.Warn("failed to persist chunk summaries", obs.Int("window_index", window.WindowIndex), obs.Err(err))
				}
				chunkSummaries = append(chunkSummaries, newSummaries...)
			}
		}

		if len(chunkSummaries) > 0 {
			embedChunkSummaries(ctx, sessionStore, embedder, embedderErr, chunkSummaries, force)
		}

		if task.Summarize {
			if len(contentParts) == 0 && summaryHint == "" && len(chunkSummaries) == 0 {
				skipped++
				continue
			}
			summary, err := summarizeWindowFromSummaries(ctx, providers, window.WindowIndex, chunkSummaries, meta, summaryHint)
			if err != nil {
				logger.Warn("LLM failed for window", obs.Int("window_index", window.WindowIndex), obs.Err(err))
			}
			if isPlaceholderSummary(summary) {
				summary = ""
			}
			if summary == "" && len(contentParts) > 0 {
				summary, err = summarizeWindowContent(ctx, providers, window.WindowIndex, strings.Join(contentParts, "\n"), meta, summaryHint)
				if err != nil {
					logger.Warn("LLM failed for window content", obs.Int("window_index", window.WindowIndex), obs.Err(err))
				}
			}
			summary = strings.TrimSpace(summary)
			if isPlaceholderSummary(summary) {
				skipped++
				continue
			}
			if err := sessionStore.UpdateContextWindowSummary(ctx, window.ID, summary); err != nil {
				skipped++
				continue
			}
			summarized++
			summaryHint = summary
		} else if summaryHint == "" {
			skipped++
			continue
		}

		// Generate and save embedding
		if embedderErr == nil && embedder != nil && summaryHint != "" {
			embeddingText := buildWindowEmbeddingText(summaryHint, meta)
			if result, err := embedder.Embed(ctx, embeddingText); err == nil && len(result.Vec) > 0 {
				embeddingBytes := vector.SerializeF32(result.Vec)
				if err := sessionStore.SetContextWindowEmbedding(ctx, window.ID, embeddingBytes, result.Model); err != nil {
					logger.Warn("failed to set window embedding", obs.Str("window_id", window.ID), obs.Err(err))
				} else {
					embedded++
				}
			}
		}
	}
	return
}

// summarizeWindowContent calls LLM to generate a concise summary for a window.
func summarizeWindowContent(ctx context.Context, providers []LLMProvider, windowIndex int, content string, meta windowSummaryMeta, compactSummary string) (string, error) {
	metaBlock := formatWindowMeta(meta)
	summaryHint := strings.TrimSpace(compactSummary)
	summaryBlock := "None."
	if summaryHint != "" {
		summaryBlock = skillout.TruncateSingleLine(summaryHint, windowSummaryHintMaxChars)
	}
	prompt := fmt.Sprintf(`Summarize this coding session context window (window #%d) for retrieval + embedding.

Output format: 4-6 short lines using labels:
Goal: ...
Work: ...
Decisions/Issues (include gotchas + fixes when present): ...
Artifacts: ... (files, tools, commands, configs, errors)
Next: ... (only if mentioned)
Preferences: ... (only if mentioned)
Tags: ... (1-3, only if mentioned)

Requirements:
- Include exact identifiers from the content (file paths, function names, config keys, error messages, tool names).
- If a compact summary is provided, use it as a hint and reconcile with the content.
- Be compact but information-dense. No filler.
- Output only the labeled lines. No JSON, no markdown.

Compact summary (Claude Code, if present):
%s

Window metadata:
%s

<content>
%s
</content>`, windowIndex, summaryBlock, metaBlock, content)

	// Try providers in order
	for _, p := range providers {
		var summary string
		var err error

		if p.IsCLI {
			summary, err = callCLIForWindowSummary(ctx, p, prompt)
		} else {
			summary, err = callAPIForWindowSummary(ctx, p, prompt, windowSummaryMaxTokens)
		}

		if err == nil && summary != "" {
			return strings.TrimSpace(summary), nil
		}

		// Skip retry check if no error (empty response case)
		if err != nil && !isRetryableError(err) {
			return "", err
		}
	}

	return "", skillerr.Runtime("all providers failed")
}

func formatWindowMeta(meta windowSummaryMeta) string {
	var lines []string
	if meta.Trigger != "" {
		lines = append(lines, fmt.Sprintf("Trigger: %s", meta.Trigger))
	}
	if len(meta.Tools) > 0 {
		lines = append(lines, fmt.Sprintf("Tools: %s", strings.Join(meta.Tools, ", ")))
	}
	if len(meta.Files) > 0 {
		lines = append(lines, fmt.Sprintf("Files: %s", strings.Join(meta.Files, ", ")))
	}
	if len(meta.Errors) > 0 {
		lines = append(lines, fmt.Sprintf("Errors: %s", strings.Join(meta.Errors, " | ")))
	}
	if len(lines) == 0 {
		return "None."
	}
	return strings.Join(lines, "\n")
}

func formatWindowChunkMeta(meta windowChunkSummaryMeta) string {
	var lines []string
	if meta.Trigger != "" {
		lines = append(lines, fmt.Sprintf("Trigger: %s", meta.Trigger))
	}
	if len(meta.ChunkIndices) > 0 {
		lines = append(lines, fmt.Sprintf("Chunks: %s", formatChunkIndices(meta.ChunkIndices, windowChunkIndicesDisplayMax)))
	}
	if len(meta.Tools) > 0 {
		lines = append(lines, fmt.Sprintf("Tools: %s", strings.Join(meta.Tools, ", ")))
	}
	if len(meta.Files) > 0 {
		lines = append(lines, fmt.Sprintf("Files: %s", strings.Join(meta.Files, ", ")))
	}
	if len(meta.Errors) > 0 {
		lines = append(lines, fmt.Sprintf("Errors: %s", strings.Join(meta.Errors, " | ")))
	}
	if len(lines) == 0 {
		return "None."
	}
	return strings.Join(lines, "\n")
}

func buildWindowEmbeddingText(summary string, meta windowSummaryMeta) string {
	var parts []string
	header := fmt.Sprintf("Context window %d", meta.WindowIndex)
	if meta.Trigger != "" {
		header = fmt.Sprintf("%s (trigger: %s)", header, meta.Trigger)
	}
	parts = append(parts, header)
	if summary != "" {
		parts = append(parts, summary)
	}
	if len(meta.Files) > 0 {
		parts = append(parts, fmt.Sprintf("Files: %s", strings.Join(meta.Files, ", ")))
	}
	if len(meta.Tools) > 0 {
		parts = append(parts, fmt.Sprintf("Tools: %s", strings.Join(meta.Tools, ", ")))
	}
	if len(meta.Errors) > 0 {
		parts = append(parts, fmt.Sprintf("Errors: %s", strings.Join(meta.Errors, " | ")))
	}
	result := strings.Join(parts, "\n")
	if len(result) > 6000 {
		result = result[:6000]
	}
	return result
}

func buildChunkEmbeddingText(summary string, meta windowChunkSummaryMeta) string {
	if summary == "" {
		return ""
	}
	chunkLabel := formatChunkIndices(meta.ChunkIndices, windowChunkIndicesDisplayMax)
	header := fmt.Sprintf("Context window %d chunks %s", meta.WindowIndex, chunkLabel)
	if meta.Trigger != "" {
		header = fmt.Sprintf("%s (trigger: %s)", header, meta.Trigger)
	}
	parts := []string{header, summary}
	if len(meta.Files) > 0 {
		parts = append(parts, fmt.Sprintf("Files: %s", strings.Join(meta.Files, ", ")))
	}
	if len(meta.Tools) > 0 {
		parts = append(parts, fmt.Sprintf("Tools: %s", strings.Join(meta.Tools, ", ")))
	}
	if len(meta.Errors) > 0 {
		parts = append(parts, fmt.Sprintf("Errors: %s", strings.Join(meta.Errors, " | ")))
	}
	result := strings.Join(parts, "\n")
	if len(result) > windowChunkEmbeddingMaxChars {
		result = result[:windowChunkEmbeddingMaxChars]
	}
	return result
}

func sortedUniqueInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	out := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func formatChunkIndices(indices []int, limit int) string {
	if len(indices) == 0 {
		return "none"
	}
	total := len(indices)
	if limit > 0 && total > limit {
		indices = indices[:limit]
	}
	parts := make([]string, 0, len(indices))
	for _, idx := range indices {
		parts = append(parts, strconv.Itoa(idx))
	}
	result := strings.Join(parts, ", ")
	if limit > 0 && total > limit {
		result = fmt.Sprintf("%s (+%d more)", result, total-limit)
	}
	return result
}

func sortedSet(values map[string]struct{}, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func uniqueLimited(values []string, limit int) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// callAPIForWindowSummary calls an OpenAI-compatible API for a simple text response.
func callAPIForWindowSummary(ctx context.Context, provider LLMProvider, prompt string, maxTokens int) (string, error) {
	if maxTokens <= 0 {
		maxTokens = windowSummaryMaxTokens
	}
	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", skillerr.WrapRuntime("marshal request", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", skillerr.WrapRuntime("create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	if strings.HasPrefix(provider.Name, "openrouter:") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
		req.Header.Set("X-Title", "agentctl")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", skillerr.Runtimef("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", skillerr.WrapParse("decode response", err)
	}

	if len(result.Choices) == 0 {
		return "", skillerr.Runtime("empty response")
	}

	return result.Choices[0].Message.Content, nil
}

// callCLIForWindowSummary calls a CLI tool for a simple text response.
func callCLIForWindowSummary(ctx context.Context, provider LLMProvider, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var cmdName string
	var args []string
	switch provider.Name {
	case "gemini-cli":
		cmdName = "gemini"
		args = []string{"-m", provider.Model, "-p", prompt}
	case "claude-cli":
		cmdName = "claude"
		args = []string{"-p", prompt, "--model", provider.Model, "--output-format", "text"}
	default:
		return "", skillerr.Runtimef("unknown CLI provider: %s", provider.Name)
	}

	result := executil.Run(ctx, "", cmdName, args...)
	if result.Err != nil {
		return "", skillerr.Runtimef("CLI error: %v (stderr: %s)", result.Err, string(result.Stderr))
	}

	return string(result.Stdout), nil
}

// buildEmbeddingText creates text to embed from the summary response.
// Format: [Jan 2, 2026] [activity] Summary\nAccomplished: ...\nFiles: ...\nTopics: ...
func buildEmbeddingText(summary *SummaryResponse, startedAt time.Time, keyFiles []string) string {
	var parts []string

	// Date and activity type prefix
	dateStr := startedAt.Format("Jan 2, 2006")
	activity := inferActivityType(summary.Tags)
	parts = append(parts, fmt.Sprintf("[%s] [%s]", dateStr, activity))

	if summary.Summary != "" {
		parts = append(parts, summary.Summary)
	}

	if len(summary.Accomplished) > 0 {
		parts = append(parts, "Accomplished: "+strings.Join(summary.Accomplished, "; "))
	}

	if len(summary.Decisions) > 0 {
		parts = append(parts, "Decisions: "+strings.Join(summary.Decisions, "; "))
	}

	if len(summary.Gotchas) > 0 {
		parts = append(parts, "Gotchas: "+strings.Join(summary.Gotchas, "; "))
	}

	if len(summary.UserInsights) > 0 {
		parts = append(parts, "User feedback: "+strings.Join(summary.UserInsights, "; "))
	}

	if len(summary.UserPreferences) > 0 {
		parts = append(parts, "User preferences: "+strings.Join(summary.UserPreferences, "; "))
	}

	if len(summary.TimeSinks) > 0 {
		parts = append(parts, "Time sinks: "+strings.Join(summary.TimeSinks, "; "))
	}

	if len(keyFiles) > 0 {
		parts = append(parts, "Files: "+strings.Join(keyFiles, ", "))
	}

	if len(summary.Tags) > 0 {
		parts = append(parts, "Topics: "+strings.Join(summary.Tags, ", "))
	}

	return strings.Join(parts, "\n")
}
