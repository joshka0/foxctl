package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Query:     "analyze database connections",
		Workspace: "/var/project",
		Depth:     5,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Depth, decoded.Depth)
}

func TestInput_DepthValues(t *testing.T) {
	depths := []int{1, 2, 3, 4, 5}

	for _, d := range depths {
		in := input{Depth: d}
		assert.Equal(t, d, in.Depth)
	}
}

// Tests for buildCodemapSummary helper

func TestBuildCodemapSummary_TitleAndDescription(t *testing.T) {
	result := buildCodemapSummary("Auth Flow", "Complete authentication flow analysis", "fallback-id")
	assert.Equal(t, "Auth Flow - Complete authentication flow analysis", result)
}

func TestBuildCodemapSummary_TitleOnly(t *testing.T) {
	result := buildCodemapSummary("Error Handlers", "", "fallback-id")
	assert.Equal(t, "Error Handlers", result)
}

func TestBuildCodemapSummary_DescriptionOnly(t *testing.T) {
	result := buildCodemapSummary("", "Database connection analysis", "fallback-id")
	assert.Equal(t, "Database connection analysis", result)
}

func TestBuildCodemapSummary_Fallback(t *testing.T) {
	result := buildCodemapSummary("", "", "abc123")
	assert.Equal(t, "abc123", result)
}

func TestBuildCodemapSummary_EmptyFallback(t *testing.T) {
	result := buildCodemapSummary("", "", "")
	assert.Equal(t, "", result)
}

func TestBuildCodemapSummary_WhitespaceHandling(t *testing.T) {
	// Function doesn't trim whitespace, preserves as-is
	result := buildCodemapSummary("  Title  ", "  Description  ", "fallback")
	assert.Equal(t, "  Title   -   Description  ", result)
}

func TestBuildCodemapSummary_LongValues(t *testing.T) {
	longTitle := "This is a very long title that describes the codemap in great detail"
	longDesc := "This is an equally long description that provides additional context about what this codemap represents"
	result := buildCodemapSummary(longTitle, longDesc, "id")
	assert.Contains(t, result, longTitle)
	assert.Contains(t, result, longDesc)
	assert.Contains(t, result, " - ")
}

func TestBuildCodemapSummary_SpecialCharacters(t *testing.T) {
	result := buildCodemapSummary("Title with <special> & chars", "Desc with \"quotes\"", "id")
	assert.Contains(t, result, "<special>")
	assert.Contains(t, result, "&")
	assert.Contains(t, result, "\"quotes\"")
}

// Edge case tests

func TestInput_ZeroDepth(t *testing.T) {
	in := input{
		Query: "test query",
		Depth: 0,
	}
	assert.Zero(t, in.Depth)
}

func TestInput_NegativeDepth(t *testing.T) {
	in := input{
		Query: "test query",
		Depth: -1,
	}
	assert.Equal(t, -1, in.Depth)
}

func TestInput_LargeDepth(t *testing.T) {
	in := input{
		Query: "test query",
		Depth: 100,
	}
	assert.Equal(t, 100, in.Depth)
}

func TestInput_QueryOnly(t *testing.T) {
	in := input{
		Query: "find all API endpoints",
	}
	assert.Equal(t, "find all API endpoints", in.Query)
	assert.Empty(t, in.Workspace)
	assert.Zero(t, in.Depth)
}

func TestInput_WorkspaceOnly(t *testing.T) {
	in := input{
		Workspace: "/some/path",
	}
	assert.Empty(t, in.Query)
	assert.Equal(t, "/some/path", in.Workspace)
	assert.Zero(t, in.Depth)
}

func TestInput_ComplexQuery(t *testing.T) {
	in := input{
		Query:     "trace the auth flow from login through token validation to protected routes",
		Workspace: "/home/user/repos/myapp",
		Depth:     4,
	}
	assert.Contains(t, in.Query, "auth flow")
	assert.Contains(t, in.Query, "token validation")
}
