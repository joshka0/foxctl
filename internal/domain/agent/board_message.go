package agent

import "time"

// BoardMessageKind defines the type of mailbox message.
type BoardMessageKind string

const (
	BoardMessageKindInstruction   BoardMessageKind = "instruction"
	BoardMessageKindInfo          BoardMessageKind = "info"
	BoardMessageKindAlert         BoardMessageKind = "alert"
	BoardMessageKindReviewRequest BoardMessageKind = "review_request"
)

// BoardMessageStatus defines the read/ack status of a message.
type BoardMessageStatus string

const (
	BoardMessageStatusUnread BoardMessageStatus = "unread"
	BoardMessageStatusRead   BoardMessageStatus = "read"
	BoardMessageStatusAcked  BoardMessageStatus = "acked"
)

// BoardMessage represents a workspace-scoped message for coordination.
// This is the richer message type per mailbox_blackboard.md spec.
type BoardMessage struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id"`
	TaskID      string             `json:"task_id,omitempty"`
	Stream      string             `json:"stream"`
	Sender      string             `json:"sender"`
	Recipient   string             `json:"recipient"` // Actor ID or "*" for broadcast
	Kind        BoardMessageKind   `json:"kind"`
	Priority    int                `json:"priority"` // 1 (highest) .. 5 (lowest)
	AckRequired bool               `json:"ack_required"`
	Status      BoardMessageStatus `json:"status"`
	Subject     string             `json:"subject"`
	Body        string             `json:"body"`
	CreatedAt   time.Time          `json:"created_at"`
}

// ReservationMode defines the locking mode for file reservations.
type ReservationMode string

const (
	ReservationModeExclusive ReservationMode = "exclusive"
	ReservationModeShared    ReservationMode = "shared"
)

// FileReservation represents an advisory lock over a file path.
type FileReservation struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Path        string          `json:"path"` // Relative to workspace root
	Holder      string          `json:"holder"`
	Mode        ReservationMode `json:"mode"`
	ExpiresAt   time.Time       `json:"expires_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

// IsExpired checks if a reservation has expired.
func (r *FileReservation) IsExpired() bool {
	return time.Now().UTC().After(r.ExpiresAt)
}

// ReservationConflict describes a conflict when acquiring a reservation.
type ReservationConflict struct {
	Path   string `json:"path"`
	Holder string `json:"holder"`
	Mode   string `json:"mode"`
}

// InboxFilter defines query parameters for reading messages.
type InboxFilter struct {
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	TaskID      string `json:"task_id,omitempty"`
	Stream      string `json:"stream,omitempty"`
	OnlyUnread  bool   `json:"only_unread,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// DefaultStream is the default stream for messages.
const DefaultStream = "coordination"

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
