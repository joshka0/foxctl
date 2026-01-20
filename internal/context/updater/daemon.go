package updater

import (
	"context"
	"log/slog"

	"github.com/jkatigb/agentctl/internal/planning/llm"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// DaemonConfig provides dependencies for the context updater worker.
type DaemonConfig struct {
	// Config is the full agentctl configuration
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
func NewWorkerFromConfig(cfg DaemonConfig) (*Worker, error) {
	// Build LLM provider config from platform config
	providerCfg := llm.ProviderConfig{
		CerebrasAPIKey:   cfg.Config.LLM.CerebrasAPIKey,
		OpenRouterAPIKey: cfg.Config.LLM.OpenRouterAPIKey,
		OpenRouterModel:  cfg.Config.LLM.OpenRouterModel,
		GroqAPIKey:       cfg.Config.LLM.GroqAPIKey,
		OpenAIAPIKey:     cfg.Config.LLM.OpenAIAPIKey,
	}

	// Check if any cheap LLM provider is available
	if !llm.IsLLMPlanningAvailableFromConfig(providerCfg) {
		return nil, nil // No LLM available - skip silently
	}

	// Determine API key and provider to use
	var apiKey, provider, model string
	switch {
	case providerCfg.CerebrasAPIKey != "":
		apiKey = providerCfg.CerebrasAPIKey
		provider = "cerebras"
		model = "llama3.1-8b"
	case providerCfg.OpenRouterAPIKey != "":
		apiKey = providerCfg.OpenRouterAPIKey
		provider = "openrouter"
		model = "meta-llama/llama-3.1-8b-instruct"
	case providerCfg.GroqAPIKey != "":
		apiKey = providerCfg.GroqAPIKey
		provider = "groq"
		model = "llama-3.1-8b-instant"
	case providerCfg.OpenAIAPIKey != "":
		apiKey = providerCfg.OpenAIAPIKey
		provider = "openai"
		model = "gpt-4o-mini"
	default:
		return nil, nil
	}

	// Create analyzer with direct API key access
	workerConfig := DefaultConfig().
		WithStorageRoot(cfg.Config.Storage.Root).
		WithLLMProvider(provider).
		WithLLMModel(model)

	analyzer := NewAnalyzerWithAPIKey(provider, apiKey, model, workerConfig.LLMTimeout)

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
	return cfg.LLM.CerebrasAPIKey != "" ||
		cfg.LLM.OpenRouterAPIKey != "" ||
		cfg.LLM.GroqAPIKey != "" ||
		cfg.LLM.OpenAIAPIKey != ""
}

// NoOpSessionProvider is a session provider that returns nothing (for testing).
type NoOpSessionProvider struct{}

func (NoOpSessionProvider) ActiveSessions(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (NoOpSessionProvider) RecentTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
	return nil, nil
}

func (NoOpSessionProvider) LastTurnID(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

// NoOpFinder is a finder that returns nothing (for testing).
type NoOpFinder struct{}

func (NoOpFinder) FindContext(ctx context.Context, analysis *AnalysisResult, sessionID string) ([]ContextCandidate, error) {
	return nil, nil
}
