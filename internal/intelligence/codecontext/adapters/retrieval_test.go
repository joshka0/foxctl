package adapters

import (
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

func TestSearchHitToCandidate_MapsPathSymbolAndLine(t *testing.T) {
	hit := searchindex.SearchHit{
		Doc: searchindex.Document{
			Path:     "internal/example.go",
			SymbolID: "internal/example.go:Hello",
		},
		Score: 0.82,
	}
	hit.Doc.Anchor = searchindex.Anchor{
		Type:      searchindex.AnchorSymbol,
		Line:      45,
		Path:      "internal/example.go",
		StartLine: 10,
	}

	candidate := SearchHitToCandidate(hit)

	if candidate.Path != "internal/example.go" {
		t.Fatalf("Path = %q, want internal/example.go", candidate.Path)
	}
	if candidate.SymbolID != "internal/example.go:Hello" {
		t.Fatalf("SymbolID = %q, want internal/example.go:Hello", candidate.SymbolID)
	}
	if candidate.LineHint != 45 {
		t.Fatalf("LineHint = %d, want 45", candidate.LineHint)
	}
	if candidate.Priority != 0.82 {
		t.Fatalf("Priority = %v, want 0.82", candidate.Priority)
	}
	if len(candidate.Anchors) == 0 {
		t.Fatalf("expected anchors to be populated")
	}
	if candidate.Anchors[0].Kind != codecontext.AnchorSymbol {
		t.Fatalf("first anchor kind = %q, want %q", candidate.Anchors[0].Kind, codecontext.AnchorSymbol)
	}
}

func TestSearchHitToCandidate_FallsBackToAnchorPathAndStartLine(t *testing.T) {
	hit := searchindex.SearchHit{
		Doc:   searchindex.Document{},
		Score: 1.2,
	}
	hit.Doc.Anchor = searchindex.Anchor{
		Type:      searchindex.AnchorLine,
		Path:      "internal/anchors.go",
		StartLine: 7,
	}

	candidate := SearchHitToCandidate(hit)

	if candidate.Path != "internal/anchors.go" {
		t.Fatalf("Path = %q, want internal/anchors.go", candidate.Path)
	}
	if candidate.LineHint != 7 {
		t.Fatalf("LineHint = %d, want 7", candidate.LineHint)
	}
	if candidate.Priority != 1 {
		t.Fatalf("Priority = %v, want 1", candidate.Priority)
	}
	if candidate.SymbolID != "" {
		t.Fatalf("SymbolID should be empty, got %q", candidate.SymbolID)
	}
	if len(candidate.Anchors) == 0 || candidate.Anchors[0].Kind != codecontext.AnchorLine {
		t.Fatalf("expected line anchor, got %#v", candidate.Anchors)
	}
}

func TestSearchHitsToCandidates_SkipsEmptyPaths(t *testing.T) {
	hits := []searchindex.SearchHit{
		{Doc: searchindex.Document{Path: ""}, Score: 0.4},
		{Doc: searchindex.Document{Path: "internal/first.go"}, Score: 0.1},
	}

	candidates := SearchHitsToCandidates(hits)
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if candidates[0].Path != "internal/first.go" {
		t.Fatalf("first path = %q, want internal/first.go", candidates[0].Path)
	}
}

func TestSearchHitsToCandidates_PreservesOrder(t *testing.T) {
	hits := []searchindex.SearchHit{
		{Doc: searchindex.Document{Path: "first.go"}, Score: 0.8},
		{Doc: searchindex.Document{Path: "second.go"}, Score: 0.9},
	}

	candidates := SearchHitsToCandidates(hits)

	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(candidates))
	}
	if candidates[0].Path != "first.go" || candidates[1].Path != "second.go" {
		t.Fatalf("preserved order lost: %#v", candidates)
	}
	if candidates[0].Priority != 0.8 || candidates[1].Priority != 0.9 {
		t.Fatalf("unexpected priorities: %#v", candidates)
	}
}

func TestSearchHitToCandidate_RoundTripsToCollectInput(t *testing.T) {
	hit := searchindex.SearchHit{
		Doc:   searchindex.Document{Path: "collect.go"},
		Score: 0.77,
	}
	if !runCollectInputRoundTrip(t, SearchHitToCandidate(hit), codecontext.Candidate{Path: "collect.go", Priority: 0.77}) {
		t.Fatal("candidate mapping mismatch")
	}
}

func TestGroupsToCandidates_PreservesAnchors(t *testing.T) {
	groups := []Group{
		{
			Path:    "internal/example.go",
			Score:   0.9,
			Summary: "example summary",
			Anchors: []AnchorHit{
				{
					Anchor:     searchindex.Anchor{Type: searchindex.AnchorSymbol, Path: "internal/example.go", StartLine: 12, EndLine: 20},
					Score:      0.9,
					Source:     "lexical",
					SymbolID:   "internal/example.go:Hello",
					SymbolName: "Hello",
				},
			},
		},
	}

	candidates := GroupsToCandidates(groups)
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if candidates[0].Path != "internal/example.go" {
		t.Fatalf("path = %q", candidates[0].Path)
	}
	if len(candidates[0].Anchors) != 1 {
		t.Fatalf("expected anchor preserved, got %#v", candidates[0].Anchors)
	}
	if candidates[0].Anchors[0].Kind != codecontext.AnchorSymbol {
		t.Fatalf("anchor kind = %q", candidates[0].Anchors[0].Kind)
	}
}

func runCollectInputRoundTrip(t *testing.T, got codecontext.Candidate, want codecontext.Candidate) bool {
	t.Helper()
	if got.Path != want.Path {
		t.Errorf("Path = %q, want %q", got.Path, want.Path)
		return false
	}
	if got.SymbolID != want.SymbolID {
		t.Errorf("SymbolID = %q, want %q", got.SymbolID, want.SymbolID)
		return false
	}
	if got.Priority != want.Priority {
		t.Errorf("Priority = %v, want %v", got.Priority, want.Priority)
		return false
	}
	if got.LineHint != want.LineHint {
		t.Errorf("LineHint = %d, want %d", got.LineHint, want.LineHint)
		return false
	}
	return true
}
