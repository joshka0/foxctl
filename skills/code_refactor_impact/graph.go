package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/refactor/impact"
	"github.com/joshka0/foxctl/internal/platform/errors"
)

type structuralProvider struct {
	engine *repoindex.QueryEngine
	store  *repoindex.Store
}

type unavailableStructuralProvider struct {
	reason string
}

func openStructuralProvider(ctx context.Context, rc *skillmain.RunContext, workspace string) (impact.StructuralProvider, func()) {
	exists, err := repoindex.StoreExists(rc.Config.Storage.Root, workspace)
	if err != nil {
		return unavailableStructuralProvider{reason: fmt.Sprintf("check repoindex store: %v", err)}, func() {}
	}
	if !exists {
		return unavailableStructuralProvider{reason: "repoindex store not available"}, func() {}
	}
	store, err := repoindex.Open(ctx, rc.Config.Storage.Root, workspace)
	if err != nil {
		return unavailableStructuralProvider{reason: fmt.Sprintf("open repoindex: %v", err)}, func() {}
	}
	provider := &structuralProvider{engine: repoindex.NewQueryEngine(store), store: store}
	return provider, provider.close
}

func (p unavailableStructuralProvider) Candidates(context.Context, []impact.Target, impact.StructuralOptions) (impact.StructuralResult, error) {
	return impact.StructuralResult{Available: false, Reason: p.reason}, nil
}

func (p *structuralProvider) close() {
	if p != nil && p.store != nil {
		errors.Ignore(p.store.Close(), "close repoindex store")
	}
}

func (p *structuralProvider) Candidates(ctx context.Context, targets []impact.Target, opts impact.StructuralOptions) (impact.StructuralResult, error) {
	if p == nil || p.engine == nil {
		return impact.StructuralResult{Available: false, Reason: "repoindex graph provider not configured"}, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = impact.DefaultLimit
	}
	perTargetCap := opts.PerTargetCap
	if perTargetCap <= 0 {
		perTargetCap = impact.DefaultPerTargetCap
	}

	seeds, err := p.resolveSeeds(ctx, targets, perTargetCap)
	if err != nil {
		return impact.StructuralResult{}, err
	}
	if len(seeds) == 0 {
		return impact.StructuralResult{Available: false, Reason: "refactor targets not present in repoindex"}, nil
	}

	seen := make(map[string]impact.StructuralCandidate)
	for _, seed := range seeds {
		addStructuralCandidates(seen, []impact.StructuralCandidate{candidateFromNode(seed.Node, impact.SectionDirectTarget, seed.Target, 0, nil)})

		smart, err := p.engine.SmartContext(ctx, repoindex.SmartContextOptions{NodeID: seed.Node.ID, Limit: perTargetCap})
		if err != nil {
			return impact.StructuralResult{}, err
		}
		addStructuralCandidates(seen, candidatesFromSmartContext(smart, seed.Target))

		blast, err := p.engine.BlastRadius(ctx, repoindex.BlastRadiusOptions{
			NodeID:     seed.Node.ID,
			MaxDepth:   opts.Depth,
			Limit:      perTargetCap,
			PerNodeCap: perTargetCap,
		})
		if err != nil {
			return impact.StructuralResult{}, err
		}
		addStructuralCandidates(seen, candidatesFromBlastRadius(blast, seed.Target))
		if limit > 0 && len(seen) >= limit {
			break
		}
	}

	candidates := make([]impact.StructuralCandidate, 0, len(seen))
	for _, candidate := range seen {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Depth != candidates[j].Depth {
			return candidates[i].Depth < candidates[j].Depth
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Symbol < candidates[j].Symbol
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return impact.StructuralResult{Available: true, Candidates: candidates}, nil
}

type structuralSeed struct {
	Node   repoindex.Node
	Target impact.Target
}

func (p *structuralProvider) resolveSeeds(ctx context.Context, targets []impact.Target, limit int) ([]structuralSeed, error) {
	var seeds []structuralSeed
	seen := make(map[string]struct{})
	for _, target := range targets {
		nodes, err := p.resolveTargetNodes(ctx, target, limit)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.ID == "" {
				continue
			}
			key := node.ID + "|" + impact.TargetKey(target)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			seeds = append(seeds, structuralSeed{Node: node, Target: target})
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		if seeds[i].Node.File != seeds[j].Node.File {
			return seeds[i].Node.File < seeds[j].Node.File
		}
		return seeds[i].Node.Name < seeds[j].Node.Name
	})
	return seeds, nil
}

func (p *structuralProvider) resolveTargetNodes(ctx context.Context, target impact.Target, limit int) ([]repoindex.Node, error) {
	switch target.Kind {
	case impact.TargetFile:
		return p.engine.ResolveFileNodes(ctx, []string{target.Path})
	case impact.TargetSymbol:
		if target.Path != "" {
			nodes, err := p.resolveSymbolNodesInFile(ctx, target, limit)
			if err != nil {
				return nil, err
			}
			if len(nodes) > 0 {
				return nodes, nil
			}
		}
		return p.searchTargetNodes(ctx, target, target.Symbol, limit)
	case impact.TargetPackage:
		query := target.Package
		if query == "" {
			query = target.Path
		}
		return p.searchTargetNodes(ctx, target, query, limit)
	case impact.TargetContract:
		return p.searchTargetNodes(ctx, target, target.Contract, limit)
	default:
		return nil, nil
	}
}

func (p *structuralProvider) resolveSymbolNodesInFile(ctx context.Context, target impact.Target, limit int) ([]repoindex.Node, error) {
	files, err := p.engine.ResolveFileNodes(ctx, []string{target.Path})
	if err != nil {
		return nil, err
	}
	var out []repoindex.Node
	for _, file := range files {
		smart, err := p.engine.SmartContext(ctx, repoindex.SmartContextOptions{NodeID: file.ID, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, section := range smart.Sections {
			if section.Name != "contains_children" {
				continue
			}
			for _, node := range section.Nodes {
				if node.Kind != repoindex.NodeSymbol {
					continue
				}
				if node.Name != target.Symbol && !strings.HasSuffix(node.Name, "."+target.Symbol) {
					continue
				}
				out = append(out, node)
				if limit > 0 && len(out) >= limit {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

func (p *structuralProvider) searchTargetNodes(ctx context.Context, target impact.Target, query string, limit int) ([]repoindex.Node, error) {
	nodes, err := p.engine.Search(ctx, query, limit*4)
	if err != nil {
		return nil, err
	}
	filtered := make([]repoindex.Node, 0, len(nodes))
	for _, node := range nodes {
		if !nodeMatchesTarget(node, target) {
			continue
		}
		filtered = append(filtered, node)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

func nodeMatchesTarget(node repoindex.Node, target impact.Target) bool {
	if target.Path != "" && node.File != "" && node.File != target.Path {
		return false
	}
	switch target.Kind {
	case impact.TargetSymbol:
		return node.Kind == repoindex.NodeSymbol && (node.Name == target.Symbol || strings.HasSuffix(node.Name, "."+target.Symbol))
	case impact.TargetPackage:
		if target.Package != "" {
			return node.Kind == repoindex.NodePackage && (node.Name == target.Package || node.Pkg == target.Package)
		}
		return node.Kind == repoindex.NodeFile && node.File == target.Path
	case impact.TargetContract:
		return node.Name == target.Contract || node.Pkg == target.Contract || strings.Contains(node.Signature, target.Contract)
	default:
		return true
	}
}

func candidatesFromSmartContext(result repoindex.SmartContextResult, target impact.Target) []impact.StructuralCandidate {
	var candidates []impact.StructuralCandidate
	for _, section := range result.Sections {
		sectionKind := sectionForSmartContext(section.Name)
		edgeTypes := edgeTypesByNode(section.Edges)
		for _, node := range section.Nodes {
			if node.File == "" {
				continue
			}
			if skipSameFileExpansion(node, sectionKind, target) {
				continue
			}
			candidates = append(candidates, candidateFromNode(node, sectionKind, target, 1, edgeTypes[node.ID]))
		}
	}
	return candidates
}

func candidatesFromBlastRadius(result repoindex.BlastRadiusResult, target impact.Target) []impact.StructuralCandidate {
	edgeTypes := edgeTypesByNode(result.Graph.Edges)
	candidates := make([]impact.StructuralCandidate, 0, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		if node.File == "" {
			continue
		}
		depth := result.Layers[node.ID]
		section := sectionForEdges(edgeTypes[node.ID])
		if depth == 0 {
			section = impact.SectionDirectTarget
		}
		if skipSameFileExpansion(node, section, target) {
			continue
		}
		candidates = append(candidates, candidateFromNode(node, section, target, depth, edgeTypes[node.ID]))
	}
	for _, section := range result.Sections {
		sectionKind := sectionForSmartContext(section.Name)
		sectionEdges := edgeTypesByNode(section.Edges)
		for _, node := range section.Nodes {
			if node.File == "" {
				continue
			}
			if skipSameFileExpansion(node, sectionKind, target) {
				continue
			}
			candidates = append(candidates, candidateFromNode(node, sectionKind, target, 1, sectionEdges[node.ID]))
		}
	}
	return candidates
}

func skipSameFileExpansion(node repoindex.Node, section impact.StructuralSection, target impact.Target) bool {
	if target.Kind != impact.TargetFile || node.File != target.Path {
		return false
	}
	return section != impact.SectionDirectTarget
}

func candidateFromNode(node repoindex.Node, section impact.StructuralSection, target impact.Target, depth int, edgeTypes []string) impact.StructuralCandidate {
	return impact.StructuralCandidate{
		Path:        node.File,
		Symbol:      node.Name,
		LineHint:    node.SpanStart,
		Depth:       depth,
		EdgeTypes:   uniqueStrings(edgeTypes),
		Section:     section,
		TargetKey:   impact.TargetKey(target),
		TargetLabel: impact.TargetLabel(target),
	}
}

func sectionForSmartContext(name string) impact.StructuralSection {
	switch name {
	case "self":
		return impact.SectionDirectTarget
	case "incoming_call", "callers":
		return impact.SectionCaller
	case "callees":
		return impact.SectionCallee
	case "contains_in":
		return impact.SectionContainer
	case "contains_children":
		return impact.SectionChild
	case "docs_concepts":
		return impact.SectionDoc
	case "co_changes":
		return impact.SectionCochange
	default:
		return impact.SectionGraphNeighbor
	}
}

func sectionForEdges(edgeTypes []string) impact.StructuralSection {
	switch {
	case hasEdge(edgeTypes, string(repoindex.EdgeTests)):
		return impact.SectionTest
	case hasAnyEdge(edgeTypes, []repoindex.EdgeType{repoindex.EdgeDescribedBy, repoindex.EdgeDocRelated, repoindex.EdgeDocFlow, repoindex.EdgeDecidedBy}):
		return impact.SectionDoc
	case hasAnyEdge(edgeTypes, []repoindex.EdgeType{repoindex.EdgeImplements, repoindex.EdgeEmbeds, repoindex.EdgeEnforces, repoindex.EdgeImplementsProtocol}):
		return impact.SectionContract
	case hasAnyEdge(edgeTypes, []repoindex.EdgeType{repoindex.EdgeImports, repoindex.EdgeUsesSymbol, repoindex.EdgeRefersTo}):
		return impact.SectionImportRef
	case hasEdge(edgeTypes, string(repoindex.EdgeCalls)):
		return impact.SectionCallee
	case hasEdge(edgeTypes, string(repoindex.EdgeCoChangesWith)):
		return impact.SectionCochange
	case hasEdge(edgeTypes, string(repoindex.EdgeContains)):
		return impact.SectionChild
	default:
		return impact.SectionGraphNeighbor
	}
}

func addStructuralCandidates(seen map[string]impact.StructuralCandidate, candidates []impact.StructuralCandidate) {
	for _, candidate := range candidates {
		if candidate.Path == "" {
			continue
		}
		key := candidate.Path + "|" + candidate.Symbol + "|" + string(candidate.Section) + "|" + candidate.TargetKey
		prev, ok := seen[key]
		if !ok || candidate.Depth < prev.Depth {
			seen[key] = candidate
		}
	}
}

func edgeTypesByNode(edges []repoindex.Edge) map[string][]string {
	out := make(map[string][]string)
	for _, edge := range edges {
		value := string(edge.Type)
		out[edge.Src] = appendStringOnce(out[edge.Src], value)
		out[edge.Dst] = appendStringOnce(out[edge.Dst], value)
	}
	return out
}

func hasAnyEdge(values []string, targets []repoindex.EdgeType) bool {
	for _, target := range targets {
		if hasEdge(values, string(target)) {
			return true
		}
	}
	return false
}

func hasEdge(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = appendStringOnce(out, value)
	}
	sort.Strings(out)
	return out
}

func appendStringOnce(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
