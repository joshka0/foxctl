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

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/refactor/impact"
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
	source        impact.Source
}

type unavailableSemanticProvider struct {
	reason string
}

func openSemanticProvider(ctx context.Context, cfg config.Config, workspaceRoot string) (impact.SemanticProvider, func()) {
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

	source := impact.SourceSearchIndex
	if cfg.Turbovec.Enabled && turbovecAvailable(cfg.Turbovec.SocketPath) {
		store = searchindex.WrapWithTurboVec(store, workspaceID, meta.Dimensions, searchindex.TurboVecConfig{
			Enabled:    true,
			SocketPath: cfg.Turbovec.SocketPath,
			DataDir:    cfg.Storage.Root,
			BitWidth:   cfg.Turbovec.BitWidth,
		})
		source = impact.SourceTurboVec
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

func (p unavailableSemanticProvider) Neighbors(context.Context, impact.SemanticNeighborRequest) (impact.SemanticResult, error) {
	return impact.SemanticResult{Available: false, Reason: p.reason}, nil
}

func (p *semanticProvider) Neighbors(ctx context.Context, req impact.SemanticNeighborRequest) (impact.SemanticResult, error) {
	if p == nil || p.store == nil || p.embedder == nil {
		return impact.SemanticResult{Available: false, Reason: "semantic provider not configured"}, nil
	}
	searchCtx, cancel := context.WithTimeout(ctx, semanticLaneTimeout)
	defer cancel()

	excluded := make(map[string]struct{}, len(req.ExcludePaths))
	for _, path := range req.ExcludePaths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path != "" {
			excluded[path] = struct{}{}
		}
	}

	queries, labels := p.queriesForTargets(req.Targets)
	if len(queries) == 0 {
		return impact.SemanticResult{Available: false, Reason: "no semantic queries built"}, nil
	}
	embeddings, err := p.embedder.EmbedBatch(searchCtx, queries)
	if err != nil {
		return impact.SemanticResult{Available: false, Reason: fmt.Sprintf("embed semantic queries: %v", err)}, nil
	}
	if len(embeddings) != len(queries) {
		return impact.SemanticResult{Available: false, Reason: fmt.Sprintf("embed semantic queries: got %d embeddings for %d queries", len(embeddings), len(queries))}, nil
	}

	perTargetCap := req.PerTargetCap
	if perTargetCap <= 0 {
		perTargetCap = impact.DefaultPerTargetCap
	}
	limit := req.Limit
	if limit <= 0 {
		limit = impact.DefaultLimit
	}

	searchLimit := semanticVectorRecallLimit(perTargetCap, len(excluded))
	seen := make(map[string]impact.SemanticCandidate)
	for idx, embedding := range embeddings {
		if len(embedding) == 0 {
			continue
		}
		hits, err := p.store.VectorRecall(searchCtx, p.workspaceID, embedding, searchindex.VectorRecallOptions{
			Limit:          searchLimit,
			MinScore:       req.MinScore,
			EmbeddingModel: p.model,
		})
		if err != nil {
			return impact.SemanticResult{Available: false, Reason: fmt.Sprintf("search semantic neighbors: %v", err)}, nil
		}
		accepted := 0
		for _, hit := range hits {
			doc := hit.Doc
			path := filepath.ToSlash(strings.TrimSpace(doc.Path))
			if path == "" {
				continue
			}
			if _, changed := excluded[path]; changed {
				continue
			}
			candidate := impact.SemanticCandidate{
				Path:        path,
				Symbol:      firstNonEmpty(doc.SymbolName, doc.Title),
				LineHint:    firstPositive(doc.Anchor.Line, doc.Anchor.StartLine, doc.Anchor.EndLine),
				Similarity:  hit.Score,
				Summary:     doc.Summary,
				Source:      p.source,
				TargetKey:   labels[idx].key,
				TargetLabel: labels[idx].label,
			}
			key := path + "|" + candidate.Symbol
			if prev, ok := seen[key]; !ok || semanticCandidateLess(prev, candidate) {
				seen[key] = candidate
			}
			accepted++
			if accepted >= perTargetCap {
				break
			}
		}
	}
	candidates := make([]impact.SemanticCandidate, 0, len(seen))
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
		if candidates[i].Symbol != candidates[j].Symbol {
			return candidates[i].Symbol < candidates[j].Symbol
		}
		return candidates[i].LineHint < candidates[j].LineHint
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	if len(candidates) == 0 {
		return impact.SemanticResult{Available: false, Reason: "no semantic neighbors found"}, nil
	}
	return impact.SemanticResult{Available: true, Reason: string(p.source), Source: p.source, Candidates: candidates}, nil
}

type semanticTargetLabel struct {
	key   string
	label string
}

func (p *semanticProvider) queriesForTargets(targets []impact.Target) ([]string, []semanticTargetLabel) {
	queries := make([]string, 0, len(targets))
	labels := make([]semanticTargetLabel, 0, len(targets))
	for _, target := range targets {
		query := p.queryForTarget(target)
		if strings.TrimSpace(query) == "" {
			continue
		}
		queries = append(queries, query)
		labels = append(labels, semanticTargetLabel{key: impact.TargetKey(target), label: impact.TargetLabel(target)})
	}
	return queries, labels
}

func (p *semanticProvider) queryForTarget(target impact.Target) string {
	parts := []string{string(target.Kind), target.Path, target.OldPath, target.Symbol, target.Package, target.Contract, target.Description}
	if target.Path != "" && !target.IsDeleted {
		parts = append(parts, p.readTargetFilePrefix(target.Path))
	}
	return strings.Join(nonEmpty(parts), "\n\n")
}

func (p *semanticProvider) readTargetFilePrefix(path string) string {
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

func semanticVectorRecallLimit(perTargetCap, excludedPathCount int) int {
	if perTargetCap <= 0 {
		perTargetCap = impact.DefaultPerTargetCap
	}
	limit := perTargetCap * 20
	if limit < 100 {
		limit = 100
	}
	if minForExcludedPaths := perTargetCap + excludedPathCount; limit < minForExcludedPaths {
		limit = minForExcludedPaths
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func semanticCandidateLess(left, right impact.SemanticCandidate) bool {
	if left.Similarity != right.Similarity {
		return left.Similarity < right.Similarity
	}
	if left.Path != right.Path {
		return left.Path > right.Path
	}
	if left.Symbol != right.Symbol {
		return left.Symbol > right.Symbol
	}
	return left.LineHint > right.LineHint
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

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
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
