package services_test

import (
	"context"
	stderrors "errors"
	"sort"
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

func TestKillService_V1IDMappedToV2(t *testing.T) {
	t.Parallel()

	projections := newFakeProjectionStore()
	idMap := &fakeIDMap{
		mapping: map[string]string{
			"legacy-run-001": "run-v2-001",
		},
	}
	killer := &fakeKiller{}

	svc := services.NewKillService(services.KillDependencies{
		Killer:      killer,
		Projections: projections,
		IDMap:       idMap,
	})

	resp, err := svc.Kill(context.Background(), kill.Request{
		RunID: "legacy-run-001",
	})
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if resp.RunID != "run-v2-001" {
		t.Fatalf("run_id=%q want run-v2-001", resp.RunID)
	}
	if !resp.MappedFromLegacy {
		t.Fatal("expected mapped_from_legacy=true")
	}
	if killer.last != "run-v2-001" {
		t.Fatalf("killer target=%q want run-v2-001", killer.last)
	}
}

func TestKillService_V1IDMappedToV2_WithoutProjections(t *testing.T) {
	t.Parallel()

	idMap := &fakeIDMap{
		mapping: map[string]string{
			"legacy-run-002": "run-v2-002",
		},
	}
	killer := &fakeKiller{}

	svc := services.NewKillService(services.KillDependencies{
		Killer: killer,
		IDMap:  idMap,
	})

	resp, err := svc.Kill(context.Background(), kill.Request{
		RunID: "legacy-run-002",
	})
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if resp.RunID != "run-v2-002" {
		t.Fatalf("run_id=%q want run-v2-002", resp.RunID)
	}
	if !resp.MappedFromLegacy {
		t.Fatal("expected mapped_from_legacy=true")
	}
	if killer.last != "run-v2-002" {
		t.Fatalf("killer target=%q want run-v2-002", killer.last)
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

type fakeEventReader struct {
	events []events.Event
}

func (f *fakeEventReader) ListStream(context.Context, events.StreamFilter) ([]events.Event, error) {
	out := make([]events.Event, len(f.events))
	copy(out, f.events)
	return out, nil
}

type fakeProjectionStore struct {
	byRun      map[string]services.RunState
	lastFilter services.RunStateFilter
}

func newFakeProjectionStore() *fakeProjectionStore {
	return &fakeProjectionStore{
		byRun: map[string]services.RunState{},
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
	state.UpdatedAt = time.Now().UTC()

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
	calls   int
	lastMsg ask.Message
}

func (f *fakeAskDispatcher) Send(_ context.Context, msg ask.Message) (string, error) {
	f.calls++
	f.lastMsg = msg
	return "msg-001", nil
}

type fakeKiller struct {
	last string
}

func (f *fakeKiller) Kill(_ context.Context, runID string) error {
	f.last = runID
	return nil
}

type fakeIDMap struct {
	mapping map[string]string
}

func (f *fakeIDMap) Put(context.Context, string, string, string) error {
	return nil
}

func (f *fakeIDMap) ResolveV2ID(_ context.Context, _ string, legacyID string) (string, error) {
	if id, ok := f.mapping[legacyID]; ok {
		return id, nil
	}
	return "", events.ErrNotFound
}

func sequentialID(prefix string) func() string {
	var seq int
	return func() string {
		seq++
		return prefix + "-" + time.Date(2026, time.February, 18, 0, 0, seq, 0, time.UTC).Format("150405")
	}
}
