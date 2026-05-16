package runner

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestPipeline_RLMREPLBackendUsesRLMRunner(t *testing.T) {
	t.Parallel()

	store := &fakeEventStore{}
	clock := &fakeClock{
		current: time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC),
		step:    time.Second,
	}
	ids := &fakeUUID{prefix: "evt"}
	factory := &fakeRLMREPLFactory{
		runner: &fakeRLMRunner{
			result: rlm.Result{
				Answer:     "solution = []",
				Iterations: 3,
				Metadata: map[string]any{
					"tool_calls": 2,
					"runner":     "rlm_repl_no_subcalls",
				},
			},
		},
	}

	p := New(Config{
		EventStore:     store,
		RLMREPLFactory: factory,
		Now:            clock.Now,
		NewID:          ids.New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-rlm",
		TurnID:        "turn-rlm",
		Backend:       run.TurnBackendRLMREPL,
		Prompt:        "solve with repl",
		ActorID:       "agent-main",
		MaxIterations: 7,
		RLM: run.RLMREPLConfig{
			WorkspaceID:   "workspace-1",
			WorkspaceRoot: "/tmp/workspace",
			OutputRoot:    "/tmp/out",
			Budget: run.RLMREPLBudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   0,
				MaxIterations: 5,
			},
		},
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.Summary != "solution = []" {
		t.Fatalf("summary = %q", out.Summary)
	}
	if out.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", out.Iterations)
	}
	if out.ToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2", out.ToolCalls)
	}
	if out.Metadata["runner"] != "rlm_repl_no_subcalls" {
		t.Fatalf("metadata = %#v", out.Metadata)
	}
	if len(factory.calls) != 1 {
		t.Fatalf("factory calls = %d, want 1", len(factory.calls))
	}
	if factory.runner.task.Prompt != "solve with repl" {
		t.Fatalf("task prompt = %q", factory.runner.task.Prompt)
	}
	if factory.runner.task.MaxIterations != 5 {
		t.Fatalf("task max iterations = %d, want 5", factory.runner.task.MaxIterations)
	}
	if factory.runner.task.WorkspaceRoot != "/tmp/workspace" {
		t.Fatalf("task workspace root = %q", factory.runner.task.WorkspaceRoot)
	}
	if factory.runner.task.OutputRoot != "/tmp/out" {
		t.Fatalf("task output root = %q", factory.runner.task.OutputRoot)
	}
	if factory.runner.task.RunID != "run-rlm" {
		t.Fatalf("task run id = %q", factory.runner.task.RunID)
	}
	if factory.runner.task.AgentID != "agent-main" {
		t.Fatalf("task agent id = %q", factory.runner.task.AgentID)
	}
	assertEventTypes(
		t, store.Events(),
		events.EventRunStarted,
		events.EventTurnRecorded,
		events.EventRunCompleted,
	)
}

func TestPipeline_RLMREPLBackendDoesNotRequireModelOrToolExecutor(t *testing.T) {
	t.Parallel()

	store := &fakeEventStore{}
	factory := &fakeRLMREPLFactory{
		runner: &fakeRLMRunner{result: rlm.Result{Answer: "done"}},
	}
	p := New(Config{
		EventStore:     store,
		RLMREPLFactory: factory,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:   "run-rlm-no-model",
		Backend: run.TurnBackendRLMREPL,
		Prompt:  "solve",
	})
	if err != nil {
		t.Fatalf("RunTurn returned error without model/tool executor: %v", err)
	}
}

func TestPipeline_RLMREPLBackendPropagatesRunnerError(t *testing.T) {
	t.Parallel()

	store := &fakeEventStore{}
	factory := &fakeRLMREPLFactory{
		runner: &fakeRLMRunner{err: errors.New("rlm failed")},
	}
	p := New(Config{
		EventStore:     store,
		RLMREPLFactory: factory,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:   "run-rlm-error",
		Backend: run.TurnBackendRLMREPL,
		Prompt:  "solve",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultRLMREPLRunnerFactory_WiresRecursiveRLMQueryFactory(t *testing.T) {
	t.Parallel()

	var built []rlmruntime.REPLRunnerConfig
	runners := make([]*fakeRLMRunner, 0, 2)
	factory := defaultRLMREPLRunnerFactory{
		runnerBuilder: func(cfg rlmruntime.REPLRunnerConfig) RLMRunner {
			built = append(built, cfg)
			next := &fakeRLMRunner{
				result: rlm.Result{
					Answer: "child",
					Metadata: map[string]any{
						"runner": "rlm_repl_with_subcalls",
					},
				},
			}
			runners = append(runners, next)
			return next
		},
	}

	baseCfg := run.RLMREPLConfig{
		LLM: run.RLMREPLLLMConfig{
			Provider:      "openrouter",
			Model:         "openrouter/aurora-alpha",
			MaxIterations: 9,
		},
		Budget: run.RLMREPLBudgetConfig{
			MaxDepth:      3,
			MaxSubcalls:   4,
			MaxIterations: 9,
		},
	}

	_, err := factory.New(baseCfg)
	if err != nil {
		t.Fatalf("factory.New() error = %v", err)
	}
	if len(built) != 1 {
		t.Fatalf("builder calls after New = %d, want 1", len(built))
	}
	rootCfg := built[0]
	if rootCfg.RLMQueryFactory == nil {
		t.Fatal("expected RLMQueryFactory to be wired")
	}
	assertRecursiveModeMatchesBudget(t, rootCfg)

	queryRun := rootCfg.RLMQueryFactory(rlm.Task{Prompt: "parent"}, rlm.Environment{})
	if queryRun == nil {
		t.Fatal("expected recursive rlm_query run function")
	}

	childTask := rlm.Task{
		Prompt:        "child",
		MaxDepth:      2,
		MaxSubcalls:   3,
		MaxIterations: 5,
	}
	childEnv := rlm.Environment{
		Tools: []rlm.Tool{{Name: "python_repl"}},
	}
	result, err := queryRun(context.Background(), childTask, childEnv)
	if err != nil {
		t.Fatalf("queryRun() error = %v", err)
	}
	if result.Answer != "child" {
		t.Fatalf("child result answer = %q, want child", result.Answer)
	}

	if len(built) != 2 {
		t.Fatalf("builder calls after child run = %d, want 2", len(built))
	}
	childCfg := built[1]
	if childCfg.Budget.MaxDepth != childTask.MaxDepth || childCfg.Budget.MaxSubcalls != childTask.MaxSubcalls {
		t.Fatalf("child config budgets not narrowed: root=%+v child=%+v", rootCfg.Budget, childCfg.Budget)
	}
	if childCfg.Budget.MaxIterations != childTask.MaxIterations || childCfg.LLM.MaxIterations != childTask.MaxIterations {
		t.Fatalf("child iteration budgets not narrowed: budget=%d llm=%d want %d", childCfg.Budget.MaxIterations, childCfg.LLM.MaxIterations, childTask.MaxIterations)
	}
	if childCfg.RLMQueryFactory == nil {
		t.Fatal("expected child config to keep recursive factory")
	}
	if childCfg.LLM.RequireToolUse {
		t.Fatal("expected child config to allow direct child answers")
	}
	assertRecursiveModeMatchesBudget(t, childCfg)

	if len(runners) != 2 {
		t.Fatalf("runner instances = %d, want 2", len(runners))
	}
	if runners[1].task.MaxDepth != childTask.MaxDepth || runners[1].task.MaxSubcalls != childTask.MaxSubcalls {
		t.Fatalf("child task budget forwarding mismatch: got depth=%d subcalls=%d", runners[1].task.MaxDepth, runners[1].task.MaxSubcalls)
	}
	if runners[1].env.Tools[0].Name != "python_repl" {
		t.Fatalf("child env not forwarded: %+v", runners[1].env)
	}

	_, err = queryRun(context.Background(), rlm.Task{Prompt: "child-no-subcalls", MaxSubcalls: 0}, childEnv)
	if err != nil {
		t.Fatalf("queryRun() with MaxSubcalls=0 error = %v", err)
	}
	if len(built) != 3 {
		t.Fatalf("builder calls after second child run = %d, want 3", len(built))
	}
	assertRecursiveModeMatchesBudget(t, built[2])
}

func TestMapRLMREPLConfigSelectsYaegiSandbox(t *testing.T) {
	t.Parallel()

	cfg := mapRLMREPLConfig(run.RLMREPLConfig{
		Sandbox: run.RLMREPLSandboxConfig{
			Kind: "yaegi",
			Yaegi: run.RLMREPLYaegiConfig{
				MaxOutputBytes: 1234,
			},
		},
	})

	if cfg.Sandbox.Kind != rlmruntime.SandboxKindYaegi {
		t.Fatalf("sandbox kind = %q, want %q", cfg.Sandbox.Kind, rlmruntime.SandboxKindYaegi)
	}
	if cfg.Sandbox.Yaegi.MaxOutputBytes != 1234 {
		t.Fatalf("yaegi max output = %d, want 1234", cfg.Sandbox.Yaegi.MaxOutputBytes)
	}
}

func TestMapRLMREPLConfigPreservesLegacyPythonConfig(t *testing.T) {
	t.Parallel()

	cfg := mapRLMREPLConfig(run.RLMREPLConfig{
		Python: run.RLMREPLPythonConfig{
			PythonPath:     "/usr/bin/python3",
			MaxOutputBytes: 4321,
		},
	})

	if cfg.Sandbox.Kind != "" {
		t.Fatalf("sandbox kind = %q, want default", cfg.Sandbox.Kind)
	}
	if cfg.Sandbox.Python.PythonPath != "/usr/bin/python3" {
		t.Fatalf("python path = %q", cfg.Sandbox.Python.PythonPath)
	}
	if cfg.Sandbox.Python.MaxOutputBytes != 4321 {
		t.Fatalf("python max output = %d, want 4321", cfg.Sandbox.Python.MaxOutputBytes)
	}
}

type fakeRLMREPLFactory struct {
	calls  []run.RLMREPLConfig
	runner *fakeRLMRunner
	err    error
}

func (f *fakeRLMREPLFactory) New(cfg run.RLMREPLConfig) (RLMRunner, error) {
	f.calls = append(f.calls, cfg)
	if f.err != nil {
		return nil, f.err
	}
	return f.runner, nil
}

type fakeEventStore struct {
	events []events.Event
}

func (s *fakeEventStore) Append(_ context.Context, event events.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *fakeEventStore) Events() []events.Event {
	out := make([]events.Event, len(s.events))
	copy(out, s.events)
	return out
}

type fakeClock struct {
	current time.Time
	step    time.Duration
}

func (c *fakeClock) Now() time.Time {
	out := c.current
	c.current = c.current.Add(c.step)
	return out
}

type fakeUUID struct {
	prefix  string
	counter int
}

func (u *fakeUUID) New() string {
	u.counter++
	return u.prefix + "-" + strconv.Itoa(u.counter)
}

type fakeRLMRunner struct {
	task   rlm.Task
	env    rlm.Environment
	result rlm.Result
	err    error
}

func (r *fakeRLMRunner) Run(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
	r.task = task
	r.env = env
	if r.err != nil {
		return rlm.Result{}, r.err
	}
	return r.result, nil
}

func assertEventTypes(t *testing.T, list []events.Event, want ...events.EventType) {
	t.Helper()
	if len(list) != len(want) {
		t.Fatalf("events len=%d want %d", len(list), len(want))
	}
	for i, evt := range list {
		if evt.EventType != want[i] {
			t.Fatalf("event[%d]=%s want %s", i, evt.EventType, want[i])
		}
	}
}

func assertRecursiveModeMatchesBudget(t *testing.T, cfg rlmruntime.REPLRunnerConfig) {
	t.Helper()

	wantAsync := recursiveSubcallsEnabled(cfg.Budget)
	if cfg.AsyncRecursion != wantAsync {
		t.Fatalf("AsyncRecursion = %t, want %t", cfg.AsyncRecursion, wantAsync)
	}
}
