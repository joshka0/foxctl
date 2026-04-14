package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
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

func TestTrimExpandResultLimitsPayload(t *testing.T) {
	result := repoindex.ExpandResult{
		Nodes: []repoindex.Node{
			{ID: "n1", Doc: "doc1", Summary: "summary1"},
			{ID: "n2", Doc: "doc2", Summary: "summary2"},
			{ID: "n3", Doc: "doc3", Summary: "summary3"},
		},
		Edges: []repoindex.Edge{
			{Src: "n1", Dst: "n2", Type: repoindex.EdgeCalls},
			{Src: "n2", Dst: "n3", Type: repoindex.EdgeCalls},
			{Src: "n1", Dst: "n3", Type: repoindex.EdgeCalls},
		},
		Trail: []string{"a", "b", "c", "d"},
	}
	anchors := []repoquery.Anchor{
		{Path: "a.go", Summary: "one"},
		{Path: "b.go", Summary: "two"},
		{Path: "c.go", Summary: "three"},
	}

	trimmed, trimmedAnchors := trimExpandResult(result, anchors, 2, 1, 2)
	if len(trimmed.Nodes) != 2 {
		t.Fatalf("nodes=%d want 2", len(trimmed.Nodes))
	}
	if len(trimmed.Edges) != 1 {
		t.Fatalf("edges=%d want 1", len(trimmed.Edges))
	}
	if len(trimmedAnchors) != 2 {
		t.Fatalf("anchors=%d want 2", len(trimmedAnchors))
	}
}
