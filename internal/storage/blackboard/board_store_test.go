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
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
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

func TestBoardStore_RoomWorkspaceKeyNormalizesPathSelectors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	root := t.TempDir()
	messy := root + string(filepath.Separator) + "."

	if _, err := store.EnsureRoom(ctx, messy, "alpha", "Alpha"); err != nil {
		t.Fatalf("EnsureRoom: %v", err)
	}
	if err := store.SendMessage(ctx, &agent.BoardMessage{
		WorkspaceID: messy,
		Stream:      agent.RoomStreamName("alpha"),
		Sender:      "tester",
		Recipient:   "*",
		Subject:     "hello",
		Body:        "world",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	room, err := store.GetRoom(ctx, root, "alpha", "")
	if err != nil {
		t.Fatalf("GetRoom(clean): %v", err)
	}
	if room.WorkspaceID != root {
		t.Fatalf("room.WorkspaceID=%q want %q", room.WorkspaceID, root)
	}
	msgs, err := store.ListRoomMessages(ctx, root, "alpha", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(clean): %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs)=%d want 1", len(msgs))
	}
	if msgs[0].WorkspaceID != root {
		t.Fatalf("msg.WorkspaceID=%q want %q", msgs[0].WorkspaceID, root)
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

func TestOpenBoardStore_MigratesLegacyBoardMessagesRelatedMessageID(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "board.db")
	db, err := sqliteutil.OpenDB(ctx, path, nil)
	if err != nil {
		t.Fatalf("sqliteutil.OpenDB legacy schema: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS board_messages (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	task_id      TEXT,
	stream       TEXT NOT NULL DEFAULT 'coordination',
	sender       TEXT NOT NULL,
	recipient    TEXT NOT NULL,
	kind         TEXT NOT NULL DEFAULT 'info',
	priority     INTEGER NOT NULL DEFAULT 3,
	ack_required INTEGER NOT NULL DEFAULT 0,
	reply_expected INTEGER NOT NULL DEFAULT 0,
	status       TEXT NOT NULL DEFAULT 'unread',
	subject      TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_board_msg_workspace_recipient ON board_messages(workspace_id, recipient);
CREATE INDEX IF NOT EXISTS idx_board_msg_workspace_task ON board_messages(workspace_id, task_id);
CREATE INDEX IF NOT EXISTS idx_board_msg_priority_created ON board_messages(priority, created_at);
`); err != nil {
		t.Fatalf("legacy schema exec: %v", err)
	}

	store, err := OpenBoardStore(ctx, root)
	if err != nil {
		t.Fatalf("OpenBoardStore migrated legacy schema: %v", err)
	}
	defer store.Close()

	msg := agent.BoardMessage{
		WorkspaceID:      "ws1",
		RelatedMessageID: "orig-1",
		Sender:           "admin",
		Recipient:        "actor:agent:coder",
		Subject:          "legacy migrated",
		Body:             "legacy migrated body",
	}
	if err := store.SendMessage(ctx, &msg); err != nil {
		t.Fatalf("SendMessage after migration: %v", err)
	}
}

// TestOpenBoardStore_MigratesLegacyRoomMetadataSandboxConfig verifies that
// existing databases created before the sandbox_config column was added to
// room_metadata are successfully migrated when opened. This guards against
// the CREATE TABLE IF NOT EXISTS limitation where new columns are not added
// to pre-existing tables. (VAL-CROSS-009)
func TestOpenBoardStore_MigratesLegacyRoomMetadataSandboxConfig(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "board.db")

	// Simulate a pre-sandbox_config database by creating the room_metadata table
	// without the sandbox_config column.
	db, err := sqliteutil.OpenDB(ctx, path, nil)
	if err != nil {
		t.Fatalf("sqliteutil.OpenDB legacy schema: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS room_metadata (
	workspace_id TEXT NOT NULL,
	room_id      TEXT NOT NULL,
	stream       TEXT NOT NULL,
	title        TEXT NOT NULL,
	description  TEXT NOT NULL DEFAULT '',
	dispatch_policy TEXT NOT NULL DEFAULT 'all_subtree',
	dispatch_agent_ids TEXT NOT NULL DEFAULT '[]',
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL,
	archived_at  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (workspace_id, room_id)
);
CREATE TABLE IF NOT EXISTS board_messages (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	task_id      TEXT,
	related_message_id TEXT,
	stream       TEXT NOT NULL DEFAULT 'coordination',
	sender       TEXT NOT NULL,
	recipient    TEXT NOT NULL,
	kind         TEXT NOT NULL DEFAULT 'info',
	priority     INTEGER NOT NULL DEFAULT 3,
	ack_required INTEGER NOT NULL DEFAULT 0,
	reply_expected INTEGER NOT NULL DEFAULT 0,
	interrupt    INTEGER NOT NULL DEFAULT 0,
	status       TEXT NOT NULL DEFAULT 'unread',
	subject      TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS room_members (
	workspace_id TEXT NOT NULL,
	room_id      TEXT NOT NULL,
	actor_id     TEXT NOT NULL,
	role         TEXT NOT NULL DEFAULT '',
	backend      TEXT NOT NULL DEFAULT '',
	session      TEXT NOT NULL DEFAULT '',
	pane_id      TEXT NOT NULL DEFAULT '',
	unbound      INTEGER NOT NULL DEFAULT 0,
	joined_at    INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, room_id, actor_id)
);
`); err != nil {
		t.Fatalf("legacy schema exec: %v", err)
	}
	_ = db.Close()

	// OpenBoardStore should migrate without error.
	store, err := OpenBoardStore(ctx, root)
	if err != nil {
		t.Fatalf("OpenBoardStore failed to migrate legacy schema: %v", err)
	}
	defer store.Close()

	// Upsert a room with a SandboxConfig — this writes sandbox_config column.
	room := agent.Room{
		ID:          "migrate-test-room",
		WorkspaceID: "ws-migrate",
		Title:       "Migration Test Room",
		Members:     []agent.RoomMember{{ActorID: "human-a", Role: "coordinator"}},
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/wt/migrate-test",
			WorktreeBranch: "sandbox/room-migrate-test-room",
			TmuxSession:    "agentctl-sandbox-migrate-test-room",
			Runtime:        "worktree",
		},
	}
	if _, err := store.UpsertRoom(ctx, room); err != nil {
		t.Fatalf("UpsertRoom after migration: %v", err)
	}

	// Read back and confirm sandbox_config is persisted.
	summary, err := store.GetRoom(ctx, "ws-migrate", "migrate-test-room", "")
	if err != nil {
		t.Fatalf("GetRoom after migration: %v", err)
	}
	if summary.SandboxConfig == nil {
		t.Fatal("SandboxConfig is nil after migration round-trip")
	}
	if summary.SandboxConfig.WorktreePath != "/tmp/wt/migrate-test" {
		t.Errorf("WorktreePath = %q, want /tmp/wt/migrate-test", summary.SandboxConfig.WorktreePath)
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

	rooms, err := store.ListRooms(ctx, "ws1", "actor:agent:viewer", 10, false)
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

func TestBoardStore_DeleteRoomRemovesMetadataMembersAndMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "cleanup-room",
		WorkspaceID: "ws1",
		Title:       "Cleanup Room",
		Members: []agent.RoomMember{
			{ActorID: "actor:agent:alpha", Role: "researcher"},
		},
	}); err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	msg := &agent.BoardMessage{
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("cleanup-room"),
		Sender:      "human:gui",
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindInfo,
		Priority:    agent.DefaultPriority,
		Status:      agent.BoardMessageStatusUnread,
		Subject:     "Cleanup",
		Body:        "remove this room",
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if err := store.DeleteRoom(ctx, "ws1", "cleanup-room"); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}

	if _, err := store.GetRoom(ctx, "ws1", "cleanup-room", ""); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("GetRoom after delete err=%v want ErrRoomNotFound", err)
	}

	messages, err := store.ListRoomMessages(ctx, "ws1", "cleanup-room", 10)
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("ListRoomMessages after delete err=%v want ErrRoomNotFound", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages slice after delete=%d want 0", len(messages))
	}
}

func TestBoardStore_ArchiveAndRestoreRoom(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "archive-room",
		WorkspaceID: "ws1",
		Title:       "Archive Room",
	}); err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	if err := store.SendMessage(ctx, &agent.BoardMessage{
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("archive-room"),
		Sender:      "actor:agent:a",
		Recipient:   agent.BroadcastRecipient,
		Subject:     "persisted timeline",
		Body:        "keep this after archive",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if err := store.ArchiveRoom(ctx, "ws1", "archive-room"); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}

	activeRooms, err := store.ListRooms(ctx, "ws1", "", 10, false)
	if err != nil {
		t.Fatalf("ListRooms(active): %v", err)
	}
	if len(activeRooms) != 0 {
		t.Fatalf("active rooms=%d want 0", len(activeRooms))
	}

	archivedRooms, err := store.ListRooms(ctx, "ws1", "", 10, true)
	if err != nil {
		t.Fatalf("ListRooms(archived): %v", err)
	}
	if len(archivedRooms) != 1 || archivedRooms[0].ArchivedAt == nil {
		t.Fatalf("archived rooms=%+v want one archived room", archivedRooms)
	}

	if err := store.RestoreRoom(ctx, "ws1", "archive-room"); err != nil {
		t.Fatalf("RestoreRoom: %v", err)
	}
	restoredRooms, err := store.ListRooms(ctx, "ws1", "", 10, false)
	if err != nil {
		t.Fatalf("ListRooms(restored): %v", err)
	}
	if len(restoredRooms) != 1 || restoredRooms[0].ArchivedAt != nil {
		t.Fatalf("restored rooms=%+v want one active room", restoredRooms)
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
		{ActorID: "actor:agent:three", Role: "owner", Backend: "zellij", Session: "fascinating-salamander"},
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
	if updated.Members[0].Backend != "zellij" || updated.Members[0].Session != "fascinating-salamander" {
		t.Fatalf("updated member binding=%+v want zellij/fascinating-salamander", updated.Members[0])
	}
	if len(updated.Participants) != 1 || updated.Participants[0] != "actor:agent:three" {
		t.Fatalf("updated participants=%+v want actor:agent:three", updated.Participants)
	}
}

func TestBoardStore_SandboxConfigPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a room with sandbox config
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "sandbox-room",
		WorkspaceID: "ws1",
		Title:       "Sandbox Room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktrees/sandbox/room-sandbox-room",
			WorktreeBranch: "sandbox/room-sandbox-room",
			TmuxSession:    "agentctl-sandbox-sandbox-room",
			TerminalURL:    "/terminal/sandbox-room",
			Runtime:        "worktree",
			BaseRef:        "main",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom with sandbox: %v", err)
	}

	if room.SandboxConfig == nil {
		t.Fatal("room.SandboxConfig is nil after upsert")
	}
	if room.SandboxConfig.WorktreePath != "/tmp/worktrees/sandbox/room-sandbox-room" {
		t.Errorf("WorktreePath = %q, want %q", room.SandboxConfig.WorktreePath, "/tmp/worktrees/sandbox/room-sandbox-room")
	}
	if room.SandboxConfig.Runtime != "worktree" {
		t.Errorf("Runtime = %q, want %q", room.SandboxConfig.Runtime, "worktree")
	}

	// Read it back via GetRoom
	summary, err := store.GetRoom(ctx, "ws1", "sandbox-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if summary.SandboxConfig == nil {
		t.Fatal("summary.SandboxConfig is nil after GetRoom")
	}
	if summary.SandboxConfig.WorktreePath != "/tmp/worktrees/sandbox/room-sandbox-room" {
		t.Errorf("WorktreePath = %q, want %q", summary.SandboxConfig.WorktreePath, "/tmp/worktrees/sandbox/room-sandbox-room")
	}
	if summary.SandboxConfig.TmuxSession != "agentctl-sandbox-sandbox-room" {
		t.Errorf("TmuxSession = %q, want %q", summary.SandboxConfig.TmuxSession, "agentctl-sandbox-sandbox-room")
	}
	if summary.SandboxConfig.TerminalURL != "/terminal/sandbox-room" {
		t.Errorf("TerminalURL = %q, want %q", summary.SandboxConfig.TerminalURL, "/terminal/sandbox-room")
	}

	// List rooms includes sandbox config
	rooms, err := store.ListRooms(ctx, "ws1", "", 50, false)
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("rooms count = %d, want 1", len(rooms))
	}
	if rooms[0].SandboxConfig == nil {
		t.Fatal("listed room SandboxConfig is nil")
	}
	if rooms[0].SandboxConfig.WorktreeBranch != "sandbox/room-sandbox-room" {
		t.Errorf("WorktreeBranch = %q, want %q", rooms[0].SandboxConfig.WorktreeBranch, "sandbox/room-sandbox-room")
	}
}

func TestBoardStore_SandboxConfig_Update(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create room without sandbox config
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "upgrade-room",
		WorkspaceID: "ws1",
		Title:       "Upgrade Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	if room.SandboxConfig != nil {
		t.Fatal("SandboxConfig should be nil for non-sandbox room")
	}

	// Update with sandbox config (simulates upgrade)
	room.SandboxConfig = &agent.SandboxConfig{
		WorktreePath:   "/tmp/worktrees/sandbox/room-upgrade-room",
		WorktreeBranch: "sandbox/room-upgrade-room",
		TmuxSession:    "agentctl-sandbox-upgrade-room",
		TerminalURL:    "/terminal/upgrade-room",
		Runtime:        "worktree",
	}
	room, err = store.UpsertRoom(ctx, room)
	if err != nil {
		t.Fatalf("UpsertRoom with sandbox upgrade: %v", err)
	}
	if room.SandboxConfig == nil {
		t.Fatal("SandboxConfig should be non-nil after upgrade")
	}

	// Verify persistence
	got, err := store.GetRoom(ctx, "ws1", "upgrade-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.SandboxConfig == nil {
		t.Fatal("SandboxConfig lost after round-trip")
	}
	if got.SandboxConfig.WorktreePath != "/tmp/worktrees/sandbox/room-upgrade-room" {
		t.Errorf("WorktreePath = %q, want %q", got.SandboxConfig.WorktreePath, "/tmp/worktrees/sandbox/room-upgrade-room")
	}
}

func TestBoardStore_SandboxConfig_NonSandboxRoom(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a plain room (no sandbox)
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "plain-room",
		WorkspaceID: "ws1",
		Title:       "Plain Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	if room.SandboxConfig != nil {
		t.Fatal("plain room should have nil SandboxConfig")
	}

	// Verify GetRoom also returns nil
	got, err := store.GetRoom(ctx, "ws1", "plain-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.SandboxConfig != nil {
		t.Fatal("plain room GetRoom should have nil SandboxConfig")
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

func TestBoardStore_RoomMemberTransportEndpointRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "transport-room",
		WorkspaceID: "ws1",
		Title:       "Transport Test Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	members := []agent.RoomMember{
		{
			ActorID:           "claude-a",
			Role:              "worker",
			Backend:           "zellij",
			Session:           "test-session",
			PaneID:            "terminal_0",
			TransportEndpoint: "/tmp/agentctl-pane/test-session/claude-a.sock",
			TransportKind:     "pane_socket",
		},
		{
			ActorID: "droid-a",
			Role:    "worker",
			Backend: "tmux",
			Session: "test-session",
			PaneID:  "%5",
			// TransportEndpoint and TransportKind intentionally empty (legacy mux_pane)
		},
	}

	replaced, err := store.ReplaceRoomMembers(ctx, "ws1", "transport-room", members)
	if err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}
	if len(replaced) != 2 {
		t.Fatalf("replaced members count=%d want 2", len(replaced))
	}

	room, err := store.GetRoom(ctx, "ws1", "transport-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if len(room.Members) != 2 {
		t.Fatalf("members count=%d want 2", len(room.Members))
	}

	byActor := make(map[string]agent.RoomMember)
	for _, m := range room.Members {
		byActor[m.ActorID] = m
	}

	claudeA := byActor["claude-a"]
	if claudeA.TransportEndpoint != "/tmp/agentctl-pane/test-session/claude-a.sock" {
		t.Errorf("claude-a TransportEndpoint=%q want /tmp/agentctl-pane/test-session/claude-a.sock", claudeA.TransportEndpoint)
	}
	if claudeA.TransportKind != "pane_socket" {
		t.Errorf("claude-a TransportKind=%q want pane_socket", claudeA.TransportKind)
	}

	droidA := byActor["droid-a"]
	if droidA.TransportEndpoint != "" {
		t.Errorf("droid-a TransportEndpoint=%q want empty", droidA.TransportEndpoint)
	}
	if droidA.TransportKind != "" {
		t.Errorf("droid-a TransportKind=%q want empty", droidA.TransportKind)
	}
}

func TestBoardStore_UpdateRoomMemberTransport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{ID: "tr-room", WorkspaceID: "ws1", Title: "Transport Register Room"})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	_, err = store.ReplaceRoomMembers(ctx, "ws1", "tr-room", []agent.RoomMember{
		{ActorID: "claude-a", Role: "worker", Backend: "tmux", Session: "s1", PaneID: "%1"},
		{ActorID: "droid-a", Role: "worker"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}

	// Update transport for claude-a.
	endpoint := "/tmp/agentctl-pane/s1/claude-a.sock"
	if err := store.UpdateRoomMemberTransport(ctx, "ws1", "tr-room", "claude-a", endpoint, "pane_socket"); err != nil {
		t.Fatalf("UpdateRoomMemberTransport: %v", err)
	}

	room, err := store.GetRoom(ctx, "ws1", "tr-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	byActor := make(map[string]agent.RoomMember)
	for _, m := range room.Members {
		byActor[m.ActorID] = m
	}

	// claude-a has the updated transport.
	if got := byActor["claude-a"].TransportEndpoint; got != endpoint {
		t.Errorf("claude-a TransportEndpoint=%q want %q", got, endpoint)
	}
	if got := byActor["claude-a"].TransportKind; got != "pane_socket" {
		t.Errorf("claude-a TransportKind=%q want pane_socket", got)
	}
	// droid-a is untouched.
	if got := byActor["droid-a"].TransportEndpoint; got != "" {
		t.Errorf("droid-a TransportEndpoint=%q want empty", got)
	}
	// Existing fields on claude-a are preserved.
	if got := byActor["claude-a"].Backend; got != "tmux" {
		t.Errorf("claude-a Backend=%q want tmux (must be preserved)", got)
	}
	if byActor["claude-a"].DeliveryBinding == nil {
		t.Fatalf("claude-a DeliveryBinding=nil want mirrored binding")
	}
	if got := byActor["claude-a"].DeliveryBinding.TransportEndpoint; got != endpoint {
		t.Errorf("claude-a DeliveryBinding.TransportEndpoint=%q want %q", got, endpoint)
	}
}

func TestBoardStore_UpdateRoomMemberTransport_NotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{ID: "tr-room2", WorkspaceID: "ws1", Title: "Test"})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	_, err = store.ReplaceRoomMembers(ctx, "ws1", "tr-room2", []agent.RoomMember{
		{ActorID: "claude-a", Role: "worker"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}

	// Actor not in room → ErrRoomMemberNotFound.
	err = store.UpdateRoomMemberTransport(ctx, "ws1", "tr-room2", "nobody", "/tmp/nobody.sock", "pane_socket")
	if !errors.Is(err, ErrRoomMemberNotFound) {
		t.Errorf("UpdateRoomMemberTransport for non-member: got %v, want ErrRoomMemberNotFound", err)
	}
}

func TestBoardStore_UpdateRoomMemberBinding(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{ID: "bind-room", WorkspaceID: "ws1", Title: "Binding Room"})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	_, err = store.ReplaceRoomMembers(ctx, "ws1", "bind-room", []agent.RoomMember{
		{ActorID: "droid-a", Role: "worker", Backend: "tmux", Session: "old", PaneID: "%1"},
		{ActorID: "claude-a", Role: "worker"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}

	err = store.UpdateRoomMemberBinding(ctx, "ws1", "bind-room", agent.RoomMember{
		ActorID: "droid-a",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "tmux",
			MuxSession:        "new-session",
			MuxPaneID:         "%42",
			TransportEndpoint: "/tmp/droid-rebind.sock",
			TransportKind:     "pane_socket",
			SubmitMode:        agent.RoomDeliverySubmitModeComposerCtrlEnter,
			Health:            agent.RoomDeliveryHealthReady,
			FallbackPolicy:    agent.RoomDeliveryFallbackAllowLegacyMux,
		},
	})
	if err != nil {
		t.Fatalf("UpdateRoomMemberBinding: %v", err)
	}

	room, err := store.GetRoom(ctx, "ws1", "bind-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	byActor := make(map[string]agent.RoomMember)
	for _, m := range room.Members {
		byActor[m.ActorID] = m
	}
	got := byActor["droid-a"]
	if got.Session != "new-session" || got.PaneID != "%42" {
		t.Fatalf("droid-a binding=(%q,%q) want (new-session,%%42)", got.Session, got.PaneID)
	}
	if got.TransportEndpoint != "/tmp/droid-rebind.sock" || got.TransportKind != "pane_socket" {
		t.Fatalf("droid-a transport=(%q,%q) want updated pane_socket", got.TransportEndpoint, got.TransportKind)
	}
	if got.DeliveryBinding == nil {
		t.Fatal("droid-a DeliveryBinding=nil want persisted binding")
	}
	if got.DeliveryBinding.SubmitMode != agent.RoomDeliverySubmitModeComposerCtrlEnter {
		t.Fatalf("droid-a submit_mode=%q want %q", got.DeliveryBinding.SubmitMode, agent.RoomDeliverySubmitModeComposerCtrlEnter)
	}
	if got.DeliveryBinding.Health != agent.RoomDeliveryHealthReady {
		t.Fatalf("droid-a health=%q want %q", got.DeliveryBinding.Health, agent.RoomDeliveryHealthReady)
	}
	if byActor["claude-a"].Session != "" || byActor["claude-a"].TransportEndpoint != "" {
		t.Fatalf("claude-a was unexpectedly modified: %+v", byActor["claude-a"])
	}
}

func TestBoardStore_UpdateRoomMemberBinding_NotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenBoardStore(ctx, dir)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{ID: "bind-room2", WorkspaceID: "ws1", Title: "Binding Room 2"})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}
	_, err = store.ReplaceRoomMembers(ctx, "ws1", "bind-room2", []agent.RoomMember{
		{ActorID: "claude-a", Role: "worker"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoomMembers: %v", err)
	}

	err = store.UpdateRoomMemberBinding(ctx, "ws1", "bind-room2", agent.RoomMember{ActorID: "missing-a", Backend: "tmux"})
	if !errors.Is(err, ErrRoomMemberNotFound) {
		t.Fatalf("UpdateRoomMemberBinding got %v want ErrRoomMemberNotFound", err)
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
