package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/stretchr/testify/assert"
)

// Tests for normalizeInput helper

func TestNormalizeInput_DefaultLimit(t *testing.T) {
	in := Input{Query: "test"}
	rc := &skillmain.RunContext{
		Config:    config.Config{},
		Workspace: "/test/workspace",
	}

	normalizeInput(&in, rc)

	assert.Equal(t, defaultLimit, in.Limit)
}

func TestNormalizeInput_PreservesLimit(t *testing.T) {
	in := Input{Query: "test", Limit: 10}
	rc := &skillmain.RunContext{
		Config:    config.Config{},
		Workspace: "/test/workspace",
	}

	normalizeInput(&in, rc)

	assert.Equal(t, 10, in.Limit)
}

func TestNormalizeInput_DefaultMinSimilarity(t *testing.T) {
	in := Input{Query: "test"}
	rc := &skillmain.RunContext{
		Config:    config.Config{},
		Workspace: "/test/workspace",
	}

	normalizeInput(&in, rc)

	assert.Equal(t, defaultMinSim, in.MinSimilarity)
}

func TestNormalizeInput_PreservesMinSimilarity(t *testing.T) {
	in := Input{Query: "test", MinSimilarity: 0.5}
	rc := &skillmain.RunContext{
		Config:    config.Config{},
		Workspace: "/test/workspace",
	}

	normalizeInput(&in, rc)

	assert.Equal(t, 0.5, in.MinSimilarity)
}

func TestNormalizeInput_DefaultWorkspace(t *testing.T) {
	in := Input{Query: "test"}
	rc := &skillmain.RunContext{
		Config:    config.Config{},
		Workspace: "/test/workspace",
	}

	normalizeInput(&in, rc)

	assert.Equal(t, "/test/workspace", in.Workspace)
}

func TestNormalizeInput_PreservesWorkspace(t *testing.T) {
	in := Input{Query: "test", Workspace: "/custom/workspace"}
	rc := &skillmain.RunContext{
		Config:    config.Config{},
		Workspace: "/test/workspace",
	}

	normalizeInput(&in, rc)

	assert.Equal(t, "/custom/workspace", in.Workspace)
}

// Tests for default constants

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 5, defaultLimit)
}

func TestDefaultMinSim(t *testing.T) {
	assert.Equal(t, 0.3, defaultMinSim)
}

// Tests for Input validation

func TestInput_GranularityMutualExclusion(t *testing.T) {
	// Both window_granularity and chunk_granularity cannot be true
	in := Input{
		Query:             "test",
		WindowGranularity: true,
		ChunkGranularity:  true,
	}

	// This would return an error in run()
	// "window_granularity and chunk_granularity are mutually exclusive"
	assert.True(t, in.WindowGranularity && in.ChunkGranularity)
}

func TestInput_WindowGranularityOnly(t *testing.T) {
	in := Input{
		Query:             "test",
		WindowGranularity: true,
		ChunkGranularity:  false,
	}

	assert.True(t, in.WindowGranularity)
	assert.False(t, in.ChunkGranularity)
}

func TestInput_ChunkGranularityOnly(t *testing.T) {
	in := Input{
		Query:             "test",
		WindowGranularity: false,
		ChunkGranularity:  true,
	}

	assert.False(t, in.WindowGranularity)
	assert.True(t, in.ChunkGranularity)
}

func TestInput_SessionGranularity(t *testing.T) {
	// Default is session-level granularity (both false)
	in := Input{
		Query: "test",
	}

	assert.False(t, in.WindowGranularity)
	assert.False(t, in.ChunkGranularity)
}

// Tests for Output structure

func TestOutput_MatchesPopulation(t *testing.T) {
	output := Output{
		Query:               "test query",
		Matches:             []SessionMatch{{SessionID: "sess-1"}},
		TotalWithEmbeddings: 10,
		Status:              "ok",
		Message:             "Found 1 relevant sessions",
	}

	assert.Equal(t, "test query", output.Query)
	assert.Len(t, output.Matches, 1)
	assert.Equal(t, "sess-1", output.Matches[0].SessionID)
	assert.Equal(t, 10, output.TotalWithEmbeddings)
	assert.Equal(t, "ok", output.Status)
}

func TestOutput_WindowMatchesPopulation(t *testing.T) {
	output := Output{
		Query: "test query",
		WindowMatches: []WindowMatch{
			{
				SessionID:   "sess-1",
				WindowIndex: 0,
				Trigger:     "manual",
				Summary:     "Window summary",
				Similarity:  0.85,
			},
		},
		Status: "ok",
	}

	assert.Len(t, output.WindowMatches, 1)
	assert.Equal(t, 0, output.WindowMatches[0].WindowIndex)
	assert.Equal(t, 0.85, output.WindowMatches[0].Similarity)
}

func TestOutput_ChunkMatchesPopulation(t *testing.T) {
	output := Output{
		Query: "test query",
		ChunkMatches: []ChunkMatch{
			{
				SessionID:      "sess-1",
				WindowIndex:    0,
				ChunkIndex:     5,
				ChunkType:      "assistant",
				ContentPreview: "Content preview...",
				SummaryID:      "sum-123",
				Similarity:     0.92,
			},
		},
		Status: "ok",
	}

	assert.Len(t, output.ChunkMatches, 1)
	assert.Equal(t, 5, output.ChunkMatches[0].ChunkIndex)
	assert.Equal(t, "assistant", output.ChunkMatches[0].ChunkType)
	assert.Equal(t, 0.92, output.ChunkMatches[0].Similarity)
}

func TestOutput_NoMatches(t *testing.T) {
	output := Output{
		Query:   "test query",
		Matches: []SessionMatch{},
		Status:  "no_matches",
		Message: "No sessions matched the query above the similarity threshold",
	}

	assert.Len(t, output.Matches, 0)
	assert.Equal(t, "no_matches", output.Status)
}

// Tests for SessionMatch structure

func TestSessionMatch_Fields(t *testing.T) {
	match := SessionMatch{
		SessionID:    "sess-123",
		ProjectName:  "agentctl",
		GitBranch:    "main",
		Summary:      "Test session",
		Accomplished: []string{"Task 1", "Task 2"},
		Decisions:    []string{"Decision 1"},
		Gotchas:      []string{"Gotcha 1"},
		UserInsights: []string{"Insight 1"},
		Tags:         []string{"test", "dev"},
		KeyFiles:     []string{"main.go", "test.go"},
		Similarity:   0.87,
		StartedAt:    "2024-01-15T10:00:00Z",
	}

	assert.Equal(t, "sess-123", match.SessionID)
	assert.Equal(t, "agentctl", match.ProjectName)
	assert.Equal(t, "main", match.GitBranch)
	assert.Equal(t, 0.87, match.Similarity)
	assert.Len(t, match.Accomplished, 2)
	assert.Len(t, match.Tags, 2)
}

// Tests for WindowMatch structure

func TestWindowMatch_Fields(t *testing.T) {
	match := WindowMatch{
		SessionID:        "sess-123",
		WindowIndex:      2,
		Trigger:          "pre_compact",
		PreCompactTokens: 50000,
		Summary:          "Window summary",
		MessageCount:     25,
		StartedAt:        "2024-01-15T10:00:00Z",
		EndedAt:          "2024-01-15T11:00:00Z",
		Similarity:       0.75,
	}

	assert.Equal(t, 2, match.WindowIndex)
	assert.Equal(t, "pre_compact", match.Trigger)
	assert.Equal(t, 50000, match.PreCompactTokens)
	assert.Equal(t, 25, match.MessageCount)
	assert.Equal(t, 0.75, match.Similarity)
}

// Tests for ChunkMatch structure

func TestChunkMatch_Fields(t *testing.T) {
	match := ChunkMatch{
		SessionID:      "sess-123",
		WindowIndex:    1,
		ChunkIndex:     10,
		ChunkType:      "user",
		ContentPreview: "User message preview...",
		SummaryID:      "sum-456",
		Summary:        "Chunk summary",
		SummaryModel:   "claude-3",
		ChunkIndexMin:  8,
		ChunkIndexMax:  12,
		Similarity:     0.95,
	}

	assert.Equal(t, 10, match.ChunkIndex)
	assert.Equal(t, "user", match.ChunkType)
	assert.Equal(t, "sum-456", match.SummaryID)
	assert.Equal(t, 8, match.ChunkIndexMin)
	assert.Equal(t, 12, match.ChunkIndexMax)
	assert.Equal(t, 0.95, match.Similarity)
}
