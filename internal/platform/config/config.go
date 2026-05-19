package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/spf13/viper"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
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
	Discord        DiscordSettings   `mapstructure:"discord" json:"discord"`
	Telegram       TelegramSettings  `mapstructure:"telegram" json:"telegram"`
	Teams          TeamsSettings     `mapstructure:"teams" json:"teams"`
	OAuth          OAuthSettings     `mapstructure:"oauth" json:"oauth"`
	Curator        CuratorSettings   `mapstructure:"curator" json:"curator"`
}

// DiscordSettings configure the Discord chat adapter.
type DiscordSettings struct {
	// BotToken is the Discord bot token (from DISCORD_BOT_TOKEN).
	BotToken string `mapstructure:"bot_token" json:"bot_token"`

	// GuildID restricts slash commands to a single guild (dev mode).
	// If empty, commands are registered globally (takes ~1 hour to propagate).
	GuildID string `mapstructure:"guild_id" json:"guild_id"`

	// ActivityChannelID is the channel for compact agent activity feed messages.
	ActivityChannelID string `mapstructure:"activity_channel_id" json:"activity_channel_id"`

	// AgentChannelID is the channel where per-agent threads are created.
	AgentChannelID string `mapstructure:"agent_channel_id" json:"agent_channel_id"`

	// ChatChannelIDs lists channels where the bot responds to all messages (not just mentions).
	ChatChannelIDs []string `mapstructure:"chat_channel_ids" json:"chat_channel_ids"`

	// ChatProfile is the console session profile for chat messages (default: "explorer").
	ChatProfile string `mapstructure:"chat_profile" json:"chat_profile"`

	// ChatSystemPrompt overrides the system prompt for chat sessions.
	ChatSystemPrompt string `mapstructure:"chat_system_prompt" json:"chat_system_prompt"`
}

// MarshalJSON implements json.Marshaler to redact the BotToken field.
func (d DiscordSettings) MarshalJSON() ([]byte, error) {
	type Alias DiscordSettings
	redacted := Alias(d)
	if redacted.BotToken != "" {
		redacted.BotToken = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// TelegramSettings configure the Telegram chat adapter.
type TelegramSettings struct {
	// BotToken is the Telegram bot token (from TELEGRAM_BOT_TOKEN).
	BotToken string `mapstructure:"bot_token" json:"bot_token"`

	// ActivityChatID is the chat for compact agent activity feed messages.
	ActivityChatID int64 `mapstructure:"activity_chat_id" json:"activity_chat_id"`

	// AgentChatID is the chat where per-agent updates are posted.
	// For best UX, use a forum supergroup so agent sessions can map to topics.
	AgentChatID int64 `mapstructure:"agent_chat_id" json:"agent_chat_id"`

	// ChatIDs lists chats where the bot responds to all messages (not just mentions).
	ChatIDs []int64 `mapstructure:"chat_ids" json:"chat_ids"`

	// ChatProfile is the console session profile for chat messages (default: "explorer").
	ChatProfile string `mapstructure:"chat_profile" json:"chat_profile"`

	// ChatSystemPrompt overrides the system prompt for chat sessions.
	ChatSystemPrompt string `mapstructure:"chat_system_prompt" json:"chat_system_prompt"`

	// MaxConcurrentMessages caps concurrent command/message handlers (default: 10).
	MaxConcurrentMessages int `mapstructure:"max_concurrent_messages" json:"max_concurrent_messages"`

	// EditIntervalMS is the edit cadence for streaming responses (default: 1500ms).
	EditIntervalMS int `mapstructure:"edit_interval_ms" json:"edit_interval_ms"`
}

// MarshalJSON implements json.Marshaler to redact the BotToken field.
func (t TelegramSettings) MarshalJSON() ([]byte, error) {
	type Alias TelegramSettings
	redacted := Alias(t)
	if redacted.BotToken != "" {
		redacted.BotToken = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// TeamsSettings configure the Microsoft Teams chat adapter.
type TeamsSettings struct {
	// TenantID is the Azure AD tenant to accept inbound webhook tokens from.
	TenantID string `mapstructure:"tenant_id" json:"tenant_id"`

	// ClientID is the Azure Bot / App Registration Application (client) ID.
	ClientID string `mapstructure:"client_id" json:"client_id"`

	// ClientSecret is the app client secret used for Bot Framework Connector tokens.
	ClientSecret string `mapstructure:"client_secret" json:"client_secret"`

	// ChatConversationIDs lists conversations where the bot responds to all messages.
	// Outside this list: respond only when mentioned (except 1:1 chats).
	ChatConversationIDs []string `mapstructure:"chat_conversation_ids" json:"chat_conversation_ids"`

	// ChatProfile is the console session profile for chat messages (default: "explorer").
	ChatProfile string `mapstructure:"chat_profile" json:"chat_profile"`

	// ChatSystemPrompt overrides the system prompt for chat sessions.
	ChatSystemPrompt string `mapstructure:"chat_system_prompt" json:"chat_system_prompt"`

	// MaxConcurrentMessages caps concurrent message handlers (default: 10).
	MaxConcurrentMessages int `mapstructure:"max_concurrent_messages" json:"max_concurrent_messages"`

	// EditIntervalMS is the edit cadence for streaming responses (default: 1500ms).
	EditIntervalMS int `mapstructure:"edit_interval_ms" json:"edit_interval_ms"`

	// SkipJWTVerify disables inbound JWT verification. Dev-only; enforced at runtime.
	SkipJWTVerify bool `mapstructure:"skip_jwt_verify" json:"skip_jwt_verify"`

	// AgentConversationID is the Teams conversation where agent lifecycle cards are posted.
	// Without this, agent events are silently ignored (same pattern as Telegram AgentChatID).
	AgentConversationID string `mapstructure:"agent_conversation_id" json:"agent_conversation_id"`

	// ActivityFeedConversationID is an optional conversation for compact one-line lifecycle events.
	ActivityFeedConversationID string `mapstructure:"activity_feed_conversation_id" json:"activity_feed_conversation_id"`
}

// MarshalJSON implements json.Marshaler to redact the ClientSecret field.
func (t TeamsSettings) MarshalJSON() ([]byte, error) {
	type Alias TeamsSettings
	redacted := Alias(t)
	if redacted.ClientSecret != "" {
		redacted.ClientSecret = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// OAuthSettings configures the OAuth AuthBroker.
type OAuthSettings struct {
	// EncryptionKey is a base64-encoded 32-byte AES-256-GCM key for encrypting
	// tokens at rest. Generate with: openssl rand -base64 32
	EncryptionKey string `mapstructure:"encryption_key" json:"encryption_key"`

	// CallbackBaseURL is the public URL base for OAuth callbacks.
	// Example: "https://foxctl.example.com"
	// The callback endpoint will be at {CallbackBaseURL}/api/oauth/callback
	CallbackBaseURL string `mapstructure:"callback_base_url" json:"callback_base_url"`

	// Microsoft Graph OAuth settings
	MicrosoftClientID     string `mapstructure:"microsoft_client_id" json:"microsoft_client_id"`
	MicrosoftClientSecret string `mapstructure:"microsoft_client_secret" json:"microsoft_client_secret"`
	MicrosoftTenantID     string `mapstructure:"microsoft_tenant_id" json:"microsoft_tenant_id"` // "common" for multi-tenant, specific tenant ID for single

	// Google OAuth settings
	GoogleClientID     string `mapstructure:"google_client_id" json:"google_client_id"`
	GoogleClientSecret string `mapstructure:"google_client_secret" json:"google_client_secret"`

	// GitHub OAuth settings
	GitHubClientID     string `mapstructure:"github_client_id" json:"github_client_id"`
	GitHubClientSecret string `mapstructure:"github_client_secret" json:"github_client_secret"`
}

// CuratorSettings configures the background context plane curator.
//
// Two concurrent loops run inside the curator:
//   - "active" loop: frequent (5m) light checks — lifecycle transitions, stale detection
//   - "dream" loop: infrequent (24-72h) deep analysis — consolidation, dedup, scoring
//
// Both run simultaneously. Active keeps context tidy day-to-day;
// dream does heavier periodic maintenance.
//
// In config.yaml:
//
//   curator:
//     enabled: true
//     active_enabled: true       # 5m light sweep
//     dream_enabled: true        # 24h deep analysis
//     active_interval: 5m        # Go duration
//     dream_interval: 24h        # Go duration (72h for weekly)
//     stale_after_days: 30
//     archive_after_days: 90
//     min_confidence: 0.5
//     handoff_stale_days: 30
//     dry_run: false
//
// Env vars: FOXCTL_CURATOR_ACTIVE_INTERVAL, FOXCTL_CURATOR_DREAM_INTERVAL,
// FOXCTL_CURATOR_ENABLED, FOXCTL_CURATOR_DRY_RUN, FOXCTL_CURATOR_STALE_AFTER_DAYS,
// FOXCTL_CURATOR_ARCHIVE_AFTER_DAYS, FOXCTL_CURATOR_ACTIVE_ENABLED,
// FOXCTL_CURATOR_DREAM_ENABLED.
type CuratorSettings struct {
	// ActiveInterval is the tick for the active (light) loop.
	ActiveInterval time.Duration `mapstructure:"active_interval" json:"active_interval"`

	// DreamInterval is the tick for the dream (deep) loop.
	DreamInterval time.Duration `mapstructure:"dream_interval" json:"dream_interval"`

	// StaleAfterDays: records with no uses older than this → stale.
	StaleAfterDays int `mapstructure:"stale_after_days" json:"stale_after_days"`

	// ArchiveAfterDays: stale records older than this → archive.
	ArchiveAfterDays int `mapstructure:"archive_after_days" json:"archive_after_days"`

	// MinConfidence: observations below this → flagged for review.
	MinConfidence float64 `mapstructure:"min_confidence" json:"min_confidence"`

	// HandoffStaleDays: handoff files older than this → flag for archival.
	HandoffStaleDays int `mapstructure:"handoff_stale_days" json:"handoff_stale_days"`

	// Enabled controls whether the curator runs at all.
	Enabled *bool `mapstructure:"enabled" json:"enabled"`

	// DryRun: true = report only, no mutations.
	DryRun *bool `mapstructure:"dry_run" json:"dry_run"`

	// ActiveEnabled controls whether the active loop runs.
	ActiveEnabled *bool `mapstructure:"active_enabled" json:"active_enabled"`

	// DreamEnabled controls whether the dream loop runs.
	DreamEnabled *bool `mapstructure:"dream_enabled" json:"dream_enabled"`
}

// MarshalJSON implements json.Marshaler to redact secret fields.
func (o OAuthSettings) MarshalJSON() ([]byte, error) {
	type Alias OAuthSettings
	redacted := Alias(o)
	if redacted.EncryptionKey != "" {
		redacted.EncryptionKey = "[REDACTED]"
	}
	if redacted.MicrosoftClientSecret != "" {
		redacted.MicrosoftClientSecret = "[REDACTED]"
	}
	if redacted.GoogleClientSecret != "" {
		redacted.GoogleClientSecret = "[REDACTED]"
	}
	if redacted.GitHubClientSecret != "" {
		redacted.GitHubClientSecret = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// Paths include common on-disk locations rooted at the foxctl home directory.
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
	Output string `mapstructure:"output" json:"output"`
}

// EmbeddingSettings configure embedding provider defaults.
type EmbeddingSettings struct {
	Provider string `mapstructure:"provider" json:"provider"`
	Model    string `mapstructure:"model" json:"model"`
	BaseURL  string `mapstructure:"base_url" json:"base_url"`
	// APIKey is a generic embedding provider API key (from FOXCTL_EMBEDDING_API_KEY).
	// Use this for provider-agnostic or OpenAI-compatible embedding endpoints.
	APIKey     string            `mapstructure:"api_key" json:"api_key"`
	Dimensions int               `mapstructure:"dimensions" json:"dimensions"`
	Models     map[string]string `mapstructure:"models" json:"models"`
	Flags      EmbeddingFlags    `mapstructure:"flags" json:"flags"`
}

// MarshalJSON implements json.Marshaler to redact embedding provider secrets.
func (e EmbeddingSettings) MarshalJSON() ([]byte, error) {
	type Alias EmbeddingSettings
	redacted := Alias(e)
	if redacted.APIKey != "" {
		redacted.APIKey = "[REDACTED]"
	}
	return json.Marshal(redacted)
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

// MarshalJSON implements json.Marshaler to redact search provider secrets.
func (s SearchSettings) MarshalJSON() ([]byte, error) {
	type Alias SearchSettings
	redacted := Alias(s)
	if redacted.TavilyAPIKey != "" {
		redacted.TavilyAPIKey = "[REDACTED]"
	}
	if redacted.ExaAPIKey != "" {
		redacted.ExaAPIKey = "[REDACTED]"
	}
	if redacted.PerplexityAPIKey != "" {
		redacted.PerplexityAPIKey = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// DatabaseSettings configure database driver and connection.
type DatabaseSettings struct {
	// Driver specifies which database driver to use: "sqlite" (default), "turso", or "postgres"
	Driver string `mapstructure:"driver" json:"driver"`

	// Turso holds Turso-specific configuration (when driver is "turso")
	Turso TursoSettings `mapstructure:"turso" json:"turso"`

	// Postgres holds PostgreSQL-specific configuration (when driver is "postgres")
	Postgres PostgresSettings `mapstructure:"postgres" json:"postgres"`

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

// PostgresSettings holds PostgreSQL database configuration.
type PostgresSettings struct {
	// DSN is the PostgreSQL connection string.
	// Example: postgres://user:pass@host:5432/dbname?sslmode=require
	// Can also be set via FOXCTL_POSTGRES_DSN or DATABASE_URL.
	DSN string `mapstructure:"dsn" json:"dsn"`

	// MaxOpenConns is the maximum number of open connections per store.
	// Default: 5.
	MaxOpenConns int `mapstructure:"max_open_conns" json:"max_open_conns"`

	// RequireVector fails startup if pgvector is unavailable.
	// Default: false (gracefully degrade without vector search).
	RequireVector bool `mapstructure:"require_vector" json:"require_vector"`
}

// MarshalJSON implements json.Marshaler to redact the DSN field.
func (p PostgresSettings) MarshalJSON() ([]byte, error) {
	type Alias PostgresSettings
	redacted := Alias(p)
	if redacted.DSN != "" {
		redacted.DSN = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// VectorSettings configure native vector search capabilities.
type VectorSettings struct {
	// Enabled controls whether native vector search is active (requires Turso or Postgres)
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// Dimensions specifies the embedding vector dimensions.
	// Default: 4096 (Qwen). Override via FOXCTL_VECTOR_DIMS or config file.
	Dimensions int `mapstructure:"dimensions" json:"dimensions"`
}

// OpenAPISettings hold configuration for the generic http/openapi skill.
type OpenAPISettings struct {
	// PluginPath is a colon-separated search path that locates plugin binaries.
	// Each entry is resolved relative to the foxctl home directory when not
	// absolute. Environment variables may override this value via
	// FOXCTL_OPENAPI_PLUGIN_PATH.
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
	// Provider is the preferred LLM provider: cerebras, openrouter, groq, openai,
	// gemini, anthropic, lmstudio, openai_compat, or bedrock.
	Provider string `mapstructure:"provider" json:"provider"`

	// Model is the model name to use (provider-specific)
	Model string `mapstructure:"model" json:"model"`

	// BaseURL is the API base URL for OpenAI-compatible or self-hosted backends.
	BaseURL string `mapstructure:"base_url" json:"base_url"`

	// APIKey is the API key for the selected provider (from FOXCTL_LLM_API_KEY or provider-specific vars)
	APIKey string `mapstructure:"api_key" json:"api_key"`

	// AuthMode controls how chat requests are authenticated.
	// Supported values: auto, none, bearer, header.
	AuthMode string `mapstructure:"auth_mode" json:"auth_mode"`

	// AuthHeader is the header name to use when auth_mode=header.
	AuthHeader string `mapstructure:"auth_header" json:"auth_header"`

	// AuthPrefix is prepended to APIKey when auth_mode=bearer or auth_mode=header.
	// Example: "Bearer " or "Token ".
	AuthPrefix string `mapstructure:"auth_prefix" json:"auth_prefix"`

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

	// AtomicAPIKey is the API key for atomic fact processing (from FOXCTL_ATOMIC_API_KEY).
	// Used for fast/cheap LLM operations. Supports any OpenAI-compatible endpoint.
	AtomicAPIKey string `mapstructure:"atomic_api_key" json:"atomic_api_key"`

	// AtomicEndpoint is the endpoint URL for atomic processing (from FOXCTL_ATOMIC_ENDPOINT).
	// Defaults to OpenRouter if not set.
	AtomicEndpoint string `mapstructure:"atomic_endpoint" json:"atomic_endpoint"`

	// AtomicModel is the model for atomic processing (from FOXCTL_ATOMIC_MODEL).
	// Defaults to zhipu-ai/glm-4-flash-250414 if not set.
	AtomicModel string `mapstructure:"atomic_model" json:"atomic_model"`

	// ElevenLabsAPIKey is the ElevenLabs API key (from ELEVENLABS_API_KEY)
	// Used for text-to-speech synthesis in presence/voice skill.
	ElevenLabsAPIKey string `mapstructure:"elevenlabs_api_key" json:"elevenlabs_api_key"`

	// BedrockRegion is the AWS region for Bedrock (from BEDROCK_REGION or AWS_DEFAULT_REGION).
	// Uses the standard AWS credential chain (env vars, shared credentials, IAM role).
	BedrockRegion string `mapstructure:"bedrock_region" json:"bedrock_region"`
}

// MarshalJSON implements json.Marshaler to redact LLM provider secrets.
func (l LLMSettings) MarshalJSON() ([]byte, error) {
	type Alias LLMSettings
	redacted := Alias(l)
	if redacted.APIKey != "" {
		redacted.APIKey = "[REDACTED]"
	}
	if redacted.CerebrasAPIKey != "" {
		redacted.CerebrasAPIKey = "[REDACTED]"
	}
	if redacted.OpenRouterAPIKey != "" {
		redacted.OpenRouterAPIKey = "[REDACTED]"
	}
	if redacted.GroqAPIKey != "" {
		redacted.GroqAPIKey = "[REDACTED]"
	}
	if redacted.OpenAIAPIKey != "" {
		redacted.OpenAIAPIKey = "[REDACTED]"
	}
	if redacted.GeminiAPIKey != "" {
		redacted.GeminiAPIKey = "[REDACTED]"
	}
	if redacted.AnthropicAPIKey != "" {
		redacted.AnthropicAPIKey = "[REDACTED]"
	}
	if redacted.AtomicAPIKey != "" {
		redacted.AtomicAPIKey = "[REDACTED]"
	}
	if redacted.ElevenLabsAPIKey != "" {
		redacted.ElevenLabsAPIKey = "[REDACTED]"
	}
	return json.Marshal(redacted)
}

// ResolveAPIKey returns the API key for the given provider.
// It checks the provider-specific key first, then falls back to the generic APIKey.
func (l LLMSettings) ResolveAPIKey(provider string) string {
	provider = normalizeLLMProviderName(provider)
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
	case "bedrock":
		// Bedrock uses AWS IAM credentials, not an API key
		return "bedrock-iam"
	}
	// Fall back to generic API key
	return l.APIKey
}

// ResolveModel returns the model for the given provider.
// It checks the provider-specific model first, then falls back to the generic Model.
func (l LLMSettings) ResolveModel(provider string) string {
	provider = normalizeLLMProviderName(provider)
	switch provider {
	case "openrouter":
		if l.OpenRouterModel != "" {
			return l.OpenRouterModel
		}
	case "cerebras":
		if l.CerebrasModel != "" {
			return l.CerebrasModel
		}
	case "bedrock":
		if model := os.Getenv("BEDROCK_MODEL"); model != "" {
			return model
		}
		if l.Model != "" {
			return l.Model
		}
		return "anthropic.claude-3-5-sonnet-20241022-v2:0"
	}
	return l.Model
}

// ResolveBaseURL returns the configured or provider-default base URL.
func (l LLMSettings) ResolveBaseURL(provider string) string {
	if strings.TrimSpace(l.BaseURL) != "" {
		return strings.TrimSpace(l.BaseURL)
	}
	switch normalizeLLMProviderName(provider) {
	case "cerebras":
		return "https://api.cerebras.ai/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "lmstudio":
		if baseURL := os.Getenv("LMSTUDIO_BASE_URL"); baseURL != "" {
			return baseURL
		}
		return "http://localhost:1234/v1"
	case "bedrock":
		return ""
	default:
		return "https://api.openai.com/v1"
	}
}

// ResolveAuthMode returns the effective auth mode.
func (l LLMSettings) ResolveAuthMode(provider string) string {
	if mode := strings.ToLower(strings.TrimSpace(l.AuthMode)); mode != "" {
		return mode
	}
	switch normalizeLLMProviderName(provider) {
	case "bedrock":
		return "none"
	case "lmstudio", "openai_compat":
		if strings.TrimSpace(l.ResolveAPIKey(provider)) == "" {
			return "none"
		}
		return "bearer"
	default:
		return "bearer"
	}
}

// ResolveAuthHeader returns the header name for auth_mode=header.
func (l LLMSettings) ResolveAuthHeader(provider string) string {
	if header := strings.TrimSpace(l.AuthHeader); header != "" {
		return header
	}
	if l.ResolveAuthMode(provider) == "header" {
		return "X-API-Key"
	}
	return "Authorization"
}

// ResolveAuthPrefix returns the value prefix for auth headers.
func (l LLMSettings) ResolveAuthPrefix(provider string) string {
	if prefix := l.AuthPrefix; prefix != "" {
		return prefix
	}
	if l.ResolveAuthMode(provider) == "bearer" {
		return "Bearer "
	}
	return ""
}

func normalizeLLMProviderName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai-compatible", "openai_compat":
		return "openai_compat"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
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
	configFile    string
	workspacePath string
}

// WithConfigFile instructs the loader to read the specified file explicitly.
func WithConfigFile(path string) Option {
	return func(l *loader) {
		l.configFile = path
	}
}

// WithWorkspacePath instructs the loader to merge workspace-local config from
// <workspace>/.foxctl/config.yaml after the global config file.
func WithWorkspacePath(path string) Option {
	return func(l *loader) {
		l.workspacePath = strings.TrimSpace(path)
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
	defaultHome := filepath.Join(home, ".foxctl")
	applyDefaults(v, defaultHome)
	configureConfigFile(v, l, defaultHome)
	if err := readConfig(v, l.configFile); err != nil {
		return Config{}, err
	}
	cfg, err := decodeConfig(v)
	if err != nil {
		return Config{}, err
	}
	if err := applyWorkspaceOverrides(&cfg, l.workspacePath); err != nil {
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
	v.SetEnvPrefix("FOXCTL")
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
	v.SetDefault("logging.output", "")
	v.SetDefault("embedding.provider", "openai_compat")
	v.SetDefault("embedding.model", "text-embedding-qwen3-embedding-8b")
	v.SetDefault("embedding.dimensions", dbdriver.DefaultVectorDimensions)
	v.SetDefault("openapi.plugin_path", filepath.Join(defaultHome, "plugins"))
	v.SetDefault("indexing.post_review.enabled", false)
	v.SetDefault("indexing.post_review.async", true)
	v.SetDefault("indexing.post_review.indexers", []map[string]any{})
	// Database defaults - Turso for local-first vectors with optional sync
	v.SetDefault("database.driver", "turso")
	v.SetDefault("database.turso.url", "")
	v.SetDefault("database.turso.auth_token", "")
	v.SetDefault("database.postgres.dsn", "")
	v.SetDefault("database.postgres.max_open_conns", 5)
	v.SetDefault("database.postgres.require_vector", false)
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

func applyWorkspaceOverrides(cfg *Config, workspacePath string) error {
	if cfg == nil {
		return nil
	}
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		workspacePath = ws.Detect("")
	}
	workspacePath = ws.Normalize(workspacePath)
	if workspacePath == "" {
		return nil
	}
	configPath := filepath.Join(workspacePath, ".foxctl", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: stat workspace config: %w", err)
	}
	wv := viper.New()
	wv.SetConfigFile(configPath)
	wv.SetConfigType("yaml")
	if err := wv.ReadInConfig(); err != nil {
		return fmt.Errorf("config: read workspace config: %w", err)
	}
	var workspaceCfg Config
	if err := wv.Unmarshal(&workspaceCfg); err != nil {
		return fmt.Errorf("config: decode workspace config: %w", err)
	}
	overlayLLMSettings(&cfg.LLM, workspaceCfg.LLM)
	overlayEmbeddingSettings(&cfg.Embedding, workspaceCfg.Embedding)
	return nil
}

func overlayLLMSettings(dst *LLMSettings, src LLMSettings) {
	if dst == nil {
		return
	}
	overlayString := func(target *string, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			*target = value
		}
	}
	overlayString(&dst.Provider, src.Provider)
	overlayString(&dst.Model, src.Model)
	overlayString(&dst.BaseURL, src.BaseURL)
	overlayString(&dst.APIKey, src.APIKey)
	overlayString(&dst.AuthMode, src.AuthMode)
	overlayString(&dst.AuthHeader, src.AuthHeader)
	if src.AuthPrefix != "" {
		dst.AuthPrefix = src.AuthPrefix
	}
	overlayString(&dst.CerebrasAPIKey, src.CerebrasAPIKey)
	overlayString(&dst.OpenRouterAPIKey, src.OpenRouterAPIKey)
	overlayString(&dst.OpenRouterModel, src.OpenRouterModel)
	overlayString(&dst.GroqAPIKey, src.GroqAPIKey)
	overlayString(&dst.OpenAIAPIKey, src.OpenAIAPIKey)
	overlayString(&dst.GeminiAPIKey, src.GeminiAPIKey)
	overlayString(&dst.AnthropicAPIKey, src.AnthropicAPIKey)
	overlayString(&dst.CerebrasModel, src.CerebrasModel)
	overlayString(&dst.AtomicAPIKey, src.AtomicAPIKey)
	overlayString(&dst.AtomicEndpoint, src.AtomicEndpoint)
	overlayString(&dst.AtomicModel, src.AtomicModel)
	overlayString(&dst.ElevenLabsAPIKey, src.ElevenLabsAPIKey)
	overlayString(&dst.BedrockRegion, src.BedrockRegion)
}

func overlayEmbeddingSettings(dst *EmbeddingSettings, src EmbeddingSettings) {
	if dst == nil {
		return
	}
	overlayString := func(target *string, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			*target = value
		}
	}
	overlayString(&dst.Provider, src.Provider)
	overlayString(&dst.Model, src.Model)
	overlayString(&dst.BaseURL, src.BaseURL)
	overlayString(&dst.APIKey, src.APIKey)
	if src.Dimensions > 0 {
		dst.Dimensions = src.Dimensions
	}
	if len(src.Models) > 0 {
		if dst.Models == nil {
			dst.Models = map[string]string{}
		}
		for key, value := range src.Models {
			value = strings.TrimSpace(value)
			if strings.TrimSpace(key) == "" || value == "" {
				continue
			}
			dst.Models[key] = value
		}
	}
	if src.Flags.QueryMode != "" {
		dst.Flags.QueryMode = src.Flags.QueryMode
	}
	if src.Flags.SymbolTextMode != "" {
		dst.Flags.SymbolTextMode = src.Flags.SymbolTextMode
	}
	if src.Flags.FileTextMode != "" {
		dst.Flags.FileTextMode = src.Flags.FileTextMode
	}
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
	cfg.Logging.Output = strings.TrimSpace(cfg.Logging.Output)
	switch strings.ToLower(cfg.Logging.Output) {
	case "", "stderr", "stdout":
		// keep special stream identifiers for logging.OpenWriter.
	default:
		cfg.Logging.Output = resolvePath(cfg.Logging.Output, cfg.Home, home)
	}

	// Database/Turso: Allow standard Turso env vars as overrides
	if url := os.Getenv("TURSO_DATABASE_URL"); url != "" && cfg.Database.Turso.URL == "" {
		cfg.Database.Turso.URL = url
	}
	if token := os.Getenv("TURSO_AUTH_TOKEN"); token != "" && cfg.Database.Turso.AuthToken == "" {
		cfg.Database.Turso.AuthToken = token
	}

	// PostgreSQL env var overrides
	if dsn := os.Getenv("FOXCTL_POSTGRES_DSN"); dsn != "" && cfg.Database.Postgres.DSN == "" {
		cfg.Database.Postgres.DSN = dsn
	}
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" && cfg.Database.Postgres.DSN == "" {
		cfg.Database.Postgres.DSN = dsn
	}
	if v := os.Getenv("FOXCTL_POSTGRES_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Database.Postgres.MaxOpenConns = n
		}
	}
	if strings.EqualFold(os.Getenv("FOXCTL_POSTGRES_REQUIRE_VECTOR"), "true") {
		cfg.Database.Postgres.RequireVector = true
	}

	// Auto-detect driver from URL/DSN if not explicitly set
	if cfg.Database.Driver == "sqlite" && cfg.Database.Turso.URL != "" {
		cfg.Database.Driver = "turso"
	}
	if cfg.Database.Driver == "sqlite" && cfg.Database.Postgres.DSN != "" {
		cfg.Database.Driver = "postgres"
	}
	// Explicit driver override via env var takes precedence over auto-detection
	if driver := os.Getenv("FOXCTL_DB_DRIVER"); driver != "" {
		cfg.Database.Driver = driver
	}
	// Default vector dimensions from embedding config if not set
	if cfg.Database.Vector.Dimensions == 0 {
		cfg.Database.Vector.Dimensions = cfg.Embedding.Dimensions
	}

	// Default-enable observability: if FOXCTL_OBS_DIR is not set but
	// cfg.Paths.Observability is configured, set the env var so all
	// downstream code (skills, CLI, daemon) inherits the path.
	if cfg.Paths.Observability != "" {
		obsEnv := os.Getenv("FOXCTL_OBS_DIR")
		// NOTE: We normally respect explicit env overrides. However, it is common to
		// end up with a stale absolute home directory after a username change (e.g.
		// FOXCTL_OBS_DIR="/Users/olduser/..."). When that home no longer exists,
		// observability becomes noisy (permission errors) and effectively broken.
		if obsEnv == "" || shouldRepairObsDir(obsEnv, home) {
			// Best-effort: ignore errors since observability is non-critical
			_ = os.Setenv("FOXCTL_OBS_DIR", cfg.Paths.Observability)
		}
	}

	// CAS policy env var overrides
	if storeEnv := os.Getenv("FOXCTL_CAS_STORE"); storeEnv != "" {
		cfg.CAS.Store = storeEnv == "1" || strings.EqualFold(storeEnv, "true")
	}
	if exposeEnv := os.Getenv("FOXCTL_CAS_EXPOSE"); exposeEnv != "" {
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
	if key := os.Getenv("FOXCTL_LLM_API_KEY"); key != "" && cfg.LLM.APIKey == "" {
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
	if provider := os.Getenv("FOXCTL_LLM_PROVIDER"); provider != "" && cfg.LLM.Provider == "" {
		cfg.LLM.Provider = provider
	}
	if model := os.Getenv("FOXCTL_LLM_MODEL"); model != "" && cfg.LLM.Model == "" {
		cfg.LLM.Model = model
	}
	if baseURL := os.Getenv("FOXCTL_LLM_BASE_URL"); baseURL != "" && cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = strings.TrimSpace(baseURL)
	}
	if mode := os.Getenv("FOXCTL_LLM_AUTH_MODE"); mode != "" && cfg.LLM.AuthMode == "" {
		cfg.LLM.AuthMode = strings.TrimSpace(mode)
	}
	if header := os.Getenv("FOXCTL_LLM_AUTH_HEADER"); header != "" && cfg.LLM.AuthHeader == "" {
		cfg.LLM.AuthHeader = strings.TrimSpace(header)
	}
	if prefix := os.Getenv("FOXCTL_LLM_AUTH_PREFIX"); prefix != "" && cfg.LLM.AuthPrefix == "" {
		cfg.LLM.AuthPrefix = prefix
	}
	if region := os.Getenv("BEDROCK_REGION"); region != "" && cfg.LLM.BedrockRegion == "" {
		cfg.LLM.BedrockRegion = region
	}
	if cfg.LLM.BedrockRegion == "" {
		if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
			cfg.LLM.BedrockRegion = region
		}
	}

	// Atomic processing config (for SimpleMem-style fact decomposition)
	if key := os.Getenv("FOXCTL_ATOMIC_API_KEY"); key != "" && cfg.LLM.AtomicAPIKey == "" {
		cfg.LLM.AtomicAPIKey = key
	}
	// Fallback: use OpenRouter key if atomic key not set
	if cfg.LLM.AtomicAPIKey == "" && cfg.LLM.OpenRouterAPIKey != "" {
		cfg.LLM.AtomicAPIKey = cfg.LLM.OpenRouterAPIKey
	}
	if endpoint := os.Getenv("FOXCTL_ATOMIC_ENDPOINT"); endpoint != "" && cfg.LLM.AtomicEndpoint == "" {
		cfg.LLM.AtomicEndpoint = endpoint
	}
	if model := os.Getenv("FOXCTL_ATOMIC_MODEL"); model != "" && cfg.LLM.AtomicModel == "" {
		cfg.LLM.AtomicModel = model
	}

	// Embedding API key env var overrides (FC/IS compliant)
	if provider := os.Getenv("FOXCTL_EMBEDDING_PROVIDER"); provider != "" {
		cfg.Embedding.Provider = strings.TrimSpace(provider)
	}
	if model := os.Getenv("FOXCTL_EMBEDDING_MODEL"); model != "" {
		cfg.Embedding.Model = strings.TrimSpace(model)
	}
	if baseURL := os.Getenv("FOXCTL_EMBEDDING_BASE_URL"); baseURL != "" {
		cfg.Embedding.BaseURL = strings.TrimSpace(baseURL)
	}
	if key := os.Getenv("FOXCTL_EMBEDDING_API_KEY"); key != "" {
		cfg.Embedding.APIKey = key
	}
	// Backward-compatible aliases for OpenAI-compatible embedding endpoints.
	if model := os.Getenv("FOXCTL_OPENAI_COMPAT_EMBEDDING_MODEL"); model != "" && cfg.Embedding.Model == "" {
		cfg.Embedding.Model = strings.TrimSpace(model)
	}
	if baseURL := os.Getenv("FOXCTL_OPENAI_COMPAT_BASE_URL"); baseURL != "" && cfg.Embedding.BaseURL == "" {
		cfg.Embedding.BaseURL = strings.TrimSpace(baseURL)
	}
	if key := os.Getenv("FOXCTL_OPENAI_COMPAT_API_KEY"); key != "" && cfg.Embedding.APIKey == "" {
		cfg.Embedding.APIKey = strings.TrimSpace(key)
	}
	if cfg.Embedding.Provider == "" && (cfg.Embedding.BaseURL != "" || strings.EqualFold(os.Getenv("FOXCTL_OBSIDIAN_SEMANTIC_PROVIDER"), "openai_compat") || strings.EqualFold(os.Getenv("FOXCTL_OBSIDIAN_SEMANTIC_PROVIDER"), "openai-compatible")) {
		cfg.Embedding.Provider = "openai_compat"
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

	// Discord chat adapter env var overrides
	if token := os.Getenv("DISCORD_BOT_TOKEN"); token != "" && cfg.Discord.BotToken == "" {
		cfg.Discord.BotToken = token
	}
	if guildID := os.Getenv("DISCORD_GUILD_ID"); guildID != "" && cfg.Discord.GuildID == "" {
		cfg.Discord.GuildID = guildID
	}
	if ch := os.Getenv("DISCORD_ACTIVITY_CHANNEL"); ch != "" && cfg.Discord.ActivityChannelID == "" {
		cfg.Discord.ActivityChannelID = ch
	}
	if ch := os.Getenv("DISCORD_AGENT_CHANNEL"); ch != "" && cfg.Discord.AgentChannelID == "" {
		cfg.Discord.AgentChannelID = ch
	}
	if channels := os.Getenv("DISCORD_CHAT_CHANNELS"); channels != "" && len(cfg.Discord.ChatChannelIDs) == 0 {
		for _, ch := range strings.Split(channels, ",") {
			ch = strings.TrimSpace(ch)
			if ch != "" {
				cfg.Discord.ChatChannelIDs = append(cfg.Discord.ChatChannelIDs, ch)
			}
		}
	}
	if profile := os.Getenv("DISCORD_CHAT_PROFILE"); profile != "" && cfg.Discord.ChatProfile == "" {
		cfg.Discord.ChatProfile = profile
	}
	if cfg.Discord.ChatProfile == "" {
		cfg.Discord.ChatProfile = "explorer"
	}
	if prompt := os.Getenv("DISCORD_CHAT_SYSTEM_PROMPT"); prompt != "" && cfg.Discord.ChatSystemPrompt == "" {
		cfg.Discord.ChatSystemPrompt = prompt
	}

	// Telegram chat adapter env var overrides
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" && cfg.Telegram.BotToken == "" {
		cfg.Telegram.BotToken = token
	}
	if chatIDStr := os.Getenv("TELEGRAM_ACTIVITY_CHAT_ID"); chatIDStr != "" && cfg.Telegram.ActivityChatID == 0 {
		if id, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64); err == nil {
			cfg.Telegram.ActivityChatID = id
		}
	}
	if chatIDStr := os.Getenv("TELEGRAM_AGENT_CHAT_ID"); chatIDStr != "" && cfg.Telegram.AgentChatID == 0 {
		if id, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64); err == nil {
			cfg.Telegram.AgentChatID = id
		}
	}
	if chatIDs := os.Getenv("TELEGRAM_CHAT_IDS"); chatIDs != "" && len(cfg.Telegram.ChatIDs) == 0 {
		for _, s := range strings.Split(chatIDs, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				cfg.Telegram.ChatIDs = append(cfg.Telegram.ChatIDs, id)
			}
		}
	}
	if profile := os.Getenv("TELEGRAM_CHAT_PROFILE"); profile != "" && cfg.Telegram.ChatProfile == "" {
		cfg.Telegram.ChatProfile = profile
	}
	if cfg.Telegram.ChatProfile == "" {
		cfg.Telegram.ChatProfile = "explorer"
	}
	if prompt := os.Getenv("TELEGRAM_CHAT_SYSTEM_PROMPT"); prompt != "" && cfg.Telegram.ChatSystemPrompt == "" {
		cfg.Telegram.ChatSystemPrompt = prompt
	}
	if s := os.Getenv("TELEGRAM_MAX_CONCURRENT_MESSAGES"); s != "" && cfg.Telegram.MaxConcurrentMessages == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			cfg.Telegram.MaxConcurrentMessages = n
		}
	}
	if cfg.Telegram.MaxConcurrentMessages <= 0 {
		cfg.Telegram.MaxConcurrentMessages = 10
	}
	if s := os.Getenv("TELEGRAM_EDIT_INTERVAL_MS"); s != "" && cfg.Telegram.EditIntervalMS == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			cfg.Telegram.EditIntervalMS = n
		}
	}
	if cfg.Telegram.EditIntervalMS <= 0 {
		cfg.Telegram.EditIntervalMS = 1500
	}

	// Microsoft Teams chat adapter env var overrides
	if v := os.Getenv("TEAMS_TENANT_ID"); v != "" && cfg.Teams.TenantID == "" {
		cfg.Teams.TenantID = strings.TrimSpace(v)
	}
	if v := os.Getenv("TEAMS_CLIENT_ID"); v != "" && cfg.Teams.ClientID == "" {
		cfg.Teams.ClientID = strings.TrimSpace(v)
	}
	if v := os.Getenv("TEAMS_CLIENT_SECRET"); v != "" && cfg.Teams.ClientSecret == "" {
		cfg.Teams.ClientSecret = strings.TrimSpace(v)
	}
	if v := os.Getenv("TEAMS_CHAT_CONVERSATION_IDS"); v != "" && len(cfg.Teams.ChatConversationIDs) == 0 {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.Teams.ChatConversationIDs = append(cfg.Teams.ChatConversationIDs, s)
			}
		}
	}
	if v := os.Getenv("TEAMS_CHAT_PROFILE"); v != "" && cfg.Teams.ChatProfile == "" {
		cfg.Teams.ChatProfile = strings.TrimSpace(v)
	}
	if cfg.Teams.ChatProfile == "" {
		cfg.Teams.ChatProfile = "explorer"
	}
	if v := os.Getenv("TEAMS_CHAT_SYSTEM_PROMPT"); v != "" && cfg.Teams.ChatSystemPrompt == "" {
		cfg.Teams.ChatSystemPrompt = v
	}
	if v := os.Getenv("TEAMS_MAX_CONCURRENT_MESSAGES"); v != "" && cfg.Teams.MaxConcurrentMessages == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Teams.MaxConcurrentMessages = n
		}
	}
	if cfg.Teams.MaxConcurrentMessages <= 0 {
		cfg.Teams.MaxConcurrentMessages = 10
	}
	if v := os.Getenv("TEAMS_EDIT_INTERVAL_MS"); v != "" && cfg.Teams.EditIntervalMS == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Teams.EditIntervalMS = n
		}
	}
	if cfg.Teams.EditIntervalMS <= 0 {
		cfg.Teams.EditIntervalMS = 1500
	}
	if v := os.Getenv("TEAMS_SKIP_JWT_VERIFY"); v != "" && !cfg.Teams.SkipJWTVerify {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			cfg.Teams.SkipJWTVerify = b
		}
	}
	if v := os.Getenv("TEAMS_AGENT_CONVERSATION_ID"); v != "" && cfg.Teams.AgentConversationID == "" {
		cfg.Teams.AgentConversationID = strings.TrimSpace(v)
	}
	if v := os.Getenv("TEAMS_ACTIVITY_FEED_CONVERSATION_ID"); v != "" && cfg.Teams.ActivityFeedConversationID == "" {
		cfg.Teams.ActivityFeedConversationID = strings.TrimSpace(v)
	}

	// OAuth AuthBroker
	if v := os.Getenv("FOXCTL_OAUTH_ENC_KEY"); v != "" && cfg.OAuth.EncryptionKey == "" {
		cfg.OAuth.EncryptionKey = v
	}
	if v := os.Getenv("FOXCTL_OAUTH_CALLBACK_URL"); v != "" && cfg.OAuth.CallbackBaseURL == "" {
		cfg.OAuth.CallbackBaseURL = v
	}
	// Microsoft Graph
	if v := os.Getenv("MICROSOFT_CLIENT_ID"); v != "" && cfg.OAuth.MicrosoftClientID == "" {
		cfg.OAuth.MicrosoftClientID = v
	}
	if v := os.Getenv("MICROSOFT_CLIENT_SECRET"); v != "" && cfg.OAuth.MicrosoftClientSecret == "" {
		cfg.OAuth.MicrosoftClientSecret = v
	}
	if v := os.Getenv("MICROSOFT_TENANT_ID"); v != "" && cfg.OAuth.MicrosoftTenantID == "" {
		cfg.OAuth.MicrosoftTenantID = v
	}
	// Google
	if v := os.Getenv("GOOGLE_CLIENT_ID"); v != "" && cfg.OAuth.GoogleClientID == "" {
		cfg.OAuth.GoogleClientID = v
	}
	if v := os.Getenv("GOOGLE_CLIENT_SECRET"); v != "" && cfg.OAuth.GoogleClientSecret == "" {
		cfg.OAuth.GoogleClientSecret = v
	}
	// GitHub
	if v := os.Getenv("GITHUB_CLIENT_ID"); v != "" && cfg.OAuth.GitHubClientID == "" {
		cfg.OAuth.GitHubClientID = v
	}
	if v := os.Getenv("GITHUB_CLIENT_SECRET"); v != "" && cfg.OAuth.GitHubClientSecret == "" {
		cfg.OAuth.GitHubClientSecret = v
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
	//   obsEnv:   /Users/olduser/.foxctl/observability
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
