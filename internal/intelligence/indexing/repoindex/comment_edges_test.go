package repoindex

import (
	"encoding/json"
	"testing"
	"time"

	docparser "github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex/parser"
)

func TestApplyCommentEdges(t *testing.T) {
	repoKey := "repo-test"
	src := Node{
		ID:        SymbolID(repoKey, "pkg", "file.go:Foo"),
		Kind:      NodeSymbol,
		Pkg:       "pkg",
		Name:      "Foo",
		UpdatedAt: time.Now().UTC(),
	}
	dst := Node{
		ID:        SymbolID(repoKey, "pkg", "file.go:Bar"),
		Kind:      NodeSymbol,
		Pkg:       "pkg",
		Name:      "Bar",
		UpdatedAt: time.Now().UTC(),
	}
	meta := docparser.DocIndex{
		Keywords: []string{"alpha"},
		Related:  []string{"Bar"},
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	src.Meta = payload

	nodes := map[string]Node{
		src.ID: src,
		dst.ID: dst,
	}
	edges := make(map[string]Edge)

	applyCommentEdges(nodes, edges, repoKey)

	conceptID := NamespacedID(repoKey, ConceptKeyword+"alpha")
	if _, ok := nodes[conceptID]; !ok {
		t.Fatalf("expected concept node %s", conceptID)
	}
	if !hasEdge(edges, src.ID, conceptID, EdgeHasKeyword) {
		t.Fatalf("expected keyword edge")
	}
	if !hasEdge(edges, src.ID, dst.ID, EdgeDocRelated) {
		t.Fatalf("expected related edge")
	}
}

func hasEdge(edges map[string]Edge, src, dst string, edgeType EdgeType) bool {
	key := src + "|" + dst + "|" + string(edgeType)
	_, ok := edges[key]
	return ok
}
