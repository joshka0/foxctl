package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/codecontext"
)

func TestApplyDefaultLimits(t *testing.T) {
	tests := []struct {
		name string
		in   Limits
		want Limits
	}{
		{
			name: "all defaults",
			in:   Limits{},
			want: Limits{
				MaxFiles:        DefaultMaxFiles,
				MaxSnippets:     DefaultMaxSnippets,
				MaxBytesPerFile: DefaultMaxBytesPerFile,
			},
		},
		{
			name: "preserve explicit values",
			in:   Limits{MaxFiles: 5, MaxSnippets: 10, MaxBytesPerFile: 1024},
			want: Limits{MaxFiles: 5, MaxSnippets: 10, MaxBytesPerFile: 1024},
		},
		{
			name: "fill partial defaults",
			in:   Limits{MaxFiles: 5},
			want: Limits{MaxFiles: 5, MaxSnippets: DefaultMaxSnippets, MaxBytesPerFile: DefaultMaxBytesPerFile},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyDefaultLimits(tt.in)
			if got != tt.want {
				t.Fatalf("applyDefaultLimits(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestFatalErrorForEvidence(t *testing.T) {
	t.Run("processed files means no fatal error", func(t *testing.T) {
		evidence := &codecontext.Evidence{
			Stats: codecontext.EvidenceStats{FilesProcessed: 1},
		}
		if err := fatalErrorForEvidence(evidence); err != nil {
			t.Fatalf("expected nil error, got %+v", err)
		}
	})

	t.Run("policy error wins", func(t *testing.T) {
		evidence := &codecontext.Evidence{
			Stats: codecontext.EvidenceStats{
				FileErrors: []codecontext.FileError{
					{Code: ErrCodePolicy, Message: "path escapes workspace"},
				},
			},
		}
		err := fatalErrorForEvidence(evidence)
		if err == nil || err.Code != ErrCodePolicy {
			t.Fatalf("expected %s, got %+v", ErrCodePolicy, err)
		}
	})

	t.Run("not found maps to ENOTFOUND", func(t *testing.T) {
		evidence := &codecontext.Evidence{
			Stats: codecontext.EvidenceStats{
				FileErrors: []codecontext.FileError{
					{Code: ErrCodeNotFound, Message: "file not found"},
				},
			},
		}
		err := fatalErrorForEvidence(evidence)
		if err == nil || err.Code != ErrCodeNotFound {
			t.Fatalf("expected %s, got %+v", ErrCodeNotFound, err)
		}
	})
}

func TestRunValidationGuards(t *testing.T) {
	in := Input{}
	if in.Question != "" || in.Query != "" || len(in.Candidates) != 0 {
		t.Fatalf("unexpected input defaults: %+v", in)
	}
}

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

func TestApplySnippetInlineModePreview(t *testing.T) {
	previews := make([]codecontext.SnippetPreview, 0, 20)
	for i := 0; i < 20; i++ {
		previews = append(previews, codecontext.SnippetPreview{File: "a.go", Preview: "x"})
	}
	data := map[string]any{
		"snippets_inline": previews,
		"artifact":        "sha256:abc",
		"truncated":       false,
	}
	applySnippetInlineMode(data, InlineModeAuto)
	got, _ := data["snippets_inline"].([]codecontext.SnippetPreview)
	if mode, _ := data["inline_mode"].(string); mode != string(InlineModePreview) {
		t.Fatalf("inline_mode=%q want preview", mode)
	}
	if len(got) != defaultPreviewSnippets {
		t.Fatalf("snippets_inline=%d want %d", len(got), defaultPreviewSnippets)
	}
	if truncated, _ := data["truncated"].(bool); !truncated {
		t.Fatal("expected truncated preview")
	}
}
