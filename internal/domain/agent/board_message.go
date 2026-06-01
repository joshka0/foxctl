package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultStream is the default stream for messages.
	DefaultStream = "coordination"
	// RoomStreamPrefix is the canonical stream prefix for room timelines.
	RoomStreamPrefix = "room:"
)

// BoardMessageKind defines the type of mailbox message.
type BoardMessageKind string

const (
	// BoardMessageKindInstruction represents a directive or instruction message.
	BoardMessageKindInstruction BoardMessageKind = "instruction"
	// BoardMessageKindInfo represents an informational message.
	BoardMessageKindInfo BoardMessageKind = "info"
	// BoardMessageKindAlert represents an alert or warning message.
	BoardMessageKindAlert BoardMessageKind = "alert"
	// BoardMessageKindReviewRequest represents a code or work review request.
	BoardMessageKindReviewRequest BoardMessageKind = "review_request"
	// BoardMessageKindTaskUpdate represents a task lifecycle update shared through a room.
	BoardMessageKindTaskUpdate BoardMessageKind = "task_update"
	// BoardMessageKindLeadChange represents a durable coordinator handoff event.
	BoardMessageKindLeadChange BoardMessageKind = "lead_change"
	// BoardMessageKindCoordinatorPulse represents a coordinator-facing system pulse.
	BoardMessageKindCoordinatorPulse BoardMessageKind = "coordinator_pulse"
	// BoardMessageKindPlanSession represents the root of a room planning session.
	BoardMessageKindPlanSession BoardMessageKind = "plan_session"
	// BoardMessageKindPlanProposal represents a proposed plan slice within a planning session.
	BoardMessageKindPlanProposal BoardMessageKind = "plan_proposal"
	// BoardMessageKindPlanQuestion represents an explicit open question in a planning session.
	BoardMessageKindPlanQuestion BoardMessageKind = "plan_question"
	// BoardMessageKindPlanDecision represents an accepted/superseded planning decision.
	BoardMessageKindPlanDecision BoardMessageKind = "plan_decision"
	// BoardMessageKindPlanReview represents a review/approval/block note for a planning session.
	BoardMessageKindPlanReview BoardMessageKind = "plan_review"
	// BoardMessageKindPlanClose represents the durable closure of a planning session.
	BoardMessageKindPlanClose BoardMessageKind = "plan_close"
	// BoardMessageKindInterviewSession represents the root of a round-robin interview session.
	BoardMessageKindInterviewSession BoardMessageKind = "interview_session"
	// BoardMessageKindInterviewQuestion represents a question posed within an interview session.
	BoardMessageKindInterviewQuestion BoardMessageKind = "interview_question"
	// BoardMessageKindInterviewAnswer represents an answer to a session question.
	BoardMessageKindInterviewAnswer BoardMessageKind = "interview_answer"
	// BoardMessageKindInterviewVerify represents a verifier verdict on an interview answer.
	BoardMessageKindInterviewVerify BoardMessageKind = "interview_verify"
	// BoardMessageKindEpic represents the root of a long-running agile epic within a room.
	BoardMessageKindEpic BoardMessageKind = "epic"
	// BoardMessageKindEpicQuestion represents a discovery/intake question for an epic.
	BoardMessageKindEpicQuestion BoardMessageKind = "epic_question"
	// BoardMessageKindEpicAnswer represents an answer to an epic intake question.
	BoardMessageKindEpicAnswer BoardMessageKind = "epic_answer"
	// BoardMessageKindEpicFinalize represents the clarified epic brief after intake is complete.
	BoardMessageKindEpicFinalize BoardMessageKind = "epic_finalize"
	// BoardMessageKindEpicUpdate represents an append-only metadata update for an epic.
	BoardMessageKindEpicUpdate BoardMessageKind = "epic_update"
	// BoardMessageKindEpicClose represents an explicit closure decision for an epic.
	BoardMessageKindEpicClose BoardMessageKind = "epic_close"
	// BoardMessageKindEpicCheckpoint represents a durable resumability snapshot for an epic.
	BoardMessageKindEpicCheckpoint BoardMessageKind = "epic_checkpoint"
	// BoardMessageKindMilestoneProposal represents a proposed milestone shape derived from an epic.
	BoardMessageKindMilestoneProposal BoardMessageKind = "milestone_proposal"
	// BoardMessageKindMilestone represents a milestone nested under an epic.
	BoardMessageKindMilestone BoardMessageKind = "milestone"
	// BoardMessageKindMilestoneContract represents a coordinator-owned contract update for a milestone.
	BoardMessageKindMilestoneContract BoardMessageKind = "milestone_contract"
	// BoardMessageKindStory represents a concrete work story nested under a milestone.
	BoardMessageKindStory BoardMessageKind = "story"
	// BoardMessageKindAcceptanceCriteria represents one explicit milestone acceptance criterion.
	BoardMessageKindAcceptanceCriteria BoardMessageKind = "acceptance_criteria"
	// BoardMessageKindMilestoneReview represents a milestone review/pass-block verdict.
	BoardMessageKindMilestoneReview BoardMessageKind = "milestone_review"
	// BoardMessageKindMilestoneSummary represents a review synthesis/summary for a milestone.
	BoardMessageKindMilestoneSummary BoardMessageKind = "milestone_summary"
	// BoardMessageKindStoryProposal represents a proposed story under a milestone.
	BoardMessageKindStoryProposal BoardMessageKind = "story_proposal"
	// BoardMessageKindStoryState represents an append-only lifecycle update for an accepted story.
	BoardMessageKindStoryState BoardMessageKind = "story_state"
	// BoardMessageKindStoryUpdate represents an append-only metadata update for an accepted story.
	BoardMessageKindStoryUpdate BoardMessageKind = "story_update"
	// BoardMessageKindStoryValidation represents story-owned validation evidence.
	BoardMessageKindStoryValidation BoardMessageKind = "story_validation"
	// BoardMessageKindDeliveryLog represents a durable delivery-log entry for an epic.
	BoardMessageKindDeliveryLog BoardMessageKind = "delivery_log"
	// BoardMessageKindGuidanceUpdate represents a durable retro/guidance artifact for an epic.
	BoardMessageKindGuidanceUpdate BoardMessageKind = "guidance_update"
)

// ErrInvalidBoardMessageKind indicates an unknown board message kind.
var ErrInvalidBoardMessageKind = errors.New("agent: invalid board message kind")

// NormalizeBoardMessageKind defaults omitted kinds to info and rejects unknown non-empty kinds.
func NormalizeBoardMessageKind(kind BoardMessageKind) (BoardMessageKind, error) {
	normalized := BoardMessageKind(strings.TrimSpace(string(kind)))
	if normalized == "" {
		return BoardMessageKindInfo, nil
	}
	switch normalized {
	case BoardMessageKindInstruction,
		BoardMessageKindInfo,
		BoardMessageKindAlert,
		BoardMessageKindReviewRequest,
		BoardMessageKindTaskUpdate,
		BoardMessageKindLeadChange,
		BoardMessageKindCoordinatorPulse,
		BoardMessageKindPlanSession,
		BoardMessageKindPlanProposal,
		BoardMessageKindPlanQuestion,
		BoardMessageKindPlanDecision,
		BoardMessageKindPlanReview,
		BoardMessageKindPlanClose,
		BoardMessageKindInterviewSession,
		BoardMessageKindInterviewQuestion,
		BoardMessageKindInterviewAnswer,
		BoardMessageKindInterviewVerify,
		BoardMessageKindEpic,
		BoardMessageKindEpicQuestion,
		BoardMessageKindEpicAnswer,
		BoardMessageKindEpicFinalize,
		BoardMessageKindEpicUpdate,
		BoardMessageKindEpicClose,
		BoardMessageKindEpicCheckpoint,
		BoardMessageKindMilestoneProposal,
		BoardMessageKindMilestone,
		BoardMessageKindMilestoneContract,
		BoardMessageKindStory,
		BoardMessageKindAcceptanceCriteria,
		BoardMessageKindMilestoneReview,
		BoardMessageKindMilestoneSummary,
		BoardMessageKindStoryProposal,
		BoardMessageKindStoryState,
		BoardMessageKindStoryUpdate,
		BoardMessageKindStoryValidation,
		BoardMessageKindDeliveryLog,
		BoardMessageKindGuidanceUpdate:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: unknown board message kind %q", ErrInvalidBoardMessageKind, kind)
	}
}

// BoardMessageStatus defines the read/ack status of a message.
type BoardMessageStatus string

const (
	// BoardMessageStatusUnread is the initial status for new messages.
	BoardMessageStatusUnread BoardMessageStatus = "unread"
	// BoardMessageStatusSurfaced indicates the message has been surfaced into AI context
	// (e.g., by an automatic hook) but not explicitly read by a user/tool.
	BoardMessageStatusSurfaced BoardMessageStatus = "surfaced"
	// BoardMessageStatusRead indicates the message has been read.
	BoardMessageStatusRead BoardMessageStatus = "read"
	// BoardMessageStatusAcked indicates the message has been acknowledged.
	BoardMessageStatusAcked BoardMessageStatus = "acked"
)

// ErrInvalidBoardMessageStatus indicates an unknown board message read/ack state.
var ErrInvalidBoardMessageStatus = errors.New("agent: invalid board message status")

// ValidateBoardMessageStatus rejects unknown read/ack states.
func ValidateBoardMessageStatus(status BoardMessageStatus) error {
	switch status {
	case BoardMessageStatusUnread, BoardMessageStatusSurfaced, BoardMessageStatusRead, BoardMessageStatusAcked:
		return nil
	default:
		return fmt.Errorf("%w: unknown board message status %q", ErrInvalidBoardMessageStatus, status)
	}
}

// ErrInvalidBoardMessagePriority indicates a priority outside the documented 1..5 range.
var ErrInvalidBoardMessagePriority = errors.New("agent: invalid board message priority")

// NormalizeBoardMessagePriority defaults omitted priority to DefaultPriority and rejects invalid explicit values.
func NormalizeBoardMessagePriority(priority int) (int, error) {
	if priority == 0 {
		return DefaultPriority, nil
	}
	if err := ValidateBoardMessagePriority(priority); err != nil {
		return 0, err
	}
	return priority, nil
}

// ValidateBoardMessagePriority rejects persisted or explicit priorities outside 1..5.
func ValidateBoardMessagePriority(priority int) error {
	if priority >= 1 && priority <= 5 {
		return nil
	}
	return fmt.Errorf("%w: priority %d outside 1..5", ErrInvalidBoardMessagePriority, priority)
}

// BoardMessage represents a workspace-scoped message for coordination.
// This is the richer message type per mailbox_blackboard.md spec.
type BoardMessage struct {
	ID               string             `json:"id"`
	WorkspaceID      string             `json:"workspace_id"`
	TaskID           string             `json:"task_id,omitempty"`
	RelatedMessageID string             `json:"related_message_id,omitempty"`
	Stream           string             `json:"stream"`
	Sender           string             `json:"sender"`
	Recipient        string             `json:"recipient"` // Actor ID or "*" for broadcast
	Kind             BoardMessageKind   `json:"kind"`
	Priority         int                `json:"priority"` // 1 (highest) .. 5 (lowest)
	AckRequired      bool               `json:"ack_required"`
	ReplyExpected    bool               `json:"reply_expected,omitempty"`
	Interrupt        bool               `json:"interrupt,omitempty"`
	Status           BoardMessageStatus `json:"status"`
	Subject          string             `json:"subject"`
	Body             string             `json:"body"`
	CreatedAt        time.Time          `json:"created_at"`
}

// ReservationMode defines the locking mode for file reservations.
type ReservationMode string

const (
	// ReservationModeExclusive represents an exclusive file reservation.
	ReservationModeExclusive ReservationMode = "exclusive"
	// ReservationModeShared represents a shared (non-exclusive) reservation.
	ReservationModeShared ReservationMode = "shared"
)

// ErrInvalidReservationMode indicates an unknown file reservation locking mode.
var ErrInvalidReservationMode = errors.New("agent: invalid reservation mode")

// ValidateReservationMode rejects unknown file reservation locking modes.
func ValidateReservationMode(mode ReservationMode) error {
	switch mode {
	case ReservationModeExclusive, ReservationModeShared:
		return nil
	default:
		return fmt.Errorf("%w: unknown reservation mode %q", ErrInvalidReservationMode, mode)
	}
}

// FileReservation represents an advisory lock over a file path.
type FileReservation struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	TaskID      string          `json:"task_id,omitempty"` // Associated task if any
	Path        string          `json:"path"`              // Relative to workspace root
	Holder      string          `json:"holder"`
	Mode        ReservationMode `json:"mode"`
	Reason      string          `json:"reason,omitempty"` // Why this file is being modified
	ExpiresAt   time.Time       `json:"expires_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

// IsExpired checks if a reservation has expired.
func (r *FileReservation) IsExpired() bool {
	return time.Now().UTC().After(r.ExpiresAt)
}

// ReservationConflict describes a conflict when acquiring a reservation.
type ReservationConflict struct {
	Path      string    `json:"path"`
	Holder    string    `json:"holder"`
	Mode      string    `json:"mode"`
	TaskID    string    `json:"task_id,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// InboxFilter defines query parameters for reading messages.
type InboxFilter struct {
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	TaskID      string `json:"task_id,omitempty"`
	Stream      string `json:"stream,omitempty"`
	OnlyUnread  bool   `json:"only_unread,omitempty"`
	// OnlyUnsurfaced returns only messages that have not yet been surfaced into context.
	// Practically: status == "unread". Intended for automatic context injectors.
	OnlyUnsurfaced bool `json:"only_unsurfaced,omitempty"`
	Limit          int  `json:"limit,omitempty"`
}

// DefaultPriority is the default priority (mid-level).
const DefaultPriority = 3

// DefaultReservationTTL is the default TTL for file reservations.
const DefaultReservationTTL = 10 * time.Minute

// BroadcastRecipient is the special recipient for broadcast messages.
const BroadcastRecipient = "*"

// IsAdminSender checks if the sender is an admin.
func IsAdminSender(sender string) bool {
	return sender == "admin" || len(sender) > 12 && sender[:12] == "actor:admin:"
}

// IsOverseerSender checks if the sender is the system overseer.
func IsOverseerSender(sender string) bool {
	return sender == "actor:system:overseer"
}

// RoomSummary is a derived read model over room-scoped board messages.
type RoomSummary struct {
	ID               string         `json:"id"`
	WorkspaceID      string         `json:"workspace_id"`
	Stream           string         `json:"stream"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	DispatchPolicy   string         `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string       `json:"dispatch_agent_ids,omitempty"`
	CreatedAt        time.Time      `json:"created_at,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at,omitempty"`
	LatestSubject    string         `json:"latest_subject,omitempty"`
	LatestPreview    string         `json:"latest_preview,omitempty"`
	LatestSender     string         `json:"latest_sender,omitempty"`
	LatestMessageAt  time.Time      `json:"latest_message_at"`
	MessageCount     int            `json:"message_count"`
	UnreadCount      int            `json:"unread_count"`
	Participants     []string       `json:"participants,omitempty"`
	TaskIDs          []string       `json:"task_ids,omitempty"`
	Members          []RoomMember   `json:"members,omitempty"`
	ArchivedAt       *time.Time     `json:"archived_at,omitempty"`
	SandboxConfig    *SandboxConfig `json:"sandbox_config,omitempty"`
}

type CompactRoomSummary struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	Stream          string `json:"stream"`
	Title           string `json:"title"`
	MessageCount    int    `json:"message_count"`
	UnreadCount     int    `json:"unread_count"`
	DispatchPolicy  string `json:"dispatch_policy,omitempty"`
	LatestSubject   string `json:"latest_subject,omitempty"`
	LatestPreview   string `json:"latest_preview,omitempty"`
	LatestSender    string `json:"latest_sender,omitempty"`
	LatestMessageAt string `json:"latest_message_at,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	ArchivedAt      string `json:"archived_at,omitempty"`
}

// CompactRoomSummaryForInbox returns the small room shape used by inbox responses.
func CompactRoomSummaryForInbox(s RoomSummary) CompactRoomSummary {
	out := CompactRoomSummary{
		ID:           s.ID,
		WorkspaceID:  s.WorkspaceID,
		Stream:       s.Stream,
		Title:        s.Title,
		MessageCount: s.MessageCount,
		UnreadCount:  s.UnreadCount,
	}
	if s.DispatchPolicy != "" {
		out.DispatchPolicy = s.DispatchPolicy
	}
	if s.LatestSubject != "" {
		out.LatestSubject = s.LatestSubject
	}
	if s.LatestPreview != "" {
		out.LatestPreview = s.LatestPreview
	}
	if s.LatestSender != "" {
		out.LatestSender = s.LatestSender
	}
	if !s.LatestMessageAt.IsZero() {
		out.LatestMessageAt = s.LatestMessageAt.Format(time.RFC3339)
	}
	if !s.CreatedAt.IsZero() {
		out.CreatedAt = s.CreatedAt.Format(time.RFC3339)
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
	}
	if s.ArchivedAt != nil && !s.ArchivedAt.IsZero() {
		out.ArchivedAt = s.ArchivedAt.Format(time.RFC3339)
	}
	return out
}

// RoomDeliveryBinding is the canonical routing and transport record for one room member.
type RoomDeliveryBinding struct {
	MuxBackend        string `json:"mux_backend,omitempty"`
	MuxSession        string `json:"mux_session,omitempty"`
	MuxPaneID         string `json:"mux_pane_id,omitempty"`
	TransportEndpoint string `json:"transport_endpoint,omitempty"`
	TransportKind     string `json:"transport_kind,omitempty"`
	SubmitMode        string `json:"submit_mode,omitempty"`
	Health            string `json:"health,omitempty"`
}

// RoomMember is an explicit membership record for one room.
type RoomMember struct {
	ActorID         string               `json:"actor_id"`
	Role            string               `json:"role,omitempty"`
	Unbound         bool                 `json:"unbound,omitempty"`
	JoinedAt        time.Time            `json:"joined_at"`
	DeliveryBinding *RoomDeliveryBinding `json:"delivery_binding,omitempty"`
}

// SandboxConfig holds sandbox-related metadata for a room that was created
// with the --sandbox flag. When non-nil, the room has an associated git
// worktree, tmux session, and gateway terminal route.
type SandboxConfig struct {
	// WorktreePath is the absolute path to the git worktree directory.
	WorktreePath string `json:"worktree_path,omitempty"`
	// WorktreeBranch is the branch name checked out in the worktree.
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	// TmuxSession is the tmux session name for this sandbox room.
	TmuxSession string `json:"tmux_session,omitempty"`
	// TerminalURL is the gateway URL for web terminal access.
	TerminalURL string `json:"terminal_url,omitempty"`
	// Runtime is the sandbox runtime type ("worktree" or "opensandbox").
	// Defaults to "worktree" when empty.
	Runtime string `json:"runtime,omitempty"`
	// BaseRef is the git ref the worktree was branched from.
	BaseRef string `json:"base_ref,omitempty"`

	// OpenSandbox-specific fields (set when Runtime == "opensandbox").

	// ContainerID is the OpenSandbox container identifier.
	ContainerID string `json:"container_id,omitempty"`
	// ContainerEndpoint is the execd endpoint URL for running commands
	// inside the container.
	ContainerEndpoint string `json:"container_endpoint,omitempty"`
	// ContainerExpiresAt is the RFC3339 timestamp when the container
	// auto-expires (set from --sandbox-ttl).
	ContainerExpiresAt string `json:"container_expires_at,omitempty"`
	// ContainerCPU is the CPU resource limit (e.g. "500m").
	ContainerCPU string `json:"container_cpu,omitempty"`
	// ContainerMemory is the memory resource limit (e.g. "512Mi").
	ContainerMemory string `json:"container_memory,omitempty"`
	// Fallback is true when the sandbox was created as a worktree because
	// OpenSandbox was unavailable.
	Fallback bool `json:"fallback,omitempty"`
}

// IsSandbox returns true if the room has sandbox configuration.
// A sandbox is identified by having either a worktree path or an
// OpenSandbox container ID.
func (sc *SandboxConfig) IsSandbox() bool {
	return sc != nil && (sc.WorktreePath != "" || sc.ContainerID != "")
}

// EffectiveRuntime returns the sandbox runtime, defaulting to "worktree".
func (sc *SandboxConfig) EffectiveRuntime() string {
	if sc == nil || sc.Runtime == "" {
		return "worktree"
	}
	return sc.Runtime
}

// Room is the first-class metadata record for a room.
type Room struct {
	ID               string         `json:"id"`
	WorkspaceID      string         `json:"workspace_id"`
	Stream           string         `json:"stream"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	DispatchPolicy   string         `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string       `json:"dispatch_agent_ids,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	Members          []RoomMember   `json:"members,omitempty"`
	ArchivedAt       *time.Time     `json:"archived_at,omitempty"`
	SandboxConfig    *SandboxConfig `json:"sandbox_config,omitempty"`
}

// RoomStreamName builds the canonical board stream for a room id.
func RoomStreamName(roomID string) string {
	if roomID == "" {
		return RoomStreamPrefix
	}
	return RoomStreamPrefix + roomID
}

// RoomIDFromStream returns the room id encoded in a room stream.
func RoomIDFromStream(stream string) string {
	if len(stream) < len(RoomStreamPrefix) || stream[:len(RoomStreamPrefix)] != RoomStreamPrefix {
		return ""
	}
	return stream[len(RoomStreamPrefix):]
}
