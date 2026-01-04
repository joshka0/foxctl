// Package main implements the session/summarize skill for generating structured summaries.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters.
type Input struct {
	SessionID string `json:"session_id"`
	Force     bool   `json:"force,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID      string   `json:"session_id"`
	Summary        string   `json:"summary"`
	Accomplished   []string `json:"accomplished"`
	Decisions      []string `json:"decisions"`
	Gotchas        []string `json:"gotchas"`
	UserInsights   []string `json:"user_insights,omitempty"`
	Tags           []string `json:"tags"`
	KeyFiles       []string `json:"key_files"`
	ToolsPattern   string   `json:"tools_pattern"`
	HasEmbedding   bool     `json:"has_embedding"`
	EmbeddingModel string   `json:"embedding_model,omitempty"`
	EmbeddingDims  int      `json:"embedding_dims,omitempty"`
	Status         string   `json:"status"`
	Message        string   `json:"message"`
}

// SummaryResponse is the expected JSON response from Cerebras.
type SummaryResponse struct {
	Summary      string   `json:"summary"`
	Accomplished []string `json:"accomplished"`
	Decisions    []string `json:"decisions"`
	Gotchas      []string `json:"gotchas"`
	UserInsights []string `json:"user_insights,omitempty"`
	Tags         []string `json:"tags"`
	KeyFiles     []string `json:"key_files"`
	ToolsPattern string   `json:"tools_pattern"`
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

// geminiEmbedRequest is the request body for the Gemini embed API.
type geminiEmbedRequest struct {
	Model   string            `json:"model"`
	Content geminiContentPart `json:"content"`
}

type geminiContentPart struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

// geminiEmbedResponse is the response from the Gemini embed API.
type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *geminiError `json:"error,omitempty"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Load config for embedding settings
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", fmt.Errorf("load config: %w", err))
	}

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err))
	}

	if input.SessionID == "" {
		fail("EARG", fmt.Errorf("session_id is required"))
	}

	if input.MaxTokens <= 0 {
		input.MaxTokens = defaultMaxTokens
	}

	// Get available LLM providers
	providers := getProviders()
	if len(providers) == 0 {
		fail("EARG", fmt.Errorf("no LLM provider configured (set GROQ_API_KEY, CEREBRAS_API_KEY, or OPENROUTER_API_KEY)"))
	}

	// Get agentctl home (prefer config, fallback to env/default)
	agentctlHome := cfg.Home
	if agentctlHome == "" {
		agentctlHome = os.Getenv("AGENTCTL_HOME")
		if agentctlHome == "" {
			homeDir, _ := os.UserHomeDir()
			agentctlHome = filepath.Join(homeDir, ".agentctl")
		}
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		fail("EIO", fmt.Errorf("open sessions store: %w", err))
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Get session
	session, err := sessionStore.Get(ctx, input.SessionID)
	if err != nil {
		fail("ENOTFOUND", fmt.Errorf("session not found: %w", err))
	}

	// Check if already summarized
	if !input.Force && session.Summary != "" {
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
		env := envelope.OK(command, output)
		errs.Ignore(envelope.Write(os.Stdout, env), "emit session/summarize result")
		return
	}

	// Call LLM for summarization (with fallback, filtering per-provider)
	summaryResp, usedProvider, err := summarizeWithFallback(ctx, providers, session.RawJSONLPath, input.MaxTokens)
	if err != nil {
		fail("ERUNTIME", fmt.Errorf("summarization failed (tried %d providers): %w", len(providers), err))
	}
	_ = usedProvider // TODO: could log which provider was used

	// Update session with summary
	err = sessionStore.UpdateSummary(ctx, session.ID,
		summaryResp.Summary,
		summaryResp.Accomplished,
		summaryResp.Decisions,
		summaryResp.Gotchas,
		summaryResp.UserInsights,
		summaryResp.Tags,
		summaryResp.KeyFiles,
		summaryResp.ToolsPattern,
	)
	if err != nil {
		fail("EIO", fmt.Errorf("save summary: %w", err))
	}

	output := Output{
		SessionID:    session.ID,
		Summary:      summaryResp.Summary,
		Accomplished: ensureSlice(summaryResp.Accomplished),
		Decisions:    ensureSlice(summaryResp.Decisions),
		Gotchas:      ensureSlice(summaryResp.Gotchas),
		UserInsights: ensureSlice(summaryResp.UserInsights),
		Tags:         ensureSlice(summaryResp.Tags),
		KeyFiles:     ensureSlice(summaryResp.KeyFiles),
		ToolsPattern: summaryResp.ToolsPattern,
		Status:       "summarized",
		Message: fmt.Sprintf("Summarized session %s: %s",
			session.ID, truncate(summaryResp.Summary, 100)),
	}

	// Generate embedding - prefer Voyage (rate-limited), fall back to Gemini
	embeddingText := buildEmbeddingText(summaryResp)
	var embedding []float32
	var embeddingModel string
	var embeddingProvider string
	var embeddingErr error

	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	if voyageKey != "" {
		// Use Voyage with built-in rate limiting (3 RPM for free tier)
		vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			embeddingErr = fmt.Errorf("voyage provider: %w", err)
		} else {
			embedding, embeddingErr = vp.Embed(ctx, embeddingText)
			embeddingModel = vp.Model()
			embeddingProvider = "voyage"
		}
	} else if geminiKey != "" {
		// Fall back to Gemini
		embeddingModel = cfg.Embedding.Model
		if embeddingModel == "" {
			embeddingModel = "gemini-embedding-001"
		}
		embedding, embeddingErr = generateGeminiEmbedding(ctx, geminiKey, embeddingModel, embeddingText)
		embeddingProvider = "gemini"
	}

	if embeddingErr != nil {
		// Log but don't fail - embedding is optional
		output.Message += fmt.Sprintf(" (embedding failed: %v)", embeddingErr)
	} else if len(embedding) > 0 {
		// Serialize embedding as binary float32
		embeddingBytes := serializeEmbedding(embedding)

		if err := sessionStore.SetEmbedding(ctx, session.ID, embeddingBytes, embeddingModel); err != nil {
			output.Message += fmt.Sprintf(" (save embedding failed: %v)", err)
		} else {
			output.HasEmbedding = true
			output.EmbeddingModel = embeddingModel
			output.EmbeddingDims = len(embedding)
			output.Message += fmt.Sprintf(" (embedded: %s/%d dims)", embeddingProvider, len(embedding))

			// Record embedding metadata for dimension validation on future opens
			meta := sessions.EmbeddingMetadata{
				TableName:  "sessions",
				ColumnName: "embedding",
				Provider:   embeddingProvider,
				Model:      embeddingModel,
				Dimensions: len(embedding),
			}
			if err := sessionStore.SetEmbeddingMetadata(ctx, meta); err != nil {
				log.Printf("[WARN] session_summarize: failed to set embedding metadata: %v", err)
			}
		}
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/summarize result")
}

// filterJSONL reads the raw JSONL and extracts high-signal content.
// Aggressively filters to reduce 35MB+ session files to a few hundred KB.
func filterJSONL(ctx context.Context, path string, maxTokens int) ([]FilteredMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { errs.Ignore(file.Close(), "close JSONL file") }()

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
  "user_insights": ["user feedback, corrections, preferences", "0-5 items"],
  "tags": ["topic", "tags", "3-7 items"],
  "key_files": ["important/files/modified.go", "up to 10"],
  "tools_pattern": "Common sequence like Read->Edit->Bash(test)"
}

Be concise and specific. Focus on:
- What was the main goal and outcome?
- What important technical decisions were made?
- What problems were encountered and how were they solved?
- What files were most important?
- What did the user explicitly ask for, correct, criticize, or provide feedback on?
  Look for phrases like: "that's not right", "I meant", "don't do", "please", "actually", "no,", "yes,", "let's", "can you"

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

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/summarize failure")
	os.Exit(1)
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

	if len(summary.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(summary.Tags, ", "))
	}

	return strings.Join(parts, "\n")
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// generateGeminiEmbedding calls the Gemini embedding API with the specified model.
func generateGeminiEmbedding(ctx context.Context, apiKey, model, text string) ([]float32, error) {
	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", geminiBaseURL, model, apiKey)

	reqBody := geminiEmbedRequest{
		Model: fmt.Sprintf("models/%s", model),
		Content: geminiContentPart{
			Parts: []geminiTextPart{{Text: text}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("API error %d: %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp geminiEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embedResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return embedResp.Embedding.Values, nil
}

// serializeEmbedding converts a float32 slice to binary bytes.
func serializeEmbedding(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := math.Float32bits(v)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}
