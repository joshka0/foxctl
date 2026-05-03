package env

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
)

type expandContextGraphInput struct {
	Roots                []string                            `json:"roots"`
	Query                string                              `json:"query,omitempty"`
	TaskType             string                              `json:"task_type,omitempty"`
	Depth                int                                 `json:"depth,omitempty"`
	Direction            string                              `json:"direction,omitempty"`
	SourceProfiles       []string                            `json:"source_profiles,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	IncludeTests         bool                                `json:"include_tests,omitempty"`
	IncludeAdjacent      bool                                `json:"include_adjacent,omitempty"`
	PathPrefixes         []string                            `json:"path_prefixes,omitempty"`
	ExcludedPaths        []string                            `json:"excluded_paths,omitempty"`
	Budget               contextengine.ContextGraphBudget    `json:"budget,omitempty"`
}

func (a *ReadOnlyAdapter) expandContextGraph(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	start := time.Now()
	var input expandContextGraphInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("expand_context_graph: parse args: %w", err)
	}
	req := contextengine.ContextGraphRequest{
		WorkspaceID:          a.workspaceRoot,
		Query:                strings.TrimSpace(input.Query),
		TaskType:             strings.TrimSpace(input.TaskType),
		SourceProfiles:       contextengine.NormalizeSourceProfiles(input.SourceProfiles),
		CoverageRequirements: append([]contextengine.CoverageRequirement(nil), input.CoverageRequirements...),
		Depth:                input.Depth,
		Direction:            contextengine.ContextGraphDirection(strings.TrimSpace(strings.ToLower(input.Direction))),
		IncludeTests:         input.IncludeTests,
		IncludeAdjacent:      input.IncludeAdjacent,
		PathPrefixes:         cleanContextGraphStrings(input.PathPrefixes),
		ExcludedPaths:        cleanContextGraphStrings(input.ExcludedPaths),
		Budget:               normalizeContextGraphBudget(input.Budget),
	}
	if req.Depth > 0 {
		req.Budget.MaxDepth = req.Depth
	}
	req.RootPaths, req.Roots = parseContextGraphRoots(input.Roots, a.workspaceRoot)
	if len(req.RootPaths) == 0 {
		return nil, fmt.Errorf("expand_context_graph: roots is required")
	}

	report, err := a.expandContextGraphReport(ctx, req, start)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *ReadOnlyAdapter) expandContextGraphReport(ctx context.Context, req contextengine.ContextGraphRequest, start time.Time) (contextengine.ContextGraphReport, error) {
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return contextengine.ContextGraphReport{}, fmt.Errorf("expand_context_graph: open repoindex: %w", err)
	}
	defer store.Close()

	engine := repoindex.NewQueryEngine(store)
	meta, metaErr := store.GetMeta(ctx)
	if metaErr != nil {
		meta = repoindex.IndexMeta{}
	}
	currentHead := repoindex.ResolveGitHead(ctx, a.workspaceRoot)
	currentDirty := repoindex.ResolveGitDirty(ctx, a.workspaceRoot)
	currentSnapshot := repoindex.ResolveGitSnapshot(ctx, a.workspaceRoot)
	freshnessStatus := repoindex.CompareIndexFreshness(meta, currentSnapshot)

	rootPaths := applyContextGraphPathFilters(req.RootPaths, req.PathPrefixes, req.ExcludedPaths)
	if req.Budget.MaxRoots > 0 && len(rootPaths) > req.Budget.MaxRoots {
		rootPaths = rootPaths[:req.Budget.MaxRoots]
	}
	rootNodes, err := engine.ResolveFileNodes(ctx, rootPaths)
	if err != nil {
		return contextengine.ContextGraphReport{}, fmt.Errorf("expand_context_graph: resolve roots: %w", err)
	}
	rootByPath := make(map[string]repoindex.Node, len(rootNodes))
	for _, node := range rootNodes {
		path := normalizeGraphPath(node.File)
		if path != "" {
			rootByPath[path] = node
		}
	}

	missing := make([]contextengine.ContextGraphGap, 0)
	unresolved := make([]string, 0)
	for _, path := range rootPaths {
		if _, ok := rootByPath[path]; !ok {
			unresolved = append(unresolved, path)
		}
	}
	if len(unresolved) > 0 {
		missing = append(missing, contextengine.ContextGraphGap{
			ID:       "root_unresolved",
			Kind:     "root_unresolved",
			Severity: "block",
			Message:  "Some roots were not exact file nodes in the repo index.",
			Roots:    unresolved,
		})
	}

	unloadable := make([]string, 0)
	for _, path := range rootPaths {
		if !a.contextGraphPathLoadable(path) {
			unloadable = append(unloadable, path)
		}
	}
	if len(unloadable) > 0 {
		missing = append(missing, contextengine.ContextGraphGap{
			ID:       "root_unloadable",
			Kind:     "root_unloadable",
			Severity: "block",
			Message:  "Some roots could not be loaded from the workspace.",
			Roots:    unloadable,
		})
	}

	if metaErr != nil {
		missing = append(missing, contextengine.ContextGraphGap{
			ID:       "index_meta_unavailable",
			Kind:     "index_meta_unavailable",
			Severity: "warn",
			Message:  "Repo index metadata could not be read.",
		})
	} else if contextGraphIndexStale(meta, currentSnapshot) {
		missing = append(missing, contextengine.ContextGraphGap{
			ID:       "index_stale",
			Kind:     "index_stale",
			Severity: "warn",
			Message:  "Repo index metadata does not match the current workspace state.",
		})
	}

	seedIDs := make([]string, 0, len(rootNodes))
	for _, node := range rootNodes {
		seedIDs = append(seedIDs, node.ID)
	}
	sort.Strings(seedIDs)

	expandedNodes := map[string]repoindex.Node{}
	expandedEdges := map[string]repoindex.Edge{}
	edgeTypes := contextGraphEdgeTypes(req.IncludeTests)
	directions := contextGraphDirections(req.Direction)
	for _, dir := range directions {
		result, err := engine.Expand(ctx, seedIDs, repoindex.ExpandOptions{
			Direction:  dir,
			EdgeTypes:  edgeTypes,
			Depth:      req.Budget.MaxDepth,
			Budget:     req.Budget.MaxNodes,
			PerNodeCap: req.Budget.PerNodeCap,
		})
		if err != nil {
			return contextengine.ContextGraphReport{}, fmt.Errorf("expand_context_graph: expand %s: %w", dir, err)
		}
		for _, node := range result.Nodes {
			expandedNodes[node.ID] = node
		}
		for _, edge := range result.Edges {
			expandedEdges[edgeKeyEnv(edge)] = edge
		}
	}

	rootSet := map[string]struct{}{}
	for _, id := range seedIDs {
		rootSet[id] = struct{}{}
	}
	graphNodes := make([]contextengine.ContextGraphNode, 0, len(expandedNodes))
	roots := make([]contextengine.ContextGraphNode, 0, len(rootNodes))
	for _, node := range sortedRepoindexNodes(expandedNodes) {
		role := "dependency"
		if _, ok := rootSet[node.ID]; ok {
			role = "root"
		}
		graphNode := contextGraphNodeFromRepoNode(node, role)
		graphNodes = append(graphNodes, graphNode)
		if role == "root" {
			roots = append(roots, graphNode)
		}
	}
	graphEdges := make([]contextengine.ContextGraphEdge, 0, len(expandedEdges))
	for _, edge := range sortedRepoindexEdges(expandedEdges) {
		graphEdges = append(graphEdges, contextGraphEdgeFromRepoEdge(edge, rootSet))
	}
	localNodes, localEdges, localGaps := a.contextGraphLocalFallbacks(ctx, req, rootPaths, rootByPath, expandedEdges)
	graphNodes = appendMissingContextGraphNodes(graphNodes, localNodes...)
	graphEdges = appendMissingContextGraphEdges(graphEdges, localEdges...)
	missing = append(missing, localGaps...)
	if req.Budget.MaxEdges > 0 && len(graphEdges) > req.Budget.MaxEdges {
		graphEdges = graphEdges[:req.Budget.MaxEdges]
		missing = append(missing, contextengine.ContextGraphGap{
			ID:       "edge_budget_exhausted",
			Kind:     "budget_exhausted",
			Severity: "warn",
			Message:  "Graph edge output was capped by budget.",
		})
	}

	confidence := computeContextGraphConfidence(req, rootPaths, roots, graphNodes, graphEdges, missing, meta, currentSnapshot, metaErr == nil)
	return contextengine.ContextGraphReport{
		ID:          "context_graph_" + time.Now().UTC().Format("20060102T150405.000000000Z"),
		WorkspaceID: a.workspaceRoot,
		Query:       req.Query,
		Roots:       roots,
		Nodes:       graphNodes,
		Edges:       graphEdges,
		Missing:     missing,
		Confidence:  confidence,
		Telemetry: contextengine.EvidenceTelemetry{
			DurationMs: time.Since(start).Milliseconds(),
		},
		Metadata: map[string]any{
			"repoindex_head_sha":       meta.HeadSHA,
			"repoindex_worktree_dirty": meta.WorktreeDirty,
			"current_head_sha":         currentHead,
			"current_worktree_dirty":   currentDirty,
			"freshness_status":         freshnessStatus,
			"directions":               directionsToStrings(directions),
		},
	}, nil
}

func normalizeContextGraphBudget(b contextengine.ContextGraphBudget) contextengine.ContextGraphBudget {
	if b.MaxRoots <= 0 {
		b.MaxRoots = 12
	}
	if b.MaxNodes <= 0 {
		b.MaxNodes = 80
	}
	if b.MaxEdges <= 0 {
		b.MaxEdges = 120
	}
	if b.MaxDepth <= 0 {
		b.MaxDepth = 1
	}
	if b.PerNodeCap <= 0 {
		b.PerNodeCap = 20
	}
	if b.MaxLocalFiles <= 0 {
		b.MaxLocalFiles = 200
	}
	if b.MaxLocalBytes <= 0 {
		b.MaxLocalBytes = 2 * 1024 * 1024
	}
	return b
}

func parseContextGraphRoots(raw []string, workspaceID string) ([]string, []contextengine.EvidenceRef) {
	paths := make([]string, 0, len(raw))
	refs := make([]contextengine.EvidenceRef, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		ref, err := contextengine.ParseEvidenceRef(item)
		if err != nil {
			ref = contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: item}
		}
		ref = contextengine.NormalizeEvidenceRef(ref, workspaceID)
		refs = append(refs, ref)
		if ref.Type == contextengine.RefTypePath {
			if path := normalizeGraphPath(ref.Ref); path != "" {
				paths = append(paths, path)
			}
		}
	}
	return appendUniqueStringsEnv(nil, paths...), refs
}

func applyContextGraphPathFilters(paths []string, prefixes []string, excluded []string) []string {
	out := make([]string, 0, len(paths))
	prefixes = normalizePathListEnv(prefixes)
	excluded = normalizePathListEnv(excluded)
	for _, path := range paths {
		path = normalizeGraphPath(path)
		if path == "" {
			continue
		}
		if len(prefixes) > 0 {
			matched := false
			for _, prefix := range prefixes {
				if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if contextGraphPathExcluded(path, excluded) {
			continue
		}
		out = append(out, path)
	}
	return appendUniqueStringsEnv(nil, out...)
}

func contextGraphPathExcluded(path string, excluded []string) bool {
	for _, pattern := range excluded {
		if pattern == "" {
			continue
		}
		if path == pattern || strings.HasPrefix(path, strings.TrimSuffix(pattern, "/")+"/") {
			return true
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
	}
	return false
}

func contextGraphDirections(direction contextengine.ContextGraphDirection) []repoindex.Direction {
	switch direction {
	case contextengine.ContextGraphDirectionOut:
		return []repoindex.Direction{repoindex.DirOut}
	case contextengine.ContextGraphDirectionIn:
		return []repoindex.Direction{repoindex.DirIn}
	default:
		return []repoindex.Direction{repoindex.DirOut, repoindex.DirIn}
	}
}

func contextGraphEdgeTypes(includeTests bool) []repoindex.EdgeType {
	types := []repoindex.EdgeType{
		repoindex.EdgeImports,
		repoindex.EdgeUsesSymbol,
		repoindex.EdgeRefersTo,
		repoindex.EdgeCalls,
		repoindex.EdgeImplements,
		repoindex.EdgeEmbeds,
	}
	if includeTests {
		types = append(types, repoindex.EdgeTests)
	}
	return types
}

func contextGraphNodeFromRepoNode(node repoindex.Node, role string) contextengine.ContextGraphNode {
	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: node.File}
	loadRef := contextengine.FormatEvidenceRef(ref)
	if node.Kind == repoindex.NodeSymbol {
		ref = contextengine.EvidenceRef{Type: contextengine.RefTypeSymbol, Ref: node.Name}
		loadRef = contextengine.FormatEvidenceRef(contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: node.File})
	}
	return contextengine.ContextGraphNode{
		ID:         node.ID,
		Path:       normalizeGraphPath(node.File),
		Symbol:     node.Name,
		Kind:       string(node.Kind),
		Role:       role,
		Language:   languageFromGraphPath(node.File),
		Ref:        ref,
		LoadRef:    loadRef,
		Confidence: 0.95,
		Grounding: contextengine.GraphGrounding{
			Source:     "repoindex",
			Method:     "exact_node",
			Static:     true,
			Confidence: 0.95,
		},
	}
}

func contextGraphEdgeFromRepoEdge(edge repoindex.Edge, roots map[string]struct{}) contextengine.ContextGraphEdge {
	confidence := repoindexEdgeConfidence(edge)
	role := "dependency"
	if _, ok := roots[edge.Dst]; ok {
		role = "dependent"
	}
	grounding := repoindexEdgeGrounding(edge, confidence)
	metadata := map[string]any{"role": role, "weight": edge.Weight}
	if meta := repoindexEdgeMetadata(edge); len(meta) > 0 {
		metadata["repoindex_meta"] = meta
	}
	return contextengine.ContextGraphEdge{
		From:       edge.Src,
		To:         edge.Dst,
		Type:       strings.ToLower(string(edge.Type)),
		Confidence: confidence,
		Grounding:  grounding,
		Metadata:   metadata,
	}
}

func repoindexEdgeConfidence(edge repoindex.Edge) float64 {
	if edge.Weight > 0 && edge.Weight < 1 {
		return 0.75 + (edge.Weight * 0.15)
	}
	return 0.9
}

func repoindexEdgeGrounding(edge repoindex.Edge, confidence float64) contextengine.GraphGrounding {
	meta := repoindexEdgeMetadata(edge)
	source := "repoindex"
	method := "static_graph"
	static := true
	heuristic := false
	if value, ok := meta["grounding_source"].(string); ok && strings.TrimSpace(value) != "" {
		source = strings.TrimSpace(value)
	}
	if value, ok := meta["grounding_method"].(string); ok && strings.TrimSpace(value) != "" {
		method = strings.TrimSpace(value)
	}
	if value, ok := meta["grounding_static"].(bool); ok {
		static = value
	}
	if value, ok := meta["grounding_heuristic"].(bool); ok {
		heuristic = value
	}
	if edge.Weight > 0 && edge.Weight < 0.95 {
		static = false
		heuristic = true
		if method == "static_graph" {
			method = "weighted_repoindex_edge"
		}
	}
	return contextengine.GraphGrounding{
		Source:     source,
		Method:     method,
		Static:     static,
		Heuristic:  heuristic,
		Confidence: confidence,
	}
}

func repoindexEdgeMetadata(edge repoindex.Edge) map[string]any {
	if len(edge.Meta) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(edge.Meta, &out); err != nil {
		return nil
	}
	return out
}

func (a *ReadOnlyAdapter) contextGraphLocalFallbacks(ctx context.Context, req contextengine.ContextGraphRequest, rootPaths []string, rootByPath map[string]repoindex.Node, indexedEdges map[string]repoindex.Edge) ([]contextengine.ContextGraphNode, []contextengine.ContextGraphEdge, []contextengine.ContextGraphGap) {
	nodes := make([]contextengine.ContextGraphNode, 0)
	edges := make([]contextengine.ContextGraphEdge, 0)
	gaps := make([]contextengine.ContextGraphGap, 0)
	if req.Budget.MaxLocalFiles <= 0 || req.Budget.MaxLocalBytes <= 0 {
		return nodes, edges, gaps
	}

	rootHasIndexedEdge := make(map[string]bool, len(rootByPath))
	for _, edge := range indexedEdges {
		for path, node := range rootByPath {
			if edge.Src == node.ID || edge.Dst == node.ID {
				rootHasIndexedEdge[path] = true
			}
		}
	}

	for _, rootPath := range rootPaths {
		rootNode, ok := rootByPath[rootPath]
		if !ok {
			continue
		}
		if req.IncludeTests {
			for _, candidate := range a.contextGraphExistingTestCompanions(rootPath) {
				node := localContextGraphNode(candidate, "test", "test_companion", 0.72)
				nodes = append(nodes, node)
				edges = append(edges, localContextGraphEdge(node.ID, rootNode.ID, "tests", "test_companion", 0.72))
			}
		}
		if req.IncludeAdjacent {
			for _, candidate := range a.contextGraphAdjacentFiles(rootPath, req) {
				node := localContextGraphNode(candidate.path, candidate.role, "adjacent_file", candidate.confidence)
				nodes = append(nodes, node)
				edges = append(edges, localContextGraphEdge(rootNode.ID, node.ID, "adjacent", "adjacent_file", candidate.confidence))
			}
		}
		if !rootHasIndexedEdge[rootPath] {
			refs, capped := a.contextGraphReverseReferenceFallback(ctx, rootPath, req)
			for _, candidate := range refs {
				node := localContextGraphNode(candidate, "dependent", "reverse_reference", 0.62)
				nodes = append(nodes, node)
				edges = append(edges, localContextGraphEdge(node.ID, rootNode.ID, "refers_to", "reverse_reference", 0.62))
			}
			if capped {
				gaps = append(gaps, contextengine.ContextGraphGap{
					ID:       "local_reverse_reference_budget_exhausted",
					Kind:     "budget_exhausted",
					Severity: "warn",
					Message:  "Local reverse-reference fallback was capped by budget.",
					Roots:    []string{rootPath},
				})
			}
		}
	}
	return nodes, edges, gaps
}

type contextGraphAdjacentCandidate struct {
	path       string
	role       string
	confidence float64
}

func (a *ReadOnlyAdapter) contextGraphExistingTestCompanions(rootPath string) []string {
	candidates := contextGraphTestCompanionCandidates(rootPath)
	out := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if a.contextGraphPathLoadable(path) {
			out = append(out, path)
		}
	}
	return appendUniqueStringsEnv(nil, out...)
}

func contextGraphTestCompanionCandidates(path string) []string {
	path = normalizeGraphPath(path)
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if dir == "." {
		dir = ""
	}
	join := func(name string) string {
		if dir == "" {
			return name
		}
		return filepath.ToSlash(filepath.Join(dir, name))
	}
	out := []string{
		join(stem + "_test" + ext),
		join(stem + ".test" + ext),
		join(stem + ".spec" + ext),
	}
	if ext == ".py" {
		out = append(out, filepath.ToSlash(filepath.Join("tests", "test_"+stem+ext)), join("test_"+stem+ext))
	}
	return out
}

func (a *ReadOnlyAdapter) contextGraphAdjacentFiles(rootPath string, req contextengine.ContextGraphRequest) []contextGraphAdjacentCandidate {
	rootPath = normalizeGraphPath(rootPath)
	dir := filepath.Dir(rootPath)
	if dir == "." {
		dir = ""
	}
	fullDir := filepath.Join(a.workspaceRoot, filepath.FromSlash(dir))
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return nil
	}
	out := make([]contextGraphAdjacentCandidate, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidate := filepath.ToSlash(filepath.Join(dir, entry.Name()))
		if candidate == rootPath || contextGraphPathExcluded(candidate, req.ExcludedPaths) {
			continue
		}
		role, confidence := contextGraphAdjacentRole(candidate)
		if role == "" {
			continue
		}
		out = append(out, contextGraphAdjacentCandidate{path: candidate, role: role, confidence: confidence})
		if len(out) >= req.Budget.PerNodeCap {
			break
		}
	}
	return out
}

func contextGraphAdjacentRole(path string) (string, float64) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".yaml", ".yml", ".toml", ".env", ".ini":
		return "adjacent_config", 0.66
	case ".sql", ".proto", ".graphql", ".avsc":
		return "schema", 0.68
	case ".md", ".mdx", ".rst", ".txt":
		return "adjacent_doc", 0.58
	case ".csv", ".tsv", ".parquet":
		return "data", 0.56
	default:
		return "", 0
	}
}

func (a *ReadOnlyAdapter) contextGraphReverseReferenceFallback(ctx context.Context, rootPath string, req contextengine.ContextGraphRequest) ([]string, bool) {
	rootPath = normalizeGraphPath(rootPath)
	stem := strings.TrimSuffix(filepath.Base(rootPath), filepath.Ext(rootPath))
	if stem == "" {
		return nil, false
	}
	needlePath := strings.TrimSuffix(rootPath, filepath.Ext(rootPath))
	maxFiles := req.Budget.MaxLocalFiles
	maxBytes := req.Budget.MaxLocalBytes
	scannedFiles := 0
	scannedBytes := int64(0)
	capped := false
	out := make([]string, 0)
	err := filepath.WalkDir(a.workspaceRoot, func(full string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			capped = true
			return ctx.Err()
		default:
		}
		rel, ok := relGraphPath(a.workspaceRoot, full)
		if !ok || rel == "" {
			return nil
		}
		if entry.IsDir() {
			if contextGraphSkipLocalDir(entry.Name(), rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == rootPath || contextGraphPathExcluded(rel, req.ExcludedPaths) || !contextGraphLocalTextFile(rel) {
			return nil
		}
		if scannedFiles >= maxFiles || scannedBytes >= maxBytes {
			capped = true
			return filepath.SkipAll
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > 256*1024 {
			return nil
		}
		scannedFiles++
		scannedBytes += info.Size()
		body, readErr := os.ReadFile(full)
		if readErr != nil {
			return nil
		}
		text := string(body)
		if strings.Contains(text, stem) || strings.Contains(text, needlePath) {
			out = append(out, rel)
			if len(out) >= req.Budget.PerNodeCap {
				capped = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil && ctx.Err() != nil {
		capped = true
	}
	return appendUniqueStringsEnv(nil, out...), capped
}

func relGraphPath(root, full string) (string, bool) {
	rel, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return normalizeGraphPath(rel), true
}

func contextGraphSkipLocalDir(name, rel string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".next", "coverage", "repoindex":
		return true
	}
	return strings.Contains(rel, "/.git/")
}

func contextGraphLocalTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".ex", ".exs", ".cs", ".rs", ".java", ".kt", ".rb", ".php", ".md", ".json", ".yaml", ".yml", ".toml", ".sql", ".proto", ".graphql":
		return true
	default:
		return false
	}
}

func localContextGraphNode(path, role, method string, confidence float64) contextengine.ContextGraphNode {
	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: path}
	return contextengine.ContextGraphNode{
		ID:         "local:file:" + path,
		Path:       path,
		Kind:       "file",
		Role:       role,
		Language:   languageFromGraphPath(path),
		Ref:        ref,
		LoadRef:    contextengine.FormatEvidenceRef(ref),
		Confidence: confidence,
		Grounding: contextengine.GraphGrounding{
			Source:     "local_heuristic",
			Method:     method,
			Heuristic:  true,
			Confidence: confidence,
		},
	}
}

func localContextGraphEdge(from, to, edgeType, method string, confidence float64) contextengine.ContextGraphEdge {
	return contextengine.ContextGraphEdge{
		From:       from,
		To:         to,
		Type:       edgeType,
		Confidence: confidence,
		Grounding: contextengine.GraphGrounding{
			Source:     "local_heuristic",
			Method:     method,
			Heuristic:  true,
			Confidence: confidence,
		},
	}
}

func appendMissingContextGraphNodes(nodes []contextengine.ContextGraphNode, extra ...contextengine.ContextGraphNode) []contextengine.ContextGraphNode {
	seen := make(map[string]struct{}, len(nodes)+len(extra))
	for _, node := range nodes {
		seen[node.ID] = struct{}{}
	}
	for _, node := range extra {
		if _, ok := seen[node.ID]; ok {
			continue
		}
		seen[node.ID] = struct{}{}
		nodes = append(nodes, node)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Role != nodes[j].Role {
			return nodes[i].Role > nodes[j].Role
		}
		if nodes[i].Path != nodes[j].Path {
			return nodes[i].Path < nodes[j].Path
		}
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func appendMissingContextGraphEdges(edges []contextengine.ContextGraphEdge, extra ...contextengine.ContextGraphEdge) []contextengine.ContextGraphEdge {
	seen := make(map[string]struct{}, len(edges)+len(extra))
	for _, edge := range edges {
		seen[contextGraphEdgeKey(edge)] = struct{}{}
	}
	for _, edge := range extra {
		key := contextGraphEdgeKey(edge)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		edges = append(edges, edge)
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Type != edges[j].Type {
			return edges[i].Type < edges[j].Type
		}
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

func contextGraphEdgeKey(edge contextengine.ContextGraphEdge) string {
	return edge.From + "|" + edge.Type + "|" + edge.To
}

func computeContextGraphConfidence(req contextengine.ContextGraphRequest, requested []string, roots []contextengine.ContextGraphNode, nodes []contextengine.ContextGraphNode, edges []contextengine.ContextGraphEdge, gaps []contextengine.ContextGraphGap, meta repoindex.IndexMeta, current repoindex.GitSnapshot, metaOK bool) contextengine.ContextGraphConfidence {
	rootResolution := ratioFloat(len(roots), len(requested))
	loadability := 1.0
	blocking := false
	warnings := 0
	for _, gap := range gaps {
		if gap.Severity == "block" {
			blocking = true
		}
		if gap.Severity == "warn" {
			warnings++
		}
		if gap.Kind == "root_unloadable" {
			loadability = 1 - ratioFloat(len(gap.Roots), len(requested))
		}
	}
	freshness := 1.0
	freshnessStatus := repoindex.CompareIndexFreshness(meta, current)
	if !metaOK {
		freshness = 0.55
	} else {
		switch freshnessStatus.Level {
		case repoindex.FreshnessCurrent:
			freshness = 1.0
		case repoindex.FreshnessDirty:
			freshness = 0.78
		case repoindex.FreshnessBehind:
			freshness = 0.72
		case repoindex.FreshnessStale:
			freshness = 0.55
		default:
			freshness = 0.6
		}
	}
	graphCoverage := 0.0
	if len(roots) > 0 {
		withEdges := map[string]struct{}{}
		for _, edge := range edges {
			if _, ok := rootIDSet(roots)[edge.From]; ok {
				withEdges[edge.From] = struct{}{}
			}
			if _, ok := rootIDSet(roots)[edge.To]; ok {
				withEdges[edge.To] = struct{}{}
			}
		}
		graphCoverage = ratioFloat(len(withEdges), len(roots))
	}
	edgeGrounding := 0.0
	if len(edges) > 0 {
		for _, edge := range edges {
			edgeGrounding += edge.Confidence
		}
		edgeGrounding /= float64(len(edges))
	}
	sourceDiversity := 0.0
	if len(edges) > 0 {
		sourceDiversity = 0.7
	}
	reductionCoverage := 1.0
	if len(req.CoverageRequirements) > 0 && len(requested) == 0 {
		reductionCoverage = 0
	}
	overall := 0.18*rootResolution +
		0.12*1.0 +
		0.14*reductionCoverage +
		0.22*graphCoverage +
		0.12*edgeGrounding +
		0.10*loadability +
		0.08*freshness +
		0.06*sourceDiversity
	if blocking {
		overall -= 0.25
	}
	overall -= float64(warnings) * 0.04
	overall = clampGraphConfidence(overall)
	completeness := "low"
	if overall >= 0.82 && !blocking {
		completeness = "high"
	} else if overall >= 0.62 && rootResolution > 0 && !blocking {
		completeness = "medium"
	}
	return contextengine.ContextGraphConfidence{
		Overall:            overall,
		Completeness:       completeness,
		RootResolution:     rootResolution,
		CandidateAdmission: 1.0,
		ReductionCoverage:  reductionCoverage,
		GraphCoverage:      graphCoverage,
		EdgeGrounding:      edgeGrounding,
		Loadability:        clampGraphConfidence(loadability),
		Freshness:          freshness,
		SourceDiversity:    sourceDiversity,
		TrustedForProceed:  completeness == "high" && !blocking,
	}
}

func rootIDSet(nodes []contextengine.ContextGraphNode) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		out[node.ID] = struct{}{}
	}
	return out
}

func contextGraphIndexStale(meta repoindex.IndexMeta, current repoindex.GitSnapshot) bool {
	status := repoindex.CompareIndexFreshness(meta, current)
	return status.Level != repoindex.FreshnessCurrent
}

func (a *ReadOnlyAdapter) contextGraphPathLoadable(path string) bool {
	path = normalizeGraphPath(path)
	if path == "" || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
		return false
	}
	root, err := filepath.Abs(a.workspaceRoot)
	if err != nil {
		return false
	}
	full := filepath.Join(root, filepath.FromSlash(path))
	clean, err := filepath.Abs(full)
	if err != nil {
		return false
	}
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return false
	}
	info, err := os.Stat(clean)
	return err == nil && !info.IsDir()
}

func normalizeGraphPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	return path
}

func normalizePathListEnv(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path = normalizeGraphPath(path); path != "" {
			out = append(out, path)
		}
	}
	return appendUniqueStringsEnv(nil, out...)
}

func cleanContextGraphStrings(values []string) []string {
	return appendUniqueStringsEnv(nil, values...)
}

func sortedRepoindexNodes(nodes map[string]repoindex.Node) []repoindex.Node {
	out := make([]repoindex.Node, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedRepoindexEdges(edges map[string]repoindex.Edge) []repoindex.Edge {
	out := make([]repoindex.Edge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		return edgeKeyEnv(out[i]) < edgeKeyEnv(out[j])
	})
	return out
}

func edgeKeyEnv(edge repoindex.Edge) string {
	return edge.Src + "|" + edge.Dst + "|" + string(edge.Type)
}

func directionsToStrings(directions []repoindex.Direction) []string {
	out := make([]string, 0, len(directions))
	for _, direction := range directions {
		out = append(out, string(direction))
	}
	return out
}

func ratioFloat(num, denom int) float64 {
	if denom <= 0 {
		return 1
	}
	return float64(num) / float64(denom)
}

func clampGraphConfidence(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func languageFromGraphPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript"
	case ".py":
		return "python"
	case ".ex", ".exs":
		return "elixir"
	case ".rs":
		return "rust"
	case ".cs":
		return "csharp"
	case ".md", ".mdx":
		return "markdown"
	default:
		return ""
	}
}
