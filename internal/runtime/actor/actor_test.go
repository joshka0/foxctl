package actor

import (
	"testing"
	"time"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateStarting, "starting"},
		{StateIdle, "idle"},
		{StateProcessing, "processing"},
		{StateStopped, "stopped"},
		{StateError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("State = %q, want %q", tt.state, tt.expected)
			}
		})
	}
}

func TestDirective_Values(t *testing.T) {
	// Ensure directives have expected values
	if DirectiveResume != 0 {
		t.Errorf("DirectiveResume = %d, want 0", DirectiveResume)
	}
	if DirectiveRestart != 1 {
		t.Errorf("DirectiveRestart = %d, want 1", DirectiveRestart)
	}
	if DirectiveStop != 2 {
		t.Errorf("DirectiveStop = %d, want 2", DirectiveStop)
	}
	if DirectiveEscalate != 3 {
		t.Errorf("DirectiveEscalate = %d, want 3", DirectiveEscalate)
	}
}

func TestTimerEvent(t *testing.T) {
	now := time.Now()
	timer := TimerEvent{
		Name:      "timeout",
		Deadline:  now,
		Namespace: "actor-1",
		Data:      map[string]string{"key": "value"},
	}

	if timer.Name != "timeout" {
		t.Errorf("Name = %q, want timeout", timer.Name)
	}
	if timer.Deadline != now {
		t.Errorf("Deadline = %v, want %v", timer.Deadline, now)
	}
	if timer.Namespace != "actor-1" {
		t.Errorf("Namespace = %q, want actor-1", timer.Namespace)
	}
	if timer.Data == nil {
		t.Error("Data should not be nil")
	}
}

func TestMessage(t *testing.T) {
	now := time.Now()
	msg := Message{
		ID:        "msg-1",
		FromNS:    "actor-1",
		ToNS:      "actor-2",
		Subject:   "test",
		Body:      []byte("hello"),
		Priority:  1,
		CreatedAt: now,
	}

	if msg.ID != "msg-1" {
		t.Errorf("ID = %q, want msg-1", msg.ID)
	}
	if msg.FromNS != "actor-1" {
		t.Errorf("FromNS = %q, want actor-1", msg.FromNS)
	}
	if msg.ToNS != "actor-2" {
		t.Errorf("ToNS = %q, want actor-2", msg.ToNS)
	}
	if string(msg.Body) != "hello" {
		t.Errorf("Body = %q, want hello", string(msg.Body))
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig("test/actor")

	if cfg.ID != "test/actor" {
		t.Errorf("ID = %q, want test/actor", cfg.ID)
	}
	if cfg.Namespace != "test/actor" {
		t.Errorf("Namespace = %q, want test/actor", cfg.Namespace)
	}
	if cfg.LeaseTimeout != 5*time.Minute {
		t.Errorf("LeaseTimeout = %v, want 5m", cfg.LeaseTimeout)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.Metadata == nil {
		t.Error("Metadata should not be nil")
	}
}

func TestStats(t *testing.T) {
	stats := &Stats{}

	if stats.MessagesProcessed != 0 {
		t.Errorf("MessagesProcessed = %d, want 0", stats.MessagesProcessed)
	}

	stats.MessagesProcessed = 10
	stats.MessagesErrored = 2
	stats.RestartCount = 1
	stats.TotalProcessingNs = 1000000000 // 1 second

	if stats.MessagesProcessed != 10 {
		t.Errorf("MessagesProcessed = %d, want 10", stats.MessagesProcessed)
	}
	if stats.MessagesErrored != 2 {
		t.Errorf("MessagesErrored = %d, want 2", stats.MessagesErrored)
	}
	if stats.RestartCount != 1 {
		t.Errorf("RestartCount = %d, want 1", stats.RestartCount)
	}

	avg := stats.AverageProcessingTime()
	if avg != 100*time.Millisecond {
		t.Errorf("AverageProcessingTime = %v, want 100ms", avg)
	}
}

func TestStats_AverageProcessingTime_Zero(t *testing.T) {
	stats := &Stats{}
	avg := stats.AverageProcessingTime()
	if avg != 0 {
		t.Errorf("AverageProcessingTime with zero messages = %v, want 0", avg)
	}
}

func TestActorRef(t *testing.T) {
	ref := ActorRef{
		ID:        "actor-1",
		Namespace: "ns/actor",
		State:     StateIdle,
	}

	if ref.ID != "actor-1" {
		t.Errorf("ID = %q, want actor-1", ref.ID)
	}
	if ref.Namespace != "ns/actor" {
		t.Errorf("Namespace = %q, want ns/actor", ref.Namespace)
	}
	if ref.State != StateIdle {
		t.Errorf("State = %v, want %v", ref.State, StateIdle)
	}
}
