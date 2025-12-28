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
	effectiveBudget := int(float64(budget) * SafetyMargin)
	return estimated <= effectiveBudget
}

// RemainingBudget calculates remaining tokens in budget.
func RemainingBudget(used, budget int) int {
	effectiveBudget := int(float64(budget) * SafetyMargin)
	remaining := effectiveBudget - used
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TruncateToFit truncates text to fit within token budget.
// Returns the truncated text and whether truncation occurred.
// Uses rune-aware truncation to avoid corrupting multi-byte UTF-8 characters.
func TruncateToFit(text string, budget int) (string, bool) {
	if FitsInBudget(text, budget) {
		return text, false
	}

	// Calculate max characters
	effectiveBudget := int(float64(budget) * SafetyMargin)
	maxChars := effectiveBudget * CharsPerToken

	// Guard very small budgets
	if maxChars <= 3 {
		return "...", true
	}

	if maxChars >= len(text) {
		return text, false
	}

	// Truncate at rune boundary to avoid corrupting UTF-8
	runes := []rune(text)
	maxRunes := maxChars - 3 // Reserve space for ellipsis
	if maxRunes >= len(runes) {
		return text, false
	}
	if maxRunes <= 0 {
		return "...", true
	}

	return string(runes[:maxRunes]) + "...", true
}

// TokenBudget represents a token budget with tracking.
type TokenBudget struct {
	Total     int
	Used      int
	Remaining int
}

// NewTokenBudget creates a new token budget.
func NewTokenBudget(total int) *TokenBudget {
	effective := int(float64(total) * SafetyMargin)
	return &TokenBudget{
		Total:     total,
		Used:      0,
		Remaining: effective,
	}
}

// Add adds tokens to the used count.
func (b *TokenBudget) Add(tokens int) {
	b.Used += tokens
	b.Remaining = int(float64(b.Total)*SafetyMargin) - b.Used
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
	b.Remaining = int(float64(b.Total) * SafetyMargin)
}

// UsagePercent returns the percentage of budget used.
func (b *TokenBudget) UsagePercent() float64 {
	if b.Total == 0 {
		return 0
	}
	return float64(b.Used) / float64(b.Total) * 100
}
