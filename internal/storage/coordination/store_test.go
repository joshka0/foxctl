package coordination

import (
	"context"
	"testing"
	"time"
)

func TestStore_RoomWorkspaceKeyNormalizesPathSelectors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	messy := root + "/."
	loop := RoomLoop{
		WorkspaceID:                  messy,
		RoomID:                       "alpha",
		Enabled:                      true,
		ManagedBy:                    "test",
		PulseInterval:                30 * time.Minute,
		TaskFollowupInterval:         0,
		ReplyStaleAfter:              5 * time.Minute,
		TaskStaleAfter:               30 * time.Minute,
		MinPulseFloor:                30 * time.Second,
		InterruptAttemptLimit:        2,
		ReminderBackoffCap:           8,
		CoordinatorPulseEnabled:      true,
		CoordinatorEscalationEnabled: true,
	}
	if _, err := store.UpsertRoomLoop(ctx, loop); err != nil {
		t.Fatalf("UpsertRoomLoop: %v", err)
	}

	got, err := store.GetRoomLoop(ctx, root, "alpha")
	if err != nil {
		t.Fatalf("GetRoomLoop(clean): %v", err)
	}
	if got == nil {
		t.Fatal("GetRoomLoop(clean) = nil, want loop")
	}
	if got.WorkspaceID != root {
		t.Fatalf("loop.WorkspaceID=%q want %q", got.WorkspaceID, root)
	}
}

func TestStore_RoomLoopDeliveryRuntimeRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	lastTick := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	cursorAt := lastTick.Add(30 * time.Second)
	replySentAt := lastTick.Add(-30 * time.Second)
	taskSentAt := lastTick.Add(-15 * time.Second)
	followupSentAt := lastTick.Add(-10 * time.Second)
	coordinatorSentAt := lastTick.Add(-5 * time.Second)
	observedAt := lastTick.Add(45 * time.Second)
	loop := RoomLoop{
		WorkspaceID:             "ws1",
		RoomID:                  "alpha",
		Enabled:                 true,
		ManagedBy:               "agentctl.room.loop",
		LastTickAt:              &lastTick,
		DeliveryLeaseName:       "room-loop:ws1:alpha:delivery",
		DeliveryOwnerID:         "owner-a",
		DeliveryCursorMessageID: "01TESTCURSOR",
		DeliveryCursorAt:        &cursorAt,
		LastDeliveryTrace: &RoomLoopDeliveryTrace{
			WorkspaceID:             "ws1",
			RoomID:                  "alpha",
			MessageID:               "msg-9",
			Recipient:               "gemini-a",
			DeliveryLeaseName:       "room-loop:ws1:alpha:delivery",
			DeliveryOwnerID:         "owner-a",
			RelayBackend:            "auto",
			ChosenActorID:           "gemini-a",
			ChosenMuxBackend:        "tmux",
			ChosenMuxSession:        "room-alpha",
			ChosenMuxPaneID:         "%9",
			ChosenTransportEndpoint: "/tmp/gemini-a.sock",
			ChosenTransportKind:     "pane_socket",
			ChosenSubmitMode:        "composer_ctrl_enter",
			FallbackAttempted:       true,
			DeliveredCount:          1,
			FailedCount:             0,
			DeliveredTo:             []string{"gemini-a"},
			Outcome:                 "delivered",
			CursorBeforeMessageID:   "msg-8",
			CursorAfterMessageID:    "msg-9",
			CursorAdvanced:          true,
			ObservedAt:              observedAt,
		},
		ReplyPulseState: map[string]RoomLoopPulseState{
			"msg-1": {LastSentAt: &replySentAt, Count: 2, Escalated: true},
		},
		TaskPulseState: map[string]RoomLoopPulseState{
			"task-1": {LastSentAt: &taskSentAt, Count: 1},
		},
		TaskFollowupState: map[string]time.Time{
			"task-1": followupSentAt,
		},
		CoordinatorPulseState: map[string]time.Time{
			"coord-1": coordinatorSentAt,
		},
		PulseInterval:   30 * time.Minute,
		ReplyStaleAfter: 5 * time.Minute,
		TaskStaleAfter:  30 * time.Minute,
	}
	if _, err := store.UpsertRoomLoop(ctx, loop); err != nil {
		t.Fatalf("UpsertRoomLoop: %v", err)
	}

	got, err := store.GetRoomLoop(ctx, "ws1", "alpha")
	if err != nil {
		t.Fatalf("GetRoomLoop: %v", err)
	}
	if got == nil {
		t.Fatal("GetRoomLoop = nil, want loop")
	}
	if got.DeliveryLeaseName != loop.DeliveryLeaseName {
		t.Fatalf("DeliveryLeaseName=%q want %q", got.DeliveryLeaseName, loop.DeliveryLeaseName)
	}
	if got.DeliveryOwnerID != loop.DeliveryOwnerID {
		t.Fatalf("DeliveryOwnerID=%q want %q", got.DeliveryOwnerID, loop.DeliveryOwnerID)
	}
	if got.DeliveryCursorMessageID != loop.DeliveryCursorMessageID {
		t.Fatalf("DeliveryCursorMessageID=%q want %q", got.DeliveryCursorMessageID, loop.DeliveryCursorMessageID)
	}
	if got.DeliveryCursorAt == nil || !got.DeliveryCursorAt.Equal(cursorAt) {
		t.Fatalf("DeliveryCursorAt=%v want %v", got.DeliveryCursorAt, cursorAt)
	}
	if got.LastDeliveryTrace == nil {
		t.Fatal("LastDeliveryTrace = nil, want trace")
	}
	if got.LastDeliveryTrace.MessageID != "msg-9" || !got.LastDeliveryTrace.FallbackAttempted {
		t.Fatalf("LastDeliveryTrace=%+v", got.LastDeliveryTrace)
	}
	if !got.LastDeliveryTrace.ObservedAt.Equal(observedAt) {
		t.Fatalf("LastDeliveryTrace.ObservedAt=%v want %v", got.LastDeliveryTrace.ObservedAt, observedAt)
	}
	if got.ReplyPulseState["msg-1"].Count != 2 || !got.ReplyPulseState["msg-1"].Escalated {
		t.Fatalf("ReplyPulseState=%+v", got.ReplyPulseState["msg-1"])
	}
	if got.ReplyPulseState["msg-1"].LastSentAt == nil || !got.ReplyPulseState["msg-1"].LastSentAt.Equal(replySentAt) {
		t.Fatalf("ReplyPulseState.LastSentAt=%v want %v", got.ReplyPulseState["msg-1"].LastSentAt, replySentAt)
	}
	if got.TaskPulseState["task-1"].Count != 1 {
		t.Fatalf("TaskPulseState=%+v", got.TaskPulseState["task-1"])
	}
	if got.TaskFollowupState["task-1"] != followupSentAt {
		t.Fatalf("TaskFollowupState=%v want %v", got.TaskFollowupState["task-1"], followupSentAt)
	}
	if got.CoordinatorPulseState["coord-1"] != coordinatorSentAt {
		t.Fatalf("CoordinatorPulseState=%v want %v", got.CoordinatorPulseState["coord-1"], coordinatorSentAt)
	}
}

func TestStore_TryAcquireLeaseAndRelease(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	acquired, err := store.TryAcquireLease(ctx, "lease-a", "owner-a", time.Minute)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-a): %v", err)
	}
	if !acquired {
		t.Fatal("expected first lease acquisition")
	}
	acquired, err = store.TryAcquireLease(ctx, "lease-a", "owner-b", time.Minute)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-b): %v", err)
	}
	if acquired {
		t.Fatal("expected second owner to be rejected while lease is active")
	}
	if err := store.ReleaseLease(ctx, "lease-a", "owner-a"); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	acquired, err = store.TryAcquireLease(ctx, "lease-a", "owner-b", time.Minute)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-b, after release): %v", err)
	}
	if !acquired {
		t.Fatal("expected lease acquisition after release")
	}
}
