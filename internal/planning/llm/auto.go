package llm

import "os"

// AutoPlanner returns the best available LLM planner based on environment.
// Prefers OpenRouter (OPENROUTER_API_KEY), then Groq (GROQ_API_KEY), then OpenAI (OPENAI_API_KEY).
// Returns nil if no supported API keys are configured.
func AutoPlanner() *OpenAIPlanner {
	// Prefer OpenRouter if configured
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		model := os.Getenv("OPENROUTER_MODEL_NAME")
		if model == "" {
			model = "openrouter/auto"
		}
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:  os.Getenv("OPENROUTER_API_KEY"),
			BaseURL: "https://openrouter.ai/api/v1",
			Model:   model,
		})
	}

	// Next, check for Groq
	if os.Getenv("GROQ_API_KEY") != "" {
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:  os.Getenv("GROQ_API_KEY"),
			BaseURL: "https://api.groq.com/openai/v1",
			Model:   "llama-3.3-70b-versatile",
		})
	}

	// Finally, check for OpenAI
	if os.Getenv("OPENAI_API_KEY") != "" {
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:  os.Getenv("OPENAI_API_KEY"),
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		})
	}

	// No API key available
	return nil
}

// IsLLMPlanningAvailable returns true if LLM-based planning is available.
func IsLLMPlanningAvailable() bool {
	return os.Getenv("OPENROUTER_API_KEY") != "" ||
		os.Getenv("GROQ_API_KEY") != "" ||
		os.Getenv("OPENAI_API_KEY") != ""
}
