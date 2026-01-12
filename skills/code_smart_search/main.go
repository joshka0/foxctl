// Package main implements the code/smart_search skill.
//
// This skill combines candidate generation from symbol/semantic indexes
// with snippet extraction via code/snippet_extract. It's the recommended entry
// point for code search when you don't have pre-determined candidates.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/retrieval"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Command is the envelope command for this skill.
const Command = "code/smart_search"

// Error codes per Core Profile v1 §13.
const (
	ErrCodeArg     = "EARG"
	ErrCodeRuntime = "ERUNTIME"
	ErrCodeConfig  = "ERUNTIME"
)

// Default limits.
const (
	DefaultMaxCandidates   = 50
	DefaultMaxSnippets     = 20
	DefaultMaxBytesPerFile = 65536
)

// Input is the expected JSON input.
type Input struct {
	WorkspaceID string   `json:"workspace_id"`
	Question    string   `json:"question" validate:"required"`
	Sources     []string `json:"sources"`
	Limits      Limits   `json:"limits"`
}

// Limits controls candidate generation and snippet extraction.
type Limits struct {
	MaxCandidates   int `json:"max_candidates"`
	MaxSnippets     int `json:"max_snippets"`
	MaxBytesPerFile int `json:"max_bytes_per_file"`
}

// Output is the skill output structure.
type Output struct {
	Summary        Summary           `json:"summary"`
	Candidates     []CandidateOutput `json:"candidates"`
	SnippetsInline []json.RawMessage `json:"snippets_inline"`
	Artifact       string            `json:"artifact,omitempty"`
}

// Summary contains aggregated statistics.
type Summary struct {
	CandidatesGenerated   int            `json:"candidates_generated"`
	CandidatesBySource    map[string]int `json:"candidates_by_source"`
	FilesRelevant         int            `json:"files_relevant"`
	SnippetsEmitted       int            `json:"snippets_emitted"`
	DurationMS            int64          `json:"duration_ms"`
	CandidateGenerationMS int64          `json:"candidate_generation_ms"`
	SnippetExtractionMS   int64          `json:"snippet_extraction_ms"`
}

// CandidateOutput is the output representation of a candidate.
type CandidateOutput struct {
	Path     string  `json:"path"`
	SymbolID string  `json:"symbol_id,omitempty"`
	Score    float64 `json:"score"`
	Source   string  `json:"source"`
}

func main() {
	skillmain.Main(Command, run)
}

// run is the main skill logic.
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if in.WorkspaceID == "" {
		in.WorkspaceID = rc.PathValidator.Workspace()
	}
	if len(in.Sources) == 0 {
		in.Sources = []string{"symbols", "ripgrep"}
	}
	if in.Limits.MaxCandidates <= 0 {
		in.Limits.MaxCandidates = DefaultMaxCandidates
	}
	if in.Limits.MaxSnippets <= 0 {
		in.Limits.MaxSnippets = DefaultMaxSnippets
	}
	if in.Limits.MaxBytesPerFile <= 0 {
		in.Limits.MaxBytesPerFile = DefaultMaxBytesPerFile
	}

	start := time.Now()

	// Step 1: Generate candidates from indexes
	candidateStart := time.Now()
	candidates, err := generateCandidates(ctx, rc, in)
	if err != nil {
		return fmt.Errorf("generate candidates: %w", err)
	}
	candidateDuration := time.Since(candidateStart)

	// Count candidates by source
	bySource := make(map[string]int)
	for _, c := range candidates {
		bySource[c.Source]++
	}

	// If no candidates, return empty result
	if len(candidates) == 0 {
		return skillout.Emit(rc, Command, Output{
			Summary: Summary{
				CandidatesGenerated:   0,
				CandidatesBySource:    bySource,
				FilesRelevant:         0,
				SnippetsEmitted:       0,
				DurationMS:            time.Since(start).Milliseconds(),
				CandidateGenerationMS: candidateDuration.Milliseconds(),
			},
			Candidates:     []CandidateOutput{},
			SnippetsInline: []json.RawMessage{},
		})
	}

	// Step 2: Invoke code/snippet_extract with candidates
	snippetStart := time.Now()
	extractResult, err := invokeSnippetExtract(ctx, rc, in, candidates)
	if err != nil {
		return fmt.Errorf("invoke snippet_extract: %w", err)
	}
	snippetDuration := time.Since(snippetStart)

	// Build output
	candidateOutputs := make([]CandidateOutput, len(candidates))
	for i, c := range candidates {
		candidateOutputs[i] = CandidateOutput{
			Path:     c.Path,
			SymbolID: c.SymbolID,
			Score:    c.Score,
			Source:   c.Source,
		}
	}

	output := Output{
		Summary: Summary{
			CandidatesGenerated:   len(candidates),
			CandidatesBySource:    bySource,
			FilesRelevant:         extractResult.FilesRelevant,
			SnippetsEmitted:       extractResult.SnippetsEmitted,
			DurationMS:            time.Since(start).Milliseconds(),
			CandidateGenerationMS: candidateDuration.Milliseconds(),
			SnippetExtractionMS:   snippetDuration.Milliseconds(),
		},
		Candidates:     candidateOutputs,
		SnippetsInline: extractResult.SnippetsInline,
		Artifact:       extractResult.Artifact,
	}

	return skillout.Emit(rc, Command, output)
}

// generateCandidates uses the retrieval package to generate candidates.
func generateCandidates(ctx context.Context, rc *skillmain.RunContext, in Input) ([]retrieval.Candidate, error) {
	// Open memory store (uses Storage.Root for persistent data)
	store, err := memory.Open(ctx, rc.Config.Storage.Root, rc.Config.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close memory store") }()

	// Create candidate generator with logger
	workspace := rc.PathValidator.Workspace()
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	generator := retrieval.NewGenerator(store, nil, workspace, logger) // nil = no embedding provider

	// Build options from input
	opts := retrieval.DefaultOptions()
	opts.MaxTotalCandidates = in.Limits.MaxCandidates

	// Configure sources
	opts.EnableSymbols = false
	opts.EnableSemantic = false
	opts.EnableRipgrep = false
	for _, src := range in.Sources {
		switch src {
		case "symbols":
			opts.EnableSymbols = true
		case "semantic":
			opts.EnableSemantic = true
		case "ripgrep":
			opts.EnableRipgrep = true
		}
	}

	// Generate candidates
	result, err := generator.Generate(ctx, in.WorkspaceID, in.Question, opts)
	if err != nil {
		return nil, err
	}
	return result.Candidates, nil
}

// ExtractResult holds the parsed result from code/snippet_extract.
type ExtractResult struct {
	FilesRelevant   int
	SnippetsEmitted int
	SnippetsInline  []json.RawMessage
	Artifact        string
}

// invokeSnippetExtract calls the code/snippet_extract skill with generated candidates.
func invokeSnippetExtract(ctx context.Context, rc *skillmain.RunContext, in Input, candidates []retrieval.Candidate) (*ExtractResult, error) {
	// Build snippet_extract input
	extractCandidates := make([]map[string]any, len(candidates))
	for i, c := range candidates {
		extractCandidates[i] = map[string]any{
			"path":     c.Path,
			"priority": c.Score,
		}
		if c.SymbolID != "" {
			extractCandidates[i]["symbol_id"] = c.SymbolID
		}
	}

	extractInput := map[string]any{
		"workspace_id": in.WorkspaceID,
		"question":     in.Question,
		"candidates":   extractCandidates,
		"limits": map[string]any{
			"max_snippets":       in.Limits.MaxSnippets,
			"max_bytes_per_file": in.Limits.MaxBytesPerFile,
		},
	}

	inputJSON, err := json.Marshal(extractInput)
	if err != nil {
		return nil, fmt.Errorf("marshal snippet_extract input: %w", err)
	}

	// Find agentctl binary
	agentctlBin := findAgentctlBin()

	// Get workspace root for subprocess working directory
	// This ensures snippet_extract resolves relative paths correctly
	workspace := rc.PathValidator.Workspace()

	// Execute snippet_extract skill with JSON input via stdin to avoid command-line length limits
	// (some systems limit argv to ~128KB, but JSON input can be much larger with many candidates)
	// NOTE: --input-file - is required to read JSON from stdin; without it, agentctl run ignores stdin
	cmd := exec.CommandContext(ctx, agentctlBin, "run", "code/snippet_extract", "--input-file", "-")
	cmd.Dir = workspace // Run from workspace root so relative paths resolve correctly
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("snippet_extract execution failed: %w\nstderr: %s", err, stderr.String())
	}

	// Parse envelope
	var env struct {
		Status string `json:"status"`
		Data   struct {
			Summary struct {
				FilesRelevant   int `json:"files_relevant"`
				SnippetsEmitted int `json:"snippets_emitted"`
			} `json:"summary"`
			SnippetsInline []json.RawMessage `json:"snippets_inline"`
			Artifact       string            `json:"artifact"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		return nil, fmt.Errorf("parse snippet_extract output: %w", err)
	}

	if env.Status == "error" {
		return nil, fmt.Errorf("snippet_extract error: %s: %s", env.Error.Code, env.Error.Message)
	}

	return &ExtractResult{
		FilesRelevant:   env.Data.Summary.FilesRelevant,
		SnippetsEmitted: env.Data.Summary.SnippetsEmitted,
		SnippetsInline:  env.Data.SnippetsInline,
		Artifact:        env.Data.Artifact,
	}, nil
}

// findAgentctlBin returns the path to the agentctl binary.
func findAgentctlBin() string {
	// Check environment variable
	if bin := os.Getenv("AGENTCTL_BIN"); bin != "" {
		return bin
	}

	// Check common locations
	locations := []string{
		"./bin/agentctl",
		"bin/agentctl",
		"agentctl",
	}

	for _, loc := range locations {
		if _, err := exec.LookPath(loc); err == nil {
			return loc
		}
	}

	return "agentctl"
}
