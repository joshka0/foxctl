package main

import (
	"reflect"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/rgutil"
	"github.com/jkatigb/agentctl/internal/tools/ripgrep"
)

func TestBuildSearchOpts(t *testing.T) {
	tests := []struct {
		name        string
		in          rgutil.SearchInput
		workspace   string
		searchPath  string
		wantPattern string
		wantExclude []string
	}{
		{
			name:        "basic",
			in:          rgutil.SearchInput{Pattern: "foo"},
			workspace:   "/workspace",
			searchPath:  ".",
			wantPattern: "foo",
			wantExclude: rgutil.DefaultExcludeGlobs,
		},
		{
			name:        "custom exclude",
			in:          rgutil.SearchInput{Pattern: "foo", GlobNot: []string{"build", "dist"}},
			workspace:   "/workspace",
			searchPath:  "src",
			wantPattern: "foo",
			wantExclude: []string{"build", "dist"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := rgutil.BuildSearchOpts(tt.in, tt.workspace, tt.searchPath, nil)
			if opts.Pattern != tt.wantPattern {
				t.Errorf("Pattern = %q, want %q", opts.Pattern, tt.wantPattern)
			}
			if opts.WorkingDir != tt.workspace {
				t.Errorf("WorkingDir = %q, want %q", opts.WorkingDir, tt.workspace)
			}
			if opts.Path != tt.searchPath {
				t.Errorf("Path = %q, want %q", opts.Path, tt.searchPath)
			}
			if !reflect.DeepEqual(opts.ExcludeGlobs, tt.wantExclude) {
				t.Errorf("ExcludeGlobs = %v, want %v", opts.ExcludeGlobs, tt.wantExclude)
			}
		})
	}
}

func TestConvertMatches(t *testing.T) {
	rgMatches := []ripgrep.Match{
		{Path: "file.txt", Line: 1, Text: "hello world"},
	}

	results, fileHits := convertMatches(rgMatches)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].File != "file.txt" {
		t.Errorf("expected file.txt, got %s", results[0].File)
	}
	if fileHits["file.txt"] != 1 {
		t.Errorf("expected file hit count 1, got %d", fileHits["file.txt"])
	}
}
