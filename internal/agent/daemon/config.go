package daemon

import (
	"context"
	"time"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/platform/config"
)

// OptimizationContext holds optimization components for handler use.
type OptimizationContext struct {
	// Collector records patterns and provides hints.
	Collector *optimization.MCPPatternCollector

	// Enabled indicates whether optimization is active.
	Enabled bool

	// AgentRole for pattern matching.
	AgentRole string

	// WorkspaceID scopes patterns to a workspace.
	WorkspaceID string
}

// ChatService defines the interface needed for daemon chat execution.
type ChatService interface {
	Chat(ctx context.Context, req companion.ChatRequest) (*companion.ChatResponse, error)
}

// Options configures the agent daemon runtime and companion services.
type Options struct {
	AgentID     string
	StorageRoot string
	// Config carries already-loaded process configuration into daemon-owned
	// background workers without reloading config from daemon internals.
	Config config.Config
	// WorkspaceRoot is the absolute path used for filesystem-bound tool access.
	WorkspaceRoot string
	// RepoIndexWorkspaceRoot optionally overrides the workspace key used to open
	// the repo index. This is useful when a sandbox mounts a repo at a different
	// guest path than the host path used to build the index.
	RepoIndexWorkspaceRoot string
	PollInterval           time.Duration
	HeartbeatInterval      time.Duration
	MaxPollMessages        int
	UseMemoryDedupe        bool

	// EnableOptimization enables online pattern learning for tool selection hints.
	// When enabled, the daemon records tool usage patterns and provides hints
	// for similar tasks based on past successful executions.
	EnableOptimization bool

	// LLMProvider is the LLM provider to use (e.g., "gemini", "openai", "anthropic").
	// Loaded from config at startup (FC/IS compliant - no os.Getenv in daemon core).
	LLMProvider string

	// LLMModel is the model name to use.
	LLMModel string

	// LLMAPIKey is the API key for the LLM provider.
	LLMAPIKey string

	// LLMBaseURL overrides the base URL for OpenAI-compatible/self-hosted backends.
	LLMBaseURL string

	// LLMAuthMode controls auth mode: auto, none, bearer, header.
	LLMAuthMode string

	// LLMAuthHeader names the header when auth mode is header.
	LLMAuthHeader string

	// LLMAuthPrefix prefixes the API key for bearer/header auth.
	LLMAuthPrefix string

	// EnableCompanionMemory enables L0/L1/L2 conversation memory for agents.
	// When enabled, the daemon injects memory context into prompts and stores turns.
	EnableCompanionMemory bool

	// CompanionMode selects the memory configuration for conversation memory.
	// Values: "" (default), "standard", "roleplay"
	// - "standard": Default memory config (40K tokens, 24h vivid window)
	// - "roleplay": Extended memory for roleplay/chat (50K tokens, 48h vivid window)
	// Only applies when EnableCompanionMemory is true.
	CompanionMode string

	// CompanionService overrides the default companion service (tests/injections).
	CompanionService ChatService

	// MemoryEmbedderFactory overrides memory embedding provider construction for
	// daemon memory queue drain tests.
	MemoryEmbedderFactory func(context.Context, config.Config) (embedding.MemoryEmbedder, int, error)
	// MemoryEmbeddingBatchSize bounds memory embedding queue jobs processed per
	// daemon poll tick. Defaults to a small value when zero.
	MemoryEmbeddingBatchSize int
	// MemoryEmbeddingRecoverStaleAfter requeues running memory jobs older than
	// this duration before each drain pass. Defaults conservatively when zero.
	MemoryEmbeddingRecoverStaleAfter time.Duration
}
