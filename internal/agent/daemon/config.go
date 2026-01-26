package daemon

import (
	"context"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/domain/agent"
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

// Options configures the agent daemon.
type Options struct {
	AgentID     string
	StorageRoot string
	// WorkspaceRoot is the absolute path used for repo index and tool workspace access.
	WorkspaceRoot     string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	MaxPollMessages   int
	UseMemoryDedupe   bool

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

	// AgentFactory allows injecting a custom agent for testing.
	AgentFactory func(context.Context, agent.Agent, *tools.Registry) (agents.Agent, error)

	// EnableCompanionMemory enables L0/L1/L2 conversation memory for agents.
	// When enabled, the daemon injects memory context into prompts and stores turns.
	EnableCompanionMemory bool

	// CompanionMode selects the memory configuration for conversation memory.
	// Values: "" (default), "standard", "roleplay"
	// - "standard": Default memory config (40K tokens, 24h vivid window)
	// - "roleplay": Extended memory for roleplay/chat (50K tokens, 48h vivid window)
	// Only applies when EnableCompanionMemory is true.
	CompanionMode string
}
