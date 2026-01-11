package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/rs/zerolog"
)

// LLMChatEngine implements AgentEngine using OpenAI-compatible chat completions.
// Supports OpenRouter, Groq, OpenAI, and other compatible providers.
type LLMChatEngine struct {
	config     LLMChatConfig
	client     *http.Client
	toolRunner *ToolRunner
	logger     zerolog.Logger
}

// LLMChatConfig configures the LLM chat engine.
type LLMChatConfig struct {
	// Provider is the LLM provider: "openrouter", "groq", "openai"
	Provider string

	// APIKey is the API key for the provider.
	APIKey string

	// BaseURL is the API base URL. Auto-detected if empty.
	BaseURL string

	// Model is the model name. Auto-detected if empty.
	Model string

	// MaxIterations limits the tool call loop.
	MaxIterations int

	// Timeout is the HTTP request timeout.
	Timeout time.Duration

	// Temperature controls randomness (0-1).
	Temperature float64

	// MaxTokens limits the response length.
	MaxTokens int

	// HookDispatcher for pre/post tool use hooks (optional).
	HookDispatcher hooks.Dispatcher

	// Logger for structured logging.
	Logger zerolog.Logger
}

// DefaultLLMChatConfig returns sensible defaults.
func DefaultLLMChatConfig() LLMChatConfig {
	return LLMChatConfig{
		MaxIterations: 50,
		Timeout:       120 * time.Second,
		Temperature:   0.0,
		MaxTokens:     8192,
	}
}

// NewLLMChatEngine creates a new LLM chat engine.
// Auto-detects provider from environment if not specified.
func NewLLMChatEngine(cfg LLMChatConfig) (*LLMChatEngine, error) {
	// Auto-detect provider and API key
	if cfg.APIKey == "" {
		cfg.APIKey, cfg.Provider = detectProvider()
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key configured (set OPENROUTER_API_KEY, GROQ_API_KEY, or OPENAI_API_KEY)")
	}

	// Set base URL based on provider
	if cfg.BaseURL == "" {
		cfg.BaseURL = baseURLForProvider(cfg.Provider)
	}

	// Set default model based on provider
	if cfg.Model == "" {
		cfg.Model = defaultModelForProvider(cfg.Provider)
	}

	// Apply defaults
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 50
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}

	engine := &LLMChatEngine{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		logger: cfg.Logger,
	}

	return engine, nil
}

// SetToolRunner sets the tool runner for executing tools.
func (e *LLMChatEngine) SetToolRunner(runner *ToolRunner) {
	e.toolRunner = runner
}

// Run implements AgentEngine.
func (e *LLMChatEngine) Run(ctx context.Context, input EngineInput) (EngineOutput, error) {
	// Build initial messages
	messages := e.buildMessages(input)
	tools := e.buildTools(input.Tools)

	var output EngineOutput
	iteration := 0

	for {
		// Check iteration limit
		iteration++
		if iteration > e.config.MaxIterations {
			output.StopReason = StopReasonMaxIterations
			output.Error = fmt.Sprintf("exceeded max iterations (%d)", e.config.MaxIterations)
			return output, nil
		}

		// Check context cancellation
		if ctx.Err() != nil {
			output.StopReason = StopReasonCancelled
			output.Error = ctx.Err().Error()
			return output, nil
		}

		// Call LLM
		resp, err := e.callLLM(ctx, messages, tools)
		if err != nil {
			output.StopReason = StopReasonError
			output.Error = err.Error()
			return output, nil
		}

		// Track tokens
		output.Tokens.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

		// Check for tool calls
		if len(resp.Choices) == 0 {
			output.StopReason = StopReasonError
			output.Error = "no response from LLM"
			return output, nil
		}

		choice := resp.Choices[0]

		// If tool calls present, execute them
		if len(choice.Message.ToolCalls) > 0 {
			// Add assistant message with tool calls to history
			messages = append(messages, choice.Message)

			// Execute each tool call
			for _, tc := range choice.Message.ToolCalls {
				toolCall := ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: json.RawMessage(tc.Function.Arguments),
				}
				output.ToolCalls = append(output.ToolCalls, toolCall)

				// Execute tool
				var result ToolResult
				if e.toolRunner != nil {
					result, _ = e.toolRunner.Execute(ctx, toolCall)
				} else {
					result = ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Tool %q not available", tc.Function.Name),
						IsError:    true,
					}
				}
				output.ToolResults = append(output.ToolResults, result)

				// Add tool result to messages
				messages = append(messages, oaiMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    result.Content,
				})
			}

			// Continue the loop
			continue
		}

		// No tool calls - this is the final response
		output.AssistantText = choice.Message.Content
		output.StopReason = mapFinishReason(choice.FinishReason)
		return output, nil
	}
}

// buildMessages converts EngineInput messages to OpenAI format.
func (e *LLMChatEngine) buildMessages(input EngineInput) []oaiMessage {
	var messages []oaiMessage

	// Add system prompt if provided
	if input.SystemPrompt != "" {
		messages = append(messages, oaiMessage{
			Role:    "system",
			Content: input.SystemPrompt,
		})
	}

	// Convert input messages
	for _, msg := range input.Messages {
		oaiMsg := oaiMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Handle tool calls in assistant messages
		if len(msg.ToolCalls) > 0 {
			oaiMsg.ToolCalls = make([]oaiToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				oaiMsg.ToolCalls[i] = oaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oaiFunction{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
		}

		// Handle tool result messages
		if msg.Role == RoleTool {
			oaiMsg.ToolCallID = msg.ToolCallID
		}

		messages = append(messages, oaiMsg)
	}

	return messages
}

// buildTools converts ToolDef to OpenAI format.
func (e *LLMChatEngine) buildTools(tools []ToolDef) []oaiTool {
	if len(tools) == 0 {
		return nil
	}

	oaiTools := make([]oaiTool, len(tools))
	for i, t := range tools {
		oaiTools[i] = oaiTool{
			Type:     "function",
			Function: oaiToolFunction(t),
		}
	}
	return oaiTools
}

// callLLM makes the API request.
func (e *LLMChatEngine) callLLM(ctx context.Context, messages []oaiMessage, tools []oaiTool) (*oaiResponse, error) {
	reqBody := oaiRequest{
		Model:       e.config.Model,
		Messages:    messages,
		Temperature: e.config.Temperature,
		MaxTokens:   e.config.MaxTokens,
	}

	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.config.BaseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.config.APIKey)

	// OpenRouter-specific headers
	if e.config.Provider == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://agentctl.dev")
		req.Header.Set("X-Title", "agentctl")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if oaiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", oaiResp.Error.Message)
	}

	return &oaiResp, nil
}

// OpenAI API types

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type oaiResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message      oaiMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Helper functions

func detectProvider() (apiKey, provider string) {
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		return key, "openrouter"
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		return key, "groq"
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key, "openai"
	}
	return "", ""
}

func baseURLForProvider(provider string) string {
	switch provider {
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	default:
		return "https://api.openai.com/v1"
	}
}

func defaultModelForProvider(provider string) string {
	switch provider {
	case "openrouter":
		if model := os.Getenv("OPENROUTER_MODEL"); model != "" {
			return model
		}
		return "openai/gpt-4o-mini"
	case "groq":
		return "llama-3.3-70b-versatile"
	default:
		return "gpt-4o-mini"
	}
}

func mapFinishReason(reason string) StopReason {
	switch reason {
	case "stop":
		return StopReasonEndTurn
	case "length":
		return StopReasonMaxTokens
	case "tool_calls":
		return StopReasonEndTurn // Will continue in loop
	default:
		return StopReasonEndTurn
	}
}
