package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDedupeStore_Basic(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := OpenSQLiteDedupeStore(ctx, tmpDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	processed, err := store.IsProcessed(ctx, "agent-1", "msg-1")
	require.NoError(t, err)
	assert.False(t, processed, "initially not processed")

	err = store.MarkProcessed(ctx, "agent-1", "msg-1")
	require.NoError(t, err)

	processed, err = store.IsProcessed(ctx, "agent-1", "msg-1")
	require.NoError(t, err)
	assert.True(t, processed, "processed after marking")
}

func TestSQLiteDedupeStore_Persistence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// First session
	store1, err := OpenSQLiteDedupeStore(ctx, tmpDir)
	require.NoError(t, err)
	err = store1.MarkProcessed(ctx, "agent-1", "msg-1")
	require.NoError(t, err)
	require.NoError(t, store1.Close())

	// Second session
	store2, err := OpenSQLiteDedupeStore(ctx, tmpDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store2.Close()) }()

	processed, err := store2.IsProcessed(ctx, "agent-1", "msg-1")
	require.NoError(t, err)
	assert.True(t, processed, "persisted processed state")
}

func TestSQLiteDedupeStore_Cleanup(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := OpenSQLiteDedupeStore(ctx, tmpDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	// Insert old record
	// Manually insert to control timestamp
	_, err = store.db.ExecContext(ctx,
		`INSERT INTO daemon_dedupe (agent_id, message_id, processed_at) VALUES (?, ?, ?)`,
		"agent-1", "old-msg", time.Now().Add(-2*time.Hour).Unix(),
	)
	require.NoError(t, err)

	// Insert new record
	err = store.MarkProcessed(ctx, "agent-1", "new-msg")
	require.NoError(t, err)

	// Cleanup older than 1 hour
	deleted, err := store.Cleanup(ctx, 1*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Verify old is gone
	processed, err := store.IsProcessed(ctx, "agent-1", "old-msg")
	require.NoError(t, err)
	assert.False(t, processed)

	// Verify new remains
	processed, err = store.IsProcessed(ctx, "agent-1", "new-msg")
	require.NoError(t, err)
	assert.True(t, processed)
}

func TestSQLiteDedupeStore_AgentIsolation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := OpenSQLiteDedupeStore(ctx, tmpDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	err = store.MarkProcessed(ctx, "agent-1", "msg-1")
	require.NoError(t, err)

	// Check agent-2 same msg ID
	processed, err := store.IsProcessed(ctx, "agent-2", "msg-1")
	require.NoError(t, err)
	assert.False(t, processed, "agent-2 should not see agent-1's processed message")
}
