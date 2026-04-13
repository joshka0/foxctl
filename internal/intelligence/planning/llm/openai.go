package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIConfig configures the OpenAI-compatible planner.
type OpenAIConfig struct {
	APIKey   string        // API key (must be provided by caller)
	BaseURL  string        // API base URL (default based on Provider)
	Model    string        // Model name (default based on Provider)
	Timeout  time.Duration // Request timeout (default: 60s)
	Provider string        // Provider: "openai", "groq", "openrouter", "cerebras", or "lmstudio"
}

// OpenAIPlanner implements Planner using an OpenAI-compatible API.
// Works with OpenAI, Groq, and other compatible providers.
type OpenAIPlanner struct {
	config OpenAIConfig
	client *http.Client
}

// NewOpenAIPlanner creates a new OpenAI-compatible planner.
// The caller must provide APIKey and Provider in the config.
// BaseURL and Model will be set to provider-specific defaults if not specified.
// This function does NOT read environment variables (FC/IS compliant).
func NewOpenAIPlanner(ctx context.Context, config OpenAIConfig) *OpenAIPlanner {
	// Set BaseURL based on provider if not specified
	if config.BaseURL == "" {
		switch config.Provider {
		case "cerebras":
			config.BaseURL = "https://api.cerebras.ai/v1"
		case "openrouter":
			config.BaseURL = "https://openrouter.ai/api/v1"
		case "groq":
			config.BaseURL = "https://api.groq.com/openai/v1"
		case "lmstudio":
			config.BaseURL = "http://localhost:1234/v1"
		default:
			config.BaseURL = "https://api.openai.com/v1"
		}
	}

	// Set default model based on provider if not specified
	if config.Model == "" {
		switch config.Provider {
		case "cerebras":
			config.Model = "llama3.1-8b" // Fastest, cheapest for background tasks
		case "openrouter":
			config.Model = "openai/gpt-4o-mini"
		case "groq":
			config.Model = "llama-3.3-70b-versatile"
		case "lmstudio":
			config.Model = "zai-org/glm-4.7-flash"
		default:
			config.Model = "gpt-4o-mini"
		}
	}

	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < config.Timeout {
			config.Timeout = remaining
		}
	}

	return &OpenAIPlanner{
		config: config,
		client: &http.Client{Timeout: config.Timeout},
	}
}

// openAIRequest represents the OpenAI chat completion request.
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse represents the OpenAI chat completion response.
type openAIResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Plan implements the Planner interface.
func (p *OpenAIPlanner) Plan(ctx context.Context, req PlanRequest) (*PlanResult, error) {
	if p.config.APIKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}

	prompt := buildPrompt(req)

	oaiReq := openAIRequest{
		Model: p.config.Model,
		Messages: []openAIMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3, // Lower temperature for more deterministic output
		MaxTokens:   4096,
	}

	reqBody, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if oaiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", oaiResp.Error.Message)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no completion returned")
	}

	content := oaiResp.Choices[0].Message.Content
	result, err := parseResponse(content)
	if err != nil {
		return nil, err
	}

	result.ModelUsed = p.config.Model
	result.TokensUsed = oaiResp.Usage.TotalTokens

	return result, nil
}

// Available returns true if the planner has a valid API key configured.
func (p *OpenAIPlanner) Available() bool {
	return p.config.APIKey != ""
}

// Provider returns the provider name (e.g., "cerebras", "groq", "openrouter", or "openai").
// Uses the explicitly tracked provider source, falling back to BaseURL detection for backward compatibility.
func (p *OpenAIPlanner) Provider() string {
	// Prefer explicitly tracked provider
	if p.config.Provider != "" {
		return p.config.Provider
	}
	// Fallback to BaseURL detection for backward compatibility with callers
	// that construct OpenAIConfig with explicit BaseURL but no Provider field
	switch p.config.BaseURL {
	case "https://api.cerebras.ai/v1":
		return "cerebras"
	case "https://api.groq.com/openai/v1":
		return "groq"
	case "https://openrouter.ai/api/v1":
		return "openrouter"
	case "http://localhost:1234/v1":
		return "lmstudio"
	default:
		return "openai"
	}
}
