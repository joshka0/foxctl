package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/providers/llmcompat"
)

// LLMClient executes a single-turn chat completion.
type LLMClient interface {
	Chat(ctx context.Context, systemPrompt, userPrompt string, opts LLMCallOptions) (string, error)
}

// LLMCallOptions configures a single LLM call.
type LLMCallOptions struct {
	MaxTokens   int
	Temperature float64
}

// OpenAIConfig configures an OpenAI-compatible client.
type OpenAIConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	Timeout  time.Duration
}

// OpenAIClient calls OpenAI-compatible chat completion APIs.
type OpenAIClient struct {
	cfg    OpenAIConfig
	client *http.Client
}

// NewOpenAIClient creates an OpenAI-compatible client with sensible defaults.
func NewOpenAIClient(cfg OpenAIConfig) (*OpenAIClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("api key is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = baseURLForProvider(cfg.Provider)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = llmproviders.DefaultModelForProvider(cfg.Provider)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &OpenAIClient{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

type openAIRequest struct {
	Model              string          `json:"model"`
	Messages           []openAIMessage `json:"messages"`
	Temperature        float64         `json:"temperature,omitempty"`
	MaxTokens          int             `json:"max_tokens,omitempty"`
	ChatTemplateKwargs map[string]any  `json:"chat_template_kwargs,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat executes a single-turn chat completion.
func (c *OpenAIClient) Chat(ctx context.Context, systemPrompt, userPrompt string, opts LLMCallOptions) (string, error) {
	if c == nil {
		return "", fmt.Errorf("client is nil")
	}
	messages := make([]openAIMessage, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		systemPrompt = llmcompat.ApplySystemPromptDefaults(c.cfg.Model, systemPrompt)
		messages = append(messages, openAIMessage{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: userPrompt})

	reqBody := openAIRequest{
		Model:    c.cfg.Model,
		Messages: messages,
	}
	if opts.MaxTokens > 0 {
		reqBody.MaxTokens = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		reqBody.Temperature = opts.Temperature
	}
	if llmcompat.IsQwenModel(c.cfg.Model) {
		reqBody.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM error (status %d): %s", resp.StatusCode, string(body))
	}

	var out openAIResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("LLM error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("no completion returned")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content != "" {
		return content, nil
	}
	reasoning := strings.TrimSpace(out.Choices[0].Message.ReasoningContent)
	if reasoning != "" {
		return reasoning, nil
	}
	return "", nil
}

func baseURLForProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "cerebras":
		return "https://api.cerebras.ai/v1"
	case "gemini":
		return "https://gemini.googleapis.com/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "lmstudio":
		return "http://localhost:1234/v1"
	case "openai":
		return "https://api.openai.com/v1"
	default:
		return ""
	}
}
