package main

import (
	"context"
	"sort"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/intelligence/branchimpact"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/errors"
)

type graphProvider struct {
	engine *repoindex.QueryEngine
	store  *repoindex.Store
}

func openGraphProvider(ctx context.Context, rc *skillmain.RunContext, workspace string) (*graphProvider, error) {
	exists, err := repoindex.StoreExists(rc.Config.Storage.Root, workspace)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	store, err := repoindex.Open(ctx, rc.Config.Storage.Root, workspace)
	if err != nil {
		return nil, err
	}
	return &graphProvider{engine: repoindex.NewQueryEngine(store), store: store}, nil
}

func (p *graphProvider) close() {
	if p != nil && p.store != nil {
		errors.Ignore(p.store.Close(), "close repoindex store")
	}
}

func (p *graphProvider) BlastRadius(ctx context.Context, changes []branchimpact.Change, opts branchimpact.GraphOptions) (branchimpact.GraphResult, error) {
	if p == nil || p.engine == nil {
		return branchimpact.GraphResult{Available: false, Reason: "repoindex store not available"}, nil
	}
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.IsDeleted {
			continue
		}
		paths = append(paths, change.Path)
	}
	nodes, err := p.engine.ResolveFileNodes(ctx, paths)
	if err != nil {
		return branchimpact.GraphResult{}, err
	}
	if len(nodes) == 0 {
		return branchimpact.GraphResult{Available: false, Reason: "changed files not present in repoindex"}, nil
	}

	seen := make(map[string]branchimpact.GraphCandidate)
	for _, node := range nodes {
		blast, err := p.engine.BlastRadius(ctx, repoindex.BlastRadiusOptions{
			NodeID:     node.ID,
			MaxDepth:   opts.Depth,
			Limit:      opts.PerFileCap,
			PerNodeCap: opts.PerFileCap,
		})
		if err != nil {
			return branchimpact.GraphResult{}, err
		}
		addGraphCandidates(seen, candidatesFromBlast(blast))

		for _, symbol := range changedFileSymbols(blast, opts.PerFileCap) {
			contextResult, err := p.engine.SmartContext(ctx, repoindex.SmartContextOptions{
				NodeID: symbol.ID,
				Limit:  opts.PerFileCap,
			})
			if err != nil {
				return branchimpact.GraphResult{}, err
			}
			addGraphCandidates(seen, candidatesFromContextSections(contextResult.Sections, 1))
		}

		if opts.Limit > 0 && len(seen) >= opts.Limit {
			break
		}
	}
	candidates := make([]branchimpact.GraphCandidate, 0, len(seen))
	for _, candidate := range seen {
		candidates = append(candidates, candidate)
	}
	return branchimpact.GraphResult{Available: true, Candidates: candidates}, nil
}

func addGraphCandidates(seen map[string]branchimpact.GraphCandidate, candidates []branchimpact.GraphCandidate) {
	for _, candidate := range candidates {
		if candidate.Path == "" {
			continue
		}
		key := candidate.Path + "|" + candidate.Symbol
		if prev, ok := seen[key]; !ok || candidate.Depth < prev.Depth {
			seen[key] = candidate
		}
	}
}

func candidatesFromBlast(blast repoindex.BlastRadiusResult) []branchimpact.GraphCandidate {
	edgeTypes := edgeTypesByNode(blast.Graph.Edges)
	candidates := make([]branchimpact.GraphCandidate, 0, len(blast.Graph.Nodes))
	for _, graphNode := range blast.Graph.Nodes {
		if graphNode.File == "" {
			continue
		}
		candidates = append(candidates, branchimpact.GraphCandidate{
			Path:      graphNode.File,
			Symbol:    graphNode.Name,
			LineHint:  graphNode.SpanStart,
			Depth:     blast.Layers[graphNode.ID],
			EdgeTypes: edgeTypes[graphNode.ID],
		})
	}
	candidates = append(candidates, candidatesFromContextSections(blast.Sections, 1)...)
	return candidates
}

func candidatesFromContextSections(sections []repoindex.ContextSection, depth int) []branchimpact.GraphCandidate {
	var candidates []branchimpact.GraphCandidate
	for _, section := range sections {
		if !isImpactContextSection(section.Name) {
			continue
		}
		edgeTypes := edgeTypesByNode(section.Edges)
		for _, node := range section.Nodes {
			if node.File == "" {
				continue
			}
			candidates = append(candidates, branchimpact.GraphCandidate{
				Path:      node.File,
				Symbol:    node.Name,
				LineHint:  node.SpanStart,
				Depth:     depth,
				EdgeTypes: edgeTypes[node.ID],
			})
		}
	}
	return candidates
}

func isImpactContextSection(name string) bool {
	switch name {
	case "incoming_call", "callers", "co_changes", "docs_concepts":
		return true
	default:
		return false
	}
}

func changedFileSymbols(blast repoindex.BlastRadiusResult, limit int) []repoindex.Node {
	originFile := blast.Origin.File
	if originFile == "" {
		return nil
	}
	var symbols []repoindex.Node
	for _, node := range blast.Graph.Nodes {
		if node.Kind == repoindex.NodeSymbol && node.File == originFile {
			symbols = append(symbols, node)
		}
	}
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].SpanStart != symbols[j].SpanStart {
			return symbols[i].SpanStart < symbols[j].SpanStart
		}
		return symbols[i].Name < symbols[j].Name
	})
	if limit > 0 && len(symbols) > limit {
		return symbols[:limit]
	}
	return symbols
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

func appendStringOnce(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
