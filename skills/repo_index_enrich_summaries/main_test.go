package main

import (
	"slices"
	"testing"
)

func TestBuildEnrichSummariesArgs(t *testing.T) {
	got := buildEnrichSummariesArgs("/tmp/repo", Input{DryRun: true})

	for _, want := range []string{
		"index",
		"repo",
		"enrich",
		"summaries",
		"--workspace",
		"/tmp/repo",
		"--dry-run=true",
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("args=%v want %q", got, want)
		}
	}
}
