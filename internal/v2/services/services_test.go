package services_test

import (
	"context"
	stderrors "errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/kill"
	"github.com/jkatigb/agentctl/internal/v2/core/list"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
	"github.com/jkatigb/agentctl/internal/v2/services"
)

func TestSpawnService_ValidInputCreatesV2Record(t *testing.T) {
	t.Parallel()

	runSvc := &fakeRunInvoker{
		out: run.TurnOutput{
			TurnID:     "turn-0001",
			Summary:    "done",
			Iterations: 1,
			ToolCalls:  1,
		},
	}
	eventsReader := &fakeEventReader{
		events: []events.Event{
			{
				ID:         "evt-001",
				StreamID:   "run-0001",
				StreamType: events.StreamTypeRun,
				EventType:  events.EventRunStarted,
				RequestID:  "req-001",
				ActorID:    "actor:overseer:id-0001",
				Command:    "spawn",
			},
			{
				ID:         "evt-002",
				StreamID:   "run-0001",
				StreamType: events.StreamTypeRun,
				EventType:  events.EventRunCompleted,
				RequestID:  "req-001",
				ActorID:    "actor:overseer:id-0001",
				Command:    "spawn",
			},
		},
	}
	projections := newFakeProjectionStore()

	svc := services.NewSpawnService(services.SpawnDependencies{
		RunService:  runSvc,
		Events:      eventsReader,
		Projections: projections,
		Now: func() time.Time {
			return time.Date(2026, time.February, 18, 18, 0, 0, 0, time.UTC)
		},
		NewID: sequentialID("id"),
	})

	resp, err := svc.Spawn(context.Background(), spawn.Request{
		RequestID: "req-001",
		Role:      "overseer",
		RunID:     "run-0001",
		Prompt:    "review storage",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if resp.RunID != "run-0001" {
		t.Fatalf("run_id=%q want run-0001", resp.RunID)
	}
	if resp.AgentID == "" {
		t.Fatal("agent_id is empty")
	}
	if resp.ActorID == "" {
		t.Fatal("actor_id is empty")
	}
	if resp.Status != "completed" {
		t.Fatalf("status=%q want completed", resp.Status)
	}
	if runSvc.calls != 1 {
		t.Fatalf("run invocations=%d want 1", runSvc.calls)
	}
	if got := runSvc.lastIn.Command; got != "spawn" {
		t.Fatalf("runner command=%q want spawn", got)
	}
	state, err := projections.GetRunState(context.Background(), "run-0001")
	if err != nil {
		t.Fatalf("GetRunState() error = %v", err)
	}
	if state.RequestID != "req-001" {
		t.Fatalf("projection request_id=%q want req-001", state.RequestID)
	}
}

func TestSpawnService_DuplicateRequestIDIdempotent(t *testing.T) {
	t.Parallel()

	projections := newFakeProjectionStore()
	projections.byRun["run-existing"] = services.RunState{
		RunID:     "run-existing",
		Status:    "running",
		RequestID: "req-dup",
		ActorID:   "actor:overseer:seed",
		UpdatedAt: time.Now().UTC(),
	}

	runSvc := &fakeRunInvoker{
		out: run.TurnOutput{TurnID: "turn-new"},
	}
	svc := services.NewSpawnService(services.SpawnDependencies{
		RunService:  runSvc,
		Projections: projections,
		Now:         func() time.Time { return time.Date(2026, time.February, 18, 18, 10, 0, 0, time.UTC) },
		NewID:       sequentialID("id"),
	})

	resp, err := svc.Spawn(context.Background(), spawn.Request{
		RequestID: "req-dup",
		Role:      "overseer",
		AgentID:   "agent-existing",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if !resp.Idempotent {
		t.Fatal("expected idempotent response")
	}
	if resp.RunID != "run-existing" {
		t.Fatalf("run_id=%q want run-existing", resp.RunID)
	}
	if runSvc.calls != 0 {
		t.Fatalf("run invocations=%d want 0", runSvc.calls)
	}
}

func TestSpawnService_WithParentAgentUsesRuntimeSpawner(t *testing.T) {
	t.Parallel()

	runtimeSpawner := &fakeRuntimeSpawner{
		resp: spawn.Response{
			RunID:     "run-jido-001",
			AgentID:   "agent:worker-1",
			ActorID:   "actor:worker-1",
			TurnID:    "spawn-1",
			RequestID: "req-runtime",
			Status:    "spawned",
		},
	}
	runSvc := &fakeRunInvoker{}
	svc := services.NewSpawnService(services.SpawnDependencies{
		RunService:     runSvc,
		RuntimeSpawner: runtimeSpawner,
		Now:            func() time.Time { return time.Date(2026, time.March, 6, 10, 0, 0, 0, time.UTC) },
		NewID:          sequentialID("id"),
	})

	resp, err := svc.Spawn(context.Background(), spawn.Request{
		RequestID:     "req-runtime",
		Role:          "worker",
		RunID:         "run-jido-001",
		AgentID:       "agent:worker-1",
		ActorID:       "actor:worker-1",
		ParentAgentID: "agent:parent-1",
		Prompt:        "investigate issue",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if resp.Status != "spawned" {
		t.Fatalf("status=%q want spawned", resp.Status)
	}
	if runtimeSpawner.calls != 1 {
		t.Fatalf("runtime spawn calls=%d want 1", runtimeSpawner.calls)
	}
	if runSvc.calls != 0 {
		t.Fatalf("run invocations=%d want 0", runSvc.calls)
	}
	if runtimeSpawner.last.ParentAgentID != "agent:parent-1" {
		t.Fatalf("parent_agent_id=%q want agent:parent-1", runtimeSpawner.last.ParentAgentID)
	}
}

func TestSpawnService_WithParentAgentRequiresRuntimeSpawner(t *testing.T) {
	t.Parallel()

	svc := services.NewSpawnService(services.SpawnDependencies{
		Now:   func() time.Time { return time.Date(2026, time.March, 6, 10, 5, 0, 0, time.UTC) },
		NewID: sequentialID("id"),
	})

	_, err := svc.Spawn(context.Background(), spawn.Request{
		RequestID:     "req-parent-missing",
		Role:          "worker",
		ParentAgentID: "agent:parent-1",
	})
	if err == nil {
		t.Fatal("expected runtime spawner dependency error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrDependency {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrDependency)
	}
}

func TestAskService_PolicyViolationReturns403Mapping(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeAskDispatcher{}
	svc := services.NewAskService(services.AskDependencies{
		Dispatcher: dispatcher,
		Policy: fakeAskPolicy{
			err: stderrors.New("blocked"),
		},
		NewID: sequentialID("id"),
	})

	_, err := svc.Ask(context.Background(), ask.Request{
		AgentID:  "agent-001",
		Question: "what did you find?",
	})
	if err == nil {
		t.Fatal("expected policy error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrPolicyViolation {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrPolicyViolation)
	}
	if status := verr.HTTPStatus(); status != 403 {
		t.Fatalf("http_status=%d want 403", status)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("dispatcher calls=%d want 0", dispatcher.calls)
	}
}

func TestAskService_AppendsAndProjectsDispatchEvent(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeAskDispatcher{}
	eventStore := &fakeEventAppender{}
	projections := newFakeProjectionStore()
	now := time.Date(2026, time.March, 5, 12, 30, 0, 0, time.UTC)

	svc := services.NewAskService(services.AskDependencies{
		Dispatcher:  dispatcher,
		Events:      eventStore,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       sequentialID("id"),
	})

	resp, err := svc.Ask(context.Background(), ask.Request{
		RequestID: "req-ask-1",
		AskID:     "ask-1",
		AgentID:   "agent-001",
		Namespace: "agent:001",
		Kind:      "context",
		Question:  "what changed?",
		CallerNS:  "cli:1",
		Timeout:   45 * time.Second,
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if resp.Status != "sent" {
		t.Fatalf("status=%q want sent", resp.Status)
	}
	if len(eventStore.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(eventStore.events))
	}
	evt := eventStore.events[0]
	if evt.EventType != events.EventRunStarted {
		t.Fatalf("event_type=%q want %q", evt.EventType, events.EventRunStarted)
	}
	if evt.StreamID != "ask:ask-1" {
		t.Fatalf("stream_id=%q want ask:ask-1", evt.StreamID)
	}
	if evt.Command != "ask" {
		t.Fatalf("command=%q want ask", evt.Command)
	}

	state, err := projections.GetRunState(context.Background(), "ask:ask-1")
	if err != nil {
		t.Fatalf("GetRunState() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("projection status=%q want running", state.Status)
	}
	if state.RequestID != "req-ask-1" {
		t.Fatalf("projection request_id=%q want req-ask-1", state.RequestID)
	}
}

func TestAskService_DispatchFailureRecordsRunFailed(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeAskDispatcher{err: stderrors.New("mailbox down")}
	eventStore := &fakeEventAppender{}
	projections := newFakeProjectionStore()
	now := time.Date(2026, time.March, 5, 12, 40, 0, 0, time.UTC)

	svc := services.NewAskService(services.AskDependencies{
		Dispatcher:  dispatcher,
		Events:      eventStore,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       sequentialID("id"),
	})

	_, err := svc.Ask(context.Background(), ask.Request{
		RequestID: "req-ask-fail",
		AskID:     "ask-fail",
		AgentID:   "agent-001",
		Namespace: "agent:001",
		Kind:      "context",
		Question:  "what changed?",
		CallerNS:  "cli:1",
		Timeout:   45 * time.Second,
	})
	if err == nil {
		t.Fatal("expected Ask() error")
	}
	if len(eventStore.events) != 2 {
		t.Fatalf("appended events=%d want 2", len(eventStore.events))
	}
	if eventStore.events[0].EventType != events.EventRunStarted {
		t.Fatalf("event[0].type=%q want %q", eventStore.events[0].EventType, events.EventRunStarted)
	}
	if eventStore.events[1].EventType != events.EventRunFailed {
		t.Fatalf("event[1].type=%q want %q", eventStore.events[1].EventType, events.EventRunFailed)
	}
	state, getErr := projections.GetRunState(context.Background(), "ask:ask-fail")
	if getErr != nil {
		t.Fatalf("GetRunState() error = %v", getErr)
	}
	if state.Status != "failed" {
		t.Fatalf("projection status=%q want failed", state.Status)
	}
}

func TestKillService_UsesProvidedRunID(t *testing.T) {
	t.Parallel()

	killer := &fakeKiller{}

	svc := services.NewKillService(services.KillDependencies{
		Killer: killer,
	})

	resp, err := svc.Kill(context.Background(), kill.Request{
		RunID: "run-v2-001",
	})
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if resp.RunID != "run-v2-001" {
		t.Fatalf("run_id=%q want run-v2-001", resp.RunID)
	}
	if killer.last != "run-v2-001" {
		t.Fatalf("killer target=%q want run-v2-001", killer.last)
	}
}

func TestKillService_ProjectionNotFoundReturnsNotFound(t *testing.T) {
	t.Parallel()

	projections := newFakeProjectionStore()
	killer := &fakeKiller{}

	svc := services.NewKillService(services.KillDependencies{
		Killer:      killer,
		Projections: projections,
	})

	_, err := svc.Kill(context.Background(), kill.Request{
		RunID: "run-missing",
	})
	if err == nil {
		t.Fatal("expected not found error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrNotFound {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrNotFound)
	}
	if killer.last != "" {
		t.Fatalf("killer target=%q want empty", killer.last)
	}
}

func TestListService_UsesProjectionAndFilters(t *testing.T) {
	t.Parallel()

	projections := newFakeProjectionStore()
	projections.byRun["run-1"] = services.RunState{
		RunID:     "run-1",
		Status:    "running",
		Command:   "spawn",
		RequestID: "req-1",
		ActorID:   "actor-1",
		UpdatedAt: time.Date(2026, time.February, 18, 10, 0, 0, 0, time.UTC),
	}
	projections.byRun["run-2"] = services.RunState{
		RunID:     "run-2",
		Status:    "completed",
		Command:   "spawn",
		RequestID: "req-2",
		ActorID:   "actor-2",
		UpdatedAt: time.Date(2026, time.February, 18, 9, 0, 0, 0, time.UTC),
	}
	projections.byRun["run-3"] = services.RunState{
		RunID:     "run-3",
		Status:    "running",
		Command:   "ask",
		RequestID: "req-3",
		ActorID:   "actor-1",
		UpdatedAt: time.Date(2026, time.February, 18, 8, 0, 0, 0, time.UTC),
	}

	svc := services.NewListService(projections)
	resp, err := svc.List(context.Background(), list.Request{
		Limit:   5,
		Status:  "running",
		Command: "spawn",
		ActorID: "actor-1",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if projections.lastFilter.Status != "running" || projections.lastFilter.Command != "spawn" || projections.lastFilter.ActorID != "actor-1" {
		t.Fatalf("unexpected filter %+v", projections.lastFilter)
	}
	if resp.Count != 1 {
		t.Fatalf("count=%d want 1", resp.Count)
	}
	if len(resp.Items) != 1 || resp.Items[0].RunID != "run-1" {
		t.Fatalf("items=%+v want only run-1", resp.Items)
	}
}

type fakeRunInvoker struct {
	out    run.TurnOutput
	err    error
	calls  int
	lastIn run.TurnInput
}

func (f *fakeRunInvoker) Run(ctx context.Context, in run.TurnInput) (run.TurnOutput, error) {
	f.calls++
	f.lastIn = in
	return f.out, f.err
}

type fakeRuntimeSpawner struct {
	resp  spawn.Response
	err   error
	calls int
	last  spawn.Request
}

func (f *fakeRuntimeSpawner) SpawnChild(_ context.Context, req spawn.Request) (spawn.Response, error) {
	f.calls++
	f.last = req
	return f.resp, f.err
}

type fakeEventReader struct {
	events []events.Event
}

func (f *fakeEventReader) ListStream(context.Context, events.StreamFilter) ([]events.Event, error) {
	out := make([]events.Event, len(f.events))
	copy(out, f.events)
	return out, nil
}

type fakeEventAppender struct {
	events []events.Event
}

func (f *fakeEventAppender) Append(_ context.Context, evt events.Event) error {
	f.events = append(f.events, evt)
	return nil
}

type fakeProjectionStore struct {
	byRun      map[string]services.RunState
	lastFilter services.RunStateFilter
	now        func() time.Time
}

func newFakeProjectionStore() *fakeProjectionStore {
	return &fakeProjectionStore{
		byRun: map[string]services.RunState{},
		now: func() time.Time {
			return time.Date(2026, time.February, 18, 12, 0, 0, 0, time.UTC)
		},
	}
}

func (f *fakeProjectionStore) Apply(_ context.Context, evt events.Event) error {
	if evt.StreamType != events.StreamTypeRun {
		return nil
	}
	state := f.byRun[evt.StreamID]
	state.RunID = evt.StreamID
	state.Command = evt.Command
	state.RequestID = evt.RequestID
	state.ActorID = evt.ActorID
	state.UpdatedAt = f.now().UTC()

	switch evt.EventType {
	case events.EventRunStarted:
		state.Status = "running"
	case events.EventRunCompleted:
		state.Status = "completed"
	case events.EventRunFailed:
		state.Status = "failed"
	}
	f.byRun[evt.StreamID] = state
	return nil
}

func (f *fakeProjectionStore) GetRunState(_ context.Context, runID string) (services.RunState, error) {
	state, ok := f.byRun[runID]
	if !ok {
		return services.RunState{}, events.ErrNotFound
	}
	return state, nil
}

func (f *fakeProjectionStore) GetRunStateByRequestID(_ context.Context, requestID string) (services.RunState, error) {
	for _, state := range f.byRun {
		if state.RequestID == requestID {
			return state, nil
		}
	}
	return services.RunState{}, events.ErrNotFound
}

func (f *fakeProjectionStore) ListRunStates(_ context.Context, filter services.RunStateFilter) ([]services.RunState, error) {
	f.lastFilter = filter
	out := make([]services.RunState, 0, len(f.byRun))
	for _, state := range f.byRun {
		if filter.Status != "" && state.Status != filter.Status {
			continue
		}
		if filter.Command != "" && state.Command != filter.Command {
			continue
		}
		if filter.ActorID != "" && state.ActorID != filter.ActorID {
			continue
		}
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

type fakeAskPolicy struct {
	err error
}

func (f fakeAskPolicy) AuthorizeAsk(context.Context, ask.Request) error {
	return f.err
}

type fakeAskDispatcher struct {
	calls     int
	lastMsg   ask.Message
	messageID string
	err       error
}

func (f *fakeAskDispatcher) Send(_ context.Context, msg ask.Message) (string, error) {
	f.calls++
	f.lastMsg = msg
	if f.err != nil {
		return "", f.err
	}
	if strings.TrimSpace(f.messageID) != "" {
		return f.messageID, nil
	}
	return "msg-001", nil
}

type fakeKiller struct {
	last string
}

func (f *fakeKiller) Kill(_ context.Context, runID string) error {
	f.last = runID
	return nil
}

func sequentialID(prefix string) func() string {
	var seq int
	return func() string {
		seq++
		return prefix + "-" + time.Date(2026, time.February, 18, 0, 0, seq, 0, time.UTC).Format("150405")
	}
}
