package llm

import "strings"

// ModelLimits records provider-published model budget limits that foxctl needs
// for routing and eval planning. A zero MaxOutputTokens means the output limit
// is not encoded here.
type ModelLimits struct {
	ContextTokens   int
	MaxOutputTokens int
}

const (
	Context1M   = 1_000_000
	Context256K = 256_000
	Context200K = 200_000
	Context128K = 128_000
)

// LimitsForModel returns known context/output limits for current non-OpenAI
// provider models used in long-memory and agent-eval workflows.
func LimitsForModel(provider, model string) (ModelLimits, bool) {
	normalized := normalizeModelLimitID(provider, model)
	if normalized == "" {
		return ModelLimits{}, false
	}

	switch {
	case isGLM52Model(normalized):
		return ModelLimits{ContextTokens: Context1M, MaxOutputTokens: Context128K}, true
	case isGLM5Model(normalized):
		return ModelLimits{ContextTokens: Context200K, MaxOutputTokens: Context128K}, true
	case isMiniMaxM3Model(normalized):
		return ModelLimits{ContextTokens: Context1M}, true
	case isMiniMaxM1Model(normalized):
		return ModelLimits{ContextTokens: Context1M}, true
	case isKimiK2CurrentModel(normalized):
		return ModelLimits{ContextTokens: Context256K}, true
	case isMoonshotV1128KModel(normalized):
		return ModelLimits{ContextTokens: Context128K}, true
	default:
		return ModelLimits{}, false
	}
}

// ContextTokensForModel returns the known context limit for provider/model, or
// zero when foxctl does not have provider metadata for the model.
func ContextTokensForModel(provider, model string) int {
	limits, ok := LimitsForModel(provider, model)
	if !ok {
		return 0
	}
	return limits.ContextTokens
}

func normalizeModelLimitID(provider, model string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	model = strings.TrimPrefix(model, "models/")
	model = strings.TrimPrefix(model, "openrouter/")
	if provider != "" && model != "" {
		model = strings.TrimPrefix(model, provider+":")
	}
	return model
}

func isGLM52Model(model string) bool {
	model = stripModelNamespace(model)
	return model == "glm-5.2" || model == "glm-5.2[1m]"
}

func isGLM5Model(model string) bool {
	model = stripModelNamespace(model)
	switch model {
	case "glm-5.1", "glm-5", "glm-5-turbo":
		return true
	default:
		return false
	}
}

func isMiniMaxM3Model(model string) bool {
	model = stripModelNamespace(model)
	return model == "minimax-m3"
}

func isMiniMaxM1Model(model string) bool {
	model = stripModelNamespace(model)
	return model == "minimax-m1"
}

func isKimiK2CurrentModel(model string) bool {
	model = stripModelNamespace(model)
	switch model {
	case "kimi-k2.7-code", "kimi-k2.6", "kimi-k2.5":
		return true
	default:
		return false
	}
}

func isMoonshotV1128KModel(model string) bool {
	model = stripModelNamespace(model)
	return model == "moonshot-v1-128k"
}

func stripModelNamespace(model string) string {
	for _, prefix := range []string{
		"z-ai/",
		"zai-org/",
		"zhipuai/",
		"minimax/",
		"moonshotai/",
		"moonshot/",
	} {
		model = strings.TrimPrefix(model, prefix)
	}
	return model
}
