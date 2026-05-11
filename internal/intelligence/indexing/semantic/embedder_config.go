package semantic

import (
	"strings"

	"github.com/joshka0/foxctl/internal/platform/config"
)

// DetectProviderForConfig resolves the effective embedding provider for a repo/workspace config.
// Priority: explicit provider > model family > base URL/API key shape > Gemini key > local default.
func DetectProviderForConfig(cfg config.Config, geminiKey string) string {
	provider := normalizeEmbeddingProviderName(cfg.Embedding.Provider)
	if provider != "" {
		return provider
	}
	if inferred := providerFromModel(ResolveModelForScope(ScopeDefault, cfg)); inferred != "" {
		return inferred
	}
	if strings.TrimSpace(cfg.Embedding.BaseURL) != "" || strings.TrimSpace(cfg.Embedding.APIKey) != "" {
		return "openai_compat"
	}
	if strings.TrimSpace(geminiKey) != "" {
		return "gemini"
	}
	return DefaultEmbeddingProvider
}

// ResolveModelForScope returns the configured model for a scope.
// Priority: per-scope override > global model > scope recommendation.
func ResolveModelForScope(scope EmbeddingScope, cfg config.Config) string {
	model := ResolveModelOverride(scope, cfg.Embedding.Model, cfg.Embedding.Models)
	if strings.TrimSpace(model) == "" {
		model, _ = ScopeModelRecommendation(scope)
	}
	return strings.TrimSpace(model)
}

// NewEmbedderFromConfig creates an Embedder using config-based model/provider selection.
func NewEmbedderFromConfig(scope EmbeddingScope, cfg config.Config, opts ...EmbedderOption) (*Embedder, error) {
	model := ResolveModelForScope(scope, cfg)
	configOpts := make([]EmbedderOption, 0, len(opts)+6)
	if strings.TrimSpace(model) != "" {
		configOpts = append(configOpts, WithModelOverride(model))
	}
	configOpts = append(configOpts,
		WithProvider(strings.TrimSpace(cfg.Embedding.Provider)),
		WithAPIKey(strings.TrimSpace(cfg.Embedding.APIKey)),
		WithBaseURL(strings.TrimSpace(cfg.Embedding.BaseURL)),
		WithGeminiKey(strings.TrimSpace(cfg.LLM.GeminiAPIKey)),
	)
	configOpts = applyProviderPreference(model, cfg, configOpts)
	configOpts = append(configOpts, opts...)
	return NewEmbedder(scope, configOpts...)
}

// NewProviderForScope creates an EmbeddingProvider using config-based model selection.
func NewProviderForScope(scope EmbeddingScope, cfg config.Config, opts ...EmbedderOption) (EmbeddingProvider, error) {
	embedder, err := NewEmbedderFromConfig(scope, cfg, opts...)
	if err != nil {
		return nil, err
	}
	return embedder.provider, nil
}

// NewProviderForModel creates an EmbeddingProvider for an explicit model.
func NewProviderForModel(model string, cfg config.Config, opts ...EmbedderOption) (EmbeddingProvider, error) {
	if strings.TrimSpace(model) == "" {
		model = ResolveModelForScope(ScopeDefault, cfg)
	}
	if strings.TrimSpace(model) != "" {
		opts = append(opts, WithModelOverride(model))
	}
	opts = applyProviderPreference(model, cfg, opts)
	embedder, err := NewEmbedder(ScopeDefault, opts...)
	if err != nil {
		return nil, err
	}
	return embedder.provider, nil
}

func applyProviderPreference(model string, cfg config.Config, opts []EmbedderOption) []EmbedderOption {
	provider := providerFromModel(model)
	if provider == "" {
		provider = normalizeEmbeddingProviderName(cfg.Embedding.Provider)
	}
	if provider == "" && strings.TrimSpace(cfg.Embedding.BaseURL) != "" {
		provider = "openai_compat"
	}
	if provider != "" {
		opts = append(opts, WithProvider(provider))
	}
	switch provider {
	case "gemini":
	case "openai_compat":
		opts = append(opts, WithGeminiKey(""))
	}
	return opts
}

func providerFromModel(model string) string {
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

func normalizeEmbeddingProviderName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai_compat", "openai-compatible", "lmstudio":
		return "openai_compat"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}
