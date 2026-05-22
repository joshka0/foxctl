package contextplane

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
)

const (
	defaultRepoSymbolMechanismLimit      = 500
	defaultRepoSymbolMechanismPerNodeCap = 200
)

type RepoSymbolEmbeddingStore interface {
	GetEmbedding(ctx context.Context, workspaceID, symbolID string) (*embedding.EmbeddingResult, error)
}

type RepoSymbolMechanismBuildOptions struct {
	WorkspaceID string               `json:"workspace_id"`
	MaxSymbols  int                  `json:"max_symbols,omitempty"`
	PerNodeCap  int                  `json:"per_node_cap,omitempty"`
	EdgeTypes   []repoindex.EdgeType `json:"edge_types,omitempty"`
	EdgeOrder   []string             `json:"edge_order,omitempty"`
}

type RepoSymbolMechanismBuildResult struct {
	Candidates        []RepoSymbolMechanismCandidate `json:"candidates"`
	SkippedInvalid    int                            `json:"skipped_invalid"`
	SkippedUnembedded int                            `json:"skipped_unembedded"`
}

type RepoSymbolMechanismCandidate struct {
	SymbolID         string                `json:"symbol_id"`
	Node             repoindex.Node        `json:"node"`
	Projection       MechanismProjection   `json:"projection"`
	StructuralShape  MemoryStructuralShape `json:"structural_shape"`
	LiteralVector    []float32             `json:"literal_vector"`
	StructuralVector []float32             `json:"structural_vector"`
}

func (c RepoSymbolMechanismCandidate) MechanismMemory() MechanismMemory {
	return MechanismMemory{
		ID:               c.Projection.ID,
		OriginalDomain:   c.Projection.OriginalDomain,
		Summary:          c.Projection.Summary,
		AbstractSchema:   c.Projection.AbstractSchema,
		MechanismTags:    append([]string(nil), c.Projection.MechanismTags...),
		LiteralVector:    append([]float32(nil), c.LiteralVector...),
		StructuralVector: append([]float32(nil), c.StructuralVector...),
		SourceRefs:       compactEvidenceRefs(c.Projection.SourceRefs),
	}
}

// BuildRepoSymbolMechanismCandidates joins repoindex symbols with existing
// symbol embeddings and emits planner-ready mechanism memories. It is read-only:
// no memory rows, embeddings, Obsidian notes, or queue jobs are written.
func BuildRepoSymbolMechanismCandidates(ctx context.Context, repo *repoindex.Store, embeddings RepoSymbolEmbeddingStore, opts RepoSymbolMechanismBuildOptions) (RepoSymbolMechanismBuildResult, error) {
	if repo == nil {
		return RepoSymbolMechanismBuildResult{}, fmt.Errorf("repo symbol mechanisms: repo store required")
	}
	if embeddings == nil {
		return RepoSymbolMechanismBuildResult{}, fmt.Errorf("repo symbol mechanisms: embedding store required")
	}
	opts.WorkspaceID = strings.TrimSpace(opts.WorkspaceID)
	if opts.WorkspaceID == "" {
		return RepoSymbolMechanismBuildResult{}, fmt.Errorf("repo symbol mechanisms: workspace_id required")
	}
	if opts.MaxSymbols <= 0 {
		opts.MaxSymbols = defaultRepoSymbolMechanismLimit
	}
	if opts.PerNodeCap <= 0 {
		opts.PerNodeCap = defaultRepoSymbolMechanismPerNodeCap
	}
	if len(opts.EdgeTypes) == 0 {
		opts.EdgeTypes = repoindex.CopyEdgeSet(repoindex.EdgeSetStructural)
	} else {
		opts.EdgeTypes = repoindex.DeduplicateEdgeTypes(opts.EdgeTypes)
	}

	nodes, err := repo.ListNodesByKind(ctx, repoindex.NodeSymbol, opts.MaxSymbols)
	if err != nil {
		return RepoSymbolMechanismBuildResult{}, fmt.Errorf("list repo symbols: %w", err)
	}

	var result RepoSymbolMechanismBuildResult
	for _, node := range nodes {
		candidate, ok, err := buildRepoSymbolMechanismCandidate(ctx, opts.WorkspaceID, repo, embeddings, node, opts)
		if err != nil {
			return RepoSymbolMechanismBuildResult{}, err
		}
		if !ok {
			result.SkippedUnembedded++
			continue
		}
		if strings.TrimSpace(candidate.Projection.AbstractSchema) == "" || len(candidate.StructuralVector) == 0 {
			result.SkippedInvalid++
			continue
		}
		result.Candidates = append(result.Candidates, candidate)
	}

	sort.SliceStable(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].Projection.ID < result.Candidates[j].Projection.ID
	})
	return result, nil
}

func buildRepoSymbolMechanismCandidate(ctx context.Context, workspaceID string, repo *repoindex.Store, embeddings RepoSymbolEmbeddingStore, node repoindex.Node, opts RepoSymbolMechanismBuildOptions) (RepoSymbolMechanismCandidate, bool, error) {
	stored, symbolID, ok, err := getRepoSymbolEmbedding(ctx, workspaceID, embeddings, node)
	if err != nil {
		return RepoSymbolMechanismCandidate{}, false, err
	}
	if !ok {
		return RepoSymbolMechanismCandidate{}, false, nil
	}

	outgoing, err := repo.GetOutgoingEdges(ctx, node.ID, opts.EdgeTypes, opts.PerNodeCap)
	if err != nil {
		return RepoSymbolMechanismCandidate{}, false, fmt.Errorf("repo symbol mechanisms: outgoing edges for %s: %w", node.ID, err)
	}
	incoming, err := repo.GetIncomingEdges(ctx, node.ID, opts.EdgeTypes, opts.PerNodeCap)
	if err != nil {
		return RepoSymbolMechanismCandidate{}, false, fmt.Errorf("repo symbol mechanisms: incoming edges for %s: %w", node.ID, err)
	}

	graphShape := RepoSymbolGraphShape(node, outgoing, incoming)
	shape := MemoryStructuralShape{
		Mechanism:  "directed local graph neighborhood",
		Actors:     []string{"source node", "neighbor nodes"},
		Operations: []string{"emit typed outgoing relationships", "receive typed incoming relationships"},
		Signals:    []string{"edge direction mix", "edge type distribution", "bounded neighborhood size"},
		Graph:      &graphShape,
	}
	projection, err := BlurMemoryProjection(MemoryBlurInput{
		ID:             "repo-symbol:" + node.ID,
		WorkspaceID:    workspaceID,
		OriginalDomain: repoSymbolDomain(node),
		Summary:        repoSymbolSummary(node),
		LiteralText:    repoSymbolLiteralText(node),
		Shape:          shape,
		SourceRefs:     repoSymbolEvidenceRefs(node, workspaceID),
		MechanismTags:  []string{"directed_graph_neighborhood", "bounded_relationships"},
		Tags:           []string{"repo_symbol", "mechanism"},
	})
	if err != nil {
		return RepoSymbolMechanismCandidate{}, false, err
	}

	return RepoSymbolMechanismCandidate{
		SymbolID:         symbolID,
		Node:             node,
		Projection:       projection,
		StructuralShape:  shape,
		LiteralVector:    append([]float32(nil), stored.Embedding...),
		StructuralVector: GraphShapeVector(graphShape, repoSymbolEdgeOrder(opts)),
	}, true, nil
}

func getRepoSymbolEmbedding(ctx context.Context, workspaceID string, embeddings RepoSymbolEmbeddingStore, node repoindex.Node) (*embedding.EmbeddingResult, string, bool, error) {
	var lastErr error
	for _, symbolID := range repoSymbolEmbeddingIDCandidates(node) {
		result, err := embeddings.GetEmbedding(ctx, workspaceID, symbolID)
		if err == nil && result != nil && len(result.Embedding) > 0 {
			return result, symbolID, true, nil
		}
		if err != nil && !errors.Is(err, embedding.ErrNotFound) {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, "", false, fmt.Errorf("repo symbol mechanisms: get embedding: %w", lastErr)
	}
	return nil, "", false, nil
}

func RepoSymbolGraphShape(node repoindex.Node, outgoing, incoming []repoindex.Edge) MemoryGraphShape {
	neighbors := map[string]struct{}{}
	countNeighbors := func(edge repoindex.Edge) {
		if edge.Src != "" && edge.Src != node.ID {
			neighbors[edge.Src] = struct{}{}
		}
		if edge.Dst != "" && edge.Dst != node.ID {
			neighbors[edge.Dst] = struct{}{}
		}
	}
	for _, edge := range outgoing {
		countNeighbors(edge)
	}
	for _, edge := range incoming {
		countNeighbors(edge)
	}
	return MemoryGraphShape{
		NodeKind:    string(node.Kind),
		Outgoing:    countRepoEdgesByType(outgoing),
		Incoming:    countRepoEdgesByType(incoming),
		SpanLines:   repoSymbolSpanLines(node),
		NeighborMix: len(neighbors),
	}
}

func repoSymbolEmbeddingIDCandidates(node repoindex.Node) []string {
	key := repoSymbolKey(node)
	if key == "" {
		return nil
	}
	candidates := []string{
		symbolutil.ScopedSymbolID(node.Pkg, key),
		key,
	}
	if lang := repoSymbolLanguage(node.File); lang != "" {
		derivedPkg := symbolutil.DeriveSymbolPackage(node.File, lang)
		candidates = append([]string{symbolutil.ScopedSymbolID(derivedPkg, key)}, candidates...)
	}
	return compactStringsInOrder(candidates)
}

func repoSymbolKey(node repoindex.Node) string {
	_, raw := repoindex.SplitNamespacedID(node.ID)
	raw = strings.TrimPrefix(raw, "sym:")
	if raw == "" {
		return strings.TrimSpace(node.Name)
	}
	if node.Pkg != "" {
		prefix := node.Pkg + ":"
		if strings.HasPrefix(raw, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(raw, prefix))
		}
	}
	if idx := strings.LastIndex(raw, ":"); idx >= 0 && idx+1 < len(raw) {
		return strings.TrimSpace(raw[idx+1:])
	}
	return strings.TrimSpace(node.Name)
}

func repoSymbolDomain(node repoindex.Node) string {
	if lang := repoSymbolLanguage(node.File); lang != "" {
		return symbolutil.DeriveSymbolPackage(node.File, lang)
	}
	if node.Pkg != "" {
		return node.Pkg
	}
	dir := filepath.ToSlash(filepath.Dir(node.File))
	if dir == "." || dir == "" {
		return "repo:root"
	}
	return "repo:" + dir
}

func repoSymbolSummary(node repoindex.Node) string {
	return firstNonEmpty(node.Summary, node.Doc, node.Signature, node.Name, node.ID)
}

func repoSymbolLiteralText(node repoindex.Node) string {
	var b strings.Builder
	writeRepoSymbolField(&b, "symbol", node.Name)
	writeRepoSymbolField(&b, "signature", node.Signature)
	writeRepoSymbolField(&b, "package", node.Pkg)
	writeRepoSymbolField(&b, "file", node.File)
	writeRepoSymbolField(&b, "summary", node.Summary)
	writeRepoSymbolField(&b, "doc", node.Doc)
	return strings.TrimSpace(b.String())
}

func repoSymbolEvidenceRefs(node repoindex.Node, workspaceID string) []contextengine.EvidenceRef {
	refs := []contextengine.EvidenceRef{{
		Type:        contextengine.RefTypeSymbol,
		Ref:         node.ID,
		WorkspaceID: workspaceID,
		Title:       node.Name,
	}}
	if strings.TrimSpace(node.File) != "" {
		refs = append(refs, contextengine.EvidenceRef{
			Type:        contextengine.RefTypePath,
			Ref:         node.File,
			WorkspaceID: workspaceID,
			Title:       filepath.Base(node.File),
		})
	}
	return compactEvidenceRefs(refs)
}

func repoSymbolEdgeOrder(opts RepoSymbolMechanismBuildOptions) []string {
	if len(opts.EdgeOrder) > 0 {
		return opts.EdgeOrder
	}
	out := make([]string, 0, len(opts.EdgeTypes))
	for _, edgeType := range opts.EdgeTypes {
		out = append(out, string(edgeType))
	}
	return out
}

func countRepoEdgesByType(edges []repoindex.Edge) map[string]int {
	counts := make(map[string]int)
	for _, edge := range edges {
		edgeType := strings.TrimSpace(string(edge.Type))
		if edgeType == "" {
			continue
		}
		counts[edgeType]++
	}
	return counts
}

func repoSymbolSpanLines(node repoindex.Node) int {
	if node.SpanStart <= 0 || node.SpanEnd < node.SpanStart {
		return 0
	}
	return node.SpanEnd - node.SpanStart + 1
}

func repoSymbolLanguage(pathValue string) string {
	switch strings.ToLower(filepath.Ext(filepath.ToSlash(pathValue))) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".ex", ".exs":
		return "elixir"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

func writeRepoSymbolField(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}
