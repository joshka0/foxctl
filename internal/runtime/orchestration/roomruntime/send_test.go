package roomruntime

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/agent"
)

func TestSendMessageDefaultsOnlyOmittedPriority(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name         string
		priority     int
		wantPriority int
	}{
		{name: "zero defaults", priority: 0, wantPriority: agent.DefaultPriority},
		{name: "negative preserved for store validation", priority: -1, wantPriority: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingSendStore{}
			_, err := SendMessage(ctx, store, SendMessageInput{
				WorkspaceID: "ws1",
				RoomID:      "room1",
				Sender:      "actor:agent:sender",
				Recipient:   agent.BroadcastRecipient,
				Subject:     "priority",
				Body:        "priority body",
				Priority:    tt.priority,
			})
			if err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			if store.sent == nil {
				t.Fatal("SendMessage did not persist message")
			}
			if store.sent.Priority != tt.wantPriority {
				t.Fatalf("sent priority=%d want %d", store.sent.Priority, tt.wantPriority)
			}
		})
	}
}

type recordingSendStore struct {
	sent *agent.BoardMessage
}

func (s *recordingSendStore) EnsureRoom(context.Context, string, string, string) (agent.Room, error) {
	return agent.Room{}, nil
}

func (s *recordingSendStore) GetRoom(context.Context, string, string, string) (agent.RoomSummary, error) {
	return agent.RoomSummary{}, nil
}

func (s *recordingSendStore) SendMessage(_ context.Context, msg *agent.BoardMessage) error {
	copy := *msg
	s.sent = &copy
	return nil
}
