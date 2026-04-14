package roomruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

// BoardStore captures the room message persistence operations needed by the
// shared room-runtime send flow.
type BoardStore interface {
	EnsureRoom(ctx context.Context, workspaceID, roomID, title string) (agent.Room, error)
	GetRoom(ctx context.Context, workspaceID, roomID, actorID string) (agent.RoomSummary, error)
	SendMessage(ctx context.Context, msg *agent.BoardMessage) error
}

// LiveRelayResult is the transport-neutral live relay summary returned by the
// shared room-runtime send flow.
type LiveRelayResult struct {
	Backend        string   `json:"backend"`
	DeliveredCount int      `json:"delivered_count,omitempty"`
	FailedCount    int      `json:"failed_count,omitempty"`
	DeliveredTo    []string `json:"delivered_to,omitempty"`
	FailedMembers  []string `json:"failed_members,omitempty"`
	SkippedMembers []string `json:"skipped_members,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// SendMessageInput is the normalized room message send contract shared by CLI
// and API adapters.
type SendMessageInput struct {
	WorkspaceID              string
	RoomID                   string
	RoomTitle                string
	Sender                   string
	Recipient                string
	RelatedMessageID         string
	Subject                  string
	Body                     string
	TaskID                   string
	Kind                     agent.BoardMessageKind
	Priority                 int
	AckRequired              bool
	ReplyExpected            bool
	Interrupt                bool
	EnsureRoom               bool
	RequireExistingRecipient bool
}

// SendMessageResult returns the persisted message.
type SendMessageResult struct {
	Room    agent.RoomSummary
	Message *agent.BoardMessage
}

// SendMessage persists one normalized room message. Live delivery is owned by
// the room runtime/loop instead of the send caller.
func SendMessage(ctx context.Context, store BoardStore, input SendMessageInput) (SendMessageResult, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	roomID := strings.TrimSpace(input.RoomID)
	sender := strings.TrimSpace(input.Sender)
	recipient := strings.TrimSpace(input.Recipient)
	subject := strings.TrimSpace(input.Subject)
	body := strings.TrimSpace(input.Body)

	if workspaceID == "" {
		return SendMessageResult{}, fmt.Errorf("workspace_id is required")
	}
	if roomID == "" {
		return SendMessageResult{}, fmt.Errorf("room_id is required")
	}
	if sender == "" {
		return SendMessageResult{}, fmt.Errorf("sender is required")
	}
	if body == "" {
		return SendMessageResult{}, fmt.Errorf("body is required")
	}
	if recipient == "" {
		recipient = agent.BroadcastRecipient
	}
	if input.ReplyExpected && recipient == agent.BroadcastRecipient {
		return SendMessageResult{}, fmt.Errorf("reply_expected requires a direct recipient")
	}
	if input.Interrupt && recipient == agent.BroadcastRecipient {
		return SendMessageResult{}, fmt.Errorf("interrupt requires a direct recipient")
	}

	roomTitle := strings.TrimSpace(input.RoomTitle)
	if roomTitle == "" {
		roomTitle = roomID
	}
	if input.EnsureRoom {
		if _, err := store.EnsureRoom(ctx, workspaceID, roomID, roomTitle); err != nil {
			return SendMessageResult{}, err
		}
	}

	var summary agent.RoomSummary
	if input.RequireExistingRecipient {
		var err error
		summary, err = store.GetRoom(ctx, workspaceID, roomID, "")
		if err != nil {
			return SendMessageResult{}, err
		}
		if recipient != agent.BroadcastRecipient && !roomHasParticipant(summary, recipient) {
			return SendMessageResult{}, fmt.Errorf("recipient %q is not a participant in room %q", recipient, roomID)
		}
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      workspaceID,
		TaskID:           strings.TrimSpace(input.TaskID),
		RelatedMessageID: strings.TrimSpace(input.RelatedMessageID),
		Stream:           agent.RoomStreamName(roomID),
		Sender:           sender,
		Recipient:        recipient,
		Kind:             input.Kind,
		Priority:         input.Priority,
		AckRequired:      input.AckRequired,
		ReplyExpected:    input.ReplyExpected,
		Interrupt:        input.Interrupt,
		Subject:          subject,
		Body:             body,
	}
	if msg.Kind == "" {
		msg.Kind = agent.BoardMessageKindInfo
	}
	if msg.Priority <= 0 {
		msg.Priority = agent.DefaultPriority
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		return SendMessageResult{}, err
	}

	return SendMessageResult{Room: summary, Message: msg}, nil
}

func roomHasParticipant(room agent.RoomSummary, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	for _, participant := range room.Participants {
		if sameRoomParticipant(participant, actorID) {
			return true
		}
	}
	for _, member := range room.Members {
		if sameRoomParticipant(member.ActorID, actorID) {
			return true
		}
	}
	return false
}

func sameRoomParticipant(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}
