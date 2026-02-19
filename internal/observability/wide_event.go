package observability

import (
	"time"
)

// WideEvent represents a comprehensive observability event for a single operation.
// Unlike narrow events (e.g., SweGrepEvent), wide events capture the full context
// of an operation including identity, timing, outcome, and domain-specific data.
//
// Design principles (from loggingsucks.com):
//   - Log what happened to the request, not what your code does
//   - One event per operation with all potentially useful debugging info
//   - Include business context alongside technical details
type WideEvent struct {
	// Identity - who/what/when
	Ts       time.Time `json:"ts"`                  // Event timestamp (UTC)
	TraceID  string    `json:"trace_id"`            // ULID correlating all events in an operation
	SpanID   string    `json:"span_id"`             // ULID for this specific event
	ParentID string    `json:"parent_id,omitempty"` // Parent span for nested operations

	// Service metadata
	Service   string `json:"service"`   // "agentctl"
	Version   string `json:"version"`   // Build version
	Component string `json:"component"` // "cli", "web", "hook", "skill"

	// Operation context
	Operation string `json:"operation"`         // "skill.run", "hook.execute", etc.
	Command   string `json:"command,omitempty"` // Skill/hook/command name
	Subtype   string `json:"subtype,omitempty"` // Additional classification

	// Business context
	SessionID   string `json:"session_id,omitempty"`   // AGENTCTL_SESSION_ID
	AgentID     string `json:"agent_id,omitempty"`     // AGENTCTL_AGENT_ID
	WorkspaceID string `json:"workspace_id,omitempty"` // Logical workspace
	JobID       string `json:"job_id,omitempty"`       // Background job ID

	// Outcome
	Status     Status `json:"status"`      // ok, error, canceled
	DurationMS int64  `json:"duration_ms"` // Wall-clock duration

	// Error details (populated when Status == StatusError)
	ErrorType    string `json:"error_type,omitempty"`    // Error category (e.g., "validation", "timeout")
	ErrorCode    string `json:"error_code,omitempty"`    // Machine-readable error code
	ErrorMessage string `json:"error_message,omitempty"` // Human-readable message (redacted)
	Retriable    *bool  `json:"retriable,omitempty"`     // Whether the operation can be retried

	// Domain-specific data (extensible)
	// Use for skill-specific metrics, counts, booleans, hashes - never raw content
	Data map[string]any `json:"data,omitempty"`
}

// Status represents the outcome of an operation.
type Status string

const (
	// StatusOK indicates the operation completed successfully.
	StatusOK Status = "ok"
	// StatusError indicates the operation failed.
	StatusError Status = "error"
	// StatusCanceled indicates the operation was canceled (e.g., context canceled).
	StatusCanceled Status = "canceled"
)

// Component constants for consistent tagging.
const (
	ComponentCLI   = "cli"
	ComponentWeb   = "web"
	ComponentHook  = "hook"
	ComponentSkill = "skill"
	ComponentJob   = "job"
	ComponentAgent = "agent"
	// ComponentContextBuilder identifies layered context assembly and retrieval operations.
	ComponentContextBuilder = "contextbuilder"
)

// Operation constants for common operations.
const (
	OpSkillRun     = "skill.run"
	OpSkillCache   = "skill.cache"
	OpHookExecute  = "hook.execute"
	OpJobSubmit    = "job.submit"
	OpJobComplete  = "job.complete"
	OpHTTPRequest  = "http.request"
	OpSessionStart = "session.start"
	OpSessionEnd   = "session.end"

	// OpAgentSpawn is the operation name for agent spawn.
	OpAgentSpawn     = "agent.spawn"
	OpAgentWait      = "agent.wait"
	OpAgentComplete  = "agent.complete"
	OpAgentKill      = "agent.kill"
	OpAgentIteration = "agent.iteration"
	OpAgentTool      = "agent.tool"

	// OpContextSemanticArtifactSearch is emitted for optional semantic artifact retrieval.
	OpContextSemanticArtifactSearch = "context.semantic_artifact_search"
)
