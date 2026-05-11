package repoindex

import "context"

// ExportSnapshot is the canonical read-only repoindex graph image used by
// projection builders. It carries stored repoindex state only; projection-only
// rows belong in the exporter, not in the store contract.
type ExportSnapshot struct {
	RepoKey    string
	Meta       IndexMeta
	FileStates []FileState
	Nodes      []Node
	Edges      []Edge
}

// ExportSnapshot reads a deterministic, uncapped graph snapshot for projection
// builders without exercising traversal/search APIs.
func (s *Store) ExportSnapshot(ctx context.Context) (ExportSnapshot, error) {
	meta, err := s.GetMeta(ctx)
	if err != nil {
		return ExportSnapshot{}, err
	}
	fileStates, err := s.ListFileStates(ctx)
	if err != nil {
		return ExportSnapshot{}, err
	}

	nodeKinds := []NodeKind{NodePackage, NodeFile, NodeSymbol, NodeConcept}
	var nodes []Node
	for _, kind := range nodeKinds {
		kindNodes, err := s.ListAllNodesByKind(ctx, kind)
		if err != nil {
			return ExportSnapshot{}, err
		}
		nodes = append(nodes, kindNodes...)
	}

	edges, err := s.ListAllEdges(ctx)
	if err != nil {
		return ExportSnapshot{}, err
	}

	return ExportSnapshot{
		RepoKey:    s.RepoKey(),
		Meta:       meta,
		FileStates: fileStates,
		Nodes:      nodes,
		Edges:      edges,
	}, nil
}
