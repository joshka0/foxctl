package adapters

import (
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestConvertSearchResponse(t *testing.T) {
	src := SearchResponse{
		Query: "architecture overview",
		Plan:  "semantic_search",
		Hits: []FusedHit{
			{
				Document:     "docs/arch.md",
				Score:        0.92,
				Title:        "Architecture",
				Snippet:      "The system uses events",
				Sources:      []string{"vault", "code"},
				SourceScores: map[string]float64{"vault": 0.8, "code": 0.12},
			},
		},
		Groups: []string{"architecture"},
		Stats:  SearchStats{TotalHits: 10, DurationMS: 150, TokensUsed: 500},
	}

	got := ConvertSearchResponse("ws1", src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Query != "architecture overview" {
		t.Errorf("Query = %q", got.Query)
	}
	if got.Lane != contextengine.LaneMixed {
		t.Errorf("Lane = %q", got.Lane)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("Nodes = %d, want 1", len(got.Nodes))
	}
	if got.Nodes[0].Ref.Ref != "docs/arch.md" {
		t.Errorf("Node.Ref.Ref = %q", got.Nodes[0].Ref.Ref)
	}
	if got.Nodes[0].Confidence != 0.92 {
		t.Errorf("Node.Confidence = %f", got.Nodes[0].Confidence)
	}
	if got.Telemetry.DurationMs != 150 {
		t.Errorf("Telemetry.DurationMs = %d", got.Telemetry.DurationMs)
	}
	if got.Telemetry.TokensUsed != 500 {
		t.Errorf("Telemetry.TokensUsed = %d", got.Telemetry.TokensUsed)
	}
}

func TestConvertFusedHit(t *testing.T) {
	src := FusedHit{
		Document: "src/main.go",
		Score:    0.85,
		Title:    "Main Entry",
		Snippet:  "func main()",
		Sources:  []string{"code"},
		Contributions: []HitContribution{
			{Source: "code", Score: 0.85},
		},
	}

	got := ConvertFusedHit("ws1", 0, src)

	if got.NodeType != contextengine.EvidenceNodeTypeRetrieval {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Ref.Ref != "src/main.go" {
		t.Errorf("Ref.Ref = %q", got.Ref.Ref)
	}
	if got.Confidence != 0.85 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
	if got.Grounding != contextengine.GroundingSemantic {
		t.Errorf("Grounding = %q", got.Grounding)
	}
	if got.Statement != "func main()" {
		t.Errorf("Statement = %q", got.Statement)
	}
}

func TestConvertSearchResponse_Empty(t *testing.T) {
	src := SearchResponse{
		Query: "test",
		Stats: SearchStats{},
	}
	got := ConvertSearchResponse("ws1", src)
	if len(got.Nodes) != 0 {
		t.Errorf("expected 0 nodes for empty hits, got %d", len(got.Nodes))
	}
}
