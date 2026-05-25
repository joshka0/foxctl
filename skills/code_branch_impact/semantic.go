package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/branchimpact"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/workspace"
)

const semanticLaneTimeout = 60 * time.Second

type semanticProvider struct {
	workspaceRoot string
	workspaceID   string
	store         searchindex.Store
	embedder      semantic.EmbeddingProvider
	model         string
	source        string
}

type unavailableSemanticProvider struct {
	reason string
}

func openSemanticProvider(ctx context.Context, cfg config.Config, workspaceRoot string) (branchimpact.SemanticProvider, func()) {
	workspaceID := workspace.ID(workspaceRoot)
	store, err := searchindex.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return unavailableSemanticProvider{reason: fmt.Sprintf("open searchindex: %v", err)}, func() {}
	}
	closeStore := func() { errors.Ignore(store.Close(), "close searchindex store") }

	count, err := store.CountWorkspace(ctx, workspaceID)
	if err != nil {
		closeStore()
		return unavailableSemanticProvider{reason: fmt.Sprintf("count searchindex workspace: %v", err)}, func() {}
	}
	if count == 0 {
		closeStore()
		return unavailableSemanticProvider{reason: "searchindex has no documents for workspace"}, func() {}
	}

	meta, err := store.GetEmbeddingMetadata(ctx, workspaceID)
	if err != nil {
		closeStore()
		return unavailableSemanticProvider{reason: fmt.Sprintf("read searchindex embedding metadata: %v", err)}, func() {}
	}
	if meta == nil || meta.Dimensions == 0 || strings.TrimSpace(meta.Model) == "" {
		closeStore()
		return unavailableSemanticProvider{reason: "searchindex has no embedding metadata for workspace"}, func() {}
	}

	provider, err := semantic.NewProviderForModel(
		meta.Model,
		cfg,
		semantic.WithProvider(cfg.Embedding.Provider),
		semantic.WithAPIKey(cfg.Embedding.APIKey),
		semantic.WithBaseURL(cfg.Embedding.BaseURL),
		semantic.WithGeminiKey(cfg.LLM.GeminiAPIKey),
	)
	if err != nil {
		closeStore()
		return unavailableSemanticProvider{reason: fmt.Sprintf("create embedding provider: %v", err)}, func() {}
	}

	source := "searchindex_vector"
	if cfg.Turbovec.Enabled && turbovecAvailable(cfg.Turbovec.SocketPath) {
		store = searchindex.WrapWithTurboVec(store, workspaceID, meta.Dimensions, searchindex.TurboVecConfig{
			Enabled:    true,
			SocketPath: cfg.Turbovec.SocketPath,
			DataDir:    cfg.Storage.Root,
			BitWidth:   cfg.Turbovec.BitWidth,
		})
		source = "turbovec_vector"
	}

	return &semanticProvider{
		workspaceRoot: workspaceRoot,
		workspaceID:   workspaceID,
		store:         store,
		embedder:      provider,
		model:         meta.Model,
		source:        source,
	}, closeStore
}

func (p unavailableSemanticProvider) Neighbors(context.Context, []branchimpact.Change, branchimpact.SemanticOptions) (branchimpact.SemanticResult, error) {
	return branchimpact.SemanticResult{Available: false, Reason: p.reason}, nil
}

func (p *semanticProvider) Neighbors(ctx context.Context, changes []branchimpact.Change, opts branchimpact.SemanticOptions) (branchimpact.SemanticResult, error) {
	if p == nil || p.store == nil || p.embedder == nil {
		return branchimpact.SemanticResult{Available: false, Reason: "semantic provider not configured"}, nil
	}
	searchCtx, cancel := context.WithTimeout(ctx, semanticLaneTimeout)
	defer cancel()

	changedPaths := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		changedPaths[filepath.ToSlash(change.Path)] = struct{}{}
	}

	perChangeLimit := opts.PerFileCap
	if perChangeLimit <= 0 {
		perChangeLimit = branchimpact.DefaultPerFileCap
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = branchimpact.DefaultLimit
	}

	queries := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.IsDeleted {
			continue
		}
		query := p.queryForChange(change)
		if strings.TrimSpace(query) == "" {
			continue
		}
		queries = append(queries, query)
	}
	if len(queries) == 0 {
		return branchimpact.SemanticResult{Available: false, Reason: "no semantic queries built"}, nil
	}

	embeddings, err := p.embedder.EmbedBatch(searchCtx, queries)
	if err != nil {
		return branchimpact.SemanticResult{Available: false, Reason: fmt.Sprintf("embed semantic queries: %v", err)}, nil
	}
	if len(embeddings) != len(queries) {
		return branchimpact.SemanticResult{Available: false, Reason: fmt.Sprintf("embed semantic queries: got %d embeddings for %d queries", len(embeddings), len(queries))}, nil
	}

	searchLimit := semanticVectorRecallLimit(perChangeLimit, len(changedPaths))
	seen := make(map[string]branchimpact.SemanticCandidate)
	for idx := range queries {
		embedding := embeddings[idx]
		if len(embedding) == 0 {
			continue
		}
		hits, err := p.store.VectorRecall(searchCtx, p.workspaceID, embedding, searchindex.VectorRecallOptions{
			Limit:          searchLimit,
			EmbeddingModel: p.model,
		})
		if err != nil {
			return branchimpact.SemanticResult{Available: false, Reason: fmt.Sprintf("search semantic neighbors: %v", err)}, nil
		}
		acceptedForChange := 0
		for _, hit := range hits {
			doc := hit.Doc
			path := filepath.ToSlash(strings.TrimSpace(doc.Path))
			if path == "" {
				continue
			}
			if _, changed := changedPaths[path]; changed {
				continue
			}
			candidate := branchimpact.SemanticCandidate{
				Path:       path,
				Symbol:     firstNonEmpty(doc.SymbolName, doc.Title),
				LineHint:   firstPositive(doc.Anchor.Line, doc.Anchor.StartLine, doc.Anchor.EndLine),
				Similarity: hit.Score,
				Summary:    doc.Summary,
				Source:     p.source,
			}
			key := path + "|" + candidate.Symbol
			if prev, ok := seen[key]; !ok || candidate.Similarity > prev.Similarity {
				seen[key] = candidate
			}
			acceptedForChange++
			if acceptedForChange >= perChangeLimit {
				break
			}
		}
	}
	candidates := make([]branchimpact.SemanticCandidate, 0, len(seen))
	for _, candidate := range seen {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Similarity != candidates[j].Similarity {
			return candidates[i].Similarity > candidates[j].Similarity
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Symbol < candidates[j].Symbol
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	if len(candidates) == 0 {
		return branchimpact.SemanticResult{Available: false, Reason: "no semantic neighbors found"}, nil
	}
	return branchimpact.SemanticResult{Available: true, Reason: p.source, Candidates: candidates}, nil
}

func semanticVectorRecallLimit(perChangeLimit, changedPathCount int) int {
	if perChangeLimit <= 0 {
		perChangeLimit = branchimpact.DefaultPerFileCap
	}
	limit := perChangeLimit * 20
	if limit < 100 {
		limit = 100
	}
	if minForChangedPaths := perChangeLimit + changedPathCount; limit < minForChangedPaths {
		limit = minForChangedPaths
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func (p *semanticProvider) queryForChange(change branchimpact.Change) string {
	parts := []string{change.Path}
	if change.OldPath != "" {
		parts = append(parts, change.OldPath)
	}
	if content := p.readChangedFilePrefix(change.Path); content != "" {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

func (p *semanticProvider) readChangedFilePrefix(path string) string {
	fullPath := filepath.Join(p.workspaceRoot, filepath.FromSlash(path))
	rel, err := filepath.Rel(p.workspaceRoot, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 12*1024))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func turbovecAvailable(socketPath string) bool {
	if strings.TrimSpace(socketPath) == "" {
		socketPath = turbovec.DefaultSocketPath()
	}
	if _, err := os.Stat(socketPath); err != nil {
		return false
	}
	client, err := turbovec.Dial(socketPath)
	if err != nil {
		return false
	}
	_ = client.Close()
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
