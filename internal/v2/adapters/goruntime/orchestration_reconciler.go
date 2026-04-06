package goruntime

import (
	"context"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
	v2orchestration "github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
)

const (
	commandDispatchIssue            = "orchestration/dispatch-issue"
	defaultOrchestrationEventIDPref = "evt"
)

type EventAppender = v2jido.EventAppender
type ProjectionApplier = v2jido.ProjectionApplier
type OrchestrationCardReader = v2jido.OrchestrationCardReader

// OrchestrationReconciler converts subprocess spawn and worker outcomes into canonical orchestration events.
type OrchestrationReconciler struct {
	events              EventAppender
	projections         ProjectionApplier
	reader              OrchestrationCardReader
	workers             coreworker.StateReader
	parentIDs           []string
	successTrackerState string
	retryPolicy         v2jido.RetryPolicy
	now                 func() time.Time
	newID               func() string
}

// OrchestrationReconcilerConfig wires the subprocess orchestration reconciler.
type OrchestrationReconcilerConfig struct {
	Events              EventAppender
	Projections         ProjectionApplier
	Reader              OrchestrationCardReader
	Workers             coreworker.StateReader
	ParentAgentIDs      []string
	SuccessTrackerState string
	RetryPolicy         v2jido.RetryPolicy
	Now                 func() time.Time
	NewID               func() string
}

// NewOrchestrationReconciler builds a subprocess-backed orchestration reconciler.
func NewOrchestrationReconciler(cfg OrchestrationReconcilerConfig) (*OrchestrationReconciler, error) {
	if cfg.Events == nil {
		return nil, fmt.Errorf("event appender is required")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		seq := 0
		cfg.NewID = func() string {
			seq++
			return fmt.Sprintf("%s-%06d", defaultOrchestrationEventIDPref, seq)
		}
	}
	retryPolicy := cfg.RetryPolicy
	if len(retryPolicy.Classes) == 0 {
		retryPolicy = v2jido.DefaultRetryPolicy()
	}
	return &OrchestrationReconciler{
		events:              cfg.Events,
		projections:         cfg.Projections,
		reader:              cfg.Reader,
		workers:             cfg.Workers,
		parentIDs:           normalizeParentAgentIDs(cfg.ParentAgentIDs),
		successTrackerState: strings.TrimSpace(cfg.SuccessTrackerState),
		retryPolicy:         retryPolicy,
		now:                 cfg.Now,
		newID:               cfg.NewID,
	}, nil
}

// SpawnResultCallback converts child-spawn outcomes into orchestration events.
func (r *OrchestrationReconciler) SpawnResultCallback() func(ctx context.Context, req spawn.Request, resp spawn.Response, err error) error {
	if r == nil {
		return nil
	}
	return func(ctx context.Context, req spawn.Request, resp spawn.Response, err error) error {
		if err != nil {
			_, reconcileErr := r.RecordDispatchFailed(ctx, req, err)
			return reconcileErr
		}
		_, reconcileErr := r.RecordDispatchSpawned(ctx, req, resp)
		return reconcileErr
	}
}

// Reconcile polls subprocess-backed worker state and projects terminal outcomes.
func (r *OrchestrationReconciler) Reconcile(ctx context.Context) error {
	if r == nil || len(r.parentIDs) == 0 {
		return nil
	}
	if r.reader == nil {
		return fmt.Errorf("orchestration card reader is required for subprocess reconcile")
	}
	if r.workers == nil {
		return fmt.Errorf("worker state reader is required for subprocess reconcile")
	}

	for _, parentID := range r.parentIDs {
		children, err := r.workers.Children(ctx, coreworker.ChildrenRequest{ParentAgentID: parentID})
		if err != nil {
			return fmt.Errorf("list subprocess children for %s: %w", parentID, err)
		}
		for _, child := range sortedWorkerRecords(children) {
			issueID := dispatchMetaString(child.Metadata, "issue_id")
			if issueID == "" {
				continue
			}
			workspaceID := dispatchMetaString(child.Metadata, "workspace_id")
			card, err := r.reader.Card(ctx, v2orchestration.CardRequest{WorkspaceID: workspaceID, IssueID: issueID})
			if err != nil {
				return fmt.Errorf("read orchestration card issue_id=%s: %w", issueID, err)
			}
			if card.Card.State != v2orchestration.StateRunning {
				continue
			}
			if !sameRun(card.Card.RunID, dispatchMetaString(child.Metadata, "run_id"), child.RunID) {
				continue
			}
			if card.Card.AgentID != "" && strings.TrimSpace(child.AgentID) != "" && card.Card.AgentID != strings.TrimSpace(child.AgentID) {
				continue
			}
			if !coreworker.IsTerminal(child.Status) {
				continue
			}

			switch child.Status {
			case coreworker.StatusCompleted:
				if _, err := r.RecordDispatchCompleted(ctx, child); err != nil {
					return err
				}
			case coreworker.StatusFailed, coreworker.StatusCancelled:
				if _, err := r.RecordDispatchRuntimeFailed(ctx, child); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RecordDispatchSpawned appends a run.started orchestration event for one accepted child dispatch.
func (r *OrchestrationReconciler) RecordDispatchSpawned(ctx context.Context, req spawn.Request, resp spawn.Response) (v2events.Event, error) {
	issue, ok := dispatchIssuePayload(req, resp)
	if !ok {
		return v2events.Event{}, nil
	}

	now := r.now().UTC()
	issue["state"] = string(v2orchestration.StateRunning)
	issue["eligibility"] = string(v2orchestration.EligibilityEligible)
	issue["policy_status"] = string(v2orchestration.PolicyStatusOK)
	issue["run_id"] = chooseNonEmpty(resp.RunID, req.RunID)
	issue["agent_id"] = chooseNonEmpty(resp.AgentID, req.AgentID)
	issue["actor_id"] = chooseNonEmpty(resp.ActorID, req.ActorID)

	evt := v2events.Event{
		ID:            prefixedID(defaultOrchestrationEventIDPref, r.newID),
		StreamID:      chooseNonEmpty(resp.RunID, req.RunID, req.RequestID),
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(req.CorrelationID, req.RequestID),
		CausationID:   chooseNonEmpty(req.CausationID, req.RequestID),
		ActorID:       chooseNonEmpty(resp.ActorID, req.ActorID),
		RequestID:     strings.TrimSpace(req.RequestID),
		Command:       commandDispatchIssue,
		Payload:       v2events.MustMarshalPayload(issue),
	}
	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

// RecordDispatchFailed appends a run.failed orchestration event for one rejected child dispatch.
func (r *OrchestrationReconciler) RecordDispatchFailed(ctx context.Context, req spawn.Request, spawnErr error) (v2events.Event, error) {
	issue, ok := dispatchIssuePayload(req, spawn.Response{})
	if !ok {
		return v2events.Event{}, nil
	}

	now := r.now().UTC()
	issue["state"] = string(v2orchestration.StateReleased)
	issue["run_id"] = strings.TrimSpace(req.RunID)
	issue["actor_id"] = strings.TrimSpace(req.ActorID)
	issueID := dispatchMetaString(req.Metadata, "issue_id")

	denialReason, policyStatus, outcome, eligibility, suggestion := classifyDispatchFailure(spawnErr)
	state := v2orchestration.StateReleased
	nextAttempt := dispatchMetaInt(req.Metadata, "attempt")
	if retry, ok := retryPlanForMessage(denialReason, nextAttempt, r.retryPolicy, r.now); ok && policyStatus == v2orchestration.PolicyStatusBlocked {
		state = v2orchestration.StateRetryQueue
		policyStatus = v2orchestration.PolicyStatusOK
		eligibility = v2orchestration.EligibilityEligible
		nextAttempt = retry.Attempt
		issue["retry_due_at"] = retry.DueAt
		suggestion = chooseNonEmpty(suggestion, retry.Suggestion)
	}
	issue["state"] = string(state)
	issue["policy_status"] = string(policyStatus)
	issue["last_outcome"] = string(outcome)
	issue["eligibility"] = string(eligibility)
	if nextAttempt > 0 {
		issue["attempt"] = nextAttempt
	}
	if denialReason != "" {
		issue["denial_reason"] = denialReason
	}
	if suggestion != "" {
		issue["suggestion"] = suggestion
	}

	evt := v2events.Event{
		ID:            prefixedID(defaultOrchestrationEventIDPref, r.newID),
		StreamID:      chooseNonEmpty(req.RunID, req.RequestID, issueID),
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunFailed,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(req.CorrelationID, req.RequestID),
		CausationID:   chooseNonEmpty(req.CausationID, req.RequestID),
		ActorID:       strings.TrimSpace(req.ActorID),
		RequestID:     strings.TrimSpace(req.RequestID),
		Command:       commandDispatchIssue,
		Payload:       v2events.MustMarshalPayload(issue),
	}
	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

// RecordDispatchCompleted appends a run.completed orchestration event for one completed subprocess worker.
func (r *OrchestrationReconciler) RecordDispatchCompleted(ctx context.Context, worker coreworker.Record) (v2events.Event, error) {
	payload, ok := dispatchPayloadFromWorker(worker)
	if !ok {
		return v2events.Event{}, nil
	}

	now := r.now().UTC()
	payload["state"] = string(v2orchestration.StateReleased)
	payload["eligibility"] = string(v2orchestration.EligibilityEligible)
	payload["policy_status"] = string(v2orchestration.PolicyStatusOK)
	payload["agent_id"] = strings.TrimSpace(worker.AgentID)
	putDispatchString(payload, "tracker_state", chooseNonEmpty(dispatchMetaString(worker.Metadata, "tracker_state"), r.successTrackerState))

	evt := v2events.Event{
		ID:            prefixedID(defaultOrchestrationEventIDPref, r.newID),
		StreamID:      chooseNonEmpty(dispatchMetaString(worker.Metadata, "run_id"), worker.RunID, terminalStreamID(worker, "completed")),
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunCompleted,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(dispatchMetaString(worker.Metadata, "request_id"), dispatchMetaString(worker.Metadata, "issue_id")),
		CausationID:   chooseNonEmpty(dispatchMetaString(worker.Metadata, "request_id"), dispatchMetaString(worker.Metadata, "run_id"), worker.RunID),
		ActorID:       dispatchMetaString(worker.Metadata, "actor_id"),
		RequestID:     terminalRequestID(worker, "completed"),
		Command:       commandDispatchIssue,
		Payload:       v2events.MustMarshalPayload(payload),
	}
	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

// RecordDispatchRuntimeFailed appends a run.failed orchestration event for one failed subprocess worker.
func (r *OrchestrationReconciler) RecordDispatchRuntimeFailed(ctx context.Context, worker coreworker.Record) (v2events.Event, error) {
	payload, ok := dispatchPayloadFromWorker(worker)
	if !ok {
		return v2events.Event{}, nil
	}

	now := r.now().UTC()
	state := v2orchestration.StateReleased
	eligibility := v2orchestration.EligibilityIneligible
	policyStatus := v2orchestration.PolicyStatusBlocked
	attempt := dispatchMetaInt(worker.Metadata, "attempt")
	reason := chooseNonEmpty(strings.TrimSpace(worker.StopReason), terminalFailureReason(worker))
	if retry, ok := retryPlanForMessage(reason, attempt, r.retryPolicy, r.now); ok {
		state = v2orchestration.StateRetryQueue
		eligibility = v2orchestration.EligibilityEligible
		policyStatus = v2orchestration.PolicyStatusOK
		attempt = retry.Attempt
		payload["retry_due_at"] = retry.DueAt
		putDispatchString(payload, "suggestion", chooseNonEmpty(dispatchMetaString(worker.Metadata, "retry_suggestion"), retry.Suggestion))
	} else {
		putDispatchString(payload, "suggestion", resolveFailureSuggestion(worker.Metadata))
	}
	payload["state"] = string(state)
	payload["eligibility"] = string(eligibility)
	payload["policy_status"] = string(policyStatus)
	payload["last_outcome"] = string(v2orchestration.OutcomeExecFailed)
	payload["agent_id"] = strings.TrimSpace(worker.AgentID)
	if attempt > 0 {
		payload["attempt"] = attempt
	}
	putDispatchString(payload, "denial_reason", reason)

	evt := v2events.Event{
		ID:            prefixedID(defaultOrchestrationEventIDPref, r.newID),
		StreamID:      chooseNonEmpty(dispatchMetaString(worker.Metadata, "run_id"), worker.RunID, terminalStreamID(worker, "failed")),
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunFailed,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(dispatchMetaString(worker.Metadata, "request_id"), dispatchMetaString(worker.Metadata, "issue_id")),
		CausationID:   chooseNonEmpty(dispatchMetaString(worker.Metadata, "request_id"), dispatchMetaString(worker.Metadata, "run_id"), worker.RunID),
		ActorID:       dispatchMetaString(worker.Metadata, "actor_id"),
		RequestID:     terminalRequestID(worker, "failed"),
		Command:       commandDispatchIssue,
		Payload:       v2events.MustMarshalPayload(payload),
	}
	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

func (r *OrchestrationReconciler) appendAndProject(ctx context.Context, evt v2events.Event) error {
	if err := r.events.Append(ctx, evt); err != nil {
		return err
	}
	if r.projections != nil {
		if err := r.projections.Apply(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func dispatchIssuePayload(req spawn.Request, resp spawn.Response) (map[string]any, bool) {
	meta := req.Metadata
	if len(meta) == 0 {
		return nil, false
	}
	issueID := strings.TrimSpace(dispatchMetaString(meta, "issue_id"))
	if issueID == "" {
		return nil, false
	}
	payload := map[string]any{"issue_id": issueID}
	putDispatchString(payload, "workspace_id", dispatchMetaString(meta, "workspace_id"))
	putDispatchString(payload, "issue_identifier", dispatchMetaString(meta, "issue_identifier"))
	putDispatchString(payload, "title", dispatchMetaString(meta, "title"))
	putDispatchString(payload, "tracker_state", dispatchMetaString(meta, "tracker_state"))
	if attempt := dispatchMetaInt(meta, "attempt"); attempt > 0 {
		payload["attempt"] = attempt
	}
	if runID := chooseNonEmpty(resp.RunID, dispatchMetaString(meta, "run_id"), req.RunID); runID != "" {
		payload["run_id"] = runID
	}
	if actorID := chooseNonEmpty(resp.ActorID, dispatchMetaString(meta, "actor_id"), req.ActorID); actorID != "" {
		payload["actor_id"] = actorID
	}
	return payload, true
}

func dispatchPayloadFromWorker(worker coreworker.Record) (map[string]any, bool) {
	meta := worker.Metadata
	if len(meta) == 0 {
		return nil, false
	}
	issueID := strings.TrimSpace(dispatchMetaString(meta, "issue_id"))
	if issueID == "" {
		return nil, false
	}
	payload := map[string]any{"issue_id": issueID}
	putDispatchString(payload, "workspace_id", dispatchMetaString(meta, "workspace_id"))
	putDispatchString(payload, "issue_identifier", dispatchMetaString(meta, "issue_identifier"))
	putDispatchString(payload, "title", dispatchMetaString(meta, "title"))
	putDispatchString(payload, "run_id", chooseNonEmpty(dispatchMetaString(meta, "run_id"), worker.RunID))
	putDispatchString(payload, "actor_id", dispatchMetaString(meta, "actor_id"))
	if attempt := dispatchMetaInt(meta, "attempt"); attempt > 0 {
		payload["attempt"] = attempt
	}
	return payload, true
}

func classifyDispatchFailure(err error) (string, v2orchestration.PolicyStatus, v2orchestration.Outcome, v2orchestration.Eligibility, string) {
	denialReason := strings.TrimSpace(errString(err))
	policyStatus := v2orchestration.PolicyStatusBlocked
	outcome := v2orchestration.OutcomeExecFailed
	eligibility := v2orchestration.EligibilityIneligible
	var verr *v2errors.V2Error
	if err != nil && stderrors.As(err, &verr) {
		denialReason = strings.TrimSpace(verr.Message)
		if denialReason == "" {
			denialReason = strings.TrimSpace(verr.Error())
		}
		suggestion := detailString(verr.Details, "suggestion")
		switch verr.Kind {
		case v2errors.ErrPolicyViolation:
			return denialReason, v2orchestration.PolicyStatusDenied, v2orchestration.OutcomePolicyDenied, eligibility, suggestion
		case v2errors.ErrValidation:
			return denialReason, v2orchestration.PolicyStatusValidationError, v2orchestration.OutcomePreflightErr, eligibility, suggestion
		default:
			return denialReason, policyStatus, outcome, eligibility, suggestion
		}
	}
	return denialReason, policyStatus, outcome, eligibility, ""
}

type retryPlan struct {
	Attempt    int
	DueAt      string
	Suggestion string
}

func retryPlanForMessage(message string, currentAttempt int, policy v2jido.RetryPolicy, nowFn func() time.Time) (retryPlan, bool) {
	_, cfg, ok := classifyRetryFailure(message, policy)
	if !ok {
		return retryPlan{}, false
	}
	nextAttempt := currentAttempt + 1
	if nextAttempt < 1 {
		nextAttempt = 1
	}
	if cfg.MaxAttempts > 0 && nextAttempt > cfg.MaxAttempts {
		return retryPlan{}, false
	}
	dueAt := nowFn().UTC().Add(retryDelay(nextAttempt, cfg.BaseDelay, cfg.MaxDelay))
	return retryPlan{Attempt: nextAttempt, DueAt: dueAt.Format(time.RFC3339Nano), Suggestion: chooseNonEmpty(cfg.Suggestion, "retry scheduled after transient runtime failure")}, true
}

func classifyRetryFailure(message string, policy v2jido.RetryPolicy) (v2jido.RetryFailureClass, v2jido.RetryClassPolicy, bool) {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" || len(policy.Classes) == 0 {
		return "", v2jido.RetryClassPolicy{}, false
	}
	order := []v2jido.RetryFailureClass{
		v2jido.RetryFailureTimeout,
		v2jido.RetryFailureTransport,
		v2jido.RetryFailureStorage,
		v2jido.RetryFailureTransient,
	}
	for _, class := range order {
		cfg, ok := policy.Classes[class]
		if !ok || !cfg.Enabled {
			continue
		}
		for _, pattern := range cfg.Patterns {
			if strings.Contains(normalized, strings.ToLower(strings.TrimSpace(pattern))) {
				return class, cfg, true
			}
		}
	}
	return "", v2jido.RetryClassPolicy{}, false
}

func retryDelay(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 5 * time.Minute
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= max {
			return max
		}
		delay *= 2
		if delay > max {
			return max
		}
	}
	return delay
}

func resolveFailureSuggestion(meta map[string]any) string {
	return chooseNonEmpty(dispatchMetaString(meta, "retry_suggestion"), dispatchMetaString(meta, "suggestion"))
}

func terminalFailureReason(worker coreworker.Record) string {
	if worker.Status == coreworker.StatusCancelled {
		return "runtime child cancelled"
	}
	if worker.ExitCode > 0 {
		return fmt.Sprintf("runtime child failed with exit code %d", worker.ExitCode)
	}
	return "runtime child failed"
}

func terminalStreamID(worker coreworker.Record, suffix string) string {
	return chooseNonEmpty(strings.TrimSpace(worker.RunID), strings.TrimSpace(worker.WorkerID)+":"+suffix, strings.TrimSpace(worker.AgentID)+":"+suffix)
}

func terminalRequestID(worker coreworker.Record, suffix string) string {
	base := chooseNonEmpty(dispatchMetaString(worker.Metadata, "request_id"), dispatchMetaString(worker.Metadata, "issue_id"), worker.RunID, worker.AgentID)
	if base == "" {
		return ""
	}
	return base + ":" + suffix
}

func sameRun(values ...string) bool {
	base := ""
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if base == "" {
			base = trimmed
			continue
		}
		if base != trimmed {
			return false
		}
	}
	return true
}

func sortedWorkerRecords(records []coreworker.Record) []coreworker.Record {
	if len(records) == 0 {
		return nil
	}
	out := append([]coreworker.Record(nil), records...)
	sort.Slice(out, func(i, j int) bool {
		left := chooseNonEmpty(out[i].AgentID, out[i].WorkerID)
		right := chooseNonEmpty(out[j].AgentID, out[j].WorkerID)
		return left < right
	})
	return out
}

func normalizeParentAgentIDs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func dispatchMetaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func dispatchMetaInt(meta map[string]any, key string) int {
	if len(meta) == 0 || meta[key] == nil {
		return 0
	}
	switch value := meta[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func putDispatchString(dst map[string]any, key, value string) {
	if dst == nil {
		return
	}
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		dst[key] = trimmed
	}
}

func detailString(details map[string]any, key string) string {
	if len(details) == 0 {
		return ""
	}
	value, ok := details[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func prefixedID(prefix string, newID func() string) string {
	if newID == nil {
		return prefix
	}
	value := strings.TrimSpace(newID())
	if value == "" {
		return prefix
	}
	if strings.HasPrefix(value, prefix+"-") {
		return value
	}
	return prefix + "-" + value
}
