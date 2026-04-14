package updater

import (
	"context"
	"log/slog"

	"github.com/joshka0/foxctl/internal/platform/config"
	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
)

// DaemonConfig provides dependencies for the context updater worker.
type DaemonConfig struct {
	// Config is the full foxctl configuration
	Config config.Config

	// Logger is the logger to use
	Logger *slog.Logger

	// SessionProvider provides session access
	Sessions SessionProvider

	// Finder finds relevant context
	Finder ContextFinder

	// Injector delivers context to sessions
	Injector ContextInjector
}

// NewWorkerFromConfig creates a context updater worker configured from daemon settings.
// Returns nil if LLM providers are not available.
func NewWorkerFromConfig(ctx context.Context, cfg DaemonConfig) (*Worker, error) {
	// Determine API key and provider to use.
	var apiKey, provider, model string
	provider = cfg.Config.LLM.Provider
	if provider == "" {
		provider = "lmstudio"
	}
	switch provider {
	case "lmstudio":
		apiKey = cfg.Config.LLM.ResolveAPIKey(provider)
		model = cfg.Config.LLM.ResolveModel(provider)
		if model == "" {
			model = llmproviders.DefaultModelForProvider(provider)
		}
	case "openrouter":
		apiKey = cfg.Config.LLM.ResolveAPIKey(provider)
		model = cfg.Config.LLM.ResolveModel(provider)
		if model == "" {
			model = "qwen/qwen3-coder-next"
		}
	case "cerebras":
		apiKey = cfg.Config.LLM.ResolveAPIKey(provider)
		model = cfg.Config.LLM.ResolveModel(provider)
		if model == "" {
			model = "llama3.1-8b"
		}
	case "groq":
		apiKey = cfg.Config.LLM.ResolveAPIKey(provider)
		model = cfg.Config.LLM.ResolveModel(provider)
		if model == "" {
			model = "llama-3.1-8b-instant"
		}
	case "openai":
		apiKey = cfg.Config.LLM.ResolveAPIKey(provider)
		model = cfg.Config.LLM.ResolveModel(provider)
		if model == "" {
			model = "gpt-4o-mini"
		}
	default:
		return nil, nil // Unknown provider.
	}
	if apiKey == "" {
		return nil, nil
	}

	// Create analyzer with direct API key access
	workerConfig := DefaultConfig().
		WithStorageRoot(cfg.Config.Storage.Root).
		WithLLMProvider(provider).
		WithLLMModel(model)

	analyzer := NewAnalyzerWithAPIKey(ctx, provider, apiKey, model, workerConfig.LLMTimeout)

	// Create worker
	worker := NewWorker(
		workerConfig,
		analyzer,
		cfg.Sessions,
		cfg.Finder,
		cfg.Injector,
		cfg.Logger,
	)

	return worker, nil
}

// Available returns true if cheap LLM providers are configured.
func Available(cfg config.Config) bool {
	if cfg.LLM.Provider == "" || cfg.LLM.Provider == "lmstudio" {
		return true
	}
	return cfg.LLM.CerebrasAPIKey != "" ||
		cfg.LLM.OpenRouterAPIKey != "" ||
		cfg.LLM.GroqAPIKey != "" ||
		cfg.LLM.OpenAIAPIKey != ""
}

// NoOpSessionProvider is a SessionProvider that returns empty results (for testing).
type NoOpSessionProvider struct{}

// ActiveSessions returns nil (no active sessions).
func (NoOpSessionProvider) ActiveSessions(ctx context.Context) ([]string, error) {
	return nil, nil
}

// RecentTurns returns nil (no turns).
func (NoOpSessionProvider) RecentTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
	return nil, nil
}

// LastTurnID returns an empty string (no turns).
func (NoOpSessionProvider) LastTurnID(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

// GetSessionWorkspace returns an empty string (no workspace).
func (NoOpSessionProvider) GetSessionWorkspace(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

// NoOpFinder is a ContextFinder that returns no candidates (for testing).
type NoOpFinder struct{}

// FindContext returns nil (no context candidates).
func (NoOpFinder) FindContext(ctx context.Context, analysis *AnalysisResult, sessionID, workspace string) ([]ContextCandidate, error) {
	return nil, nil
}
