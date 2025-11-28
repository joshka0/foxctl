// Package types defines core types for dspy-go agent integration.
package types

import "time"

// AgentRole defines the type of agent (coder, planner, reviewer, etc.).
type AgentRole string

const (
	RoleCoder    AgentRole = "coder"
	RolePlanner  AgentRole = "planner"
	RoleReviewer AgentRole = "reviewer"
	RoleFixer    AgentRole = "fixer"
)

// AgentStatus represents the current state of an agent session.
type AgentStatus string

const (
	StatusRunning     AgentStatus = "running"
	StatusOK          AgentStatus = "ok"
	StatusNeedsReview AgentStatus = "needs_review"
	StatusBlocked     AgentStatus = "blocked"
	StatusError       AgentStatus = "error"
	StatusCanceled    AgentStatus = "canceled"
)

// CodingInput defines the input fields for a coding agent.
// Maps to §6.1 of dspy_go_agents.md spec.
type CodingInput struct {
	// Goal is a one-sentence description of what to accomplish.
	Goal string `json:"goal"`

	// Description is richer context, possibly from user prompt or plan.
	Description string `json:"description,omitempty"`

	// WorkspaceID is the workspace anchor.
	WorkspaceID string `json:"workspace_id"`

	// EpicID is the optional epic / plan root.
	EpicID string `json:"epic_id,omitempty"`

	// TaskID is the primary task being worked on.
	TaskID string `json:"task_id,omitempty"`

	// ScopePaths are allowed directories/files (enforced by task_guard).
	ScopePaths []string `json:"scope_paths,omitempty"`

	// Constraints are human- or overseer-specified constraints.
	Constraints []string `json:"constraints,omitempty"`

	// Mode is the execution mode: "analyze", "edit", "test", or "mixed".
	Mode string `json:"mode,omitempty"`
}

// CodingOutput defines the output fields for a coding agent.
// Maps to §6.1 of dspy_go_agents.md spec.
type CodingOutput struct {
	// Status is the outcome: "ok", "needs_review", "blocked", or "error".
	Status AgentStatus `json:"status"`

	// Summary is a human-readable summary for overseer/human.
	Summary string `json:"summary"`

	// ChangedFiles lists relative paths modified.
	ChangedFiles []string `json:"changed_files,omitempty"`

	// NewTasks are tasks to create or update.
	NewTasks []TaskRef `json:"new_tasks,omitempty"`

	// PlanUpdates are suggested changes to the plan (never applied directly).
	PlanUpdates []PlanDelta `json:"plan_updates,omitempty"`

	// MailDrafts are messages the agent recommends sending.
	MailDrafts []MailDraft `json:"mail_drafts,omitempty"`
}

// TaskRef references a task to create or update.
type TaskRef struct {
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// PlanDelta represents a suggested change to the plan.
type PlanDelta struct {
	Action    string `json:"action"` // "split", "merge", "reorder", "reprioritize"
	TaskID    string `json:"task_id,omitempty"`
	Reason    string `json:"reason"`
	NewTitle  string `json:"new_title,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	DependsOn string `json:"depends_on,omitempty"`
}

// MailDraft represents a message the agent recommends sending.
type MailDraft struct {
	Recipient   string `json:"recipient"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	Kind        string `json:"kind,omitempty"` // "instruction", "info", "alert", "review_request"
	Priority    int    `json:"priority,omitempty"`
	AckRequired bool   `json:"ack_required,omitempty"`
}

// PlanningInput defines input for a planning agent.
// Maps to §6.2 of dspy_go_agents.md spec.
type PlanningInput struct {
	WorkspaceID string   `json:"workspace_id"`
	EpicID      string   `json:"epic_id,omitempty"`
	RootTaskID  string   `json:"root_task_id,omitempty"`
	Goal        string   `json:"goal"`
	Description string   `json:"description,omitempty"`
	ScopePaths  []string `json:"scope_paths,omitempty"`
}

// PlanningOutput defines output for a planning agent.
type PlanningOutput struct {
	// ProposedTasks are new tasks to create.
	ProposedTasks []TaskRef `json:"proposed_tasks,omitempty"`

	// SuggestedChanges are changes to existing tasks.
	SuggestedChanges []PlanDelta `json:"suggested_changes,omitempty"`

	// Rationale explains the planning decisions.
	Rationale string `json:"rationale"`

	// RiskNotes are potential issues with the plan.
	RiskNotes []string `json:"risk_notes,omitempty"`

	// MailDrafts are messages to send (e.g., to overseer).
	MailDrafts []MailDraft `json:"mail_drafts,omitempty"`
}

// AgentConfig holds configuration for spawning an agent.
type AgentConfig struct {
	// Role is the agent type (coder, planner, etc.).
	Role AgentRole `json:"role"`

	// ActorID is the mailbox identity for this agent.
	ActorID string `json:"actor_id"`

	// WorkspaceID is the workspace this agent operates in.
	WorkspaceID string `json:"workspace_id"`

	// EpicID is the optional epic scope.
	EpicID string `json:"epic_id,omitempty"`

	// TaskID is the optional task scope.
	TaskID string `json:"task_id,omitempty"`

	// TeamID is the optional team this agent belongs to.
	TeamID string `json:"team_id,omitempty"`

	// MaxIterations limits the number of ReAct iterations.
	MaxIterations int `json:"max_iterations,omitempty"`

	// Timeout is the maximum execution time.
	Timeout time.Duration `json:"timeout,omitempty"`

	// LLMProvider specifies which LLM to use (e.g., "gemini", "openai").
	LLMProvider string `json:"llm_provider,omitempty"`

	// LLMModel specifies the model name.
	LLMModel string `json:"llm_model,omitempty"`
}

// AgentSession represents a running or completed agent session.
type AgentSession struct {
	// ID is the unique session identifier (ULID).
	ID string `json:"id"`

	// JobID links to the jobs store.
	JobID string `json:"job_id"`

	// Config is the agent configuration.
	Config AgentConfig `json:"config"`

	// Status is the current session status.
	Status AgentStatus `json:"status"`

	// StartedAt is when the session began.
	StartedAt time.Time `json:"started_at"`

	// EndedAt is when the session ended (if terminal).
	EndedAt *time.Time `json:"ended_at,omitempty"`

	// Iterations is the number of ReAct loops completed.
	Iterations int `json:"iterations"`

	// Summary is the final output summary.
	Summary string `json:"summary,omitempty"`

	// Error holds error details if status is "error".
	Error string `json:"error,omitempty"`
}

// ToolCall represents a single tool invocation by the agent.
type ToolCall struct {
	// ToolName is the name of the tool called.
	ToolName string `json:"tool_name"`

	// Args are the arguments passed to the tool.
	Args map[string]any `json:"args,omitempty"`

	// Result is the tool's output.
	Result any `json:"result,omitempty"`

	// Error is any error from the tool.
	Error string `json:"error,omitempty"`

	// Duration is how long the tool call took.
	Duration time.Duration `json:"duration"`

	// Timestamp is when the call was made.
	Timestamp time.Time `json:"timestamp"`
}

// ExecutionTrace captures the full execution history of an agent session.
type ExecutionTrace struct {
	SessionID  string      `json:"session_id"`
	Iterations []Iteration `json:"iterations"`
}

// Iteration represents one ReAct loop iteration.
type Iteration struct {
	Number      int        `json:"number"`
	Thought     string     `json:"thought,omitempty"`
	Action      string     `json:"action,omitempty"`
	ToolCalls   []ToolCall `json:"tool_calls,omitempty"`
	Observation string     `json:"observation,omitempty"`
	Timestamp   time.Time  `json:"timestamp"`
}
