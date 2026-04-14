package companion

import (
	"strings"

	actormemory "github.com/joshka0/foxctl/internal/runtime/actor/memory"
	tiktoken "github.com/pkoukk/tiktoken-go"
)

// TokenCounter counts model tokens for companion memory budgeting.
type TokenCounter interface {
	Count(text string) int
}

type heuristicTokenCounter struct{}

func (heuristicTokenCounter) Count(text string) int {
	return actormemory.EstimateTokens(text)
}

type tikTokenCounter struct {
	encoder *tiktoken.Tiktoken
}

func (c tikTokenCounter) Count(text string) int {
	if c.encoder == nil || strings.TrimSpace(text) == "" {
		return 0
	}
	tokens := c.encoder.Encode(text, nil, nil)
	return len(tokens)
}

// NewHeuristicTokenCounter returns the existing len/4 estimator.
func NewHeuristicTokenCounter() TokenCounter {
	return heuristicTokenCounter{}
}

// NewTikTokenCounter returns a tiktoken-backed counter for the provided model.
//
// The model may include provider prefixes (for example "openai/gpt-4o" or
// "openrouter/openai/gpt-4o"). When model-specific encoding resolution fails,
// it falls back to cl100k_base.
func NewTikTokenCounter(model string) TokenCounter {
	model = normalizeModelNameForTokenizer(model)

	encoder, err := tiktoken.EncodingForModel(model)
	if err != nil || encoder == nil {
		encoder, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil || encoder == nil {
			return NewHeuristicTokenCounter()
		}
	}

	return tikTokenCounter{encoder: encoder}
}

func normalizeModelNameForTokenizer(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}

	// openrouter/provider/model -> model
	// provider/model -> model
	if strings.Contains(model, "/") {
		parts := strings.Split(model, "/")
		model = parts[len(parts)-1]
	}

	return strings.TrimSpace(model)
}
