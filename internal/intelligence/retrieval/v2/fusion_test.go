package retrievalv2

import (
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

func TestFuseReciprocalRank(t *testing.T) {
	hits := map[SourceID][]SourceHit{
		SourceLexical: {
			{Source: SourceLexical, ID: "a", Rank: 1, Document: searchindex.Document{ID: "a", Path: "a.go", GroupKey: "a.go"}, Score: 1.0},
			{Source: SourceLexical, ID: "b", Rank: 2, Document: searchindex.Document{ID: "b", Path: "b.go", GroupKey: "b.go"}, Score: 0.8},
		},
		SourceVector: {
			{Source: SourceVector, ID: "a", Rank: 1, Document: searchindex.Document{ID: "a", Path: "a.go", GroupKey: "a.go"}, Score: 0.9},
			{Source: SourceVector, ID: "c", Rank: 2, Document: searchindex.Document{ID: "c", Path: "c.go", GroupKey: "c.go"}, Score: 0.9},
		},
	}

	fused, _ := Fuse(hits, FuseOptions{Mode: FuseModeRRF, TopK: 5, SourceWeights: map[SourceID]float64{SourceLexical: 1, SourceVector: 0.8}})
	if len(fused) != 3 {
		t.Fatalf("expected 3 fused hits, got %d", len(fused))
	}
	if fused[0].Document.Path != "a.go" {
		t.Fatalf("expected first hit a.go, got %q", fused[0].Document.Path)
	}
	if len(fused[0].Contributions) != 2 {
		t.Fatalf("expected two source contributions for a.go, got %d", len(fused[0].Contributions))
	}
}

func TestFuseDeduplicatesDuplicateDocsInSource(t *testing.T) {
	hits := map[SourceID][]SourceHit{
		SourceLexical: {
			{Source: SourceLexical, ID: "a", Rank: 1, Document: searchindex.Document{ID: "a", Path: "a.go", GroupKey: "a.go"}, Score: 1.0},
			{Source: SourceLexical, ID: "a", Rank: 2, Document: searchindex.Document{ID: "a", Path: "a.go", GroupKey: "a.go"}, Score: 0.8},
		},
	}

	fused, _ := Fuse(hits, FuseOptions{Mode: FuseModeWeighted, SourceWeights: map[SourceID]float64{SourceLexical: 1}})
	if len(fused) != 1 {
		t.Fatalf("expected duplicate doc to dedupe to one hit, got %d", len(fused))
	}
	if len(fused[0].Contributions) != 1 {
		t.Fatalf("expected one contribution for deduped hit, got %d", len(fused[0].Contributions))
	}
}
