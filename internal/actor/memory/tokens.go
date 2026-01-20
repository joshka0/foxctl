package memory

// Token estimation utilities.
//
// Contract: Simple estimator for MVP
// - Uses len(text)/4 as rough approximation
// - Provider-agnostic (works across Gemini, Claude, OpenAI)
// - Targets 80% of budget to leave headroom for estimation error

const (
	// CharsPerToken is the approximate number of characters per token.
	// This is a rough estimate that works reasonably well for English text.
	CharsPerToken = 4

	// SafetyMargin is the fraction of budget to use (80%).
	// This leaves headroom for estimation error.
	SafetyMargin = 0.8
)

func normalizeMargin(margin float64) float64 {
	if margin <= 0 || margin > 1 {
		return SafetyMargin
	}
	return margin
}

func effectiveBudget(budget int, margin float64) int {
	if budget <= 0 {
		return 0
	}
	return int(float64(budget) * normalizeMargin(margin))
}

// EstimateTokens estimates the number of tokens in text.
//
// Uses a simple heuristic: ~4 characters per token.
// This is intentionally conservative and works across different providers.
func EstimateTokens(text string) int {
	return len(text) / CharsPerToken
}

// EstimateTokensWithOverhead adds overhead for message formatting.
func EstimateTokensWithOverhead(text string, overhead int) int {
	return EstimateTokens(text) + overhead
}

// FitsInBudget checks if text fits within the token budget.
// Uses 80% of budget as safety margin.
func FitsInBudget(text string, budget int) bool {
	estimated := EstimateTokens(text)
	effective := effectiveBudget(budget, SafetyMargin)
	return estimated <= effective
}

// RemainingBudget calculates remaining tokens in budget.
func RemainingBudget(used, budget int) int {
	effective := effectiveBudget(budget, SafetyMargin)
	remaining := effective - used
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TruncateToFit truncates text to fit within token budget.
// Returns the truncated text and whether truncation occurred.
// Uses rune-aware truncation to avoid corrupting multi-byte UTF-8 characters.
func TruncateToFit(text string, budget int) (string, bool) {
	return TruncateToFitWithMargin(text, budget, SafetyMargin, false)
}

// TruncateToFitTail truncates text to fit within token budget, keeping the tail.
func TruncateToFitTail(text string, budget int) (string, bool) {
	return TruncateToFitWithMargin(text, budget, SafetyMargin, true)
}

// TruncateToFitWithMargin truncates text with a custom safety margin.
// When fromTail is true, the tail of the text is preserved.
func TruncateToFitWithMargin(text string, budget int, margin float64, fromTail bool) (string, bool) {
	if text == "" {
		return text, false
	}

	effective := effectiveBudget(budget, margin)
	if EstimateTokens(text) <= effective {
		return text, false
	}

	maxChars := effective * CharsPerToken
	if maxChars <= 3 {
		return "...", true
	}

	if maxChars >= len(text) {
		return text, false
	}

	runes := []rune(text)
	maxRunes := maxChars - 3 // Reserve space for ellipsis
	if maxRunes >= len(runes) {
		return text, false
	}
	if maxRunes <= 0 {
		return "...", true
	}

	if fromTail {
		return "..." + string(runes[len(runes)-maxRunes:]), true
	}

	return string(runes[:maxRunes]) + "...", true
}

// TokenBudget represents a token budget with tracking.
type TokenBudget struct {
	Total     int
	Used      int
	Remaining int
	Margin    float64
}

// NewTokenBudget creates a new token budget.
func NewTokenBudget(total int) *TokenBudget {
	return NewTokenBudgetWithMargin(total, SafetyMargin)
}

// NewTokenBudgetWithMargin creates a new token budget with a custom safety margin.
func NewTokenBudgetWithMargin(total int, margin float64) *TokenBudget {
	effective := effectiveBudget(total, margin)
	return &TokenBudget{
		Total:     total,
		Used:      0,
		Remaining: effective,
		Margin:    normalizeMargin(margin),
	}
}

// Add adds tokens to the used count.
func (b *TokenBudget) Add(tokens int) {
	b.Used += tokens
	b.Remaining = effectiveBudget(b.Total, b.Margin) - b.Used
	if b.Remaining < 0 {
		b.Remaining = 0
	}
}

// AddText estimates and adds tokens for text.
func (b *TokenBudget) AddText(text string) int {
	tokens := EstimateTokens(text)
	b.Add(tokens)
	return tokens
}

// CanFit checks if additional tokens can fit.
func (b *TokenBudget) CanFit(tokens int) bool {
	return tokens <= b.Remaining
}

// CanFitText checks if text can fit.
func (b *TokenBudget) CanFitText(text string) bool {
	return b.CanFit(EstimateTokens(text))
}

// Reset resets the budget.
func (b *TokenBudget) Reset() {
	b.Used = 0
	b.Remaining = effectiveBudget(b.Total, b.Margin)
}

// UsagePercent returns the percentage of budget used.
func (b *TokenBudget) UsagePercent() float64 {
	if b.Total == 0 {
		return 0
	}
	return float64(b.Used) / float64(b.Total) * 100
}
