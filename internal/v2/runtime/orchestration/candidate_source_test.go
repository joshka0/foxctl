package orchestration

import (
	"context"
	"testing"
	"time"

	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
)

func TestBoardCandidateSource_ListCandidates_MapsTodoCards(t *testing.T) {
	t.Parallel()

	source, err := NewBoardCandidateSource(BoardCandidateSourceConfig{
		Reader: &fakeBoardReader{
			responses: map[coreorchestration.Lane]coreorchestration.BoardResponse{
				coreorchestration.LaneTodo: {
					Lanes: []coreorchestration.LaneColumn{
						{
							ID: coreorchestration.LaneTodo,
							Cards: []coreorchestration.Card{
								{
									IssueID:         "issue-1",
									IssueIdentifier: "ABC-1",
									Title:           "Implement scheduler",
									Attempt:         2,
								},
								{
									IssueID:         "issue-2",
									IssueIdentifier: "ABC-2",
									Title:           "Review memory layer",
								},
							},
						},
					},
				},
			},
		},
		WorkspaceID:   "ws-1",
		ParentAgentID: "agent:overseer",
	})
	if err != nil {
		t.Fatalf("NewBoardCandidateSource() error = %v", err)
	}

	got, err := source.ListCandidates(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates=%d want 2", len(got))
	}
	if got[0].WorkspaceID != "ws-1" {
		t.Fatalf("workspace_id=%q want ws-1", got[0].WorkspaceID)
	}
	if got[0].ParentAgentID != "agent:overseer" {
		t.Fatalf("parent_agent_id=%q want agent:overseer", got[0].ParentAgentID)
	}
	if got[0].Role != defaultCandidateRole {
		t.Fatalf("role=%q want %q", got[0].Role, defaultCandidateRole)
	}
	if got[0].ExecMode != defaultCandidateExecMode {
		t.Fatalf("exec_mode=%q want %q", got[0].ExecMode, defaultCandidateExecMode)
	}
	if got[0].Prompt != "Work on issue ABC-1: Implement scheduler" {
		t.Fatalf("prompt=%q unexpected", got[0].Prompt)
	}
	if got[0].Attempt != 2 {
		t.Fatalf("attempt=%d want 2", got[0].Attempt)
	}
}

func TestBoardCandidateSource_ListCandidates_QueriesRetryQueueBeforeTodo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	notDue := now.Add(time.Minute)

	reader := &fakeBoardReader{
		responses: map[coreorchestration.Lane]coreorchestration.BoardResponse{
			coreorchestration.LaneRetryQueue: {
				Lanes: []coreorchestration.LaneColumn{
					{
						ID: coreorchestration.LaneRetryQueue,
						Cards: []coreorchestration.Card{
							{
								IssueID:         "issue-retry-due",
								IssueIdentifier: "ABC-RETRY-1",
								Title:           "Retry ready",
								Attempt:         3,
								RetryDueAt:      &due,
							},
							{
								IssueID:         "issue-retry-later",
								IssueIdentifier: "ABC-RETRY-2",
								Title:           "Retry later",
								Attempt:         4,
								RetryDueAt:      &notDue,
							},
						},
					},
				},
			},
			coreorchestration.LaneTodo: {
				Lanes: []coreorchestration.LaneColumn{
					{
						ID: coreorchestration.LaneTodo,
						Cards: []coreorchestration.Card{
							{
								IssueID:         "issue-todo-1",
								IssueIdentifier: "ABC-TODO-1",
								Title:           "Fresh work",
							},
						},
					},
				},
			},
		},
	}

	source, err := NewBoardCandidateSource(BoardCandidateSourceConfig{
		Reader: reader,
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBoardCandidateSource() error = %v", err)
	}

	got, err := source.ListCandidates(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates=%d want 2", len(got))
	}
	if got[0].IssueID != "issue-retry-due" {
		t.Fatalf("first issue_id=%q want issue-retry-due", got[0].IssueID)
	}
	if got[0].Attempt != 3 {
		t.Fatalf("retry attempt=%d want 3", got[0].Attempt)
	}
	if got[1].IssueID != "issue-todo-1" {
		t.Fatalf("second issue_id=%q want issue-todo-1", got[1].IssueID)
	}
	if len(reader.reqs) != 2 {
		t.Fatalf("board requests=%d want 2", len(reader.reqs))
	}
	if reader.reqs[0].Lane != coreorchestration.LaneRetryQueue {
		t.Fatalf("first lane=%q want %q", reader.reqs[0].Lane, coreorchestration.LaneRetryQueue)
	}
	if reader.reqs[1].Lane != coreorchestration.LaneTodo {
		t.Fatalf("second lane=%q want %q", reader.reqs[1].Lane, coreorchestration.LaneTodo)
	}
}

func TestBoardCandidateSource_ListCandidates_UsesTodoLaneQueryWhenNoRetries(t *testing.T) {
	t.Parallel()

	reader := &fakeBoardReader{
		responses: map[coreorchestration.Lane]coreorchestration.BoardResponse{
			coreorchestration.LaneRetryQueue: {},
			coreorchestration.LaneTodo:       {},
		},
	}
	source, err := NewBoardCandidateSource(BoardCandidateSourceConfig{
		Reader: reader,
	})
	if err != nil {
		t.Fatalf("NewBoardCandidateSource() error = %v", err)
	}

	got, err := source.ListCandidates(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("candidates=%d want 0", len(got))
	}
	if len(reader.reqs) != 2 {
		t.Fatalf("board requests=%d want 2", len(reader.reqs))
	}
	if reader.reqs[0].Lane != coreorchestration.LaneRetryQueue {
		t.Fatalf("first lane=%q want %q", reader.reqs[0].Lane, coreorchestration.LaneRetryQueue)
	}
	if reader.reqs[1].Lane != coreorchestration.LaneTodo {
		t.Fatalf("second lane=%q want %q", reader.reqs[1].Lane, coreorchestration.LaneTodo)
	}
	if reader.reqs[1].Limit != 5 {
		t.Fatalf("todo limit=%d want 5", reader.reqs[1].Limit)
	}
}

type fakeBoardReader struct {
	reqs      []coreorchestration.BoardRequest
	responses map[coreorchestration.Lane]coreorchestration.BoardResponse
	err       error
}

func (f *fakeBoardReader) Board(_ context.Context, req coreorchestration.BoardRequest) (coreorchestration.BoardResponse, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return coreorchestration.BoardResponse{}, f.err
	}
	if resp, ok := f.responses[req.Lane]; ok {
		return resp, nil
	}
	return coreorchestration.BoardResponse{}, nil
}
