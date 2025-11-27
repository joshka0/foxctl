package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// OpenAIConfig configures the OpenAI-compatible planner.
type OpenAIConfig struct {
	APIKey  string // API key (from env OPENAI_API_KEY or GROQ_API_KEY)
	BaseURL string // API base URL (default: https://api.openai.com/v1)
	Model   string // Model name (default: gpt-4o-mini)
	Timeout time.Duration
}

// OpenAIPlanner implements Planner using an OpenAI-compatible API.
// Works with OpenAI, Groq, and other compatible providers.
type OpenAIPlanner struct {
	config OpenAIConfig
	client *http.Client
}

// NewOpenAIPlanner creates a new OpenAI-compatible planner.
// Automatically detects Groq API key if GROQ_API_KEY is set.
func NewOpenAIPlanner(config OpenAIConfig) *OpenAIPlanner {
	if config.APIKey == "" {
		// Try Groq first, then OpenAI
		config.APIKey = os.Getenv("GROQ_API_KEY")
		if config.APIKey == "" {
			config.APIKey = os.Getenv("OPENAI_API_KEY")
		}
	}

	if config.BaseURL == "" {
		// Auto-detect based on API key source
		if os.Getenv("GROQ_API_KEY") != "" && config.APIKey == os.Getenv("GROQ_API_KEY") {
			config.BaseURL = "https://api.groq.com/openai/v1"
		} else {
			config.BaseURL = "https://api.openai.com/v1"
		}
	}

	if config.Model == "" {
		// Default model based on provider
		if config.BaseURL == "https://api.groq.com/openai/v1" {
			config.Model = "llama-3.3-70b-versatile"
		} else {
			config.Model = "gpt-4o-mini"
		}
	}

	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
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
		return nil, fmt.Errorf("no API key configured (set GROQ_API_KEY or OPENAI_API_KEY)")
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
	defer func() {
		_ = resp.Body.Close()
	}()

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

// Provider returns the provider name (e.g., "groq" or "openai").
func (p *OpenAIPlanner) Provider() string {
	if p.config.BaseURL == "https://api.groq.com/openai/v1" {
		return "groq"
	}
	return "openai"
}
