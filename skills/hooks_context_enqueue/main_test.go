package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

// Tests for validation

func TestContextEnqueue_MissingWorkspaceID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		SessionID: "session-123",
		Source:    "test",
		Text:      "some text",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace_id is required")
}

func TestContextEnqueue_MissingSessionID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		Source:      "test",
		Text:        "some text",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id is required")
}

func TestContextEnqueue_MissingSource(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Text:        "some text",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source is required")
}

func TestContextEnqueue_MissingText(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "test",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is required")
}

// Tests for successful enqueue

func TestContextEnqueue_BasicSuccess(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "test-hook",
		Text:        "## Important Context\n\nThis is some context.",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.NotEmpty(t, data["id"])
	assert.Equal(t, "test-hook", data["source"])
	assert.NotEmpty(t, data["expires_at"])
	assert.GreaterOrEqual(t, data["total_pending"], float64(1))
}

func TestContextEnqueue_CustomPriority(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "test-hook",
		Text:        "High priority context",
		Priority:    1, // High priority
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(1), data["priority"])
}

func TestContextEnqueue_CustomTTL(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "test-hook",
		Text:        "Short-lived context",
		TTLSeconds:  10,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.NotEmpty(t, data["expires_at"])
}

func TestContextEnqueue_MultipleEnqueues(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	// Enqueue first entry
	in1 := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "hook-1",
		Text:        "First context",
	}
	err := run(ctx, rc, in1)
	require.NoError(t, err)

	// Enqueue second entry
	buf.Reset()
	in2 := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "hook-2",
		Text:        "Second context",
	}
	err = run(ctx, rc, in2)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.GreaterOrEqual(t, data["total_pending"], float64(2))
}

func TestContextEnqueue_DedupeSkipsDuplicate(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	// Enqueue first entry
	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "hook-dedup",
		Text:        "Unique context",
		Dedupe:      true,
	}
	err := run(ctx, rc, in)
	require.NoError(t, err)

	// Get first entry ID
	env1 := decodeEnvelope(t, &buf)
	data1 := getData(t, env1)
	firstID := data1["id"]

	// Enqueue same entry again with dedupe=true
	buf.Reset()
	err = run(ctx, rc, in)
	require.NoError(t, err)

	env2 := decodeEnvelope(t, &buf)
	data2 := getData(t, env2)
	secondID := data2["id"]

	// Should return same ID (skipped duplicate)
	assert.Equal(t, firstID, secondID)
}

func TestContextEnqueue_WithAgentID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		AgentID:     "agent-456",
		Source:      "test-hook",
		Text:        "Agent-scoped context",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
}

func TestContextEnqueue_WithMetadata(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Source:      "test-hook",
		Text:        "Context with metadata",
		Metadata:    map[string]any{"key": "value", "count": 42},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)

	// Verify by draining
	ctx := context.Background()
	store, err := contextbuffer.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	result, err := store.Drain(ctx, contextbuffer.DrainParams{
		WorkspaceID:  "workspace-123",
		SessionID:    "session-123",
		Limit:        10,
		MarkConsumed: true,
	})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	assert.Equal(t, "value", result.Entries[0].Metadata["key"])
}
