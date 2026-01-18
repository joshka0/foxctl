package llm

// ProviderConfig holds pre-loaded LLM provider configuration.
// This struct is populated at application startup from environment/config files
// and passed to planning functions (FC/IS compliant - no os.Getenv in core).
type ProviderConfig struct {
	// Provider is the preferred provider: openrouter, groq, openai
	Provider string
	// Model is the model to use
	Model string
	// APIKey is the generic API key (used if provider-specific key is empty)
	APIKey string
	// OpenRouterAPIKey is the OpenRouter API key
	OpenRouterAPIKey string
	// OpenRouterModel is the model for OpenRouter
	OpenRouterModel string
	// GroqAPIKey is the Groq API key
	GroqAPIKey string
	// OpenAIAPIKey is the OpenAI API key
	OpenAIAPIKey string
}

// AutoPlannerFromConfig returns the best available LLM planner based on config.
// Prefers OpenRouter, then Groq, then OpenAI based on which API keys are present.
// Returns nil if no supported API keys are configured.
// This function does NOT read environment variables - config must be pre-populated.
func AutoPlannerFromConfig(cfg ProviderConfig) *OpenAIPlanner {
	// Prefer OpenRouter if configured
	if cfg.OpenRouterAPIKey != "" {
		model := cfg.OpenRouterModel
		if model == "" {
			model = "openrouter/auto"
		}
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:   cfg.OpenRouterAPIKey,
			BaseURL:  "https://openrouter.ai/api/v1",
			Model:    model,
			Provider: "openrouter",
		})
	}

	// Next, check for Groq
	if cfg.GroqAPIKey != "" {
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:   cfg.GroqAPIKey,
			BaseURL:  "https://api.groq.com/openai/v1",
			Model:    "llama-3.3-70b-versatile",
			Provider: "groq",
		})
	}

	// Finally, check for OpenAI
	if cfg.OpenAIAPIKey != "" {
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:   cfg.OpenAIAPIKey,
			BaseURL:  "https://api.openai.com/v1",
			Model:    "gpt-4o-mini",
			Provider: "openai",
		})
	}

	// No API key available
	return nil
}

// IsLLMPlanningAvailableFromConfig returns true if LLM-based planning is available.
// This function does NOT read environment variables - config must be pre-populated.
func IsLLMPlanningAvailableFromConfig(cfg ProviderConfig) bool {
	return cfg.OpenRouterAPIKey != "" || cfg.GroqAPIKey != "" || cfg.OpenAIAPIKey != ""
}
