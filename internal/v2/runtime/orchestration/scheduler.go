package orchestration

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	coreorchestration "github.com/jkatigb/agentctl/internal/v2/core/orchestration"
)

const (
	defaultMaxDispatchPerTick = 10
	defaultRetryBaseDelay     = 10 * time.Second
	defaultRetryMaxDelay      = 5 * time.Minute
	defaultRequestPrefix      = "req-orch"
)

// DispatchService is the scheduler-facing orchestration command surface.
type DispatchService interface {
	DispatchIssue(ctx context.Context, req coreorchestration.DispatchRequest) (coreorchestration.DispatchResponse, error)
}

// CandidateSource provides dispatch-eligible issue candidates for each tick.
type CandidateSource interface {
	ListCandidates(ctx context.Context, limit int) ([]Candidate, error)
}

// Candidate is one dispatch-eligible issue work item.
type Candidate struct {
	WorkspaceID     string
	IssueID         string
	IssueIdentifier string
	Title           string
	ParentAgentID   string
	Role            string
	Prompt          string
	ExecMode        string
	ThinkInterval   int
	MaxIterations   int
	Attempt         int
}

// SchedulerConfig wires dispatch/retry behavior.
type SchedulerConfig struct {
	Source             CandidateSource
	Service            DispatchService
	RetryQueue         *RetryQueue
	MaxDispatchPerTick int
	RetryBaseDelay     time.Duration
	RetryMaxDelay      time.Duration
	NewRequestID       func() string
	Now                func() time.Time
}

// Scheduler performs one dispatch tick and manages retry enqueue/dequeue behavior.
type Scheduler struct {
	source     CandidateSource
	service    DispatchService
	retryQueue *RetryQueue

	maxDispatchPerTick int
	retryBaseDelay     time.Duration
	retryMaxDelay      time.Duration
	newRequestID       func() string
	now                func() time.Time
}

// NewScheduler builds a scheduler with deterministic defaults.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	maxDispatchPerTick := cfg.MaxDispatchPerTick
	if maxDispatchPerTick <= 0 {
		maxDispatchPerTick = defaultMaxDispatchPerTick
	}
	retryBaseDelay := cfg.RetryBaseDelay
	if retryBaseDelay <= 0 {
		retryBaseDelay = defaultRetryBaseDelay
	}
	retryMaxDelay := cfg.RetryMaxDelay
	if retryMaxDelay <= 0 {
		retryMaxDelay = defaultRetryMaxDelay
	}
	newRequestID := cfg.NewRequestID
	if newRequestID == nil {
		newRequestID = defaultSchedulerID()
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	return &Scheduler{
		source:             cfg.Source,
		service:            cfg.Service,
		retryQueue:         cfg.RetryQueue,
		maxDispatchPerTick: maxDispatchPerTick,
		retryBaseDelay:     retryBaseDelay,
		retryMaxDelay:      retryMaxDelay,
		newRequestID:       newRequestID,
		now:                now,
	}
}

// Tick runs one deterministic schedule pass: due retries first, then fresh candidates.
func (s *Scheduler) Tick(ctx context.Context) error {
	if s == nil || s.service == nil {
		return fmt.Errorf("orchestration scheduler: missing dispatch service")
	}

	remaining := s.maxDispatchPerTick
	if remaining <= 0 {
		return nil
	}

	var firstErr error

	if s.retryQueue != nil {
		due := s.retryQueue.PopDue(s.now().UTC(), remaining)
		for _, entry := range due {
			if err := s.dispatch(ctx, entry.Candidate); err != nil {
				nextAttempt := entry.Attempt + 1
				if retryErr := s.enqueueRetry(entry.Candidate, nextAttempt, err.Error()); retryErr != nil && firstErr == nil {
					firstErr = retryErr
				}
			}
			remaining--
			if remaining == 0 {
				return firstErr
			}
		}
	}

	if s.source == nil {
		return firstErr
	}
	candidates, err := s.source.ListCandidates(ctx, remaining)
	if err != nil {
		return err
	}

	for _, candidate := range candidates {
		if remaining == 0 {
			break
		}
		if err := s.dispatch(ctx, candidate); err != nil {
			attempt := candidate.Attempt
			if attempt < 1 {
				attempt = 1
			}
			if retryErr := s.enqueueRetry(candidate, attempt+1, err.Error()); retryErr != nil && firstErr == nil {
				firstErr = retryErr
			}
		}
		remaining--
	}

	return firstErr
}

func (s *Scheduler) dispatch(ctx context.Context, candidate Candidate) error {
	if strings.TrimSpace(candidate.IssueID) == "" {
		return fmt.Errorf("orchestration scheduler: candidate issue_id is required")
	}
	requestID := fmt.Sprintf("%s-%s", defaultRequestPrefix, strings.TrimSpace(s.newRequestID()))
	_, err := s.service.DispatchIssue(ctx, coreorchestration.DispatchRequest{
		RequestID:       requestID,
		WorkspaceID:     strings.TrimSpace(candidate.WorkspaceID),
		IssueID:         strings.TrimSpace(candidate.IssueID),
		IssueIdentifier: strings.TrimSpace(candidate.IssueIdentifier),
		Title:           strings.TrimSpace(candidate.Title),
		ParentAgentID:   strings.TrimSpace(candidate.ParentAgentID),
		Role:            strings.TrimSpace(candidate.Role),
		Prompt:          strings.TrimSpace(candidate.Prompt),
		ExecMode:        strings.TrimSpace(candidate.ExecMode),
		ThinkInterval:   candidate.ThinkInterval,
		MaxIterations:   candidate.MaxIterations,
		Attempt:         candidate.Attempt,
	})
	return err
}

func (s *Scheduler) enqueueRetry(candidate Candidate, attempt int, reason string) error {
	if s == nil || s.retryQueue == nil {
		return nil
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := retryDelay(attempt, s.retryBaseDelay, s.retryMaxDelay)
	ok := s.retryQueue.Upsert(RetryEntry{
		Candidate: candidate,
		Attempt:   attempt,
		DueAt:     s.now().UTC().Add(delay),
		LastError: strings.TrimSpace(reason),
	})
	if !ok {
		return fmt.Errorf("orchestration scheduler: retry queue full issue_id=%s", strings.TrimSpace(candidate.IssueID))
	}
	return nil
}

func retryDelay(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = defaultRetryBaseDelay
	}
	if max <= 0 {
		max = defaultRetryMaxDelay
	}
	// min(base * 2^(attempt-1), max)
	power := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(base) * power)
	if delay > max {
		return max
	}
	return delay
}

func defaultSchedulerID() func() string {
	var seq atomic.Uint64
	return func() string {
		n := seq.Add(1)
		return fmt.Sprintf("id-%06d", n)
	}
}
