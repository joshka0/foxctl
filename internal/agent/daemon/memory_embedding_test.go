package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/platform/config"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

type fakeDaemonMemoryEmbedder struct {
	vec      []float32
	provider string
	model    string
	err      error
	calls    int
	lastText string
}

func (f *fakeDaemonMemoryEmbedder) Embed(_ context.Context, text string) (embedding.MemoryEmbedding, error) {
	f.calls++
	f.lastText = text
	if f.err != nil {
		return embedding.MemoryEmbedding{}, f.err
	}
	vec := make([]float32, len(f.vec))
	copy(vec, f.vec)
	return embedding.MemoryEmbedding{Vec: vec, Model: f.model}, nil
}

func (f *fakeDaemonMemoryEmbedder) Provider() string { return f.provider }
func (f *fakeDaemonMemoryEmbedder) Model() string    { return f.model }
func (f *fakeDaemonMemoryEmbedder) Dimensions() int  { return len(f.vec) }

func TestMemoryEmbeddingDrainerProcessesQueuedMemoryJob(t *testing.T) {
	ctx := context.Background()
	fixture := newMemoryEmbeddingDrainFixture(t, "decision:daemon")
	embedder := &fakeDaemonMemoryEmbedder{
		vec:      []float32{0.1, 0.2, 0.3},
		provider: "openai_compat",
		model:    "text-embedding-qwen3-embedding-8b",
	}
	drainer := fixture.drainer(func(context.Context) (embedding.MemoryEmbedder, int, error) {
		return embedder, 3, nil
	})

	result := drainer.Drain(ctx)
	if result.Errors != 0 || result.Processed != 1 || result.Claimed != 1 {
		t.Fatalf("drain result=%#v, want one processed job", result)
	}
	if embedder.calls != 1 {
		t.Fatalf("embedder calls=%d want 1", embedder.calls)
	}
	if !strings.Contains(embedder.lastText, "daemon memory embedding") {
		t.Fatalf("embedder text=%q missing job content", embedder.lastText)
	}

	got, err := fixture.memory.GetEmbedding(ctx, fixture.memoryName, fixture.workspaceID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != 3 || got[0] != 0.1 || got[1] != 0.2 || got[2] != 0.3 {
		t.Fatalf("embedding=%v want [0.1 0.2 0.3]", got)
	}

	job, err := fixture.queue.GetJob(ctx, fixture.jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.State != embedding.StateOK {
		t.Fatalf("state=%q want ok", job.State)
	}
}

func TestMemoryEmbeddingDrainerFailsJobOnDimensionMismatch(t *testing.T) {
	ctx := context.Background()
	fixture := newMemoryEmbeddingDrainFixture(t, "decision:bad-dims")
	embedder := &fakeDaemonMemoryEmbedder{
		vec:      []float32{0.1, 0.2},
		provider: "openai_compat",
		model:    "text-embedding-qwen3-embedding-8b",
	}
	drainer := fixture.drainer(func(context.Context) (embedding.MemoryEmbedder, int, error) {
		return embedder, 3, nil
	})

	result := drainer.Drain(ctx)
	if result.Errors != 1 || result.Processed != 0 || result.Claimed != 1 {
		t.Fatalf("drain result=%#v, want one failed job", result)
	}
	if !strings.Contains(result.LastError, "dimension mismatch") {
		t.Fatalf("last error=%q want dimension mismatch", result.LastError)
	}

	job, err := fixture.queue.GetJob(ctx, fixture.jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.State != embedding.StateRetry {
		t.Fatalf("state=%q want retry", job.State)
	}
	if !strings.Contains(job.Error, "dimension mismatch") {
		t.Fatalf("job error=%q want dimension mismatch", job.Error)
	}

	got, err := fixture.memory.GetEmbedding(ctx, fixture.memoryName, fixture.workspaceID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if got != nil {
		t.Fatalf("embedding=%v want nil after failed drain", got)
	}
}

func TestMemoryEmbeddingDrainerRecoversStaleRunningMemoryJob(t *testing.T) {
	ctx := context.Background()
	fixture := newMemoryEmbeddingDrainFixture(t, "decision:stale")
	claimed, err := fixture.queue.ClaimNextInWorkspaceKind(ctx, fixture.workspaceID, embedqueue.TaskKindMemory)
	if err != nil {
		t.Fatalf("claim stale job: %v", err)
	}
	if claimed == nil {
		t.Fatalf("expected claimed stale job")
	}

	db, closeDB, err := sqliteutil.OpenDBShared(ctx, filepath.Join(fixture.queueRoot, "embedding_queue.db"), nil)
	if err != nil {
		t.Fatalf("open queue db: %v", err)
	}
	staleUpdatedAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := db.ExecContext(ctx, `UPDATE embedding_queue_jobs SET updated_at = ? WHERE id = ?`, staleUpdatedAt, claimed.ID); err != nil {
		_ = closeDB()
		t.Fatalf("mark stale job: %v", err)
	}
	if err := closeDB(); err != nil {
		t.Fatalf("close queue db: %v", err)
	}

	embedder := &fakeDaemonMemoryEmbedder{
		vec:      []float32{0.4, 0.5, 0.6},
		provider: "openai_compat",
		model:    "text-embedding-qwen3-embedding-8b",
	}
	drainer := fixture.drainer(func(context.Context) (embedding.MemoryEmbedder, int, error) {
		return embedder, 3, nil
	})
	drainer.recoverStaleAfter = time.Hour

	result := drainer.Drain(ctx)
	if result.Recovered != 1 || result.Processed != 1 || result.Errors != 0 {
		t.Fatalf("drain result=%#v, want one recovered and processed job", result)
	}
	if embedder.calls != 1 {
		t.Fatalf("embedder calls=%d want 1", embedder.calls)
	}

	job, err := fixture.queue.GetJob(ctx, fixture.jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.State != embedding.StateOK {
		t.Fatalf("state=%q want ok", job.State)
	}
	got, err := fixture.memory.GetEmbedding(ctx, fixture.memoryName, fixture.workspaceID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != 3 || got[0] != 0.4 || got[1] != 0.5 || got[2] != 0.6 {
		t.Fatalf("embedding=%v want [0.4 0.5 0.6]", got)
	}
}

type memoryEmbeddingDrainFixture struct {
	workspaceID string
	memoryName  string
	queueRoot   string
	memory      *memorystore.Store
	queue       *embedding.Store
	jobID       string
}

func newMemoryEmbeddingDrainFixture(t *testing.T, memoryName string) memoryEmbeddingDrainFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	queueRoot := filepath.Join(root, "cache")
	workspaceID := "ws-daemon"

	memory, err := memorystore.Open(ctx, root, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	t.Cleanup(func() { _ = memory.Close() })

	queue, err := embedding.OpenStore(ctx, queueRoot)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	if _, err := memory.SaveFromResult(ctx, memoryName, "decision", workspaceID,
		"Daemon memory embedding decision",
		[]byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-06-02T00:00:00Z"},"error":{}}`)); err != nil {
		t.Fatalf("save memory: %v", err)
	}
	queued, err := queue.EnqueueMemories(ctx, embedding.MemoryEnqueueRequest{
		WorkspaceID: workspaceID,
		Memories: []embedding.MemoryInput{{
			Name:    memoryName,
			Type:    "decision",
			Content: "daemon memory embedding content",
		}},
		Model: "text-embedding-qwen3-embedding-8b",
	})
	if err != nil {
		t.Fatalf("enqueue memory: %v", err)
	}
	if queued.Queued != 1 || len(queued.JobIDs) != 1 {
		t.Fatalf("queued=%#v want one job", queued)
	}
	return memoryEmbeddingDrainFixture{
		workspaceID: workspaceID,
		memoryName:  memoryName,
		queueRoot:   queueRoot,
		memory:      memory,
		queue:       queue,
		jobID:       queued.JobIDs[0],
	}
}

func (f memoryEmbeddingDrainFixture) drainer(factory func(context.Context) (embedding.MemoryEmbedder, int, error)) *memoryEmbeddingDrainer {
	return &memoryEmbeddingDrainer{
		queue:             f.queue,
		memory:            f.memory,
		workspaceID:       f.workspaceID,
		batchSize:         1,
		recoverStaleAfter: 0,
		embedderFactory: func(ctx context.Context, _ config.Config) (embedding.MemoryEmbedder, int, error) {
			return factory(ctx)
		},
	}
}
