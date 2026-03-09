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
	"github.com/jkatigb/agentctl/internal/codecontext"
	ccadapt "github.com/jkatigb/agentctl/internal/codecontext/adapters"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/repoquery"
	retrievalv2 "github.com/jkatigb/agentctl/internal/retrieval/v2"
	"github.com/jkatigb/agentctl/internal/searchindex"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const Command = "code/smart_search"
const ArtifactKind = "application/x-swe-grep-snippets+ndjson"

const (
	DefaultMaxCandidates   = 50
	DefaultMaxSnippets     = 20
	DefaultMaxBytesPerFile = 64 * 1024
)

type Input struct {
	WorkspaceID string   `json:"workspace_id"`
	Question    string   `json:"question"`
	Sources     []string `json:"sources"`
	RepoIndexMode string `json:"repo_index_mode,omitempty"` // off, search, dag
	Limits      struct {
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
	store, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return nil, nil, nil, skillerr.WrapIO("open memory store", err)
	}
	defer store.Close()

	indexStore, err := searchindex.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return nil, nil, nil, skillerr.WrapIO("open search index", err)
	}
	defer indexStore.Close()

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

	if err := indexStore.DeleteWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, nil, skillerr.WrapIO("reset search index workspace", err)
	}
	if _, err := searchindex.BuildCodeDocuments(ctx, memoryListByTypeSource{store: store}, indexStore, in.WorkspaceID, searchindex.BuildCodeOptions{
		Limit:         in.Limits.MaxCandidates * 10,
		EmbedProvider: embedder,
	}); err != nil {
		return nil, nil, nil, skillerr.WrapRuntime("build code search documents", err)
	}

	engine := retrievalv2.NewEngine(indexStore, embedder)
	repoMode := normalizeRepoIndexMode(in.RepoIndexMode)
	if repoMode != "off" {
		if repoStore, err := repoindex.Open(ctx, rc.Config.Storage.Root, rc.PathValidator.Workspace()); err == nil {
		defer repoStore.Close()
		engine = engine.WithRepoQueryService(repoquery.NewQueryService(repoindex.NewQueryEngine(repoStore)))
	}
	}
	req := retrievalv2.DefaultSearchRequest(in.WorkspaceID, in.Question)
	req.MaxResults = in.Limits.MaxCandidates
	req.Sources.EnableLexical = true
	req.Sources.EnableVector = embedder != nil
	req.Sources.EnableRepoIndex = repoMode != "off"
	req.Sources.RepoIndexMode = repoMode

	resp, err := engine.Search(ctx, req)
	if err != nil {
		return nil, nil, nil, skillerr.WrapRuntime("search retrieval v2", err)
	}

	bySource := make(map[string]int)
	for source, stats := range resp.Stats.Sources {
		if stats.Returned > 0 {
			bySource[string(source)] = stats.Returned
		}
	}

	candidates := make([]CandidateOutput, 0, len(resp.Groups))
	for _, group := range resp.Groups {
		symbolID := ""
		source := ""
		if len(group.Anchors) > 0 {
			symbolID = group.Anchors[0].SymbolID
			source = string(group.Anchors[0].Source)
		}
		candidates = append(candidates, CandidateOutput{
			Path:     group.Path,
			SymbolID: symbolID,
			Score:    group.Score,
			Source:   source,
		})
	}

	return candidates, ccadapt.GroupsToCandidates(resp.Groups), bySource, nil
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

func normalizeRepoIndexMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto"
	case "off", "none", "disabled":
		return "off"
	case "dag", "dag_grep", "repo_index_dag":
		return "dag"
	case "search":
		return "search"
	default:
		return "auto"
	}
}

type memoryListByTypeSource struct {
	store storage.MemoryStore
}

func (s memoryListByTypeSource) ListByType(ctx context.Context, workspaceID, entryType string, limit int) ([]storage.NamedEntry, error) {
	if limit > 0 {
		entries, _, err := s.store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{Types: []string{entryType}}, limit, 0)
		return entries, err
	}
	var out []storage.NamedEntry
	offset := 0
	for {
		page, total, err := s.store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{Types: []string{entryType}}, 200, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		offset += len(page)
		if len(page) == 0 || offset >= total {
			break
		}
	}
	return out, nil
}
