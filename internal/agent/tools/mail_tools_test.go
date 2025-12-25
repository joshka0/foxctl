package tools

import (
	"context"
	"encoding/json"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseResult(t *testing.T, result *models.CallToolResult) map[string]any {
	t.Helper()
	require.False(t, result.IsError, "tool execution failed: %v", result.Content)
	require.NotEmpty(t, result.Content, "no content returned")

	text, ok := result.Content[0].(models.TextContent)
	require.True(t, ok, "expected TextContent")

	var data map[string]any
	err := json.Unmarshal([]byte(text.Text), &data)
	require.NoError(t, err, "unmarshal result")
	return data
}

func TestMailSend_PersistsMessage(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:1",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	args := map[string]any{
		"recipient": "actor:agent:2",
		"subject":   "Hello",
		"body":      "World",
		"kind":      "info",
	}
	result, err := r.mailSend(ctx, args)
	require.NoError(t, err)
	parseResult(t, result)

	// Verify persistence via store
	store, err := blackboard.OpenBoardStore(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	messages, err := store.Inbox(ctx, agent.InboxFilter{
		WorkspaceID: "ws-1",
		ActorID:     "actor:agent:2",
	})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "Hello", messages[0].Subject)
}

func TestMailInbox_FiltersByUnread(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	store, err := blackboard.OpenBoardStore(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	err = store.SendMessage(ctx, &agent.BoardMessage{
		WorkspaceID: "ws-1",
		Recipient:   "actor:agent:1",
		Subject:     "Msg1",
		Status:      agent.BoardMessageStatusRead,
	})
	require.NoError(t, err)
	err = store.SendMessage(ctx, &agent.BoardMessage{
		WorkspaceID: "ws-1",
		Recipient:   "actor:agent:1",
		Subject:     "Msg2",
		Status:      agent.BoardMessageStatusUnread,
	})
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:1",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Test unread_only=true (default)
	result, err := r.mailInbox(ctx, map[string]any{})
	require.NoError(t, err)
	parsed := parseResult(t, result)
	count1, ok := parsed["count"].(float64)
	require.True(t, ok, "count should be a float64")
	assert.Equal(t, 1, int(count1))

	// Test unread_only=false
	result, err = r.mailInbox(ctx, map[string]any{"unread_only": false})
	require.NoError(t, err)
	parsed = parseResult(t, result)
	count2, ok := parsed["count"].(float64)
	require.True(t, ok, "count should be a float64")
	assert.Equal(t, 2, int(count2))
}

func TestMailAck_TransitionsStatus(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	store, err := blackboard.OpenBoardStore(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	msg := &agent.BoardMessage{
		ID:          "msg-1",
		WorkspaceID: "ws-1",
		Recipient:   "actor:agent:1",
		Status:      agent.BoardMessageStatusUnread,
	}
	err = store.SendMessage(ctx, msg)
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:1",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	result, err := r.mailAck(ctx, map[string]any{"message_id": "msg-1"})
	require.NoError(t, err)
	parseResult(t, result)

	// Verify status
	updated, err := store.Inbox(ctx, agent.InboxFilter{WorkspaceID: "ws-1", ActorID: "actor:agent:1", OnlyUnread: false})
	require.NoError(t, err)
	assert.Equal(t, agent.BoardMessageStatusAcked, updated[0].Status)
}

func TestMailReserve_CreatesReservation(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:1",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	args := map[string]any{
		"paths": []any{"file1.txt"},
		"mode":  "exclusive",
	}
	result, err := r.mailReserve(ctx, args)
	require.NoError(t, err)
	parseResult(t, result)

	store, err := blackboard.OpenBoardStore(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	reservations, err := store.ListReservations(ctx, "ws-1")
	require.NoError(t, err)
	require.Len(t, reservations, 1)
	assert.Contains(t, reservations[0].Path, "file1.txt")
}

func TestMailReserve_DetectsConflict(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	// Use the tool to create the first reservation (actor:agent:2)
	cfg1 := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:2",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r1, err := NewRegistry(cfg1, nil)
	require.NoError(t, err)

	// First actor reserves the file
	result1, err := r1.mailReserve(ctx, map[string]any{
		"paths":       []any{"file1.txt"},
		"mode":        "exclusive",
		"ttl_seconds": float64(3600),
	})
	require.NoError(t, err)
	data1 := parseResult(t, result1)
	require.True(t, data1["success"].(bool), "first reservation should succeed")

	// Now try to reserve the same file as a different actor
	cfg2 := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:1",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r2, err := NewRegistry(cfg2, nil)
	require.NoError(t, err)

	result2, err := r2.mailReserve(ctx, map[string]any{
		"paths": []any{"file1.txt"},
	})
	require.NoError(t, err)

	// Should be success: false in result content, not an execution error
	data2 := parseResult(t, result2)
	assert.False(t, data2["success"].(bool))
	assert.Equal(t, "reservation conflict", data2["error"])
	assert.NotNil(t, data2["conflicts"], "should include conflict details")
}

func TestMailRelease_RemovesReservation(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:agent:1",
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return blackboard.OpenBoardStore(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Create reservation via tool to ensure consistent path resolution
	result1, err := r.mailReserve(ctx, map[string]any{
		"paths":       []any{"file1.txt"},
		"mode":        "exclusive",
		"ttl_seconds": float64(3600),
	})
	require.NoError(t, err)
	data1 := parseResult(t, result1)
	require.True(t, data1["success"].(bool), "reservation should succeed")

	// Now release it
	result2, err := r.mailRelease(ctx, map[string]any{
		"paths": []any{"file1.txt"},
	})
	require.NoError(t, err)
	parseResult(t, result2)

	// Verify it's gone
	store, err := blackboard.OpenBoardStore(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	reservations, err := store.ListReservations(ctx, "ws-1")
	require.NoError(t, err)
	require.Empty(t, reservations)
}
