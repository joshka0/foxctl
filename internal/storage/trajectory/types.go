package trajectory

import (
	"time"
)

// Status represents the outcome of a trajectory.
type Status string

// Status constants per dspy_trajectory_capture.md §3.1.
const (
	StatusOK      Status = "ok"
	StatusError   Status = "error"
	StatusAborted Status = "aborted"
	StatusPartial Status = "partial"
)

// Outcome captures the final result of a trajectory for optimization.
// This data is used by the optimization system to learn from past executions.
type Outcome struct {
	// Success indicates whether the trajectory achieved its goal.
	Success bool `json:"success"`

	// TasksCompleted is the number of tasks completed during the trajectory.
	TasksCompleted int `json:"tasks_completed"`

	// ToolCallCount is the total number of tool calls made.
	ToolCallCount int `json:"tool_call_count"`

	// ErrorCount is the number of errors encountered.
	ErrorCount int `json:"error_count"`

	// DurationMS is the total execution time in milliseconds.
	DurationMS int64 `json:"duration_ms"`

	// HumanRating is an optional human-provided rating (1-5 scale).
	HumanRating *int `json:"human_rating,omitempty"`

	// Feedback is optional human-provided feedback text.
	Feedback string `json:"feedback,omitempty"`

	// Metrics holds arbitrary optimization metrics.
	Metrics map[string]float64 `json:"metrics,omitempty"`

	// RecordedAt is when the outcome was recorded.
	RecordedAt time.Time `json:"recorded_at,omitempty"`
}

// EventKind represents the type of trajectory event.
type EventKind string

// EventKind constants per dspy_trajectory_capture.md §3.3.
const (
	EventKindUserRequest    EventKind = "user_request"
	EventKindAgentThought   EventKind = "agent_thought"
	EventKindToolCall       EventKind = "tool_call"
	EventKindToolResult     EventKind = "tool_result"
	EventKindReviewRequest  EventKind = "review_request"
	EventKindReviewResult   EventKind = "review_result"
	EventKindTaskTransition EventKind = "task_transition"
	EventKindGraphSearch    EventKind = "graph_search"
	EventKindSWEGrep        EventKind = "swe_grep"
	EventKindHookCall       EventKind = "hook_call"
	EventKindHookResult     EventKind = "hook_result"
)

// Source indicates where a user request originated.
type Source string

// Source constants per dspy_trajectory_capture.md §3.2.
const (
	SourceCLI     Source = "cli"
	SourceMailbox Source = "mailbox"
	SourceAPI     Source = "api"
	SourceViewer  Source = "viewer"
)

// Trajectory is a logical record representing a coherent run or episode,
// usually anchored to a single user request and task.
// Per dspy_trajectory_capture.md §3.1.
type Trajectory struct {
	// ID is a ULID uniquely identifying the trajectory.
	ID string `json:"id"`

	// WorkspaceID scopes the trajectory to a workspace.
	WorkspaceID string `json:"workspace_id"`

	// RootRequestID links to the UserRequestCapture that started the trajectory.
	RootRequestID string `json:"root_request_id,omitempty"`

	// TaskIDs lists tasks touched by this trajectory.
	TaskIDs []string `json:"task_ids,omitempty"`

	// EpicID optionally links to an epic-level plan.
	EpicID string `json:"epic_id,omitempty"`

	// AgentRole identifies the agent type (coder, planner, reviewer, overseer).
	AgentRole string `json:"agent_role,omitempty"`

	// JobID links to the jobs entry for the main agent run.
	JobID string `json:"job_id,omitempty"`

	// TraceID is the Protocol v1 meta.trace_id linking all envelopes.
	TraceID string `json:"trace_id,omitempty"`

	// Status indicates the outcome: ok, error, aborted, partial.
	Status Status `json:"status"`

	// Summary is a human-readable description.
	Summary string `json:"summary,omitempty"`

	// ArtifactDigest is a CAS digest for the full trajectory payload if large.
	ArtifactDigest string `json:"artifact_digest,omitempty"`

	// Outcome captures the final result for optimization purposes.
	Outcome *Outcome `json:"outcome,omitempty"`

	// CreatedAt records when the trajectory was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt records the last update time.
	UpdatedAt time.Time `json:"updated_at"`

	// SessionID links to the AI coding tool session that created this trajectory.
	// Supports Claude Code, OpenCode, Cursor, and other tools via AGENTCTL_SESSION_ID.
	SessionID string `json:"session_id,omitempty"`
}

// CommandContext provides context about the command that triggered a user request.
// Per dspy_trajectory_capture.md §3.2.
type CommandContext struct {
	// CLICommand is the full CLI command string (e.g., "foxctl agent spawn ...").
	CLICommand string `json:"cli_command,omitempty"`

	// ProtocolCommand is the Protocol v1 command (e.g., "agent/spawn").
	ProtocolCommand string `json:"protocol_command,omitempty"`

	// JobID links to a job triggered by the command.
	JobID string `json:"job_id,omitempty"`

	// TraceID is the Protocol v1 meta.trace_id.
	TraceID string `json:"trace_id,omitempty"`
}

// TaskHints provides pre-linking hints to tasks/epics.
// Per dspy_trajectory_capture.md §3.2.
type TaskHints struct {
	// TaskID is the task related to the request.
	TaskID string `json:"task_id,omitempty"`

	// EpicID is the epic related to the request.
	EpicID string `json:"epic_id,omitempty"`

	// ScopePaths limits the request scope to specific paths.
	ScopePaths []string `json:"scope_paths,omitempty"`
}

// UserRequestCapture normalizes a user-initiated intent.
// Per dspy_trajectory_capture.md §3.2.
type UserRequestCapture struct {
	// ID uniquely identifies the user request.
	ID string `json:"id"`

	// WorkspaceID scopes the request to a workspace.
	WorkspaceID string `json:"workspace_id"`

	// Actor identifies who made the request (e.g., actor:human:<id>).
	Actor string `json:"actor"`

	// Source indicates where the request came from (cli, mailbox, api, viewer).
	Source Source `json:"source"`

	// TS is the timestamp of the request.
	TS time.Time `json:"ts"`

	// Text is the raw user request or prompt.
	Text string `json:"text"`

	// CommandContext provides context about the command that triggered the request.
	CommandContext *CommandContext `json:"command_context,omitempty"`

	// TaskHints provides pre-linking hints to tasks/epics.
	TaskHints *TaskHints `json:"task_hints,omitempty"`
}

// EventMeta holds correlation metadata for trajectory events.
// Per dspy_trajectory_capture.md §3.3.1.
type EventMeta struct {
	// TraceID is the primary correlation id; required for trajectory-related events.
	TraceID string `json:"trace_id,omitempty"`

	// JobID links to a job when events originate from jobs/* commands.
	JobID string `json:"job_id,omitempty"`

	// TaskID links to a task for task-scoped events.
	TaskID string `json:"task_id,omitempty"`

	// EpicID links to an epic-level plan.
	EpicID string `json:"epic_id,omitempty"`

	// ReviewID links to a review artifact.
	ReviewID string `json:"review_id,omitempty"`

	// ReviewRequestID links to a review request message or job.
	ReviewRequestID string `json:"review_request_id,omitempty"`

	// ReviewResultID links to a review result message or job.
	ReviewResultID string `json:"review_result_id,omitempty"`

	// ActorID or AgentID identifies the agent/actor (e.g., actor:agent:dspy:<slug>).
	ActorID string `json:"actor_id,omitempty"`

	// TaskRunID uniquely identifies an execution attempt of a task.
	TaskRunID string `json:"task_run_id,omitempty"`

	// TraceParent links to a parent trace for spawned trajectories.
	TraceParent string `json:"trace_parent,omitempty"`

	// JobAttempt is the retry attempt number for a job.
	JobAttempt int `json:"job_attempt,omitempty"`

	// CreatedBy identifies the service/user that created the event.
	CreatedBy string `json:"created_by,omitempty"`

	// CASDigest is the sha256:<hex> digest matching any referenced CAS artifact.
	CASDigest string `json:"cas_digest,omitempty"`
}

// Event is a normalized view over envelopes, mailbox messages,
// and review artifacts.
// Per dspy_trajectory_capture.md §3.3.
type Event struct {
	// ID uniquely identifies the event (ULID).
	ID string `json:"id"`

	// TrajectoryID links to the parent trajectory.
	TrajectoryID string `json:"trajectory_id"`

	// TS is the event timestamp.
	TS time.Time `json:"ts"`

	// Kind indicates the event type (user_request, tool_call, etc.).
	Kind EventKind `json:"kind"`

	// Actor identifies who or what produced the event.
	Actor string `json:"actor,omitempty"`

	// Command is the Protocol v1 command when applicable.
	Command string `json:"command,omitempty"`

	// Status is derived from envelope status or review status.
	Status string `json:"status,omitempty"`

	// DataInline is a small preview (e.g., truncated message).
	DataInline map[string]any `json:"data_inline,omitempty"`

	// DataArtifact is a CAS digest for full details.
	DataArtifact string `json:"data_artifact,omitempty"`

	// Meta holds correlation metadata.
	Meta *EventMeta `json:"meta,omitempty"`
}

// ListFilter specifies filtering options for listing trajectories.
type ListFilter struct {
	// WorkspaceID filters by workspace (required).
	WorkspaceID string

	// TaskID filters by task.
	TaskID string

	// EpicID filters by epic.
	EpicID string

	// AgentRole filters by agent role (coder, planner, reviewer, overseer).
	AgentRole string

	// Status filters by trajectory status.
	Status Status

	// TraceID filters by trace id.
	TraceID string

	// SessionID filters by session id.
	SessionID string

	// Since filters to trajectories created after this time.
	Since time.Time

	// Until filters to trajectories created before this time.
	Until time.Time

	// Limit caps the number of results (default 100).
	Limit int
}

// OutcomeFilter specifies filtering options for listing trajectories by outcome.
type OutcomeFilter struct {
	// WorkspaceID scopes the query (required).
	WorkspaceID string

	// AgentRole filters by agent role.
	AgentRole string

	// Success filters by success status (nil = any).
	Success *bool

	// MinRating filters to trajectories with human rating >= this value.
	MinRating *int

	// MaxRating filters to trajectories with human rating <= this value.
	MaxRating *int

	// HasFeedback filters to trajectories that have human feedback.
	HasFeedback *bool

	// Since filters to trajectories with outcomes recorded after this time.
	Since time.Time

	// Until filters to trajectories with outcomes recorded before this time.
	Until time.Time

	// Limit caps the number of results (default 100).
	Limit int
}

// EventFilter specifies filtering options for listing events.
type EventFilter struct {
	// TrajectoryID filters by trajectory (required).
	TrajectoryID string

	// Kind filters by event kind.
	Kind EventKind

	// Since filters to events after this time.
	Since time.Time

	// Until filters to events before this time.
	Until time.Time

	// Limit caps the number of results (default 1000).
	Limit int
}
