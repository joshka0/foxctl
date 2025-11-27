package blackboard

import (
	"context"
	"os"
	"path/filepath"
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
	defer store.Close()

	// Send a message
	msg := agent.BoardMessage{
		WorkspaceID: "ws1",
		Sender:      "admin",
		Recipient:   "actor:agent:coder",
		Subject:     "Test message",
		Body:        "This is a test",
		Priority:    1,
	}
	if err := store.SendMessage(ctx, msg); err != nil {
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
	if err := store.SendMessage(ctx, msg); err != nil {
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
	if err := store.SendMessage(ctx, msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Get message ID
	messages, _ := store.Inbox(ctx, agent.InboxFilter{WorkspaceID: "ws1", ActorID: "actor:agent:coder"})
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
	if err := store.Reserve(ctx, res); err != nil {
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
	reservations, _ = store.ListReservations(ctx, "ws1")
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
	if err := store.Reserve(ctx, res); err != nil {
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
	if err := store.Reserve(ctx, res1); err != nil {
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
	if err := store.Reserve(ctx, res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// Should not appear in active reservations
	reservations, _ := store.ListReservations(ctx, "ws1")
	if len(reservations) != 0 {
		t.Errorf("expected 0 active reservations (expired), got %d", len(reservations))
	}

	// Should not conflict
	conflicts, _ := store.CheckConflicts(ctx, "ws1", []string{"src/main.go"}, "actor:agent:coder2", agent.ReservationModeExclusive)
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
	for _, m := range msgs {
		if err := store.SendMessage(ctx, m); err != nil {
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

func TestMain(m *testing.M) {
	// Ensure temp directories are cleaned up
	code := m.Run()
	// Clean up any leftover test databases
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "board_test_*"))
	for _, match := range matches {
		os.RemoveAll(match)
	}
	os.Exit(code)
}
