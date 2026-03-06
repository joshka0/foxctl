package jido

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
	v2orchestration "github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

const commandDispatchIssue = "orchestration/dispatch-issue"

// OrchestrationReconciler appends Jido-backed dispatch outcomes as canonical v2 orchestration events.
type OrchestrationReconciler struct {
	events      EventAppender
	projections ProjectionApplier
	reader      OrchestrationCardReader
	client      Client
	parentIDs   []string
	reviewState string
	retryPolicy RetryPolicy
	now         func() time.Time
	newID       func() string
}

// OrchestrationCardReader reads projected orchestration cards for lifecycle filtering.
type OrchestrationCardReader interface {
	Card(ctx context.Context, req v2orchestration.CardRequest) (v2orchestration.CardResponse, error)
}

// OrchestrationReconcilerConfig configures orchestration event reconciliation.
type OrchestrationReconcilerConfig struct {
	Events              EventAppender
	Projections         ProjectionApplier
	Reader              OrchestrationCardReader
	Client              Client
	ParentAgentIDs      []string
	SuccessTrackerState string
	RetryPolicy         RetryPolicy
	Now                 func() time.Time
	NewID               func() string
}

// NewOrchestrationReconciler builds a reconciler for runtime-backed orchestration dispatch.
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
			return fmt.Sprintf("%s-%06d", defaultEventIDPre, seq)
		}
	}
	return &OrchestrationReconciler{
		events:      cfg.Events,
		projections: cfg.Projections,
		reader:      cfg.Reader,
		client:      cfg.Client,
		parentIDs:   normalizeParentAgentIDs(cfg.ParentAgentIDs),
		reviewState: normalizeSuccessTrackerState(cfg.SuccessTrackerState),
		retryPolicy: resolveRetryPolicyConfig(cfg.RetryPolicy),
		now:         cfg.Now,
		newID:       cfg.NewID,
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

// Reconcile polls configured Jido parent agents and converts terminal child state into orchestration events.
func (r *OrchestrationReconciler) Reconcile(ctx context.Context) error {
	if r == nil || len(r.parentIDs) == 0 {
		return nil
	}
	if r.client == nil {
		return fmt.Errorf("jido client is required for orchestration reconcile")
	}
	if r.reader == nil {
		return fmt.Errorf("orchestration card reader is required for orchestration reconcile")
	}

	for _, parentID := range r.parentIDs {
		childrenResp, err := r.client.GetChildren(ctx, GetChildrenRequest{AgentID: parentID})
		if err != nil {
			return fmt.Errorf("list jido children for %s: %w", parentID, err)
		}

		for _, child := range sortedChildren(childrenResp.Children) {
			issueID := dispatchMetaString(child.Metadata, "issue_id")
			if issueID == "" {
				continue
			}

			workspaceID := dispatchMetaString(child.Metadata, "workspace_id")
			card, err := r.reader.Card(ctx, v2orchestration.CardRequest{
				WorkspaceID: workspaceID,
				IssueID:     issueID,
			})
			if err != nil {
				return fmt.Errorf("read orchestration card issue_id=%s: %w", issueID, err)
			}
			if card.Card.State != v2orchestration.StateRunning {
				continue
			}
			if !sameRun(card.Card.RunID, dispatchMetaString(child.Metadata, "run_id")) {
				continue
			}
			if card.Card.AgentID != "" && strings.TrimSpace(child.AgentID) != "" && card.Card.AgentID != strings.TrimSpace(child.AgentID) {
				continue
			}

			childAgentID := strings.TrimSpace(child.AgentID)
			if childAgentID == "" {
				continue
			}

			stateResp, err := r.client.State(ctx, StateRequest{AgentID: childAgentID})
			if err != nil {
				return fmt.Errorf("read jido child state agent_id=%s: %w", childAgentID, err)
			}

			outcome, err := decodeChildLifecycle(stateResp)
			if err != nil {
				return fmt.Errorf("decode jido child state agent_id=%s: %w", childAgentID, err)
			}
			if !outcome.Terminal {
				continue
			}

			if outcome.Success {
				if _, err := r.RecordDispatchCompleted(ctx, child, outcome); err != nil {
					return err
				}
				continue
			}
			if _, err := r.RecordDispatchRuntimeFailed(ctx, child, outcome); err != nil {
				return err
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
		ID:            prefixedID(defaultEventIDPre, r.newID),
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
	if retry, ok := r.retryPlan(denialReason, nextAttempt); ok && policyStatus == v2orchestration.PolicyStatusBlocked {
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
		ID:            prefixedID(defaultEventIDPre, r.newID),
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

// RecordDispatchCompleted appends a run.completed orchestration event for one completed runtime child.
func (r *OrchestrationReconciler) RecordDispatchCompleted(ctx context.Context, child ChildRef, outcome childLifecycle) (v2events.Event, error) {
	payload, ok := dispatchPayloadFromChild(child)
	if !ok {
		return v2events.Event{}, nil
	}

	now := r.now().UTC()
	payload["state"] = string(v2orchestration.StateReleased)
	payload["eligibility"] = string(v2orchestration.EligibilityEligible)
	payload["policy_status"] = string(v2orchestration.PolicyStatusOK)
	payload["agent_id"] = strings.TrimSpace(child.AgentID)
	putDispatchString(payload, "tracker_state", resolveSuccessTrackerState(child.Metadata, outcome.TrackerState, r.reviewState))

	evt := v2events.Event{
		ID:            prefixedID(defaultEventIDPre, r.newID),
		StreamID:      chooseNonEmpty(dispatchMetaString(child.Metadata, "run_id"), terminalStreamID(child, "completed")),
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunCompleted,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(dispatchMetaString(child.Metadata, "request_id"), dispatchMetaString(child.Metadata, "issue_id")),
		CausationID:   chooseNonEmpty(dispatchMetaString(child.Metadata, "request_id"), dispatchMetaString(child.Metadata, "run_id")),
		ActorID:       dispatchMetaString(child.Metadata, "actor_id"),
		RequestID:     terminalRequestID(child, "completed"),
		Command:       commandDispatchIssue,
		Payload:       v2events.MustMarshalPayload(payload),
	}

	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

// RecordDispatchRuntimeFailed appends a run.failed orchestration event for one failed runtime child.
func (r *OrchestrationReconciler) RecordDispatchRuntimeFailed(ctx context.Context, child ChildRef, outcome childLifecycle) (v2events.Event, error) {
	payload, ok := dispatchPayloadFromChild(child)
	if !ok {
		return v2events.Event{}, nil
	}

	now := r.now().UTC()
	state := v2orchestration.StateReleased
	eligibility := v2orchestration.EligibilityIneligible
	policyStatus := v2orchestration.PolicyStatusBlocked
	attempt := dispatchMetaInt(child.Metadata, "attempt")
	if retry, ok := r.retryPlan(outcome.Error, attempt); ok {
		state = v2orchestration.StateRetryQueue
		eligibility = v2orchestration.EligibilityEligible
		policyStatus = v2orchestration.PolicyStatusOK
		attempt = retry.Attempt
		payload["retry_due_at"] = retry.DueAt
		putDispatchString(payload, "suggestion", chooseNonEmpty(dispatchMetaString(child.Metadata, "retry_suggestion"), retry.Suggestion))
	} else {
		putDispatchString(payload, "suggestion", resolveFailureSuggestion(child.Metadata))
	}
	payload["state"] = string(state)
	payload["eligibility"] = string(eligibility)
	payload["policy_status"] = string(policyStatus)
	payload["last_outcome"] = string(v2orchestration.OutcomeExecFailed)
	payload["agent_id"] = strings.TrimSpace(child.AgentID)
	if attempt > 0 {
		payload["attempt"] = attempt
	}
	putDispatchString(payload, "denial_reason", outcome.Error)

	evt := v2events.Event{
		ID:            prefixedID(defaultEventIDPre, r.newID),
		StreamID:      chooseNonEmpty(dispatchMetaString(child.Metadata, "run_id"), terminalStreamID(child, "failed")),
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunFailed,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(dispatchMetaString(child.Metadata, "request_id"), dispatchMetaString(child.Metadata, "issue_id")),
		CausationID:   chooseNonEmpty(dispatchMetaString(child.Metadata, "request_id"), dispatchMetaString(child.Metadata, "run_id")),
		ActorID:       dispatchMetaString(child.Metadata, "actor_id"),
		RequestID:     terminalRequestID(child, "failed"),
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

	payload := map[string]any{
		"issue_id": issueID,
	}
	putDispatchString(payload, "workspace_id", dispatchMetaString(meta, "workspace_id"))
	putDispatchString(payload, "issue_identifier", dispatchMetaString(meta, "issue_identifier"))
	putDispatchString(payload, "title", dispatchMetaString(meta, "title"))
	putDispatchString(payload, "tracker_state", dispatchMetaString(meta, "tracker_state"))

	if attempt := dispatchMetaInt(meta, "attempt"); attempt > 0 {
		payload["attempt"] = attempt
	}
	if retryDueAt := dispatchMetaString(meta, "retry_due_at"); retryDueAt != "" {
		payload["retry_due_at"] = retryDueAt
	}
	if runID := chooseNonEmpty(resp.RunID, dispatchMetaString(meta, "run_id"), req.RunID); runID != "" {
		payload["run_id"] = runID
	}
	if actorID := chooseNonEmpty(resp.ActorID, dispatchMetaString(meta, "actor_id"), req.ActorID); actorID != "" {
		payload["actor_id"] = actorID
	}
	return payload, true
}

func dispatchPayloadFromChild(child ChildRef) (map[string]any, bool) {
	meta := child.Metadata
	if len(meta) == 0 {
		return nil, false
	}
	issueID := strings.TrimSpace(dispatchMetaString(meta, "issue_id"))
	if issueID == "" {
		return nil, false
	}

	payload := map[string]any{
		"issue_id": issueID,
	}
	putDispatchString(payload, "workspace_id", dispatchMetaString(meta, "workspace_id"))
	putDispatchString(payload, "issue_identifier", dispatchMetaString(meta, "issue_identifier"))
	putDispatchString(payload, "title", dispatchMetaString(meta, "title"))
	putDispatchString(payload, "run_id", dispatchMetaString(meta, "run_id"))
	putDispatchString(payload, "actor_id", dispatchMetaString(meta, "actor_id"))
	if attempt := dispatchMetaInt(meta, "attempt"); attempt > 0 {
		payload["attempt"] = attempt
	}
	return payload, true
}

func classifyDispatchFailure(err error) (denialReason string, policyStatus v2orchestration.PolicyStatus, outcome v2orchestration.Outcome, eligibility v2orchestration.Eligibility, suggestion string) {
	denialReason = strings.TrimSpace(errString(err))
	policyStatus = v2orchestration.PolicyStatusBlocked
	outcome = v2orchestration.OutcomeExecFailed
	eligibility = v2orchestration.EligibilityIneligible

	var verr *v2errors.V2Error
	if err != nil && stderrors.As(err, &verr) {
		denialReason = strings.TrimSpace(verr.Message)
		if denialReason == "" {
			denialReason = strings.TrimSpace(verr.Error())
		}
		suggestion = dispatchDetailString(verr.Details, "suggestion")
		switch verr.Kind {
		case v2errors.ErrPolicyViolation:
			policyStatus = v2orchestration.PolicyStatusDenied
			outcome = v2orchestration.OutcomePolicyDenied
		case v2errors.ErrValidation:
			policyStatus = v2orchestration.PolicyStatusValidationError
			outcome = v2orchestration.OutcomePreflightErr
		default:
			policyStatus = v2orchestration.PolicyStatusBlocked
			outcome = v2orchestration.OutcomeExecFailed
		}
	}

	return denialReason, policyStatus, outcome, eligibility, suggestion
}

type retryPlan struct {
	Class      RetryFailureClass
	Attempt    int
	DueAt      string
	Suggestion string
}

func (r *OrchestrationReconciler) retryPlan(message string, currentAttempt int) (retryPlan, bool) {
	class, cfg, ok := classifyRetryFailure(message, r.retryPolicy)
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
	dueAt := r.now().UTC().Add(retryDelay(nextAttempt, cfg.BaseDelay, cfg.MaxDelay))
	return retryPlan{
		Class:      class,
		Attempt:    nextAttempt,
		DueAt:      dueAt.Format(time.RFC3339Nano),
		Suggestion: chooseNonEmpty(cfg.Suggestion, "retry scheduled after transient runtime failure"),
	}, true
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

type childLifecycle struct {
	Terminal     bool
	Success      bool
	Status       string
	Error        string
	TrackerState string
}

func decodeChildLifecycle(resp StateResponse) (childLifecycle, error) {
	if strings.EqualFold(strings.TrimSpace(resp.Status), "not_found") {
		return childLifecycle{
			Terminal: true,
			Status:   "failed",
			Error:    "runtime agent not found",
		}, nil
	}
	if len(resp.State) == 0 || string(resp.State) == "null" {
		return childLifecycle{}, nil
	}

	var root map[string]any
	if err := json.Unmarshal(resp.State, &root); err != nil {
		return childLifecycle{}, err
	}

	target := mapAt(root, "agentctl")
	if len(target) == 0 {
		target = root
	}
	lastResult := mapAt(target, "last_result")

	status := normalizeCallbackStatus(stringValue(target["status"]))
	if status == "" {
		status = normalizeCallbackStatus(stringValue(root["status"]))
	}

	errMsg := errorValue(target["last_error"])
	if errMsg == "" {
		errMsg = errorValue(lastResult["error"])
	}

	envelope := mapAt(lastResult, "envelope")
	if envStatus := strings.TrimSpace(stringValue(envelope["status"])); strings.EqualFold(envStatus, "error") {
		status = "failed"
		if errMsg == "" {
			errMsg = extractEnvelopeError(envelope)
		}
	}
	if errMsg != "" && status != "completed" {
		status = "failed"
	}

	trackerState := chooseNonEmpty(
		strings.TrimSpace(stringValue(lastResult["tracker_state"])),
		strings.TrimSpace(stringValue(target["tracker_state"])),
		strings.TrimSpace(stringValue(root["tracker_state"])),
	)

	switch status {
	case "completed":
		if errMsg != "" {
			return childLifecycle{
				Terminal:     true,
				Status:       "failed",
				Error:        errMsg,
				TrackerState: trackerState,
			}, nil
		}
		return childLifecycle{
			Terminal:     true,
			Success:      true,
			Status:       "completed",
			TrackerState: trackerState,
		}, nil
	case "failed":
		return childLifecycle{
			Terminal:     true,
			Status:       "failed",
			Error:        chooseNonEmpty(errMsg, "runtime child failed"),
			TrackerState: trackerState,
		}, nil
	default:
		return childLifecycle{
			Terminal:     false,
			Status:       status,
			TrackerState: trackerState,
		}, nil
	}
}

func normalizeParentAgentIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func normalizeSuccessTrackerState(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Human Review"
	}
	return value
}

func sortedChildren(children map[string]ChildRef) []ChildRef {
	if len(children) == 0 {
		return nil
	}
	keys := make([]string, 0, len(children))
	for key := range children {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]ChildRef, 0, len(keys))
	for _, key := range keys {
		out = append(out, children[key])
	}
	return out
}

func sameRun(cardRunID, metaRunID string) bool {
	cardRunID = strings.TrimSpace(cardRunID)
	metaRunID = strings.TrimSpace(metaRunID)
	if cardRunID == "" || metaRunID == "" {
		return true
	}
	return cardRunID == metaRunID
}

func terminalRequestID(child ChildRef, suffix string) string {
	base := chooseNonEmpty(
		dispatchMetaString(child.Metadata, "request_id"),
		dispatchMetaString(child.Metadata, "issue_id"),
		child.AgentID,
	)
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return base + ":terminal"
	}
	return base + ":terminal:" + suffix
}

func terminalStreamID(child ChildRef, suffix string) string {
	base := chooseNonEmpty(
		dispatchMetaString(child.Metadata, "issue_id"),
		child.AgentID,
	)
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return "orch:" + base
	}
	return "orch:" + base + ":" + suffix
}

func resolveSuccessTrackerState(meta map[string]any, fromState, fallback string) string {
	return chooseNonEmpty(
		dispatchMetaString(meta, "completion_tracker_state"),
		dispatchMetaString(meta, "success_tracker_state"),
		strings.TrimSpace(fromState),
		normalizeSuccessTrackerState(fallback),
	)
}

func resolveFailureSuggestion(meta map[string]any) string {
	return chooseNonEmpty(
		dispatchMetaString(meta, "failure_suggestion"),
		"inspect child runtime state",
	)
}

func dispatchMetaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(raw))
}

func dispatchMetaInt(meta map[string]any, key string) int {
	if len(meta) == 0 {
		return 0
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return 0
	}
	switch value := raw.(type) {
	case int:
		return value
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

func dispatchDetailString(details map[string]any, key string) string {
	if len(details) == 0 {
		return ""
	}
	raw, ok := details[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(raw))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
