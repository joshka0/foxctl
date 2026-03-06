package llm

import "context"

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
// Priority: explicit provider override, else local-first LM Studio default.
// This function does NOT read environment variables - config must be pre-populated.
func AutoPlannerFromConfig(ctx context.Context, cfg ProviderConfig) *OpenAIPlanner {
	if ctx == nil {
		ctx = context.Background()
	}
	// Explicit provider override.
	switch cfg.Provider {
	case "lmstudio":
		return NewOpenAIPlanner(ctx, OpenAIConfig{
			APIKey:   "lm-studio",
			BaseURL:  "http://localhost:1234/v1",
			Model:    cfg.Model,
			Provider: "lmstudio",
		})
	case "cerebras":
		if cfg.CerebrasAPIKey != "" {
			return NewOpenAIPlanner(ctx, OpenAIConfig{
				APIKey:   cfg.CerebrasAPIKey,
				BaseURL:  "https://api.cerebras.ai/v1",
				Model:    cfg.Model,
				Provider: "cerebras",
			})
		}
	case "openrouter":
		if cfg.OpenRouterAPIKey != "" {
			model := cfg.OpenRouterModel
			if model == "" {
				model = cfg.Model
			}
			if model == "" {
				model = "mistralai/devstral-2512"
			}
			return NewOpenAIPlanner(ctx, OpenAIConfig{
				APIKey:   cfg.OpenRouterAPIKey,
				BaseURL:  "https://openrouter.ai/api/v1",
				Model:    model,
				Provider: "openrouter",
			})
		}
	case "groq":
		if cfg.GroqAPIKey != "" {
			return NewOpenAIPlanner(ctx, OpenAIConfig{
				APIKey:   cfg.GroqAPIKey,
				BaseURL:  "https://api.groq.com/openai/v1",
				Model:    cfg.Model,
				Provider: "groq",
			})
		}
	case "openai":
		if cfg.OpenAIAPIKey != "" {
			return NewOpenAIPlanner(ctx, OpenAIConfig{
				APIKey:   cfg.OpenAIAPIKey,
				BaseURL:  "https://api.openai.com/v1",
				Model:    cfg.Model,
				Provider: "openai",
			})
		}
	}

	// Local-first default.
	return NewOpenAIPlanner(ctx, OpenAIConfig{
		APIKey:   "lm-studio",
		BaseURL:  "http://localhost:1234/v1",
		Model:    cfg.Model,
		Provider: "lmstudio",
	})
}

// IsLLMPlanningAvailableFromConfig returns true if LLM-based planning is available.
// This function does NOT read environment variables - config must be pre-populated.
func IsLLMPlanningAvailableFromConfig(cfg ProviderConfig) bool {
	return cfg.Provider == "lmstudio" ||
		cfg.CerebrasAPIKey != "" ||
		cfg.OpenRouterAPIKey != "" ||
		cfg.GroqAPIKey != "" ||
		cfg.OpenAIAPIKey != "" ||
		cfg.Provider == ""
}
