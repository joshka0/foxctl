// Package main implements the code/smart_read skill.
// It provides intelligent file reading with auto-selection, context-aware
// extraction, and multiple rendering modes.
//
// This skill is the entry point for the Code Context Funnel:
//
//	smart_read → swe_grep (evidence) → counsel (analysis)
//	    ↓                ↓                    ↓
//	"what to read"   "what's relevant"   "what does it mean"
package main

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/secretutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext/autoselect"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext/guard"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
)

const command = "code/smart_read"

// Default limits.
const (
	DefaultMaxFiles        = 8
	DefaultMaxBytesPerFile = 200 * 1024 // 200KB
	DefaultMaxSnippets     = 50
	DefaultContextLines    = 5
	DefaultTimeout         = 30 * time.Second
)

// Input is the expected JSON input for code/smart_read operations.
type Input struct {
	// Query is the natural-language question guiding file selection.
	Query string `json:"query" validate:"required"`

	// Files are explicit file paths to read (optional).
	// If empty and AutoFiles is true, files are auto-selected.
	Files []string `json:"files,omitempty"`

	// AutoFiles enables automatic file selection based on query.
	// Uses searchindex + retrieval/v2 to find relevant files.
	AutoFiles *bool `json:"auto_files,omitempty"`

	// MaxFiles limits the number of files to process.
	MaxFiles int `json:"max_files,omitempty"`

	// Mode determines how content is extracted and rendered.
	// Options: "general" (snippets), "structure" (API shape), "flow" (control flow)
	Mode string `json:"mode,omitempty"`

	// MaxBytesPerFile limits bytes read per file.
	MaxBytesPerFile int `json:"max_bytes_per_file,omitempty"`

	// ContextLines is the number of lines to include around matches.
	ContextLines int `json:"context_lines,omitempty"`

	// RepoIndexMode controls repo-index contribution when auto-selecting files.
	// Options: auto, search, dag, off.
	RepoIndexMode string `json:"repo_index_mode,omitempty"`
}

// Output is the skill output structure for code/smart_read results.
type Output struct {
	// Evidence contains the extracted code snippets.
	Evidence *codecontext.Evidence `json:"evidence"`

	// Candidates lists files that were considered (when auto_files=true).
	Candidates []CandidateInfo `json:"candidates,omitempty"`

	// SecretFindings contains any detected secrets (redacted).
	SecretFindings []SecretFinding `json:"secret_findings,omitempty"`

	// Stats provides metrics about the extraction process.
	Stats Stats `json:"stats"`
}

// SecretFinding represents a detected secret in the output.
type SecretFinding = secretutil.Finding

// CandidateInfo describes a candidate file for smart reading.
type CandidateInfo struct {
	Path   string  `json:"path"`
	Score  float64 `json:"score"`
	Source string  `json:"source"`
}

// Stats provides metrics about the extraction process.
type Stats struct {
	LatencyMS       int    `json:"latency_ms"`
	FilesConsidered int    `json:"files_considered"`
	FilesProcessed  int    `json:"files_processed"`
	SnippetsFound   int    `json:"snippets_found"`
	Mode            string `json:"mode"`
	SelectionMethod string `json:"selection_method"` // "explicit", "auto"
}

// main is the skill entry point for code/smart_read.
func main() {
	skillmain.Main(command, skillmain.Chain(
		run,
		skillmain.WithTimeout[Input](DefaultTimeout),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates intelligent file reading with auto-selection and context extraction.
//
// Index:
//
//	Purpose: Provide intelligent file reading with auto-selection, context-aware extraction, and multiple rendering modes
//	Flow: apply defaults → determine candidates (explicit or auto) → collect evidence → scan for secrets → emit results
//	SideEffects: file system reads; embedding API calls (auto-selection); secret scanning; artifact persistence
//	FailureModes: invalid paths, file read errors, embedding provider errors, timeout errors
//	Observability: emits evidence/candidates/secret_findings/stats
//	Related: autoSelectFiles, codecontext.Collect, secretutil.ScanEvidence, mapMode
//	Keywords: code/smart_read, evidence, snippets, auto_selection, secrets, structure, flow
//
// [[domain:intelligent-code-reading]]
// [[protocol:code-context-funnel]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	start := time.Now()
	logger := rc.Logger.With().Str("skill", command).Logger()

	// Apply defaults
	if in.AutoFiles == nil {
		defaultTrue := true
		in.AutoFiles = &defaultTrue
	}
	if in.MaxFiles <= 0 {
		in.MaxFiles = DefaultMaxFiles
	}
	if in.MaxBytesPerFile <= 0 {
		in.MaxBytesPerFile = DefaultMaxBytesPerFile
	}
	if in.ContextLines <= 0 {
		in.ContextLines = DefaultContextLines
	}
	if in.Mode == "" {
		in.Mode = "general"
	}

	// Map mode to RenderMode
	renderMode := mapMode(in.Mode)

	out := &Output{
		Stats: Stats{
			Mode: in.Mode,
		},
	}

	// Determine candidates
	var candidates []codecontext.Candidate

	if len(in.Files) > 0 {
		// Explicit files provided
		out.Stats.SelectionMethod = "explicit"
		for _, f := range in.Files {
			candidates = append(candidates, codecontext.Candidate{
				Path:     f,
				Priority: 1.0,
			})
		}
		logger.Debug().Int("count", len(candidates)).Msg("using explicit files")
	} else if *in.AutoFiles {
		// Auto-select files using retrieval
		out.Stats.SelectionMethod = "auto"
		selected, err := autoSelectFiles(ctx, rc, in.Query, in.MaxFiles, in.RepoIndexMode, logger)
		if err != nil {
			logger.Warn().Err(err).Msg("auto-selection failed, using empty candidates")
		} else {
			candidates = selected.candidates
			out.Candidates = selected.info
			logger.Debug().Int("count", len(candidates)).Msg("auto-selected files")
		}
	}

	out.Stats.FilesConsidered = len(candidates)

	// Collect evidence using codecontext
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates:      candidates,
		Query:           in.Query,
		PathValidator:   rc.PathValidator,
		MaxFiles:        in.MaxFiles,
		MaxSnippets:     DefaultMaxSnippets,
		MaxBytesPerFile: in.MaxBytesPerFile,
		ContextLines:    in.ContextLines,
		Mode:            renderMode,
	})
	if err != nil {
		return skillerr.WrapRuntime("collect evidence", err)
	}

	out.Evidence = evidence
	out.Stats.FilesProcessed = evidence.Stats.FilesProcessed
	out.Stats.SnippetsFound = evidence.Stats.SnippetsExtracted

	// Scan evidence for secrets
	secretFindings, hasHighSeverity := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeBlock)
	out.SecretFindings = secretFindings

	// Block if high-severity secrets detected
	if hasHighSeverity {
		out.Stats.LatencyMS = int(time.Since(start).Milliseconds())
		// Still emit but with error indication in the findings
		// The caller can decide how to handle high-severity secrets
		logger.Warn().Int("count", len(secretFindings)).Msg("high-severity secrets detected in evidence")
	}

	out.Stats.LatencyMS = int(time.Since(start).Milliseconds())

	return skillout.Emit(rc, command, out)
}

// autoSelectResult holds the result of auto-selection with candidates and info.
type autoSelectResult struct {
	candidates []codecontext.Candidate
	info       []CandidateInfo
}

// autoSelectFiles uses searchindex + retrieval/v2 to find relevant files based on query.
func autoSelectFiles(ctx context.Context, rc *skillmain.RunContext, query string, maxFiles int, repoIndexMode string, logger zerolog.Logger) (*autoSelectResult, error) {
	var embedProvider semantic.EmbeddingProvider
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeSymbols, rc.Config, skillmain.EmbeddingGuard(rc))
	if err == nil {
		embedProvider = &embedderAdapter{embedder: embedder}
	} else {
		logger.Debug().Err(err).Msg("embedding provider unavailable, using lexical retrieval only")
	}

	selected, err := autoselect.Select(ctx, rc.Config, autoselect.Options{
		WorkspacePath: rc.PathValidator.Workspace(),
		Query:         query,
		MaxFiles:      maxFiles,
		RepoIndexMode: repoIndexMode,
		EmbedProvider: embedProvider,
	})
	if err != nil {
		return nil, skillerr.WrapRuntime("auto-select files", err)
	}

	info := make([]CandidateInfo, 0, len(selected.Matches))
	for _, match := range selected.Matches {
		info = append(info, CandidateInfo{
			Path:   match.Path,
			Score:  match.Score,
			Source: match.Source,
		})
	}

	return &autoSelectResult{candidates: selected.Candidates, info: info}, nil
}

// mapMode converts input mode string to codecontext.RenderMode.
func mapMode(mode string) codecontext.RenderMode {
	switch mode {
	case "structure":
		return codecontext.ModeStructure
	case "flow":
		return codecontext.ModeFlow
	case "masked":
		return codecontext.ModeMasked
	default:
		return codecontext.ModeSnippets
	}
}

// embedderAdapter adapts *Embedder to EmbeddingProvider interface.
// The Embedder returns EmbedResult (with Vec, Model, etc.) but EmbeddingProvider
// expects just []float32 from Embed().
type embedderAdapter struct {
	embedder *semantic.Embedder
}

func (a *embedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := a.embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return result.Vec, nil
}

func (a *embedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results, err := a.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(results))
	for i, r := range results {
		vecs[i] = r.Vec
	}
	return vecs, nil
}

func (a *embedderAdapter) Model() string {
	return a.embedder.Model()
}

func (a *embedderAdapter) Dimensions() int {
	return a.embedder.Dimensions()
}
