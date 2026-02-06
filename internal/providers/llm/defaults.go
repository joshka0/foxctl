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
		return "llama-4-scout-17b-16e"
	case "openrouter":
		if model := os.Getenv("OPENROUTER_MODEL"); model != "" {
			return model
		}
		return "mistralai/devstral-2512:free"
	case "groq":
		if model := os.Getenv("GROQ_MODEL"); model != "" {
			return model
		}
		return "llama-4-scout-17b-16e"
	case "gemini", "":
		if model := os.Getenv("GEMINI_MODEL"); model != "" {
			return model
		}
		return "gemini-2.5-flash"
	case "openai":
		if model := os.Getenv("OPENAI_MODEL"); model != "" {
			return model
		}
		return "gpt-4.1-mini"
	case "anthropic":
		if model := os.Getenv("ANTHROPIC_MODEL"); model != "" {
			return model
		}
		return "claude-haiku-4-5"
	case "lmstudio":
		if model := os.Getenv("LMSTUDIO_MODEL"); model != "" {
			return model
		}
		return "zai-org/glm-4.7-flash"
	default:
		// Default to gemini for unknown providers
		return "gemini-2.5-flash"
	}
}
