// Package main implements the session/summarize skill for generating structured summaries.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/vector"
)

// Input defines the skill input parameters.
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
}

// Output defines the skill output.
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
	// WindowsSkipped is the count of windows that already had summaries (mode=windows).
	WindowsSkipped int `json:"windows_skipped,omitempty"`

	// SessionsReembedded is the count of sessions re-embedded (mode=reembed).
	SessionsReembedded int `json:"sessions_reembedded,omitempty"`
	// SessionsSkipped is the count of sessions skipped (already correct, mode=reembed).
	SessionsSkipped int `json:"sessions_skipped,omitempty"`
	// WindowsReembedded is the count of context windows re-embedded (mode=reembed).
	WindowsReembedded int `json:"windows_reembedded,omitempty"`

	Status  string `json:"status"`
	Message string `json:"message"`
}

// SummaryResponse is the expected JSON response from Cerebras.
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

// ClaudeMessage represents a message from Claude Code's JSONL format.
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

// MessageContent represents the content of a message.
type MessageContent struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Model   string          `json:"model,omitempty"`
}

// ContentBlock represents a block in assistant message content.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"` // Note: NOT used for summarization (can be huge)
	ToolUseID string          `json:"id,omitempty"`
}

// UserContentBlock represents a block in user message content (text or tool_result).
type UserContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"` // tool result content (often large)
}

// FilteredMessage is a high-signal message for summarization.
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
type LLMProvider struct {
	Name      string
	Endpoint  string // empty for CLI providers
	APIKey    string // empty for CLI providers
	Model     string
	IsCLI     bool // true for CLI tools like gemini/claude
	MaxTokens int  // max input tokens for this provider
}

// getProviders returns available LLM providers in priority order.
// Priority: OpenRouter (devstral free) → Groq → Cerebras → CLI (slow startup)
func getProviders() []LLMProvider {
	var providers []LLMProvider

	// OpenRouter - devstral is the preferred free model
	// Set OPENROUTER_MODELS as comma-separated list, or use defaults
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		models := os.Getenv("OPENROUTER_MODELS")
		if models == "" {
			// Default: devstral (free, fast, good at code)
			models = "minimax/minimax-m2.1"
		}
		for _, model := range strings.Split(models, ",") {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			providers = append(providers, LLMProvider{
				Name:      "openrouter:" + model,
				Endpoint:  "https://openrouter.ai/api/v1/chat/completions",
				APIKey:    key,
				Model:     model,
				MaxTokens: 24000, // 32k context minus output buffer
			})
		}
	}

	// Groq - fast and cheap with 128k context
	// Free tier: 12k TPM limit, so use 10k to be safe
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		model := os.Getenv("GROQ_MODEL")
		if model == "" {
			model = "llama-3.3-70b-versatile" // 128k context, $0.59/$0.79 per M
		}
		providers = append(providers, LLMProvider{
			Name:      "groq",
			Endpoint:  "https://api.groq.com/openai/v1/chat/completions",
			APIKey:    key,
			Model:     model,
			MaxTokens: 10000, // free tier TPM limit
		})
	}

	// Cerebras - fastest inference but rate limits
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" {
		model := os.Getenv("CEREBRAS_MODEL")
		if model == "" {
			model = "llama-3.3-70b"
		}
		providers = append(providers, LLMProvider{
			Name:      "cerebras",
			Endpoint:  "https://api.cerebras.ai/v1/chat/completions",
			APIKey:    key,
			Model:     model,
			MaxTokens: 8000, // conservative for rate limits
		})
	}

	// CLI providers as fallback (slow startup ~60s)
	// Gemini CLI - 1M context
	if _, err := exec.LookPath("gemini"); err == nil {
		model := os.Getenv("GEMINI_MODEL")
		if model == "" {
			model = "gemini-2.5-flash" // 1M context
		}
		providers = append(providers, LLMProvider{
			Name:      "gemini-cli",
			Model:     model,
			IsCLI:     true,
			MaxTokens: 100000, // can handle much more
		})
	}

	// Claude CLI - 200k context
	if _, err := exec.LookPath("claude"); err == nil {
		model := os.Getenv("CLAUDE_MODEL")
		if model == "" {
			model = "claude-haiku-4-5"
		}
		providers = append(providers, LLMProvider{
			Name:      "claude-cli",
			Model:     model,
			IsCLI:     true,
			MaxTokens: 50000, // conservative for 200k context
		})
	}

	return providers
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if in.MaxTokens <= 0 {
		in.MaxTokens = defaultMaxTokens
	}

	// Get available LLM providers
	providers := getProviders()
	if len(providers) == 0 {
		return skillerr.Arg("no LLM provider configured (set GROQ_API_KEY, CEREBRAS_API_KEY, or OPENROUTER_API_KEY)")
	}

	// Get paths from sessionkit
	paths := sessionkit.ResolvePaths(rc.Config)

	// Parse mode early - some modes don't need session_id
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "summary"
	}
	if mode != "summary" && mode != "seed" && mode != "windows" && mode != "reembed" {
		return skillerr.Arg(fmt.Sprintf("invalid mode %q (expected summary, windows, seed, or reembed)", in.Mode))
	}

	// Open sessions store
	sessionStore, cleanup, err := sessionkit.OpenSessions(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer cleanup()

	// Handle reembed mode early - doesn't need a specific session
	if mode == "reembed" {
		output := reembedAll(ctx, sessionStore, in)
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
		output := summarizeWindows(ctx, sessionStore, session, providers, in)
		return skillout.Emit(rc, command, output)
	}

	needsSummarize := mode == "summary" && (in.Force || strings.TrimSpace(session.Summary) == "")

	// Default behavior: return existing summary quickly.
	if mode == "summary" && !needsSummarize {
		output := Output{
			SessionID:    session.ID,
			Summary:      session.Summary,
			Accomplished: ensureSlice(session.Accomplished),
			Decisions:    ensureSlice(session.Decisions),
			Gotchas:      ensureSlice(session.Gotchas),
			Tags:         ensureSlice(session.Tags),
			KeyFiles:     ensureSlice(session.KeyFiles),
			ToolsPattern: session.ToolsPattern,
			Status:       "exists",
			Message:      fmt.Sprintf("Session %s already summarized (use force=true to re-summarize)", session.ID),
		}
		return skillout.Emit(rc, command, output)
	}

	var (
		summaryResp        *SummaryResponse
		usedProvider       string
		persistedLearnings int
		persistErr         error
	)

	if needsSummarize {
		// Call LLM for summarization (with fallback, filtering per-provider)
		var err error
		summaryResp, usedProvider, err = summarizeWithFallback(ctx, providers, session.RawJSONLPath, in.MaxTokens)
		if err != nil {
			return skillerr.Runtime(fmt.Sprintf("summarization failed (tried %d providers)", len(providers)), skillerr.WithCause(err))
		}
		_ = usedProvider // TODO: could log which provider was used

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

		persistedLearnings, persistErr = persistSessionLearnings(ctx, paths.AgentctlHome, session, summaryResp)
	} else {
		// Use persisted session fields.
		summaryResp = &SummaryResponse{
			Summary:         session.Summary,
			Accomplished:    ensureSlice(session.Accomplished),
			Decisions:       ensureSlice(session.Decisions),
			Gotchas:         ensureSlice(session.Gotchas),
			UserInsights:    ensureSlice(session.UserInsights),
			UserPreferences: []string{},
			TimeSinks:       []string{},
			Tags:            ensureSlice(session.Tags),
			KeyFiles:        ensureSlice(session.KeyFiles),
			ToolsPattern:    session.ToolsPattern,
			KeyQuestions:    ensureSlice(session.KeyQuestions),
		}
	}

	status := "summarized"
	message := fmt.Sprintf("Summarized session %s: %s", session.ID, truncate(summaryResp.Summary, 100))
	if !needsSummarize {
		status = "exists"
		message = fmt.Sprintf("Loaded existing summary for session %s", session.ID)
	}

	output := Output{
		SessionID:       session.ID,
		Summary:         summaryResp.Summary,
		Accomplished:    ensureSlice(summaryResp.Accomplished),
		Decisions:       ensureSlice(summaryResp.Decisions),
		Gotchas:         ensureSlice(summaryResp.Gotchas),
		UserInsights:    ensureSlice(summaryResp.UserInsights),
		UserPreferences: ensureSlice(summaryResp.UserPreferences),
		TimeSinks:       ensureSlice(summaryResp.TimeSinks),
		KeyQuestions:    ensureSlice(summaryResp.KeyQuestions),
		Tags:            ensureSlice(summaryResp.Tags),
		KeyFiles:        ensureSlice(summaryResp.KeyFiles),
		ToolsPattern:    summaryResp.ToolsPattern,
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
		seedPrompt, err := buildSeedPrompt(ctx, sessionStore, session, summaryResp, in)
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
		embeddingText := buildEmbeddingText(summaryResp)
		var embeddingResult semantic.EmbedResult
		var embeddingErr error

		embedder, err := semantic.NewEmbedder(semantic.ScopeSessions, semantic.WithAllowFallback(true))
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
					TableName:  "sessions",
					ColumnName: "embedding",
					Provider:   embeddingResult.Provider,
					Model:      embeddingResult.Model,
					Dimensions: embeddingResult.Dims,
				}
				if err := sessionStore.SetEmbeddingMetadata(ctx, meta); err != nil {
					log.Printf("[WARN] session_summarize: failed to set embedding metadata: %v", err)
				}
			}
		}
	}

	return skillout.Emit(rc, command, output)
}

func persistSessionLearnings(ctx context.Context, agentctlHome string, session sessions.Session, resp *SummaryResponse) (int, error) {
	workspace := session.WorkspacePath
	if strings.TrimSpace(workspace) == "" {
		return 0, fmt.Errorf("missing session workspace_path")
	}

	storageRoot := filepath.Join(agentctlHome, "storage")
	casRoot := filepath.Join(agentctlHome, "cas")
	store, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer func() { _ = store.Close() }()

	count := 0
	if n, err := persistLearnings(ctx, store, session.ID, workspace, "gotcha", resp.Gotchas); err != nil {
		return count, err
	} else {
		count += n
	}
	if n, err := persistLearnings(ctx, store, session.ID, workspace, "decision", resp.Decisions); err != nil {
		return count, err
	} else {
		count += n
	}
	if n, err := persistLearnings(ctx, store, session.ID, workspace, "user_pref", resp.UserPreferences); err != nil {
		return count, err
	} else {
		count += n
	}
	if n, err := persistLearnings(ctx, store, session.ID, workspace, "time_sink", resp.TimeSinks); err != nil {
		return count, err
	} else {
		count += n
	}

	return count, nil
}

func persistLearnings(ctx context.Context, store *memory.Store, sessionID, workspace, typ string, items []string) (int, error) {
	count := 0
	for _, raw := range items {
		text := normalizeLearning(raw)
		if text == "" {
			continue
		}
		digest := shortHash(text)
		name := fmt.Sprintf("session:%s:%s:%s", sessionID, typ, digest)

		payload, err := json.Marshal(map[string]any{
			"session_id": sessionID,
			"type":       typ,
			"text":       text,
		})
		if err != nil {
			return count, fmt.Errorf("marshal %s: %w", typ, err)
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
			return count, fmt.Errorf("save %s: %w", typ, err)
		}
		count++
	}
	return count, nil
}

func normalizeLearning(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)[:16]
}

// filterJSONL reads the raw JSONL and extracts high-signal content.
// Aggressively filters to reduce 35MB+ session files to a few hundred KB.
func filterJSONL(ctx context.Context, path string, maxTokens int) ([]FilteredMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var filtered []FilteredMessage
	scanner := bufio.NewScanner(file)

	// Increase buffer size for large lines (still need to read them to skip)
	const maxCapacity = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	estimatedTokens := 0
	tokensPerChar := 0.25 // Rough estimate: 4 chars per token

	// Pre-filter patterns for quick rejection
	const maxLineSize = 50 * 1024 // Skip lines >50KB (likely tool_result or huge tool_use)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
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

		// OPTIMIZATION 2: Quick type check before full JSON parse
		// Skip known noise types without parsing
		lineStr := string(line)
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
		return nil, fmt.Errorf("scan error: %w", err)
	}

	return filtered, nil
}

// filterMessage extracts high-signal content from a message.
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
				Content: truncate(content, 1000), // Increased limit for user requests
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
				textParts = append(textParts, truncate(block.Text, 1000))
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
					textParts = append(textParts, truncate(block.Text, 300))
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
func summarizeWithFallback(ctx context.Context, providers []LLMProvider, jsonlPath string, userMaxTokens int) (*SummaryResponse, string, error) {
	var lastErr error
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
		filtered, err := filterJSONL(ctx, jsonlPath, maxTokens)
		if err != nil {
			lastErr = fmt.Errorf("%s: filter error: %w", p.Name, err)
			continue
		}

		var resp *SummaryResponse
		if p.IsCLI {
			resp, err = summarizeWithCLI(ctx, p, filtered)
		} else {
			resp, err = summarizeWithProvider(ctx, p, filtered)
		}

		if err == nil {
			return resp, p.Name, nil
		}
		lastErr = fmt.Errorf("%s: %w", p.Name, err)
		// Continue to next provider on rate limit or server errors
		if !isRetryableError(err) {
			return nil, "", lastErr
		}
	}
	return nil, "", lastErr
}

// summarizeWithCLI calls a local CLI tool (gemini or claude) to generate a summary.
func summarizeWithCLI(ctx context.Context, provider LLMProvider, filtered []FilteredMessage) (*SummaryResponse, error) {
	// Build the conversation content (compact JSON)
	filteredJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, fmt.Errorf("marshal filtered: %w", err)
	}

	prompt := buildSummarizationPrompt(string(filteredJSON))

	// Enforce an upper bound for CLI calls to avoid hanging processes.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	switch provider.Name {
	case "gemini-cli":
		// gemini -m <model> -p "<prompt>"
		cmd = exec.CommandContext(ctx, "gemini", "-m", provider.Model, "-p", prompt)
	case "claude-cli":
		// claude -p "<prompt>" --model <model> --output-format text
		cmd = exec.CommandContext(ctx, "claude", "-p", prompt, "--model", provider.Model, "--output-format", "text")
	default:
		return nil, fmt.Errorf("unknown CLI provider: %s", provider.Name)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("CLI error: %w (stderr: %s)", err, stderr.String())
	}

	return parseSummaryResponse(stdout.String())
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
func summarizeWithProvider(ctx context.Context, provider LLMProvider, filtered []FilteredMessage) (*SummaryResponse, error) {
	// Build the conversation content (compact JSON to save tokens)
	filteredJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, fmt.Errorf("marshal filtered: %w", err)
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
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
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
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response from %s", provider.Name)
	}

	// Parse the JSON response
	return parseSummaryResponse(result.Choices[0].Message.Content)
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
		return nil, fmt.Errorf("no JSON object found in response")
	}
	response = response[start : end+1]

	// Clean up common LLM JSON issues
	response = cleanJSONResponse(response)

	var summary SummaryResponse
	if err := json.Unmarshal([]byte(response), &summary); err != nil {
		return nil, fmt.Errorf("parse summary JSON: %w (response: %s)", err, truncate(response, 200))
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

// ensureSlice returns an empty slice if s is nil, otherwise returns s.
// This ensures proper JSON serialization ([] instead of null).
func ensureSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

type scoredSeedWindow struct {
	Window     sessions.ContextWindow
	Similarity float64
}

func buildSeedPrompt(ctx context.Context, sessionStore *sessions.Store, session sessions.Session, summary *SummaryResponse, in Input) (string, error) {
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
		return "", fmt.Errorf("get context windows: %w", err)
	}

	var latest *sessions.ContextWindow
	if len(windows) > 0 {
		latest = &windows[len(windows)-1]
	}

	var queryEmbedding []float32
	if query != "" {
		if emb, _, err := embedSeedQuery(ctx, query); err == nil {
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
				if !appendLine(truncate(window.Summary, 800)) {
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
				line := fmt.Sprintf("- [%s #%d] %s", chunk.ChunkType, chunk.ChunkIndex, truncate(chunk.ContentPreview, 240))
				if !appendLine(line) {
					break
				}
			}
			appendLine("")
		}
	}

	// Fallback: if we couldn't include any windows/chunks, include recent filtered messages.
	if len(selected) == 0 && session.RawJSONLPath != "" {
		filtered, err := filterJSONL(ctx, session.RawJSONLPath, 800)
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
				text := truncate(fm.Content, 240)
				if !appendLine(fmt.Sprintf("- [%s] %s", role, text)) {
					break
				}
			}
		}
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("seed prompt empty")
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

func embedSeedQuery(ctx context.Context, text string) ([]float32, string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, "", nil
	}

	// Use Embedder with Gemini fallback for query embedding
	embedder, err := semantic.NewEmbedder(semantic.ScopeSessions, semantic.WithAllowFallback(true))
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
func reembedAll(ctx context.Context, sessionStore *sessions.Store, input Input) Output {
	output := Output{
		Status: "reembed_complete",
	}

	// Create embedder for sessions scope
	embedder, err := semantic.NewEmbedder(semantic.ScopeSessions)
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
			log.Printf("warning: failed to embed session %s: %v", sess.ID, err)
			continue
		}

		// Serialize and update session with new embedding
		embeddingBytes := vector.SerializeF32(result.Vec)
		if err := sessionStore.SetEmbedding(ctx, sess.ID, embeddingBytes, expectedModel); err != nil {
			log.Printf("warning: failed to update session %s embedding: %v", sess.ID, err)
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

			// Generate new embedding
			result, err := embedder.Embed(ctx, win.Summary)
			if err != nil {
				log.Printf("warning: failed to embed window %s: %v", win.ID, err)
				continue
			}

			// Serialize embedding
			embeddingBytes := vector.SerializeF32(result.Vec)

			// Update window embedding only (summary unchanged)
			if err := sessionStore.SetContextWindowEmbedding(ctx, win.ID, embeddingBytes, expectedModel); err != nil {
				log.Printf("warning: failed to update window %s embedding: %v", win.ID, err)
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
func buildEmbeddingTextFromSession(sess sessions.Session) string {
	var parts []string
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
	return strings.Join(parts, "\n")
}

// summarizeWindows generates LLM-based summaries for each context window.
func summarizeWindows(ctx context.Context, sessionStore *sessions.Store, session sessions.Session, providers []LLMProvider, input Input) Output {
	output := Output{
		SessionID: session.ID,
		Status:    "windows_summarized",
	}

	// Get all context windows for this session
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

	summarized := 0
	skipped := 0

	for _, window := range windows {
		// Skip windows that already have LLM summaries (unless force)
		// Check for marker that indicates LLM-generated summary
		if !input.Force && strings.TrimSpace(window.Summary) != "" && len(window.Summary) < 2000 {
			// Existing summary that's reasonably sized (not raw compact message)
			skipped++
			continue
		}

		// Build content from chunks using the window's chunk range
		var contentParts []string
		estimatedTokens := 0
		maxTokens := 4000 // Keep window summaries focused

		for chunkIdx := window.ChunkStart; chunkIdx <= window.ChunkEnd; chunkIdx++ {
			chunk, err := sessionStore.GetChunk(ctx, session.ID, chunkIdx)
			if err != nil {
				continue
			}
			preview := chunk.ContentPreview
			if preview == "" {
				continue
			}
			chunkTokens := len(preview) / 4 // rough estimate
			if estimatedTokens+chunkTokens > maxTokens {
				break
			}
			contentParts = append(contentParts, fmt.Sprintf("[%s] %s", chunk.ChunkType, preview))
			estimatedTokens += chunkTokens
		}

		if len(contentParts) == 0 {
			skipped++
			continue
		}

		// Build prompt for window summarization
		windowContent := strings.Join(contentParts, "\n")
		summary, err := summarizeWindowContent(ctx, providers, window.WindowIndex, windowContent)
		if err != nil {
			// Log error but continue with other windows
			skipped++
			continue
		}

		// Update window summary only (embedding unchanged)
		if err := sessionStore.UpdateContextWindowSummary(ctx, window.ID, summary); err != nil {
			skipped++
			continue
		}

		summarized++
	}

	output.WindowsSummarized = summarized
	output.WindowsSkipped = skipped
	output.Message = fmt.Sprintf("Summarized %d windows (%d skipped) for session %s", summarized, skipped, session.ID)

	return output
}

// summarizeWindowContent calls LLM to generate a concise summary for a window.
func summarizeWindowContent(ctx context.Context, providers []LLMProvider, windowIndex int, content string) (string, error) {
	prompt := fmt.Sprintf(`Summarize this coding session context window (window #%d) in 2-3 sentences.
Focus on:
- What was the main task or goal?
- What was accomplished or attempted?
- Any key decisions or problems encountered?

Be concise and specific. Output only the summary text, no JSON or formatting.

<content>
%s
</content>`, windowIndex, content)

	// Try providers in order
	for _, p := range providers {
		var summary string
		var err error

		if p.IsCLI {
			summary, err = callCLIForWindowSummary(ctx, p, prompt)
		} else {
			summary, err = callAPIForWindowSummary(ctx, p, prompt)
		}

		if err == nil && summary != "" {
			return strings.TrimSpace(summary), nil
		}

		if !isRetryableError(err) {
			return "", err
		}
	}

	return "", fmt.Errorf("all providers failed")
}

// callAPIForWindowSummary calls an OpenAI-compatible API for a simple text response.
func callAPIForWindowSummary(ctx context.Context, provider LLMProvider, prompt string) (string, error) {
	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": 500,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
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
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return result.Choices[0].Message.Content, nil
}

// callCLIForWindowSummary calls a CLI tool for a simple text response.
func callCLIForWindowSummary(ctx context.Context, provider LLMProvider, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch provider.Name {
	case "gemini-cli":
		cmd = exec.CommandContext(ctx, "gemini", "-m", provider.Model, "-p", prompt)
	case "claude-cli":
		cmd = exec.CommandContext(ctx, "claude", "-p", prompt, "--model", provider.Model, "--output-format", "text")
	default:
		return "", fmt.Errorf("unknown CLI provider: %s", provider.Name)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("CLI error: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.String(), nil
}

// buildEmbeddingText creates text to embed from the summary response.
func buildEmbeddingText(summary *SummaryResponse) string {
	var parts []string

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

	if len(summary.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(summary.Tags, ", "))
	}

	return strings.Join(parts, "\n")
}
