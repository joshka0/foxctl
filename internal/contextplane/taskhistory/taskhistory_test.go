package taskhistory

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
)

type fakeGitRunner struct {
	commits map[string][]GitCommit
}

func (f fakeGitRunner) FileHistory(_ context.Context, _ string, filePath string, _ int) ([]GitCommit, error) {
	return append([]GitCommit(nil), f.commits[filePath]...), nil
}

func TestCollectorCollectBuildsPack(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID:  wsID,
		Objective:    "Compact Handoff Pattern",
		Phase:        "design",
		RelevantRefs: []string{"path:internal/contextplane/store.go"},
		UpdatedAt:    time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	if _, err := wsStore.SaveHandoff(contextplane.Handoff{
		TaskID:       "T-1",
		Phase:        "design",
		Outcome:      "partial",
		Summary:      "Collected compact handoff evidence.",
		FilesTouched: []string{"internal/contextplane/store.go"},
		EvidenceRefs: []string{"path:internal/contextplane/store.go"},
		CreatedAt:    time.Date(2026, 3, 14, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	task, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "T-1",
		WorkspaceID: wsID,
		Title:       "Compact Handoff Pattern",
		Description: "Trace the compact handoff implementation.",
		ScopePath:   "internal/contextplane/store.go",
		Status:      taskstore.StatusInProgress,
		SessionID:   "sess-1",
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	sessionDB, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open sessions: %v", err)
	}
	defer sessionDB.Close()
	if _, err := sessionDB.Save(ctx, storage.Session{
		ID:            "sess-1",
		WorkspaceID:   wsID,
		WorkspacePath: workspacePath,
		ProjectName:   "agentctl",
		Summary:       "Worked on the compact handoff flow.",
		Decisions:     []string{"Prefer a bounded handoff envelope."},
		Gotchas:       []string{"Do not inline large restore context."},
		KeyFiles:      []string{"internal/contextplane/store.go"},
		StartedAt:     time.Date(2026, 3, 14, 0, 30, 0, 0, time.UTC),
		EndedAt:       time.Date(2026, 3, 14, 1, 30, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Save session: %v", err)
	}

	repo, err := repoindex.Open(ctx, storageRoot, workspacePath)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	defer repo.Close()
	repoKey := repo.RepoKey()
	if err := repo.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:      repoindex.FileID(repoKey, "internal/contextplane", "internal/contextplane/store.go"),
			Kind:    repoindex.NodeFile,
			Pkg:     "internal/contextplane",
			File:    "internal/contextplane/store.go",
			Name:    "store.go",
			Summary: "Workspace ACA store implementation.",
		},
		{
			ID:      repoindex.NamespacedID(repoKey, "concept:compact-handoff-pattern"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "internal/contextplane",
			File:    "internal/contextplane/store.go",
			Name:    "Compact Handoff Pattern",
			Summary: "Compact handoff pattern for ACA.",
		},
	}, nil); err != nil {
		t.Fatalf("ReplaceAll repoindex: %v", err)
	}

	vaultRoot := retrievalFixtureVaultRoot(t)
	index, err := obsidianindex.Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("open obsidian index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, vaultRoot); err != nil {
		t.Fatalf("rebuild obsidian index: %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
		SessionStore:   sessionDB,
		RepoStore:      repo,
		VaultIndex:     index,
		GitRunner: fakeGitRunner{
			commits: map[string][]GitCommit{
				"internal/contextplane/store.go": {{
					Hash:    "abc123",
					Date:    "2026-03-14",
					Subject: "refine compact handoff storage",
				}},
			},
		},
	}

	pack, err := collector.Collect(ctx, Options{
		WorkspacePath:  workspacePath,
		WorkspaceID:    wsID,
		TaskID:         task.ID,
		SessionLimit:   3,
		HandoffLimit:   5,
		FileLimit:      5,
		GitCommitLimit: 3,
		AnchorLimit:    5,
		NoteLimit:      3,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if pack.Task.ID != "T-1" {
		t.Fatalf("task id=%q", pack.Task.ID)
	}
	if len(pack.Handoffs) != 1 {
		t.Fatalf("handoffs=%d", len(pack.Handoffs))
	}
	if len(pack.FilesTouched) == 0 || pack.FilesTouched[0] != "internal/contextplane/store.go" {
		t.Fatalf("files=%v", pack.FilesTouched)
	}
	if len(pack.Sessions) == 0 || pack.Sessions[0].ID != "sess-1" {
		t.Fatalf("sessions=%v", pack.Sessions)
	}
	if len(pack.GitHistory) == 0 || pack.GitHistory[0].Path != "internal/contextplane/store.go" {
		t.Fatalf("git history=%v", pack.GitHistory)
	}
	if len(pack.RepoAnchors) == 0 {
		t.Fatalf("repo anchors empty")
	}
	if len(pack.DAGAnchors) == 0 {
		t.Fatalf("dag anchors empty")
	}
	if len(pack.ACANotes) == 0 {
		t.Fatalf("aca notes empty")
	}
	if pack.Summary == "" {
		t.Fatalf("summary empty")
	}
}

func TestCollectorPrefersInRepoTaskWhenTaskIDOmitted(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Current repo work",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()

	if _, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "external-task",
		WorkspaceID: wsID,
		Title:       "Write /Users/joshka/.claude/plans/example.md",
		ScopePath:   "/Users/joshka/.claude/plans/example.md",
		Status:      taskstore.StatusInProgress,
		CreatedAt:   time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Add external task: %v", err)
	}
	if _, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "repo-task",
		WorkspaceID: wsID,
		Title:       "Refine ACA retrieval",
		ScopePath:   "internal/contextplane/retrieval.go",
		Status:      taskstore.StatusPending,
		CreatedAt:   time.Date(2026, 3, 14, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Add repo task: %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
	}
	taskID, err := collector.selectTaskID(ctx, Options{
		WorkspacePath: workspacePath,
		WorkspaceID:   wsID,
	})
	if err != nil {
		t.Fatalf("selectTaskID: %v", err)
	}
	if taskID != "repo-task" {
		t.Fatalf("taskID=%q want repo-task", taskID)
	}
}

func retrievalFixtureVaultRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "tools", "obsidian", "testdata", "vaults", "basic"))
}
