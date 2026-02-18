package spawn

import "time"

// Request is the canonical input for v2 spawn orchestration.
type Request struct {
	RequestID string `json:"request_id,omitempty"`
	Role      string `json:"role"`
	Prompt    string `json:"prompt,omitempty"`
	ExecMode  string `json:"exec_mode,omitempty"`

	RunID         string `json:"run_id,omitempty"`
	LegacyRunID   string `json:"legacy_run_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	LegacyAgentID string `json:"legacy_agent_id,omitempty"`
	ActorID       string `json:"actor_id,omitempty"`

	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`

	MaxIterations    int `json:"max_iterations,omitempty"`
	MaxContextTokens int `json:"max_context_tokens,omitempty"`
	MaxAutoTurns     int `json:"max_auto_turns,omitempty"`
}

// Response is the canonical spawn service output.
type Response struct {
	RunID        string    `json:"run_id"`
	AgentID      string    `json:"agent_id"`
	ActorID      string    `json:"actor_id"`
	TurnID       string    `json:"turn_id,omitempty"`
	RequestID    string    `json:"request_id,omitempty"`
	Status       string    `json:"status"`
	Summary      string    `json:"summary,omitempty"`
	Iterations   int       `json:"iterations,omitempty"`
	ToolCalls    int       `json:"tool_calls,omitempty"`
	Degraded     bool      `json:"degraded,omitempty"`
	Idempotent   bool      `json:"idempotent,omitempty"`
	MappedLegacy bool      `json:"mapped_legacy,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
