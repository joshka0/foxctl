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
