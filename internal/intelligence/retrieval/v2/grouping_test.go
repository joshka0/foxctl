package retrievalv2

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/searchindex"
)

func TestGroupResultsByDocumentPath(t *testing.T) {
	hits := []FusedHit{
		{Document: searchindex.Document{ID: "a1", Path: "internal/a.go", GroupKey: "internal/a.go", Summary: "a summary", Anchor: searchindex.Anchor{Type: searchindex.AnchorSymbol, Path: "internal/a.go", StartLine: 10}, SymbolID: "a.go:One"}, Score: 0.8},
		{Document: searchindex.Document{ID: "a2", Path: "internal/a.go", GroupKey: "internal/a.go", Anchor: searchindex.Anchor{Type: searchindex.AnchorSymbol, Path: "internal/a.go", StartLine: 20}, SymbolID: "a.go:Two"}, Score: 0.6},
		{Document: searchindex.Document{ID: "b1", Path: "internal/b.go", GroupKey: "internal/b.go"}, Score: 0.9},
	}

	groups := GroupResults(hits, GroupOptions{Enabled: true, MaxGroups: 10, MaxMembers: 1})
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Path != "internal/b.go" {
		t.Fatalf("expected top group internal/b.go, got %q", groups[0].Path)
	}
	if len(groups[1].Hits) != 1 {
		t.Fatalf("expected max members respected")
	}
	if groups[0].HitCount != 1 {
		t.Fatalf("expected hit count 1 for top group, got %d", groups[0].HitCount)
	}
	if groups[1].Summary != "a summary" {
		t.Fatalf("expected summary propagated, got %q", groups[1].Summary)
	}
	if len(groups[1].Anchors) != 2 {
		t.Fatalf("expected two anchors to survive grouping, got %d", len(groups[1].Anchors))
	}
}
