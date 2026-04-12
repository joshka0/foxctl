package cmd

import (
	"testing"
	"time"
)

func TestDefaultRoomLoopPolicyUsesCanonicalPersistedDefaults(t *testing.T) {
	loop := defaultRoomLoopPolicy("/tmp/workspace", "alpha", roomPulseConfig{})

	if !loop.Enabled {
		t.Fatalf("expected loop to default enabled")
	}
	if got := loop.PulseInterval; got != roomLoopDefaultPulseInterval {
		t.Fatalf("PulseInterval = %v, want %v", got, roomLoopDefaultPulseInterval)
	}
	if got := loop.ReplyStaleAfter; got != roomLoopDefaultReplyStale {
		t.Fatalf("ReplyStaleAfter = %v, want %v", got, roomLoopDefaultReplyStale)
	}
	if got := loop.TaskStaleAfter; got != roomLoopDefaultTaskStale {
		t.Fatalf("TaskStaleAfter = %v, want %v", got, roomLoopDefaultTaskStale)
	}
	if got := loop.MinPulseFloor; got != roomLoopMinimumPulseFloor {
		t.Fatalf("MinPulseFloor = %v, want %v", got, roomLoopMinimumPulseFloor)
	}
	if got := loop.InterruptAttemptLimit; got != roomPulseInterruptLimit {
		t.Fatalf("InterruptAttemptLimit = %d, want %d", got, roomPulseInterruptLimit)
	}
	if got := loop.ReminderBackoffCap; got != roomPulseBackoffCap {
		t.Fatalf("ReminderBackoffCap = %d, want %d", got, roomPulseBackoffCap)
	}
	if got := loop.ManagedBy; got != roomLoopManagedBy {
		t.Fatalf("ManagedBy = %q, want %q", got, roomLoopManagedBy)
	}
}

func TestDefaultRoomLoopPolicyPreservesExplicitCurrentValues(t *testing.T) {
	custom := roomPulseConfig{
		Enabled:                      true,
		Interval:                     45 * time.Minute,
		ReplyStaleAfter:              3 * time.Hour,
		TaskStaleAfter:               6 * time.Hour,
		MinPulseFloor:                36 * time.Hour,
		InterruptAttemptLimit:        4,
		ReminderBackoffCap:           10,
		CoordinatorPulseEnabled:      true,
		CoordinatorEscalationEnabled: true,
	}

	loop := defaultRoomLoopPolicy("/tmp/workspace", "alpha", custom)

	if got := loop.PulseInterval; got != custom.Interval {
		t.Fatalf("PulseInterval = %v, want %v", got, custom.Interval)
	}
	if got := loop.ReplyStaleAfter; got != custom.ReplyStaleAfter {
		t.Fatalf("ReplyStaleAfter = %v, want %v", got, custom.ReplyStaleAfter)
	}
	if got := loop.TaskStaleAfter; got != custom.TaskStaleAfter {
		t.Fatalf("TaskStaleAfter = %v, want %v", got, custom.TaskStaleAfter)
	}
}
