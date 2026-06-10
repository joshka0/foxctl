package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	defaultMemoryEmbeddingBatchSize        = 1
	defaultMemoryEmbeddingRecoverStaleTime = 10 * time.Minute
)

type memoryEmbeddingDrainer struct {
	queue             *embedding.Store
	memory            storage.MemoryStore
	workspaceID       string
	cfg               config.Config
	batchSize         int
	recoverStaleAfter time.Duration
	embedderFactory   func(context.Context, config.Config) (embedding.MemoryEmbedder, int, error)
}

type memoryEmbeddingDrainResult struct {
	Recovered int64
	Claimed   int
	Processed int
	Errors    int
	LastError string
}

func newMemoryEmbeddingDrainer(stores *daemonStores, opts Options) *memoryEmbeddingDrainer {
	if stores == nil || stores.embeddingStore == nil || stores.namedMemoryStore == nil {
		return nil
	}
	workspaceID := workspaceutil.CanonicalID(opts.WorkspaceRoot)
	if strings.TrimSpace(workspaceID) == "" {
		return nil
	}
	cfg := daemonEmbeddingConfig(opts)
	batchSize := opts.MemoryEmbeddingBatchSize
	if batchSize <= 0 {
		batchSize = defaultMemoryEmbeddingBatchSize
	}
	recoverStaleAfter := opts.MemoryEmbeddingRecoverStaleAfter
	if recoverStaleAfter <= 0 {
		recoverStaleAfter = defaultMemoryEmbeddingRecoverStaleTime
	}
	factory := opts.MemoryEmbedderFactory
	if factory == nil {
		factory = newSemanticMemoryEmbedder
	}
	return &memoryEmbeddingDrainer{
		queue:             stores.embeddingStore,
		memory:            stores.namedMemoryStore,
		workspaceID:       workspaceID,
		cfg:               cfg,
		batchSize:         batchSize,
		recoverStaleAfter: recoverStaleAfter,
		embedderFactory:   factory,
	}
}

func (d *memoryEmbeddingDrainer) Drain(ctx context.Context) memoryEmbeddingDrainResult {
	var result memoryEmbeddingDrainResult
	if d == nil || d.queue == nil || d.memory == nil || strings.TrimSpace(d.workspaceID) == "" {
		return result
	}
	if d.recoverStaleAfter > 0 {
		recovered, err := d.queue.RequeueStaleRunningInWorkspaceKind(ctx, d.workspaceID, embedqueue.TaskKindMemory, d.recoverStaleAfter)
		if err != nil {
			result.Errors++
			result.LastError = fmt.Sprintf("recover stale memory embedding jobs: %v", err)
			return result
		}
		result.Recovered = recovered
	}

	batchSize := d.batchSize
	if batchSize <= 0 {
		batchSize = defaultMemoryEmbeddingBatchSize
	}
	var proc *embedding.MemoryJobProcessor
	for i := 0; i < batchSize; i++ {
		job, err := d.queue.ClaimNextInWorkspaceKind(ctx, d.workspaceID, embedqueue.TaskKindMemory)
		if err != nil {
			result.Errors++
			result.LastError = fmt.Sprintf("claim memory embedding job: %v", err)
			return result
		}
		if job == nil {
			return result
		}
		result.Claimed++
		if proc == nil {
			proc, err = d.newProcessor(ctx)
			if err != nil {
				failErr := d.queue.Fail(ctx, job.ID, err.Error())
				result.Errors++
				result.LastError = err.Error()
				if failErr != nil {
					result.LastError = fmt.Sprintf("fail memory embedding job: %v (original: %v)", failErr, err)
				}
				return result
			}
		}
		if err := proc.Process(ctx, job); err != nil {
			failErr := d.queue.Fail(ctx, job.ID, err.Error())
			result.Errors++
			result.LastError = err.Error()
			if failErr != nil {
				result.LastError = fmt.Sprintf("fail memory embedding job: %v (original: %v)", failErr, err)
			}
			continue
		}
		result.Processed++
	}
	return result
}

func (d *memoryEmbeddingDrainer) newProcessor(ctx context.Context) (*embedding.MemoryJobProcessor, error) {
	if d.embedderFactory == nil {
		return nil, fmt.Errorf("memory embedding provider factory unavailable")
	}
	embedder, expectedDimensions, err := d.embedderFactory(ctx, d.cfg)
	if err != nil {
		return nil, fmt.Errorf("memory embedding provider: %w", err)
	}
	return &embedding.MemoryJobProcessor{
		Store:              d.queue,
		Memory:             d.memory,
		Embedder:           embedder,
		ExpectedDimensions: expectedDimensions,
	}, nil
}

func newSemanticMemoryEmbedder(_ context.Context, cfg config.Config) (embedding.MemoryEmbedder, int, error) {
	model := semantic.ResolveModelForScope(semantic.ScopeMemory, cfg)
	expectedDimensions := semantic.ResolveDimensionsForModel(model, cfg.Embedding.Dimensions)
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, cfg)
	if err != nil {
		return nil, expectedDimensions, err
	}
	expectedDimensions = semantic.ResolveDimensionsForModel(embedder.Model(), cfg.Embedding.Dimensions)
	return daemonSemanticMemoryEmbedder{embedder: embedder}, expectedDimensions, nil
}

type daemonSemanticMemoryEmbedder struct {
	embedder *semantic.Embedder
}

func (e daemonSemanticMemoryEmbedder) Embed(ctx context.Context, text string) (embedding.MemoryEmbedding, error) {
	if e.embedder == nil {
		return embedding.MemoryEmbedding{}, fmt.Errorf("memory embedding provider not available")
	}
	result, err := e.embedder.Embed(ctx, text)
	if err != nil {
		return embedding.MemoryEmbedding{}, err
	}
	return embedding.MemoryEmbedding{Vec: result.Vec, Model: result.Model}, nil
}

func (e daemonSemanticMemoryEmbedder) Provider() string {
	if e.embedder == nil {
		return ""
	}
	return e.embedder.Provider()
}

func (e daemonSemanticMemoryEmbedder) Model() string {
	if e.embedder == nil {
		return ""
	}
	return e.embedder.Model()
}

func (e daemonSemanticMemoryEmbedder) Dimensions() int {
	if e.embedder == nil {
		return 0
	}
	return e.embedder.Dimensions()
}

func daemonEmbeddingConfig(opts Options) config.Config {
	cfg := opts.Config
	if strings.TrimSpace(cfg.Storage.Root) == "" {
		cfg.Storage.Root = opts.StorageRoot
	}
	if strings.TrimSpace(cfg.Paths.Cache) == "" {
		cfg.Paths.Cache = opts.StorageRoot
	}
	return cfg
}

func embeddingQueueRootForOptions(defaultRoot string, opts Options) string {
	cfg := daemonEmbeddingConfig(opts)
	if root := strings.TrimSpace(cfg.Paths.Cache); root != "" {
		return root
	}
	return defaultRoot
}

func logMemoryEmbeddingDrainResult(logger zerolog.Logger, result memoryEmbeddingDrainResult) {
	if result.Recovered == 0 && result.Claimed == 0 && result.Processed == 0 && result.Errors == 0 {
		return
	}
	event := logger.Info().
		Int64("recovered", result.Recovered).
		Int("claimed", result.Claimed).
		Int("processed", result.Processed).
		Int("errors", result.Errors)
	if result.LastError != "" {
		event = event.Str("last_error", result.LastError)
	}
	event.Msg("memory embedding queue drain tick")
}
