package cmd

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/agent"
)

func TestNewPaneCommandHasServeSubcommand(t *testing.T) {
	cmd := newPaneCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "serve" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected pane serve subcommand")
	}
}

func TestPaneChildEnvIncludesParticipantAndRoom(t *testing.T) {
	got := paneChildEnv("gemini-a", "room-1")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "FOXCTL_PARTICIPANT_ID=gemini-a") {
		t.Fatalf("paneChildEnv missing FOXCTL_PARTICIPANT_ID: %v", got)
	}
	if !strings.Contains(joined, "FOXCTL_PARTICIPANT=gemini-a") {
		t.Fatalf("paneChildEnv missing FOXCTL_PARTICIPANT: %v", got)
	}
	if !strings.Contains(joined, "FOXCTL_ROOM_ID=room-1") {
		t.Fatalf("paneChildEnv missing FOXCTL_ROOM_ID: %v", got)
	}
}

func TestRegisterParticipantTransportUpdatesExistingMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	roomID := "test-transport-room"
	if _, err := store.EnsureRoom(ctx, workspace, roomID, roomID); err != nil {
		t.Fatalf("EnsureRoom: %v", err)
	}
	// Add member without transport.
	members := []agent.RoomMember{
		{ActorID: "claude-a", Backend: "tmux", Session: "collab", PaneID: "%42"},
	}
	if _, err := store.ReplaceRoomMembers(ctx, workspace, roomID, members); err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}

	socketPath := "/tmp/test-foxctl/pane/test-transport-room/claude-a.sock"
	cmd, _ := newRoomTestCommand(ctx)
	if err := registerParticipantTransport(cmd, workspace, roomID, "claude-a", socketPath); err != nil {
		t.Fatalf("registerParticipantTransport: %v", err)
	}

	// Re-open the store to read fresh data (separate DB connection).
	freshStore, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore (fresh): %v", err)
	}
	defer freshStore.Close()

	summary, err := freshStore.GetRoom(ctx, workspace, roomID, "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	found := false
	for _, m := range summary.Members {
		if strings.TrimSpace(m.ActorID) == "claude-a" {
			found = true
			if m.TransportEndpoint != socketPath {
				t.Errorf("TransportEndpoint=%q want %q", m.TransportEndpoint, socketPath)
			}
			if m.TransportKind != agent.PaneSocketTransportKind {
				t.Errorf("TransportKind=%q want %q", m.TransportKind, agent.PaneSocketTransportKind)
			}
			if m.DeliveryBinding == nil {
				t.Fatal("DeliveryBinding=nil want mirrored binding")
			}
			if m.DeliveryBinding.TransportEndpoint != socketPath {
				t.Errorf("DeliveryBinding.TransportEndpoint=%q want %q", m.DeliveryBinding.TransportEndpoint, socketPath)
			}
			// Existing fields should be preserved.
			if m.Backend != "tmux" {
				t.Errorf("Backend=%q want tmux (preserved)", m.Backend)
			}
			if m.Session != "collab" {
				t.Errorf("Session=%q want collab (preserved)", m.Session)
			}
			if m.PaneID != "%42" {
				t.Errorf("PaneID=%q want %%42 (preserved)", m.PaneID)
			}
			if m.DeliveryBinding.MuxBackend != "tmux" {
				t.Errorf("DeliveryBinding.MuxBackend=%q want tmux", m.DeliveryBinding.MuxBackend)
			}
			if m.DeliveryBinding.MuxSession != "collab" {
				t.Errorf("DeliveryBinding.MuxSession=%q want collab", m.DeliveryBinding.MuxSession)
			}
			if m.DeliveryBinding.MuxPaneID != "%42" {
				t.Errorf("DeliveryBinding.MuxPaneID=%q want %%42", m.DeliveryBinding.MuxPaneID)
			}
		}
	}
	if !found {
		t.Fatal("claude-a member not found after registration")
	}
}

func TestRegisterParticipantTransportCreatesNewMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	roomID := "test-transport-new"
	if _, err := store.EnsureRoom(ctx, workspace, roomID, roomID); err != nil {
		t.Fatalf("EnsureRoom: %v", err)
	}
	// No members yet.

	socketPath := "/tmp/test-foxctl/pane/test-transport-new/droid-a.sock"
	cmd, _ := newRoomTestCommand(ctx)
	if err := registerParticipantTransport(cmd, workspace, roomID, "droid-a", socketPath); err != nil {
		t.Fatalf("registerParticipantTransport: %v", err)
	}

	summary, err := store.GetRoom(ctx, workspace, roomID, "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	found := false
	for _, m := range summary.Members {
		if strings.TrimSpace(m.ActorID) == "droid-a" {
			found = true
			if m.TransportEndpoint != socketPath {
				t.Errorf("TransportEndpoint=%q want %q", m.TransportEndpoint, socketPath)
			}
			if m.TransportKind != agent.PaneSocketTransportKind {
				t.Errorf("TransportKind=%q want %q", m.TransportKind, agent.PaneSocketTransportKind)
			}
			if m.DeliveryBinding == nil {
				t.Fatal("DeliveryBinding=nil want mirrored binding")
			}
			if m.DeliveryBinding.TransportEndpoint != socketPath {
				t.Errorf("DeliveryBinding.TransportEndpoint=%q want %q", m.DeliveryBinding.TransportEndpoint, socketPath)
			}
		}
	}
	if !found {
		t.Fatal("droid-a member not found after registration")
	}
}

func TestRegisterParticipantTransportConcurrentExistingMembersDoNotClobberEachOther(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	roomID := "test-transport-concurrent"
	if _, err := store.EnsureRoom(ctx, workspace, roomID, roomID); err != nil {
		t.Fatalf("EnsureRoom: %v", err)
	}
	members := []agent.RoomMember{
		{ActorID: "claude-a", Backend: "tmux", Session: "claude", PaneID: "%1"},
		{ActorID: "gemini-a", Backend: "tmux", Session: "gemini", PaneID: "%2"},
	}
	if _, err := store.ReplaceRoomMembers(ctx, workspace, roomID, members); err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}

	cmdA, _ := newRoomTestCommand(ctx)
	cmdB, _ := newRoomTestCommand(ctx)
	socketA := "/tmp/test-foxctl/pane/test-transport-concurrent/claude-a.sock"
	socketB := "/tmp/test-foxctl/pane/test-transport-concurrent/gemini-a.sock"

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errCh <- registerParticipantTransport(cmdA, workspace, roomID, "claude-a", socketA)
	}()
	go func() {
		defer wg.Done()
		errCh <- registerParticipantTransport(cmdB, workspace, roomID, "gemini-a", socketB)
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("registerParticipantTransport concurrent: %v", err)
		}
	}

	summary, err := store.GetRoom(ctx, workspace, roomID, "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	got := map[string]agent.RoomMember{}
	for _, m := range summary.Members {
		got[strings.TrimSpace(m.ActorID)] = m
	}
	if got["claude-a"].TransportEndpoint != socketA {
		t.Fatalf("claude-a TransportEndpoint=%q want %q", got["claude-a"].TransportEndpoint, socketA)
	}
	if got["gemini-a"].TransportEndpoint != socketB {
		t.Fatalf("gemini-a TransportEndpoint=%q want %q", got["gemini-a"].TransportEndpoint, socketB)
	}
	if got["claude-a"].TransportKind != agent.PaneSocketTransportKind {
		t.Fatalf("claude-a TransportKind=%q want %q", got["claude-a"].TransportKind, agent.PaneSocketTransportKind)
	}
	if got["gemini-a"].TransportKind != agent.PaneSocketTransportKind {
		t.Fatalf("gemini-a TransportKind=%q want %q", got["gemini-a"].TransportKind, agent.PaneSocketTransportKind)
	}
	if got["claude-a"].DeliveryBinding == nil || got["claude-a"].DeliveryBinding.TransportEndpoint != socketA {
		t.Fatalf("claude-a DeliveryBinding=%+v want endpoint %q", got["claude-a"].DeliveryBinding, socketA)
	}
	if got["gemini-a"].DeliveryBinding == nil || got["gemini-a"].DeliveryBinding.TransportEndpoint != socketB {
		t.Fatalf("gemini-a DeliveryBinding=%+v want endpoint %q", got["gemini-a"].DeliveryBinding, socketB)
	}
}

func TestRegisterParticipantTransportNoopOnMissingRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	err := registerParticipantTransport(cmd, workspace, "nonexistent-room", "droid-a", "/tmp/sock")
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
}
