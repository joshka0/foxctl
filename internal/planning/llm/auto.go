package llm

import "os"

// AutoPlanner returns the best available LLM planner based on environment.
// Checks for GROQ_API_KEY, OPENAI_API_KEY in that order.
// Returns nil if no API keys are configured.
func AutoPlanner() *OpenAIPlanner {
	// Check for Groq
	if os.Getenv("GROQ_API_KEY") != "" {
		return NewOpenAIPlanner(OpenAIConfig{
			APIKey:  os.Getenv("GROQ_API_KEY"),
			BaseURL: "https://api.groq.com/openai/v1",
			Model:   "llama-3.3-70b-versatile",
		})
	}

	// Check for OpenAI
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
	return os.Getenv("GROQ_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != ""
}
