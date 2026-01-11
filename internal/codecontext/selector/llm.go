package selector

import (
	"context"

	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

// LLMSelector uses a language model to select relevant code spans.
// This is a placeholder for future LLM-assisted selection.
//
// Potential implementations:
//   - Local model (Qwen, CodeLlama) for fast selection
//   - Cloud model (Claude, GPT) for complex queries
//   - Hybrid: heuristic pre-filter + LLM refinement
type LLMSelector struct {
	opts    LLMOpts
	backend LLMBackend
}

// LLMOpts configures the LLM selector.
type LLMOpts struct {
	// Model is the model identifier to use.
	Model string

	// MaxTokens limits the response length.
	MaxTokens int

	// Temperature controls randomness (0.0 = deterministic).
	Temperature float64

	// PreFilter uses heuristic selection first to reduce context.
	PreFilter bool

	// MaxPreFilterSpans limits heuristic pre-filtering.
	MaxPreFilterSpans int
}

// LLMBackend is the interface for LLM providers.
// This will be implemented by specific providers (local, OpenRouter, etc.)
type LLMBackend interface {
	// Complete generates a completion for the given prompt.
	Complete(ctx context.Context, prompt string, opts LLMOpts) (string, error)
}

// NewLLM creates a new LLM selector.
// Returns an error if no backend is provided.
func NewLLM(opts LLMOpts, backend LLMBackend) (*LLMSelector, error) {
	if backend == nil {
		return nil, &SelectorError{
			Selector: "llm",
			Message:  "backend is required",
		}
	}

	// Apply defaults
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 1024
	}
	if opts.MaxPreFilterSpans <= 0 {
		opts.MaxPreFilterSpans = 20
	}

	return &LLMSelector{
		opts:    opts,
		backend: backend,
	}, nil
}

func (s *LLMSelector) Name() string {
	return "llm"
}

func (s *LLMSelector) Select(ctx context.Context, query string, content *files.FileContent, hints Hints) ([]Span, error) {
	hints.ApplyDefaults()

	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// If pre-filter is enabled, use heuristic first
	var candidateSpans []Span
	if s.opts.PreFilter {
		heuristic := NewHeuristic(HeuristicOpts{ContextLines: 5})
		preHints := hints
		preHints.MaxSpans = s.opts.MaxPreFilterSpans
		preHints.ExpandToBlock = true

		var err error
		candidateSpans, err = heuristic.Select(ctx, query, content, preHints)
		if err != nil {
			return nil, &SelectorError{
				Selector: "llm",
				Message:  "pre-filter failed",
				Cause:    err,
			}
		}
	}

	// Build prompt for LLM
	prompt := s.buildPrompt(query, content, candidateSpans, hints)

	// Call LLM backend
	response, err := s.backend.Complete(ctx, prompt, s.opts)
	if err != nil {
		return nil, &SelectorError{
			Selector: "llm",
			Message:  "completion failed",
			Cause:    err,
		}
	}

	// Parse LLM response into spans
	spans, err := s.parseResponse(response, content.LineCount())
	if err != nil {
		return nil, &SelectorError{
			Selector: "llm",
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	// Limit to max spans
	if len(spans) > hints.MaxSpans {
		spans = spans[:hints.MaxSpans]
	}

	return spans, nil
}

// buildPrompt constructs the prompt for the LLM.
func (s *LLMSelector) buildPrompt(query string, content *files.FileContent, candidates []Span, hints Hints) string {
	// TODO: Implement prompt construction
	// The prompt should:
	// 1. Describe the task (select relevant code spans)
	// 2. Include the query
	// 3. Include candidate spans or full file content
	// 4. Request structured output (line ranges)
	return ""
}

// parseResponse parses the LLM response into spans.
func (s *LLMSelector) parseResponse(response string, totalLines int) ([]Span, error) {
	// TODO: Implement response parsing
	// The response should be parsed into structured spans
	// Handle various output formats (JSON, line ranges, etc.)
	return nil, nil
}

// NoOpBackend is a placeholder backend that returns empty results.
// Use this for testing or when LLM is not available.
type NoOpBackend struct{}

func (b *NoOpBackend) Complete(ctx context.Context, prompt string, opts LLMOpts) (string, error) {
	return "", nil
}
