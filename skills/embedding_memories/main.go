// Package main implements the embedding/memories skill for generating memory embeddings.
package main

import (
	"context"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const (
	command         = "embedding/memories"
	defaultBatchMax = 10 // Process 10 memories at a time by default
)

// Input is the skill input schema.
type Input struct {
	// Workspace is the workspace to process memories for (optional, defaults to cwd).
	Workspace string `json:"workspace,omitempty"`

	// BatchSize limits how many memories to process per batch.
	BatchSize int `json:"batch_size,omitempty"`

	// ProcessAll loops internally until all memories are embedded.
	// When false, returns after one batch.
	ProcessAll bool `json:"process_all,omitempty"`

	// DryRun if true, lists memories but doesn't generate embeddings.
	DryRun bool `json:"dry_run,omitempty"`
}

// Output is the skill output.
type Output struct {
	Workspace    string         `json:"workspace"`
	MemoriesFound int           `json:"memories_found"`
	Embedded     int            `json:"embedded"`
	Skipped      int            `json:"skipped"`
	Errors       int            `json:"errors"`
	Remaining    int            `json:"remaining,omitempty"`
	BatchCount   int            `json:"batch_count,omitempty"`
	DurationMs   int64          `json:"duration_ms"`
	Memories     []MemoryResult `json:"memories,omitempty"`
	ErrorDetails []string       `json:"error_details,omitempty"`
}

// MemoryResult captures the result of embedding a single memory.
type MemoryResult struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"` // "embedded", "skipped", "error"
	Dimensions int    `json:"dimensions,omitempty"`
	Message    string `json:"message,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Set defaults
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatchMax
	}
	if in.Workspace == "" {
		cwd, err := os.Getwd()
		if err == nil {
			in.Workspace = cwd
		} else {
			in.Workspace = "default"
		}
	}

	start := time.Now()
	output := Output{
		Workspace: in.Workspace,
	}

	// Check for API key
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if voyageKey == "" && geminiKey == "" && !in.DryRun {
		return skillerr.Auth("no embedding API key set", skillerr.WithHint("Set VOYAGE_API_KEY or GEMINI_API_KEY"))
	}

	// Open memory store
	memStore, err := memory.Open(ctx, rc.Config.Storage.Root, rc.Config.Paths.CAS)
	if err != nil {
		return skillerr.Runtime("open memory store", skillerr.WithCause(err))
	}
	defer memStore.Close() //nolint:errcheck

	// Get initial count of memories without embeddings
	allMemories, err := memStore.ListWithoutEmbedding(ctx, in.Workspace, 10000)
	if err != nil {
		return skillerr.Runtime("list memories", skillerr.WithCause(err))
	}
	output.MemoriesFound = len(allMemories)

	// Dry run - just list memories
	if in.DryRun {
		for _, m := range allMemories {
			output.Memories = append(output.Memories, MemoryResult{
				Name:    m.Name,
				Type:    m.Type,
				Status:  "dry_run",
				Message: "Would embed",
			})
		}
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	// Initialize embedding provider (prefer Voyage, fall back to Gemini)
	var provider interface {
		Embed(ctx context.Context, text string) ([]float32, error)
		Model() string
	}

	if voyageKey != "" {
		model, _ := semantic.ScopeModelRecommendation(semantic.ScopeMemory)
		vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			Model:         model,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return skillerr.Runtime("voyage provider", skillerr.WithCause(err))
		}
		provider = vp
	} else {
		gp, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey:        geminiKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return skillerr.Runtime("gemini provider", skillerr.WithCause(err))
		}
		provider = gp
	}

	// Track already embedded memories to skip
	embeddedNames := make(map[string]bool)

	// Process in batches
	for {
		// Get fresh list of memories without embeddings each iteration
		memories, err := memStore.ListWithoutEmbedding(ctx, in.Workspace, in.BatchSize)
		if err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "list memories: "+err.Error())
			output.Errors++
			break
		}

		// Filter out already processed in this run
		var batch []memory.NamedEntry
		for _, m := range memories {
			if !embeddedNames[m.Name] {
				batch = append(batch, m)
			}
		}

		// Nothing left
		if len(batch) == 0 {
			break
		}

		// Apply batch limit
		remaining := 0
		if len(batch) > in.BatchSize {
			remaining = len(batch) - in.BatchSize
			batch = batch[:in.BatchSize]
		}

		// Process batch
		for _, m := range batch {
			content := m.Summary
			if content == "" {
				content = m.Name
			}
			if content == "" {
				output.Skipped++
				embeddedNames[m.Name] = true
				continue
			}

			embedding, err := provider.Embed(ctx, content)
			if err != nil {
				output.Errors++
				output.ErrorDetails = append(output.ErrorDetails, m.Name+": "+err.Error())
				embeddedNames[m.Name] = true
				continue
			}

			if err := memStore.UpdateEmbedding(ctx, m.Name, in.Workspace, embedding); err != nil {
				output.Errors++
				output.ErrorDetails = append(output.ErrorDetails, m.Name+": update failed: "+err.Error())
				embeddedNames[m.Name] = true
				continue
			}

			output.Embedded++
			embeddedNames[m.Name] = true
		}
		output.BatchCount++

		// If not process_all, return after one batch
		if !in.ProcessAll {
			output.Remaining = remaining
			break
		}

		// Check context
		if ctx.Err() != nil {
			output.Remaining = remaining
			break
		}
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return skillout.Emit(rc, command, output)
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
