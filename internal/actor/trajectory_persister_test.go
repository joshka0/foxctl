package actor

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// mockTrajectoryStore is a test implementation of trajectory.Store.
type mockTrajectoryStore struct {
	mu           sync.Mutex
	trajectories map[string]trajectory.Trajectory
	events       []trajectory.Event
	closed       bool
}

func newMockTrajectoryStore() *mockTrajectoryStore {
	return &mockTrajectoryStore{
		trajectories: make(map[string]trajectory.Trajectory),
		events:       make([]trajectory.Event, 0),
	}
}

func (m *mockTrajectoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockTrajectoryStore) InsertTrajectory(_ context.Context, t trajectory.Trajectory) (trajectory.Trajectory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trajectories[t.ID] = t
	return t, nil
}

func (m *mockTrajectoryStore) GetTrajectory(_ context.Context, workspaceID, id string) (trajectory.Trajectory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.trajectories[id]; ok && t.WorkspaceID == workspaceID {
		return t, nil
	}
	return trajectory.Trajectory{}, trajectory.ErrNotFound
}

func (m *mockTrajectoryStore) UpdateTrajectory(_ context.Context, t trajectory.Trajectory) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.trajectories[t.ID]; !ok {
		return trajectory.ErrNotFound
	}
	m.trajectories[t.ID] = t
	return nil
}

func (m *mockTrajectoryStore) ListTrajectories(_ context.Context, _ trajectory.ListFilter) ([]trajectory.Trajectory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]trajectory.Trajectory, 0, len(m.trajectories))
	for _, t := range m.trajectories {
		result = append(result, t)
	}
	return result, nil
}

func (m *mockTrajectoryStore) DeleteTrajectory(_ context.Context, _, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.trajectories, id)
	return nil
}

func (m *mockTrajectoryStore) SetOutcome(_ context.Context, workspaceID, id string, outcome trajectory.Outcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.trajectories[id]; ok && t.WorkspaceID == workspaceID {
		t.Outcome = &outcome
		m.trajectories[id] = t
		return nil
	}
	return trajectory.ErrNotFound
}

func (m *mockTrajectoryStore) ListByOutcome(_ context.Context, _ trajectory.OutcomeFilter) ([]trajectory.Trajectory, error) {
	return nil, nil
}

func (m *mockTrajectoryStore) InsertUserRequest(_ context.Context, ur trajectory.UserRequestCapture) (trajectory.UserRequestCapture, error) {
	return ur, nil
}

func (m *mockTrajectoryStore) GetUserRequest(_ context.Context, _, _ string) (trajectory.UserRequestCapture, error) {
	return trajectory.UserRequestCapture{}, trajectory.ErrNotFound
}

func (m *mockTrajectoryStore) ListUserRequests(_ context.Context, _ string, _ int) ([]trajectory.UserRequestCapture, error) {
	return nil, nil
}

func (m *mockTrajectoryStore) InsertEvent(_ context.Context, e trajectory.Event) (trajectory.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return e, nil
}

func (m *mockTrajectoryStore) InsertEvents(_ context.Context, events []trajectory.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, events...)
	return nil
}

func (m *mockTrajectoryStore) ListEvents(_ context.Context, _ trajectory.EventFilter) ([]trajectory.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events, nil
}

func (m *mockTrajectoryStore) GetEventsByTraceID(_ context.Context, _, _ string) ([]trajectory.Event, error) {
	return nil, nil
}

func (m *mockTrajectoryStore) getEvents() []trajectory.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]trajectory.Event, len(m.events))
	copy(result, m.events)
	return result
}

func (m *mockTrajectoryStore) getTrajectories() []trajectory.Trajectory {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]trajectory.Trajectory, 0, len(m.trajectories))
	for _, t := range m.trajectories {
		result = append(result, t)
	}
	return result
}

func TestNewTrajectoryPersister(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	if persister == nil {
		t.Fatal("persister is nil")
	}
	if persister.store != store {
		t.Error("store not set correctly")
	}
}

func TestTrajectoryPersister_Persist_MailReceived(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	event := NewEvent(EventMailReceived, "test-actor").
		WithTarget("target-actor").
		WithSession("session-123").
		WithWorkspace("workspace-1")

	err := persister.Persist(context.Background(), event)
	if err != nil {
		t.Fatalf("Persist() error = %v", err)
	}

	// Check trajectory was created
	trajectories := store.getTrajectories()
	if len(trajectories) != 1 {
		t.Fatalf("expected 1 trajectory, got %d", len(trajectories))
	}
	if trajectories[0].WorkspaceID != "workspace-1" {
		t.Errorf("workspace = %s, want workspace-1", trajectories[0].WorkspaceID)
	}
	if trajectories[0].AgentRole != "actor-system" {
		t.Errorf("agent_role = %s, want actor-system", trajectories[0].AgentRole)
	}

	// Check event was stored
	events := store.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != trajectory.EventKindToolCall {
		t.Errorf("kind = %s, want %s", events[0].Kind, trajectory.EventKindToolCall)
	}
	if events[0].Actor != "test-actor" {
		t.Errorf("actor = %s, want test-actor", events[0].Actor)
	}
}

func TestTrajectoryPersister_Persist_MultipleEvents(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	// Persist multiple events to same workspace
	events := []Event{
		NewEvent(EventMailReceived, "actor-1").WithWorkspace("ws-1"),
		NewEvent(EventMailSent, "actor-1").WithWorkspace("ws-1"),
		NewEvent(EventAgentStarted, "actor-2").WithWorkspace("ws-1"),
	}

	for _, e := range events {
		if err := persister.Persist(context.Background(), e); err != nil {
			t.Fatalf("Persist() error = %v", err)
		}
	}

	// Should reuse same trajectory
	trajectories := store.getTrajectories()
	if len(trajectories) != 1 {
		t.Fatalf("expected 1 trajectory, got %d", len(trajectories))
	}

	// All events should be stored
	storedEvents := store.getEvents()
	if len(storedEvents) != 3 {
		t.Fatalf("expected 3 events, got %d", len(storedEvents))
	}
}

func TestTrajectoryPersister_Persist_MultipleWorkspaces(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	// Persist events to different workspaces
	events := []Event{
		NewEvent(EventMailReceived, "actor-1").WithWorkspace("ws-1"),
		NewEvent(EventMailReceived, "actor-2").WithWorkspace("ws-2"),
		NewEvent(EventMailReceived, "actor-3").WithWorkspace("ws-3"),
	}

	for _, e := range events {
		if err := persister.Persist(context.Background(), e); err != nil {
			t.Fatalf("Persist() error = %v", err)
		}
	}

	// Should create separate trajectories per workspace
	trajectories := store.getTrajectories()
	if len(trajectories) != 3 {
		t.Fatalf("expected 3 trajectories, got %d", len(trajectories))
	}
}

func TestTrajectoryPersister_Persist_SkipsNonPersistedEvents(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	// These event types should NOT be persisted
	nonPersistedEvents := []Event{
		NewEvent(EventHookTriggered, "hook").WithWorkspace("ws-1"),
		NewEvent(EventFileChanged, "file").WithWorkspace("ws-1"),
		NewEvent(EventTaskCreated, "task").WithWorkspace("ws-1"),
	}

	for _, e := range nonPersistedEvents {
		if err := persister.Persist(context.Background(), e); err != nil {
			t.Fatalf("Persist() error = %v", err)
		}
	}

	// No trajectories or events should be created
	trajectories := store.getTrajectories()
	if len(trajectories) != 0 {
		t.Fatalf("expected 0 trajectories, got %d", len(trajectories))
	}

	events := store.getEvents()
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestTrajectoryPersister_Persist_WithData(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	data := map[string]any{
		"message_id": "msg-123",
		"subject":    "agent.ask",
	}

	event := NewEvent(EventMailReceived, "actor-1").
		WithWorkspace("ws-1").
		WithData(data)

	if err := persister.Persist(context.Background(), event); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}

	events := store.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Check data was preserved
	if events[0].DataInline == nil {
		t.Fatal("data_inline is nil")
	}
	if events[0].DataInline["source"] != "actor-1" {
		t.Errorf("source = %v, want actor-1", events[0].DataInline["source"])
	}
	payload, ok := events[0].DataInline["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload not found or wrong type")
	}
	if payload["message_id"] != "msg-123" {
		t.Errorf("message_id = %v, want msg-123", payload["message_id"])
	}
}

func TestTrajectoryPersister_Persist_DefaultWorkspace(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	// Event without workspace should use "default"
	event := NewEvent(EventAgentStarted, "actor-1")

	if err := persister.Persist(context.Background(), event); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}

	trajectories := store.getTrajectories()
	if len(trajectories) != 1 {
		t.Fatalf("expected 1 trajectory, got %d", len(trajectories))
	}
	if trajectories[0].WorkspaceID != "default" {
		t.Errorf("workspace = %s, want default", trajectories[0].WorkspaceID)
	}
}

func TestTrajectoryPersister_Persist_Concurrent(t *testing.T) {
	store := newMockTrajectoryStore()
	persister := NewTrajectoryPersister(store)

	// Persist events concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			event := NewEvent(EventMailReceived, "actor").
				WithWorkspace("ws-concurrent")
			_ = persister.Persist(context.Background(), event)
		}(i)
	}
	wg.Wait()

	// Should have exactly 1 trajectory (reused)
	trajectories := store.getTrajectories()
	if len(trajectories) != 1 {
		t.Errorf("expected 1 trajectory, got %d", len(trajectories))
	}

	// Should have all 100 events
	events := store.getEvents()
	if len(events) != 100 {
		t.Errorf("expected 100 events, got %d", len(events))
	}
}

func TestMapEventTypeToKind(t *testing.T) {
	tests := []struct {
		eventType EventType
		wantKind  trajectory.EventKind
	}{
		{EventMailReceived, trajectory.EventKindToolCall},
		{EventMailSent, trajectory.EventKindToolResult},
		{EventMailAcked, trajectory.EventKindTaskTransition},
		{EventMailExpired, trajectory.EventKindTaskTransition},
		{EventAgentStarted, trajectory.EventKindTaskTransition},
		{EventAgentStopped, trajectory.EventKindTaskTransition},
		{EventAgentError, trajectory.EventKindAgentThought},
		{EventTaskCompleted, trajectory.EventKindTaskTransition},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			got := mapEventTypeToKind(tt.eventType)
			if got != tt.wantKind {
				t.Errorf("mapEventTypeToKind(%s) = %s, want %s", tt.eventType, got, tt.wantKind)
			}
		})
	}
}

func TestMapActorEventToTrajectory(t *testing.T) {
	data := map[string]any{"key": "value"}
	dataBytes, _ := json.Marshal(data)

	event := Event{
		ID:        "event-123",
		Type:      EventMailReceived,
		Source:    "source-actor",
		Target:    "target-actor",
		Timestamp: time.Now(),
		Data:      dataBytes,
		SessionID: "session-456",
		Workspace: "ws-1",
	}

	trajEvent := mapActorEventToTrajectory(event, "traj-789")

	if trajEvent.ID != "event-123" {
		t.Errorf("ID = %s, want event-123", trajEvent.ID)
	}
	if trajEvent.TrajectoryID != "traj-789" {
		t.Errorf("TrajectoryID = %s, want traj-789", trajEvent.TrajectoryID)
	}
	if trajEvent.Kind != trajectory.EventKindToolCall {
		t.Errorf("Kind = %s, want %s", trajEvent.Kind, trajectory.EventKindToolCall)
	}
	if trajEvent.Actor != "source-actor" {
		t.Errorf("Actor = %s, want source-actor", trajEvent.Actor)
	}
	if trajEvent.DataInline["source"] != "source-actor" {
		t.Errorf("DataInline[source] = %v, want source-actor", trajEvent.DataInline["source"])
	}
	if trajEvent.DataInline["target"] != "target-actor" {
		t.Errorf("DataInline[target] = %v, want target-actor", trajEvent.DataInline["target"])
	}
	if trajEvent.Meta == nil || trajEvent.Meta.TraceID != "session-456" {
		t.Errorf("Meta.TraceID = %v, want session-456", trajEvent.Meta)
	}
}
