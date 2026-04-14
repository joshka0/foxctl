package jido

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	SignalModeCall = "call"
	SignalModeCast = "cast"
)

const (
	MethodRuntimeHealth      = "runtime.health"
	MethodRuntimeStartAgent  = "runtime.start_agent"
	MethodRuntimeStopAgent   = "runtime.stop_agent"
	MethodRuntimeSignal      = "runtime.signal"
	MethodRuntimeSpawnChild  = "runtime.spawn_child"
	MethodRuntimeAwait       = "runtime.await"
	MethodRuntimeGetChildren = "runtime.get_children"
	MethodRuntimeState       = "runtime.state"
)

const (
	DefaultSignalSource = "/foxctl/v2"
	DefaultAskSignal    = "agent.ask"
)

// Signal is the bridge-level event envelope sent to a Jido runtime.
type Signal struct {
	ID            string          `json:"id,omitempty"`
	Type          string          `json:"type"`
	Source        string          `json:"source"`
	Subject       string          `json:"subject,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	Data          json.RawMessage `json:"data,omitempty"`
	Metadata      map[string]any  `json:"metadata,omitempty"`
}

// StartAgentRequest starts one runtime agent process.
type StartAgentRequest struct {
	RequestID       string          `json:"request_id,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	AgentID         string          `json:"agent_id"`
	Profile         string          `json:"profile,omitempty"`
	MemoryRetention string          `json:"memory_retention,omitempty"`
	ExecMode        string          `json:"exec_mode,omitempty"`
	ThinkInterval   int             `json:"think_interval,omitempty"`
	InitialState    json.RawMessage `json:"initial_state,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

// StartAgentResponse contains the runtime start result.
type StartAgentResponse struct {
	AgentID  string         `json:"agent_id"`
	PID      string         `json:"pid,omitempty"`
	Status   string         `json:"status"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// StopAgentRequest stops one runtime agent process.
type StopAgentRequest struct {
	RequestID      string `json:"request_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	AgentID        string `json:"agent_id"`
	Reason         string `json:"reason,omitempty"`
}

// StopAgentResponse contains the runtime stop result.
type StopAgentResponse struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

// SignalRequest delivers one signal to a runtime agent.
type SignalRequest struct {
	RequestID      string `json:"request_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	AgentID        string `json:"agent_id"`
	Signal         Signal `json:"signal"`
	Mode           string `json:"mode,omitempty"`
	TimeoutMS      int64  `json:"timeout_ms,omitempty"`
}

// Timeout converts timeout_ms to time.Duration.
func (r SignalRequest) Timeout() time.Duration {
	if r.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(r.TimeoutMS) * time.Millisecond
}

// SignalResponse contains runtime dispatch acknowledgement.
type SignalResponse struct {
	AgentID   string          `json:"agent_id"`
	MessageID string          `json:"message_id,omitempty"`
	SignalID  string          `json:"signal_id,omitempty"`
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// AwaitRequest waits for agent completion/result.
type AwaitRequest struct {
	RequestID string `json:"request_id,omitempty"`
	AgentID   string `json:"agent_id"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

// AwaitResponse is one await result.
type AwaitResponse struct {
	AgentID  string          `json:"agent_id"`
	Status   string          `json:"status"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    map[string]any  `json:"error,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// GetChildrenRequest queries tracked children for one parent.
type GetChildrenRequest struct {
	RequestID string `json:"request_id,omitempty"`
	AgentID   string `json:"agent_id"`
}

// ChildRef identifies one runtime child.
type ChildRef struct {
	Tag      string         `json:"tag"`
	AgentID  string         `json:"agent_id,omitempty"`
	PID      string         `json:"pid,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GetChildrenResponse returns children by tag.
type GetChildrenResponse struct {
	AgentID  string              `json:"agent_id"`
	Children map[string]ChildRef `json:"children,omitempty"`
}

// StateRequest queries runtime state for one agent.
type StateRequest struct {
	RequestID string `json:"request_id,omitempty"`
	AgentID   string `json:"agent_id"`
}

// StateResponse returns runtime state payload.
type StateResponse struct {
	AgentID string          `json:"agent_id"`
	State   json.RawMessage `json:"state,omitempty"`
	Status  string          `json:"status,omitempty"`
}

// HealthResponse contains runtime health details.
type HealthResponse struct {
	Status  string         `json:"status"`
	Runtime string         `json:"runtime,omitempty"`
	Version string         `json:"version,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// NormalizeSignalMode applies deterministic runtime defaults.
func NormalizeSignalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SignalModeCast:
		return SignalModeCast
	case SignalModeCall:
		fallthrough
	default:
		return SignalModeCall
	}
}
