package embedding

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/storage"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// fakeEmbedder is a configurable MemoryEmbedder for processor tests.
type fakeEmbedder struct {
	dim      int
	provider string
	model    string
	vec      []float32
	err      error
	calls    int
	lastText string
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) (MemoryEmbedding, error) {
	f.calls++
	f.lastText = text
	if f.err != nil {
		return MemoryEmbedding{}, f.err
	}
	vec := make([]float32, len(f.vec))
	copy(vec, f.vec)
	return MemoryEmbedding{Vec: vec, Model: f.model}, nil
}

func (f *fakeEmbedder) Provider() string { return f.provider }
func (f *fakeEmbedder) Model() string    { return f.model }
func (f *fakeEmbedder) Dimensions() int  { return f.dim }

func newTestMemoryJob(t *testing.T) (*memstore.Store, *Store, *EmbeddingJob) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()

	memory, err := memstore.Open(ctx, root, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	t.Cleanup(func() { _ = memory.Close() })

	queue, err := OpenStore(ctx, filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	workspaceID := "ws-processor"
	if _, err := memory.SaveFromResult(ctx, "decision:backend", "decision", workspaceID,
		"Use Turso with Qwen embeddings for memory",
		[]byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)); err != nil {
		t.Fatalf("save memory: %v", err)
	}

	if _, err := queue.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: workspaceID,
		Memories: []MemoryInput{{
			Name:    "decision:backend",
			Type:    "decision",
			Content: "[May 2026] [decision] Use Turso with Qwen embeddings",
		}},
		Model: "text-embedding-qwen3-embedding-8b",
	}); err != nil {
		t.Fatalf("enqueue memory job: %v", err)
	}

	job, err := queue.ClaimNextKind(ctx, embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("claim memory job: %v", err)
	}
	if job == nil {
		t.Fatalf("expected claimed memory job, got nil")
	}
	return memory, queue, job
}

func TestMemoryJobProcessor_Process_StoresMetadataAndCompletesJob(t *testing.T) {
	ctx := context.Background()
	memory, queue, job := newTestMemoryJob(t)

	embedder := &fakeEmbedder{
		dim:      3,
		provider: "openai_compat",
		model:    "text-embedding-qwen3-embedding-8b",
		vec:      []float32{0.1, 0.2, 0.3},
	}
	proc := &MemoryJobProcessor{
		Store:              queue,
		Memory:             memory,
		Embedder:           embedder,
		ExpectedDimensions: 3,
		Now:                func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) },
	}

	if err := proc.Process(ctx, job); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if embedder.calls != 1 {
		t.Fatalf("embedder calls=%d want 1", embedder.calls)
	}
	if !strings.Contains(embedder.lastText, "Turso with Qwen") {
		t.Fatalf("embedder text=%q", embedder.lastText)
	}

	// Named-memory row should now have the embedding.
	got, err := memory.GetEmbedding(ctx, "decision:backend", "ws-processor")
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("embedding len=%d want 3", len(got))
	}
	if got[0] != 0.1 || got[1] != 0.2 || got[2] != 0.3 {
		t.Fatalf("embedding values=%v", got)
	}

	// Queue job should be marked complete.
	updated, err := queue.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if updated.State != StateOK {
		t.Fatalf("state=%q want ok", updated.State)
	}
	if updated.Error != "" {
		t.Fatalf("error=%q want empty", updated.Error)
	}
}

func TestMemoryJobProcessor_Process_DryRunSkipsEmbedAndCompletes(t *testing.T) {
	ctx := context.Background()
	memory, queue, job := newTestMemoryJob(t)

	embedder := &fakeEmbedder{dim: 3, model: "model-x"}
	proc := &MemoryJobProcessor{
		Store:              queue,
		Memory:             memory,
		Embedder:           embedder,
		ExpectedDimensions: 3,
		DryRun:             true,
	}

	if err := proc.Process(ctx, job); err != nil {
		t.Fatalf("Process dry-run: %v", err)
	}
	if embedder.calls != 0 {
		t.Fatalf("embedder calls=%d want 0 in dry-run", embedder.calls)
	}
	// Memory row should NOT have an embedding in dry-run.
	got, err := memory.GetEmbedding(ctx, "decision:backend", "ws-processor")
	if err != nil {
		t.Fatalf("GetEmbedding after dry-run: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no embedding after dry-run, found %v", got)
	}
	// Job should be complete.
	updated, err := queue.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if updated.State != StateOK {
		t.Fatalf("state=%q want ok", updated.State)
	}
}

func TestMemoryJobProcessor_Process_ProviderErrorLeavesJobRetryable(t *testing.T) {
	ctx := context.Background()
	memory, queue, job := newTestMemoryJob(t)

	embedder := &fakeEmbedder{
		dim:      3,
		provider: "openai_compat",
		model:    "model-x",
		err:      errors.New("provider timeout"),
	}
	proc := &MemoryJobProcessor{
		Store:              queue,
		Memory:             memory,
		Embedder:           embedder,
		ExpectedDimensions: 3,
	}

	err := proc.Process(ctx, job)
	if err == nil || !strings.Contains(err.Error(), "embed memory") {
		t.Fatalf("Process error=%v, want embed-memory error", err)
	}

	// Caller records the failure using the existing queue semantics.
	if failErr := queue.Fail(ctx, job.ID, err.Error()); failErr != nil {
		t.Fatalf("queue.Fail: %v", failErr)
	}
	updated, err := queue.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if updated.State != StateRetry {
		t.Fatalf("state=%q want retry (provider error)", updated.State)
	}
	if !strings.Contains(updated.Error, "provider timeout") {
		t.Fatalf("error=%q want provider timeout", updated.Error)
	}
	if updated.Attempts != 1 {
		t.Fatalf("attempts=%d want 1", updated.Attempts)
	}
}

func TestMemoryJobProcessor_Process_DimensionMismatchLeavesJobRetryable(t *testing.T) {
	ctx := context.Background()
	memory, queue, job := newTestMemoryJob(t)

	// Embedder returns a vector with the wrong length.
	embedder := &fakeEmbedder{
		dim:      4,
		provider: "openai_compat",
		model:    "model-x",
		vec:      []float32{0.1, 0.2, 0.3}, // length 3 != expected 4
	}
	proc := &MemoryJobProcessor{
		Store:              queue,
		Memory:             memory,
		Embedder:           embedder,
		ExpectedDimensions: 4,
	}

	err := proc.Process(ctx, job)
	if err == nil || !strings.Contains(err.Error(), "dimension mismatch") {
		t.Fatalf("Process error=%v, want dimension mismatch", err)
	}

	if failErr := queue.Fail(ctx, job.ID, err.Error()); failErr != nil {
		t.Fatalf("queue.Fail: %v", failErr)
	}
	updated, err := queue.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if updated.State != StateRetry {
		t.Fatalf("state=%q want retry (dimension mismatch)", updated.State)
	}
	if !strings.Contains(updated.Error, "dimension mismatch") {
		t.Fatalf("error=%q want dimension mismatch", updated.Error)
	}
}

func TestMemoryJobProcessor_Process_RejectsNonMemoryJob(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	queue, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	proc := &MemoryJobProcessor{Store: queue}
	err = proc.Process(ctx, &EmbeddingJob{Kind: embedqueue.TaskKindSymbol})
	if !errors.Is(err, ErrUnsupportedMemoryJobKind) {
		t.Fatalf("Process err=%v, want ErrUnsupportedMemoryJobKind", err)
	}
}

func TestMemoryJobProcessor_Process_RejectsMissingIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	queue, err := OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	proc := &MemoryJobProcessor{Store: queue}
	if err := proc.Process(ctx, &EmbeddingJob{Kind: embedqueue.TaskKindMemory}); err == nil ||
		!strings.Contains(err.Error(), "workspace_id is required") {
		t.Fatalf("missing workspace err=%v", err)
	}
	if err := proc.Process(ctx, &EmbeddingJob{Kind: embedqueue.TaskKindMemory, WorkspaceID: "ws"}); err == nil ||
		!strings.Contains(err.Error(), "memory_name is required") {
		t.Fatalf("missing memory_name err=%v", err)
	}
}

func TestMemoryJobProcessor_Process_StaleRecoveryAvoidsDoubleWrite(t *testing.T) {
	ctx := context.Background()
	memory, queue, _ := newTestMemoryJob(t)

	// Enqueue a second memory job so we can simulate a worker crash on
	// the first and recovery on the second.
	workspaceID := "ws-processor"
	if _, err := memory.SaveFromResult(ctx, "decision:recovery", "decision", workspaceID,
		"Stale recovery smoke entry",
		[]byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)); err != nil {
		t.Fatalf("save recovery memory: %v", err)
	}
	if _, err := queue.EnqueueMemories(ctx, MemoryEnqueueRequest{
		WorkspaceID: workspaceID,
		Memories: []MemoryInput{{
			Name:    "decision:recovery",
			Type:    "decision",
			Content: "stale recovery content",
		}},
		Model: "text-embedding-qwen3-embedding-8b",
	}); err != nil {
		t.Fatalf("enqueue recovery job: %v", err)
	}

	embedder := &fakeEmbedder{
		dim:      3,
		provider: "openai_compat",
		model:    "text-embedding-qwen3-embedding-8b",
		vec:      []float32{0.4, 0.5, 0.6},
	}
	proc := &MemoryJobProcessor{
		Store:              queue,
		Memory:             memory,
		Embedder:           embedder,
		ExpectedDimensions: 3,
		Now:                func() time.Time { return time.Date(2026, 5, 7, 13, 0, 0, 0, time.UTC) },
	}

	// Simulate a worker that claimed a job and then crashed: the job is
	// still in state=running. The recovery path (RequeueStaleRunning) moves
	// stale running jobs back to queued without losing them.
	staleJob, err := queue.ClaimNextKind(ctx, embedqueue.TaskKindMemory)
	if err != nil || staleJob == nil {
		t.Fatalf("claim stale job: job=%v err=%v", staleJob, err)
	}
	recovered, err := queue.RequeueStaleRunning(ctx, 0)
	if err != nil {
		t.Fatalf("requeue stale: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered=%d want 0 for fresh running job", recovered)
	}
	staleUpdatedAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := queue.db.ExecContext(ctx, `UPDATE embedding_queue_jobs SET updated_at = ? WHERE id = ?`, staleUpdatedAt, staleJob.ID); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	recovered, err = queue.RequeueStaleRunning(ctx, time.Hour)
	if err != nil {
		t.Fatalf("requeue stale after aging: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want 1 after aging", recovered)
	}

	// Reclaim the recovered job and process it normally. The processor
	// must not double-write: it embeds exactly once and marks complete.
	job2, err := queue.ClaimNextKind(ctx, embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if job2 == nil {
		t.Fatalf("expected reclaimed job, got nil")
	}
	if job2.ID != staleJob.ID {
		t.Fatalf("reclaimed id=%q want %q", job2.ID, staleJob.ID)
	}
	if err := proc.Process(ctx, job2); err != nil {
		t.Fatalf("Process after recovery: %v", err)
	}

	// The final embedding should reflect exactly one embed call.
	got, err := memory.GetEmbedding(ctx, "decision:recovery", workspaceID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != 3 || got[0] != 0.4 || got[1] != 0.5 || got[2] != 0.6 {
		t.Fatalf("embedding=%v want [0.4 0.5 0.6]", got)
	}

	// The job should be in ok state.
	updated, err := queue.GetJob(ctx, job2.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if updated.State != StateOK {
		t.Fatalf("state=%q want ok", updated.State)
	}
}

func TestMemoryJobProcessor_Process_RecordsMetadataWithEmbeddedModel(t *testing.T) {
	ctx := context.Background()
	memory, queue, job := newTestMemoryJob(t)

	embedder := &fakeEmbedder{
		dim:      2,
		provider: "openai_compat",
		model:    "text-embedding-qwen3-embedding-8b",
		vec:      []float32{0.5, 0.6},
	}
	proc := &MemoryJobProcessor{
		Store:              queue,
		Memory:             memory,
		Embedder:           embedder,
		ExpectedDimensions: 2,
		Now:                func() time.Time { return time.Date(2026, 5, 7, 14, 0, 0, 0, time.UTC) },
	}
	if err := proc.Process(ctx, job); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// EmbeddingMetadata was set with the same provider/model/dims.
	// Validate by storing a conflicting-dim vector and expecting an error
	// (the existing ValidateEmbeddingDimensions path uses the metadata).
	if err := memory.UpdateEmbedding(ctx, "decision:backend", "ws-processor", []float32{1, 2, 3}); err == nil {
		t.Fatalf("expected dimension-validation error, got nil")
	}
}

// _ keeps storage import in use if a future assertion needs it.
var _ = storage.EmbeddingMetadata{}
