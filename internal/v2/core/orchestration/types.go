package orchestration

import "time"

// State is the canonical internal orchestration state.
type State string

const (
	StateUnclaimed  State = "Unclaimed"
	StateClaimed    State = "Claimed"
	StateRunning    State = "Running"
	StateRetryQueue State = "RetryQueued"
	StateReleased   State = "Released"
)

// Lane is the projected Kanban lane identifier.
type Lane string

const (
	LaneTodo       Lane = "Todo"
	LaneClaimed    Lane = "Claimed"
	LaneRunning    Lane = "Running"
	LaneRetryQueue Lane = "RetryQueued"
	LaneBlocked    Lane = "Blocked"
	LaneReview     Lane = "Review"
	LaneDone       Lane = "Done"
)

// LaneOrder returns canonical deterministic lane ordering for board rendering.
func LaneOrder() []Lane {
	return []Lane{
		LaneTodo,
		LaneClaimed,
		LaneRunning,
		LaneRetryQueue,
		LaneBlocked,
		LaneReview,
		LaneDone,
	}
}

// Eligibility indicates whether an item is currently dispatch-eligible.
type Eligibility string

const (
	EligibilityEligible   Eligibility = "eligible"
	EligibilityIneligible Eligibility = "ineligible"
)

// PolicyStatus indicates policy outcome associated with the item.
type PolicyStatus string

const (
	PolicyStatusOK              PolicyStatus = "ok"
	PolicyStatusDenied          PolicyStatus = "denied"
	PolicyStatusBlocked         PolicyStatus = "blocked"
	PolicyStatusValidationError PolicyStatus = "validation_failed"
)

// Outcome is the latest orchestration outcome marker used for lane derivation.
type Outcome string

const (
	OutcomeSpawnDenied  Outcome = "spawn_denied"
	OutcomePolicyDenied Outcome = "policy_denied"
	OutcomePreflightErr Outcome = "preflight_failed"
	OutcomeExecFailed   Outcome = "execution_failed"
)

// Card is the canonical projected issue card used for lane mapping.
type Card struct {
	WorkspaceID     string       `json:"workspace_id,omitempty"`
	IssueID         string       `json:"issue_id"`
	IssueIdentifier string       `json:"issue_identifier,omitempty"`
	Title           string       `json:"title,omitempty"`
	State           State        `json:"state"`
	Lane            Lane         `json:"lane,omitempty"`
	TrackerState    string       `json:"tracker_state,omitempty"`
	PolicyStatus    PolicyStatus `json:"policy_status,omitempty"`
	LastOutcome     Outcome      `json:"last_outcome,omitempty"`
	Eligibility     Eligibility  `json:"eligibility,omitempty"`
	DenialReason    string       `json:"denial_reason,omitempty"`
	Suggestion      string       `json:"suggestion,omitempty"`

	RunID       string     `json:"run_id,omitempty"`
	AgentID     string     `json:"agent_id,omitempty"`
	ActorID     string     `json:"actor_id,omitempty"`
	Attempt     int        `json:"attempt,omitempty"`
	RetryDueAt  *time.Time `json:"retry_due_at,omitempty"`
	LastEvent   string     `json:"last_event_type,omitempty"`
	LastEventAt *time.Time `json:"last_event_at,omitempty"`
}

// LaneColumn groups cards for one lane in board responses.
type LaneColumn struct {
	ID    Lane   `json:"id"`
	Title string `json:"title"`
	Cards []Card `json:"cards"`
}

// BoardRequest is the canonical board query request.
type BoardRequest struct {
	RequestID   string `json:"request_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
	Lane        Lane   `json:"lane,omitempty"`
}

// BoardResponse is the canonical board query response model.
type BoardResponse struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Counts      map[Lane]int `json:"counts"`
	Lanes       []LaneColumn `json:"lanes"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

// DispatchRequest requests one issue dispatch through canonical spawn path.
type DispatchRequest struct {
	RequestID       string `json:"request_id"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	IssueID         string `json:"issue_id"`
	IssueIdentifier string `json:"issue_identifier,omitempty"`
	Title           string `json:"title,omitempty"`

	Role     string `json:"role"`
	Prompt   string `json:"prompt,omitempty"`
	ExecMode string `json:"exec_mode,omitempty"`

	RunID   string `json:"run_id,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	ActorID string `json:"actor_id,omitempty"`

	ParentAgentID string `json:"parent_agent_id,omitempty"`

	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`

	MaxIterations    int `json:"max_iterations,omitempty"`
	MaxContextTokens int `json:"max_context_tokens,omitempty"`
	MaxAutoTurns     int `json:"max_auto_turns,omitempty"`
	ThinkInterval    int `json:"think_interval,omitempty"`
	Attempt          int `json:"attempt,omitempty"`
}

// DispatchResponse returns dispatch decision and spawned run details.
type DispatchResponse struct {
	RequestID       string       `json:"request_id"`
	WorkspaceID     string       `json:"workspace_id,omitempty"`
	IssueID         string       `json:"issue_id"`
	IssueIdentifier string       `json:"issue_identifier,omitempty"`
	Status          string       `json:"status"`
	PolicyStatus    PolicyStatus `json:"policy_status,omitempty"`
	LastOutcome     Outcome      `json:"last_outcome,omitempty"`
	DenialReason    string       `json:"denial_reason,omitempty"`
	Suggestion      string       `json:"suggestion,omitempty"`

	RunID  string `json:"run_id,omitempty"`
	TurnID string `json:"turn_id,omitempty"`

	AgentID string `json:"agent_id,omitempty"`
	ActorID string `json:"actor_id,omitempty"`

	Idempotent bool      `json:"idempotent,omitempty"`
	Timestamp  time.Time `json:"ts"`
}

// CardRequest fetches one issue card.
type CardRequest struct {
	RequestID   string `json:"request_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	IssueID     string `json:"issue_id"`
}

// CardResponse fetches one projected card.
type CardResponse struct {
	Card Card `json:"card"`
}

// RefreshRequest requests immediate reconcile/poll work.
type RefreshRequest struct {
	RequestID   string `json:"request_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// RefreshResponse is refresh command output.
type RefreshResponse struct {
	RequestID  string    `json:"request_id"`
	Queued     bool      `json:"queued"`
	Coalesced  bool      `json:"coalesced,omitempty"`
	Idempotent bool      `json:"idempotent,omitempty"`
	Timestamp  time.Time `json:"ts"`
}
