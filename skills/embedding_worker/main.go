// Package main implements the embedding worker skill for processing queued embeddings.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/memoryutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/symbolutil"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/annotations"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

const (
	command            = "embedding/worker"
	defaultBatch       = 10
	defaultMaxDur      = 300 // 5 minutes
	defaultParallelism = 1
	maxParallelism     = 16
)

// Input is the skill input schema for embedding/worker operations.
type Input struct {
	// WorkspaceID restricts queue claiming, stats, and stale recovery to one queue group.
	WorkspaceID string `json:"workspace_id,omitempty"`

	// Kind restricts queue claiming and stats to one task kind (symbol or memory).
	Kind string `json:"kind,omitempty"`

	// BatchSize is the number of jobs to process per batch (default: 10).
	BatchSize int `json:"batch_size,omitempty"`

	// MaxDuration is the maximum processing time in seconds (default: 300).
	MaxDuration int `json:"max_duration,omitempty"`

	// ProcessAll loops until queue is empty or MaxDuration is reached.
	// When false, returns after processing BatchSize jobs.
	ProcessAll bool `json:"process_all,omitempty"`

	// Parallelism is the maximum number of claimed jobs processed concurrently.
	Parallelism int `json:"parallelism,omitempty"`

	// DryRun if true, claims jobs but doesn't call the embedding API.
	DryRun bool `json:"dry_run,omitempty"`

	// JobDelayMS pauses between claimed jobs, useful for local GPU-backed embedding.
	JobDelayMS int `json:"job_delay_ms,omitempty"`

	// RecoverStaleAfterSeconds moves running jobs older than this back to retry before processing.
	RecoverStaleAfterSeconds int `json:"recover_stale_after_seconds,omitempty"`

	// SyncMemory requests syncing symbol embeddings into named memory after processing.
	SyncMemory bool `json:"sync_memory,omitempty"`

	// SyncOnlyMissing controls whether sync only fills missing embeddings (default true).
	SyncOnlyMissing *bool `json:"sync_only_missing,omitempty"`

	// SyncWorkspace restricts sync to a specific workspace ID.
	SyncWorkspace string `json:"sync_workspace,omitempty"`

	// SyncAll forces a full workspace sync instead of only touched symbols.
	SyncAll bool `json:"sync_all,omitempty"`

	// ProcessAnnotations enables processing of session context-window annotations.
	ProcessAnnotations bool `json:"process_annotations,omitempty"`

	// AnnotationSessionID restricts annotation processing to a specific session.
	AnnotationSessionID string `json:"annotation_session_id,omitempty"`

	// AnnotationBackfill only processes windows missing embeddings and can expand
	// to recent sessions when annotation_session_id is empty.
	AnnotationBackfill bool `json:"annotation_backfill,omitempty"`

	// ProcessAnnotationQueue processes jobs from the annotation embedding queue
	// (populated by session/annotate with queue_embedding=true).
	ProcessAnnotationQueue bool `json:"process_annotation_queue,omitempty"`
}

// Output is the skill output for embedding/worker operations.
type Output struct {
	Processed   int            `json:"processed"`
	Kind        string         `json:"kind,omitempty"`
	Errors      int            `json:"errors"`
	Memories    int            `json:"memories,omitempty"`
	Synced      int            `json:"synced,omitempty"`
	SyncErrors  int            `json:"sync_errors,omitempty"`
	Recovered   int64          `json:"recovered,omitempty"`
	Remaining   int            `json:"remaining"`
	BatchCount  int            `json:"batch_count,omitempty"`
	Parallelism int            `json:"parallelism,omitempty"`
	Status      string         `json:"status"` // "completed", "timeout", "no_jobs", "error"
	DurationMs  int64          `json:"duration_ms"`
	LastError   string         `json:"last_error,omitempty"`
	Stats       *QueueSnapshot `json:"stats,omitempty"`
	Message     string         `json:"message"`
}

// QueueSnapshot is a summary of queue state after processing.
type QueueSnapshot struct {
	Queued     int `json:"queued"`
	Running    int `json:"running"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Embeddings int `json:"embeddings"`
}

// main is the skill entry point for embedding/worker.
func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates background embedding job processing with timeout and batch controls.
//
// Index:
//
//	Purpose: Process queued embedding jobs with batch processing, timeout handling, and error recovery
//	Flow: configure embedder → open queue store → claim workspace/kind-scoped jobs → generate embeddings → store results → lazy-open memory backend when needed
//	SideEffects: embedding API calls; job state transitions; queue statistics; dimension validation; optional named-memory updates
//	FailureModes: missing API keys, store errors, embedding failures, dimension mismatches, timeouts
//	Observability: emits processing statistics, error details, queue snapshots, and timing metrics
//	Keywords: embedding/worker, background, jobs, batch_processing, kind_filter, timeout, error_recovery
//
// [[domain:background-embedding-worker]]
// [[protocol:embedding-job-lifecycle]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	log := rc.Logger

	// Apply defaults
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatch
	}
	if in.MaxDuration <= 0 {
		in.MaxDuration = defaultMaxDur
	}
	parallelism, err := normalizeParallelism(in.Parallelism, in.BatchSize)
	if err != nil {
		return err
	}
	in.Parallelism = parallelism

	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	var symbolEmbedder *semantic.Embedder
	symbolEmbeddingModel := semantic.ResolveModelForScope(semantic.ScopeSymbols, rc.Config)
	symbolExpectedDims := semantic.ResolveDimensionsForModel(symbolEmbeddingModel, rc.Config.Embedding.Dimensions)
	var symbolEmbedderMu sync.Mutex
	getSymbolEmbedder := func() (*semantic.Embedder, int, error) {
		if in.DryRun {
			return nil, symbolExpectedDims, nil
		}
		symbolEmbedderMu.Lock()
		defer symbolEmbedderMu.Unlock()
		if symbolEmbedder != nil {
			return symbolEmbedder, symbolExpectedDims, nil
		}
		var err error
		symbolEmbedder, err = semantic.NewEmbedderFromConfig(
			semantic.ScopeSymbols,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			skillmain.EmbeddingGuard(rc),
		)
		if err != nil {
			return nil, 0, err
		}
		symbolExpectedDims = semantic.ResolveDimensionsForModel(symbolEmbedder.Model(), rc.Config.Embedding.Dimensions)
		log.Info().
			Str("provider", symbolEmbedder.Provider()).
			Str("model", symbolEmbedder.Model()).
			Int("dims", symbolExpectedDims).
			Msg("using symbol embeddings")
		return symbolEmbedder, symbolExpectedDims, nil
	}

	var memoryEmbedder *semantic.Embedder
	memoryEmbeddingModel := semantic.ResolveModelForScope(semantic.ScopeMemory, rc.Config)
	memoryExpectedDims := semantic.ResolveDimensionsForModel(memoryEmbeddingModel, rc.Config.Embedding.Dimensions)
	var memoryEmbedderMu sync.Mutex
	getMemoryEmbedder := func() (*semantic.Embedder, int, error) {
		if in.DryRun {
			return nil, memoryExpectedDims, nil
		}
		memoryEmbedderMu.Lock()
		defer memoryEmbedderMu.Unlock()
		if memoryEmbedder != nil {
			return memoryEmbedder, memoryExpectedDims, nil
		}
		var err error
		memoryEmbedder, err = semantic.NewEmbedderFromConfig(
			semantic.ScopeMemory,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			skillmain.EmbeddingGuard(rc),
		)
		if err != nil {
			return nil, 0, err
		}
		memoryExpectedDims = semantic.ResolveDimensionsForModel(memoryEmbedder.Model(), rc.Config.Embedding.Dimensions)
		log.Info().
			Str("provider", memoryEmbedder.Provider()).
			Str("model", memoryEmbedder.Model()).
			Int("dims", memoryExpectedDims).
			Msg("using memory embeddings")
		return memoryEmbedder, memoryExpectedDims, nil
	}

	// Open store using cache path from config
	store, err := embedding.OpenStore(ctx, rc.Config.Paths.Cache)
	if err != nil {
		return skillerr.IO("open store", skillerr.WithCause(err),
			skillerr.WithHint("check that the store path exists and has correct permissions: "+rc.Config.Paths.Cache))
	}
	defer store.Close()

	var memoryStore memoryutil.Store
	var memoryStoreMu sync.Mutex
	openMemoryStore := func() (memoryutil.Store, error) {
		memoryStoreMu.Lock()
		defer memoryStoreMu.Unlock()
		if memoryStore != nil {
			return memoryStore, nil
		}
		ms, memErr := memoryutil.OpenFromConfig(ctx, rc.Config)
		if memErr != nil {
			return nil, memErr
		}
		memoryStore = ms
		return memoryStore, nil
	}
	defer func() {
		if memoryStore != nil {
			_ = memoryStore.Close()
		}
	}()

	embeddingDBPath := filepath.Join(rc.Config.Paths.Cache, "embedding_queue.db")
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	kind, err := normalizeTaskKind(in.Kind)
	if err != nil {
		return err
	}
	recoveredStale := int64(0)
	if in.RecoverStaleAfterSeconds < 0 {
		return skillerr.Arg("recover_stale_after_seconds must be >= 0")
	}
	if in.RecoverStaleAfterSeconds > 0 {
		olderThan := time.Duration(in.RecoverStaleAfterSeconds) * time.Second
		var recoverErr error
		switch {
		case workspaceID != "" && kind != "":
			recoveredStale, recoverErr = store.RequeueStaleRunningInWorkspaceKind(ctx, workspaceID, kind, olderThan)
		case workspaceID != "":
			recoveredStale, recoverErr = store.RequeueStaleRunningInWorkspace(ctx, workspaceID, olderThan)
		case kind != "":
			recoveredStale, recoverErr = store.RequeueStaleRunningKind(ctx, kind, olderThan)
		default:
			recoveredStale, recoverErr = store.RequeueStaleRunning(ctx, olderThan)
		}
		if recoverErr != nil {
			return skillerr.IO("recover stale embedding jobs", skillerr.WithCause(recoverErr))
		}
		if recoveredStale > 0 {
			logEvent := log.Info().Int64("recovered", recoveredStale)
			if workspaceID != "" {
				logEvent = logEvent.Str("workspace_id", workspaceID)
			}
			if kind != "" {
				logEvent = logEvent.Str("kind", string(kind))
			}
			logEvent.Msg("recovered stale embedding jobs")
		}
	}
	syncTargets := make(map[string]map[string]struct{})
	processedWorkspaces := make(map[string]struct{})
	addSyncTarget := func(workspaceID, symbolID string) {
		workspaceID = strings.TrimSpace(workspaceID)
		symbolID = strings.TrimSpace(symbolID)
		if workspaceID == "" || symbolID == "" {
			return
		}
		targets := syncTargets[workspaceID]
		if targets == nil {
			targets = make(map[string]struct{})
			syncTargets[workspaceID] = targets
		}
		targets[symbolID] = struct{}{}
	}

	// Set up timeout
	deadline := time.Now().Add(time.Duration(in.MaxDuration) * time.Second)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	start := time.Now()
	output := Output{Status: "completed", Recovered: recoveredStale, Parallelism: in.Parallelism}
	if kind != "" {
		output.Kind = string(kind)
	}

	var memoryJobMu sync.Mutex
	processJob := func(ctx context.Context, job *embedding.EmbeddingJob) embeddingJobResult {
		result := embeddingJobResult{}
		if ws := strings.TrimSpace(job.WorkspaceID); ws != "" {
			result.ProcessedWorkspaces = append(result.ProcessedWorkspaces, ws)
		}

		if job.Kind == embedqueue.TaskKindMemory {
			memoryJobMu.Lock()
			defer memoryJobMu.Unlock()

			var ms memoryutil.Store
			if !in.DryRun {
				var memErr error
				ms, memErr = openMemoryStore()
				if memErr != nil {
					err := fmt.Errorf("open memory store: %w", memErr)
					log.Error().Err(err).Str("job_id", job.ID).Str("memory", job.MemoryName).Msg("memory embedding job failed")
					return failEmbeddingJob(ctx, store, job.ID, "fail memory job", err)
				}
			}
			if err := processMemoryEmbeddingJob(ctx, store, ms, job, in.DryRun, getMemoryEmbedder); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Str("memory", job.MemoryName).Msg("memory embedding job failed")
				return failEmbeddingJob(ctx, store, job.ID, "fail memory job", err)
			}
			log.Info().Str("job_id", job.ID).Str("memory", job.MemoryName).Str("status", "completed").Msg("memory job completed")
			result.Processed = 1
			result.Memories = 1
			if err := waitEmbeddingJobDelay(ctx, in.JobDelayMS); err != nil {
				result.Timeout = true
				result.LastError = err.Error()
			}
			return result
		}
		if job.Kind != "" && job.Kind != embedqueue.TaskKindSymbol {
			errMsg := fmt.Sprintf("unsupported embedding job kind %q", job.Kind)
			log.Error().Str("job_id", job.ID).Msg(errMsg)
			return failEmbeddingJob(ctx, store, job.ID, "fail job", fmt.Errorf("%s", errMsg))
		}

		if in.DryRun {
			_, expectedDims, err := getSymbolEmbedder()
			if err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to resolve dry-run embedding dimensions")
				return embeddingJobResult{Errors: 1, LastError: err.Error()}
			}
			fakeEmbed := make([]float32, expectedDims)
			if err := store.Complete(ctx, job.ID, fakeEmbed, "dry-run"); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to complete dry-run job")
				return embeddingJobResult{Errors: 1, LastError: err.Error()}
			}
			log.Info().Str("job_id", job.ID).Str("status", "dry-run").Int("dims", expectedDims).Msg("job completed")
		} else {
			embedder, expectedDims, err := getSymbolEmbedder()
			if err != nil {
				errMsg := fmt.Sprintf("symbol embedding provider: %v", err)
				log.Error().Err(err).Str("job_id", job.ID).Msg("symbol embedding provider unavailable")
				return failEmbeddingJob(ctx, store, job.ID, "fail job", fmt.Errorf("%s", errMsg))
			}

			embedResult, err := embedder.Embed(ctx, job.Content)
			if err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("embedding generation failed")
				return failEmbeddingJob(ctx, store, job.ID, "fail job", err)
			}

			embed := embedResult.Vec
			model := embedResult.Model
			if len(embed) != expectedDims {
				errMsg := fmt.Sprintf("dimension mismatch: got %d, expected %d from config; update embedding.model or embedding.dimensions", len(embed), expectedDims)
				log.Error().Str("job_id", job.ID).Msg(errMsg)
				return failEmbeddingJob(ctx, store, job.ID, "fail job", fmt.Errorf("%s", errMsg))
			}

			log.Info().
				Str("job_id", job.ID).
				Int("embedding_dim", len(embed)).
				Msg("embedding generated")

			if err := store.Complete(ctx, job.ID, embed, model); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to store embedding")
				return embeddingJobResult{Errors: 1, LastError: err.Error()}
			}
			if in.SyncMemory {
				workspaceID := strings.TrimSpace(job.WorkspaceID)
				entryName := symbolMemoryEntryName(job)
				if workspaceID == "" || entryName == "" {
					log.Warn().
						Str("job_id", job.ID).
						Str("symbol_id", job.SymbolID).
						Msg("skipping embedding update due to missing workspace/symbol identity")
					result.SyncTargets = append(result.SyncTargets, embeddingSyncTarget{WorkspaceID: workspaceID, SymbolID: job.SymbolID})
				} else {
					memoryJobMu.Lock()
					ms, memErr := openMemoryStore()
					if memErr != nil {
						log.Warn().Err(memErr).Str("job_id", job.ID).Str("symbol_id", job.SymbolID).Msg("failed to open memory store for symbol embedding update")
						result.SyncTargets = append(result.SyncTargets, embeddingSyncTarget{WorkspaceID: workspaceID, SymbolID: job.SymbolID})
					} else if err := ms.UpdateEmbedding(ctx, entryName, workspaceID, embed); err != nil {
						log.Warn().Err(err).Str("job_id", job.ID).Str("symbol_id", job.SymbolID).Msg("failed to update symbol embedding")
						result.SyncTargets = append(result.SyncTargets, embeddingSyncTarget{WorkspaceID: workspaceID, SymbolID: job.SymbolID})
					}
					memoryJobMu.Unlock()
				}
			}
			log.Info().Str("job_id", job.ID).Str("status", "completed").Str("model", model).Msg("job completed")
		}

		result.Processed = 1
		if err := waitEmbeddingJobDelay(ctx, in.JobDelayMS); err != nil {
			result.Timeout = true
			result.LastError = err.Error()
		}
		return result
	}

	// Claim jobs sequentially to preserve queue ordering, then process each claimed
	// batch with bounded parallelism.
	for {
		noMoreJobs := false
		jobs := make([]*embedding.EmbeddingJob, 0, in.BatchSize)

		for i := 0; i < in.BatchSize; i++ {
			if time.Now().After(deadline.Add(-5 * time.Second)) {
				output.Status = "timeout"
				log.Warn().Msg("approaching deadline, stopping processing")
				break
			}

			job, err := claimNextEmbeddingJob(ctx, store, workspaceID, kind)
			if err != nil {
				log.Error().Err(err).Msg("failed to claim job")
				output.LastError = err.Error()
				output.Errors++
				continue
			}

			if job == nil {
				// No more jobs
				noMoreJobs = true
				if output.Processed == 0 && output.Errors == 0 {
					output.Status = "no_jobs"
				}
				log.Debug().Msg("no more jobs in queue")
				break
			}

			contentPreview := job.Content
			if len(contentPreview) > 100 {
				contentPreview = contentPreview[:100] + "..."
			}
			log.Info().
				Str("job_id", job.ID).
				Str("content_preview", contentPreview).
				Msg("claimed job")
			jobs = append(jobs, job)
		}

		if len(jobs) > 0 {
			batchResult := processEmbeddingJobBatch(ctx, jobs, in.Parallelism, processJob)
			output.Processed += batchResult.Processed
			output.Memories += batchResult.Memories
			output.Errors += batchResult.Errors
			if batchResult.LastError != "" {
				output.LastError = batchResult.LastError
			}
			if batchResult.Timeout {
				output.Status = "timeout"
			}
			for _, ws := range batchResult.ProcessedWorkspaces {
				processedWorkspaces[ws] = struct{}{}
			}
			for _, target := range batchResult.SyncTargets {
				addSyncTarget(target.WorkspaceID, target.SymbolID)
			}
			if batchResult.Processed > 0 {
				output.BatchCount++
			}
		}

		// If not process_all, return after one batch
		if !in.ProcessAll {
			break
		}

		// If no more jobs or timeout, exit
		if noMoreJobs || output.Status == "timeout" {
			break
		}

		// Check context
		if ctx.Err() != nil {
			break
		}
	}

	annotationProcessed := 0
	if in.ProcessAnnotations {
		sessionStore, err := rc.Stores.Sessions(ctx)
		if err != nil {
			return skillerr.IO("open sessions store for annotations", skillerr.WithCause(err))
		}

		targetSessions := make([]string, 0, 16)
		if sid := strings.TrimSpace(in.AnnotationSessionID); sid != "" {
			targetSessions = append(targetSessions, sid)
		} else if in.AnnotationBackfill {
			opts := sessions.ListOptions{
				WorkspacePath: strings.TrimSpace(rc.Workspace),
				Limit:         200,
			}
			listed, err := sessionStore.List(ctx, opts)
			if err != nil {
				return skillerr.IO("list sessions for annotation backfill", skillerr.WithCause(err))
			}
			for _, s := range listed {
				targetSessions = append(targetSessions, s.ID)
			}
		} else {
			log.Warn().Msg("process_annotations requested without annotation_session_id; nothing to process")
		}

		var annotationEmbedder *semantic.Embedder
		if !in.DryRun && len(targetSessions) > 0 {
			var err error
			annotationEmbedder, err = semantic.NewEmbedderFromConfig(
				semantic.ScopeSessions,
				rc.Config,
				semantic.WithVoyageKey(voyageKey),
				semantic.WithGeminiKey(geminiKey),
				skillmain.EmbeddingGuard(rc),
			)
			if err != nil {
				return skillerr.Auth(
					"VOYAGE_API_KEY or GEMINI_API_KEY required for annotation embedding",
					skillerr.WithCause(err),
					skillerr.WithHint("set VOYAGE_API_KEY (preferred) or GEMINI_API_KEY environment variable"),
				)
			}
		}

		for _, sessionID := range targetSessions {
			windows, err := sessionStore.GetContextWindows(ctx, sessionID)
			if err != nil {
				log.Warn().Err(err).Str("session_id", sessionID).Msg("failed to list context windows for annotations")
				output.Errors++
				output.LastError = err.Error()
				continue
			}

			for _, win := range windows {
				embeddingText := strings.TrimSpace(win.Summary)
				if embeddingText == "" {
					continue
				}
				if in.AnnotationBackfill && len(win.Embedding) > 0 {
					continue
				}

				if in.DryRun {
					annotationProcessed++
					continue
				}
				if annotationEmbedder == nil {
					output.Errors++
					output.LastError = "annotation embedder not available"
					continue
				}

				result, err := annotationEmbedder.Embed(ctx, embeddingText)
				if err != nil {
					log.Warn().
						Err(err).
						Str("session_id", win.SessionID).
						Int("window_index", win.WindowIndex).
						Msg("annotation embedding generation failed")
					output.Errors++
					output.LastError = err.Error()
					continue
				}

				annExpectedDims := annotationEmbedder.Dimensions()
				if annExpectedDims > 0 && len(result.Vec) != annExpectedDims {
					output.Errors++
					output.LastError = fmt.Sprintf("annotation dimension mismatch: got %d, expected %d", len(result.Vec), annExpectedDims)
					continue
				}

				if err := sessionStore.SetContextWindowEmbedding(ctx, win.ID, vector.SerializeF32(result.Vec), result.Model); err != nil {
					log.Warn().
						Err(err).
						Str("session_id", win.SessionID).
						Int("window_index", win.WindowIndex).
						Msg("failed to store annotation embedding")
					output.Errors++
					output.LastError = err.Error()
					continue
				}

				annotationProcessed++
			}
		}

		if annotationProcessed > 0 {
			output.Processed += annotationProcessed
			if output.Status == "no_jobs" {
				output.Status = "completed"
			}
		}
		fmt.Fprintf(os.Stderr, "processed %d annotation embeddings\n", annotationProcessed)
	}

	// Process annotation queue (from session/annotate queue_embedding=true)
	if in.ProcessAnnotationQueue {
		annStore, annErr := annotations.Open(ctx, "")
		if annErr != nil {
			fmt.Fprintf(os.Stderr, "WARN: cannot open annotations store: %v\n", annErr)
		} else {
			defer annStore.Close()

			annQueue, qErr := annotations.OpenQueue(ctx, rc.Config.Storage.Root)
			if qErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: cannot open annotation queue: %v\n", qErr)
			} else {
				defer annQueue.Close()

				var annEmbedder *semantic.Embedder
				if !in.DryRun {
					annEmbedder, err = semantic.NewEmbedderFromConfig(
						semantic.ScopeSessions,
						rc.Config,
						semantic.WithVoyageKey(voyageKey),
						semantic.WithGeminiKey(geminiKey),
						skillmain.EmbeddingGuard(rc),
					)
					if err != nil {
						fmt.Fprintf(os.Stderr, "WARN: cannot create annotation embedder: %v — skipping annotation queue\n", err)
					}
				}

				annQueueProcessed := 0

				for {
					// Check deadline
					if time.Now().After(deadline.Add(-5 * time.Second)) {
						output.Status = "timeout"
						break
					}
					job, claimErr := annQueue.ClaimNext(ctx)
					if claimErr != nil {
						fmt.Fprintf(os.Stderr, "WARN: annotation queue claim: %v\n", claimErr)
						output.Errors++
						break
					}
					if job == nil {
						break // queue empty
					}

					if in.DryRun {
						_ = annQueue.Complete(ctx, job.ID)
						annQueueProcessed++
						continue
					}

					if annEmbedder == nil {
						_ = annQueue.Fail(ctx, job.ID, "embedder not available")
						output.Errors++
						break // no point continuing — every job will fail
					}

					result, embErr := annEmbedder.Embed(ctx, job.EmbeddingText)
					if embErr != nil {
						_ = annQueue.Fail(ctx, job.ID, embErr.Error())
						fmt.Fprintf(os.Stderr, "WARN: annotation embed failed %s:%d: %v\n", job.SessionID, job.TurnIndex, embErr)
						output.Errors++
						continue
					}

					annExpectedDims := annEmbedder.Dimensions()
					if annExpectedDims > 0 && len(result.Vec) != annExpectedDims {
						_ = annQueue.Fail(ctx, job.ID, fmt.Sprintf("dimension mismatch: got %d, expected %d", len(result.Vec), annExpectedDims))
						output.Errors++
						continue
					}

					if setErr := annStore.SetEmbedding(ctx, job.SessionID, job.TurnIndex, result.Vec, annEmbedder.Model(), job.EmbeddingText); setErr != nil {
						_ = annQueue.Fail(ctx, job.ID, setErr.Error())
						output.Errors++
						continue
					}

					_ = annQueue.Complete(ctx, job.ID)
					annQueueProcessed++
				}

				output.Processed += annQueueProcessed
				if annQueueProcessed > 0 && output.Status == "no_jobs" {
					output.Status = "completed"
				}
				fmt.Fprintf(os.Stderr, "processed %d annotation queue jobs\n", annQueueProcessed)
			}
		}
	}

	if in.SyncMemory {
		syncOnlyMissing := true
		if in.SyncOnlyMissing != nil {
			syncOnlyMissing = *in.SyncOnlyMissing
		}

		if in.DryRun {
			log.Warn().Msg("sync_memory requested during dry_run; skipping sync")
		} else {
			ms, memErr := openMemoryStore()
			if memErr != nil {
				output.SyncErrors++
				output.LastError = memErr.Error()
				log.Warn().Err(memErr).Msg("sync_memory requested but memory store could not be opened")
			} else if syncStore, ok := ms.(memoryutil.SymbolSyncStore); !ok {
				log.Warn().Msg("sync_memory requested but memory store does not support symbol sync")
			} else {
				if len(syncTargets) > 0 {
					for ws, symbols := range syncTargets {
						ids := make([]string, 0, len(symbols))
						for id := range symbols {
							ids = append(ids, id)
						}
						updated, err := syncStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, memoryutil.SyncSymbolEmbeddingsOptions{
							WorkspaceID: ws,
							SymbolIDs:   ids,
							OnlyMissing: syncOnlyMissing,
						})
						if err != nil {
							output.SyncErrors++
							log.Warn().Err(err).Str("workspace", ws).Msg("failed to sync symbol embeddings")
							continue
						}
						if updated > 0 {
							output.Synced += updated
							log.Info().Int("synced", updated).Str("workspace", ws).Msg("synced symbol embeddings")
						}
					}
				}

				syncWorkspaces := make(map[string]struct{})
				if ws := strings.TrimSpace(in.SyncWorkspace); ws != "" {
					syncWorkspaces[ws] = struct{}{}
				} else {
					for ws := range processedWorkspaces {
						syncWorkspaces[ws] = struct{}{}
					}
				}
				if len(syncWorkspaces) == 0 {
					log.Warn().Msg("no workspaces available for sync_memory")
				} else if in.SyncAll {
					for ws := range syncWorkspaces {
						updated, err := syncStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, memoryutil.SyncSymbolEmbeddingsOptions{
							WorkspaceID: ws,
							OnlyMissing: syncOnlyMissing,
						})
						if err != nil {
							output.SyncErrors++
							log.Warn().Err(err).Str("workspace", ws).Msg("failed to sync workspace embeddings")
							continue
						}
						if updated > 0 {
							output.Synced += updated
							log.Info().Int("synced", updated).Str("workspace", ws).Msg("synced workspace embeddings")
						}
					}
				} else if len(syncTargets) == 0 {
					if ws := strings.TrimSpace(in.SyncWorkspace); ws != "" {
						updated, err := syncStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, memoryutil.SyncSymbolEmbeddingsOptions{
							WorkspaceID: ws,
							OnlyMissing: syncOnlyMissing,
						})
						if err != nil {
							output.SyncErrors++
							log.Warn().Err(err).Str("workspace", ws).Msg("failed to sync embeddings for workspace")
						} else if updated > 0 {
							output.Synced += updated
							log.Info().Int("synced", updated).Str("workspace", ws).Msg("synced workspace embeddings")
						}
					}
				}
			}
		}
	}

	output.DurationMs = time.Since(start).Milliseconds()

	// Get final stats
	stats, err := embeddingQueueStats(ctx, store, workspaceID, kind)
	if err == nil {
		output.Stats = &QueueSnapshot{
			Queued:     stats.QueuedCount,
			Running:    stats.RunningCount,
			Completed:  stats.CompletedCount,
			Failed:     stats.FailedCount,
			Embeddings: stats.EmbeddingsCount,
		}
		output.Remaining = stats.QueuedCount
	}

	// Build message
	switch output.Status {
	case "completed":
		output.Message = fmt.Sprintf("Processed %d embeddings (%d errors) in %dms",
			output.Processed, output.Errors, output.DurationMs)
	case "timeout":
		output.Message = fmt.Sprintf("Timeout after %d embeddings (%d errors, %d remaining)",
			output.Processed, output.Errors, output.Remaining)
	case "no_jobs":
		output.Message = "No jobs in queue"
	default:
		output.Message = fmt.Sprintf("Worker finished: %s", output.Status)
	}
	if output.Recovered > 0 {
		output.Message = fmt.Sprintf("%s; recovered %d stale jobs", output.Message, output.Recovered)
	}

	return skillout.Emit(rc, command, output)
}

func normalizeTaskKind(raw string) (embedqueue.TaskKind, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	kind := embedqueue.TaskKind(raw)
	switch kind {
	case embedqueue.TaskKindSymbol, embedqueue.TaskKindMemory:
		return kind, nil
	default:
		return "", skillerr.Arg("kind must be one of: symbol, memory")
	}
}

func claimNextEmbeddingJob(ctx context.Context, store *embedding.Store, workspaceID string, kind embedqueue.TaskKind) (*embedding.EmbeddingJob, error) {
	if kind != "" {
		if strings.TrimSpace(workspaceID) == "" {
			return store.ClaimNextKind(ctx, kind)
		}
		return store.ClaimNextInWorkspaceKind(ctx, workspaceID, kind)
	}
	if strings.TrimSpace(workspaceID) == "" {
		return store.ClaimNext(ctx)
	}
	return store.ClaimNextInWorkspace(ctx, workspaceID)
}

func embeddingQueueStats(ctx context.Context, store *embedding.Store, workspaceID string, kind embedqueue.TaskKind) (*embedding.QueueStats, error) {
	if kind != "" {
		if strings.TrimSpace(workspaceID) == "" {
			return store.StatsKind(ctx, kind)
		}
		return store.StatsInWorkspaceKind(ctx, workspaceID, kind)
	}
	if strings.TrimSpace(workspaceID) == "" {
		return store.Stats(ctx)
	}
	return store.StatsInWorkspace(ctx, workspaceID)
}

type embeddingSyncTarget struct {
	WorkspaceID string
	SymbolID    string
}

type embeddingJobResult struct {
	Processed           int
	Memories            int
	Errors              int
	LastError           string
	Timeout             bool
	ProcessedWorkspaces []string
	SyncTargets         []embeddingSyncTarget
}

func processEmbeddingJobBatch(
	ctx context.Context,
	jobs []*embedding.EmbeddingJob,
	parallelism int,
	process func(context.Context, *embedding.EmbeddingJob) embeddingJobResult,
) embeddingJobResult {
	if len(jobs) == 0 {
		return embeddingJobResult{}
	}
	if parallelism <= 1 || len(jobs) == 1 {
		var batch embeddingJobResult
		for _, job := range jobs {
			batch.merge(process(ctx, job))
		}
		return batch
	}
	if parallelism > len(jobs) {
		parallelism = len(jobs)
	}

	jobCh := make(chan *embedding.EmbeddingJob)
	resultCh := make(chan embeddingJobResult, len(jobs))
	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				resultCh <- process(ctx, job)
			}
		}()
	}
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	wg.Wait()
	close(resultCh)

	var batch embeddingJobResult
	for result := range resultCh {
		batch.merge(result)
	}
	return batch
}

func (r *embeddingJobResult) merge(other embeddingJobResult) {
	r.Processed += other.Processed
	r.Memories += other.Memories
	r.Errors += other.Errors
	if other.LastError != "" {
		r.LastError = other.LastError
	}
	r.Timeout = r.Timeout || other.Timeout
	r.ProcessedWorkspaces = append(r.ProcessedWorkspaces, other.ProcessedWorkspaces...)
	r.SyncTargets = append(r.SyncTargets, other.SyncTargets...)
}

func failEmbeddingJob(ctx context.Context, store *embedding.Store, jobID, action string, err error) embeddingJobResult {
	if failErr := store.Fail(ctx, jobID, err.Error()); failErr != nil {
		return embeddingJobResult{
			Errors:    1,
			LastError: fmt.Sprintf("%s: %v (original: %v)", action, failErr, err),
		}
	}
	return embeddingJobResult{Errors: 1, LastError: err.Error()}
}

func normalizeParallelism(raw, batchSize int) (int, error) {
	if raw < 0 {
		return 0, skillerr.Arg("parallelism must be >= 0")
	}
	if raw == 0 {
		raw = defaultParallelism
	}
	if raw > maxParallelism {
		return 0, skillerr.Arg(fmt.Sprintf("parallelism must be <= %d", maxParallelism))
	}
	if batchSize > 0 && raw > batchSize {
		return batchSize, nil
	}
	return raw, nil
}

func processMemoryEmbeddingJob(
	ctx context.Context,
	store *embedding.Store,
	memoryStore memoryutil.Store,
	job *embedding.EmbeddingJob,
	dryRun bool,
	getEmbedder func() (*semantic.Embedder, int, error),
) error {
	if strings.TrimSpace(job.MemoryName) == "" {
		return fmt.Errorf("memory_name is required")
	}
	if strings.TrimSpace(job.WorkspaceID) == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if dryRun {
		return store.CompleteJob(ctx, job.ID)
	}
	if memoryStore == nil {
		return fmt.Errorf("memory store unavailable")
	}
	embedder, expectedDims, err := getEmbedder()
	if err != nil {
		return fmt.Errorf("memory embedding provider: %w", err)
	}
	if embedder == nil {
		return fmt.Errorf("memory embedding provider not available")
	}
	result, err := embedder.Embed(ctx, job.Content)
	if err != nil {
		return fmt.Errorf("embed memory: %w", err)
	}
	if expectedDims > 0 && len(result.Vec) != expectedDims {
		return fmt.Errorf("memory dimension mismatch: got %d, expected %d", len(result.Vec), expectedDims)
	}
	if err := memoryStore.ValidateEmbeddingDimensions(ctx, job.WorkspaceID, len(result.Vec)); err != nil {
		return err
	}
	now := time.Now().UTC()
	createdAt := job.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	if err := memoryStore.SetEmbeddingMetadata(ctx, storage.EmbeddingMetadata{
		Workspace:  job.WorkspaceID,
		Provider:   embedder.Provider(),
		Model:      result.Model,
		Dimensions: len(result.Vec),
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}); err != nil {
		return fmt.Errorf("set memory embedding metadata: %w", err)
	}
	if err := memoryStore.UpdateEmbedding(ctx, job.MemoryName, job.WorkspaceID, result.Vec); err != nil {
		return fmt.Errorf("update memory embedding: %w", err)
	}
	if err := store.CompleteJob(ctx, job.ID); err != nil {
		return fmt.Errorf("complete memory job: %w", err)
	}
	return nil
}

func symbolMemoryEntryName(job *embedding.EmbeddingJob) string {
	if job == nil {
		return ""
	}
	if name := strings.TrimSpace(job.MemoryName); name != "" {
		return name
	}
	workspaceID := strings.TrimSpace(job.WorkspaceID)
	packageID := strings.TrimSpace(job.PackageID)
	symbolKey := strings.TrimSpace(job.SymbolKey)
	if workspaceID != "" && packageID != "" && symbolKey != "" {
		return symbolutil.KeyEntryName(workspaceID, packageID, symbolKey)
	}
	filePath := strings.TrimSpace(job.FilePath)
	symbolName := strings.TrimSpace(job.SymbolName)
	if workspaceID != "" && filePath != "" && symbolName != "" {
		return symbolutil.EntryName(workspaceID, filePath, symbolName)
	}
	return ""
}

func waitEmbeddingJobDelay(ctx context.Context, delayMS int) error {
	if delayMS <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
