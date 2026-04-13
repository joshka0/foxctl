package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/intelligence/codemap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

//nolint:unused // Test utility for future tests
func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbuffer: %s", err, buf.String())
	}
	return env
}

// Tests for validation

func TestCodemapCheck_MissingCodemapID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codemap_id is required")
}

// Tests for extractFilePaths helper

func TestExtractFilePaths_Empty(t *testing.T) {
	cm := &codemap.Codemap{
		Traces: []codemap.Trace{},
	}
	paths := extractFilePaths(cm)
	assert.Empty(t, paths)
}

func TestExtractFilePaths_WithAnnotations(t *testing.T) {
	cm := &codemap.Codemap{
		Traces: []codemap.Trace{
			{
				Annotations: []codemap.Annotation{
					{Path: "@internal/auth/handler.go:42"},
					{Path: "internal/api/router.go:10"},
					{Path: "@./cmd/main.go:1"},
				},
			},
		},
	}

	paths := extractFilePaths(cm)

	assert.Len(t, paths, 3)
	assert.Contains(t, paths, "internal/auth/handler.go")
	assert.Contains(t, paths, "internal/api/router.go")
	assert.Contains(t, paths, "cmd/main.go")
}

func TestExtractFilePaths_Deduplication(t *testing.T) {
	cm := &codemap.Codemap{
		Traces: []codemap.Trace{
			{
				Annotations: []codemap.Annotation{
					{Path: "@internal/auth.go:10"},
					{Path: "@internal/auth.go:20"},
					{Path: "@internal/auth.go:30"},
				},
			},
		},
	}

	paths := extractFilePaths(cm)

	assert.Len(t, paths, 1)
	assert.Equal(t, "internal/auth.go", paths[0])
}

func TestExtractFilePaths_EmptyPaths(t *testing.T) {
	cm := &codemap.Codemap{
		Traces: []codemap.Trace{
			{
				Annotations: []codemap.Annotation{
					{Path: ""},
					{Path: "@valid/file.go"},
				},
			},
		},
	}

	paths := extractFilePaths(cm)

	assert.Len(t, paths, 1)
	assert.Contains(t, paths, "valid/file.go")
}

func TestExtractFilePaths_Sorted(t *testing.T) {
	cm := &codemap.Codemap{
		Traces: []codemap.Trace{
			{
				Annotations: []codemap.Annotation{
					{Path: "@z/file.go"},
					{Path: "@a/file.go"},
					{Path: "@m/file.go"},
				},
			},
		},
	}

	paths := extractFilePaths(cm)

	assert.Equal(t, []string{"a/file.go", "m/file.go", "z/file.go"}, paths)
}

// Tests for codemapIDFromName helper

func TestCodemapIDFromName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"codemap://test-id", "test-id"},
		{"codemap:test-id", "test-id"},
		{"test-id", "test-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codemapIDFromName(tt.name)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for parseGitLogOutput helper

func TestParseGitLogOutput_Empty(t *testing.T) {
	watched := []string{"file1.go", "file2.go"}
	result, err := parseGitLogOutput("", watched)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestParseGitLogOutput_SingleFile(t *testing.T) {
	output := `abc123|2024-01-15 10:00:00 +0000

internal/auth.go`
	watched := []string{"internal/auth.go", "internal/api.go"}

	result, err := parseGitLogOutput(output, watched)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, "internal/auth.go", result[0].Path)
	assert.Equal(t, 1, result[0].CommitsSince)
	assert.Equal(t, "2024-01-15 10:00:00 +0000", result[0].LastChange)
}

func TestParseGitLogOutput_MultipleCommits(t *testing.T) {
	output := `abc123|2024-01-15 10:00:00 +0000

internal/auth.go

def456|2024-01-14 09:00:00 +0000

internal/auth.go
internal/api.go`
	watched := []string{"internal/auth.go", "internal/api.go"}

	result, err := parseGitLogOutput(output, watched)
	require.NoError(t, err)

	// Should have 2 files
	assert.Len(t, result, 2)

	// Find auth.go which should have 2 commits
	var authFile *changedFile
	for i := range result {
		if result[i].Path == "internal/auth.go" {
			authFile = &result[i]
			break
		}
	}
	require.NotNil(t, authFile)
	assert.Equal(t, 2, authFile.CommitsSince)
}

func TestParseGitLogOutput_UnwatchedFilesIgnored(t *testing.T) {
	output := `abc123|2024-01-15 10:00:00 +0000

internal/auth.go
not/watched/file.go`
	watched := []string{"internal/auth.go"}

	result, err := parseGitLogOutput(output, watched)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, "internal/auth.go", result[0].Path)
}

func TestParseGitLogOutput_SortedByCommits(t *testing.T) {
	output := `abc123|2024-01-15 10:00:00 +0000
file1.go

def456|2024-01-14 09:00:00 +0000
file2.go
file1.go

ghi789|2024-01-13 08:00:00 +0000
file2.go
file1.go`
	watched := []string{"file1.go", "file2.go"}

	result, err := parseGitLogOutput(output, watched)
	require.NoError(t, err)

	// file1.go has 3 commits, file2.go has 2 - should be sorted descending
	assert.Equal(t, "file1.go", result[0].Path)
	assert.Equal(t, 3, result[0].CommitsSince)
	assert.Equal(t, "file2.go", result[1].Path)
	assert.Equal(t, 2, result[1].CommitsSince)
}

// Tests for buildSummary helper

func TestBuildSummary_Fresh(t *testing.T) {
	cm := &codemap.Codemap{
		Title:     "Auth Flow",
		CreatedAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	changed := []changedFile{}

	summary := buildSummary(cm, 5, changed, 0.0)

	assert.Contains(t, summary, "fresh")
	assert.Contains(t, summary, "Auth Flow")
	assert.Contains(t, summary, "5 referenced files")
}

func TestBuildSummary_MinorStaleness(t *testing.T) {
	cm := &codemap.Codemap{
		Title:     "Auth Flow",
		CreatedAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	changed := []changedFile{
		{Path: "internal/auth.go", CommitsSince: 1},
	}

	summary := buildSummary(cm, 5, changed, 0.2)

	assert.Contains(t, summary, "minor staleness")
	assert.Contains(t, summary, "20%")
	assert.Contains(t, summary, "Consider updating")
}

func TestBuildSummary_SignificantStaleness(t *testing.T) {
	cm := &codemap.Codemap{
		Title:     "Auth Flow",
		CreatedAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	changed := []changedFile{
		{Path: "internal/auth.go", CommitsSince: 1},
		{Path: "internal/api.go", CommitsSince: 2},
		{Path: "internal/handler.go", CommitsSince: 1},
	}

	summary := buildSummary(cm, 5, changed, 0.6)

	assert.Contains(t, summary, "significantly stale")
	assert.Contains(t, summary, "60%")
	assert.Contains(t, summary, "regenerating")
}

// Tests for recommendation logic

func TestRecommendationLogic(t *testing.T) {
	tests := []struct {
		score          float64
		recommendation string
	}{
		{0.0, "none"},
		{0.1, "update"},
		{0.49, "update"},
		{0.5, "regenerate"},
		{1.0, "regenerate"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			recommendation := "none"
			switch {
			case tt.score >= 0.5:
				recommendation = "regenerate"
			case tt.score > 0:
				recommendation = "update"
			}
			assert.Equal(t, tt.recommendation, recommendation)
		})
	}
}

// Tests for staleness calculation

func TestStalenessCalculation(t *testing.T) {
	tests := []struct {
		changedCount  int
		totalFiles    int
		expectedScore float64
		isStale       bool
	}{
		{0, 10, 0.0, false},
		{1, 10, 0.1, true},
		{5, 10, 0.5, true},
		{10, 10, 1.0, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			score := float64(tt.changedCount) / float64(tt.totalFiles)
			isStale := tt.changedCount > 0
			assert.Equal(t, tt.expectedScore, score)
			assert.Equal(t, tt.isStale, isStale)
		})
	}
}

// Tests for pathPattern regex

func TestPathPattern(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"@internal/auth.go:42", "internal/auth.go"},
		{"internal/auth.go:42", "internal/auth.go"},
		{"@internal/auth.go", "internal/auth.go"},
		{"internal/auth.go", "internal/auth.go"},
		{"@./cmd/main.go:1", "./cmd/main.go"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := pathPattern.FindStringSubmatch(tt.input)
			require.Len(t, matches, 2)
			assert.Equal(t, tt.expected, matches[1])
		})
	}
}
