package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

const (
	// DefaultInlineOutputKB is the fallback size threshold for inline envelopes.
	DefaultInlineOutputKB = 32
	// DefaultMaxCaptureKB limits captured stdout/stderr data per job.
	DefaultMaxCaptureKB = 10240
)

// ExposePolicy controls how CAS artifacts are exposed in output envelopes.
type ExposePolicy string

const (
	// ExposePolicyOff hides CAS digests from output (store for debugging only).
	ExposePolicyOff ExposePolicy = "off"
	// ExposePolicyDigest includes the raw CAS digest in output.
	ExposePolicyDigest ExposePolicy = "digest"
	// ExposePolicyHint includes a CAS hint with retrieval commands.
	ExposePolicyHint ExposePolicy = "hint"
)

// CASPolicy controls content-addressed storage behavior.
type CASPolicy struct {
	// Store controls whether outputs are stored in CAS (default: true).
	// When true, large outputs are always stored for debugging/retrieval.
	Store bool `mapstructure:"store" json:"store"`

	// Expose controls how CAS digests appear in output envelopes.
	// Values: "off" (hidden), "digest" (raw digest), "hint" (with retrieval commands).
	// Default: "off" for hooks, "hint" for skills.
	Expose ExposePolicy `mapstructure:"expose" json:"expose"`
}

// Config represents the fully materialized runtime configuration.
type Config struct {
	Home           string            `mapstructure:"home" json:"home"`
	InlineOutputKB int               `mapstructure:"inline_output_kb" json:"inline_output_kb"`
	MaxCaptureKB   int               `mapstructure:"max_capture_kb" json:"max_capture_kb"`
	Paths          Paths             `mapstructure:"paths" json:"paths"`
	Storage        StorageSettings   `mapstructure:"storage" json:"storage"`
	Memory         MemorySettings    `mapstructure:"memory" json:"memory"`
	Cache          CacheSettings     `mapstructure:"cache" json:"cache"`
	CAS            CASPolicy         `mapstructure:"cas" json:"cas"`
	Logging        LoggingSettings   `mapstructure:"logging" json:"logging"`
	OpenAPI        OpenAPISettings   `mapstructure:"openapi" json:"openapi"`
	Embedding      EmbeddingSettings `mapstructure:"embedding" json:"embedding"`
	Search         SearchSettings    `mapstructure:"search" json:"search"`
	Indexing       IndexingSettings  `mapstructure:"indexing" json:"indexing"`
	Database       DatabaseSettings  `mapstructure:"database" json:"database"`
	LLM            LLMSettings       `mapstructure:"llm" json:"llm"`
}

// Paths include common on-disk locations rooted at the agentctl home directory.
type Paths struct {
	CAS           string `mapstructure:"cas" json:"cas"`
	Jobs          string `mapstructure:"jobs" json:"jobs"`
	Cache         string `mapstructure:"cache" json:"cache"`
	Skills        string `mapstructure:"skills" json:"skills"`
	Observability string `mapstructure:"observability" json:"observability"`
}

// StorageSettings configure persistent storage for agents, mailboxes, blackboard, and quotas.
type StorageSettings struct {
	Root string `mapstructure:"root" json:"root"`
}

// MemorySettings influence cache + named memory behavior.
type MemorySettings struct {
	AutoCacheTTL      time.Duration `mapstructure:"auto_cache_ttl" json:"auto_cache_ttl,format:units"`
	DefaultNamedTTL   time.Duration `mapstructure:"default_named_ttl" json:"default_named_ttl,format:units"`
	AutoLoadWorkspace bool          `mapstructure:"auto_load_workspace" json:"auto_load_workspace"`
}

// CacheSettings describe run-time caching defaults.
type CacheSettings struct {
	DefaultMode string `mapstructure:"default_mode" json:"default_mode"`
}

// LoggingSettings configure CLI logging behavior.
type LoggingSettings struct {
	Level  string `mapstructure:"level" json:"level"`
	Format string `mapstructure:"format" json:"format"`
}

// EmbeddingSettings configure embedding provider defaults.
type EmbeddingSettings struct {
	Provider   string            `mapstructure:"provider" json:"provider"`
	Model      string            `mapstructure:"model" json:"model"`
	Dimensions int               `mapstructure:"dimensions" json:"dimensions"`
	Models     map[string]string `mapstructure:"models" json:"models"`
	Flags      EmbeddingFlags    `mapstructure:"flags" json:"flags"`

	// VoyageAPIKey is the Voyage AI API key (from VOYAGE_API_KEY)
	// Required for embeddings with voyage-code-3, voyage-3-large, etc.
	VoyageAPIKey string `mapstructure:"voyage_api_key" json:"voyage_api_key"`
}

// SearchSettings configure web search provider API keys.
// These are used by the web_search skill and MCP server.
type SearchSettings struct {
	// TavilyAPIKey is the Tavily API key (from TAVILY_API_KEY)
	// Used for web search, extract, crawl, and map operations.
	TavilyAPIKey string `mapstructure:"tavily_api_key" json:"tavily_api_key"`

	// ExaAPIKey is the Exa API key (from EXA_API_KEY)
	// Used for code search and general web search.
	ExaAPIKey string `mapstructure:"exa_api_key" json:"exa_api_key"`

	// PerplexityAPIKey is the Perplexity API key (from PERPLEXITY_API_KEY)
	// Used for the "ask" tool for question answering.
	PerplexityAPIKey string `mapstructure:"perplexity_api_key" json:"perplexity_api_key"`
}

// DatabaseSettings configure database driver and connection.
type DatabaseSettings struct {
	// Driver specifies which database driver to use: "sqlite" (default), "libsql", or "turso"
	Driver string `mapstructure:"driver" json:"driver"`

	// Turso holds Turso-specific configuration (when driver is "turso")
	Turso TursoSettings `mapstructure:"turso" json:"turso"`

	// Vector configures native vector search capabilities
	Vector VectorSettings `mapstructure:"vector" json:"vector"`
}

// TursoSettings holds Turso cloud database configuration.
type TursoSettings struct {
	// URL is the Turso database URL (e.g., libsql://your-database.turso.io)
	// Can also be set via TURSO_DATABASE_URL environment variable
	URL string `mapstructure:"url" json:"url"`

	// AuthToken is the authentication token for Turso
	// Can also be set via TURSO_AUTH_TOKEN environment variable
	AuthToken string `mapstructure:"auth_token" json:"auth_token"`
}

// MarshalJSON implements json.Marshaler to redact the AuthToken field.
// TODO(jsonv2): Migrate to MarshalerTo when encoding/json/v2 is stable:
//
//	func (t TursoSettings) MarshalJSONTo(enc *jsontext.Encoder) error {
//	    ...
//	    return json.MarshalEncode(enc, redacted)
//	}
func (t TursoSettings) MarshalJSON() ([]byte, error) {
	type Alias TursoSettings
	redacted := Alias(t)
	if redacted.AuthToken != "" {
		redacted.AuthToken = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// VectorSettings configure native vector search capabilities.
type VectorSettings struct {
	// Enabled controls whether native vector search is active (requires Turso/libsql)
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// Dimensions specifies the embedding vector dimensions.
	// Default: 1024 (Voyage). Override via AGENTCTL_VECTOR_DIMS or config file.
	Dimensions int `mapstructure:"dimensions" json:"dimensions"`
}

// OpenAPISettings hold configuration for the generic http/openapi skill.
type OpenAPISettings struct {
	// PluginPath is a colon-separated search path that locates plugin binaries.
	// Each entry is resolved relative to the agentctl home directory when not
	// absolute. Environment variables may override this value via
	// AGENTCTL_OPENAPI_PLUGIN_PATH.
	PluginPath []string `mapstructure:"plugin_path" json:"plugin_path"`
}

// IndexingSettings configure post-review indexing pipelines.
// See: docs/spec/semantic_file_index.md §8.2
type IndexingSettings struct {
	// PostReview configures the post-review indexing pipeline.
	PostReview PostReviewSettings `mapstructure:"post_review" json:"post_review"`
}

// PostReviewSettings holds the configuration for post-review indexers.
type PostReviewSettings struct {
	// Enabled controls whether post-review indexing is active.
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// Async controls whether indexing runs asynchronously (default: true).
	// When false, task completion blocks until indexing completes.
	Async bool `mapstructure:"async" json:"async"`

	// Indexers lists the configured indexers.
	Indexers []IndexerSettings `mapstructure:"indexers" json:"indexers"`
}

// LLMSettings configures LLM providers for planning and agent operations.
// Environment variables are loaded at config time (FC/IS compliant).
type LLMSettings struct {
	// Provider is the preferred LLM provider: cerebras, openrouter, groq, openai, gemini, anthropic
	Provider string `mapstructure:"provider" json:"provider"`

	// Model is the model name to use (provider-specific)
	Model string `mapstructure:"model" json:"model"`

	// APIKey is the API key for the selected provider (from AGENTCTL_LLM_API_KEY or provider-specific vars)
	APIKey string `mapstructure:"api_key" json:"api_key"`

	// CerebrasAPIKey is the Cerebras API key (from CEREBRAS_API_KEY)
	// Cerebras is preferred for background tasks due to low cost (~$0.10/M tokens)
	CerebrasAPIKey string `mapstructure:"cerebras_api_key" json:"cerebras_api_key"`

	// OpenRouterAPIKey is the OpenRouter API key (from OPENROUTER_API_KEY)
	OpenRouterAPIKey string `mapstructure:"openrouter_api_key" json:"openrouter_api_key"`

	// OpenRouterModel is the model for OpenRouter (from OPENROUTER_MODEL_NAME)
	OpenRouterModel string `mapstructure:"openrouter_model" json:"openrouter_model"`

	// GroqAPIKey is the Groq API key (from GROQ_API_KEY)
	GroqAPIKey string `mapstructure:"groq_api_key" json:"groq_api_key"`

	// OpenAIAPIKey is the OpenAI API key (from OPENAI_API_KEY)
	OpenAIAPIKey string `mapstructure:"openai_api_key" json:"openai_api_key"`

	// GeminiAPIKey is the Gemini API key (from GEMINI_API_KEY)
	GeminiAPIKey string `mapstructure:"gemini_api_key" json:"gemini_api_key"`

	// AnthropicAPIKey is the Anthropic API key (from ANTHROPIC_API_KEY)
	AnthropicAPIKey string `mapstructure:"anthropic_api_key" json:"anthropic_api_key"`

	// CerebrasModel is the model for Cerebras (from CEREBRAS_MODEL)
	CerebrasModel string `mapstructure:"cerebras_model" json:"cerebras_model"`

	// AtomicAPIKey is the API key for atomic fact processing (from AGENTCTL_ATOMIC_API_KEY).
	// Used for fast/cheap LLM operations. Supports any OpenAI-compatible endpoint.
	AtomicAPIKey string `mapstructure:"atomic_api_key" json:"atomic_api_key"`

	// AtomicEndpoint is the endpoint URL for atomic processing (from AGENTCTL_ATOMIC_ENDPOINT).
	// Defaults to OpenRouter if not set.
	AtomicEndpoint string `mapstructure:"atomic_endpoint" json:"atomic_endpoint"`

	// AtomicModel is the model for atomic processing (from AGENTCTL_ATOMIC_MODEL).
	// Defaults to zhipu-ai/glm-4-flash-250414 if not set.
	AtomicModel string `mapstructure:"atomic_model" json:"atomic_model"`

	// ElevenLabsAPIKey is the ElevenLabs API key (from ELEVENLABS_API_KEY)
	// Used for text-to-speech synthesis in presence/voice skill.
	ElevenLabsAPIKey string `mapstructure:"elevenlabs_api_key" json:"elevenlabs_api_key"`
}

// ResolveAPIKey returns the API key for the given provider.
// It checks the provider-specific key first, then falls back to the generic APIKey.
func (l LLMSettings) ResolveAPIKey(provider string) string {
	// Check provider-specific key first
	switch provider {
	case "openrouter":
		if l.OpenRouterAPIKey != "" {
			return l.OpenRouterAPIKey
		}
	case "groq":
		if l.GroqAPIKey != "" {
			return l.GroqAPIKey
		}
	case "openai":
		if l.OpenAIAPIKey != "" {
			return l.OpenAIAPIKey
		}
	case "gemini":
		if l.GeminiAPIKey != "" {
			return l.GeminiAPIKey
		}
	case "anthropic":
		if l.AnthropicAPIKey != "" {
			return l.AnthropicAPIKey
		}
	case "cerebras":
		if l.CerebrasAPIKey != "" {
			return l.CerebrasAPIKey
		}
	case "elevenlabs":
		if l.ElevenLabsAPIKey != "" {
			return l.ElevenLabsAPIKey
		}
	case "lmstudio":
		// LM Studio doesn't require a real API key
		return "lm-studio"
	}
	// Fall back to generic API key
	return l.APIKey
}

// ResolveModel returns the model for the given provider.
// It checks the provider-specific model first, then falls back to the generic Model.
func (l LLMSettings) ResolveModel(provider string) string {
	switch provider {
	case "openrouter":
		if l.OpenRouterModel != "" {
			return l.OpenRouterModel
		}
	case "cerebras":
		if l.CerebrasModel != "" {
			return l.CerebrasModel
		}
	}
	return l.Model
}

// IndexerSettings defines the configuration for a single indexer.
type IndexerSettings struct {
	// ID is a unique identifier for this indexer instance.
	ID string `mapstructure:"id" json:"id"`

	// Kind identifies the indexer type (e.g., "semantic_file_index", "code_symbol_dag").
	Kind string `mapstructure:"kind" json:"kind"`

	// Enabled controls whether this indexer is active.
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// IncludeGlobs are glob patterns for files to include.
	IncludeGlobs []string `mapstructure:"include_globs" json:"include_globs"`

	// ExcludeGlobs are glob patterns for files to exclude.
	ExcludeGlobs []string `mapstructure:"exclude_globs" json:"exclude_globs"`

	// MaxFileKB is the maximum file size in KB to index (0 = no limit).
	MaxFileKB int `mapstructure:"max_file_kb" json:"max_file_kb"`
}

// Option customizes the loader.
type Option func(*loader)

type loader struct {
	configFile string
}

// WithConfigFile instructs the loader to read the specified file explicitly.
func WithConfigFile(path string) Option {
	return func(l *loader) {
		l.configFile = path
	}
}

// Cached config for daemon mode - avoids re-loading config on every request.
var (
	cachedConfig    Config
	cachedConfigErr error
	configOnce      sync.Once
)

// LoadCached returns the cached configuration, loading it on first call.
// This is useful for daemon mode where we want to avoid re-loading config
// on every request (~5-10ms overhead per load).
//
// Note: This caches the first successful load. If you need to reload config
// (e.g., after editing config.yaml), restart the daemon.
func LoadCached(ctx context.Context, opts ...Option) (Config, error) {
	configOnce.Do(func() {
		cachedConfig, cachedConfigErr = Load(ctx, opts...)
	})
	return cachedConfig, cachedConfigErr
}

// ResetCachedConfig clears the cached config (for testing only).
func ResetCachedConfig() {
	configOnce = sync.Once{}
	cachedConfig = Config{}
	cachedConfigErr = nil
}

// Load returns the hydrated configuration by applying defaults, config file, and env overrides.
func Load(_ context.Context, opts ...Option) (Config, error) {
	l := parseOptions(opts)

	home, err := userHomeDir()
	if err != nil {
		return Config{}, err
	}

	v := newConfiguredViper()
	defaultHome := filepath.Join(home, ".agentctl")
	applyDefaults(v, defaultHome)
	configureConfigFile(v, l, defaultHome)
	if err := readConfig(v, l.configFile); err != nil {
		return Config{}, err
	}

	cfg, err := decodeConfig(v)
	if err != nil {
		return Config{}, err
	}

	return finalizeConfig(cfg, home), nil
}

func parseOptions(opts []Option) *loader {
	l := &loader{}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func newConfiguredViper() *viper.Viper {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("AGENTCTL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v
}

func applyDefaults(v *viper.Viper, defaultHome string) {
	v.SetDefault("home", defaultHome)
	v.SetDefault("inline_output_kb", DefaultInlineOutputKB)
	v.SetDefault("max_capture_kb", DefaultMaxCaptureKB)
	v.SetDefault("paths.cas", filepath.Join(defaultHome, "cas"))
	v.SetDefault("paths.jobs", filepath.Join(defaultHome, "jobs"))
	v.SetDefault("paths.cache", filepath.Join(defaultHome, "cache"))
	v.SetDefault("paths.skills", filepath.Join(defaultHome, "skills"))
	v.SetDefault("paths.observability", filepath.Join(defaultHome, "observability"))
	v.SetDefault("storage.root", filepath.Join(defaultHome, "storage"))
	v.SetDefault("memory.auto_cache_ttl", "24h")
	v.SetDefault("memory.default_named_ttl", "720h") // 30d
	v.SetDefault("memory.auto_load_workspace", true)
	v.SetDefault("cache.default_mode", "off")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
	v.SetDefault("embedding.provider", "voyage")
	v.SetDefault("embedding.model", "voyage-code-3")
	v.SetDefault("embedding.dimensions", dbdriver.DefaultVectorDimensions)
	v.SetDefault("openapi.plugin_path", filepath.Join(defaultHome, "plugins"))
	v.SetDefault("indexing.post_review.enabled", false)
	v.SetDefault("indexing.post_review.async", true)
	v.SetDefault("indexing.post_review.indexers", []map[string]any{})
	// Database defaults - libsql for local-first with optional sync
	v.SetDefault("database.driver", "libsql")
	v.SetDefault("database.turso.url", "")
	v.SetDefault("database.turso.auth_token", "")
	v.SetDefault("database.vector.enabled", true)
	v.SetDefault("database.vector.dimensions", dbdriver.DefaultVectorDimensions)
	// CAS policy defaults - store always on, expose off by default (hooks/tools)
	v.SetDefault("cas.store", true)
	v.SetDefault("cas.expose", "off")
}

func configureConfigFile(v *viper.Viper, l *loader, defaultHome string) {
	if l.configFile != "" {
		v.SetConfigFile(l.configFile)
		return
	}
	v.SetConfigName("config")
	v.AddConfigPath(defaultHome)
}

func readConfig(v *viper.Viper, explicit string) error {
	if err := v.ReadInConfig(); err != nil {
		var configErr viper.ConfigFileNotFoundError
		if explicit != "" || !errors.As(err, &configErr) {
			return fmt.Errorf("config: %w", err)
		}
	}
	return nil
}

func decodeConfig(v *viper.Viper) (Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config decode: %w", err)
	}
	cfg.OpenAPI.PluginPath = parsePluginPathList(v.Get("openapi.plugin_path"))
	return cfg, nil
}

// finalizeConfig finalizes and normalizes a Config using the provided home directory.
//
// It resolves and normalizes configured paths relative to the resolved Home, applies
// sensible defaults for unset numeric/time/string fields (inline output size, capture
// limits, memory TTLs, cache/default mode, logging defaults, plugin paths, embedding
// models, etc.), derives or adjusts related fields (database driver detection, vector
// dimensions), and applies environment-variable overrides for Turso, CAS, LLM, Atomic,
// embedding, search, and observability settings. The returned Config is ready for use
// by the application.
func finalizeConfig(cfg Config, home string) Config {
	cfg.Home = absPath(cfg.Home, home)
	cfg.Paths.CAS = resolvePath(cfg.Paths.CAS, cfg.Home, home)
	cfg.Paths.Jobs = resolvePath(cfg.Paths.Jobs, cfg.Home, home)
	cfg.Paths.Cache = resolvePath(cfg.Paths.Cache, cfg.Home, home)
	cfg.Paths.Skills = resolvePath(cfg.Paths.Skills, cfg.Home, home)
	cfg.Paths.Observability = resolvePath(cfg.Paths.Observability, cfg.Home, home)
	cfg.Storage.Root = resolvePath(cfg.Storage.Root, cfg.Home, home)
	if len(cfg.OpenAPI.PluginPath) == 0 {
		cfg.OpenAPI.PluginPath = []string{filepath.Join(cfg.Home, "plugins")}
	}
	cfg.OpenAPI.PluginPath = normalizePluginPaths(cfg.OpenAPI.PluginPath, cfg.Home, home)
	cfg.Embedding.Models = normalizeEmbeddingModels(cfg.Embedding.Models)

	if cfg.InlineOutputKB <= 0 {
		cfg.InlineOutputKB = DefaultInlineOutputKB
	}
	if cfg.MaxCaptureKB <= 0 {
		cfg.MaxCaptureKB = DefaultMaxCaptureKB
	}
	if cfg.Memory.AutoCacheTTL <= 0 {
		cfg.Memory.AutoCacheTTL = 24 * time.Hour
	}
	if cfg.Memory.DefaultNamedTTL <= 0 {
		cfg.Memory.DefaultNamedTTL = 30 * 24 * time.Hour
	}
	if cfg.Cache.DefaultMode == "" {
		cfg.Cache.DefaultMode = "auto"
	}

	cfg.Logging.Level = strings.ToLower(strings.TrimSpace(cfg.Logging.Level))
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	cfg.Logging.Format = strings.ToLower(strings.TrimSpace(cfg.Logging.Format))
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}

	// Database/Turso: Allow standard Turso env vars as overrides
	if url := os.Getenv("TURSO_DATABASE_URL"); url != "" && cfg.Database.Turso.URL == "" {
		cfg.Database.Turso.URL = url
	}
	if token := os.Getenv("TURSO_AUTH_TOKEN"); token != "" && cfg.Database.Turso.AuthToken == "" {
		cfg.Database.Turso.AuthToken = token
	}
	// Auto-detect driver from URL if not explicitly set
	if cfg.Database.Driver == "sqlite" && cfg.Database.Turso.URL != "" {
		cfg.Database.Driver = "turso"
	}
	// Default vector dimensions from embedding config if not set
	if cfg.Database.Vector.Dimensions == 0 {
		cfg.Database.Vector.Dimensions = cfg.Embedding.Dimensions
	}

	// Default-enable observability: if AGENTCTL_OBS_DIR is not set but
	// cfg.Paths.Observability is configured, set the env var so all
	// downstream code (skills, CLI, daemon) inherits the path.
	if cfg.Paths.Observability != "" {
		obsEnv := os.Getenv("AGENTCTL_OBS_DIR")
		// NOTE: We normally respect explicit env overrides. However, it is common to
		// end up with a stale absolute home directory after a username change (e.g.
		// AGENTCTL_OBS_DIR="/Users/olduser/..."). When that home no longer exists,
		// observability becomes noisy (permission errors) and effectively broken.
		if obsEnv == "" || shouldRepairObsDir(obsEnv, home) {
			// Best-effort: ignore errors since observability is non-critical
			_ = os.Setenv("AGENTCTL_OBS_DIR", cfg.Paths.Observability)
		}
	}

	// CAS policy env var overrides
	if storeEnv := os.Getenv("AGENTCTL_CAS_STORE"); storeEnv != "" {
		cfg.CAS.Store = storeEnv == "1" || strings.EqualFold(storeEnv, "true")
	}
	if exposeEnv := os.Getenv("AGENTCTL_CAS_EXPOSE"); exposeEnv != "" {
		switch strings.ToLower(exposeEnv) {
		case "off", "0", "false":
			cfg.CAS.Expose = ExposePolicyOff
		case "digest":
			cfg.CAS.Expose = ExposePolicyDigest
		case "hint":
			cfg.CAS.Expose = ExposePolicyHint
		}
	}

	// LLM API key env var overrides (load once at config time - FC/IS compliant)
	if key := os.Getenv("AGENTCTL_LLM_API_KEY"); key != "" && cfg.LLM.APIKey == "" {
		cfg.LLM.APIKey = key
	}
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" && cfg.LLM.CerebrasAPIKey == "" {
		cfg.LLM.CerebrasAPIKey = key
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" && cfg.LLM.OpenRouterAPIKey == "" {
		cfg.LLM.OpenRouterAPIKey = key
	}
	if model := os.Getenv("OPENROUTER_MODEL_NAME"); model != "" && cfg.LLM.OpenRouterModel == "" {
		cfg.LLM.OpenRouterModel = model
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" && cfg.LLM.GroqAPIKey == "" {
		cfg.LLM.GroqAPIKey = key
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" && cfg.LLM.OpenAIAPIKey == "" {
		cfg.LLM.OpenAIAPIKey = key
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" && cfg.LLM.GeminiAPIKey == "" {
		cfg.LLM.GeminiAPIKey = key
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" && cfg.LLM.AnthropicAPIKey == "" {
		cfg.LLM.AnthropicAPIKey = key
	}
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" && cfg.LLM.CerebrasAPIKey == "" {
		cfg.LLM.CerebrasAPIKey = key
	}
	if model := os.Getenv("CEREBRAS_MODEL"); model != "" && cfg.LLM.CerebrasModel == "" {
		cfg.LLM.CerebrasModel = model
	}
	if provider := os.Getenv("AGENTCTL_LLM_PROVIDER"); provider != "" && cfg.LLM.Provider == "" {
		cfg.LLM.Provider = provider
	}
	if model := os.Getenv("AGENTCTL_LLM_MODEL"); model != "" && cfg.LLM.Model == "" {
		cfg.LLM.Model = model
	}

	// Atomic processing config (for SimpleMem-style fact decomposition)
	if key := os.Getenv("AGENTCTL_ATOMIC_API_KEY"); key != "" && cfg.LLM.AtomicAPIKey == "" {
		cfg.LLM.AtomicAPIKey = key
	}
	// Fallback: use OpenRouter key if atomic key not set
	if cfg.LLM.AtomicAPIKey == "" && cfg.LLM.OpenRouterAPIKey != "" {
		cfg.LLM.AtomicAPIKey = cfg.LLM.OpenRouterAPIKey
	}
	if endpoint := os.Getenv("AGENTCTL_ATOMIC_ENDPOINT"); endpoint != "" && cfg.LLM.AtomicEndpoint == "" {
		cfg.LLM.AtomicEndpoint = endpoint
	}
	if model := os.Getenv("AGENTCTL_ATOMIC_MODEL"); model != "" && cfg.LLM.AtomicModel == "" {
		cfg.LLM.AtomicModel = model
	}

	// Embedding API key env var overrides (FC/IS compliant)
	if key := os.Getenv("VOYAGE_API_KEY"); key != "" && cfg.Embedding.VoyageAPIKey == "" {
		cfg.Embedding.VoyageAPIKey = key
	}

	// Search API key env var overrides (FC/IS compliant)
	if key := os.Getenv("TAVILY_API_KEY"); key != "" && cfg.Search.TavilyAPIKey == "" {
		cfg.Search.TavilyAPIKey = key
	}
	if key := os.Getenv("EXA_API_KEY"); key != "" && cfg.Search.ExaAPIKey == "" {
		cfg.Search.ExaAPIKey = key
	}
	if key := os.Getenv("PERPLEXITY_API_KEY"); key != "" && cfg.Search.PerplexityAPIKey == "" {
		cfg.Search.PerplexityAPIKey = key
	}

	// Voice/TTS API key env var overrides (FC/IS compliant)
	if key := os.Getenv("ELEVENLABS_API_KEY"); key != "" && cfg.LLM.ElevenLabsAPIKey == "" {
		cfg.LLM.ElevenLabsAPIKey = key
	}

	return cfg
}

func shouldRepairObsDir(obsEnv, userHome string) bool {
	obsEnv = strings.TrimSpace(obsEnv)
	userHome = strings.TrimSpace(userHome)
	if obsEnv == "" || userHome == "" {
		return false
	}

	// "~" is never expanded for file paths at runtime, and will cause writes to
	// target a literal "./~" directory depending on CWD.
	if strings.HasPrefix(obsEnv, "~") {
		return true
	}

	// Repair stale macOS home paths after a username change:
	//   obsEnv:   /Users/olduser/.agentctl/observability
	//   userHome: /Users/newuser
	if strings.HasPrefix(obsEnv, "/Users/") && strings.HasPrefix(userHome, "/Users/") {
		rest := strings.TrimPrefix(obsEnv, "/Users/")
		oldUser, _, _ := strings.Cut(rest, "/")
		if oldUser == "" {
			return false
		}
		homeRest := strings.TrimPrefix(userHome, "/Users/")
		newUser, _, _ := strings.Cut(homeRest, "/")
		if newUser == "" {
			return false
		}
		if oldUser == newUser {
			return false
		}
		if _, err := os.Stat(filepath.Join("/Users", oldUser)); os.IsNotExist(err) {
			return true
		}
	}

	return false
}

func userHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home: %w", err)
	}
	return home, nil
}

func absPath(p, home string) string {
	if p == "" {
		return home
	}
	return resolvePath(p, home, home)
}

func resolvePath(p, base, userHome string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		trimmed := strings.TrimPrefix(p, "~")
		trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
		return filepath.Join(userHome, trimmed)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

func parsePluginPathList(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return splitPathList(v)
	case []string:
		return normalizeStringSlice(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return normalizeStringSlice(out)
	default:
		return nil
	}
}

func splitPathList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, string(os.PathListSeparator))
	return normalizeStringSlice(parts)
}

func normalizeStringSlice(parts []string) []string {
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeEmbeddingModels(models map[string]string) map[string]string {
	if len(models) == 0 {
		return nil
	}
	out := make(map[string]string, len(models))
	for key, value := range models {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		normalizedValue := strings.TrimSpace(value)
		if normalizedValue == "" {
			continue
		}
		out[normalizedKey] = normalizedValue
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizePluginPaths(paths []string, base, userHome string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		resolved := resolvePath(p, base, userHome)
		if resolved == "" {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		out = append(out, resolved)
	}
	return out
}
