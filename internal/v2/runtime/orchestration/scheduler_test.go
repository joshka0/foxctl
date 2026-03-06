package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	coreorchestration "github.com/jkatigb/agentctl/internal/v2/core/orchestration"
)

func TestScheduler_Tick_DispatchesAndQueuesRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 5, 13, 0, 0, 0, time.UTC)
	queue := NewRetryQueue(16)
	source := &fakeCandidateSource{
		candidates: []Candidate{
			{IssueID: "issue-1", Role: "coder"},
			{IssueID: "issue-2", Role: "coder"},
		},
	}
	service := &fakeDispatchService{
		errByIssue: map[string]error{
			"issue-1": errors.New("dispatch failed"),
		},
	}
	scheduler := NewScheduler(SchedulerConfig{
		Source:             source,
		Service:            service,
		RetryQueue:         queue,
		MaxDispatchPerTick: 10,
		RetryBaseDelay:     time.Second,
		RetryMaxDelay:      5 * time.Second,
		NewRequestID:       sequentialTestID("req"),
		Now:                func() time.Time { return now },
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(service.calls) != 2 {
		t.Fatalf("dispatch calls=%d want 2", len(service.calls))
	}
	if queue.Len() != 1 {
		t.Fatalf("retry queue len=%d want 1", queue.Len())
	}
	due := queue.PopDue(now.Add(2*time.Second), 10)
	if len(due) != 1 {
		t.Fatalf("due retries=%d want 1", len(due))
	}
	if due[0].Candidate.IssueID != "issue-1" {
		t.Fatalf("retry issue=%q want issue-1", due[0].Candidate.IssueID)
	}
}

func TestScheduler_Tick_ProcessesDueRetryBeforeNewCandidates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 5, 13, 10, 0, 0, time.UTC)
	queue := NewRetryQueue(16)
	ok := queue.Upsert(RetryEntry{
		Candidate: Candidate{IssueID: "retry-issue", Role: "coder"},
		Attempt:   2,
		DueAt:     now.Add(-time.Second),
	})
	if !ok {
		t.Fatal("expected retry upsert to succeed")
	}

	source := &fakeCandidateSource{
		candidates: []Candidate{{IssueID: "fresh-issue", Role: "coder"}},
	}
	service := &fakeDispatchService{}
	scheduler := NewScheduler(SchedulerConfig{
		Source:             source,
		Service:            service,
		RetryQueue:         queue,
		MaxDispatchPerTick: 1,
		NewRequestID:       sequentialTestID("req"),
		Now:                func() time.Time { return now },
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(service.calls) != 1 {
		t.Fatalf("dispatch calls=%d want 1", len(service.calls))
	}
	if service.calls[0].IssueID != "retry-issue" {
		t.Fatalf("first dispatch issue=%q want retry-issue", service.calls[0].IssueID)
	}
}

func TestScheduler_Tick_ReturnsErrorWhenRetryQueueIsFull(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 5, 13, 15, 0, 0, time.UTC)
	queue := NewRetryQueue(1)
	ok := queue.Upsert(RetryEntry{
		Candidate: Candidate{IssueID: "existing", Role: "coder"},
		Attempt:   1,
		DueAt:     now.Add(10 * time.Minute),
	})
	if !ok {
		t.Fatal("expected initial upsert to succeed")
	}

	source := &fakeCandidateSource{
		candidates: []Candidate{{IssueID: "issue-fail", Role: "coder"}},
	}
	service := &fakeDispatchService{
		errByIssue: map[string]error{
			"issue-fail": errors.New("dispatch failed"),
		},
	}
	scheduler := NewScheduler(SchedulerConfig{
		Source:             source,
		Service:            service,
		RetryQueue:         queue,
		MaxDispatchPerTick: 1,
		NewRequestID:       sequentialTestID("req"),
		Now:                func() time.Time { return now },
	})

	err := scheduler.Tick(context.Background())
	if err == nil {
		t.Fatal("expected queue-full error")
	}
	if want := "retry queue full"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%q want contains %q", err.Error(), want)
	}
}

func TestScheduler_Tick_ReturnsRetryErrorWhenSourceNil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 5, 13, 25, 0, 0, time.UTC)
	queue := NewRetryQueue(16)
	// Force a malformed due entry (issue_id empty) to trigger dispatch+enqueue retry error.
	queue.byIssue["malformed"] = RetryEntry{
		Candidate: Candidate{
			IssueID: "",
			Role:    "coder",
		},
		Attempt: 1,
		DueAt:   now.Add(-time.Second),
	}
	service := &fakeDispatchService{}
	scheduler := NewScheduler(SchedulerConfig{
		Service:      service,
		RetryQueue:   queue,
		NewRequestID: sequentialTestID("req"),
		Now:          func() time.Time { return now },
	})

	err := scheduler.Tick(context.Background())
	if err == nil {
		t.Fatal("expected retry-path error with nil source")
	}
	if want := "retry queue full"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%q want contains %q", err.Error(), want)
	}
}

func TestRetryQueue_PopDueSortedAndLimited(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 5, 13, 20, 0, 0, time.UTC)
	queue := NewRetryQueue(16)
	_ = queue.Upsert(RetryEntry{Candidate: Candidate{IssueID: "b"}, Attempt: 1, DueAt: now.Add(-2 * time.Second)})
	_ = queue.Upsert(RetryEntry{Candidate: Candidate{IssueID: "a"}, Attempt: 1, DueAt: now.Add(-2 * time.Second)})
	_ = queue.Upsert(RetryEntry{Candidate: Candidate{IssueID: "c"}, Attempt: 1, DueAt: now.Add(-1 * time.Second)})

	due := queue.PopDue(now, 2)
	if len(due) != 2 {
		t.Fatalf("due len=%d want 2", len(due))
	}
	if due[0].Candidate.IssueID != "a" {
		t.Fatalf("due[0].issue=%q want a", due[0].Candidate.IssueID)
	}
	if due[1].Candidate.IssueID != "b" {
		t.Fatalf("due[1].issue=%q want b", due[1].Candidate.IssueID)
	}
	if queue.Len() != 1 {
		t.Fatalf("queue len=%d want 1", queue.Len())
	}
}

func TestRetryDelay_Capped(t *testing.T) {
	t.Parallel()

	delay := retryDelay(10, time.Second, 5*time.Second)
	if delay != 5*time.Second {
		t.Fatalf("delay=%s want 5s", delay)
	}
}

type fakeCandidateSource struct {
	candidates []Candidate
	err        error
}

func (f *fakeCandidateSource) ListCandidates(context.Context, int) ([]Candidate, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Candidate, len(f.candidates))
	copy(out, f.candidates)
	return out, nil
}

type fakeDispatchService struct {
	errByIssue map[string]error
	calls      []coreorchestration.DispatchRequest
}

func (f *fakeDispatchService) DispatchIssue(_ context.Context, req coreorchestration.DispatchRequest) (coreorchestration.DispatchResponse, error) {
	f.calls = append(f.calls, req)
	if f.errByIssue != nil {
		if err, ok := f.errByIssue[req.IssueID]; ok {
			return coreorchestration.DispatchResponse{}, err
		}
	}
	return coreorchestration.DispatchResponse{Status: "dispatched"}, nil
}

func sequentialTestID(prefix string) func() string {
	var n int
	return func() string {
		n++
		return fmt.Sprintf("%s-%03d", prefix, n)
	}
}
