package trajectorycapture

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

func TestStartAndCaptureResult_TodoReviewRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	input := map[string]any{
		"operation": "review_request",
		"review_request": map[string]any{
			"task_id": "task-123",
			"kind":    "auto",
		},
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	capture, err := Start(ctx, StartOptions{
		StorageRoot:     root,
		WorkspaceID:     "ws-1",
		Actor:           "actor:human:cli",
		Source:          trajectory.SourceCLI,
		CLICommand:      "agentctl run todo/manage",
		ProtocolCommand: "todo/manage",
		JobID:           "job-1",
		CorrelationID:   "corr-1",
		Input:           inputBytes,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { capture.Close() })

	data := map[string]any{
		"task": map[string]any{
			"id":                 "task-123",
			"last_review_id":     "review-1",
			"last_review_status": "pending",
		},
		"summary": "review requested",
	}
	envObj := envelope.OK("todo/manage", data)
	envBytes, err := json.Marshal(envObj)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	if err := capture.CaptureResult(ctx, envBytes, "job-1", "corr-1"); err != nil {
		t.Fatalf("capture result: %v", err)
	}

	gotTraj, err := capture.store.GetTrajectory(ctx, "ws-1", capture.traj.ID)
	if err != nil {
		t.Fatalf("get trajectory: %v", err)
	}
	if gotTraj.Status != trajectory.StatusOK {
		t.Fatalf("expected status ok got %q", gotTraj.Status)
	}
	if gotTraj.TraceID != "corr-1" {
		t.Fatalf("expected trace_id corr-1 got %q", gotTraj.TraceID)
	}
	if gotTraj.JobID != "job-1" {
		t.Fatalf("expected job_id job-1 got %q", gotTraj.JobID)
	}
	if len(gotTraj.TaskIDs) != 1 || gotTraj.TaskIDs[0] != "task-123" {
		t.Fatalf("expected task_ids [task-123] got %+v", gotTraj.TaskIDs)
	}

	events, err := capture.store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: capture.traj.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events got %d", len(events))
	}
	if events[0].Kind != trajectory.EventKindUserRequest {
		t.Fatalf("expected first event kind user_request got %q", events[0].Kind)
	}
	if events[1].Kind != trajectory.EventKindReviewRequest {
		t.Fatalf("expected second event kind review_request got %q", events[1].Kind)
	}
	if events[1].Meta == nil || events[1].Meta.ReviewID != "review-1" {
		t.Fatalf("expected review_id review-1 got %+v", events[1].Meta)
	}
}

func TestStart_RedactsSecrets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	capture, err := Start(ctx, StartOptions{
		StorageRoot:     root,
		WorkspaceID:     "ws-1",
		Actor:           "actor:human:cli",
		Source:          trajectory.SourceCLI,
		CLICommand:      "agentctl run http/openapi --header Authorization: Bearer secret",
		ProtocolCommand: "http/openapi",
		CorrelationID:   "corr-1",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { capture.Close() })

	ur, err := capture.store.GetUserRequest(ctx, "ws-1", capture.request.ID)
	if err != nil {
		t.Fatalf("get user request: %v", err)
	}
	if ur.Text == "" {
		t.Fatalf("expected user request text to be set")
	}
	if ur.Text == "agentctl run http/openapi --header Authorization: Bearer secret" {
		t.Fatalf("expected redaction to modify stored text")
	}
}
