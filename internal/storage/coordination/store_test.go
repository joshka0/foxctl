package coordination

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/quick"
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
		ManagedBy:               "foxctl.room.loop",
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
	if got.LastDeliveryTrace.MessageID != "msg-9" {
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

func TestStore_GetRoomLoopRejectsNullJSONState(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		column     string
		wantErrSub string
	}{
		{
			name:       "last delivery trace",
			column:     "last_delivery_trace_json",
			wantErrSub: "decode last delivery trace",
		},
		{
			name:       "reply pulse state",
			column:     "reply_pulse_state_json",
			wantErrSub: "decode reply pulse state",
		},
		{
			name:       "task pulse state",
			column:     "task_pulse_state_json",
			wantErrSub: "decode task pulse state",
		},
		{
			name:       "task followup state",
			column:     "task_followup_state_json",
			wantErrSub: "decode task followup state",
		},
		{
			name:       "coordinator pulse state",
			column:     "coordinator_pulse_state_json",
			wantErrSub: "decode coordinator pulse state",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := Open(ctx, t.TempDir())
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer store.Close()

			loop := RoomLoop{
				WorkspaceID:       "ws1",
				RoomID:            "alpha",
				Enabled:           true,
				ManagedBy:         "foxctl.room.loop",
				LastDeliveryTrace: &RoomLoopDeliveryTrace{MessageID: "msg-1"},
				ReplyPulseState:   map[string]RoomLoopPulseState{"msg-1": {Count: 1}},
				TaskPulseState:    map[string]RoomLoopPulseState{"task-1": {Count: 1}},
				TaskFollowupState: map[string]time.Time{"task-1": time.Now().UTC()},
				CoordinatorPulseState: map[string]time.Time{
					"coord-1": time.Now().UTC(),
				},
				PulseInterval:   30 * time.Minute,
				ReplyStaleAfter: 5 * time.Minute,
				TaskStaleAfter:  30 * time.Minute,
			}
			if _, err := store.UpsertRoomLoop(ctx, loop); err != nil {
				t.Fatalf("UpsertRoomLoop: %v", err)
			}

			query := fmt.Sprintf("UPDATE room_loops SET %s = ? WHERE workspace_id = ? AND room_id = ?", tc.column)
			if _, err := store.db.ExecContext(ctx, query, "null", "ws1", "alpha"); err != nil {
				t.Fatalf("corrupt %s: %v", tc.column, err)
			}

			got, err := store.GetRoomLoop(ctx, "ws1", "alpha")
			if err == nil {
				t.Fatalf("GetRoomLoop error = nil, got %+v", got)
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("GetRoomLoop error = %v, want substring %q", err, tc.wantErrSub)
			}
		})
	}
}

func TestStore_UpsertRoomLoopRejectsNegativeSchedulingValues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	base := RoomLoop{
		WorkspaceID:           "ws1",
		RoomID:                "alpha",
		Enabled:               true,
		PulseInterval:         30 * time.Minute,
		TaskFollowupInterval:  0,
		ReplyStaleAfter:       5 * time.Minute,
		TaskStaleAfter:        30 * time.Minute,
		MinPulseFloor:         30 * time.Second,
		InterruptAttemptLimit: 2,
		ReminderBackoffCap:    8,
	}
	if _, err := store.UpsertRoomLoop(ctx, base); err != nil {
		t.Fatalf("upsert base loop: %v", err)
	}

	tests := []struct {
		name string
		edit func(*RoomLoop)
	}{
		{name: "pulse interval", edit: func(loop *RoomLoop) { loop.PulseInterval = -time.Millisecond }},
		{name: "task followup interval", edit: func(loop *RoomLoop) { loop.TaskFollowupInterval = -time.Millisecond }},
		{name: "reply stale after", edit: func(loop *RoomLoop) { loop.ReplyStaleAfter = -time.Millisecond }},
		{name: "task stale after", edit: func(loop *RoomLoop) { loop.TaskStaleAfter = -time.Millisecond }},
		{name: "min pulse floor", edit: func(loop *RoomLoop) { loop.MinPulseFloor = -time.Millisecond }},
		{name: "interrupt attempts", edit: func(loop *RoomLoop) { loop.InterruptAttemptLimit = -1 }},
		{name: "reminder backoff cap", edit: func(loop *RoomLoop) { loop.ReminderBackoffCap = -1 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			invalid := base
			tc.edit(&invalid)
			if _, err := store.UpsertRoomLoop(ctx, invalid); err == nil {
				t.Fatal("UpsertRoomLoop accepted negative scheduling value")
			}
		})
	}

	got, err := store.GetRoomLoop(ctx, "ws1", "alpha")
	if err != nil {
		t.Fatalf("GetRoomLoop after rejected updates: %v", err)
	}
	if got == nil {
		t.Fatal("GetRoomLoop after rejected updates = nil, want base loop")
	}
	if got.PulseInterval != base.PulseInterval ||
		got.TaskFollowupInterval != base.TaskFollowupInterval ||
		got.ReplyStaleAfter != base.ReplyStaleAfter ||
		got.TaskStaleAfter != base.TaskStaleAfter ||
		got.MinPulseFloor != base.MinPulseFloor ||
		got.InterruptAttemptLimit != base.InterruptAttemptLimit ||
		got.ReminderBackoffCap != base.ReminderBackoffCap {
		t.Fatalf("rejected negative scheduling value mutated loop: %+v", got)
	}
}

func TestStore_RoomLoopSchedulingValuesAreNonNegativeProperty(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	var counter int
	prop := func(pulse, followup, replyStale, taskStale, minFloor, attempts, backoff int8) bool {
		counter++
		roomID := fmt.Sprintf("prop-%d", counter)
		loop := RoomLoop{
			WorkspaceID:           "ws-property",
			RoomID:                roomID,
			Enabled:               true,
			PulseInterval:         time.Duration(pulse) * time.Millisecond,
			TaskFollowupInterval:  time.Duration(followup) * time.Millisecond,
			ReplyStaleAfter:       time.Duration(replyStale) * time.Millisecond,
			TaskStaleAfter:        time.Duration(taskStale) * time.Millisecond,
			MinPulseFloor:         time.Duration(minFloor) * time.Millisecond,
			InterruptAttemptLimit: int(attempts),
			ReminderBackoffCap:    int(backoff),
		}
		valid := pulse >= 0 &&
			followup >= 0 &&
			replyStale >= 0 &&
			taskStale >= 0 &&
			minFloor >= 0 &&
			attempts >= 0 &&
			backoff >= 0

		_, upsertErr := store.UpsertRoomLoop(ctx, loop)
		got, getErr := store.GetRoomLoop(ctx, "ws-property", roomID)
		if getErr != nil {
			t.Logf("GetRoomLoop: %v", getErr)
			return false
		}
		if valid {
			return upsertErr == nil && got != nil
		}
		return upsertErr != nil && got == nil
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("room loop scheduling property failed: %v", err)
	}
}

func TestStore_UpsertRoomReminderRejectsNegativeSentCount(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	base := testRoomReminder("reminder-1", "ws1", "alpha")
	base.SentCount = 1
	if _, err := store.UpsertRoomReminder(ctx, base); err != nil {
		t.Fatalf("upsert base reminder: %v", err)
	}

	invalid := base
	invalid.SentCount = -1
	invalid.Subject = "should not persist"
	if _, err := store.UpsertRoomReminder(ctx, invalid); err == nil {
		t.Fatal("UpsertRoomReminder accepted negative sent_count")
	}

	got, err := store.GetRoomReminder(ctx, "ws1", base.ID)
	if err != nil {
		t.Fatalf("GetRoomReminder after rejected update: %v", err)
	}
	if got == nil {
		t.Fatal("GetRoomReminder after rejected update = nil, want base reminder")
	}
	if got.SentCount != base.SentCount {
		t.Fatalf("SentCount=%d want %d", got.SentCount, base.SentCount)
	}
	if got.Subject != base.Subject {
		t.Fatalf("Subject=%q want %q", got.Subject, base.Subject)
	}
}

func TestStore_RoomReminderSentCountIsNonNegativeProperty(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	var counter int
	prop := func(sentCount int8) bool {
		counter++
		reminder := testRoomReminder(fmt.Sprintf("reminder-prop-%d", counter), "ws-property", "alpha")
		reminder.SentCount = int(sentCount)

		_, upsertErr := store.UpsertRoomReminder(ctx, reminder)
		got, getErr := store.GetRoomReminder(ctx, "ws-property", reminder.ID)
		if getErr != nil {
			t.Logf("GetRoomReminder: %v", getErr)
			return false
		}

		if sentCount >= 0 {
			return upsertErr == nil && got != nil && got.SentCount == int(sentCount)
		}
		return upsertErr != nil && got == nil
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("room reminder sent count property failed: %v", err)
	}
}

func testRoomReminder(id, workspaceID, roomID string) RoomReminder {
	return RoomReminder{
		ID:            id,
		WorkspaceID:   workspaceID,
		RoomID:        roomID,
		RootMessageID: "root-1",
		Sender:        "foxctl",
		Recipient:     "agent-a",
		Subject:       "follow up",
		Body:          "Please follow up.",
		Interval:      time.Minute,
		MaxIterations: 3,
		Active:        true,
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

func TestStore_TryAcquireLeaseHonorsExpiryAndRenewsOwner(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	acquired, err := store.TryAcquireLease(ctx, "lease-expiry", "owner-a", time.Hour)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-a): %v", err)
	}
	if !acquired {
		t.Fatal("expected owner-a to acquire new lease")
	}

	acquired, err = store.TryAcquireLease(ctx, "lease-expiry", "owner-b", time.Hour)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-b active): %v", err)
	}
	if acquired {
		t.Fatal("expected active owner-a lease to reject owner-b")
	}
	lease, err := store.GetLease(ctx, "lease-expiry")
	if err != nil {
		t.Fatalf("GetLease after rejected contender: %v", err)
	}
	if lease == nil || lease.OwnerID != "owner-a" {
		t.Fatalf("lease after rejected contender=%+v want owner-a", lease)
	}

	expiredMS := time.Now().UTC().Add(-time.Minute).UnixMilli()
	if _, err := store.db.ExecContext(ctx, `
		UPDATE daemon_leases
		SET expires_at_ms = ?, updated_at_ms = ?
		WHERE name = ?
	`, expiredMS, expiredMS, "lease-expiry"); err != nil {
		t.Fatalf("force lease expiry: %v", err)
	}

	acquired, err = store.TryAcquireLease(ctx, "lease-expiry", "owner-b", time.Hour)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-b expired): %v", err)
	}
	if !acquired {
		t.Fatal("expected expired owner-a lease to be taken over by owner-b")
	}
	lease, err = store.GetLease(ctx, "lease-expiry")
	if err != nil {
		t.Fatalf("GetLease after takeover: %v", err)
	}
	if lease == nil || lease.OwnerID != "owner-b" {
		t.Fatalf("lease after takeover=%+v want owner-b", lease)
	}
	firstOwnerBExpiry := lease.ExpiresAt

	acquired, err = store.TryAcquireLease(ctx, "lease-expiry", "owner-b", 2*time.Hour)
	if err != nil {
		t.Fatalf("TryAcquireLease(owner-b renew): %v", err)
	}
	if !acquired {
		t.Fatal("expected current owner-b to renew active lease")
	}
	lease, err = store.GetLease(ctx, "lease-expiry")
	if err != nil {
		t.Fatalf("GetLease after renewal: %v", err)
	}
	if lease == nil || lease.OwnerID != "owner-b" {
		t.Fatalf("lease after renewal=%+v want owner-b", lease)
	}
	if !lease.ExpiresAt.After(firstOwnerBExpiry) {
		t.Fatalf("renewed expiry=%v want after first owner-b expiry %v", lease.ExpiresAt, firstOwnerBExpiry)
	}
}
