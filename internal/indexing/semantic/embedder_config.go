package semantic

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

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
	if strings.TrimSpace(model) != "" {
		opts = append(opts, WithModelOverride(model))
	}
	opts = applyProviderPreference(model, cfg, opts)
	return NewEmbedder(scope, opts...)
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
		provider = strings.ToLower(strings.TrimSpace(cfg.Embedding.Provider))
	}
	switch provider {
	case "gemini":
		opts = append(opts, WithVoyageKey(""))
	case "voyage":
		// Default selection already prefers Voyage when configured.
	}
	return opts
}

func providerFromModel(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(lower, "gemini-"):
		return "gemini"
	case strings.HasPrefix(lower, "voyage-"):
		return "voyage"
	default:
		return ""
	}
}
