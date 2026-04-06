package workers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
	runtimeworkers "github.com/jkatigb/agentctl/internal/v2/runtime/workers"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(context.Background(), filepath.Join(t.TempDir(), "workers.db"), nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(context.Background(), db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	return NewStore(db)
}

func TestStore_UpsertAndLookupByWorkerAndAgent(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	now := time.Date(2026, time.April, 6, 17, 0, 0, 0, time.UTC)
	record := coreworker.Record{
		WorkerID:         "worker-1",
		BackendKind:      coreworker.BackendSubprocess,
		BackendWorkerRef: "pid:1234",
		AgentID:          "agent:1",
		RunID:            "run-1",
		SessionID:        "sess-1",
		WorkspaceID:      "ws-1",
		Role:             "worker",
		Status:           coreworker.StatusRunning,
		Tag:              "tag-1",
		PID:              "1234",
		StartedAt:        now,
		UpdatedAt:        now,
		HeartbeatAt:      now,
		Metadata:         map[string]any{"lane": "review"},
		RawState:         []byte(`{"status":"running"}`),
	}
	if err := store.Upsert(context.Background(), record); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := store.Worker(context.Background(), coreworker.LookupRequest{WorkerID: "worker-1"})
	if err != nil {
		t.Fatalf("Worker(worker_id) error = %v", err)
	}
	if got.AgentID != "agent:1" {
		t.Fatalf("agent_id=%q want agent:1", got.AgentID)
	}
	if got.Status != coreworker.StatusRunning {
		t.Fatalf("status=%q want %q", got.Status, coreworker.StatusRunning)
	}
	if string(got.RawState) != `{"status":"running"}` {
		t.Fatalf("raw_state=%s", string(got.RawState))
	}

	gotByAgent, err := store.Worker(context.Background(), coreworker.LookupRequest{AgentID: "agent:1"})
	if err != nil {
		t.Fatalf("Worker(agent_id) error = %v", err)
	}
	if gotByAgent.WorkerID != "worker-1" {
		t.Fatalf("worker_id=%q want worker-1", gotByAgent.WorkerID)
	}
}

func TestStore_ChildrenOrdersByWorkerID(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	for _, record := range []coreworker.Record{
		{WorkerID: "worker-b", BackendKind: coreworker.BackendJido, AgentID: "agent:b", ParentAgentID: "agent:parent", Status: coreworker.StatusRunning},
		{WorkerID: "worker-a", BackendKind: coreworker.BackendJido, AgentID: "agent:a", ParentAgentID: "agent:parent", Status: coreworker.StatusStarting},
	} {
		if err := store.Upsert(context.Background(), record); err != nil {
			t.Fatalf("Upsert(%s): %v", record.WorkerID, err)
		}
	}

	children, err := store.Children(context.Background(), coreworker.ChildrenRequest{ParentAgentID: "agent:parent"})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("children len=%d want 2", len(children))
	}
	if children[0].WorkerID != "worker-a" || children[1].WorkerID != "worker-b" {
		t.Fatalf("order=%q,%q want worker-a,worker-b", children[0].WorkerID, children[1].WorkerID)
	}
}

func TestStore_ActiveFiltersToLiveBackendWorkers(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	for _, record := range []coreworker.Record{
		{WorkerID: "worker-running", BackendKind: coreworker.BackendSubprocess, AgentID: "agent:running", Status: coreworker.StatusRunning},
		{WorkerID: "worker-starting", BackendKind: coreworker.BackendSubprocess, AgentID: "agent:starting", Status: coreworker.StatusStarting},
		{WorkerID: "worker-stopping", BackendKind: coreworker.BackendSubprocess, AgentID: "agent:stopping", Status: coreworker.StatusStopping},
		{WorkerID: "worker-completed", BackendKind: coreworker.BackendSubprocess, AgentID: "agent:completed", Status: coreworker.StatusCompleted},
		{WorkerID: "worker-jido", BackendKind: coreworker.BackendJido, AgentID: "agent:jido", Status: coreworker.StatusRunning},
	} {
		if err := store.Upsert(context.Background(), record); err != nil {
			t.Fatalf("Upsert(%s): %v", record.WorkerID, err)
		}
	}

	active, err := store.Active(context.Background(), coreworker.BackendSubprocess)
	if err != nil {
		t.Fatalf("Active() error = %v", err)
	}
	if len(active) != 3 {
		t.Fatalf("active len=%d want 3", len(active))
	}
	got := map[string]bool{}
	for _, record := range active {
		got[record.WorkerID] = true
	}
	for _, want := range []string{"worker-running", "worker-starting", "worker-stopping"} {
		if !got[want] {
			t.Fatalf("active missing %q: %+v", want, got)
		}
	}
	if got["worker-completed"] || got["worker-jido"] {
		t.Fatalf("active includes unexpected workers: %+v", got)
	}
}

func TestStateComponent_PersistsAppliedWorkerState(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	component := runtimeworkers.NewStateComponent(runtimeworkers.Config{
		Buffer:   8,
		Registry: store,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	now := time.Date(2026, time.April, 6, 17, 10, 0, 0, time.UTC)
	err := component.Publish(context.Background(), coreworker.LifecycleEvent{
		EventKind:     coreworker.EventWorkerSpawned,
		ObservedAt:    now,
		WorkerID:      "worker-persist-1",
		BackendKind:   coreworker.BackendSubprocess,
		AgentID:       "agent:persist-1",
		ParentAgentID: "agent:parent",
		Status:        coreworker.StatusStarting,
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	waitForPersistedWorker(t, store, "worker-persist-1")

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func waitForPersistedWorker(t *testing.T, store *Store, workerID string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		record, err := store.Worker(context.Background(), coreworker.LookupRequest{WorkerID: workerID})
		if err == nil && record.WorkerID == workerID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker %q not persisted", workerID)
}
