// Package main implements the code/smart_search skill as an in-process pipeline:
// searchindex -> retrieval/v2 -> codecontext.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/codecontext"
	"github.com/jkatigb/agentctl/internal/codecontext/autoselect"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
)

const (
	Command      = "code/smart_search"
	ArtifactKind = "application/x-swe-grep-snippets+ndjson"
)

const (
	DefaultMaxCandidates   = 50
	DefaultMaxSnippets     = 20
	DefaultMaxBytesPerFile = 64 * 1024
)

type Input struct {
	WorkspaceID   string   `json:"workspace_id"`
	Question      string   `json:"question"`
	Sources       []string `json:"sources"`
	RepoIndexMode string   `json:"repo_index_mode,omitempty"` // off, search, dag
	Limits        struct {
		MaxCandidates   int `json:"max_candidates"`
		MaxSnippets     int `json:"max_snippets"`
		MaxBytesPerFile int `json:"max_bytes_per_file"`
	} `json:"limits,omitempty"`
}

type Output struct {
	Summary        Summary           `json:"summary"`
	Candidates     []CandidateOutput `json:"candidates"`
	SnippetsInline []json.RawMessage `json:"snippets_inline"`
	Artifact       string            `json:"artifact,omitempty"`
}

type Summary struct {
	CandidatesGenerated   int            `json:"candidates_generated"`
	CandidatesBySource    map[string]int `json:"candidates_by_source"`
	FilesRelevant         int            `json:"files_relevant"`
	SnippetsEmitted       int            `json:"snippets_emitted"`
	DurationMS            int64          `json:"duration_ms"`
	CandidateGenerationMS int64          `json:"candidate_generation_ms"`
	SnippetExtractionMS   int64          `json:"snippet_extraction_ms"`
}

type CandidateOutput struct {
	Path     string  `json:"path"`
	SymbolID string  `json:"symbol_id,omitempty"`
	Score    float64 `json:"score"`
	Source   string  `json:"source"`
}

type ExtractResult struct {
	FilesRelevant   int
	SnippetsEmitted int
	SnippetsInline  []json.RawMessage
	Artifact        string
}

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.Question == "" {
		return skillerr.Validationf("question is required")
	}
	if in.WorkspaceID == "" {
		in.WorkspaceID = ws.ID(rc.PathValidator.Workspace())
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

	candidateStart := time.Now()
	candidates, ccCandidates, bySource, err := searchCode(ctx, rc, in)
	if err != nil {
		return skillerr.WrapRuntime("search code", err)
	}
	candidateDuration := time.Since(candidateStart)

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

	snippetStart := time.Now()
	extractResult, err := collectEvidence(ctx, rc, in, ccCandidates)
	if err != nil {
		return skillerr.WrapRuntime("collect evidence", err)
	}
	snippetDuration := time.Since(snippetStart)

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
		Candidates:     candidates,
		SnippetsInline: extractResult.SnippetsInline,
		Artifact:       extractResult.Artifact,
	}

	return skillout.Emit(rc, Command, output)
}

func searchCode(ctx context.Context, rc *skillmain.RunContext, in Input) ([]CandidateOutput, []codecontext.Candidate, map[string]int, error) {
	var embedder semantic.EmbeddingProvider
	for _, src := range in.Sources {
		if src == "semantic" {
			provider, err := semantic.NewProviderForScope(
				semantic.ScopeSymbols,
				rc.Config,
				semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
				semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
			)
			if err == nil {
				embedder = provider
			}
			break
		}
	}

	selected, err := autoselect.Select(ctx, rc.Config, autoselect.Options{
		WorkspacePath: rc.PathValidator.Workspace(),
		WorkspaceID:   in.WorkspaceID,
		Query:         in.Question,
		MaxFiles:      in.Limits.MaxCandidates,
		RepoIndexMode: in.RepoIndexMode,
		EmbedProvider: embedder,
	})
	if err != nil {
		return nil, nil, nil, skillerr.WrapRuntime("auto-select code candidates", err)
	}

	bySource := make(map[string]int)
	candidates := make([]CandidateOutput, 0, len(selected.Matches))
	for _, match := range selected.Matches {
		if match.Source != "" {
			bySource[match.Source]++
		}
		candidates = append(candidates, CandidateOutput{
			Path:     match.Path,
			SymbolID: match.SymbolID,
			Score:    match.Score,
			Source:   match.Source,
		})
	}

	return candidates, selected.Candidates, bySource, nil
}

func collectEvidence(ctx context.Context, rc *skillmain.RunContext, in Input, ccCandidates []codecontext.Candidate) (*ExtractResult, error) {
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates:      ccCandidates,
		Query:           in.Question,
		PathValidator:   rc.PathValidator,
		MaxFiles:        in.Limits.MaxCandidates,
		MaxSnippets:     in.Limits.MaxSnippets,
		MaxBytesPerFile: in.Limits.MaxBytesPerFile,
		ContextLines:    3,
		Mode:            codecontext.ModeSnippets,
	})
	if err != nil {
		return nil, err
	}

	output, artifactPayload, err := codecontext.PrepareOutputWithArtifact(
		evidence,
		32,
		512,
		codecontext.RenderNDJSON,
	)
	if err != nil {
		return nil, err
	}

	previews := make([]json.RawMessage, 0, len(output.SnippetsInline))
	for _, preview := range output.SnippetsInline {
		b, err := json.Marshal(preview)
		if err != nil {
			return nil, skillerr.WrapRuntime("marshal snippet preview", err)
		}
		previews = append(previews, json.RawMessage(b))
	}

	artifactDigest := ""
	if artifactPayload != nil {
		artifact, err := skillmain.PersistBuffer(ctx, rc, bytes.NewBuffer(artifactPayload.Data), artifactPayload.Kind, "code_smart_search")
		if err != nil {
			return nil, skillerr.WrapIO("persist codecontext artifact", err)
		}
		artifactDigest = artifact.Digest
	}

	return &ExtractResult{
		FilesRelevant:   evidence.Stats.FilesProcessed,
		SnippetsEmitted: len(evidence.Snippets),
		SnippetsInline:  previews,
		Artifact:        artifactDigest,
	}, nil
}
