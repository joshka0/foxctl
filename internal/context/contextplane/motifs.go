package contextplane

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	RepoMotifType       = "repo_motif"
	repoMotifNamePrefix = "repo-motif://"
)

type RepoMotifBuildOptions struct {
	MaxSeeds       int
	MaxMotifs      int
	Depth          int
	Budget         int
	PerNodeCap     int
	MaxRelated     int
	IncludeTests   bool
	IncludeImports bool
}

type RepoMotif struct {
	Signature    string    `json:"signature"`
	MotifType    string    `json:"motif_type"`
	AnchorPath   string    `json:"anchor_path"`
	Paths        []string  `json:"paths"`
	RelatedPaths []string  `json:"related_paths,omitempty"`
	Symbols      []string  `json:"symbols,omitempty"`
	EdgeTypes    []string  `json:"edge_types,omitempty"`
	ClusterRoot  string    `json:"cluster_root,omitempty"`
	SupportScore float64   `json:"support_score"`
	GeneratedAt  time.Time `json:"generated_at"`
	Summary      string    `json:"summary"`
}

type RepoMotifSearchHit struct {
	Name         string    `json:"name"`
	MotifType    string    `json:"motif_type"`
	AnchorPath   string    `json:"anchor_path"`
	Summary      string    `json:"summary"`
	Score        float64   `json:"score"`
	Paths        []string  `json:"paths,omitempty"`
	RelatedPaths []string  `json:"related_paths,omitempty"`
	Symbols      []string  `json:"symbols,omitempty"`
	EdgeTypes    []string  `json:"edge_types,omitempty"`
	ClusterRoot  string    `json:"cluster_root,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type repoMotifPath struct {
	Path       string
	Symbols    []string
	EdgeTypes  []repoindex.EdgeType
	Hops       int
	Score      float64
	ClusterDir string
}

func DefaultRepoMotifBuildOptions() RepoMotifBuildOptions {
	return RepoMotifBuildOptions{
		MaxSeeds:       300,
		MaxMotifs:      150,
		Depth:          2,
		Budget:         30,
		PerNodeCap:     20,
		MaxRelated:     3,
		IncludeTests:   false,
		IncludeImports: true,
	}
}

func BuildRepoMotifArtifacts(ctx context.Context, workspacePath string, repo *repoindex.Store, memStore storage.MemoryStore, provider semantic.EmbeddingProvider, opts RepoMotifBuildOptions) ([]RepoMotif, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return nil, fmt.Errorf("workspace path required")
	}
	if repo == nil {
		return nil, fmt.Errorf("repo store required")
	}
	if memStore == nil {
		return nil, fmt.Errorf("memory store required")
	}
	if opts.MaxSeeds <= 0 || opts.MaxMotifs <= 0 || opts.MaxRelated <= 0 || opts.Depth <= 0 || opts.Budget <= 0 || opts.PerNodeCap <= 0 {
		defaults := DefaultRepoMotifBuildOptions()
		if opts.MaxSeeds <= 0 {
			opts.MaxSeeds = defaults.MaxSeeds
		}
		if opts.MaxMotifs <= 0 {
			opts.MaxMotifs = defaults.MaxMotifs
		}
		if opts.Depth <= 0 {
			opts.Depth = defaults.Depth
		}
		if opts.Budget <= 0 {
			opts.Budget = defaults.Budget
		}
		if opts.PerNodeCap <= 0 {
			opts.PerNodeCap = defaults.PerNodeCap
		}
		if opts.MaxRelated <= 0 {
			opts.MaxRelated = defaults.MaxRelated
		}
	}

	seedScanLimit := max(opts.MaxSeeds*4, opts.MaxSeeds)
	files, err := repo.ListNodesByKind(ctx, repoindex.NodeFile, seedScanLimit)
	if err != nil {
		return nil, fmt.Errorf("list file nodes: %w", err)
	}
	files, err = rankRepoMotifSeeds(ctx, repo, files, opts)
	if err != nil {
		return nil, err
	}
	if _, err := memStore.DeleteByNamePrefix(ctx, workspacePath, repoMotifNamePrefix); err != nil {
		return nil, fmt.Errorf("delete stale repo motifs: %w", err)
	}

	motifsBySig := map[string]RepoMotif{}
	for _, file := range files {
		anchorPath := normalizeRepoPaths([]string{file.File})
		if len(anchorPath) == 0 {
			continue
		}
		if !opts.IncludeTests && isRepoMotifTestPath(anchorPath[0]) {
			continue
		}
		motif, ok, err := buildRepoMotifFromStore(ctx, repo, file, opts)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if existing, exists := motifsBySig[motif.Signature]; exists && existing.SupportScore >= motif.SupportScore {
			continue
		}
		motifsBySig[motif.Signature] = motif
	}

	motifs := make([]RepoMotif, 0, len(motifsBySig))
	for _, motif := range motifsBySig {
		motifs = append(motifs, motif)
	}
	sort.Slice(motifs, func(i, j int) bool {
		if motifs[i].SupportScore == motifs[j].SupportScore {
			return motifs[i].Signature < motifs[j].Signature
		}
		return motifs[i].SupportScore > motifs[j].SupportScore
	})
	if opts.MaxMotifs > 0 && len(motifs) > opts.MaxMotifs {
		motifs = motifs[:opts.MaxMotifs]
	}

	for _, motif := range motifs {
		payload, err := json.Marshal(motif)
		if err != nil {
			return nil, fmt.Errorf("marshal motif %s: %w", motif.Signature, err)
		}
		name := repoMotifName(motif.Signature)
		if _, err := memStore.SaveFromResult(ctx, name, RepoMotifType, workspacePath, motif.Summary, payload); err != nil {
			return nil, fmt.Errorf("save motif %s: %w", motif.Signature, err)
		}
		if provider != nil {
			vec, err := provider.Embed(ctx, repoMotifEmbeddingText(motif))
			if err != nil {
				return nil, fmt.Errorf("embed motif %s: %w", motif.Signature, err)
			}
			if err := memStore.UpdateEmbedding(ctx, name, workspacePath, vec); err != nil {
				return nil, fmt.Errorf("store motif embedding %s: %w", motif.Signature, err)
			}
		}
	}

	return motifs, nil
}

func rankRepoMotifSeeds(ctx context.Context, repo *repoindex.Store, files []repoindex.Node, opts RepoMotifBuildOptions) ([]repoindex.Node, error) {
	type scoredSeed struct {
		node  repoindex.Node
		score float64
	}
	scored := make([]scoredSeed, 0, len(files))
	edgeTypes := repoMotifEdgeSet(opts.IncludeImports)
	for _, file := range files {
		pathValue := normalizeRepoPaths([]string{file.File})
		if len(pathValue) == 0 {
			continue
		}
		if !opts.IncludeTests && isRepoMotifTestPath(pathValue[0]) {
			continue
		}
		outEdges, err := repo.GetOutgoingEdges(ctx, file.ID, edgeTypes, opts.PerNodeCap)
		if err != nil {
			return nil, fmt.Errorf("seed outgoing edges %s: %w", file.ID, err)
		}
		inEdges, err := repo.GetIncomingEdges(ctx, file.ID, edgeTypes, opts.PerNodeCap)
		if err != nil {
			return nil, fmt.Errorf("seed incoming edges %s: %w", file.ID, err)
		}
		score := 0.0
		for _, edge := range append(outEdges, inEdges...) {
			score += repoMotifSeedEdgeWeight(edge.Type)
		}
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSeed{node: file, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].node.File < scored[j].node.File
		}
		return scored[i].score > scored[j].score
	})
	limit := min(len(scored), opts.MaxSeeds)
	out := make([]repoindex.Node, 0, limit)
	for _, item := range scored[:limit] {
		out = append(out, item.node)
	}
	return out, nil
}

func buildRepoMotifFromStore(ctx context.Context, repo *repoindex.Store, seed repoindex.Node, opts RepoMotifBuildOptions) (RepoMotif, bool, error) {
	anchorPath := normalizeRepoPaths([]string{seed.File})
	if len(anchorPath) == 0 {
		return RepoMotif{}, false, nil
	}
	related, err := collectRepoMotifPathsFromStore(ctx, repo, seed, opts)
	if err != nil {
		return RepoMotif{}, false, err
	}
	if len(related) == 0 {
		return RepoMotif{}, false, nil
	}
	sort.Slice(related, func(i, j int) bool {
		if related[i].Score == related[j].Score {
			return related[i].Path < related[j].Path
		}
		return related[i].Score > related[j].Score
	})
	if opts.MaxRelated > 0 && len(related) > opts.MaxRelated {
		related = related[:opts.MaxRelated]
	}

	relatedPaths := make([]string, 0, len(related))
	symbols := make([]string, 0, len(related)*2)
	edgeTypes := make([]string, 0, len(related)*2)
	totalScore := 0.0
	for _, item := range related {
		relatedPaths = append(relatedPaths, item.Path)
		symbols = append(symbols, item.Symbols...)
		for _, edgeType := range item.EdgeTypes {
			edgeTypes = append(edgeTypes, string(edgeType))
		}
		totalScore += item.Score
	}
	paths := append([]string{anchorPath[0]}, relatedPaths...)
	paths = normalizeRepoPaths(paths)
	relatedPaths = normalizeRepoPaths(relatedPaths)
	symbols = uniqueStrings(symbols)
	edgeTypes = uniqueStrings(edgeTypes)
	clusterRoot := repoMotifClusterRoot(paths)
	motifType := repoMotifTypeForEdges(edgeTypes)
	signature := repoMotifSignature(motifType, paths, edgeTypes)
	motif := RepoMotif{
		Signature:    signature,
		MotifType:    motifType,
		AnchorPath:   anchorPath[0],
		Paths:        paths,
		RelatedPaths: relatedPaths,
		Symbols:      symbols,
		EdgeTypes:    edgeTypes,
		ClusterRoot:  clusterRoot,
		SupportScore: totalScore / float64(max(1, len(related))),
		GeneratedAt:  time.Now().UTC(),
	}
	motif.Summary = summarizeRepoMotif(motif)
	return motif, true, nil
}

func SearchRepoMotifArtifacts(ctx context.Context, workspacePath, query string, limit int, memStore storage.MemoryStore, provider semantic.EmbeddingProvider) ([]RepoMotifSearchHit, error) {
	if memStore == nil {
		return nil, fmt.Errorf("memory store required")
	}
	if limit <= 0 {
		limit = 10
	}
	type memoryByType interface {
		ListFiltered(ctx context.Context, workspace string, filter storage.MemoryListFilter, limit, offset int) ([]storage.NamedEntry, int, error)
		SearchSimilarByType(ctx context.Context, workspace, entryType string, embedding []float32, limit int) ([]storage.ScoredEntry, error)
	}
	typedStore, ok := memStore.(memoryByType)
	if !ok {
		return nil, fmt.Errorf("memory store does not support typed motif search")
	}
	if provider != nil && strings.TrimSpace(query) != "" {
		vec, err := provider.Embed(ctx, query)
		if err == nil {
			scored, err := typedStore.SearchSimilarByType(ctx, workspacePath, RepoMotifType, vec, limit)
			if err == nil {
				return repoMotifHitsFromScored(scored), nil
			}
		}
	}
	entries, _, err := typedStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: []string{RepoMotifType}}, max(limit*5, 50), 0)
	if err != nil {
		return nil, err
	}
	return repoMotifHitsFromEntries(entries, query, limit), nil
}

func collectRepoMotifPathsFromStore(ctx context.Context, repo *repoindex.Store, seed repoindex.Node, opts RepoMotifBuildOptions) ([]repoMotifPath, error) {
	edgesByNode := map[string][]repoindex.Edge{}
	nodesByID := map[string]repoindex.Node{seed.ID: seed}
	type visit struct {
		nodeID string
		depth  int
	}
	visitedDepth := map[string]int{seed.ID: 0}
	queue := []visit{{nodeID: seed.ID, depth: 0}}
	edgeLimit := opts.PerNodeCap
	if edgeLimit <= 0 {
		edgeLimit = 20
	}
	edgeTypes := repoMotifEdgeSet(opts.IncludeImports)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= opts.Depth {
			continue
		}
		outEdges, err := repo.GetOutgoingEdges(ctx, current.nodeID, edgeTypes, edgeLimit)
		if err != nil {
			return nil, fmt.Errorf("outgoing motif edges %s: %w", current.nodeID, err)
		}
		inEdges, err := repo.GetIncomingEdges(ctx, current.nodeID, edgeTypes, edgeLimit)
		if err != nil {
			return nil, fmt.Errorf("incoming motif edges %s: %w", current.nodeID, err)
		}
		for _, edge := range append(outEdges, inEdges...) {
			edgesByNode[edge.Src] = append(edgesByNode[edge.Src], edge)
			edgesByNode[edge.Dst] = append(edgesByNode[edge.Dst], edge)
			neighborID := repoMotifNeighborID(edge, current.nodeID)
			if neighborID == "" {
				continue
			}
			if _, ok := nodesByID[neighborID]; !ok {
				node, err := repo.GetNode(ctx, neighborID)
				if err != nil {
					continue
				}
				nodesByID[neighborID] = node
			}
			nextDepth := current.depth + 1
			if prevDepth, ok := visitedDepth[neighborID]; ok && prevDepth <= nextDepth {
				continue
			}
			visitedDepth[neighborID] = nextDepth
			queue = append(queue, visit{nodeID: neighborID, depth: nextDepth})
		}
	}
	return collectRepoMotifPaths(seed, nodesByID, edgesByNode, opts), nil
}

func collectRepoMotifPaths(seed repoindex.Node, nodesByID map[string]repoindex.Node, adjacentEdges map[string][]repoindex.Edge, opts RepoMotifBuildOptions) []repoMotifPath {
	bestByPath := map[string]repoMotifPath{}
	consider := func(path repoMotifPath) {
		if path.Path == "" || path.Path == seed.File {
			return
		}
		if !opts.IncludeTests && isRepoMotifTestPath(path.Path) {
			return
		}
		path.Symbols = uniqueStrings(path.Symbols)
		if existing, ok := bestByPath[path.Path]; ok && existing.Score >= path.Score {
			return
		}
		bestByPath[path.Path] = path
	}
	for _, edge1 := range adjacentEdges[seed.ID] {
		node1ID := repoMotifNeighborID(edge1, seed.ID)
		node1, ok := nodesByID[node1ID]
		if !ok {
			continue
		}
		consider(repoMotifPathFromStep(seed, node1, []repoindex.EdgeType{edge1.Type}))
		for _, edge2 := range adjacentEdges[node1.ID] {
			node2ID := repoMotifNeighborID(edge2, node1.ID)
			node2, ok := nodesByID[node2ID]
			if !ok {
				continue
			}
			consider(repoMotifPathFromStep(seed, node2, []repoindex.EdgeType{edge1.Type, edge2.Type}, node1))
		}
	}
	out := make([]repoMotifPath, 0, len(bestByPath))
	for _, item := range bestByPath {
		out = append(out, item)
	}
	return out
}

func repoMotifNeighborID(edge repoindex.Edge, current string) string {
	if edge.Src == current {
		return edge.Dst
	}
	if edge.Dst == current {
		return edge.Src
	}
	return ""
}

func repoMotifPathFromStep(seed repoindex.Node, target repoindex.Node, edgeTypes []repoindex.EdgeType, via ...repoindex.Node) repoMotifPath {
	var pathValue string
	symbols := make([]string, 0, len(via)+1)
	switch target.Kind {
	case repoindex.NodeFile:
		pathValue = strings.TrimSpace(target.File)
	case repoindex.NodeSymbol:
		pathValue = strings.TrimSpace(target.File)
		if strings.TrimSpace(target.Name) != "" {
			symbols = append(symbols, strings.TrimSpace(target.Name))
		}
	}
	for _, node := range via {
		if node.Kind == repoindex.NodeSymbol && strings.TrimSpace(node.Name) != "" {
			symbols = append(symbols, strings.TrimSpace(node.Name))
		}
	}
	score := repoMotifEdgeScore(edgeTypes)
	if target.Kind == repoindex.NodeFile {
		score += 0.25
	}
	normalizedPath := normalizeRepoPaths([]string{pathValue})
	normalizedDir := normalizeRepoPaths([]string{filepath.Dir(pathValue)})
	finalPath := ""
	finalDir := ""
	if len(normalizedPath) > 0 {
		finalPath = normalizedPath[0]
	}
	if len(normalizedDir) > 0 {
		finalDir = normalizedDir[0]
	}
	return repoMotifPath{
		Path:       finalPath,
		Symbols:    symbols,
		EdgeTypes:  edgeTypes,
		Hops:       len(edgeTypes),
		Score:      score,
		ClusterDir: finalDir,
	}
}

func repoMotifEdgeScore(edgeTypes []repoindex.EdgeType) float64 {
	score := 0.0
	for _, edgeType := range edgeTypes {
		switch edgeType {
		case repoindex.EdgeImplements:
			score += 2.2
		case repoindex.EdgeUsesSymbol:
			score += 1.8
		case repoindex.EdgeCalls:
			score += 1.4
		case repoindex.EdgeRefersTo:
			score += 1.1
		case repoindex.EdgeImports:
			score += 0.9
		case repoindex.EdgeEmbeds:
			score += 0.8
		case repoindex.EdgeTests:
			score -= 0.5
		case repoindex.EdgeContains:
			score += 0.1
		}
	}
	if len(edgeTypes) == 2 {
		score += 0.2
	}
	return score
}

func repoMotifSeedEdgeWeight(edgeType repoindex.EdgeType) float64 {
	switch edgeType {
	case repoindex.EdgeImplements:
		return 3.0
	case repoindex.EdgeUsesSymbol:
		return 2.6
	case repoindex.EdgeCalls:
		return 2.2
	case repoindex.EdgeRefersTo:
		return 1.8
	case repoindex.EdgeImports:
		return 1.4
	case repoindex.EdgeEmbeds:
		return 1.2
	case repoindex.EdgeTests:
		return 0.5
	case repoindex.EdgeContains:
		return 0.1
	default:
		return 0
	}
}

func repoMotifTypeForEdges(edgeTypes []string) string {
	has := func(want string) bool {
		for _, edgeType := range edgeTypes {
			if edgeType == want {
				return true
			}
		}
		return false
	}
	switch {
	case has(string(repoindex.EdgeImplements)) || has(string(repoindex.EdgeUsesSymbol)):
		return "protocol_impl"
	case has(string(repoindex.EdgeCalls)):
		return "call_path"
	case has(string(repoindex.EdgeRefersTo)):
		return "reference_path"
	case has(string(repoindex.EdgeImports)):
		return "import_path"
	default:
		return "structural_path"
	}
}

func repoMotifSignature(motifType string, paths, edgeTypes []string) string {
	return motifType + "|" + strings.Join(normalizeRepoPaths(paths), "|") + "|" + strings.Join(uniqueStrings(edgeTypes), "|")
}

func repoMotifClusterRoot(paths []string) string {
	paths = normalizeRepoPaths(paths)
	if len(paths) < 2 {
		return ""
	}
	parts := strings.Split(filepath.Dir(paths[0]), "/")
	for _, pathValue := range paths[1:] {
		dirParts := strings.Split(filepath.Dir(pathValue), "/")
		shared := make([]string, 0, min(len(parts), len(dirParts)))
		for i := 0; i < len(parts) && i < len(dirParts); i++ {
			if parts[i] != dirParts[i] {
				break
			}
			shared = append(shared, parts[i])
		}
		parts = shared
		if len(parts) == 0 {
			return ""
		}
	}
	root := strings.Join(parts, "/")
	if root == "" || root == "." {
		return ""
	}
	return root
}

func summarizeRepoMotif(motif RepoMotif) string {
	related := motif.RelatedPaths
	if len(related) == 0 {
		related = motif.Paths
	}
	if len(related) > 0 && related[0] == motif.AnchorPath {
		related = related[1:]
	}
	if len(related) == 0 {
		return fmt.Sprintf("%s motif rooted at %s", motif.MotifType, motif.AnchorPath)
	}
	return fmt.Sprintf("%s motif linking %s with %s via %s",
		motif.MotifType,
		motif.AnchorPath,
		strings.Join(related, ", "),
		strings.Join(motif.EdgeTypes, ", "),
	)
}

func repoMotifEmbeddingText(motif RepoMotif) string {
	var b strings.Builder
	b.WriteString("motif type: ")
	b.WriteString(motif.MotifType)
	b.WriteString("\n")
	if motif.ClusterRoot != "" {
		b.WriteString("cluster root: ")
		b.WriteString(motif.ClusterRoot)
		b.WriteString("\n")
	}
	b.WriteString("paths:\n")
	for _, pathValue := range motif.Paths {
		b.WriteString("- ")
		b.WriteString(pathValue)
		b.WriteString("\n")
	}
	if len(motif.Symbols) > 0 {
		b.WriteString("symbols:\n")
		for _, symbol := range motif.Symbols {
			b.WriteString("- ")
			b.WriteString(symbol)
			b.WriteString("\n")
		}
	}
	if len(motif.EdgeTypes) > 0 {
		b.WriteString("edge types: ")
		b.WriteString(strings.Join(motif.EdgeTypes, ", "))
		b.WriteString("\n")
	}
	b.WriteString("summary: ")
	b.WriteString(motif.Summary)
	return b.String()
}

func repoMotifName(signature string) string {
	return repoMotifNamePrefix + strings.ReplaceAll(strings.TrimSpace(signature), " ", "_")
}

func repoMotifEdgeSet(includeImports bool) []repoindex.EdgeType {
	edges := []repoindex.EdgeType{
		repoindex.EdgeCalls,
		repoindex.EdgeRefersTo,
		repoindex.EdgeUsesSymbol,
		repoindex.EdgeImplements,
		repoindex.EdgeEmbeds,
		repoindex.EdgeTests,
		repoindex.EdgeContains,
	}
	if includeImports {
		edges = append(edges, repoindex.EdgeImports)
	}
	return edges
}

func isRepoMotifTestPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	return strings.Contains(pathValue, "/test/") || strings.Contains(pathValue, "/tests/") || strings.HasSuffix(pathValue, "_test.go") || strings.HasSuffix(pathValue, ".test.ts") || strings.HasSuffix(pathValue, ".spec.ts") || strings.HasSuffix(pathValue, ".test.tsx") || strings.HasSuffix(pathValue, ".spec.tsx") || strings.HasSuffix(pathValue, "_test.exs")
}

func repoMotifHitsFromScored(entries []storage.ScoredEntry) []RepoMotifSearchHit {
	out := make([]RepoMotifSearchHit, 0, len(entries))
	for _, item := range entries {
		motif, ok := decodeRepoMotifEntry(item.Entry)
		if !ok {
			continue
		}
		out = append(out, RepoMotifSearchHit{
			Name:         item.Entry.Name,
			MotifType:    motif.MotifType,
			AnchorPath:   motif.AnchorPath,
			Summary:      motif.Summary,
			Score:        item.Score,
			Paths:        append([]string(nil), motif.Paths...),
			RelatedPaths: append([]string(nil), motif.RelatedPaths...),
			Symbols:      append([]string(nil), motif.Symbols...),
			EdgeTypes:    append([]string(nil), motif.EdgeTypes...),
			ClusterRoot:  motif.ClusterRoot,
			UpdatedAt:    item.Entry.UpdatedAt,
		})
	}
	return out
}

func repoMotifHitsFromEntries(entries []storage.NamedEntry, query string, limit int) []RepoMotifSearchHit {
	type scored struct {
		hit   RepoMotifSearchHit
		score float64
	}
	terms := repoMotifQueryTerms(query)
	scoredHits := make([]scored, 0, len(entries))
	for _, entry := range entries {
		motif, ok := decodeRepoMotifEntry(entry)
		if !ok {
			continue
		}
		score := scoreRepoMotifLexical(motif, terms)
		scoredHits = append(scoredHits, scored{
			hit: RepoMotifSearchHit{
				Name:         entry.Name,
				MotifType:    motif.MotifType,
				AnchorPath:   motif.AnchorPath,
				Summary:      motif.Summary,
				Score:        score,
				Paths:        append([]string(nil), motif.Paths...),
				RelatedPaths: append([]string(nil), motif.RelatedPaths...),
				Symbols:      append([]string(nil), motif.Symbols...),
				EdgeTypes:    append([]string(nil), motif.EdgeTypes...),
				ClusterRoot:  motif.ClusterRoot,
				UpdatedAt:    entry.UpdatedAt,
			},
			score: score,
		})
	}
	sort.SliceStable(scoredHits, func(i, j int) bool {
		if scoredHits[i].score == scoredHits[j].score {
			return scoredHits[i].hit.Name < scoredHits[j].hit.Name
		}
		return scoredHits[i].score > scoredHits[j].score
	})
	if limit > 0 && len(scoredHits) > limit {
		scoredHits = scoredHits[:limit]
	}
	out := make([]RepoMotifSearchHit, 0, len(scoredHits))
	for _, item := range scoredHits {
		out = append(out, item.hit)
	}
	return out
}

func decodeRepoMotifEntry(entry storage.NamedEntry) (RepoMotif, bool) {
	var motif RepoMotif
	if strings.TrimSpace(entry.Type) != RepoMotifType {
		return RepoMotif{}, false
	}
	if err := json.Unmarshal(entry.Result, &motif); err != nil {
		return RepoMotif{}, false
	}
	return motif, true
}

func repoMotifQueryTerms(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 3 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, field := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(query)), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '/')
	}) {
		add(field)
	}
	return out
}

func scoreRepoMotifLexical(motif RepoMotif, terms []string) float64 {
	if len(terms) == 0 {
		return motif.SupportScore
	}
	text := strings.ToLower(strings.Join(append(append(append([]string{motif.Summary, motif.MotifType, motif.ClusterRoot}, motif.Paths...), motif.Symbols...), motif.EdgeTypes...), " "))
	matches := 0.0
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			matches += 1.0
		}
	}
	return (matches / float64(len(terms))) + (motif.SupportScore * 0.1)
}
