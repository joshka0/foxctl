package contextplane

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

type fakeTaskProvider struct {
	active tasks.Task
	has    bool
	items  []tasks.Task
}

func (f fakeTaskProvider) GetActive(context.Context, string) (tasks.Task, bool, error) {
	return f.active, f.has, nil
}

func (f fakeTaskProvider) ListWithOptions(context.Context, string, tasks.ListOptions) ([]tasks.Task, error) {
	return append([]tasks.Task(nil), f.items...), nil
}

type fakeSessionProvider struct {
	items []storage.Session
}

func (f fakeSessionProvider) List(context.Context, storage.SessionListOptions) ([]storage.Session, error) {
	return append([]storage.Session(nil), f.items...), nil
}

func TestOrienterBuildUsesTasksAndSessions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	active := tasks.Task{
		ID:        "T-1042",
		Title:     "Write ACA v0.1",
		Status:    tasks.StatusInProgress,
		Gotchas:   "Preserve provenance; keep the vault human-readable",
		PlanFile:  "docs/plans/context-architecture.md",
		ScopePath: "internal/contextplane",
	}
	taskItems := []tasks.Task{
		active,
		{ID: "T-2001", Title: "Draft ADR-0001", Status: tasks.StatusPending},
		{ID: "T-2002", Title: "Resolve vault write policy", Status: tasks.StatusBlocked},
	}
	sessionItems := []storage.Session{
		{
			ID:           "S-1",
			Summary:      "Defined planes and promotion rules.",
			Status:       storage.SessionStatusOK,
			EndedAt:      time.Date(2026, 3, 9, 12, 5, 0, 0, time.UTC),
			Decisions:    []string{"Top-of-mind is a computed attention cache"},
			KeyQuestions: []string{"Define promotion thresholds"},
			KeyFiles:     []string{"docs/architecture/system-architecture.md"},
		},
	}

	orienter := NewOrienter(
		fakeTaskProvider{active: active, has: true, items: taskItems},
		fakeSessionProvider{items: sessionItems},
	)
	top, err := orienter.Build(ctx, root)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if top.Objective != "Write ACA v0.1" {
		t.Fatalf("objective=%q", top.Objective)
	}
	if top.Phase != "execute" {
		t.Fatalf("phase=%q", top.Phase)
	}
	if len(top.ActiveTaskIDs) != 1 || top.ActiveTaskIDs[0] != "T-1042" {
		t.Fatalf("active task ids=%v", top.ActiveTaskIDs)
	}
	if len(top.Blockers) != 1 || top.Blockers[0] != "Resolve vault write policy" {
		t.Fatalf("blockers=%v", top.Blockers)
	}
	if len(top.RecentDecisions) != 1 || top.RecentDecisions[0].Text == "" {
		t.Fatalf("decisions=%v", top.RecentDecisions)
	}
	if len(top.NextActions) == 0 {
		t.Fatalf("expected next actions")
	}
	if len(top.RelevantRefs) == 0 {
		t.Fatalf("expected relevant refs")
	}
}
