package services_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	sysconfig "github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
	"github.com/joshka0/foxctl/internal/v2/services"
	"github.com/joshka0/foxctl/internal/v2/testkit/fakes"
)

func TestNewDefaultRunService_ContextShowUsesRealDefaultExecutor(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	_, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: "foxctl",
		Objective:   "Streamline hooks and ContextWiki parity",
		Phase:       "design",
		UpdatedAt:   time.Date(2026, time.March, 12, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SaveTopOfMind() error = %v", err)
	}

	eventStore := fakes.NewFakeEventStore()
	model := fakes.NewFakeModel(runner.ModelResponse{
		Message: "used the current top of mind",
		ToolCalls: []run.ToolCall{
			{Name: "context/show", Args: json.RawMessage(`{}`)},
		},
		Done: true,
	})
	recorder := &fakeTurnRecorder{}

	svc, err := services.NewDefaultRunService(services.DefaultRuntimeDependencies{
		Profile:       coretool.ProfileCompanion,
		AppConfig:     sysconfig.Config{Storage: sysconfig.StorageSettings{Root: filepath.Join(t.TempDir(), "storage")}},
		WorkspaceRoot: workspace,
		EventStore:    eventStore,
		Model:         model,
		TurnRecorder:  recorder,
		Now: func() time.Time {
			return time.Date(2026, time.March, 12, 11, 5, 0, 0, time.UTC)
		},
		NewID: sequentialID("evt"),
	})
	if err != nil {
		t.Fatalf("NewDefaultRunService() error = %v", err)
	}

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:         "run-v2-default-001",
		RequestID:     "req-v2-default-001",
		CorrelationID: "corr-v2-default-001",
		ActorID:       "actor:companion:test",
		Prompt:        "Show me the current top of mind.",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.ToolCalls != 1 {
		t.Fatalf("tool_calls=%d want 1", out.ToolCalls)
	}
	if len(recorder.turns) != 1 {
		t.Fatalf("saved turns=%d want 1", len(recorder.turns))
	}
	turn := recorder.turns[0]
	if len(turn.Iterations) != 1 || len(turn.Iterations[0].ToolCalls) != 1 {
		t.Fatalf("turn iterations=%d tool_calls=%d want 1/1", len(turn.Iterations), len(turn.Iterations[0].ToolCalls))
	}
	call := turn.Iterations[0].ToolCalls[0]
	if call.Name != "context/show" {
		t.Fatalf("tool name=%q want context/show", call.Name)
	}
	if !strings.Contains(call.ResultRef.Text, `"workspace_id":"foxctl"`) {
		t.Fatalf("tool result=%q missing workspace_id", call.ResultRef.Text)
	}
	if !strings.Contains(call.ResultRef.Text, `"objective":"Streamline hooks and ContextWiki parity"`) {
		t.Fatalf("tool result=%q missing objective", call.ResultRef.Text)
	}
	if eventStore.Count() == 0 {
		t.Fatal("expected runner to append events")
	}
}

type fakeTurnRecorder struct {
	turns []run.TurnRecord
}

func (f *fakeTurnRecorder) SaveTurn(_ context.Context, turn run.TurnRecord) error {
	f.turns = append(f.turns, turn.Clone())
	return nil
}
