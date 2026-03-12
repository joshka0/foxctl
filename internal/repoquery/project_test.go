package repoquery

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
)

func TestProjector_FromNodes(t *testing.T) {
	nodes := []repoindex.Node{
		{Kind: repoindex.NodeSymbol, File: "internal/example.go", Name: "Hello", SpanStart: 12, Summary: "symbol summary"},
		{Kind: repoindex.NodeFile, File: "internal/file.go", SpanStart: 3, Summary: "file summary"},
		{Kind: repoindex.NodeConcept, File: "deploy/api.yaml", Name: "Deployment/default/api", SpanStart: 7, Summary: "concept summary"},
		{Kind: repoindex.NodeConcept, Name: "ignored"},
	}

	anchors := (Projector{}).FromNodes(nodes)
	if len(anchors) != 3 {
		t.Fatalf("len(anchors) = %d, want 3", len(anchors))
	}
	if anchors[0].Path != "internal/example.go" || anchors[0].SymbolID == "" {
		t.Fatalf("unexpected symbol anchor: %#v", anchors[0])
	}
	if anchors[1].Path != "internal/file.go" || anchors[1].SymbolID != "" {
		t.Fatalf("unexpected file anchor: %#v", anchors[1])
	}
	if anchors[2].Path != "deploy/api.yaml" || anchors[2].SymbolName != "Deployment/default/api" {
		t.Fatalf("unexpected concept anchor: %#v", anchors[2])
	}
}
