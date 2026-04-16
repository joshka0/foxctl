package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
)

func TestMigrateSchemaCreatesCoreTables(t *testing.T) {
	ctx := context.Background()
	db, closeFn := openTestDB(t, ctx)
	defer func() { _ = closeFn() }()

	for _, table := range []string{
		"evolve_runs",
		"evolve_active_runs",
		"evolve_nodes",
		"evolve_gates",
		"evolve_attempts",
		"evolve_gate_results",
		"evolve_annotations",
		"evolve_infra_events",
	} {
		if !tableExists(t, db, table) {
			t.Fatalf("expected table %s to exist", table)
		}
	}

	for _, index := range []string{
		"idx_evolve_runs_workspace",
		"idx_evolve_nodes_run_parent",
		"idx_evolve_attempts_node_no",
		"idx_evolve_infra_events_run",
	} {
		if !indexExists(t, db, index) {
			t.Fatalf("expected index %s to exist", index)
		}
	}
}

func TestMigrateSchemaIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, closeFn := openTestDB(t, ctx)
	defer func() { _ = closeFn() }()

	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("third migrate: %v", err)
	}
}

func TestStorePersistsRunGraphAndArtifacts(t *testing.T) {
	ctx := context.Background()
	db, closeFn := openTestDB(t, ctx)
	defer func() { _ = closeFn() }()
	store := NewSQLStore(db, nil)
	now := time.Date(2026, 4, 16, 6, 45, 0, 0, time.UTC)

	run := model.Run{
		ID:               "run-1",
		WorkspacePath:    "/repo",
		TargetPath:       "/repo/pkg",
		BenchmarkCommand: "go test ./...",
		Metric:           model.MetricMax,
		Status:           model.RunStatusActive,
		Active:           true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	active, ok, err := store.ActiveRun(ctx, "/repo")
	if err != nil {
		t.Fatalf("active run: %v", err)
	}
	if !ok || active.ID != "run-1" || !active.Active {
		t.Fatalf("active run = %#v, ok=%v", active, ok)
	}

	root := model.Node{
		ID:        "node-root",
		RunID:     run.ID,
		Status:    model.NodeStatusRoot,
		Branch:    "main",
		CommitSHA: "abc123",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveNode(ctx, root); err != nil {
		t.Fatalf("save root node: %v", err)
	}

	score := 12.5
	child := model.Node{
		ID:             "node-child",
		RunID:          run.ID,
		ParentID:       root.ID,
		Status:         model.NodeStatusPending,
		Hypothesis:     "Improve benchmark",
		Score:          &score,
		Branch:         "foxctl/evolve/run-1/node-child",
		WorktreePath:   "/tmp/node-child",
		CurrentAttempt: 1,
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}
	if err := store.SaveNode(ctx, child); err != nil {
		t.Fatalf("save child node: %v", err)
	}

	children, err := store.ChildNodes(ctx, run.ID, root.ID)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(children) != 1 || children[0].ID != child.ID || children[0].Score == nil || *children[0].Score != score {
		t.Fatalf("children = %#v", children)
	}

	frontier, err := store.FrontierNodes(ctx, run.ID)
	if err != nil {
		t.Fatalf("frontier: %v", err)
	}
	if len(frontier) != 1 || frontier[0].ID != child.ID {
		t.Fatalf("frontier = %#v", frontier)
	}

	gate := model.Gate{
		ID:        "gate-1",
		RunID:     run.ID,
		NodeID:    root.ID,
		Name:      "unit",
		Command:   "go test ./...",
		CreatedAt: now,
	}
	if err := store.SaveGate(ctx, gate); err != nil {
		t.Fatalf("save gate: %v", err)
	}
	gates, err := store.GatesByNode(ctx, root.ID)
	if err != nil {
		t.Fatalf("gates by node: %v", err)
	}
	if len(gates) != 1 || gates[0].Name != "unit" {
		t.Fatalf("gates = %#v", gates)
	}

	attempt := model.Attempt{
		ID:                "attempt-1",
		NodeID:            child.ID,
		AttemptNo:         1,
		Status:            model.AttemptStatusCompleted,
		Score:             &score,
		BenchmarkArtifact: "sha256:bench",
		TraceArtifact:     "sha256:trace",
		DiffArtifact:      "sha256:diff",
		StartedAt:         now.Add(2 * time.Minute),
		FinishedAt:        now.Add(3 * time.Minute),
	}
	if err := store.SaveAttempt(ctx, attempt); err != nil {
		t.Fatalf("save attempt: %v", err)
	}
	attempts, err := store.AttemptsByNode(ctx, child.ID)
	if err != nil {
		t.Fatalf("attempts by node: %v", err)
	}
	if len(attempts) != 1 || attempts[0].BenchmarkArtifact != "sha256:bench" {
		t.Fatalf("attempts = %#v", attempts)
	}

	returnCode := 0
	result := model.GateResult{
		AttemptID:    attempt.ID,
		GateName:     gate.Name,
		SourceNodeID: root.ID,
		Passed:       true,
		ReturnCode:   &returnCode,
		LogArtifact:  "sha256:gate",
	}
	if err := store.SaveGateResult(ctx, result); err != nil {
		t.Fatalf("save gate result: %v", err)
	}
	results, err := store.GateResultsByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("gate results by attempt: %v", err)
	}
	if len(results) != 1 || !results[0].Passed || results[0].ReturnCode == nil || *results[0].ReturnCode != 0 {
		t.Fatalf("gate results = %#v", results)
	}

	if err := store.SaveAnnotation(ctx, model.Annotation{
		ID:        "annotation-1",
		RunID:     run.ID,
		NodeID:    child.ID,
		TaskID:    "task-1",
		Analysis:  "Looks promising",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("save annotation: %v", err)
	}
	annotations, err := store.AnnotationsByNode(ctx, child.ID)
	if err != nil {
		t.Fatalf("annotations by node: %v", err)
	}
	if len(annotations) != 1 || annotations[0].Analysis != "Looks promising" {
		t.Fatalf("annotations = %#v", annotations)
	}

	if err := store.SaveInfraEvent(ctx, model.InfraEvent{
		ID:        "infra-1",
		RunID:     run.ID,
		Message:   "worktree allocated",
		Breaking:  false,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("save infra event: %v", err)
	}
	events, err := store.InfraEventsByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("infra events by run: %v", err)
	}
	if len(events) != 1 || events[0].Message != "worktree allocated" || events[0].Breaking {
		t.Fatalf("infra events = %#v", events)
	}
}

func TestStoreActiveRunSwitchesByWorkspace(t *testing.T) {
	ctx := context.Background()
	db, closeFn := openTestDB(t, ctx)
	defer func() { _ = closeFn() }()
	store := NewSQLStore(db, nil)
	now := time.Date(2026, 4, 16, 7, 0, 0, 0, time.UTC)

	run1 := testRun("run-1", "/repo", now)
	run1.Active = true
	if err := store.SaveRun(ctx, run1); err != nil {
		t.Fatalf("save run1: %v", err)
	}
	run2 := testRun("run-2", "/repo", now.Add(time.Minute))
	run2.Active = true
	if err := store.SaveRun(ctx, run2); err != nil {
		t.Fatalf("save run2: %v", err)
	}

	active, ok, err := store.ActiveRun(ctx, "/repo")
	if err != nil {
		t.Fatalf("active run: %v", err)
	}
	if !ok || active.ID != "run-2" {
		t.Fatalf("active run = %#v, ok=%v", active, ok)
	}

	if err := store.ClearActiveRun(ctx, "/repo"); err != nil {
		t.Fatalf("clear active run: %v", err)
	}
	_, ok, err = store.ActiveRun(ctx, "/repo")
	if err != nil {
		t.Fatalf("active run after clear: %v", err)
	}
	if ok {
		t.Fatalf("expected no active run after clear")
	}
}

func TestStoreReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	db, closeFn := openTestDB(t, ctx)
	defer func() { _ = closeFn() }()
	store := NewSQLStore(db, nil)

	if _, err := store.Run(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Run() error = %v, want ErrNotFound", err)
	}
	if _, err := store.Node(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Node() error = %v, want ErrNotFound", err)
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=$1`, table).Scan(&count); err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}
	return count == 1
}

func indexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=$1`, index).Scan(&count); err != nil {
		t.Fatalf("query index %s: %v", index, err)
	}
	return count == 1
}

func testRun(id, workspace string, now time.Time) model.Run {
	return model.Run{
		ID:               id,
		WorkspacePath:    workspace,
		TargetPath:       workspace + "/pkg",
		BenchmarkCommand: "go test ./...",
		Metric:           model.MetricMax,
		Status:           model.RunStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func openTestDB(t *testing.T, ctx context.Context) (*sql.DB, func() error) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "evolve.db")
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, dbPath, MigrateSchema)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	return db, closeFn
}
