package companion

import "github.com/rs/zerolog"

// LLMSummarizer holds provider credentials used by hybrid episode summarization.
type LLMSummarizer struct {
	provider string
	apiKey   string
	model    string
	logger   zerolog.Logger
}

// LLMSummarizerConfig configures the hybrid episode LLM summarizer.
type LLMSummarizerConfig struct {
	Provider string
	APIKey   string
	Model    string
	Logger   zerolog.Logger
}

// NewLLMSummarizer creates an LLM summarizer descriptor used by hybrid episode summarization.
func NewLLMSummarizer(cfg LLMSummarizerConfig) *LLMSummarizer {
	return &LLMSummarizer{
		provider: cfg.Provider,
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		logger:   cfg.Logger,
	}
}

func previewForLog(text string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 120
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}
