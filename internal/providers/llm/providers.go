package llm

import (
	"os"
	"os/exec"
	"strings"
)

// Provider describes a single LLM backend configuration.
type Provider struct {
	Name      string
	Endpoint  string
	APIKey    string
	Model     string
	IsCLI     bool
	MaxTokens int
}

// SummarizationProviders returns providers for session summarization in priority order.
// Priority: Cerebras -> Groq -> OpenRouter -> CLI fallback (Gemini, Claude).
func SummarizationProviders() []Provider {
	var providers []Provider
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		envOrDefault("CEREBRAS_MODEL", "llama-3.3-70b"),
		8000,
	)
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		envOrDefault("GROQ_MODEL", "llama-3.3-70b-versatile"),
		10000,
	)
	providers = appendOpenRouterProviders(
		providers,
		envOrDefault("OPENROUTER_MODELS", "minimax/minimax-m2.1"),
		24000,
	)
	providers = appendCLIProvider(
		providers,
		"gemini",
		"gemini-cli",
		envOrDefault("GEMINI_MODEL", "gemini-2.5-flash"),
		100000,
	)
	providers = appendCLIProvider(
		providers,
		"claude",
		"claude-cli",
		envOrDefault("CLAUDE_MODEL", "claude-haiku-4-5"),
		50000,
	)
	return providers
}

// ExtractionProviders returns providers for session learnings extraction.
// Priority: Cerebras -> Groq -> OpenRouter -> Gemini CLI.
func ExtractionProviders() []Provider {
	var providers []Provider
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		"llama-3.3-70b",
		8000,
	)
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		envOrDefault("GROQ_MODEL", "llama-3.3-70b-versatile"),
		10000,
	)
	providers = appendOpenRouterProviders(
		providers,
		"anthropic/claude-3-haiku",
		24000,
	)
	providers = appendCLIProvider(
		providers,
		"gemini",
		"gemini-cli",
		"gemini-2.5-flash",
		100000,
	)
	return providers
}

// SynthesisProviders returns providers for code semantic search synthesis.
// Priority: OpenRouter -> Groq -> Cerebras.
func SynthesisProviders(modelOverride string) []Provider {
	var providers []Provider
	openRouterModel := strings.TrimSpace(modelOverride)
	if openRouterModel == "" {
		openRouterModel = envOrDefault("OPENROUTER_MODELS", "mistralai/devstral-2512:free")
	}
	providers = appendOpenRouterProviders(providers, openRouterModel, 0)

	groqModel := strings.TrimSpace(modelOverride)
	if groqModel == "" {
		groqModel = "llama-3.3-70b-versatile"
	}
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		groqModel,
		0,
	)

	cerebrasModel := strings.TrimSpace(modelOverride)
	if cerebrasModel == "" {
		cerebrasModel = "llama-3.3-70b"
	}
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		cerebrasModel,
		0,
	)
	return providers
}

func appendAPIProvider(providers []Provider, keyEnv, name, endpoint, model string, maxTokens int) []Provider {
	key := strings.TrimSpace(os.Getenv(keyEnv))
	if key == "" {
		return providers
	}
	return append(providers, Provider{
		Name:      name,
		Endpoint:  endpoint,
		APIKey:    key,
		Model:     model,
		MaxTokens: maxTokens,
	})
}

func appendOpenRouterProviders(providers []Provider, models string, maxTokens int) []Provider {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		return providers
	}
	for _, model := range strings.Split(models, ",") {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		providers = append(providers, Provider{
			Name:      "openrouter:" + model,
			Endpoint:  "https://openrouter.ai/api/v1/chat/completions",
			APIKey:    key,
			Model:     model,
			MaxTokens: maxTokens,
		})
	}
	return providers
}

func appendCLIProvider(providers []Provider, toolName, name, model string, maxTokens int) []Provider {
	if !hasTool(toolName) {
		return providers
	}
	return append(providers, Provider{
		Name:      name,
		Model:     model,
		IsCLI:     true,
		MaxTokens: maxTokens,
	})
}

func hasTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func envOrDefault(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}
