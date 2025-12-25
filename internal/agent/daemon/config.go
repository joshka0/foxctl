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
	AgentID           string
	StorageRoot       string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	MaxPollMessages   int
	UseMemoryDedupe   bool

	// EnableOptimization enables online pattern learning for tool selection hints.
	// When enabled, the daemon records tool usage patterns and provides hints
	// for similar tasks based on past successful executions.
	EnableOptimization bool

	// AgentFactory allows injecting a custom agent for testing.
	AgentFactory func(context.Context, agent.Agent, *tools.Registry) (agents.Agent, error)
}
