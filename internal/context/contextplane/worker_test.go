package contextplane

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestWorkerRunOnce(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: storageRoot}}

	tasksDB, err := taskstore.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("Open tasks: %v", err)
	}
	defer tasksDB.Close()
	if _, err := tasksDB.Add(context.Background(), taskstore.Task{
		WorkspaceID: ws.ID(workspace),
		Title:       "Formalize ACA",
		Status:      taskstore.StatusPending,
		ScopePath:   "internal/context/contextplane",
	}); err != nil {
		t.Fatalf("Add task: %v", err)
	}

	store := NewWorkspaceStore(workspace)
	if _, err := store.AppendTension(Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "high",
		Status:      "open",
		Count:       2,
		RelatedRefs: []contextengine.EvidenceRef{{Type: contextengine.RefTypeNote, Ref: "write-policy"}},
		CreatedAt:   time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}

	worker := NewWorker(WorkerConfig{
		Config:    cfg,
		Workspace: workspace,
	})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	top, err := store.LoadTopOfMind()
	if err != nil {
		t.Fatalf("LoadTopOfMind: %v", err)
	}
	if top.Objective == "" {
		t.Fatalf("expected non-empty objective")
	}
	tasks, err := store.ListMaintenanceTasks(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListMaintenanceTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("expected maintenance tasks")
	}
}

func TestWorkerRunOnceGeneratesProposalMergeMaintenanceTasks(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: storageRoot}}
	store := NewWorkspaceStore(workspace)
	if _, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-vocabulary",
		Kind:           PolicyKindExternalImport,
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "medium",
		Summary:        "Review imported evidence draft for merge consideration: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	worker := NewWorker(WorkerConfig{
		Config:    cfg,
		Workspace: workspace,
	})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	tasks, err := store.ListMaintenanceTasks(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListMaintenanceTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d want 1", len(tasks))
	}
	if tasks[0].Kind != "proposal_merge" || tasks[0].WorkPacket == nil {
		t.Fatalf("unexpected task=%+v", tasks[0])
	}
}

func TestWorkerRunOnceWithVaultHealth(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: storageRoot}}
	vaultRoot := filepath.Clean(filepath.Join(repoRootForWorkerTest(t), "..", "..", "tooling", "tools", "obsidian", "testdata", "vaults", "basic"))

	worker := NewWorker(WorkerConfig{
		Config:    cfg,
		Workspace: workspace,
		VaultPath: vaultRoot,
	})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce with vault: %v", err)
	}

	store := NewWorkspaceStore(workspace)
	tasks, err := store.ListMaintenanceTasks(context.Background(), 50)
	if err != nil {
		t.Fatalf("ListMaintenanceTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("expected health-derived maintenance tasks")
	}
}

func repoRootForWorkerTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Dir(file)
}
