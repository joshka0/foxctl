// Package main implements the summary worker skill for processing queued window summaries.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/context/sessionkit/summary"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/storage/queue"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

const (
	command       = "summary/worker"
	defaultBatch  = 10
	defaultMaxDur = 300 // 5 minutes
)

// Input is the skill input schema for summary worker with batch processing and filtering options.
type Input struct {
	// BatchSize is the number of jobs to process per batch (default: 10).
	BatchSize int `json:"batch_size,omitempty"`

	// MaxDuration is the maximum processing time in seconds (default: 300).
	MaxDuration int `json:"max_duration,omitempty"`

	// ProcessAll loops until queue is empty or MaxDuration is reached.
	ProcessAll bool `json:"process_all,omitempty"`

	// DryRun if true, claims jobs but doesn't call the LLM API.
	DryRun bool `json:"dry_run,omitempty"`

	// SessionID filters processing to only jobs for this session.
	SessionID string `json:"session_id,omitempty"`
}

// Output is the skill output with comprehensive processing statistics and queue state.
type Output struct {
	Processed  int            `json:"processed"`
	Errors     int            `json:"errors"`
	Skipped    int            `json:"skipped"`
	Remaining  int            `json:"remaining"`
	BatchCount int            `json:"batch_count,omitempty"`
	Status     string         `json:"status"` // "completed", "timeout", "no_jobs", "error"
	DurationMs int64          `json:"duration_ms"`
	LastError  string         `json:"last_error,omitempty"`
	Stats      *QueueSnapshot `json:"stats,omitempty"`
	Message    string         `json:"message"`
}

// QueueSnapshot is a summary of queue state after processing with job counts by status.
type QueueSnapshot struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// main is the skill entry point for summary/worker with queue processing capabilities.
func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates summary queue processing with batch management, timeout handling, and error recovery.
//
// Index:
// - Purpose: Process queued window summaries with LLM generation, batch management, and comprehensive error handling
// - Flow: validate providers → open stores → process jobs in batches → generate summaries → update windows → emit results
// - SideEffects: claims and completes queue jobs; updates session window summaries; manages LLM provider fallbacks
// - FailureModes: missing LLM providers, queue access failures, session store errors, LLM API failures
// - Observability: emits processing statistics, queue snapshots, error details, and comprehensive progress tracking
// - Related: getWindowContentPreview, buildWindowSummaryPrompt, callLLM
// - Keywords: summary/worker, queue_processing, batch_jobs, llm_summarization, error_recovery
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	log := rc.Logger

	// Apply defaults
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatch
	}
	if in.MaxDuration <= 0 {
		in.MaxDuration = defaultMaxDur
	}

	// Get LLM providers for summarization
	providers := llmproviders.SummarizationProviders()
	if len(providers) == 0 && !in.DryRun {
		return skillerr.Auth(
			"no LLM providers available",
			skillerr.WithHint("set CEREBRAS_API_KEY, GROQ_API_KEY, or OPENROUTER_API_KEY"),
		)
	}

	log.Info().
		Int("providers", len(providers)).
		Bool("dry_run", in.DryRun).
		Msg("summary worker starting")

	// Open queue store
	queueStore, err := queue.OpenStore(ctx, rc.Config.Storage.Root, "SUMMARY_QUEUE", summary.QueueDBName, queue.Options{Table: summary.QueueTable})
	if err != nil {
		return skillerr.IO("open queue", skillerr.WithCause(err))
	}
	defer queueStore.Close()

	// Open session store
	sessionStore, err := sessions.OpenFromConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer sessionStore.Close()

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

			// Claim next job (optionally filtered by session)
			claimOpts := queue.ClaimOptions{}
			if in.SessionID != "" {
				claimOpts.GroupID = in.SessionID
			}

			job, err := queueStore.ClaimNext(ctx, claimOpts)
			if err != nil {
				log.Error().Err(err).Msg("failed to claim job")
				output.LastError = err.Error()
				output.Errors++
				continue
			}

			if job == nil {
				noMoreJobs = true
				if output.Processed == 0 && output.Errors == 0 && output.Skipped == 0 {
					output.Status = "no_jobs"
				}
				log.Debug().Msg("no more jobs in queue")
				break
			}

			// Parse payload
			var payload summary.WindowPayload
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("invalid payload")
				if failErr := queueStore.Fail(ctx, job.ID, err.Error()); failErr != nil {
					output.LastError = fmt.Sprintf("fail job: %v (parse: %v)", failErr, err)
				} else {
					output.LastError = err.Error()
				}
				output.Errors++
				continue
			}

			log.Info().
				Str("job_id", job.ID).
				Str("session_id", payload.SessionID).
				Int("window_index", payload.WindowIndex).
				Msg("claimed job")

			// Get window from session store
			window, err := sessionStore.GetContextWindow(ctx, payload.SessionID, payload.WindowIndex)
			if err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to get window")
				if failErr := queueStore.Fail(ctx, job.ID, err.Error()); failErr != nil {
					output.LastError = fmt.Sprintf("fail: %v", failErr)
				} else {
					output.LastError = err.Error()
				}
				output.Errors++
				continue
			}

			// Skip if already has summary (unless force)
			if window.Summary != "" && !payload.Force {
				log.Debug().
					Str("job_id", job.ID).
					Msg("window already summarized, skipping")
				if err := queueStore.Complete(ctx, job.ID); err != nil {
					log.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark complete")
				}
				output.Skipped++
				continue
			}

			// Get content preview from chunks
			contentPreview, err := getWindowContentPreview(ctx, sessionStore, payload.SessionID, window.ChunkStart, window.ChunkEnd)
			if err != nil {
				log.Warn().Err(err).Msg("failed to get content preview")
				contentPreview = ""
			}

			// Skip if no content preview (reduces noise)
			if contentPreview == "" {
				log.Debug().
					Str("job_id", job.ID).
					Msg("no content preview, skipping")
				if err := queueStore.Complete(ctx, job.ID); err != nil {
					log.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark complete")
				}
				output.Skipped++
				continue
			}

			// Generate summary
			var summaryText string
			if in.DryRun {
				summaryText = fmt.Sprintf("[dry-run] Summary for window %d", payload.WindowIndex)
				log.Info().Str("job_id", job.ID).Msg("dry-run summary generated")
			} else {
				var lastErr error
				for _, provider := range providers {
					err = skillmain.GuardCall(rc, skillmain.BreakerLLMProvider, ctx, func(ctx context.Context) error {
						var e error
						summaryText, e = callLLM(ctx, provider, buildWindowSummaryPrompt(&window, contentPreview))
						return e
					})
					if err != nil {
						lastErr = err
						log.Warn().
							Err(err).
							Str("provider", provider.Name).
							Msg("provider failed, trying next")
						continue
					}
					log.Info().
						Str("job_id", job.ID).
						Str("provider", provider.Name).
						Int("summary_len", len(summaryText)).
						Msg("summary generated")
					break
				}

				if summaryText == "" {
					errMsg := "all providers failed"
					if lastErr != nil {
						errMsg = fmt.Sprintf("all providers failed: %v", lastErr)
					}
					log.Error().Str("job_id", job.ID).Msg(errMsg)
					if failErr := queueStore.Fail(ctx, job.ID, errMsg); failErr != nil {
						output.LastError = fmt.Sprintf("fail: %v (summarize: %v)", failErr, errMsg)
					} else {
						output.LastError = errMsg
					}
					output.Errors++
					continue
				}
			}

			// Save summary to session store using window ID
			if err := sessionStore.UpdateContextWindowSummary(ctx, window.ID, summaryText); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to save summary")
				if failErr := queueStore.Fail(ctx, job.ID, err.Error()); failErr != nil {
					output.LastError = fmt.Sprintf("fail: %v", failErr)
				} else {
					output.LastError = err.Error()
				}
				output.Errors++
				continue
			}

			// Mark job complete
			if err := queueStore.Complete(ctx, job.ID); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark complete")
				output.LastError = err.Error()
				output.Errors++
				continue
			}

			log.Info().
				Str("job_id", job.ID).
				Str("session_id", payload.SessionID).
				Int("window_index", payload.WindowIndex).
				Msg("job completed")

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
	stats, err := queueStore.Stats(ctx, "")
	if err == nil {
		output.Stats = &QueueSnapshot{
			Queued:    stats.QueuedCount,
			Running:   stats.RunningCount,
			Completed: stats.CompletedCount,
			Failed:    stats.FailedCount,
		}
		output.Remaining = stats.QueuedCount
	}

	// Build message
	switch output.Status {
	case "completed":
		output.Message = fmt.Sprintf("Processed %d summaries (%d skipped, %d errors) in %dms",
			output.Processed, output.Skipped, output.Errors, output.DurationMs)
	case "timeout":
		output.Message = fmt.Sprintf("Timeout after %d summaries (%d skipped, %d errors, %d remaining)",
			output.Processed, output.Skipped, output.Errors, output.Remaining)
	case "no_jobs":
		output.Message = "No jobs in queue"
	default:
		output.Message = fmt.Sprintf("Worker finished: %s", output.Status)
	}

	return skillout.Emit(rc, command, output)
}

// getWindowContentPreview extracts content previews from session chunks within the specified range.
func getWindowContentPreview(ctx context.Context, store *sessions.Store, sessionID string, chunkStart, chunkEnd int) (string, error) {
	chunks, err := store.GetChunks(ctx, sessionID, 0)
	if err != nil {
		return "", err
	}

	var previews []string
	for _, chunk := range chunks {
		if chunk.ChunkIndex >= chunkStart && chunk.ChunkIndex <= chunkEnd {
			if chunk.ContentPreview != "" {
				previews = append(previews, chunk.ContentPreview)
			}
		}
	}

	combined := strings.Join(previews, "\n")
	const maxLen = 2000
	if len(combined) > maxLen {
		combined = combined[:maxLen] + "..."
	}

	return combined, nil
}

// buildWindowSummaryPrompt creates a structured prompt for LLM window summarization with context.
func buildWindowSummaryPrompt(window *storage.ContextWindow, contentPreview string) string {
	return fmt.Sprintf(`Summarize this coding session window in 2-3 concise sentences.
Focus on: what was accomplished, key decisions made, and any issues encountered.

Window trigger: %s
Content preview:
%s

Summary:`, window.Trigger, contentPreview)
}

// callLLM makes HTTP requests to LLM providers for text generation with error handling and timeouts.
func callLLM(ctx context.Context, provider llmproviders.Provider, prompt string) (string, error) {
	if provider.IsCLI {
		return "", fmt.Errorf("CLI providers not supported in worker")
	}

	maxTokens := provider.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 500
	}

	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	if strings.HasPrefix(provider.Name, "openrouter:") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
		req.Header.Set("X-Title", "agentctl")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
