package embedding

import (
	"context"
	"os"
	"testing"
)

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
			{
				SymbolID:   "main.go:Handler",
				FilePath:   "main.go",
				SymbolName: "Handler",
				Content:    "func Handler(w http.ResponseWriter, r *http.Request) {}",
			},
			{
				SymbolID:   "main.go:Server",
				FilePath:   "main.go",
				SymbolName: "Server",
				Content:    "type Server struct { port int }",
			},
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
	}
	if job.State != StateRunning {
		t.Errorf("expected state running, got %s", job.State)
	}
	if job.WorkspaceID != "test-ws" {
		t.Errorf("expected workspace test-ws, got %s", job.WorkspaceID)
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

	symbol := SymbolInput{
		SymbolID:   "main.go:Foo",
		FilePath:   "main.go",
		SymbolName: "Foo",
		Content:    "func Foo() {}",
	}

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
		Symbols: []SymbolInput{{
			SymbolID:   "main.go:Bar",
			FilePath:   "main.go",
			SymbolName: "Bar",
			Content:    "func Bar() {}",
		}},
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
	embResult, err := store.GetEmbedding(ctx, "test-ws", "main.go:Bar")
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
		Symbols: []SymbolInput{{
			SymbolID:   "main.go:Bar",
			FilePath:   "main.go",
			SymbolName: "Bar",
			Content:    "func Bar() {}", // Same content
		}},
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

	symbol := SymbolInput{
		SymbolID:   "main.go:Digest",
		FilePath:   "main.go",
		SymbolName: "Digest",
		Content:    "func Digest() {}",
	}

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

	digest, model, ok, err = store.GetContentDigest(ctx, "test-ws", "main.go:Digest")
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
		Symbols: []SymbolInput{{
			SymbolID:   "main.go:Baz",
			FilePath:   "main.go",
			SymbolName: "Baz",
			Content:    "func Baz() {}",
		}},
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
			{SymbolID: "a.go:A", FilePath: "a.go", SymbolName: "A", Content: "func A() {}"},
			{SymbolID: "b.go:B", FilePath: "b.go", SymbolName: "B", Content: "func B() {}"},
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
		{SymbolID: "main.go:A", FilePath: "main.go", SymbolName: "A", Content: "func A() {}"},
		{SymbolID: "main.go:B", FilePath: "main.go", SymbolName: "B", Content: "func B() {}"},
		{SymbolID: "other.go:C", FilePath: "other.go", SymbolName: "C", Content: "func C() {}"},
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
		Symbols:     []SymbolInput{{SymbolID: "x.go:X", FilePath: "x.go", SymbolName: "X", Content: "func X() {}"}},
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// Helper to make tests wait for scheduled jobs
func init() {
	// Reduce retry delays for testing
}
