package agent

import "time"

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
)

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
	ID               string       `json:"id"`
	WorkspaceID      string       `json:"workspace_id"`
	Stream           string       `json:"stream"`
	Title            string       `json:"title"`
	Description      string       `json:"description,omitempty"`
	DispatchPolicy   string       `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string     `json:"dispatch_agent_ids,omitempty"`
	CreatedAt        time.Time    `json:"created_at,omitempty"`
	UpdatedAt        time.Time    `json:"updated_at,omitempty"`
	LatestSubject    string       `json:"latest_subject,omitempty"`
	LatestPreview    string       `json:"latest_preview,omitempty"`
	LatestSender     string       `json:"latest_sender,omitempty"`
	LatestMessageAt  time.Time    `json:"latest_message_at"`
	MessageCount     int          `json:"message_count"`
	UnreadCount      int          `json:"unread_count"`
	Participants     []string     `json:"participants,omitempty"`
	TaskIDs          []string     `json:"task_ids,omitempty"`
	Members          []RoomMember `json:"members,omitempty"`
	ArchivedAt       *time.Time   `json:"archived_at,omitempty"`
}

// RoomMember is an explicit membership record for one room.
type RoomMember struct {
	ActorID  string    `json:"actor_id"`
	Role     string    `json:"role,omitempty"`
	Backend  string    `json:"backend,omitempty"`
	Session  string    `json:"session,omitempty"`
	PaneID   string    `json:"pane_id,omitempty"`
	Unbound  bool      `json:"unbound,omitempty"`
	JoinedAt time.Time `json:"joined_at"`
}

// Room is the first-class metadata record for a room.
type Room struct {
	ID               string       `json:"id"`
	WorkspaceID      string       `json:"workspace_id"`
	Stream           string       `json:"stream"`
	Title            string       `json:"title"`
	Description      string       `json:"description,omitempty"`
	DispatchPolicy   string       `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string     `json:"dispatch_agent_ids,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	Members          []RoomMember `json:"members,omitempty"`
	ArchivedAt       *time.Time   `json:"archived_at,omitempty"`
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
