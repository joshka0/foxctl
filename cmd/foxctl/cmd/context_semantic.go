package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	sourceimport "github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

func openObsidianSemanticProvider(cfg config.Config) semantic.EmbeddingProvider {
	if !obsidianSemanticEnabled(cfg) {
		return nil
	}
	if provider := openOpenAICompatSemanticProvider(cfg); provider != nil {
		return provider
	}
	provider, err := semantic.NewProviderForScope(
		semantic.ScopeMemory,
		cfg,
		semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
		semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
	)
	if err != nil {
		return nil
	}
	return provider
}

func obsidianSemanticEnabled(cfg config.Config) bool {
	if value, ok := lookupEnvBool("AGENTCTL_OBSIDIAN_SEMANTIC_ENABLED"); ok {
		return value
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_BASE_URL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_MODEL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_BASE_URL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL")) != "" {
		return true
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Embedding.Provider))
	return provider == "lmstudio" || provider == "openai_compat" || provider == "openai-compatible"
}

func lookupEnvBool(name string) (bool, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false, false
	}
	if value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || strings.EqualFold(value, "on") {
		return true, true
	}
	if value == "0" || strings.EqualFold(value, "false") || strings.EqualFold(value, "no") || strings.EqualFold(value, "off") {
		return false, true
	}
	return false, false
}

func openOpenAICompatSemanticProvider(cfg config.Config) semantic.EmbeddingProvider {
	providerName := strings.ToLower(strings.TrimSpace(cfg.Embedding.Provider))
	override := strings.ToLower(strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_SEMANTIC_PROVIDER")))
	if override != "" {
		providerName = override
	}
	if providerName != "lmstudio" && providerName != "openai_compat" && providerName != "openai-compatible" {
		return nil
	}
	embedder, resolved, err := sourceimport.NewEmbedderFromConfig(sourceimport.EmbedderConfig{
		Provider: providerNameToSourceImport(providerName),
		Model: firstNonEmptySemantic(
			strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_MODEL")),
			strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL")),
			cfg.Embedding.Model,
		),
		BaseURL: firstNonEmptySemantic(
			strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_BASE_URL")),
			strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_BASE_URL")),
			cfg.Embedding.BaseURL,
		),
		APIKey: firstNonEmptySemantic(
			strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_API_KEY")),
			strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_API_KEY")),
			cfg.Embedding.APIKey,
		),
	})
	if err != nil {
		return nil
	}
	return &openAICompatSemanticProvider{
		inner: embedder,
		model: resolved.Model,
		dims:  resolved.Dimensions,
	}
}

func providerNameToSourceImport(name string) string {
	switch name {
	case "openai_compat", "openai-compatible":
		return sourceimport.EmbeddingProviderOpenAICompat
	default:
		return name
	}
}

type openAICompatSemanticProvider struct {
	inner sourceimport.Embedder
	model string
	dims  int
}

func (p *openAICompatSemanticProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	res, err := p.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return res.Vector, nil
}

func (p *openAICompatSemanticProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

func (p *openAICompatSemanticProvider) Model() string {
	return strings.TrimSpace(p.model)
}

func (p *openAICompatSemanticProvider) Dimensions() int {
	return p.dims
}

func firstNonEmptySemantic(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
