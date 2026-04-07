package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/repoquery"
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

func TestTrimSearchOutput(t *testing.T) {
	out := Output{
		Count: 3,
		Results: []repoindex.Node{
			{ID: "n1", Doc: "doc1", Summary: "summary1"},
			{ID: "n2", Doc: "doc2", Summary: "summary2"},
			{ID: "n3", Doc: "doc3", Summary: "summary3"},
		},
		Anchors: []repoquery.Anchor{
			{Path: "a.go", Summary: "one"},
			{Path: "b.go", Summary: "two"},
		},
	}

	preview := trimSearchOutput(out)
	if preview.InlineMode != string(InlineModePreview) {
		t.Fatalf("inline_mode=%q want preview", preview.InlineMode)
	}
	if preview.ResultsTotal != 3 || preview.AnchorsTotal != 2 {
		t.Fatalf("totals=%d/%d want 3/2", preview.ResultsTotal, preview.AnchorsTotal)
	}
}
