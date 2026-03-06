package services_test

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
	"github.com/jkatigb/agentctl/internal/v2/services"
)

func TestOrchestrationService_DispatchIssue_UsesSpawnAndIdempotency(t *testing.T) {
	t.Parallel()

	spawner := &fakeOrchestrationSpawner{
		resp: spawn.Response{
			RunID:   "run-001",
			TurnID:  "turn-001",
			AgentID: "agent-001",
			ActorID: "actor:coder:001",
		},
	}
	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		Spawn: spawner,
		Now: func() time.Time {
			return time.Date(2026, time.March, 5, 12, 0, 0, 0, time.UTC)
		},
	})

	first, err := svc.DispatchIssue(context.Background(), orchestration.DispatchRequest{
		RequestID:       "req-1",
		WorkspaceID:     "ws-1",
		IssueID:         "issue-1",
		IssueIdentifier: "ABC-1",
		Title:           "Implement feature",
		Role:            "coder",
		Prompt:          "implement feature",
		ParentAgentID:   "agent:parent-1",
	})
	if err != nil {
		t.Fatalf("DispatchIssue() error = %v", err)
	}
	if first.Status != "dispatched" {
		t.Fatalf("status=%q want dispatched", first.Status)
	}
	if first.RunID != "run-001" {
		t.Fatalf("run_id=%q want run-001", first.RunID)
	}
	if first.Idempotent {
		t.Fatal("first response should not be idempotent")
	}

	second, err := svc.DispatchIssue(context.Background(), orchestration.DispatchRequest{
		RequestID:       "req-1",
		WorkspaceID:     "ws-1",
		IssueID:         "issue-1",
		IssueIdentifier: "ABC-1",
		Role:            "coder",
		Prompt:          "implement feature",
	})
	if err != nil {
		t.Fatalf("DispatchIssue(duplicate) error = %v", err)
	}
	if !second.Idempotent {
		t.Fatal("duplicate response should be idempotent")
	}
	if second.RunID != "run-001" {
		t.Fatalf("duplicate run_id=%q want run-001", second.RunID)
	}
	if spawner.calls != 1 {
		t.Fatalf("spawn calls=%d want 1", spawner.calls)
	}
	if spawner.last.RequestID != "req-1" {
		t.Fatalf("spawn request_id=%q want req-1", spawner.last.RequestID)
	}
	if spawner.last.ParentAgentID != "agent:parent-1" {
		t.Fatalf("parent_agent_id=%q want agent:parent-1", spawner.last.ParentAgentID)
	}
	if got := spawner.last.Metadata["workspace_id"]; got != "ws-1" {
		t.Fatalf("metadata.workspace_id=%v want ws-1", got)
	}
	if got := spawner.last.Metadata["issue_id"]; got != "issue-1" {
		t.Fatalf("metadata.issue_id=%v want issue-1", got)
	}
	if got := spawner.last.Metadata["issue_identifier"]; got != "ABC-1" {
		t.Fatalf("metadata.issue_identifier=%v want ABC-1", got)
	}
	if got := spawner.last.Metadata["title"]; got != "Implement feature" {
		t.Fatalf("metadata.title=%v want Implement feature", got)
	}
}

func TestOrchestrationService_DispatchIssue_PolicyDeniedReturnsBlocked(t *testing.T) {
	t.Parallel()

	spawner := &fakeOrchestrationSpawner{
		err: &v2errors.V2Error{
			Kind:    v2errors.ErrPolicyViolation,
			Message: "policy denied spawn",
			Fatal:   true,
			Details: map[string]any{
				"suggestion": "request overseer approval",
			},
		},
	}
	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		Spawn: spawner,
		Now: func() time.Time {
			return time.Date(2026, time.March, 5, 12, 5, 0, 0, time.UTC)
		},
	})

	resp, err := svc.DispatchIssue(context.Background(), orchestration.DispatchRequest{
		RequestID: "req-2",
		IssueID:   "issue-2",
		Role:      "coder",
	})
	if err != nil {
		t.Fatalf("DispatchIssue() error = %v", err)
	}
	if resp.Status != "blocked" {
		t.Fatalf("status=%q want blocked", resp.Status)
	}
	if resp.PolicyStatus != orchestration.PolicyStatusDenied {
		t.Fatalf("policy_status=%q want %q", resp.PolicyStatus, orchestration.PolicyStatusDenied)
	}
	if resp.LastOutcome != orchestration.OutcomePolicyDenied {
		t.Fatalf("last_outcome=%q want %q", resp.LastOutcome, orchestration.OutcomePolicyDenied)
	}
	if resp.DenialReason == "" {
		t.Fatal("denial_reason is empty")
	}
	if resp.Suggestion != "request overseer approval" {
		t.Fatalf("suggestion=%q want request overseer approval", resp.Suggestion)
	}
}

func TestOrchestrationService_Refresh_RequiresRequestID(t *testing.T) {
	t.Parallel()

	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		RefreshQueue: &fakeOrchestrationRefreshQueue{},
	})

	_, err := svc.Refresh(context.Background(), orchestration.RefreshRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrValidation {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrValidation)
	}
}

func TestOrchestrationService_Refresh_IdempotentByWorkspaceAndRequest(t *testing.T) {
	t.Parallel()

	queue := &fakeOrchestrationRefreshQueue{
		queued:    true,
		coalesced: false,
	}
	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		RefreshQueue: queue,
		Now: func() time.Time {
			return time.Date(2026, time.March, 5, 12, 10, 0, 0, time.UTC)
		},
	})

	first, err := svc.Refresh(context.Background(), orchestration.RefreshRequest{
		RequestID:   "req-3",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if first.Idempotent {
		t.Fatal("first response should not be idempotent")
	}

	second, err := svc.Refresh(context.Background(), orchestration.RefreshRequest{
		RequestID:   "req-3",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("Refresh(duplicate) error = %v", err)
	}
	if !second.Idempotent {
		t.Fatal("duplicate response should be idempotent")
	}
	if queue.calls != 1 {
		t.Fatalf("queue calls=%d want 1", queue.calls)
	}
}

func TestOrchestrationService_Board_NormalizesLimitAndCounts(t *testing.T) {
	t.Parallel()

	reader := &fakeOrchestrationReader{
		boardResp: orchestration.BoardResponse{
			Counts: map[orchestration.Lane]int{
				orchestration.LaneRunning: 2,
			},
		},
	}
	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		Reader: reader,
		Now: func() time.Time {
			return time.Date(2026, time.March, 5, 12, 15, 0, 0, time.UTC)
		},
	})

	resp, err := svc.Board(context.Background(), orchestration.BoardRequest{
		Limit: 999,
	})
	if err != nil {
		t.Fatalf("Board() error = %v", err)
	}
	if reader.lastBoardReq.Limit != 200 {
		t.Fatalf("normalized limit=%d want 200", reader.lastBoardReq.Limit)
	}
	if resp.GeneratedAt.IsZero() {
		t.Fatal("generated_at should be set")
	}
	for _, lane := range orchestration.LaneOrder() {
		if _, ok := resp.Counts[lane]; !ok {
			t.Fatalf("missing count for lane %q", lane)
		}
	}
}

func TestOrchestrationService_Card_DerivesLane(t *testing.T) {
	t.Parallel()

	reader := &fakeOrchestrationReader{
		cardResp: orchestration.CardResponse{
			Card: orchestration.Card{
				IssueID:      "issue-1",
				State:        orchestration.StateRunning,
				PolicyStatus: orchestration.PolicyStatusOK,
				Eligibility:  orchestration.EligibilityEligible,
			},
		},
	}
	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		Reader: reader,
	})

	resp, err := svc.Card(context.Background(), orchestration.CardRequest{
		IssueID: "issue-1",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if resp.Card.Lane != orchestration.LaneRunning {
		t.Fatalf("lane=%q want %q", resp.Card.Lane, orchestration.LaneRunning)
	}
}

func TestOrchestrationService_Card_DerivesLaneWithConfiguredTrackerStates(t *testing.T) {
	t.Parallel()

	reader := &fakeOrchestrationReader{
		cardResp: orchestration.CardResponse{
			Card: orchestration.Card{
				IssueID:      "issue-1",
				State:        orchestration.StateRunning,
				TrackerState: "Done",
			},
		},
	}
	svc := services.NewOrchestrationService(services.OrchestrationDependencies{
		Reader: reader,
		LaneOptions: orchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
		},
	})

	resp, err := svc.Card(context.Background(), orchestration.CardRequest{
		IssueID: "issue-1",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if resp.Card.Lane != orchestration.LaneDone {
		t.Fatalf("lane=%q want %q", resp.Card.Lane, orchestration.LaneDone)
	}
}

type fakeOrchestrationSpawner struct {
	resp  spawn.Response
	err   error
	calls int
	last  spawn.Request
}

func (f *fakeOrchestrationSpawner) Spawn(_ context.Context, req spawn.Request) (spawn.Response, error) {
	f.calls++
	f.last = req
	return f.resp, f.err
}

type fakeOrchestrationReader struct {
	boardResp    orchestration.BoardResponse
	boardErr     error
	lastBoardReq orchestration.BoardRequest
	cardResp     orchestration.CardResponse
	cardErr      error
	lastCardReq  orchestration.CardRequest
}

func (f *fakeOrchestrationReader) Board(_ context.Context, req orchestration.BoardRequest) (orchestration.BoardResponse, error) {
	f.lastBoardReq = req
	return f.boardResp, f.boardErr
}

func (f *fakeOrchestrationReader) Card(_ context.Context, req orchestration.CardRequest) (orchestration.CardResponse, error) {
	f.lastCardReq = req
	return f.cardResp, f.cardErr
}

type fakeOrchestrationRefreshQueue struct {
	queued    bool
	coalesced bool
	err       error
	calls     int
}

func (f *fakeOrchestrationRefreshQueue) Enqueue(_ context.Context, _, _ string) (bool, bool, error) {
	f.calls++
	return f.queued, f.coalesced, f.err
}
