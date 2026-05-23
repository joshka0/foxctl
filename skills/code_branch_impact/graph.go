package main

import (
	"context"

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
		edgeTypes := edgeTypesByNode(blast.Graph.Edges)
		for _, graphNode := range blast.Graph.Nodes {
			if graphNode.File == "" {
				continue
			}
			depth := blast.Layers[graphNode.ID]
			candidate := branchimpact.GraphCandidate{
				Path:      graphNode.File,
				Symbol:    graphNode.Name,
				LineHint:  graphNode.SpanStart,
				Depth:     depth,
				EdgeTypes: edgeTypes[graphNode.ID],
			}
			key := candidate.Path + "|" + candidate.Symbol
			if prev, ok := seen[key]; !ok || candidate.Depth < prev.Depth {
				seen[key] = candidate
			}
			if opts.Limit > 0 && len(seen) >= opts.Limit {
				break
			}
		}
	}
	candidates := make([]branchimpact.GraphCandidate, 0, len(seen))
	for _, candidate := range seen {
		candidates = append(candidates, candidate)
	}
	return branchimpact.GraphResult{Available: true, Candidates: candidates}, nil
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
