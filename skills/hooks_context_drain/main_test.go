package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

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

func seedContext(t *testing.T, rc *skillmain.RunContext, workspaceID, sessionID, source, text string) {
	t.Helper()
	ctx := context.Background()
	store, err := contextbuffer.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	_, err = store.Enqueue(ctx, contextbuffer.EnqueueParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Source:      source,
		Text:        text,
		Priority:    2,
		TTL:         60 * time.Second,
	})
	require.NoError(t, err)
}

func seedContextWithPriority(t *testing.T, rc *skillmain.RunContext, workspaceID, sessionID, source, text string, priority int) {
	t.Helper()
	ctx := context.Background()
	store, err := contextbuffer.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	_, err = store.Enqueue(ctx, contextbuffer.EnqueueParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Source:      source,
		Text:        text,
		Priority:    priority,
		TTL:         60 * time.Second,
	})
	require.NoError(t, err)
}

// Tests for validation

func TestContextDrain_MissingWorkspaceID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		SessionID: "session-123",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace_id is required")
}

func TestContextDrain_MissingSessionID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id is required")
}

// Tests for empty buffer

func TestContextDrain_EmptyBuffer(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(0), data["count"])
	assert.Equal(t, "", data["markdown"])
}

// Tests for basic drain

func TestContextDrain_BasicDrain(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed some context
	seedContext(t, rc, "workspace-123", "session-123", "hook-1", "Context from hook 1")
	seedContext(t, rc, "workspace-123", "session-123", "hook-2", "Context from hook 2")

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(2), data["count"])
	assert.Contains(t, data["markdown"], "Context from hook 1")
	assert.Contains(t, data["markdown"], "Context from hook 2")

	// Verify sources map
	sources, ok := data["sources"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), sources["hook-1"])
	assert.Equal(t, float64(1), sources["hook-2"])
}

// Tests for peek (don't consume)

func TestContextDrain_PeekDoesNotConsume(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed context
	seedContext(t, rc, "workspace-123", "session-123", "hook-1", "Peeked context")

	// Peek first
	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Peek:        true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(1), data["count"])

	// Drain again - should still get the entry
	buf.Reset()
	in.Peek = false
	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env2 := decodeEnvelope(t, &buf)
	data2 := getData(t, env2)
	assert.Equal(t, float64(1), data2["count"])
}

// Tests for filter by source

func TestContextDrain_FilterBySource(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed from different sources
	seedContext(t, rc, "workspace-123", "session-123", "hook-a", "Context A")
	seedContext(t, rc, "workspace-123", "session-123", "hook-b", "Context B")
	seedContext(t, rc, "workspace-123", "session-123", "hook-c", "Context C")

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Sources:     []string{"hook-a", "hook-c"},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(2), data["count"])
	assert.Contains(t, data["markdown"], "Context A")
	assert.Contains(t, data["markdown"], "Context C")
	assert.NotContains(t, data["markdown"], "Context B")
}

// Tests for filter by min priority

func TestContextDrain_FilterByMinPriority(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed with different priorities
	seedContextWithPriority(t, rc, "workspace-123", "session-123", "high", "High priority", 1)
	seedContextWithPriority(t, rc, "workspace-123", "session-123", "normal", "Normal priority", 2)
	seedContextWithPriority(t, rc, "workspace-123", "session-123", "low", "Low priority", 3)

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		MinPriority: 1, // Only high priority
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(1), data["count"])
	assert.Contains(t, data["markdown"], "High priority")
}

// Tests for limit

func TestContextDrain_Limit(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed multiple entries
	for i := 0; i < 5; i++ {
		seedContext(t, rc, "workspace-123", "session-123", "hook", "Context entry")
	}

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Limit:       2,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(2), data["count"])
}

// Tests for JSON format

func TestContextDrain_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed context
	seedContext(t, rc, "workspace-123", "session-123", "hook-1", "JSON format context")

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Format:      "json",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Should have entries array instead of markdown
	entries, ok := data["entries"].([]any)
	require.True(t, ok, "entries should be array")
	assert.Len(t, entries, 1)
}

// Tests for prune

func TestContextDrain_Prune(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-123",
		Prune:       true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Pruned should be in output
	_, hasPruned := data["pruned"]
	assert.True(t, hasPruned)
}

// Tests for workspace scoping

func TestContextDrain_WorkspaceScoping(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed context in different workspaces
	seedContext(t, rc, "workspace-A", "session-123", "hook", "Context in A")
	seedContext(t, rc, "workspace-B", "session-123", "hook", "Context in B")

	// Drain only from workspace-A
	in := Input{
		WorkspaceID: "workspace-A",
		SessionID:   "session-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(1), data["count"])
	assert.Contains(t, data["markdown"], "Context in A")
	assert.NotContains(t, data["markdown"], "Context in B")
}

// Tests for session scoping

func TestContextDrain_SessionScoping(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed context in different sessions
	seedContext(t, rc, "workspace-123", "session-A", "hook", "Context in session A")
	seedContext(t, rc, "workspace-123", "session-B", "hook", "Context in session B")

	// Drain only from session-A
	in := Input{
		WorkspaceID: "workspace-123",
		SessionID:   "session-A",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(1), data["count"])
	assert.Contains(t, data["markdown"], "Context in session A")
	assert.NotContains(t, data["markdown"], "Context in session B")
}
