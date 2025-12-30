// Package main implements the embedding worker skill for processing queued embeddings.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/embedding"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/rs/zerolog"
)

var log zerolog.Logger

func init() {
	log = zerolog.New(os.Stderr).With().Timestamp().Str("skill", command).Logger()
}

const (
	command       = "embedding/worker"
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	defaultBatch  = 10
	defaultMaxDur = 300 // 5 minutes
)

// Input is the skill input schema.
type Input struct {
	// BatchSize is the number of jobs to process per batch (default: 10).
	BatchSize int `json:"batch_size,omitempty"`

	// MaxDuration is the maximum processing time in seconds (default: 300).
	MaxDuration int `json:"max_duration,omitempty"`

	// DryRun if true, claims jobs but doesn't call the embedding API.
	DryRun bool `json:"dry_run,omitempty"`
}

// Output is the skill output.
type Output struct {
	Processed  int            `json:"processed"`
	Errors     int            `json:"errors"`
	Remaining  int            `json:"remaining"`
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

// geminiEmbedRequest is the request body for the Gemini embed API.
type geminiEmbedRequest struct {
	Model   string            `json:"model"`
	Content geminiContentPart `json:"content"`
}

type geminiContentPart struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

// geminiEmbedResponse is the response from the Gemini embed API.
type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *geminiError `json:"error,omitempty"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Error().Err(err).Msg("worker failed")
		env := envelope.Error(command, "ERUNTIME", err.Error(), map[string]any{
			"hint": "check input payload and environment variables, run with --debug for details",
		})
		_ = json.NewEncoder(os.Stdout).Encode(env)
		os.Exit(1)
	}
}

func run(ctx context.Context, r io.Reader, w io.Writer) error {
	// Load config for embedding settings
	cfg, err := config.Load(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to load config")
		env := envelope.Error(command, "ERUNTIME", fmt.Sprintf("load config: %v", err), nil)
		_ = json.NewEncoder(w).Encode(env)
		return nil
	}

	var input Input
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		log.Error().Err(err).Msg("failed to parse input")
		env := envelope.Error(command, "EPARSE", "invalid input", map[string]any{
			"hint": "expected JSON object with optional fields: batch_size (int), max_duration (int), dry_run (bool)",
		})
		_ = json.NewEncoder(w).Encode(env)
		return nil
	}

	// Apply defaults
	if input.BatchSize <= 0 {
		input.BatchSize = defaultBatch
	}
	if input.MaxDuration <= 0 {
		input.MaxDuration = defaultMaxDur
	}

	// Determine embedding provider: prefer Voyage (rate-limited), fall back to Gemini
	var voyageProvider *semantic.VoyageProvider
	var geminiKey string
	var embeddingModel string
	var expectedDims int

	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if voyageKey != "" {
		// Use Voyage with built-in rate limiting (3 RPM for free tier)
		vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			RateLimitWait: boolPtr(true), // Wait when rate limited
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to create Voyage provider")
			env := envelope.Error(command, "EAUTH", fmt.Sprintf("voyage provider: %v", err), nil)
			_ = json.NewEncoder(w).Encode(env)
			return nil
		}
		voyageProvider = vp
		embeddingModel = vp.Model()
		expectedDims = vp.Dimensions()
		log.Info().Str("provider", "voyage").Str("model", embeddingModel).Int("dims", expectedDims).Msg("using Voyage embeddings")
	} else {
		// Fall back to Gemini
		geminiKey = os.Getenv("GEMINI_API_KEY")
		if geminiKey == "" && !input.DryRun {
			log.Error().Msg("no embedding API key set")
			env := envelope.Error(command, "EAUTH", "VOYAGE_API_KEY or GEMINI_API_KEY required", map[string]any{
				"hint": "set VOYAGE_API_KEY (preferred) or GEMINI_API_KEY environment variable",
			})
			_ = json.NewEncoder(w).Encode(env)
			return nil
		}
		embeddingModel = cfg.Embedding.Model
		if embeddingModel == "" {
			embeddingModel = "gemini-embedding-001"
		}
		expectedDims = cfg.Embedding.Dimensions
		if expectedDims == 0 {
			// Model-specific dimension defaults for Gemini
			switch embeddingModel {
			case "gemini-embedding-001":
				expectedDims = 3072
			case "text-embedding-004":
				expectedDims = 768
			default:
				expectedDims = dbdriver.GetDefaultVectorDimensions()
			}
		}
		log.Info().Str("provider", "gemini").Str("model", embeddingModel).Int("dims", expectedDims).Msg("using Gemini embeddings")
	}

	// Get storage root (prefer config, fallback to env/default)
	root := cfg.Paths.Cache
	if root == "" {
		root = os.Getenv("AGENTCTL_HOME")
		if root == "" {
			home, _ := os.UserHomeDir()
			root = home + "/.agentctl/cache"
		} else {
			root = root + "/cache"
		}
	}

	// Open store
	store, err := embedding.OpenStore(ctx, root)
	if err != nil {
		log.Error().Err(err).Str("path", root).Msg("failed to open store")
		env := envelope.Error(command, "EIO", fmt.Sprintf("open store: %v", err), map[string]any{
			"hint": "check that the store path exists and has correct permissions: " + root,
		})
		_ = json.NewEncoder(w).Encode(env)
		return nil
	}
	defer store.Close()

	// Set up timeout
	deadline := time.Now().Add(time.Duration(input.MaxDuration) * time.Second)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	start := time.Now()
	output := Output{Status: "completed"}

	// Process jobs in batches
	for i := 0; i < input.BatchSize; i++ {
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
		if input.DryRun {
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
			var embed []float32
			var err error

			if voyageProvider != nil {
				// Use Voyage with built-in rate limiting and 429 retry
				embed, err = voyageProvider.Embed(ctx, job.Content)
			} else {
				// Fall back to Gemini
				embed, err = generateGeminiEmbedding(ctx, geminiKey, embeddingModel, job.Content)
			}

			if err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("embedding generation failed")
				// Rate limit or API error - fail with retry
				if failErr := store.Fail(ctx, job.ID, err.Error()); failErr != nil {
					output.LastError = fmt.Sprintf("fail job: %v (original: %v)", failErr, err)
				} else {
					output.LastError = err.Error()
				}
				output.Errors++

				// If rate limited (Gemini only - Voyage handles this internally)
				if voyageProvider == nil && isRateLimited(err) {
					log.Warn().Str("job_id", job.ID).Msg("rate limited, backing off")
					time.Sleep(2 * time.Second)
				}
				continue
			}

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

			// Store the embedding with model from config
			if err := store.Complete(ctx, job.ID, embed, embeddingModel); err != nil {
				log.Error().Err(err).Str("job_id", job.ID).Msg("failed to store embedding")
				output.LastError = err.Error()
				output.Errors++
				continue
			}
			log.Info().Str("job_id", job.ID).Str("status", "completed").Str("model", embeddingModel).Msg("job completed")
		}

		output.Processed++
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

	env := envelope.OK(command, output)
	return json.NewEncoder(w).Encode(env)
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// generateGeminiEmbedding calls the Gemini embedding API with the specified model.
func generateGeminiEmbedding(ctx context.Context, apiKey, model, text string) ([]float32, error) {
	// Use header-based authentication to prevent API key leakage in logs
	url := fmt.Sprintf("%s/models/%s:embedContent", geminiBaseURL, model)

	reqBody := geminiEmbedRequest{
		Model: fmt.Sprintf("models/%s", model),
		Content: geminiContentPart{
			Parts: []geminiTextPart{{Text: text}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			log.Error().
				Int("http_status", resp.StatusCode).
				Int("api_code", errResp.Error.Code).
				Str("api_status", errResp.Error.Status).
				Str("api_message", errResp.Error.Message).
				Msg("Gemini API error")
			return nil, &apiError{
				code:    errResp.Error.Code,
				message: errResp.Error.Message,
				status:  errResp.Error.Status,
			}
		}
		log.Error().
			Int("http_status", resp.StatusCode).
			Str("response_body", string(respBody)).
			Msg("Gemini API error (unparsed)")
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp geminiEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embedResp.Embedding.Values) == 0 {
		return nil, errors.New("empty embedding returned")
	}

	return embedResp.Embedding.Values, nil
}

type apiError struct {
	code    int
	message string
	status  string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("API error %d (%s): %s", e.code, e.status, e.message)
}

func isRateLimited(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.code == 429 || ae.status == "RESOURCE_EXHAUSTED"
	}
	return false
}
