package main

import (
	"slices"
	"testing"
)

func TestBuildRepoIndexArgsDefaultsIncremental(t *testing.T) {
	got := buildRepoIndexArgs("/tmp/repo", Input{})

	for _, want := range []string{
		"index",
		"repo",
		"build",
		"--workspace",
		"/tmp/repo",
		"--go-pattern",
		"./...",
		"--go=true",
		"--typescript=true",
		"--progress=false",
		"--incremental=true",
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("args=%v want %q", got, want)
		}
	}
}

func TestBuildRepoIndexArgsCanForceFullRebuild(t *testing.T) {
	includeGo := false
	incremental := false

	got := buildRepoIndexArgs("/tmp/repo", Input{
		IncludeGo:   &includeGo,
		Incremental: &incremental,
	})

	for _, want := range []string{"--go=false", "--incremental=false"} {
		if !slices.Contains(got, want) {
			t.Fatalf("args=%v want %q", got, want)
		}
	}
}
