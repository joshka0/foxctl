package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	ToolChoice  any                `json:"tool_choice,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (e *LLMChatEngine) callAnthropicCompat(ctx context.Context, messages []oaiMessage, tools []oaiTool) (*oaiResponse, error) {
	reqBody := anthropicRequest{
		Model:       e.config.Model,
		Messages:    anthropicMessagesFromOAI(messages),
		Tools:       anthropicToolsFromOAI(tools),
		Temperature: e.config.Temperature,
		MaxTokens:   e.config.MaxTokens,
	}
	reqBody.System = anthropicSystemFromOAI(messages)
	if len(tools) > 0 && string(e.config.ToolChoice) == `"required"` {
		reqBody.ToolChoice = map[string]string{"type": "any"}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicMessagesURL(e.config.BaseURL), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	applyAuthHeader(req, e.config)
	applyAnthropicCompatProviderHeaders(req, e.config)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(body))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("anthropic API error: %s", anthropicResp.Error.Message)
	}
	return oaiResponseFromAnthropic(anthropicResp), nil
}

func anthropicMessagesURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	switch {
	case strings.HasSuffix(base, "/v1/messages"):
		return base
	case strings.HasSuffix(base, "/messages"):
		return base
	case strings.HasSuffix(base, "/v1"):
		return base + "/messages"
	default:
		return base + "/v1/messages"
	}
}

func applyAnthropicCompatProviderHeaders(req *http.Request, cfg LLMChatConfig) {
	if isKimiCodingEndpoint(cfg.BaseURL) {
		req.Header.Set("User-Agent", "claude-code/0.1.0")
	}
}

func isKimiCodingEndpoint(baseURL string) bool {
	normalized := strings.ToLower(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	return strings.HasPrefix(normalized, "https://api.kimi.com/coding")
}

func anthropicSystemFromOAI(messages []oaiMessage) string {
	var parts []string
	for _, msg := range messages {
		if msg.Role == "system" && strings.TrimSpace(msg.Content) != "" {
			parts = append(parts, strings.TrimSpace(msg.Content))
		}
	}
	return strings.Join(parts, "\n\n")
}

func anthropicMessagesFromOAI(messages []oaiMessage) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			continue
		case "assistant":
			out = append(out, anthropicAssistantMessageFromOAI(msg))
		case "tool":
			out = append(out, anthropicToolResultMessageFromOAI(msg))
		default:
			out = append(out, anthropicTextMessageFromOAI("user", msg.Content))
		}
	}
	return out
}

func anthropicTextMessageFromOAI(role, text string) anthropicMessage {
	return anthropicMessage{
		Role: role,
		Content: []anthropicContentBlock{{
			Type: "text",
			Text: text,
		}},
	}
}

func anthropicAssistantMessageFromOAI(msg oaiMessage) anthropicMessage {
	blocks := make([]anthropicContentBlock, 0, 1+len(msg.ToolCalls))
	if strings.TrimSpace(msg.Content) != "" {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
	}
	for _, call := range msg.ToolCalls {
		input := json.RawMessage(strings.TrimSpace(call.Function.Arguments))
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Function.Name,
			Input: input,
		})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: ""})
	}
	return anthropicMessage{Role: "assistant", Content: blocks}
}

func anthropicToolResultMessageFromOAI(msg oaiMessage) anthropicMessage {
	return anthropicMessage{
		Role: "user",
		Content: []anthropicContentBlock{{
			Type:      "tool_result",
			ToolUseID: msg.ToolCallID,
			Content:   msg.Content,
		}},
	}
}

func anthropicToolsFromOAI(tools []oaiTool) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, tool := range tools {
		schema := tool.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
		}
		out = append(out, anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: schema,
		})
	}
	return out
}

func oaiResponseFromAnthropic(resp anthropicResponse) *oaiResponse {
	var msg oaiMessage
	msg.Role = "assistant"
	msg.Content = anthropicTextFromBlocks(resp.Content)
	msg.ReasoningContent = anthropicThinkingFromBlocks(resp.Content)
	msg.ToolCalls = oaiToolCallsFromAnthropic(resp.Content)

	out := &oaiResponse{
		ID: resp.ID,
		Choices: []struct {
			Message      oaiMessage `json:"message"`
			FinishReason string     `json:"finish_reason"`
		}{{
			Message:      msg,
			FinishReason: oaiFinishReasonFromAnthropic(resp.StopReason),
		}},
	}
	out.Usage.PromptTokens = resp.Usage.InputTokens
	out.Usage.CompletionTokens = resp.Usage.OutputTokens
	return out
}

func anthropicTextFromBlocks(blocks []anthropicContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func anthropicThinkingFromBlocks(blocks []anthropicContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "thinking" && strings.TrimSpace(block.Thinking) != "" {
			parts = append(parts, strings.TrimSpace(block.Thinking))
		}
	}
	return strings.Join(parts, "\n")
}

func oaiToolCallsFromAnthropic(blocks []anthropicContentBlock) []oaiToolCall {
	var calls []oaiToolCall
	for _, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}
		input := block.Input
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		calls = append(calls, oaiToolCall{
			ID:   block.ID,
			Type: "function",
			Function: oaiFunction{
				Name:      block.Name,
				Arguments: string(input),
			},
		})
	}
	return calls
}

func oaiFinishReasonFromAnthropic(reason string) string {
	switch strings.TrimSpace(reason) {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}
