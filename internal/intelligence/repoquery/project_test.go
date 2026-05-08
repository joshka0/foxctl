package repoquery

import (
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
)

func TestProjector_FromNodes(t *testing.T) {
	repoKey := "repo"
	pkg := "go:internal/example"
	symbolID := repoindex.SymbolID(repoKey, pkg, "example.go/Hello")
	nodes := []repoindex.Node{
		{ID: symbolID, Kind: repoindex.NodeSymbol, Pkg: pkg, File: "internal/example.go", Name: "Hello", SpanStart: 12, Summary: "symbol summary"},
		{Kind: repoindex.NodeFile, File: "internal/file.go", SpanStart: 3, Summary: "file summary"},
		{Kind: repoindex.NodeConcept, File: "deploy/api.yaml", Name: "Deployment/default/api", SpanStart: 7, Summary: "concept summary"},
		{Kind: repoindex.NodeConcept, Name: "ignored"},
	}

	anchors := (Projector{}).FromNodes(nodes)
	if len(anchors) != 3 {
		t.Fatalf("len(anchors) = %d, want 3", len(anchors))
	}
	if anchors[0].Path != "internal/example.go" || anchors[0].SymbolID != symbolID {
		t.Fatalf("unexpected symbol anchor: %#v", anchors[0])
	}
	if anchors[0].SymbolID == "internal/example.go:Hello" {
		t.Fatalf("symbol anchor used path/name locator as durable ID: %#v", anchors[0])
	}
	if anchors[1].Path != "internal/file.go" || anchors[1].SymbolID != "" {
		t.Fatalf("unexpected file anchor: %#v", anchors[1])
	}
	if anchors[2].Path != "deploy/api.yaml" || anchors[2].SymbolName != "Deployment/default/api" {
		t.Fatalf("unexpected concept anchor: %#v", anchors[2])
	}
}
