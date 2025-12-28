package actor

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestEventType_ShouldPersist(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  bool
	}{
		{EventMailReceived, true},
		{EventMailAcked, true},
		{EventMailExpired, true},
		{EventAgentStarted, true},
		{EventAgentStopped, true},
		{EventAgentError, true},
		{EventTaskCompleted, true},
		{EventMailSent, true},       // persisted
		{EventTaskCreated, false},   // ephemeral
		{EventTaskUpdated, false},   // ephemeral
		{EventHookTriggered, false}, // ephemeral
		{EventHookBlocked, false},   // ephemeral
		{EventFileChanged, false},   // ephemeral
		{EventFileCreated, false},   // ephemeral
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			got := tt.eventType.ShouldPersist()
			if got != tt.expected {
				t.Errorf("ShouldPersist() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewEvent(t *testing.T) {
	event := NewEvent(EventMailReceived, "actor-1")

	if event.ID == "" {
		t.Error("NewEvent() should generate ID")
	}
	if event.Type != EventMailReceived {
		t.Errorf("Type = %v, want %v", event.Type, EventMailReceived)
	}
	if event.Source != "actor-1" {
		t.Errorf("Source = %v, want actor-1", event.Source)
	}
	if event.Timestamp.IsZero() {
		t.Error("NewEvent() should set Timestamp")
	}
}

func TestEvent_WithData(t *testing.T) {
	event := NewEvent(EventTaskCreated, "supervisor")

	data := map[string]string{"task_id": "123"}
	event = event.WithData(data)

	var decoded map[string]string
	if err := json.Unmarshal(event.Data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal data: %v", err)
	}

	if decoded["task_id"] != "123" {
		t.Errorf("Data task_id = %v, want 123", decoded["task_id"])
	}
}

func TestEvent_WithTarget(t *testing.T) {
	event := NewEvent(EventMailSent, "actor-1").WithTarget("actor-2")

	if event.Target != "actor-2" {
		t.Errorf("Target = %v, want actor-2", event.Target)
	}
}

func TestEvent_WithSession(t *testing.T) {
	event := NewEvent(EventAgentStarted, "actor-1").WithSession("session-123")

	if event.SessionID != "session-123" {
		t.Errorf("SessionID = %v, want session-123", event.SessionID)
	}
}

func TestEvent_WithWorkspace(t *testing.T) {
	event := NewEvent(EventAgentStarted, "actor-1").WithWorkspace("/path/to/workspace")

	if event.Workspace != "/path/to/workspace" {
		t.Errorf("Workspace = %v, want /path/to/workspace", event.Workspace)
	}
}

func TestNewEventBus(t *testing.T) {
	eb := NewEventBus()

	if eb == nil {
		t.Fatal("NewEventBus() returned nil")
	}

	stats := eb.Stats()
	if stats.TotalSubscribers != 0 {
		t.Errorf("TotalSubscribers = %d, want 0", stats.TotalSubscribers)
	}
}

func TestEventBus_Subscribe(t *testing.T) {
	eb := NewEventBus()

	ch := eb.Subscribe(EventMailReceived, EventMailSent)

	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	stats := eb.Stats()
	if stats.SubscriberCounts[EventMailReceived] != 1 {
		t.Errorf("SubscriberCounts[EventMailReceived] = %d, want 1", stats.SubscriberCounts[EventMailReceived])
	}
	if stats.SubscriberCounts[EventMailSent] != 1 {
		t.Errorf("SubscriberCounts[EventMailSent] = %d, want 1", stats.SubscriberCounts[EventMailSent])
	}
}

func TestEventBus_SubscribeAll(t *testing.T) {
	eb := NewEventBus()

	ch := eb.SubscribeAll()

	if ch == nil {
		t.Fatal("SubscribeAll() returned nil channel")
	}

	stats := eb.Stats()
	// Should have subscribers for all event types
	if stats.TotalSubscribers < 10 { // We have ~12 event types
		t.Errorf("TotalSubscribers = %d, expected > 10", stats.TotalSubscribers)
	}
}

func TestEventBus_Publish(t *testing.T) {
	eb := NewEventBus()

	ch := eb.Subscribe(EventMailReceived)

	event := NewEvent(EventMailReceived, "actor-1")
	eb.Publish(event)

	select {
	case received := <-ch:
		if received.Type != EventMailReceived {
			t.Errorf("Received event type = %v, want %v", received.Type, EventMailReceived)
		}
		if received.Source != "actor-1" {
			t.Errorf("Received event source = %v, want actor-1", received.Source)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for event")
	}
}

func TestEventBus_Publish_SetsIDAndTimestamp(t *testing.T) {
	eb := NewEventBus()
	ch := eb.Subscribe(EventMailReceived)

	// Publish event without ID and timestamp
	event := Event{Type: EventMailReceived, Source: "test"}
	eb.Publish(event)

	select {
	case received := <-ch:
		if received.ID == "" {
			t.Error("Publish should set ID if empty")
		}
		if received.Timestamp.IsZero() {
			t.Error("Publish should set Timestamp if zero")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for event")
	}
}

func TestEventBus_Publish_NonBlocking(t *testing.T) {
	eb := NewEventBus(WithSubscriberBuffer(1))

	ch := eb.Subscribe(EventMailReceived)

	// Fill the channel
	eb.Publish(NewEvent(EventMailReceived, "actor-1"))
	eb.Publish(NewEvent(EventMailReceived, "actor-1"))

	// This should not block even though channel is full
	done := make(chan bool)
	go func() {
		eb.Publish(NewEvent(EventMailReceived, "actor-1"))
		done <- true
	}()

	select {
	case <-done:
		// Good, publish didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("Publish blocked on full channel")
	}

	// Drain to avoid leaking goroutine
	<-ch
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus()

	ch := eb.Subscribe(EventMailReceived)
	eb.Unsubscribe(ch)

	stats := eb.Stats()
	if stats.SubscriberCounts[EventMailReceived] != 0 {
		t.Errorf("SubscriberCounts after unsubscribe = %d, want 0", stats.SubscriberCounts[EventMailReceived])
	}
}

func TestEventBus_Publish_MultipleSubscribers(t *testing.T) {
	eb := NewEventBus()

	ch1 := eb.Subscribe(EventMailReceived)
	ch2 := eb.Subscribe(EventMailReceived)

	event := NewEvent(EventMailReceived, "actor-1")
	eb.Publish(event)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		select {
		case <-ch1:
			// received
		case <-time.After(100 * time.Millisecond):
			t.Error("ch1 didn't receive event")
		}
	}()

	go func() {
		defer wg.Done()
		select {
		case <-ch2:
			// received
		case <-time.After(100 * time.Millisecond):
			t.Error("ch2 didn't receive event")
		}
	}()

	wg.Wait()
}

func TestEventBus_Publish_NoSubscribers(t *testing.T) {
	eb := NewEventBus()

	// This should not panic
	event := NewEvent(EventMailReceived, "actor-1")
	eb.Publish(event)
}

// mockPersister implements Persister for testing
type mockPersister struct {
	mu          sync.Mutex
	events      []Event
	errToReturn error
}

func (m *mockPersister) Persist(event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errToReturn != nil {
		return m.errToReturn
	}
	m.events = append(m.events, event)
	return nil
}

func (m *mockPersister) getEvents() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

func TestEventBus_Publish_WithPersister(t *testing.T) {
	mp := &mockPersister{}
	eb := NewEventBus(WithPersister(mp))

	// Publish persistable event
	eb.Publish(NewEvent(EventMailReceived, "actor-1"))

	// Wait for async persist
	time.Sleep(50 * time.Millisecond)

	events := mp.getEvents()
	if len(events) != 1 {
		t.Errorf("Persister got %d events, want 1", len(events))
	}
}

func TestEventBus_Publish_EphemeralNotPersisted(t *testing.T) {
	mp := &mockPersister{}
	eb := NewEventBus(WithPersister(mp))

	// Publish ephemeral event
	eb.Publish(NewEvent(EventHookTriggered, "actor-1"))

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	events := mp.getEvents()
	if len(events) != 0 {
		t.Errorf("Persister got %d events for ephemeral, want 0", len(events))
	}
}

func TestEventBus_PublishSync(t *testing.T) {
	mp := &mockPersister{}
	eb := NewEventBus(WithPersister(mp))

	ch := eb.Subscribe(EventMailReceived)

	err := eb.PublishSync(NewEvent(EventMailReceived, "actor-1"))
	if err != nil {
		t.Errorf("PublishSync() error = %v", err)
	}

	// Should have received event
	select {
	case <-ch:
		// Good
	default:
		t.Error("Subscriber didn't receive event")
	}

	// Should be persisted immediately (synchronously)
	events := mp.getEvents()
	if len(events) != 1 {
		t.Errorf("Persister got %d events, want 1", len(events))
	}
}

func TestEventBus_PublishSync_EphemeralNoPersist(t *testing.T) {
	mp := &mockPersister{}
	eb := NewEventBus(WithPersister(mp))

	err := eb.PublishSync(NewEvent(EventHookTriggered, "actor-1"))
	if err != nil {
		t.Errorf("PublishSync() error = %v", err)
	}

	events := mp.getEvents()
	if len(events) != 0 {
		t.Errorf("Persister got %d events for ephemeral, want 0", len(events))
	}
}

func TestEventBus_Stats(t *testing.T) {
	eb := NewEventBus()

	eb.Subscribe(EventMailReceived)
	eb.Subscribe(EventMailReceived)
	eb.Subscribe(EventMailSent)

	stats := eb.Stats()

	if stats.TotalSubscribers != 3 {
		t.Errorf("TotalSubscribers = %d, want 3", stats.TotalSubscribers)
	}
	if stats.SubscriberCounts[EventMailReceived] != 2 {
		t.Errorf("SubscriberCounts[EventMailReceived] = %d, want 2", stats.SubscriberCounts[EventMailReceived])
	}
	if stats.SubscriberCounts[EventMailSent] != 1 {
		t.Errorf("SubscriberCounts[EventMailSent] = %d, want 1", stats.SubscriberCounts[EventMailSent])
	}
}

func TestWithSubscriberBuffer(t *testing.T) {
	eb := NewEventBus(WithSubscriberBuffer(50))

	ch := eb.Subscribe(EventMailReceived)

	// Publish 50 events without reading
	for i := 0; i < 50; i++ {
		eb.Publish(NewEvent(EventMailReceived, "actor-1"))
	}

	// All 50 should be buffered
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 50 {
		t.Errorf("Received %d events, want 50", count)
	}
}
