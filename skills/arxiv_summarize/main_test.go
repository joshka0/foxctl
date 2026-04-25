package main

import (
	"strings"
	"testing"
)

func TestResolvePDFURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "new id",
			input: "2401.12345",
			want:  "https://arxiv.org/pdf/2401.12345",
		},
		{
			name:  "versioned id",
			input: "2401.12345v2",
			want:  "https://arxiv.org/pdf/2401.12345v2",
		},
		{
			name:  "abs url",
			input: "https://arxiv.org/abs/2401.12345",
			want:  "https://arxiv.org/pdf/2401.12345",
		},
		{
			name:  "pdf url",
			input: "https://example.com/paper.pdf",
			want:  "https://example.com/paper.pdf",
		},
		{
			name:  "old id",
			input: "cs.AI/0701001",
			want:  "https://arxiv.org/pdf/cs.AI/0701001",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolvePDFURL(tt.input)
			if err != nil {
				t.Fatalf("resolvePDFURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolvePDFURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePDFURLRejectsUnsupportedInput(t *testing.T) {
	t.Parallel()
	if _, err := resolvePDFURL("not a paper"); err == nil {
		t.Fatal("resolvePDFURL() expected error")
	}
	if _, err := resolvePDFURL("ftp://example.com/paper.pdf"); err == nil {
		t.Fatal("resolvePDFURL() expected unsupported scheme error")
	}
}

func TestExtractContent(t *testing.T) {
	t.Parallel()
	got := extractContent(chatResponse{
		Choices: []chatChoice{{
			Message: responseMessage{Content: []any{
				map[string]any{"type": "text", "text": "first"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "x"}},
				map[string]any{"type": "text", "text": "second"},
			}},
		}},
	})
	if got != "first\nsecond" {
		t.Fatalf("extractContent() = %q", got)
	}
}

func TestFilenameFromURL(t *testing.T) {
	t.Parallel()
	got := filenameFromURL("https://arxiv.org/pdf/2401.12345", "application/pdf")
	if got != "2401.12345.pdf" {
		t.Fatalf("filenameFromURL() = %q", got)
	}
}

func TestBuildPromptModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   input
		want string
	}{
		{
			name: "outline",
			in:   input{Mode: "outline"},
			want: "Create a full outline",
		},
		{
			name: "implementation",
			in:   input{Mode: "implementation"},
			want: "implementing the method in code",
		},
		{
			name: "query",
			in:   input{Mode: "query", Query: "What data structures are needed?"},
			want: "What data structures are needed?",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildPrompt(tt.in)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("buildPrompt() = %q, want substring %q", got, tt.want)
			}
			if !strings.Contains(got, "Leave citations out") {
				t.Fatalf("buildPrompt() should include citation exclusion")
			}
		})
	}
}

func TestResultCacheKey(t *testing.T) {
	t.Parallel()
	a := resultCacheKey("pdf", "query", "  What   now? ", "prompt", "model", "native")
	b := resultCacheKey("pdf", "query", "What now?", "prompt", "model", "native")
	c := resultCacheKey("pdf", "implementation", "What now?", "prompt", "model", "native")
	if a != b {
		t.Fatalf("resultCacheKey should normalize query whitespace: %q != %q", a, b)
	}
	if a == c {
		t.Fatal("resultCacheKey should include mode")
	}
}
