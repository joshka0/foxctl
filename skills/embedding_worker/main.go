// Package main implements the embedding worker skill for processing queued embeddings.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/embedding"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
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
}

// Output is the skill output for embedding/worker operations.
type Output struct {
	Processed  int            `json:"processed"`
	Errors     int            `json:"errors"`
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
	skillmain.Main(command, run)
}

// run orchestrates background embedding job processing with timeout and batch controls.
//
// Index:
// - Purpose: Process queued embedding jobs with batch processing, timeout handling, and error recovery
// - Flow: configure embedder → open store → claim jobs → generate embeddings → store results → repeat until done
// - SideEffects: embedding API calls; job state transitions; queue statistics; dimension validation
// - FailureModes: missing API keys, store errors, embedding failures, dimension mismatches, timeouts
// - Observability: emits processing statistics, error details, queue snapshots, and timing metrics
// - Keywords: embedding/worker, background, jobs, batch_processing, timeout, error_recovery
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

	// Set up timeout
	deadline := time.Now().Add(time.Duration(in.MaxDuration) * time.Second)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	start := time.Now()
	output := Output{Status: "completed"}

	// Process jobs in batches
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
