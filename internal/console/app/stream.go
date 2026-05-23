package consoleapp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/joshka0/foxctl/internal/providers/llmcompat"
)

// StreamDelta represents a chunk of streaming LLM output.
type StreamDelta = llmcompat.StreamDelta

// ToolCallDelta represents a streaming tool call update.
type ToolCallDelta = llmcompat.ToolCallDelta

// StreamParser parses SSE streams from OpenAI-compatible APIs.
type StreamParser struct {
	reader      io.Reader
	onDelta     func(StreamDelta)
	onError     func(error)
	accumulator *StreamAccumulator
}

// NewStreamParser creates a new stream parser.
func NewStreamParser(reader io.Reader, onDelta func(StreamDelta)) *StreamParser {
	return &StreamParser{
		reader:      reader,
		onDelta:     onDelta,
		accumulator: NewStreamAccumulator(),
	}
}

// Parse reads and parses the SSE stream.
func (p *StreamParser) Parse() error {
	scanner := bufio.NewScanner(p.reader)

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
				return nil
			}

			// Parse JSON chunk
			delta, err := p.parseChunk([]byte(data))
			if err != nil {
				if p.onError != nil {
					p.onError(err)
				}
				continue
			}

			// Accumulate and emit delta
			if delta != nil {
				p.accumulator.Add(*delta)
				if p.onDelta != nil {
					p.onDelta(*delta)
				}
			}
		}
	}

	return scanner.Err()
}

// parseChunk parses a single SSE data chunk.
func (p *StreamParser) parseChunk(data []byte) (*StreamDelta, error) {
	var chunk llmcompat.ChatCompletionStreamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, err
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	choice := chunk.Choices[0]
	delta := &StreamDelta{
		FinishReason: choice.FinishReason,
	}

	// Handle content delta
	if choice.Delta.Content != "" {
		delta.ContentDelta = choice.Delta.Content
	}

	// Handle tool call deltas
	if len(choice.Delta.ToolCalls) > 0 {
		tc := choice.Delta.ToolCalls[0]
		delta.ToolCallDelta = &ToolCallDelta{
			Index:          tc.Index,
			ID:             tc.ID,
			Name:           tc.Function.Name,
			ArgumentsDelta: tc.Function.Arguments,
		}
	}

	return delta, nil
}

// Result returns the accumulated result.
func (p *StreamParser) Result() *StreamAccumulator {
	return p.accumulator
}

// StreamAccumulator accumulates streaming deltas into complete data.
type StreamAccumulator struct {
	Content      strings.Builder
	ToolCalls    []AccumulatedToolCall
	FinishReason string
}

// AccumulatedToolCall is a complete tool call built from deltas.
type AccumulatedToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// NewStreamAccumulator creates a new accumulator.
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{
		ToolCalls: make([]AccumulatedToolCall, 0),
	}
}

// Add incorporates a delta into the accumulated state.
func (a *StreamAccumulator) Add(delta StreamDelta) {
	// Accumulate content
	if delta.ContentDelta != "" {
		a.Content.WriteString(delta.ContentDelta)
	}

	// Accumulate tool calls
	if delta.ToolCallDelta != nil {
		tc := delta.ToolCallDelta

		// Ensure we have enough slots
		for len(a.ToolCalls) <= tc.Index {
			a.ToolCalls = append(a.ToolCalls, AccumulatedToolCall{})
		}

		// Update the tool call
		if tc.ID != "" {
			a.ToolCalls[tc.Index].ID = tc.ID
		}
		if tc.Name != "" {
			a.ToolCalls[tc.Index].Name = tc.Name
		}
		if tc.ArgumentsDelta != "" {
			a.ToolCalls[tc.Index].Arguments.WriteString(tc.ArgumentsDelta)
		}
	}

	// Track finish reason
	if delta.FinishReason != "" {
		a.FinishReason = delta.FinishReason
	}
}

// GetContent returns the accumulated content.
func (a *StreamAccumulator) GetContent() string {
	return a.Content.String()
}

// GetToolCalls returns the completed tool calls.
func (a *StreamAccumulator) GetToolCalls() []CompletedToolCall {
	calls := make([]CompletedToolCall, len(a.ToolCalls))
	for i, tc := range a.ToolCalls {
		calls[i] = CompletedToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments.String(),
		}
	}
	return calls
}

// CompletedToolCall is a fully accumulated tool call.
type CompletedToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ParseSSEStream is a convenience function to parse an SSE stream.
func ParseSSEStream(data []byte, onDelta func(StreamDelta)) (*StreamAccumulator, error) {
	parser := NewStreamParser(bytes.NewReader(data), onDelta)
	if err := parser.Parse(); err != nil {
		return nil, err
	}
	return parser.Result(), nil
}
