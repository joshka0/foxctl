package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
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
		t.Fatalf("unmarshal envelope: %v\nbuffer: %s", err, buf.String())
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

func seedTrajectory(t *testing.T, rc *skillmain.RunContext, workspaceID string, opts *seedOpts) trajectory.Trajectory {
	t.Helper()
	ctx := context.Background()

	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	traj := trajectory.Trajectory{
		WorkspaceID: workspaceID,
		Status:      trajectory.StatusOK,
	}

	if opts != nil {
		if opts.Status != "" {
			traj.Status = opts.Status
		}
		if opts.TaskID != "" {
			traj.TaskIDs = []string{opts.TaskID}
		}
		if opts.EpicID != "" {
			traj.EpicID = opts.EpicID
		}
		if opts.AgentRole != "" {
			traj.AgentRole = opts.AgentRole
		}
		if opts.TraceID != "" {
			traj.TraceID = opts.TraceID
		}
		if opts.RootRequestID != "" {
			traj.RootRequestID = opts.RootRequestID
		}
	}

	inserted, err := store.InsertTrajectory(ctx, traj)
	require.NoError(t, err)
	return inserted
}

func seedUserRequest(t *testing.T, rc *skillmain.RunContext, workspaceID, text string) trajectory.UserRequestCapture {
	t.Helper()
	ctx := context.Background()

	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	ur := trajectory.UserRequestCapture{
		WorkspaceID: workspaceID,
		Actor:       "user",
		Source:      "test",
		TS:          time.Now().UTC(),
		Text:        text,
	}

	inserted, err := store.InsertUserRequest(ctx, ur)
	require.NoError(t, err)
	return inserted
}

func seedEvent(t *testing.T, rc *skillmain.RunContext, trajectoryID string, kind trajectory.EventKind) trajectory.Event {
	t.Helper()
	ctx := context.Background()

	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	event := trajectory.Event{
		TrajectoryID: trajectoryID,
		TS:           time.Now().UTC(),
		Kind:         kind,
		Actor:        "test-agent",
	}

	inserted, err := store.InsertEvent(ctx, event)
	require.NoError(t, err)
	return inserted
}

type seedOpts struct {
	Status        trajectory.Status
	TaskID        string
	EpicID        string
	AgentRole     string
	TraceID       string
	RootRequestID string
}

// Tests for validation

func TestTrajectoryExport_MissingWorkspaceID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace_id required")
}

func TestTrajectoryExport_InvalidStatus(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		WorkspaceID: "workspace-123",
		Status:      "invalid_status",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status")
}

func TestTrajectoryExport_InvalidTimestamp(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		WorkspaceID: "workspace-123",
		Since:       "not-a-timestamp",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timestamp")
}

// Tests for empty store

func TestTrajectoryExport_EmptyStore(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(0), summary["count"])
	assert.NotEmpty(t, data["artifact"])
}

// Tests for basic export

func TestTrajectoryExport_BasicExport(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
	assert.Equal(t, "ndjson", summary["format"])
	assert.NotEmpty(t, data["artifact"])
}

func TestTrajectoryExport_MultipleTrajectories(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed multiple trajectories
	seedTrajectory(t, rc, "workspace-123", nil)
	seedTrajectory(t, rc, "workspace-123", nil)
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(3), summary["count"])
}

// Tests for workspace scoping

func TestTrajectoryExport_WorkspaceScoping(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories in different workspaces
	seedTrajectory(t, rc, "workspace-A", nil)
	seedTrajectory(t, rc, "workspace-A", nil)
	seedTrajectory(t, rc, "workspace-B", nil)

	in := input{
		WorkspaceID: "workspace-A",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["count"])
}

// Tests for filter by status

func TestTrajectoryExport_FilterByStatus(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories with different statuses
	seedTrajectory(t, rc, "workspace-123", &seedOpts{Status: trajectory.StatusOK})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{Status: trajectory.StatusOK})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{Status: trajectory.StatusError})

	in := input{
		WorkspaceID: "workspace-123",
		Status:      "ok",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["count"])
}

// Tests for filter by task_id

func TestTrajectoryExport_FilterByTaskID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories with different task IDs
	seedTrajectory(t, rc, "workspace-123", &seedOpts{TaskID: "task-A"})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{TaskID: "task-B"})
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
		TaskID:      "task-A",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
}

// Tests for filter by epic_id

func TestTrajectoryExport_FilterByEpicID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories with different epic IDs
	seedTrajectory(t, rc, "workspace-123", &seedOpts{EpicID: "epic-123"})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{EpicID: "epic-456"})
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
		EpicID:      "epic-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
}

// Tests for filter by agent_role

func TestTrajectoryExport_FilterByAgentRole(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories with different agent roles
	seedTrajectory(t, rc, "workspace-123", &seedOpts{AgentRole: "planner"})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{AgentRole: "implementer"})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{AgentRole: "planner"})

	in := input{
		WorkspaceID: "workspace-123",
		AgentRole:   "planner",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["count"])
}

// Tests for filter by trace_id

func TestTrajectoryExport_FilterByTraceID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories with different trace IDs
	seedTrajectory(t, rc, "workspace-123", &seedOpts{TraceID: "trace-ABC"})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{TraceID: "trace-XYZ"})
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
		TraceID:     "trace-ABC",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
}

// Tests for limit

func TestTrajectoryExport_Limit(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed multiple trajectories
	for i := 0; i < 5; i++ {
		seedTrajectory(t, rc, "workspace-123", nil)
	}

	in := input{
		WorkspaceID: "workspace-123",
		Limit:       2,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["count"])
}

// Tests for dry_run

func TestTrajectoryExport_DryRun(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories
	seedTrajectory(t, rc, "workspace-123", nil)
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
		DryRun:      true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.True(t, data["dry_run"].(bool))
	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["count"])
	assert.Greater(t, summary["estimated_bytes"].(float64), float64(0))
	// Dry run should not produce artifact
	_, hasArtifact := data["artifact"]
	assert.False(t, hasArtifact)
}

// Tests for trajectory with events

func TestTrajectoryExport_WithEvents(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory with events
	traj := seedTrajectory(t, rc, "workspace-123", nil)
	seedEvent(t, rc, traj.ID, trajectory.EventKindToolCall)
	seedEvent(t, rc, traj.ID, trajectory.EventKindToolCall)
	seedEvent(t, rc, traj.ID, trajectory.EventKindToolResult)

	in := input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])

	// Verify the artifact was created (contains metrics about events)
	artifact := data["artifact"].(string)
	assert.True(t, strings.HasPrefix(artifact, "sha256:"))
}

// Tests for trajectory with user request

func TestTrajectoryExport_WithUserRequest(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed user request first
	ur := seedUserRequest(t, rc, "workspace-123", "Please implement feature X")

	// Seed trajectory with root request ID
	seedTrajectory(t, rc, "workspace-123", &seedOpts{RootRequestID: ur.ID})

	in := input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
}

// Tests for time filtering

func TestTrajectoryExport_FilterByTime(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory
	seedTrajectory(t, rc, "workspace-123", nil)

	// Use a time range that includes now
	now := time.Now().UTC()
	since := now.Add(-1 * time.Hour).Format(time.RFC3339Nano)
	until := now.Add(1 * time.Hour).Format(time.RFC3339Nano)

	in := input{
		WorkspaceID: "workspace-123",
		Since:       since,
		Until:       until,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
}

func TestTrajectoryExport_FilterByTime_ExcludesOld(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory
	seedTrajectory(t, rc, "workspace-123", nil)

	// Use a time range in the future (should exclude all)
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)

	in := input{
		WorkspaceID: "workspace-123",
		Since:       future,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(0), summary["count"])
}

// Tests for include_raw_traces

func TestTrajectoryExport_IncludeRawTraces(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID:      "workspace-123",
		IncludeRawTraces: true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["count"])
}

// Tests for default limit

func TestTrajectoryExport_DefaultLimit(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed a few trajectories (less than default limit of 100)
	for i := 0; i < 3; i++ {
		seedTrajectory(t, rc, "workspace-123", nil)
	}

	in := input{
		WorkspaceID: "workspace-123",
		// No limit specified - should use default of 100
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(3), summary["count"])
}

// Tests for combined filters

func TestTrajectoryExport_CombinedFilters(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectories with various combinations
	seedTrajectory(t, rc, "workspace-123", &seedOpts{
		Status:    trajectory.StatusOK,
		AgentRole: "planner",
		EpicID:    "epic-1",
	})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{
		Status:    trajectory.StatusOK,
		AgentRole: "implementer",
		EpicID:    "epic-1",
	})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{
		Status:    trajectory.StatusError,
		AgentRole: "planner",
		EpicID:    "epic-1",
	})
	seedTrajectory(t, rc, "workspace-123", &seedOpts{
		Status:    trajectory.StatusOK,
		AgentRole: "planner",
		EpicID:    "epic-2",
	})

	in := input{
		WorkspaceID: "workspace-123",
		Status:      "ok",
		AgentRole:   "planner",
		EpicID:      "epic-1",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(map[string]any)
	// Only the first trajectory matches all filters
	assert.Equal(t, float64(1), summary["count"])
}

// Tests for artifact output

func TestTrajectoryExport_ArtifactFormat(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Verify artifact is a SHA256 digest
	artifact := data["artifact"].(string)
	assert.True(t, strings.HasPrefix(artifact, "sha256:"))
	assert.Len(t, artifact, 7+64) // "sha256:" + 64 hex chars
}

// Tests for pin

func TestTrajectoryExport_Pin(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Seed trajectory
	seedTrajectory(t, rc, "workspace-123", nil)

	in := input{
		WorkspaceID: "workspace-123",
		Pin:         true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Should still produce artifact
	assert.NotEmpty(t, data["artifact"])
}
