package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamConfig configures streaming behavior.
type StreamConfig struct {
	// Stream enables streaming mode.
	Stream bool

	// OnDelta is called for each streaming delta.
	OnDelta func(StreamDelta)

	// OnToolCall is called when a tool call is detected.
	OnToolCall func(ToolCall)

	// OnToolResult is called after a tool is executed.
	OnToolResult func(ToolCall, ToolResult)
}

// StreamDelta represents a streaming chunk from the LLM.
type StreamDelta struct {
	// ContentDelta is the new text content.
	ContentDelta string

	// ToolCallDelta is a partial tool call update.
	ToolCallDelta *ToolCallStreamDelta

	// FinishReason indicates why streaming stopped.
	FinishReason string
}

// ToolCallStreamDelta represents a streaming tool call update.
type ToolCallStreamDelta struct {
	// Index is the tool call index.
	Index int

	// ID is the tool call ID (only in first chunk).
	ID string

	// Name is the function name (only in first chunk).
	Name string

	// ArgumentsDelta is the incremental arguments JSON.
	ArgumentsDelta string
}

// RunStreaming executes with streaming output.
func (e *LLMChatEngine) RunStreaming(ctx context.Context, input EngineInput, streamCfg StreamConfig) (EngineOutput, error) {
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

		// Call LLM with streaming
		resp, err := e.callLLMStreaming(ctx, messages, tools, streamCfg)
		if err != nil {
			output.StopReason = StopReasonError
			output.Error = err.Error()
			return output, nil
		}

		// Track tokens (estimated for streaming)
		output.Tokens.Add(resp.inputTokens, resp.outputTokens)

		// If tool calls present, execute them
		if len(resp.toolCalls) > 0 {
			// Build assistant message with tool calls
			assistantMsg := oaiMessage{
				Role:      "assistant",
				ToolCalls: make([]oaiToolCall, len(resp.toolCalls)),
			}
			for i, tc := range resp.toolCalls {
				assistantMsg.ToolCalls[i] = oaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oaiFunction{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
			if resp.content != "" {
				assistantMsg.Content = resp.content
			}
			messages = append(messages, assistantMsg)

			// Execute each tool call
			for _, tc := range resp.toolCalls {
				toolCall := ToolCall(tc)
				output.ToolCalls = append(output.ToolCalls, toolCall)

				// Notify callback
				if streamCfg.OnToolCall != nil {
					streamCfg.OnToolCall(toolCall)
				}

				// Execute tool
				var result ToolResult
				if e.toolRunner != nil {
					var execErr error
					result, execErr = e.toolRunner.Execute(ctx, toolCall)
					if execErr != nil {
						result = ToolResult{
							ToolCallID: tc.ID,
							Content:    fmt.Sprintf("Tool execution error: %v", execErr),
							IsError:    true,
						}
					}
				} else {
					result = ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Tool %q not available", tc.Name),
						IsError:    true,
					}
				}
				output.ToolResults = append(output.ToolResults, result)

				// Notify callback
				if streamCfg.OnToolResult != nil {
					streamCfg.OnToolResult(toolCall, result)
				}

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
		output.AssistantText = resp.content
		output.StopReason = mapFinishReason(resp.finishReason)
		return output, nil
	}
}

// streamResponse holds the accumulated streaming response.
type streamResponse struct {
	content      string
	toolCalls    []accumulatedToolCall
	finishReason string
	inputTokens  int
	outputTokens int
}

// accumulatedToolCall is a tool call built from stream deltas.
type accumulatedToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// callLLMStreaming makes a streaming API request.
func (e *LLMChatEngine) callLLMStreaming(ctx context.Context, messages []oaiMessage, tools []oaiTool, streamCfg StreamConfig) (*streamResponse, error) {
	reqBody := oaiRequest{
		Model:       e.config.Model,
		Messages:    messages,
		Temperature: e.config.Temperature,
		MaxTokens:   e.config.MaxTokens,
	}

	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	// Add stream flag
	reqBodyMap := map[string]any{
		"model":       reqBody.Model,
		"messages":    reqBody.Messages,
		"temperature": reqBody.Temperature,
		"max_tokens":  reqBody.MaxTokens,
		"stream":      true,
	}
	if len(tools) > 0 {
		reqBodyMap["tools"] = tools
	}

	jsonBody, err := json.Marshal(reqBodyMap)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse SSE stream
	return e.parseSSEStream(resp.Body, streamCfg)
}

// parseSSEStream parses the SSE response stream.
func (e *LLMChatEngine) parseSSEStream(reader io.Reader, streamCfg StreamConfig) (*streamResponse, error) {
	scanner := bufio.NewScanner(reader)

	result := &streamResponse{
		toolCalls: make([]accumulatedToolCall, 0),
	}

	var contentBuilder strings.Builder
	toolCallBuilders := make(map[int]*toolCallBuilder)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Check for stream end
			if data == "[DONE]" {
				break
			}

			// Parse chunk
			var chunk oaiStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]

			// Accumulate content
			if choice.Delta.Content != "" {
				contentBuilder.WriteString(choice.Delta.Content)

				// Emit delta callback
				if streamCfg.OnDelta != nil {
					streamCfg.OnDelta(StreamDelta{
						ContentDelta: choice.Delta.Content,
					})
				}
			}

			// Accumulate tool calls
			for _, tc := range choice.Delta.ToolCalls {
				builder, ok := toolCallBuilders[tc.Index]
				if !ok {
					builder = &toolCallBuilder{}
					toolCallBuilders[tc.Index] = builder
				}

				if tc.ID != "" {
					builder.id = tc.ID
				}
				if tc.Function.Name != "" {
					builder.name = tc.Function.Name
				}
				builder.arguments.WriteString(tc.Function.Arguments)

				// Emit delta callback
				if streamCfg.OnDelta != nil {
					streamCfg.OnDelta(StreamDelta{
						ToolCallDelta: &ToolCallStreamDelta{
							Index:          tc.Index,
							ID:             tc.ID,
							Name:           tc.Function.Name,
							ArgumentsDelta: tc.Function.Arguments,
						},
					})
				}
			}

			// Track finish reason
			if choice.FinishReason != "" {
				result.finishReason = choice.FinishReason

				// Emit delta callback
				if streamCfg.OnDelta != nil {
					streamCfg.OnDelta(StreamDelta{
						FinishReason: choice.FinishReason,
					})
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	// Build result
	result.content = contentBuilder.String()

	// Build tool calls
	for i := 0; i < len(toolCallBuilders); i++ {
		if builder, ok := toolCallBuilders[i]; ok {
			result.toolCalls = append(result.toolCalls, accumulatedToolCall{
				ID:        builder.id,
				Name:      builder.name,
				Arguments: json.RawMessage(builder.arguments.String()),
			})
		}
	}

	return result, nil
}

// toolCallBuilder accumulates tool call chunks.
type toolCallBuilder struct {
	id        string
	name      string
	arguments strings.Builder
}

// oaiStreamChunk is a streaming response chunk.
type oaiStreamChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        int      `json:"index"`
		Delta        oaiDelta `json:"delta"`
		FinishReason string   `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage,omitempty"`
}

// oaiDelta is the delta content in a streaming chunk.
type oaiDelta struct {
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []oaiToolCallDelta `json:"tool_calls,omitempty"`
}

// oaiToolCallDelta is a tool call delta in streaming.
type oaiToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}
