package hooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
)

// MockActionSkillRunner implements ActionSkillRunner for testing.
type MockActionSkillRunner struct {
	RunSkillFn func(ctx context.Context, skill string, args json.RawMessage) (string, error)
	Calls      []struct {
		Skill string
		Args  json.RawMessage
	}
}

func (m *MockActionSkillRunner) RunSkill(ctx context.Context, skill string, args json.RawMessage) (string, error) {
	m.Calls = append(m.Calls, struct {
		Skill string
		Args  json.RawMessage
	}{Skill: skill, Args: args})

	if m.RunSkillFn != nil {
		return m.RunSkillFn(ctx, skill, args)
	}
	return `{"result": "ok"}`, nil
}

// MockContextBuffer implements contextbuffer.Store for testing.
type MockContextBuffer struct {
	EnqueueFn func(ctx context.Context, params contextbuffer.EnqueueParams) (*contextbuffer.Entry, error)
	Entries   []contextbuffer.EnqueueParams
}

func (m *MockContextBuffer) Close() error { return nil }
func (m *MockContextBuffer) Enqueue(ctx context.Context, params contextbuffer.EnqueueParams) (*contextbuffer.Entry, error) {
	m.Entries = append(m.Entries, params)
	if m.EnqueueFn != nil {
		return m.EnqueueFn(ctx, params)
	}
	return &contextbuffer.Entry{ID: "test-entry-id", Source: params.Source, Text: params.Text}, nil
}

func (m *MockContextBuffer) Drain(ctx context.Context, params contextbuffer.DrainParams) (*contextbuffer.DrainResult, error) {
	return &contextbuffer.DrainResult{}, nil
}

func (m *MockContextBuffer) Peek(ctx context.Context, params contextbuffer.DrainParams) (*contextbuffer.DrainResult, error) {
	return &contextbuffer.DrainResult{}, nil
}

func (m *MockContextBuffer) PruneExpired(ctx context.Context, maxConsumedAge time.Duration) (int, error) {
	return 0, nil
}

func (m *MockContextBuffer) Count(ctx context.Context, workspaceID, sessionID string) (int, error) {
	return 0, nil
}
func (m *MockContextBuffer) DB() *sql.DB { return nil }

// MockMailboxStore implements mailbox.Store for testing.
type MockMailboxStore struct {
	SendFn   func(ctx context.Context, msg agent.Message) error
	Messages []agent.Message
}

func (m *MockMailboxStore) Close() error { return nil }
func (m *MockMailboxStore) Send(ctx context.Context, msg agent.Message) error {
	m.Messages = append(m.Messages, msg)
	if m.SendFn != nil {
		return m.SendFn(ctx, msg)
	}
	return nil
}

func (m *MockMailboxStore) Poll(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int) ([]agent.Message, error) {
	return nil, nil
}

func (m *MockMailboxStore) PollByTypes(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int, types []agent.MessageType) ([]agent.Message, error) {
	return nil, nil
}
func (m *MockMailboxStore) Ack(ctx context.Context, messageID string) error { return nil }
func (m *MockMailboxStore) Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error {
	return nil
}

func (m *MockMailboxStore) List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error) {
	return nil, nil
}
func (m *MockMailboxStore) Delete(ctx context.Context, messageID string) error { return nil }
func (m *MockMailboxStore) ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.Message, error) {
	return nil, nil
}

func (m *MockMailboxStore) ListByWorkspace(ctx context.Context, workspace string, limit int) ([]agent.Message, error) {
	return nil, nil
}
func (m *MockMailboxStore) DB() *sql.DB { return nil }

// MockBoardStore implements blackboard.BoardStore for testing.
type MockBoardStore struct {
	SendMessageFn func(ctx context.Context, msg *agent.BoardMessage) error
	MarkReadFn    func(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
	Messages      []*agent.BoardMessage
	ClaimedIDs    []string
	Rooms         []agent.Room
	RoomMembers   []agent.RoomMember
}

func (m *MockBoardStore) Close() error { return nil }
func (m *MockBoardStore) SendMessage(ctx context.Context, msg *agent.BoardMessage) error {
	m.Messages = append(m.Messages, msg)
	if m.SendMessageFn != nil {
		return m.SendMessageFn(ctx, msg)
	}
	return nil
}

func (m *MockBoardStore) Inbox(ctx context.Context, filter agent.InboxFilter) ([]agent.BoardMessage, error) {
	return nil, nil
}

func (m *MockBoardStore) UpsertRoom(ctx context.Context, room agent.Room) (agent.Room, error) {
	m.Rooms = append(m.Rooms, room)
	return room, nil
}

func (m *MockBoardStore) EnsureRoom(ctx context.Context, workspaceID, roomID, title string) (agent.Room, error) {
	room := agent.Room{
		ID:          roomID,
		WorkspaceID: workspaceID,
		Stream:      agent.RoomStreamName(roomID),
		Title:       title,
	}
	m.Rooms = append(m.Rooms, room)
	return room, nil
}

func (m *MockBoardStore) ReplaceRoomMembers(ctx context.Context, workspaceID, roomID string, members []agent.RoomMember) ([]agent.RoomMember, error) {
	m.RoomMembers = append([]agent.RoomMember(nil), members...)
	return members, nil
}

func (m *MockBoardStore) UpdateRoomMemberBinding(_ context.Context, _, _ string, _ agent.RoomMember) error {
	return nil
}

func (m *MockBoardStore) ListRooms(ctx context.Context, workspaceID, actorID string, limit int, archivedOnly bool) ([]agent.RoomSummary, error) {
	return nil, nil
}

func (m *MockBoardStore) GetRoom(ctx context.Context, workspaceID, roomID, actorID string) (agent.RoomSummary, error) {
	return agent.RoomSummary{}, nil
}

func (m *MockBoardStore) ArchiveRoom(ctx context.Context, workspaceID, roomID string) error {
	return nil
}

func (m *MockBoardStore) RestoreRoom(ctx context.Context, workspaceID, roomID string) error {
	return nil
}

func (m *MockBoardStore) DeleteRoom(ctx context.Context, workspaceID, roomID string) error {
	return nil
}

func (m *MockBoardStore) ListRoomMessages(ctx context.Context, workspaceID, roomID string, limit int) ([]agent.BoardMessage, error) {
	return nil, nil
}

func (m *MockBoardStore) MarkSurfaced(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error) {
	return 0, nil
}

func (m *MockBoardStore) MarkRead(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error) {
	m.ClaimedIDs = append(m.ClaimedIDs, messageIDs...)
	if m.MarkReadFn != nil {
		return m.MarkReadFn(ctx, workspaceID, actorID, messageIDs)
	}
	return len(messageIDs), nil
}

func (m *MockBoardStore) AckMessages(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error) {
	return 0, nil
}

func (m *MockBoardStore) CountMessagesByTask(ctx context.Context, workspaceID, taskID string) (admin, overseer, total int, err error) {
	return 0, 0, 0, nil
}

func (m *MockBoardStore) Reserve(ctx context.Context, res *agent.FileReservation) error { return nil }

func (m *MockBoardStore) CheckConflicts(ctx context.Context, workspaceID string, paths []string, holder string, mode agent.ReservationMode) ([]agent.ReservationConflict, error) {
	return nil, nil
}

func (m *MockBoardStore) Release(ctx context.Context, workspaceID, actorID string, paths []string) (int, error) {
	return 0, nil
}

func (m *MockBoardStore) ReleaseByID(ctx context.Context, reservationIDs []string) (int, error) {
	return 0, nil
}

func (m *MockBoardStore) ListReservations(ctx context.Context, workspaceID string) ([]agent.FileReservation, error) {
	return nil, nil
}

func TestExecutor_Execute_NoActions(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{})
	injected, err := exec.Execute(context.Background(), nil, Input{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if injected != "" {
		t.Errorf("expected empty injected context, got %q", injected)
	}
}

func TestExecutor_Execute_InjectContext(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{})

	actions := []Action{
		{Type: ActionInjectContext, Text: "Context line 1"},
		{Type: ActionInjectContext, Text: "Context line 2"},
	}

	injected, err := exec.Execute(context.Background(), actions, Input{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Context line 1\n\nContext line 2"
	if injected != expected {
		t.Errorf("expected %q, got %q", expected, injected)
	}
}

func TestExecutor_Execute_RunSkill(t *testing.T) {
	mockRunner := &MockActionSkillRunner{}
	exec := NewExecutor(ExecutorConfig{
		SkillRunner: mockRunner,
	})

	actions := []Action{
		{
			Type:  ActionRunSkill,
			Skill: "memory/query",
			Args:  json.RawMessage(`{"query": "test"}`),
		},
	}

	_, err := exec.Execute(context.Background(), actions, Input{SessionID: "session-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockRunner.Calls) != 1 {
		t.Fatalf("expected 1 skill call, got %d", len(mockRunner.Calls))
	}
	if mockRunner.Calls[0].Skill != "memory/query" {
		t.Errorf("expected memory/query, got %s", mockRunner.Calls[0].Skill)
	}
}

func TestExecutor_Execute_RunSkill_NoRunner(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{})

	actions := []Action{
		{Type: ActionRunSkill, Skill: "memory/query"},
	}

	// Should not error when no runner configured
	_, err := exec.Execute(context.Background(), actions, Input{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecutor_Execute_RunSkill_Error(t *testing.T) {
	mockRunner := &MockActionSkillRunner{
		RunSkillFn: func(ctx context.Context, skill string, args json.RawMessage) (string, error) {
			return "", errors.New("skill failed")
		},
	}
	exec := NewExecutor(ExecutorConfig{
		SkillRunner: mockRunner,
		FailOpen:    false, // Fail on error
	})

	actions := []Action{
		{Type: ActionRunSkill, Skill: "memory/query"},
	}

	_, err := exec.Execute(context.Background(), actions, Input{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecutor_Execute_RunSkill_FailOpen(t *testing.T) {
	mockRunner := &MockActionSkillRunner{
		RunSkillFn: func(ctx context.Context, skill string, args json.RawMessage) (string, error) {
			return "", errors.New("skill failed")
		},
	}
	exec := NewExecutor(ExecutorConfig{
		SkillRunner: mockRunner,
		FailOpen:    true, // Continue on error
	})

	actions := []Action{
		{Type: ActionRunSkill, Skill: "memory/query"},
	}

	// Should not error when FailOpen is true
	_, err := exec.Execute(context.Background(), actions, Input{})
	if err != nil {
		t.Fatalf("unexpected error with FailOpen=true: %v", err)
	}
}

func TestExecutor_Execute_EnqueueContext(t *testing.T) {
	mockBuffer := &MockContextBuffer{}
	exec := NewExecutor(ExecutorConfig{
		ContextBuffer: mockBuffer,
	})

	actions := []Action{
		{
			Type:       ActionEnqueueContext,
			Source:     "test-hook",
			Text:       "Relevant context for the session",
			Priority:   1,
			TTLSeconds: 120,
			Dedupe:     true,
		},
	}

	input := Input{
		SessionID:   "session-123",
		WorkspaceID: "workspace-456",
	}

	_, err := exec.Execute(context.Background(), actions, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockBuffer.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(mockBuffer.Entries))
	}

	entry := mockBuffer.Entries[0]
	if entry.Source != "test-hook" {
		t.Errorf("expected source test-hook, got %s", entry.Source)
	}
	if entry.SessionID != "session-123" {
		t.Errorf("expected session-123, got %s", entry.SessionID)
	}
	if entry.Priority != 1 {
		t.Errorf("expected priority 1, got %d", entry.Priority)
	}
	if !entry.Dedupe {
		t.Error("expected dedupe=true")
	}
}

func TestExecutor_Execute_SendMailbox(t *testing.T) {
	mockMailbox := &MockMailboxStore{}
	exec := NewExecutor(ExecutorConfig{
		MailboxStore: mockMailbox,
	})

	actions := []Action{
		{
			Type:        ActionSendMailbox,
			ToNS:        "agent:coder-1",
			MessageType: "ask",
			Payload:     json.RawMessage(`{"task": "review code"}`),
			Headers:     map[string]string{"priority": "high"},
		},
	}

	input := Input{
		SessionID:     "session-123",
		ActorID:       "actor:overseer",
		WorkspaceRoot: "/workspace",
	}

	_, err := exec.Execute(context.Background(), actions, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockMailbox.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mockMailbox.Messages))
	}

	msg := mockMailbox.Messages[0]
	if msg.ToNS != "agent:coder-1" {
		t.Errorf("expected agent:coder-1, got %s", msg.ToNS)
	}
	if msg.FromNS != "actor:overseer" {
		t.Errorf("expected actor:overseer, got %s", msg.FromNS)
	}
	if msg.Type != "ask" {
		t.Errorf("expected ask, got %s", msg.Type)
	}
}

func TestExecutor_Execute_BBPost(t *testing.T) {
	mockBoard := &MockBoardStore{}
	exec := NewExecutor(ExecutorConfig{
		BoardStore: mockBoard,
	})

	actions := []Action{
		{
			Type:      ActionBBPost,
			Topic:     "status",
			BBPayload: json.RawMessage(`{"status": "working"}`),
		},
	}

	input := Input{
		SessionID:   "session-123",
		ActorID:     "actor:coder",
		WorkspaceID: "workspace-456",
	}

	_, err := exec.Execute(context.Background(), actions, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockBoard.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mockBoard.Messages))
	}

	msg := mockBoard.Messages[0]
	if msg.Stream != "status" {
		t.Errorf("expected status, got %s", msg.Stream)
	}
	if msg.Sender != "actor:coder" {
		t.Errorf("expected actor:coder, got %s", msg.Sender)
	}
}

func TestExecutor_Execute_BBClaim(t *testing.T) {
	mockBoard := &MockBoardStore{}
	exec := NewExecutor(ExecutorConfig{
		BoardStore: mockBoard,
	})

	actions := []Action{
		{
			Type:     ActionBBClaim,
			Topic:    "tasks",
			RecordID: "task-abc-123",
		},
	}

	input := Input{
		ActorID:     "actor:coder",
		WorkspaceID: "workspace-456",
	}

	_, err := exec.Execute(context.Background(), actions, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockBoard.ClaimedIDs) != 1 {
		t.Fatalf("expected 1 claimed ID, got %d", len(mockBoard.ClaimedIDs))
	}
	if mockBoard.ClaimedIDs[0] != "task-abc-123" {
		t.Errorf("expected task-abc-123, got %s", mockBoard.ClaimedIDs[0])
	}
}

func TestExecutor_Execute_MultipleActions(t *testing.T) {
	mockRunner := &MockActionSkillRunner{}
	mockBuffer := &MockContextBuffer{}
	mockMailbox := &MockMailboxStore{}

	exec := NewExecutor(ExecutorConfig{
		SkillRunner:   mockRunner,
		ContextBuffer: mockBuffer,
		MailboxStore:  mockMailbox,
	})

	actions := []Action{
		{Type: ActionInjectContext, Text: "Injected context"},
		{Type: ActionRunSkill, Skill: "memory/query", Args: json.RawMessage(`{}`)},
		{Type: ActionEnqueueContext, Source: "hook", Text: "Enqueued context"},
		{Type: ActionSendMailbox, ToNS: "agent:test", MessageType: "cmd"},
	}

	input := Input{
		SessionID:   "session-123",
		ActorID:     "actor:test",
		WorkspaceID: "workspace-456",
	}

	injected, err := exec.Execute(context.Background(), actions, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check inject_context
	if injected != "Injected context" {
		t.Errorf("expected 'Injected context', got %q", injected)
	}

	// Check run_skill
	if len(mockRunner.Calls) != 1 {
		t.Errorf("expected 1 skill call, got %d", len(mockRunner.Calls))
	}

	// Check enqueue_context
	if len(mockBuffer.Entries) != 1 {
		t.Errorf("expected 1 buffer entry, got %d", len(mockBuffer.Entries))
	}

	// Check send_mailbox
	if len(mockMailbox.Messages) != 1 {
		t.Errorf("expected 1 mailbox message, got %d", len(mockMailbox.Messages))
	}
}

func TestNopExecutor(t *testing.T) {
	exec := NopExecutor{}

	actions := []Action{
		{Type: ActionRunSkill, Skill: "anything"},
		{Type: ActionInjectContext, Text: "anything"},
	}

	injected, err := exec.Execute(context.Background(), actions, Input{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if injected != "" {
		t.Errorf("expected empty, got %q", injected)
	}
}
