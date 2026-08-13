package embed

import "strings"

// NormalizeProviderName canonicalizes an embedding provider name.
// Aliases like "lmstudio" and "openai-compatible" map to "openai_compat".
func NormalizeProviderName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai_compat", "openai-compatible", "lmstudio":
		return "openai_compat"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

// ProviderFromModel infers the embedding provider from a model identifier.
// Returns an empty string when no provider can be inferred.
func ProviderFromModel(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(lower, "gemini-"):
		return "gemini"
	case strings.HasPrefix(lower, "text-embedding-"), strings.HasPrefix(lower, "nomic-embed-"), strings.Contains(lower, "embeddinggemma"):
		return "openai_compat"
	default:
		return ""
	}
}
