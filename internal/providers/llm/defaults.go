package llm

import "os"

// DefaultModelForProvider returns the default model for a given LLM provider.
// Environment variables can override defaults (e.g., CEREBRAS_MODEL, OPENROUTER_MODEL).
// This is the canonical source for default model selection across the codebase.
func DefaultModelForProvider(provider string) string {
	switch provider {
	case "cerebras":
		if model := os.Getenv("CEREBRAS_MODEL"); model != "" {
			return model
		}
		return "llama-3.3-70b"
	case "openrouter":
		if model := os.Getenv("OPENROUTER_MODEL"); model != "" {
			return model
		}
		return "mistralai/devstral-2512:free"
	case "groq":
		if model := os.Getenv("GROQ_MODEL"); model != "" {
			return model
		}
		return "llama-3.3-70b-versatile"
	case "gemini", "":
		if model := os.Getenv("GEMINI_MODEL"); model != "" {
			return model
		}
		return "gemini-2.0-flash"
	case "openai":
		if model := os.Getenv("OPENAI_MODEL"); model != "" {
			return model
		}
		return "gpt-4o-mini"
	case "anthropic":
		if model := os.Getenv("ANTHROPIC_MODEL"); model != "" {
			return model
		}
		return "claude-3-5-haiku-20241022"
	default:
		// Default to gemini for unknown providers
		return "gemini-2.0-flash"
	}
}
