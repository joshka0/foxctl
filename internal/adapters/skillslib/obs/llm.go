// Package obs provides LLM token and cost tracking for observability.
package obs

import (
	"encoding/json"
	"os"
	"sync"
)

// TokenUsage tracks LLM token consumption and costs.
type TokenUsage struct {
	Model         string  `json:"model"`
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	TotalTokens   int     `json:"total_tokens"`
	InputCostUSD  float64 `json:"input_cost_usd"`
	OutputCostUSD float64 `json:"output_cost_usd"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
}

// ModelPricing defines token pricing for a model (per million tokens in USD).
type ModelPricing struct {
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

// Data key constants for LLM observability
const (
	KeyLLMModel         = "llm_model"
	KeyLLMInputTokens   = "llm_input_tokens"
	KeyLLMOutputTokens  = "llm_output_tokens"
	KeyLLMTotalTokens   = "llm_total_tokens"
	KeyLLMInputCostUSD  = "llm_input_cost_usd"
	KeyLLMOutputCostUSD = "llm_output_cost_usd"
	KeyLLMTotalCostUSD  = "llm_total_cost_usd"
)

// pricingCache holds loaded model pricing (lazy-loaded).
var (
	pricingCache     map[string]ModelPricing
	pricingCacheOnce sync.Once
)

// defaultPricing provides fallback pricing for common models if file not found.
var defaultPricing = map[string]ModelPricing{
	"z-ai/glm-4.7-flash":      {InputPerMillion: 0.07, OutputPerMillion: 0.40},
	"deepseek/deepseek-v3.2":  {InputPerMillion: 0.25, OutputPerMillion: 0.38},
	"google/gemini-2.5-flash": {InputPerMillion: 0.30, OutputPerMillion: 2.50},
	"openai/gpt-4o-mini":      {InputPerMillion: 0.15, OutputPerMillion: 0.60},
}

// PricingFile represents the JSON structure of the pricing file.
type PricingFile struct {
	UpdatedAt string `json:"updated_at"`
	Models    []struct {
		ID               string  `json:"id"`
		InputPerMillion  float64 `json:"input_per_million"`
		OutputPerMillion float64 `json:"output_per_million"`
		ContextLength    int     `json:"context_length"`
	} `json:"models"`
}

// LoadPricing loads model pricing from configs/openrouter_pricing.json.
// Falls back to defaults if file not found.
func LoadPricing() map[string]ModelPricing {
	pricingCacheOnce.Do(func() {
		pricingCache = make(map[string]ModelPricing)

		// Try common locations for the pricing file
		paths := []string{
			"configs/openrouter_pricing.json", // relative to cwd
		}

		// Add AGENTCTL_HOME path
		if home := os.Getenv("AGENTCTL_HOME"); home != "" {
			paths = append(paths, home+"/configs/openrouter_pricing.json")
		}

		// Add user home directory path
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, home+"/.agentctl/configs/openrouter_pricing.json")
		}

		var loaded bool
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var pf PricingFile
			if err := json.Unmarshal(data, &pf); err != nil {
				continue
			}

			for _, m := range pf.Models {
				pricingCache[m.ID] = ModelPricing{
					InputPerMillion:  m.InputPerMillion,
					OutputPerMillion: m.OutputPerMillion,
				}
			}
			loaded = true
			break
		}

		// Fall back to defaults if no file found
		if !loaded {
			for k, v := range defaultPricing {
				pricingCache[k] = v
			}
		}
	})
	return pricingCache
}

// GetModelPricing returns pricing for a model, or nil if not found.
func GetModelPricing(model string) *ModelPricing {
	pricing := LoadPricing()
	if p, ok := pricing[model]; ok {
		return &p
	}
	return nil
}

// CalculateTokenCost computes the USD cost for token usage.
func CalculateTokenCost(model string, inputTokens, outputTokens int) TokenUsage {
	usage := TokenUsage{
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
	}

	if pricing := GetModelPricing(model); pricing != nil {
		usage.InputCostUSD = float64(inputTokens) / 1_000_000 * pricing.InputPerMillion
		usage.OutputCostUSD = float64(outputTokens) / 1_000_000 * pricing.OutputPerMillion
		usage.TotalCostUSD = usage.InputCostUSD + usage.OutputCostUSD
	}

	return usage
}

// WithTokenUsage adds LLM token usage data to a span.
// Use this with StartSpan or directly on an EventBuilder.
func WithTokenUsage(usage TokenUsage) SpanOpt {
	return WithDataMap(map[string]any{
		KeyLLMModel:         usage.Model,
		KeyLLMInputTokens:   usage.InputTokens,
		KeyLLMOutputTokens:  usage.OutputTokens,
		KeyLLMTotalTokens:   usage.TotalTokens,
		KeyLLMInputCostUSD:  usage.InputCostUSD,
		KeyLLMOutputCostUSD: usage.OutputCostUSD,
		KeyLLMTotalCostUSD:  usage.TotalCostUSD,
	})
}

// WithLLMCall is a convenience function that calculates costs and returns a SpanOpt.
func WithLLMCall(model string, inputTokens, outputTokens int) SpanOpt {
	return WithTokenUsage(CalculateTokenCost(model, inputTokens, outputTokens))
}
