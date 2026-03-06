package agent

import (
	"encoding/json"
	"time"
)

// State represents the lifecycle state of an agent.
type State string

const (
	// StateStarting indicates agent is being initialized.
	StateStarting State = "starting"
	// StateRunning indicates agent is active and processing.
	StateRunning State = "running"
	// StateStopped indicates agent has been gracefully stopped.
	StateStopped State = "stopped"
	// StateError indicates agent encountered a fatal error.
	StateError State = "error"
)

// ExecutionMode defines how an agent processes work.
type ExecutionMode string

const (
	// ModeReactive means agent only responds to incoming messages (default).
	ModeReactive ExecutionMode = "reactive"
	// ModeAutonomous means agent can continue working across multiple turns.
	ModeAutonomous ExecutionMode = "autonomous"
	// ModeAutonomousReactive means agent runs autonomous turns first, then remains alive in reactive mailbox mode.
	ModeAutonomousReactive ExecutionMode = "autonomous_reactive"
	// ModeProactive means agent can initiate work on its own.
	ModeProactive ExecutionMode = "proactive"
	// ModeTick means agent executes one interval-driven tick at a fixed cadence.
	ModeTick ExecutionMode = "tick"
	// ModeStory means agent runs a gather + dialogue story loop.
	ModeStory ExecutionMode = "story"
)

// MemoryScope defines how a human-facing agent workbench should scope memory lineage.
type MemoryScope string

const (
	// MemoryScopeAgent uses the agent's stable canonical conversation lineage.
	MemoryScopeAgent MemoryScope = "agent"
	// MemoryScopeSession uses a detached workbench session lineage rather than the canonical agent thread.
	MemoryScopeSession MemoryScope = "session"
)

// NormalizeMemoryScope applies stable defaults for persisted agent metadata.
func NormalizeMemoryScope(scope MemoryScope) MemoryScope {
	switch scope {
	case MemoryScopeSession:
		return MemoryScopeSession
	case MemoryScopeAgent:
		fallthrough
	default:
		return MemoryScopeAgent
	}
}

// MemoryRetention defines the expected retention profile for an agent's memory.
type MemoryRetention string

const (
	// MemoryRetentionCompanion is for long-lived companion-style agents with durable layered memory.
	MemoryRetentionCompanion MemoryRetention = "companion"
	// MemoryRetentionDurable is for long-running agents that should keep stable memory but are not persona-driven companions.
	MemoryRetentionDurable MemoryRetention = "durable"
	// MemoryRetentionTask is for task-scoped worker memory that is useful during a unit of work.
	MemoryRetentionTask MemoryRetention = "task"
	// MemoryRetentionEphemeral is for scratch agents that should stay mostly detached.
	MemoryRetentionEphemeral MemoryRetention = "ephemeral"
)

// NormalizeMemoryRetention applies stable defaults for persisted retention metadata.
func NormalizeMemoryRetention(retention MemoryRetention) MemoryRetention {
	switch retention {
	case MemoryRetentionCompanion, MemoryRetentionDurable, MemoryRetentionTask, MemoryRetentionEphemeral:
		return retention
	default:
		return MemoryRetentionDurable
	}
}

// DefaultMemoryRetentionForScope maps older scope-only records onto the newer retention presets.
func DefaultMemoryRetentionForScope(scope MemoryScope) MemoryRetention {
	switch NormalizeMemoryScope(scope) {
	case MemoryScopeSession:
		return MemoryRetentionTask
	default:
		return MemoryRetentionDurable
	}
}

// RecommendedMemoryScopeForRetention returns the default lineage scope for a retention preset.
func RecommendedMemoryScopeForRetention(retention MemoryRetention) MemoryScope {
	switch NormalizeMemoryRetention(retention) {
	case MemoryRetentionTask, MemoryRetentionEphemeral:
		return MemoryScopeSession
	default:
		return MemoryScopeAgent
	}
}

// Agent represents an autonomous actor in the multi-agent system.
type Agent struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parent_id,omitempty"`
	Namespace   string    `json:"ns"`
	Name        string    `json:"name,omitempty"` // Human name (e.g., "Luna", "Atlas")
	Slug        string    `json:"slug,omitempty"` // Human-readable handle for referencing (e.g., "researcher", "companion")
	Role        string    `json:"role,omitempty"`
	Prompt      string    `json:"prompt,omitempty"`
	SkillsAllow []string  `json:"skills_allow"`
	Policy      Policy    `json:"policy"`
	ShareBB     string    `json:"share_bb"` // all|scoped|none
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	HeartbeatAt time.Time `json:"heartbeat_at,omitempty"`

	// LLM configuration (per-agent, overrides environment defaults)
	LLMProvider string `json:"llm_provider,omitempty"` // gemini|openai|anthropic|groq|openrouter
	LLMModel    string `json:"llm_model,omitempty"`    // Model ID (e.g., claude-haiku-4-5)
	LLMAPIKey   string `json:"llm_api_key,omitempty"`  // API key (or env var name like $GROQ_API_KEY)

	// Execution mode configuration
	ExecMode      ExecutionMode `json:"exec_mode,omitempty"`      // reactive|autonomous|autonomous_reactive|proactive|tick|story (default: reactive)
	MaxIterations int           `json:"max_iterations,omitempty"` // Max tool calls per turn (default: 10)
	MaxAutoTurns  int           `json:"max_auto_turns,omitempty"` // Max autonomous turns per session (default: 1)
	ThinkInterval int           `json:"think_interval,omitempty"` // Seconds between proactive/tick cycles (default: 60)

	// Linked companion conversation
	ConversationID  string          `json:"conversation_id,omitempty"`  // Linked companion conversation ID for chat history
	MemoryScope     MemoryScope     `json:"memory_scope,omitempty"`     // agent|session for human-facing memory lineage
	MemoryRetention MemoryRetention `json:"memory_retention,omitempty"` // companion|durable|task|ephemeral retention preset
}

// Policy defines execution constraints and capabilities for an agent.
type Policy struct {
	CPU         int                `json:"cpu,omitempty"`
	MemoryMB    int                `json:"memMB,omitempty"`
	Timeout     string             `json:"timeout,omitempty"` // Duration string like "20m"
	Network     string             `json:"network,omitempty"` // none|egress
	EgressAllow []string           `json:"egressAllow,omitempty"`
	MaxOutputKB int                `json:"max_output_kb,omitempty"`
	EnvAllow    []string           `json:"envAllow,omitempty"`
	Secrets     []string           `json:"secrets,omitempty"`
	Filesystem  []FilesystemPolicy `json:"filesystem,omitempty"`
}

// FilesystemPolicy defines filesystem access rules.
type FilesystemPolicy struct {
	Type string `json:"type"` // workdir|ro
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// Quotas defines resource limits for a namespace.
type Quotas struct {
	Namespace         string `json:"ns"`
	MaxConcurrentJobs int    `json:"max_concurrent_jobs,omitempty"`
	CPULimit          int    `json:"cpu_limit,omitempty"`
	MemMBLimit        int    `json:"memMB_limit,omitempty"`
	LLMCallsPerMin    int    `json:"llm_calls_per_min,omitempty"`
	EgressBytesPerMin int    `json:"egress_bytes_per_min,omitempty"`
}

// QuotaConsumption tracks current resource usage for a namespace.
type QuotaConsumption struct {
	Namespace       string `json:"ns"`
	ActiveJobs      int    `json:"active_jobs"`
	CPUUsed         int    `json:"cpu_used"`
	MemMBUsed       int    `json:"memMB_used"`
	LLMCalls1Min    int    `json:"llm_calls_1min"`
	EgressBytes1Min int    `json:"egress_bytes_1min"`
	LastResetTS     int64  `json:"last_reset_ts"`
}

// MarshalPolicyJSON serializes a policy to JSON bytes.
func MarshalPolicyJSON(p Policy) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalPolicyJSON deserializes a policy from JSON bytes.
func UnmarshalPolicyJSON(data []byte) (Policy, error) {
	var p Policy
	err := json.Unmarshal(data, &p)
	return p, err
}
