package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
)

func TestParseInlineMode(t *testing.T) {
	tests := []struct {
		in      string
		want    InlineMode
		wantErr bool
	}{
		{"", InlineModeAuto, false},
		{"auto", InlineModeAuto, false},
		{"full", InlineModeFull, false},
		{"preview", InlineModePreview, false},
		{"artifact_only", InlineModeArtifactOnly, false},
		{"bad", InlineModeAuto, true},
	}

	for _, tt := range tests {
		got, err := parseInlineMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseInlineMode(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseInlineMode(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestTrimDAGResultLimitsNodesEdgesAndSeeds(t *testing.T) {
	result := repoindex.DAGGrepResult{
		Seeds: []repoindex.ScoredNode{
			{Node: repoindex.Node{ID: "n1"}},
			{Node: repoindex.Node{ID: "n2"}},
			{Node: repoindex.Node{ID: "n3"}},
		},
		Graph: repoindex.ExpandResult{
			Nodes: []repoindex.Node{
				{ID: "n1", Kind: repoindex.NodeSymbol, Doc: "doc1", Summary: "summary1"},
				{ID: "n2", Kind: repoindex.NodeSymbol, Doc: "doc2", Summary: "summary2"},
				{ID: "n3", Kind: repoindex.NodeSymbol, Doc: "doc3", Summary: "summary3"},
			},
			Edges: []repoindex.Edge{
				{Src: "n1", Dst: "n2", Type: repoindex.EdgeCalls},
				{Src: "n2", Dst: "n3", Type: repoindex.EdgeCalls},
			},
		},
		DAG: repoindex.DAGView{
			Layers: map[string]int{"n1": 0, "n2": 1, "n3": 2},
			Edges: []repoindex.Edge{
				{Src: "n1", Dst: "n2", Type: repoindex.EdgeCalls},
				{Src: "n2", Dst: "n3", Type: repoindex.EdgeCalls},
			},
		},
	}
	result.Stats.NodeCount = 3
	result.Stats.EdgeCount = 2

	preview := buildDAGPreview(result, "")
	if preview.InlineMode != string(InlineModePreview) {
		t.Fatalf("inline_mode=%q want preview", preview.InlineMode)
	}
	if preview.NodeCountTotal != 3 || preview.EdgeCountTotal != 2 {
		t.Fatalf("totals=%d/%d want 3/2", preview.NodeCountTotal, preview.EdgeCountTotal)
	}
	if !preview.Truncated {
		t.Fatal("expected truncated preview")
	}
}
