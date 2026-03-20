package optimization

import "strings"

// NormalizePromptTargetProfile applies stable formatting to target-profile keys.
func NormalizePromptTargetProfile(profile string) string {
	profile = strings.ToLower(strings.TrimSpace(profile))
	profile = strings.ReplaceAll(profile, " ", "_")
	profile = strings.ReplaceAll(profile, "-", "_")
	return strings.Trim(profile, "_")
}

// DerivePromptTargetProfile returns a stable prompt-family key for a runtime target.
func DerivePromptTargetProfile(executionLayer, provider, model string) string {
	executionLayer = strings.ToLower(strings.TrimSpace(executionLayer))
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))

	switch {
	case provider == "lmstudio":
		return "local_lmstudio"
	case provider == "openrouter" && executionLayer == "jido":
		return "jido_openrouter"
	case provider == "openrouter":
		return "openrouter_remote"
	case provider == "openai" && executionLayer == "jido":
		return "jido_openai"
	case provider == "openai":
		return "openai_remote"
	case provider == "anthropic" && executionLayer == "jido":
		return "jido_anthropic"
	case provider == "anthropic":
		return "anthropic_remote"
	case strings.Contains(model, "qwen") || strings.Contains(model, "liquid/"):
		return "local_lmstudio"
	default:
		return "generic"
	}
}
