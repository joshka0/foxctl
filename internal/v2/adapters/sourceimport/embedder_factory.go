package sourceimport

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	EmbeddingProviderHash         = "hash"
	EmbeddingProviderLMStudio     = "lmstudio"
	EmbeddingProviderOpenAICompat = "openai_compat"
	EmbeddingProviderVoyage       = "voyage"
)

// EmbedderConfig controls provider selection and override values.
type EmbedderConfig struct {
	Provider   string
	Model      string
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	Dimensions int
	EnvLookup  func(string) string
}

// ResolvedEmbedderConfig is a normalized embedder configuration with defaults applied.
type ResolvedEmbedderConfig struct {
	Provider   string
	Model      string
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	Dimensions int
}

// ResolveEmbedderConfig applies provider defaults and environment fallbacks.
func ResolveEmbedderConfig(cfg EmbedderConfig) (ResolvedEmbedderConfig, error) {
	env := cfg.EnvLookup
	if env == nil {
		env = os.Getenv
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = EmbeddingProviderHash
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultOpenAICompatTimeout
	}

	out := ResolvedEmbedderConfig{
		Provider: provider,
		Model:    strings.TrimSpace(cfg.Model),
		BaseURL:  strings.TrimSpace(cfg.BaseURL),
		APIKey:   strings.TrimSpace(cfg.APIKey),
		Timeout:  timeout,
	}

	switch provider {
	case EmbeddingProviderHash:
		if out.Model == "" {
			out.Model = hashEmbeddingModel
		}
		if cfg.Dimensions > 0 {
			out.Dimensions = cfg.Dimensions
		} else {
			out.Dimensions = defaultEmbeddingDims
		}
	case EmbeddingProviderLMStudio, EmbeddingProviderOpenAICompat:
		if out.Model == "" {
			out.Model = strings.TrimSpace(env("AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL"))
		}
		if out.Model == "" {
			out.Model = strings.TrimSpace(env("LMSTUDIO_EMBEDDING_MODEL"))
		}
		if out.Model == "" {
			out.Model = "text-embedding-nomic-embed-text-v1.5"
		}
		if out.BaseURL == "" {
			out.BaseURL = strings.TrimSpace(env("AGENTCTL_OPENAI_COMPAT_BASE_URL"))
		}
		if out.BaseURL == "" {
			out.BaseURL = strings.TrimSpace(env("LMSTUDIO_BASE_URL"))
		}
		if out.APIKey == "" {
			out.APIKey = strings.TrimSpace(env("AGENTCTL_OPENAI_COMPAT_API_KEY"))
		}
		if out.APIKey == "" {
			out.APIKey = strings.TrimSpace(env("LMSTUDIO_API_KEY"))
		}
		out.Provider = EmbeddingProviderOpenAICompat
	case EmbeddingProviderVoyage:
		if out.Model == "" {
			out.Model = strings.TrimSpace(env("VOYAGE_EMBEDDING_MODEL"))
		}
		if out.Model == "" {
			out.Model = strings.TrimSpace(env("AGENTCTL_EMBEDDING_MODEL_TEXT"))
		}
		if out.Model == "" {
			out.Model = "voyage-4"
		}
		if out.BaseURL == "" {
			out.BaseURL = strings.TrimSpace(env("VOYAGE_BASE_URL"))
		}
		if out.APIKey == "" {
			out.APIKey = strings.TrimSpace(env("VOYAGE_API_KEY"))
		}
	default:
		return ResolvedEmbedderConfig{}, fmt.Errorf("embedding provider must be one of: %s, %s, %s, %s",
			EmbeddingProviderHash, EmbeddingProviderLMStudio, EmbeddingProviderOpenAICompat, EmbeddingProviderVoyage)
	}

	return out, nil
}

// NewEmbedderFromConfig resolves and constructs an embedder.
func NewEmbedderFromConfig(cfg EmbedderConfig) (Embedder, ResolvedEmbedderConfig, error) {
	resolved, err := ResolveEmbedderConfig(cfg)
	if err != nil {
		return nil, ResolvedEmbedderConfig{}, err
	}

	embedder, err := NewEmbedderFromResolvedConfig(resolved)
	if err != nil {
		return nil, ResolvedEmbedderConfig{}, err
	}
	return embedder, resolved, nil
}

// NewEmbedderFromResolvedConfig constructs an embedder from normalized config.
func NewEmbedderFromResolvedConfig(cfg ResolvedEmbedderConfig) (Embedder, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case EmbeddingProviderHash:
		return NewHashEmbedder(cfg.Dimensions), nil
	case EmbeddingProviderLMStudio, EmbeddingProviderOpenAICompat:
		return NewOpenAICompatEmbedder(cfg.BaseURL, cfg.Model, cfg.APIKey, cfg.Timeout)
	case EmbeddingProviderVoyage:
		return NewVoyageEmbedder(cfg.BaseURL, cfg.Model, cfg.APIKey, cfg.Timeout)
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.Provider)
	}
}

// DeclaredEmbedderDimensions returns a static dimensions value when available.
func DeclaredEmbedderDimensions(embedder Embedder) int {
	if dimensional, ok := embedder.(DimensionalEmbedder); ok {
		return dimensional.Dimensions()
	}
	return 0
}

// ProbeEmbedderDimensions probes one embedding call to infer vector width and model.
func ProbeEmbedderDimensions(ctx context.Context, embedder Embedder, timeout time.Duration) (dims int, model string, err error) {
	if embedder == nil {
		return 0, "", nil
	}
	if timeout <= 0 {
		timeout = defaultOpenAICompatTimeout
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := embedder.Embed(probeCtx, "agentctl embedding dimension probe")
	if err != nil {
		return 0, "", err
	}
	return len(res.Vector), strings.TrimSpace(res.Model), nil
}
