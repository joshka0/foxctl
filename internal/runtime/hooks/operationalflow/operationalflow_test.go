package operationalflow

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runtime/hooks/lifecycle"
)

func TestIndexEditedFileSkipsUnsupportedPaths(t *testing.T) {
	resp, err := IndexEditedFile(context.Background(), Dependencies{}, LiveIndexRequest{
		Workspace: "/tmp/workspace",
		Payload: LiveIndexPayload{
			ToolInput: struct {
				FilePath string `json:"file_path,omitempty"`
				Path     string `json:"path,omitempty"`
			}{FilePath: "README.md"},
		},
	})
	if err != nil {
		t.Fatalf("IndexEditedFile: %v", err)
	}
	if resp.Context != "" {
		t.Fatalf("expected empty context, got %q", resp.Context)
	}
}

func TestIndexEditedFileBuildsContext(t *testing.T) {
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			if skill != "code/incremental_index" {
				t.Fatalf("unexpected skill %s", skill)
			}
			target := out.(*incrementalIndexEnvelope)
			target.Data.SymbolsUpdated = 4
			target.Data.SymbolsDeleted = 1
			target.Data.EmbeddingQueued = 2
			target.Data.DurationMS = 18
			return nil
		},
	}

	resp, err := IndexEditedFile(context.Background(), deps, LiveIndexRequest{
		Workspace: "/tmp/workspace",
		Payload: LiveIndexPayload{
			ToolInput: struct {
				FilePath string `json:"file_path,omitempty"`
				Path     string `json:"path,omitempty"`
			}{FilePath: "internal/runtime/hooks/runtime.go"},
		},
	})
	if err != nil {
		t.Fatalf("IndexEditedFile: %v", err)
	}
	if !strings.Contains(resp.Context, "Indexed **4** symbols (+1 removed)") {
		t.Fatalf("expected index summary, got %q", resp.Context)
	}
	if !strings.Contains(resp.Context, "Queued **2** for embedding") {
		t.Fatalf("expected embedding queue info, got %q", resp.Context)
	}
}

func TestDiagnoseEditedFileFormatsGoDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	filePath := tmp + "/main.go"
	if err := osWriteFile(filePath, "package main\n"); err != nil {
		t.Fatalf("write file: %v", err)
	}
	deps := Dependencies{
		LookPath: func(name string) (string, error) {
			if name == "gopls" {
				return "/usr/bin/gopls", nil
			}
			return "", errors.New("missing")
		},
		ExecCmd: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			return filePath + ":7:14-16: undefined: os\n", errors.New("exit 1")
		},
	}

	resp, err := DiagnoseEditedFile(context.Background(), deps, LSPDiagnosticsRequest{
		Workspace: tmp,
		Payload: LSPDiagnosticsPayload{
			ToolInput: struct {
				FilePath string `json:"file_path,omitempty"`
				Path     string `json:"path,omitempty"`
			}{FilePath: "main.go"},
		},
	})
	if err != nil {
		t.Fatalf("DiagnoseEditedFile: %v", err)
	}
	if len(resp.Diagnostics) != 1 || !strings.Contains(resp.Diagnostics[0], "undefined: os") {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
}

func TestMaintainGraphSyncRunsCleanupAndPagerank(t *testing.T) {
	calls := make([]string, 0, 3)
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			calls = append(calls, skill)
			switch skill {
			case "graph/manage":
				target := out.(*graphCleanupEnvelope)
				target.Data.Result.ExpiredEdgesRemoved = 3
				target.Data.Result.DanglingEdgesRemoved = 2
				target.Data.Result.DegreesRecalculated = true
			case "graph/pagerank":
				target := out.(*graphPageRankEnvelope)
				target.Data.NodesUpdated = 8
				target.Data.EdgesProcessed = 21
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
	}

	resp, err := MaintainGraphSync(context.Background(), deps, GraphMaintenanceRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("MaintainGraphSync: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 skill calls, got %d", len(calls))
	}
	if !resp.CleanupRan || !resp.DegreeRepairRan || !resp.PageRankRan {
		t.Fatalf("expected all maintenance stages to run, got %#v", resp)
	}
}

func TestFlushEmbeddingsUsesConfiguredAPIKey(t *testing.T) {
	deps := Dependencies{
		Config: config.Config{
			Embedding: config.EmbeddingSettings{APIKey: "test-key"},
		},
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			switch skill {
			case "embedding/queue":
				target := out.(*embeddingQueueStatsEnvelope)
				target.Data.Stats.QueuedCount = 3
			case "embedding/worker":
				target := out.(*embeddingWorkerEnvelope)
				target.Data.Processed = 3
				target.Data.Remaining = 0
				target.Data.Status = "completed"
				target.Data.Message = "processed"
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
	}

	resp, err := FlushEmbeddings(context.Background(), deps, EmbeddingFlushRequest{Workspace: "/tmp/workspace"})
	if err != nil {
		t.Fatalf("FlushEmbeddings: %v", err)
	}
	if resp.Skipped {
		t.Fatalf("expected worker to run, got %#v", resp)
	}
	if resp.Processed != 3 || resp.Status != "completed" {
		t.Fatalf("unexpected flush response: %#v", resp)
	}
}

func TestSyncPlansReturnsSummary(t *testing.T) {
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			if skill != "plan/sync" {
				t.Fatalf("unexpected skill %s", skill)
			}
			target := out.(*planSyncEnvelope)
			target.Data.PlansProcessed = 2
			target.Data.PlansChanged = 1
			target.Data.TasksCreated = 0
			target.Data.Provider = "claude"
			target.Data.Message = "Synced 2 claude plans"
			return nil
		},
	}

	resp, err := SyncPlans(context.Background(), deps, PlanSyncRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("SyncPlans: %v", err)
	}
	if resp.PlansProcessed != 2 || resp.Provider != "claude" {
		t.Fatalf("unexpected plan sync response: %#v", resp)
	}
}

func TestMaintainGraphSyncKeepsWarningsNonFatal(t *testing.T) {
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			if skill == "graph/pagerank" {
				return errors.New("pagerank unavailable")
			}
			return nil
		},
	}

	resp, err := MaintainGraphSync(context.Background(), deps, GraphMaintenanceRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("MaintainGraphSync: %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Fatalf("expected warning when pagerank fails")
	}
}

func TestNewDependenciesCopiesLifecycleRunnerAndConfig(t *testing.T) {
	cfg := config.Config{Embedding: config.EmbeddingSettings{Provider: "voyage"}}
	life := lifecycle.Dependencies{}
	deps := NewDependencies(cfg, life)
	if deps.RunSkill != nil {
		t.Fatalf("expected nil run skill copy, got %#v", deps.RunSkill)
	}
	if deps.Config.Embedding.Provider != "voyage" {
		t.Fatalf("provider=%q want voyage", deps.Config.Embedding.Provider)
	}
}

func osWriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
