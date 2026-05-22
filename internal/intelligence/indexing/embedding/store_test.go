package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/storage/queue"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

const testSymbolPackageID = "go:testpkg"

func testSymbolInput(filePath, name, content string) SymbolInput {
	return SymbolInput{
		SymbolID:   filePath + ":" + name,
		FilePath:   filePath,
		SymbolName: name,
		Language:   "go",
		PackageID:  testSymbolPackageID,
		SymbolKey:  filePath + ":" + name,
		Content:    content,
	}
}

func testSymbolStorageID(filePath, name string) string {
	return testSymbolPackageID + "::" + filePath + ":" + name
}

func TestStore_EnqueueAndClaim(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Enqueue symbols
	result, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{
			testSymbolInput("main.go", "Handler", "func Handler(w http.ResponseWriter, r *http.Request) {}"),
			testSymbolInput("main.go", "Server", "type Server struct { port int }"),
		},
		Priority: PriorityHigh,
	})
	if err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	if result.Queued != 2 {
		t.Errorf("expected 2 queued, got %d", result.Queued)
	}
	if len(result.JobIDs) != 2 {
		t.Errorf("expected 2 job IDs, got %d", len(result.JobIDs))
	}

	// Claim first job
	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("failed to claim: %v", err)
	}
	if job == nil {
		t.Fatal("expected a job, got nil")
		return
	}
	if job.State != StateRunning {
		t.Errorf("expected state running, got %s", job.State)
	}
	if job.WorkspaceID != "test-ws" {
		t.Errorf("expected workspace test-ws, got %s", job.WorkspaceID)
	}
}

func TestStore_ClaimNextInWorkspaceOnlyClaimsMatchingJobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	for _, workspaceID := range []string{"workspace-a", "workspace-b"} {
		_, err := store.Enqueue(ctx, EnqueueRequest{
			WorkspaceID: workspaceID,
			Symbols:     []SymbolInput{testSymbolInput("main.go", "Handler", "func Handler() {}")},
		})
		if err != nil {
			t.Fatalf("enqueue %s: %v", workspaceID, err)
		}
	}

	job, err := store.ClaimNextInWorkspace(ctx, "workspace-b")
	if err != nil {
		t.Fatalf("claim scoped: %v", err)
	}
	if job == nil || job.WorkspaceID != "workspace-b" {
		t.Fatalf("job=%+v want workspace-b", job)
	}

	statsA, err := store.StatsInWorkspace(ctx, "workspace-a")
	if err != nil {
		t.Fatalf("stats a: %v", err)
	}
	if statsA.QueuedCount != 1 || statsA.RunningCount != 0 {
		t.Fatalf("stats workspace-a=%+v want queued=1 running=0", statsA)
	}

	statsB, err := store.StatsInWorkspace(ctx, "workspace-b")
	if err != nil {
		t.Fatalf("stats b: %v", err)
	}
	if statsB.QueuedCount != 0 || statsB.RunningCount != 1 {
		t.Fatalf("stats workspace-b=%+v want queued=0 running=1", statsB)
	}
}

func TestStore_ClaimNextInWorkspaceKindSkipsOtherKinds(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	if _, err := store.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: "workspace-a",
		Memories: []MemoryInput{{
			Name:    "memory://first",
			Type:    "note",
			Content: "older memory job",
		}},
		Model: "model-a",
	}); err != nil {
		t.Fatalf("enqueue memory: %v", err)
	}
	if _, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "workspace-a",
		Symbols:     []SymbolInput{testSymbolInput("main.go", "Handler", "func Handler() {}")},
		Model:       "model-a",
	}); err != nil {
		t.Fatalf("enqueue symbol: %v", err)
	}

	job, err := store.ClaimNextInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindSymbol)
	if err != nil {
		t.Fatalf("claim symbol: %v", err)
	}
	if job == nil || job.Kind != embedqueue.TaskKindSymbol {
		t.Fatalf("job=%+v want symbol", job)
	}

	next, err := store.ClaimNextInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("claim memory: %v", err)
	}
	if next == nil || next.Kind != embedqueue.TaskKindMemory {
		t.Fatalf("next=%+v want memory", next)
	}

	symbolStats, err := store.StatsInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindSymbol)
	if err != nil {
		t.Fatalf("symbol stats: %v", err)
	}
	if symbolStats.RunningCount != 1 || symbolStats.QueuedCount != 0 {
		t.Fatalf("symbol stats=%+v want running=1 queued=0", symbolStats)
	}

	memoryStats, err := store.StatsInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}
	if memoryStats.RunningCount != 1 || memoryStats.QueuedCount != 0 || memoryStats.EmbeddingsCount != 0 {
		t.Fatalf("memory stats=%+v want running=1 queued=0 embeddings=0", memoryStats)
	}
}

func TestStore_RequeueStaleRunningInWorkspaceOnlyRecoversMatchingJobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	for _, workspaceID := range []string{"workspace-a", "workspace-b"} {
		_, err := store.Enqueue(ctx, EnqueueRequest{
			WorkspaceID: workspaceID,
			Symbols:     []SymbolInput{testSymbolInput("main.go", "Handler", "func Handler() {}")},
		})
		if err != nil {
			t.Fatalf("enqueue %s: %v", workspaceID, err)
		}
		if _, err := store.ClaimNextInWorkspace(ctx, workspaceID); err != nil {
			t.Fatalf("claim %s: %v", workspaceID, err)
		}
	}

	staleUpdatedAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET updated_at = ?
		WHERE state = 'running'
	`, embeddingQueueTable), staleUpdatedAt); err != nil {
		t.Fatalf("mark stale: %v", err)
	}

	recovered, err := store.RequeueStaleRunningInWorkspace(ctx, "workspace-a", time.Hour)
	if err != nil {
		t.Fatalf("recover scoped: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want 1", recovered)
	}

	statsA, err := store.StatsInWorkspace(ctx, "workspace-a")
	if err != nil {
		t.Fatalf("stats a: %v", err)
	}
	if statsA.QueuedCount != 1 || statsA.RunningCount != 0 {
		t.Fatalf("stats workspace-a=%+v want queued=1 running=0", statsA)
	}

	statsB, err := store.StatsInWorkspace(ctx, "workspace-b")
	if err != nil {
		t.Fatalf("stats b: %v", err)
	}
	if statsB.QueuedCount != 0 || statsB.RunningCount != 1 {
		t.Fatalf("stats workspace-b=%+v want queued=0 running=1", statsB)
	}
}

func TestStore_RequeueStaleRunningInWorkspaceKindOnlyRecoversMatchingKind(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	if _, err := store.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: "workspace-a",
		Memories: []MemoryInput{{
			Name:    "memory://first",
			Content: "memory job",
		}},
	}); err != nil {
		t.Fatalf("enqueue memory: %v", err)
	}
	if _, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "workspace-a",
		Symbols:     []SymbolInput{testSymbolInput("main.go", "Handler", "func Handler() {}")},
	}); err != nil {
		t.Fatalf("enqueue symbol: %v", err)
	}

	if _, err := store.ClaimNextInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindMemory); err != nil {
		t.Fatalf("claim memory: %v", err)
	}
	if _, err := store.ClaimNextInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindSymbol); err != nil {
		t.Fatalf("claim symbol: %v", err)
	}

	staleUpdatedAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET updated_at = ?
		WHERE state = 'running'
	`, embeddingQueueTable), staleUpdatedAt); err != nil {
		t.Fatalf("mark stale: %v", err)
	}

	recovered, err := store.RequeueStaleRunningInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindSymbol, time.Hour)
	if err != nil {
		t.Fatalf("recover scoped kind: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want 1", recovered)
	}

	symbolStats, err := store.StatsInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindSymbol)
	if err != nil {
		t.Fatalf("symbol stats: %v", err)
	}
	if symbolStats.QueuedCount != 1 || symbolStats.RunningCount != 0 {
		t.Fatalf("symbol stats=%+v want queued=1 running=0", symbolStats)
	}

	memoryStats, err := store.StatsInWorkspaceKind(ctx, "workspace-a", embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}
	if memoryStats.QueuedCount != 0 || memoryStats.RunningCount != 1 {
		t.Fatalf("memory stats=%+v want queued=0 running=1", memoryStats)
	}
}

func TestStore_EnqueueMemoriesAndClaim(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	result, err := store.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: "test-ws",
		Memories: []MemoryInput{{
			Name:    "decision:backend",
			Type:    "decision",
			Content: "[May 2026] [decision] Use Turso with Qwen embeddings",
		}},
		Priority: PriorityHigh,
		Model:    "text-embedding-qwen3-embedding-8b",
	})
	if err != nil {
		t.Fatalf("enqueue memories failed: %v", err)
	}
	if result.Queued != 1 {
		t.Fatalf("queued=%d want 1", result.Queued)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if job == nil {
		t.Fatal("expected a job, got nil")
	}
	if job.Kind != embedqueue.TaskKindMemory {
		t.Fatalf("kind=%q want %q", job.Kind, embedqueue.TaskKindMemory)
	}
	if job.MemoryName != "decision:backend" {
		t.Fatalf("memory name=%q", job.MemoryName)
	}
	if job.Model != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("model=%q", job.Model)
	}
}

func TestStore_EnqueueMemoriesDeduplicatesByModelAndDigest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	req := MemoryEnqueueRequest{
		WorkspaceID: "test-ws",
		Memories: []MemoryInput{{
			Name:    "note:queue",
			Type:    "note",
			Content: "queue this memory",
		}},
		Model: "model-a",
	}
	first, err := store.EnqueueMemories(ctx, req)
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	if first.Queued != 1 {
		t.Fatalf("first queued=%d want 1", first.Queued)
	}
	second, err := store.EnqueueMemories(ctx, req)
	if err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}
	if second.Queued != 0 || second.Skipped != 1 {
		t.Fatalf("second queued=%d skipped=%d, want 0/1", second.Queued, second.Skipped)
	}

	req.Model = "model-b"
	third, err := store.EnqueueMemories(ctx, req)
	if err != nil {
		t.Fatalf("third enqueue failed: %v", err)
	}
	if third.Queued != 1 {
		t.Fatalf("third queued=%d want 1 for changed model", third.Queued)
	}
}

func TestStore_EnqueueSymbolPreservesCanonicalIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	result, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{{
			SymbolID:   "legacy-id",
			FilePath:   "pkg/foo/foo.go",
			SymbolName: "Handler",
			Language:   "go",
			PackageID:  "go:pkg/foo",
			SymbolKey:  "func Handler",
			MemoryName: "symbol://test-ws/go:pkg/foo::func Handler",
			Content:    "func Handler() {}",
		}},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if result.Queued != 1 {
		t.Fatalf("queued=%d want 1", result.Queued)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if job.MemoryName != "symbol://test-ws/go:pkg/foo::func Handler" {
		t.Fatalf("memory name=%q", job.MemoryName)
	}
	if job.SymbolID != "go:pkg/foo::func Handler" {
		t.Fatalf("symbol id=%q, want package-scoped storage id", job.SymbolID)
	}
	if job.Language != "go" || job.PackageID != "go:pkg/foo" || job.SymbolKey != "func Handler" {
		t.Fatalf("identity language/package/key=%q/%q/%q", job.Language, job.PackageID, job.SymbolKey)
	}
}

func TestStore_EnqueueSymbolDedupeDistinguishesPackages(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	req := EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{
			{
				SymbolID:   "legacy-same",
				FilePath:   "pkg/a/foo.go",
				SymbolName: "Handler",
				Language:   "go",
				PackageID:  "go:pkg/a",
				SymbolKey:  "func Handler",
				MemoryName: "symbol://test-ws/go:pkg/a::func Handler",
				Content:    "func Handler() {}",
			},
			{
				SymbolID:   "legacy-same",
				FilePath:   "pkg/b/foo.go",
				SymbolName: "Handler",
				Language:   "go",
				PackageID:  "go:pkg/b",
				SymbolKey:  "func Handler",
				MemoryName: "symbol://test-ws/go:pkg/b::func Handler",
				Content:    "func Handler() {}",
			},
		},
	}
	result, err := store.Enqueue(ctx, req)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if result.Queued != 2 || result.Skipped != 0 {
		t.Fatalf("queued/skipped=%d/%d want 2/0", result.Queued, result.Skipped)
	}
	jobA, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if err := store.Complete(ctx, jobA.ID, []float32{0.1, 0.2}, "model-a"); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	jobB, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim second: %v", err)
	}
	if err := store.Complete(ctx, jobB.ID, []float32{0.3, 0.4}, "model-a"); err != nil {
		t.Fatalf("complete second: %v", err)
	}
	if _, err := store.GetEmbedding(ctx, "test-ws", "go:pkg/a::func Handler"); err != nil {
		t.Fatalf("get package a embedding: %v", err)
	}
	if _, err := store.GetEmbedding(ctx, "test-ws", "go:pkg/b::func Handler"); err != nil {
		t.Fatalf("get package b embedding: %v", err)
	}
}

func TestStore_EnqueueSymbolRequiresCanonicalIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	_, err = store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{{
			SymbolID:   "main.go:Legacy",
			FilePath:   "main.go",
			SymbolName: "Legacy",
			Content:    "func Legacy() {}",
		}},
	})
	if err == nil {
		t.Fatal("expected canonical identity error")
	}
	if !strings.Contains(err.Error(), "memory_name or package_id and symbol_key") {
		t.Fatalf("error=%v", err)
	}
}

func TestStore_EnqueueSymbolDerivesIdentityFromCanonicalMemoryName(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	memoryName := "symbol://test-ws/go:testpkg::main.go:Handler"
	result, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{{
			FilePath:   "main.go",
			SymbolName: "Handler",
			MemoryName: memoryName,
			Content:    "func Handler() {}",
		}},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if result.Queued != 1 {
		t.Fatalf("queued=%d want 1", result.Queued)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if job.SymbolID != "go:testpkg::main.go:Handler" {
		t.Fatalf("symbol_id=%q", job.SymbolID)
	}
	if job.PackageID != "go:testpkg" || job.SymbolKey != "main.go:Handler" || job.MemoryName != memoryName {
		t.Fatalf("identity package/key/name=%q/%q/%q", job.PackageID, job.SymbolKey, job.MemoryName)
	}
}

func TestStore_ClaimNextSkipsLegacySymbolJobWithoutCanonicalIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	payload, err := json.Marshal(embeddingPayload{
		WorkspaceID:   "test-ws",
		SymbolID:      "main.go:Legacy",
		FilePath:      "main.go",
		SymbolName:    "Legacy",
		Content:       "func Legacy() {}",
		ContentDigest: computeDigest("func Legacy() {}"),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enqueued, err := store.queue.Enqueue(ctx, queue.EnqueueRequest{
		GroupID:     "test-ws",
		Payload:     payload,
		DedupeKey:   "legacy-incomplete",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("enqueue raw queue job: %v", err)
	}
	if enqueued.Queued != 1 {
		t.Fatalf("queued=%d want 1", enqueued.Queued)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job != nil {
		t.Fatalf("job=%+v want nil after invalid legacy skip", job)
	}

	rawJob, err := store.queue.GetJob(ctx, enqueued.JobIDs[0])
	if err != nil {
		t.Fatalf("get raw job: %v", err)
	}
	if rawJob.State != queue.StateError {
		t.Fatalf("legacy job state=%s want error", rawJob.State)
	}
	if !strings.Contains(rawJob.Error, "invalid symbol embedding identity") {
		t.Fatalf("legacy job error=%q", rawJob.Error)
	}
}

func TestMigrateLegacyJobsOnlyCopiesCanonicalSymbolIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	if _, err := store.db.ExecContext(ctx, `
		CREATE TABLE embedding_jobs (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			symbol_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			symbol_name TEXT NOT NULL,
			content TEXT NOT NULL,
			content_digest TEXT NOT NULL,
			state TEXT NOT NULL,
			priority INTEGER NOT NULL,
			attempts INTEGER NOT NULL,
			max_attempts INTEGER NOT NULL,
			error TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			scheduled_at TEXT,
			completed_at TEXT
		)
	`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	now := sqlutil.FormatTimestamp(time.Now().UTC())
	for _, row := range []struct {
		id       string
		symbolID string
		content  string
	}{
		{id: "bad", symbolID: "main.go:Legacy", content: "func Legacy() {}"},
		{id: "good", symbolID: testSymbolStorageID("main.go", "Good"), content: "func Good() {}"},
	} {
		if _, err := store.db.ExecContext(ctx, `
			INSERT INTO embedding_jobs
				(id, workspace_id, symbol_id, file_path, symbol_name, content, content_digest,
				 state, priority, attempts, max_attempts, error, created_at, updated_at,
				 scheduled_at, completed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', 50, 0, 3, NULL, ?, ?, NULL, NULL)
		`, row.id, "test-ws", row.symbolID, "main.go", row.id, row.content, computeDigest(row.content), now, now); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.id, err)
		}
	}

	if err := migrateLegacyJobs(ctx, store.db); err != nil {
		t.Fatalf("migrate legacy jobs: %v", err)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.QueuedCount != 1 {
		t.Fatalf("queued=%d want only canonical legacy row", stats.QueuedCount)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim migrated job: %v", err)
	}
	if job == nil {
		t.Fatal("expected migrated canonical job")
	}
	if job.SymbolID != testSymbolStorageID("main.go", "Good") {
		t.Fatalf("symbol_id=%q", job.SymbolID)
	}
	if job.MemoryName != "symbol://test-ws/"+testSymbolStorageID("main.go", "Good") {
		t.Fatalf("memory_name=%q", job.MemoryName)
	}
}

func TestStore_CompleteMemoryJobDoesNotCreateSymbolEmbedding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	_, err = store.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: "test-ws",
		Memories: []MemoryInput{{
			Name:    "note:external-storage",
			Content: "memory embeddings are stored in named memory",
		}},
	})
	if err != nil {
		t.Fatalf("enqueue memories failed: %v", err)
	}
	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if err := store.CompleteJob(ctx, job.ID); err != nil {
		t.Fatalf("complete memory job failed: %v", err)
	}
	if _, err := store.GetEmbedding(ctx, "test-ws", "note:external-storage"); err == nil {
		t.Fatal("memory job created symbol embedding unexpectedly")
	}
}

func TestStore_Deduplication(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	symbol := testSymbolInput("main.go", "Foo", "func Foo() {}")

	// First enqueue
	result1, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{symbol},
	})
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	if result1.Queued != 1 {
		t.Errorf("expected 1 queued, got %d", result1.Queued)
	}

	// Second enqueue - same content, same digest
	result2, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{symbol},
	})
	if err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}
	if result2.Queued != 0 {
		t.Errorf("expected 0 queued (deduplicated), got %d", result2.Queued)
	}
	if result2.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", result2.Skipped)
	}
}

func TestStore_CompleteAndGetEmbedding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Enqueue
	result, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{testSymbolInput("main.go", "Bar", "func Bar() {}")},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Claim
	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	// Complete with embedding
	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	err = store.Complete(ctx, job.ID, embedding, "test-model")
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	// Verify job state
	updatedJob, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if updatedJob.State != StateOK {
		t.Errorf("expected state ok, got %s", updatedJob.State)
	}

	// Get embedding
	embResult, err := store.GetEmbedding(ctx, "test-ws", testSymbolStorageID("main.go", "Bar"))
	if err != nil {
		t.Fatalf("get embedding failed: %v", err)
	}
	if embResult.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", embResult.Model)
	}
	if len(embResult.Embedding) != 5 {
		t.Errorf("expected 5 dimensions, got %d", len(embResult.Embedding))
	}

	// Verify deduplication works after embedding exists
	result2, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{testSymbolInput("main.go", "Bar", "func Bar() {}")},
		Deduplicate: true,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if result2.Skipped != 1 {
		t.Errorf("expected 1 skipped (embedding exists), got %d", result2.Skipped)
	}

	_ = result // avoid unused warning
}

func TestStore_GetContentDigest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	digest, model, ok, err := store.GetContentDigest(ctx, "test-ws", "main.go:Digest")
	if err != nil {
		t.Fatalf("get content digest failed: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for missing symbol, got digest=%s model=%s", digest, model)
	}

	symbol := testSymbolInput("main.go", "Digest", "func Digest() {}")

	_, err = store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{symbol},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	err = store.Complete(ctx, job.ID, []float32{0.1, 0.2}, "test-model")
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	digest, model, ok, err = store.GetContentDigest(ctx, "test-ws", testSymbolStorageID("main.go", "Digest"))
	if err != nil {
		t.Fatalf("get content digest failed: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for existing symbol")
	}
	if model != "test-model" {
		t.Errorf("expected model test-model, got %s", model)
	}
	expectedDigest := computeDigest(symbol.Content)
	if digest != expectedDigest {
		t.Errorf("expected digest %s, got %s", expectedDigest, digest)
	}
}

func TestStore_FailAndRetry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Enqueue
	_, err = store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{testSymbolInput("main.go", "Baz", "func Baz() {}")},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Claim
	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	// Fail first attempt
	err = store.Fail(ctx, job.ID, "API error")
	if err != nil {
		t.Fatalf("fail failed: %v", err)
	}

	// Check state is retry
	updatedJob, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if updatedJob.State != StateRetry {
		t.Errorf("expected state retry, got %s", updatedJob.State)
	}
	if updatedJob.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", updatedJob.Attempts)
	}
	if updatedJob.Error != "API error" {
		t.Errorf("expected error 'API error', got %s", updatedJob.Error)
	}
}

func TestStore_Stats(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Initial stats
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if stats.QueuedCount != 0 {
		t.Errorf("expected 0 queued, got %d", stats.QueuedCount)
	}

	// Enqueue some jobs
	_, err = store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{
			testSymbolInput("a.go", "A", "func A() {}"),
			testSymbolInput("b.go", "B", "func B() {}"),
		},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Check stats
	stats, err = store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if stats.QueuedCount != 2 {
		t.Errorf("expected 2 queued, got %d", stats.QueuedCount)
	}
}

func TestStore_GetEmbeddingsByFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Create some embeddings
	symbols := []SymbolInput{
		testSymbolInput("main.go", "A", "func A() {}"),
		testSymbolInput("main.go", "B", "func B() {}"),
		testSymbolInput("other.go", "C", "func C() {}"),
	}

	for _, sym := range symbols {
		_, err = store.Enqueue(ctx, EnqueueRequest{
			WorkspaceID: "test-ws",
			Symbols:     []SymbolInput{sym},
		})
		if err != nil {
			t.Fatalf("enqueue failed: %v", err)
		}

		job, _ := store.ClaimNext(ctx)
		embedding := []float32{0.1, 0.2, 0.3}
		_ = store.Complete(ctx, job.ID, embedding, "test-model")
	}

	// Get embeddings for main.go
	results, err := store.GetEmbeddingsByFile(ctx, "test-ws", "main.go")
	if err != nil {
		t.Fatalf("get by file failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 embeddings for main.go, got %d", len(results))
	}
}

func TestStore_Cleanup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Create and complete a job
	_, err = store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{testSymbolInput("x.go", "X", "func X() {}")},
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	job, _ := store.ClaimNext(ctx)
	_ = store.Complete(ctx, job.ID, []float32{0.1}, "test-model")

	// Cleanup with 0 duration (remove all completed)
	deleted, err := store.Cleanup(ctx, 0)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Verify job is gone
	_, err = store.GetJob(ctx, job.ID)
	if err == nil {
		t.Error("expected job to be deleted")
	}
}

func TestStore_CleanupInWorkspaceKindOnlyDeletesMatchingJobs(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	if _, err := store.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: "test-ws",
		Memories:    []MemoryInput{{Name: "memory://one", Content: "remember this"}},
	}); err != nil {
		t.Fatalf("enqueue memory: %v", err)
	}
	if _, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{testSymbolInput("x.go", "X", "func X() {}")},
	}); err != nil {
		t.Fatalf("enqueue symbol: %v", err)
	}

	memoryJob, err := store.ClaimNextInWorkspaceKind(ctx, "test-ws", embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("claim memory: %v", err)
	}
	if err := store.CompleteJob(ctx, memoryJob.ID); err != nil {
		t.Fatalf("complete memory: %v", err)
	}
	symbolJob, err := store.ClaimNextInWorkspaceKind(ctx, "test-ws", embedqueue.TaskKindSymbol)
	if err != nil {
		t.Fatalf("claim symbol: %v", err)
	}
	if err := store.Complete(ctx, symbolJob.ID, []float32{0.1}, "test-model"); err != nil {
		t.Fatalf("complete symbol: %v", err)
	}

	deleted, err := store.CleanupInWorkspaceKind(ctx, "test-ws", embedqueue.TaskKindMemory, 0)
	if err != nil {
		t.Fatalf("cleanup memory: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}
	if _, err := store.GetJob(ctx, symbolJob.ID); err != nil {
		t.Fatalf("symbol job should remain: %v", err)
	}
}

func TestStore_PurgeInWorkspaceKindOnlyDeletesMatchingJobs(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	if _, err := store.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: "test-ws",
		Memories:    []MemoryInput{{Name: "memory://one", Content: "remember this"}},
	}); err != nil {
		t.Fatalf("enqueue memory: %v", err)
	}
	if _, err := store.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: "test-ws",
		Symbols:     []SymbolInput{testSymbolInput("x.go", "X", "func X() {}")},
	}); err != nil {
		t.Fatalf("enqueue symbol: %v", err)
	}

	deleted, err := store.Purge(ctx, "test-ws", embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("purge memory: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}
	stats, err := store.StatsInWorkspace(ctx, "test-ws")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.QueuedCount != 1 {
		t.Fatalf("stats=%+v want one remaining symbol job", stats)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// Helper to make tests wait for scheduled jobs
func init() {
	// Reduce retry delays for testing
}
