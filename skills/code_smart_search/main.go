// Package main implements the code/smart_search skill as an in-process pipeline:
// searchindex -> retrieval/v2 -> codecontext.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/autoselect"
	"github.com/jkatigb/agentctl/internal/platform/config"
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
	InlineMode    string   `json:"inline_mode,omitempty"`
	Limits        struct {
		MaxCandidates   int `json:"max_candidates"`
		MaxSnippets     int `json:"max_snippets"`
		MaxBytesPerFile int `json:"max_bytes_per_file"`
	} `json:"limits,omitempty"`
}

type Output struct {
	Summary         Summary           `json:"summary"`
	InlineMode      string            `json:"inline_mode,omitempty"`
	Candidates      []CandidateOutput `json:"candidates"`
	CandidatesTotal int               `json:"candidates_total,omitempty"`
	SnippetsInline  []json.RawMessage `json:"snippets_inline"`
	SnippetsTotal   int               `json:"snippets_total,omitempty"`
	Truncated       bool              `json:"truncated,omitempty"`
	Artifact        string            `json:"artifact,omitempty"`
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
	Truncated       bool
}

type InlineMode string

const (
	InlineModeAuto           InlineMode = "auto"
	InlineModeFull           InlineMode = "full"
	InlineModePreview        InlineMode = "preview"
	InlineModeArtifactOnly   InlineMode = "artifact_only"
	defaultPreviewCandidates            = 12
	defaultPreviewSnippets              = 12
)

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
			InlineMode:      string(InlineModeFull),
			Candidates:      []CandidateOutput{},
			CandidatesTotal: 0,
			SnippetsInline:  []json.RawMessage{},
			SnippetsTotal:   0,
		})
	}

	snippetStart := time.Now()
	extractResult, err := collectEvidence(ctx, rc, in, ccCandidates)
	if err != nil {
		return skillerr.WrapRuntime("collect evidence", err)
	}
	snippetDuration := time.Since(snippetStart)

	inlineMode, err := parseInlineMode(in.InlineMode)
	if err != nil {
		return err
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
		Candidates:      candidates,
		CandidatesTotal: len(candidates),
		SnippetsInline:  extractResult.SnippetsInline,
		SnippetsTotal:   extractResult.SnippetsEmitted,
		Artifact:        extractResult.Artifact,
		Truncated:       extractResult.Truncated,
	}
	output = applySmartSearchInlineMode(output, inlineMode)

	return skillout.Emit(rc, Command, output)
}

func parseInlineMode(value string) (InlineMode, error) {
	switch InlineMode(strings.ToLower(strings.TrimSpace(value))) {
	case "", InlineModeAuto:
		return InlineModeAuto, nil
	case InlineModeFull:
		return InlineModeFull, nil
	case InlineModePreview:
		return InlineModePreview, nil
	case InlineModeArtifactOnly:
		return InlineModeArtifactOnly, nil
	default:
		return InlineModeAuto, skillerr.Validationf("invalid inline_mode: %s (valid: auto, full, preview, artifact_only)", strings.TrimSpace(value))
	}
}

func applySmartSearchInlineMode(out Output, requested InlineMode) Output {
	hasArtifact := strings.TrimSpace(out.Artifact) != ""
	resolved := requested
	if resolved == InlineModeAuto {
		if hasArtifact || len(out.Candidates) > defaultPreviewCandidates || len(out.SnippetsInline) > defaultPreviewSnippets {
			resolved = InlineModePreview
		} else {
			resolved = InlineModeFull
		}
	}
	if resolved == InlineModeArtifactOnly && !hasArtifact {
		resolved = InlineModePreview
	}
	out.InlineMode = string(resolved)

	switch resolved {
	case InlineModeFull:
		return out
	case InlineModePreview:
		if len(out.Candidates) > defaultPreviewCandidates {
			out.Candidates = append([]CandidateOutput(nil), out.Candidates[:defaultPreviewCandidates]...)
			out.Truncated = true
		}
		if len(out.SnippetsInline) > defaultPreviewSnippets {
			out.SnippetsInline = append([]json.RawMessage(nil), out.SnippetsInline[:defaultPreviewSnippets]...)
			out.Truncated = true
		}
		return out
	case InlineModeArtifactOnly:
		out.Candidates = nil
		out.SnippetsInline = nil
		out.Truncated = true
		return out
	default:
		return out
	}
}

func searchCode(ctx context.Context, rc *skillmain.RunContext, in Input) ([]CandidateOutput, []codecontext.Candidate, map[string]int, error) {
	var embedder semantic.EmbeddingProvider
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	for _, src := range in.Sources {
		if src == "semantic" {
			if semantic.DetectProviderForConfig(rc.Config, voyageKey, geminiKey) != "" {
				provider, err := semantic.NewProviderForScope(
					semantic.ScopeSymbols,
					rc.Config,
					semantic.WithVoyageKey(voyageKey),
					semantic.WithGeminiKey(geminiKey),
				)
				if err == nil {
					embedder = provider
				}
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
	inlineKB := rc.InlineKB
	if inlineKB <= 0 {
		inlineKB = config.DefaultInlineOutputKB
	}

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
		inlineKB,
		512,
		codecontext.RenderNDJSON,
	)
	if err != nil {
		return nil, err
	}

	inlinePreviews := output.SnippetsInline
	if artifactPayload != nil && len(inlinePreviews) == 0 && len(evidence.Snippets) > 0 {
		inlinePreviews = codecontext.MakePreviews(evidence.Snippets, 512)
	}
	previews := make([]json.RawMessage, 0, len(inlinePreviews))
	for _, preview := range inlinePreviews {
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
		Truncated:       output.Truncated || artifactPayload != nil,
	}, nil
}
