// Package main implements the embedding worker skill for processing queued embeddings.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/memoryutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/symbolutil"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage/annotations"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

const (
	command       = "embedding/worker"
	defaultBatch  = 10
	defaultMaxDur = 300 // 5 minutes
)

// Input is the skill input schema for embedding/worker operations.
type Input struct {
	// BatchSize is the number of jobs to process per batch (default: 10).
	BatchSize int `json:"batch_size,omitempty"`

	// MaxDuration is the maximum processing time in seconds (default: 300).
	MaxDuration int `json:"max_duration,omitempty"`

	// ProcessAll loops until queue is empty or MaxDuration is reached.
	// When false, returns after processing BatchSize jobs.
	ProcessAll bool `json:"process_all,omitempty"`

	// DryRun if true, claims jobs but doesn't call the embedding API.
	DryRun bool `json:"dry_run,omitempty"`

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
	Processed  int            `json:"processed"`
	Errors     int            `json:"errors"`
	Synced     int            `json:"synced,omitempty"`
	SyncErrors int            `json:"sync_errors,omitempty"`
	Remaining  int            `json:"remaining"`
	BatchCount int            `json:"batch_count,omitempty"`
	Status     string         `json:"status"` // "completed", "timeout", "no_jobs", "error"
	DurationMs int64          `json:"duration_ms"`
	LastError  string         `json:"last_error,omitempty"`
	Stats      *QueueSnapshot `json:"stats,omitempty"`
	Message    string         `json:"message"`
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
//	Flow: configure embedder → open store → claim jobs → generate embeddings → store results → repeat until done
//	SideEffects: embedding API calls; job state transitions; queue statistics; dimension validation
//	FailureModes: missing API keys, store errors, embedding failures, dimension mismatches, timeouts
//	Observability: emits processing statistics, error details, queue snapshots, and timing metrics
//	Keywords: embedding/worker, background, jobs, batch_processing, timeout, error_recovery
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

	// Determine embedding provider (prefer Voyage, fall back to Gemini).
	var embeddingModel string
	var expectedDims int

	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	model := semantic.ResolveModelForScope(semantic.ScopeSymbols, rc.Config)

	var embedder *semantic.Embedder
	if !in.DryRun {
		var err error
		embedder, err = semantic.NewEmbedderFromConfig(
			semantic.ScopeSymbols,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			skillmain.EmbeddingGuard(rc),
		)
		if err != nil {
			return skillerr.Auth(
				"VOYAGE_API_KEY or GEMINI_API_KEY required",
				skillerr.WithCause(err),
				skillerr.WithHint("set VOYAGE_API_KEY (preferred) or GEMINI_API_KEY environment variable"),
			)
		}
	} else {
		embedder, _ = semantic.NewEmbedderFromConfig(
			semantic.ScopeSymbols,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			skillmain.EmbeddingGuard(rc),
		)
	}

	if embedder != nil {
		embeddingModel = embedder.Model()
		expectedDims = embedder.Dimensions()
		log.Info().
			Str("provider", embedder.Provider()).
			Str("model", embeddingModel).
			Int("dims", expectedDims).
			Msg("using embeddings")
	} else {
		embeddingModel = model
		if embeddingModel == "" {
			embeddingModel = "gemini-embedding-001"
		}
		expectedDims = semantic.DimensionsForModel(embeddingModel)
		log.Info().
			Str("provider", "dry-run").
			Str("model", embeddingModel).
			Int("dims", expectedDims).
			Msg("using embeddings")
	}

	if rc.Config.Embedding.Dimensions > 0 {
		expectedDims = rc.Config.Embedding.Dimensions
	}

	// Open store using cache path from config
	store, err := embedding.OpenStore(ctx, rc.Config.Paths.Cache)
	if err != nil {
		return skillerr.IO("open store", skillerr.WithCause(err),
			skillerr.WithHint("check that the store path exists and has correct permissions: "+rc.Config.Paths.Cache))
	}
	defer store.Close()

	var memoryStore memoryutil.Store
	if ms, memErr := memoryutil.OpenFromConfig(ctx, rc.Config); memErr != nil {
		log.Warn().Err(memErr).Msg("failed to open memory store for embedding updates")
	} else {
		memoryStore = ms
		defer memoryStore.Close()
	}

	embeddingDBPath := filepath.Join(rc.Config.Paths.Cache, "embedding_queue.db")
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
	output := Output{Status: "completed"}

	// Process symbol queue jobs in batches
	for {
		batchProcessed := 0
		noMoreJobs := false

		for i := 0; i < in.BatchSize; i++ {
			// Check deadline
			if time.Now().After(deadline.Add(-5 * time.Second)) {
				output.Status = "timeout"
				log.Warn().Msg("approaching deadline, stopping processing")
				break
			}

			// Claim next job
			job, err := store.ClaimNext(ctx)
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

			// Log job claimed with content preview
			contentPreview := job.Content
			if len(contentPreview) > 100 {
				contentPreview = contentPreview[:100] + "..."
			}
			log.Info().
				Str("job_id", job.ID).
				Str("content_preview", contentPreview).
				Msg("claimed job")

			if ws := strings.TrimSpace(job.WorkspaceID); ws != "" {
				processedWorkspaces[ws] = struct{}{}
			}

			// Generate embedding
			if in.DryRun {
				// Dry run: mark as complete with fake embedding using config dimensions
				fakeEmbed := make([]float32, expectedDims)
				if err := store.Complete(ctx, job.ID, fakeEmbed, "dry-run"); err != nil {
					log.Error().Err(err).Str("job_id", job.ID).Msg("failed to complete dry-run job")
					output.LastError = err.Error()
					output.Errors++
					continue
				}
				log.Info().Str("job_id", job.ID).Str("status", "dry-run").Int("dims", expectedDims).Msg("job completed")
			} else {
				if embedder == nil {
					output.LastError = "embedding provider not available"
					output.Errors++
					log.Error().Str("job_id", job.ID).Msg("embedding provider not available")
					continue
				}

				result, err := embedder.Embed(ctx, job.Content)
				if err != nil {
					log.Error().Err(err).Str("job_id", job.ID).Msg("embedding generation failed")
					// Rate limit or API error - fail with retry
					if failErr := store.Fail(ctx, job.ID, err.Error()); failErr != nil {
						output.LastError = fmt.Sprintf("fail job: %v (original: %v)", failErr, err)
					} else {
						output.LastError = err.Error()
					}
					output.Errors++
					continue
				}

				embed := result.Vec
				model := result.Model

				// Validate embedding dimensions match config
				if len(embed) != expectedDims {
					errMsg := fmt.Sprintf("dimension mismatch: got %d, expected %d from config; update embedding.model or embedding.dimensions", len(embed), expectedDims)
					log.Error().Str("job_id", job.ID).Msg(errMsg)
					if failErr := store.Fail(ctx, job.ID, errMsg); failErr != nil {
						output.LastError = fmt.Sprintf("fail job: %v (original: %v)", failErr, errMsg)
					} else {
						output.LastError = errMsg
					}
					output.Errors++
					continue
				}

				log.Info().
					Str("job_id", job.ID).
					Int("embedding_dim", len(embed)).
					Msg("embedding generated")

				// Store the embedding with the model used for generation.
				if err := store.Complete(ctx, job.ID, embed, model); err != nil {
					log.Error().Err(err).Str("job_id", job.ID).Msg("failed to store embedding")
					output.LastError = err.Error()
					output.Errors++
					continue
				}
				if memoryStore != nil {
					workspaceID := strings.TrimSpace(job.WorkspaceID)
					filePath := strings.TrimSpace(job.FilePath)
					symbolName := strings.TrimSpace(job.SymbolName)
					if workspaceID == "" || filePath == "" || symbolName == "" {
						log.Warn().
							Str("job_id", job.ID).
							Str("symbol_id", job.SymbolID).
							Msg("skipping embedding update due to missing workspace/file/symbol")
						addSyncTarget(workspaceID, job.SymbolID)
					} else {
						entryName := symbolutil.EntryName(workspaceID, filePath, symbolName)
						if err := memoryStore.UpdateEmbedding(ctx, entryName, workspaceID, embed); err != nil {
							log.Warn().Err(err).Str("job_id", job.ID).Str("symbol_id", job.SymbolID).Msg("failed to update symbol embedding")
							addSyncTarget(workspaceID, job.SymbolID)
						}
					}
				}
				log.Info().Str("job_id", job.ID).Str("status", "completed").Str("model", model).Msg("job completed")
			}

			output.Processed++
			batchProcessed++
		}

		// Only count batch if we processed at least one job
		if batchProcessed > 0 {
			output.BatchCount++
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

	if memoryStore != nil {
		syncOnlyMissing := true
		if in.SyncOnlyMissing != nil {
			syncOnlyMissing = *in.SyncOnlyMissing
		}

		if in.SyncMemory {
			if in.DryRun {
				log.Warn().Msg("sync_memory requested during dry_run; skipping sync")
			} else {
				if len(syncTargets) > 0 {
					for ws, symbols := range syncTargets {
						ids := make([]string, 0, len(symbols))
						for id := range symbols {
							ids = append(ids, id)
						}
						updated, err := memoryStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, memoryutil.SyncSymbolEmbeddingsOptions{
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
						updated, err := memoryStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, memoryutil.SyncSymbolEmbeddingsOptions{
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
						updated, err := memoryStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, memoryutil.SyncSymbolEmbeddingsOptions{
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
	stats, err := store.Stats(ctx)
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

	return skillout.Emit(rc, command, output)
}
