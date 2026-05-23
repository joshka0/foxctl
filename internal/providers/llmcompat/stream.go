package llmcompat

// StreamDelta is a normalized streaming update from an OpenAI-compatible chat
// completion stream.
type StreamDelta struct {
	ContentDelta  string         `json:"content_delta,omitempty"`
	ToolCallDelta *ToolCallDelta `json:"tool_call_delta,omitempty"`
	FinishReason  string         `json:"finish_reason,omitempty"`
}

// ToolCallDelta is a normalized partial tool-call update from a streaming
// response.
type ToolCallDelta struct {
	Index          int    `json:"index"`
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ArgumentsDelta string `json:"arguments_delta,omitempty"`
}

// ChatCompletionStreamChunk is the provider wire shape for a streamed
// OpenAI-compatible chat completion chunk.
type ChatCompletionStreamChunk struct {
	ID      string                       `json:"id"`
	Choices []ChatCompletionStreamChoice `json:"choices"`
	Usage   *ChatCompletionStreamUsage   `json:"usage,omitempty"`
}

// ChatCompletionStreamChoice is one streamed choice in a chat completion chunk.
type ChatCompletionStreamChoice struct {
	Index        int                       `json:"index"`
	Delta        ChatCompletionStreamDelta `json:"delta"`
	FinishReason string                    `json:"finish_reason"`
}

// ChatCompletionStreamDelta is the streamed message delta in a chat completion
// chunk.
type ChatCompletionStreamDelta struct {
	Role      string                              `json:"role,omitempty"`
	Content   string                              `json:"content,omitempty"`
	ToolCalls []ChatCompletionStreamToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatCompletionStreamToolCallDelta is a streamed tool-call delta.
type ChatCompletionStreamToolCallDelta struct {
	Index    int                                       `json:"index"`
	ID       string                                    `json:"id,omitempty"`
	Type     string                                    `json:"type,omitempty"`
	Function ChatCompletionStreamToolCallFunctionDelta `json:"function,omitempty"`
}

// ChatCompletionStreamToolCallFunctionDelta is the streamed function-call
// payload in a tool-call delta.
type ChatCompletionStreamToolCallFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ChatCompletionStreamUsage is optional usage data some OpenAI-compatible
// providers include on stream chunks.
type ChatCompletionStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
