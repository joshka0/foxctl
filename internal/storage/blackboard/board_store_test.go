package blackboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

func TestBoardStore_SendAndInbox(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer func() {
		// Test cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("RemoveAll: %v", err)
		}
	}()

	// Send a message
	msg := agent.BoardMessage{
		WorkspaceID:   "ws1",
		Sender:        "admin",
		Recipient:     "actor:agent:coder",
		Subject:       "Test message",
		Body:          "This is a test",
		Priority:      1,
		ReplyExpected: true,
	}
	if err := store.SendMessage(ctx, &msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Read inbox
	filter := agent.InboxFilter{
		WorkspaceID: "ws1",
		ActorID:     "actor:agent:coder",
	}
	messages, err := store.Inbox(ctx, filter)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Subject != "Test message" {
		t.Errorf("expected subject 'Test message', got %q", messages[0].Subject)
	}
	if messages[0].Sender != "admin" {
		t.Errorf("expected sender 'admin', got %q", messages[0].Sender)
	}
	if !messages[0].ReplyExpected {
		t.Errorf("expected reply_expected=true, got false")
	}
}

func TestBoardStore_BroadcastMessage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Send broadcast message
	msg := agent.BoardMessage{
		WorkspaceID: "ws1",
		Sender:      "actor:system:overseer",
		Recipient:   "*", // Broadcast
		Subject:     "Broadcast",
		Body:        "Everyone should see this",
	}
	if err := store.SendMessage(ctx, &msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Any actor should see broadcast messages
	filter := agent.InboxFilter{
		WorkspaceID: "ws1",
		ActorID:     "actor:agent:random",
	}
	messages, err := store.Inbox(ctx, filter)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 broadcast message, got %d", len(messages))
	}
}

func TestBoardStore_AckMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Send message
	msg := agent.BoardMessage{
		WorkspaceID: "ws1",
		Sender:      "admin",
		Recipient:   "actor:agent:coder",
		Subject:     "Needs ack",
		AckRequired: true,
	}
	if err := store.SendMessage(ctx, &msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Get message ID
	messages, err := store.Inbox(ctx, agent.InboxFilter{WorkspaceID: "ws1", ActorID: "actor:agent:coder"})
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("no messages found")
	}

	// Ack the message
	count, err := store.AckMessages(ctx, "ws1", "actor:agent:coder", []string{messages[0].ID})
	if err != nil {
		t.Fatalf("AckMessages: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 acked, got %d", count)
	}
}

func TestBoardStore_InboxFiltersByStreamAndTask(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	msgs := []agent.BoardMessage{
		{
			WorkspaceID: "ws1",
			TaskID:      "task-1",
			Stream:      "room:alpha",
			Sender:      "actor:agent:a",
			Recipient:   "actor:agent:coder",
			Subject:     "alpha-task-1",
			Body:        "match",
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-2",
			Stream:      "room:alpha",
			Sender:      "actor:agent:b",
			Recipient:   "actor:agent:coder",
			Subject:     "alpha-task-2",
			Body:        "wrong-task",
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-1",
			Stream:      "room:beta",
			Sender:      "actor:agent:c",
			Recipient:   "actor:agent:coder",
			Subject:     "beta-task-1",
			Body:        "wrong-stream",
		},
	}
	for i := range msgs {
		if err := store.SendMessage(ctx, &msgs[i]); err != nil {
			t.Fatalf("SendMessage[%d]: %v", i, err)
		}
	}

	got, err := store.Inbox(ctx, agent.InboxFilter{
		WorkspaceID: "ws1",
		ActorID:     "actor:agent:coder",
		TaskID:      "task-1",
		Stream:      "room:alpha",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 filtered message, got %d", len(got))
	}
	if got[0].Subject != "alpha-task-1" {
		t.Fatalf("subject=%q want alpha-task-1", got[0].Subject)
	}
}

func TestBoardStore_ListRoomsAndRoomMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	msgs := []agent.BoardMessage{
		{
			WorkspaceID: "ws1",
			TaskID:      "task-1",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      "actor:agent:a",
			Recipient:   agent.BroadcastRecipient,
			Subject:     "alpha-1",
			Body:        "first",
			Status:      agent.BoardMessageStatusRead,
			CreatedAt:   now.Add(-2 * time.Minute),
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-2",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      "actor:agent:b",
			Recipient:   "actor:agent:viewer",
			Subject:     "alpha-2",
			Body:        "second",
			Status:      agent.BoardMessageStatusUnread,
			CreatedAt:   now.Add(-1 * time.Minute),
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-3",
			Stream:      agent.RoomStreamName("beta"),
			Sender:      "actor:agent:c",
			Recipient:   agent.BroadcastRecipient,
			Subject:     "beta-1",
			Body:        "third",
			Status:      agent.BoardMessageStatusUnread,
			CreatedAt:   now,
		},
	}
	for i := range msgs {
		if err := store.SendMessage(ctx, &msgs[i]); err != nil {
			t.Fatalf("SendMessage[%d]: %v", i, err)
		}
	}

	rooms, err := store.ListRooms(ctx, "ws1", "actor:agent:viewer", 10)
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("rooms=%d want 2", len(rooms))
	}
	if rooms[0].ID != "beta" {
		t.Fatalf("latest room=%q want beta", rooms[0].ID)
	}
	if rooms[1].ID != "alpha" {
		t.Fatalf("second room=%q want alpha", rooms[1].ID)
	}
	if rooms[1].UnreadCount != 1 {
		t.Fatalf("alpha unread=%d want 1", rooms[1].UnreadCount)
	}
	if rooms[1].MessageCount != 2 {
		t.Fatalf("alpha message_count=%d want 2", rooms[1].MessageCount)
	}
	if len(rooms[1].Members) != 0 {
		t.Fatalf("alpha members=%d want 0", len(rooms[1].Members))
	}
	if len(rooms[1].Participants) != 3 {
		t.Fatalf("alpha participants=%d want 3", len(rooms[1].Participants))
	}

	room, err := store.GetRoom(ctx, "ws1", "alpha", "actor:agent:viewer")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if room.Title != "alpha-2" {
		t.Fatalf("room title=%q want alpha-2", room.Title)
	}
	if len(room.TaskIDs) != 2 {
		t.Fatalf("alpha task_ids=%d want 2", len(room.TaskIDs))
	}

	roomMessages, err := store.ListRoomMessages(ctx, "ws1", "alpha", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}
	if len(roomMessages) != 2 {
		t.Fatalf("room messages=%d want 2", len(roomMessages))
	}
	if roomMessages[0].Subject != "alpha-1" || roomMessages[1].Subject != "alpha-2" {
		t.Fatalf("room messages not chronological: %+v", []string{roomMessages[0].Subject, roomMessages[1].Subject})
	}
}

func TestBoardStore_RoomMetadataAndMembers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "alpha",
		WorkspaceID: "ws1",
		Title:       "Alpha Room",
		Description: "Primary coordination room",
		Members: []agent.RoomMember{
			{ActorID: "actor:agent:one", Role: "owner"},
			{ActorID: "actor:agent:two", Role: "member"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	if room.Stream != agent.RoomStreamName("alpha") {
		t.Fatalf("stream=%q want %q", room.Stream, agent.RoomStreamName("alpha"))
	}

	summary, err := store.GetRoom(ctx, "ws1", "alpha", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if summary.Title != "Alpha Room" {
		t.Fatalf("title=%q want Alpha Room", summary.Title)
	}
	if summary.Description != "Primary coordination room" {
		t.Fatalf("description=%q want Primary coordination room", summary.Description)
	}
	if len(summary.Members) != 2 {
		t.Fatalf("members=%d want 2", len(summary.Members))
	}
	if len(summary.Participants) != 2 {
		t.Fatalf("participants=%d want 2", len(summary.Participants))
	}

	messages, err := store.ListRoomMessages(ctx, "ws1", "alpha", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages metadata-only room: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("metadata-only room messages=%d want 0", len(messages))
	}

	replaced, err := store.ReplaceRoomMembers(ctx, "ws1", "alpha", []agent.RoomMember{
		{ActorID: "actor:agent:three", Role: "owner"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}
	if len(replaced) != 1 {
		t.Fatalf("replaced members=%d want 1", len(replaced))
	}

	updated, err := store.GetRoom(ctx, "ws1", "alpha", "")
	if err != nil {
		t.Fatalf("GetRoom after replace members: %v", err)
	}
	if len(updated.Members) != 1 || updated.Members[0].ActorID != "actor:agent:three" {
		t.Fatalf("updated members=%+v want actor:agent:three", updated.Members)
	}
	if len(updated.Participants) != 1 || updated.Participants[0] != "actor:agent:three" {
		t.Fatalf("updated participants=%+v want actor:agent:three", updated.Participants)
	}
}

func TestBoardStore_ReserveAndRelease(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Reserve a file
	res := agent.FileReservation{
		WorkspaceID: "ws1",
		Path:        "src/main.go",
		Holder:      "actor:agent:coder",
		Mode:        agent.ReservationModeExclusive,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
	if err := store.Reserve(ctx, &res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// List reservations
	reservations, err := store.ListReservations(ctx, "ws1")
	if err != nil {
		t.Fatalf("ListReservations: %v", err)
	}
	if len(reservations) != 1 {
		t.Fatalf("expected 1 reservation, got %d", len(reservations))
	}

	// Release
	count, err := store.Release(ctx, "ws1", "actor:agent:coder", []string{"src/main.go"})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 released, got %d", count)
	}

	// Should be empty now
	reservations, err = store.ListReservations(ctx, "ws1")
	if err != nil {
		t.Fatalf("ListReservations: %v", err)
	}
	if len(reservations) != 0 {
		t.Errorf("expected 0 reservations after release, got %d", len(reservations))
	}
}

func TestBoardStore_ReservationConflicts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// First actor reserves exclusively
	res := agent.FileReservation{
		WorkspaceID: "ws1",
		Path:        "src/main.go",
		Holder:      "actor:agent:coder1",
		Mode:        agent.ReservationModeExclusive,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
	if err := store.Reserve(ctx, &res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// Second actor tries to reserve same file
	conflicts, err := store.CheckConflicts(ctx, "ws1", []string{"src/main.go"}, "actor:agent:coder2", agent.ReservationModeExclusive)
	if err != nil {
		t.Fatalf("CheckConflicts: %v", err)
	}

	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Holder != "actor:agent:coder1" {
		t.Errorf("expected holder 'actor:agent:coder1', got %q", conflicts[0].Holder)
	}
}

func TestBoardStore_SharedReservations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// First actor reserves shared
	res1 := agent.FileReservation{
		WorkspaceID: "ws1",
		Path:        "docs/README.md",
		Holder:      "actor:agent:coder1",
		Mode:        agent.ReservationModeShared,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
	if err := store.Reserve(ctx, &res1); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// Second actor can also reserve shared (no conflict)
	conflicts, err := store.CheckConflicts(ctx, "ws1", []string{"docs/README.md"}, "actor:agent:coder2", agent.ReservationModeShared)
	if err != nil {
		t.Fatalf("CheckConflicts: %v", err)
	}

	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts for shared+shared, got %d", len(conflicts))
	}
}

func TestBoardStore_ExpiredReservations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create already-expired reservation
	res := agent.FileReservation{
		WorkspaceID: "ws1",
		Path:        "src/main.go",
		Holder:      "actor:agent:coder1",
		Mode:        agent.ReservationModeExclusive,
		ExpiresAt:   time.Now().Add(-1 * time.Minute), // Already expired
	}
	if err := store.Reserve(ctx, &res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// Should not appear in active reservations
	reservations, err := store.ListReservations(ctx, "ws1")
	if err != nil {
		t.Fatalf("ListReservations: %v", err)
	}
	if len(reservations) != 0 {
		t.Errorf("expected 0 active reservations (expired), got %d", len(reservations))
	}

	// Should not conflict
	conflicts, err := store.CheckConflicts(ctx, "ws1", []string{"src/main.go"}, "actor:agent:coder2", agent.ReservationModeExclusive)
	if err != nil {
		t.Fatalf("CheckConflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts (expired), got %d", len(conflicts))
	}
}

func TestBoardStore_PriorityOrdering(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Send messages with different priorities (out of order)
	msgs := []agent.BoardMessage{
		{WorkspaceID: "ws1", Sender: "admin", Recipient: "actor:agent:coder", Subject: "Low priority", Priority: 5},
		{WorkspaceID: "ws1", Sender: "admin", Recipient: "actor:agent:coder", Subject: "High priority", Priority: 1},
		{WorkspaceID: "ws1", Sender: "admin", Recipient: "actor:agent:coder", Subject: "Medium priority", Priority: 3},
	}
	for i := range msgs {
		if err := store.SendMessage(ctx, &msgs[i]); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}

	// Inbox should return highest priority first
	messages, err := store.Inbox(ctx, agent.InboxFilter{WorkspaceID: "ws1", ActorID: "actor:agent:coder"})
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].Priority != 1 {
		t.Errorf("expected first message priority 1, got %d", messages[0].Priority)
	}
	if messages[1].Priority != 3 {
		t.Errorf("expected second message priority 3, got %d", messages[1].Priority)
	}
	if messages[2].Priority != 5 {
		t.Errorf("expected third message priority 5, got %d", messages[2].Priority)
	}
}

func TestSurfacedLifecycleAndFilters(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenBoardStore(ctx, root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	workspaceID := "ws"
	recipient := "overseer"

	// Create an unread message (default)
	msg := &agent.BoardMessage{
		WorkspaceID: workspaceID,
		Sender:      "human",
		Recipient:   recipient,
		Subject:     "Test",
		Body:        "Hello",
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msg.Status != agent.BoardMessageStatusUnread {
		t.Fatalf("expected status unread, got %q", msg.Status)
	}

	// Mark as surfaced
	if _, err := store.MarkSurfaced(ctx, workspaceID, recipient, []string{msg.ID}); err != nil {
		t.Fatalf("MarkSurfaced: %v", err)
	}

	// OnlyUnread should include surfaced
	got, err := store.Inbox(ctx, agent.InboxFilter{
		WorkspaceID: workspaceID,
		ActorID:     recipient,
		OnlyUnread:  true,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Inbox OnlyUnread: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Status != agent.BoardMessageStatusSurfaced {
		t.Fatalf("expected status surfaced, got %q", got[0].Status)
	}

	// OnlyUnsurfaced should exclude surfaced
	got, err = store.Inbox(ctx, agent.InboxFilter{
		WorkspaceID:    workspaceID,
		ActorID:        recipient,
		OnlyUnread:     true,
		OnlyUnsurfaced: true,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("Inbox OnlyUnsurfaced: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(got))
	}

	// MarkRead should convert surfaced -> read
	if _, err := store.MarkRead(ctx, workspaceID, recipient, []string{msg.ID}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	// Now OnlyUnread should return none
	got, err = store.Inbox(ctx, agent.InboxFilter{
		WorkspaceID: workspaceID,
		ActorID:     recipient,
		OnlyUnread:  true,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Inbox OnlyUnread after MarkRead: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(got))
	}
}

func TestCountMessagesByTaskCountsSurfaced(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenBoardStore(ctx, root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	workspaceID := "ws"
	taskID := "task-1"
	recipient := "overseer"

	// Unread
	m1 := &agent.BoardMessage{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Sender:      "human",
		Recipient:   recipient,
		Subject:     "A",
		Body:        "A",
	}
	if err := store.SendMessage(ctx, m1); err != nil {
		t.Fatalf("SendMessage m1: %v", err)
	}

	// Another unread that we'll surface
	m2 := &agent.BoardMessage{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Sender:      "human",
		Recipient:   recipient,
		Subject:     "B",
		Body:        "B",
	}
	if err := store.SendMessage(ctx, m2); err != nil {
		t.Fatalf("SendMessage m2: %v", err)
	}
	if _, err := store.MarkSurfaced(ctx, workspaceID, recipient, []string{m2.ID}); err != nil {
		t.Fatalf("MarkSurfaced m2: %v", err)
	}

	admin, overseer, total, err := store.CountMessagesByTask(ctx, workspaceID, taskID)
	if err != nil {
		t.Fatalf("CountMessagesByTask: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2 (unread+surfaced), got %d (admin=%d overseer=%d)", total, admin, overseer)
	}
}

func TestRetryBoardBusyRetriesBusyErrors(t *testing.T) {
	var calls int32
	err := retryBoardBusy(context.Background(), func() error {
		if atomic.AddInt32(&calls, 1) < 3 {
			return errors.New("database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryBoardBusy: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls=%d want 3", got)
	}
}

func TestRetryBoardBusyDoesNotRetryNonBusyErrors(t *testing.T) {
	var calls int32
	want := errors.New("boom")
	err := retryBoardBusy(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want %v", err, want)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls=%d want 1", got)
	}
}

func TestMain(m *testing.M) {
	// Ensure temp directories are cleaned up
	code := m.Run()
	// Clean up any leftover test databases
	// Test cleanup; errors are not actionable.
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "board_test_*")) //nolint:errcheck
	for _, match := range matches {
		_ = os.RemoveAll(match) //nolint:errcheck
	}
	os.Exit(code)
}
