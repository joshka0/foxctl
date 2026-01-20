package llm

// ProviderConfig holds pre-loaded LLM provider configuration.
// This struct is populated at application startup from environment/config files
// and passed to planning functions (FC/IS compliant - no os.Getenv in core).
type ProviderConfig struct {
	// Provider is the preferred provider: cerebras, openrouter, groq, openai
	Provider string
	// Model is the model to use
	Model string
	// APIKey is the generic API key (used if provider-specific key is empty)
	APIKey string
	// CerebrasAPIKey is the Cerebras API key (preferred for background tasks - cheapest)
	CerebrasAPIKey string
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
// Priority order for background/cheap tasks: Cerebras → OpenRouter → Groq → OpenAI.
// Returns nil if no supported API keys are configured.
// This function does NOT read environment variables - config must be pre-populated.
func AutoPlannerFromConfig(cfg ProviderConfig) *OpenAIPlanner {
	// Prefer Cerebras if configured (fastest, cheapest for background tasks)
	if cfg.CerebrasAPIKey != "" {
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:   cfg.CerebrasAPIKey,
			BaseURL:  "https://api.cerebras.ai/v1",
			Model:    "llama3.1-8b", // ~$0.10/M tokens
			Provider: "cerebras",
		})
	}

	// Next, check for OpenRouter
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
	return cfg.CerebrasAPIKey != "" || cfg.OpenRouterAPIKey != "" || cfg.GroqAPIKey != "" || cfg.OpenAIAPIKey != ""
}
