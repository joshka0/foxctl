package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 10, defaultLimit)
}

// Tests for Input structure

func TestChunkDetail_TypeValues(t *testing.T) {
	types := []string{"user_request", "assistant_response", "tool_use", "tool_output", "error", "compact_boundary"}

	for _, chunkType := range types {
		detail := ChunkDetail{Type: chunkType}
		assert.Equal(t, chunkType, detail.Type)
	}
}

func TestInput_LimitDefault(t *testing.T) {
	in := Input{}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 10, limit)
}

func TestInput_LimitNegative(t *testing.T) {
	in := Input{Limit: -5}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 10, limit)
}

func TestInput_LimitZero(t *testing.T) {
	in := Input{Limit: 0}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 10, limit)
}

func TestInput_LimitPositive(t *testing.T) {
	in := Input{Limit: 25}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 25, limit)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		SessionID:    "sess-full",
		ChunkIndex:   15,
		ChunkIndices: []int{1, 5, 10, 15, 20},
		Query:        "full test query",
		Limit:        100,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.ChunkIndex, decoded.ChunkIndex)
	assert.Equal(t, in.ChunkIndices, decoded.ChunkIndices)
	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestOutput_NoChunksResult(t *testing.T) {
	output := Output{
		SessionID:   "sess-empty",
		ArchivePath: "/archives/empty.gz",
		Chunks:      []ChunkDetail{},
		TotalFound:  0,
		Status:      "no_chunks",
		Message:     "No chunks found for this session",
	}

	assert.Equal(t, "no_chunks", output.Status)
	assert.Empty(t, output.Chunks)
	assert.Zero(t, output.TotalFound)
}

func TestChunkDetail_WithRawContent(t *testing.T) {
	rawJSON := `{"type":"tool_use","name":"Bash","input":{"command":"ls"}}`
	detail := ChunkDetail{
		Index:      1,
		Type:       "tool_use",
		RawContent: json.RawMessage(rawJSON),
	}

	data, err := json.Marshal(detail)
	assert.NoError(t, err)

	var decoded ChunkDetail
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.NotNil(t, decoded.RawContent)
	assert.Contains(t, string(decoded.RawContent), "tool_use")
}

func TestOutput_MultipleChunks(t *testing.T) {
	output := Output{
		SessionID:   "sess-multi",
		ArchivePath: "/archives/multi.gz",
		Chunks: []ChunkDetail{
			{Index: 1, Type: "user_request"},
			{Index: 2, Type: "assistant_response"},
			{Index: 3, Type: "tool_use"},
		},
		TotalFound: 3,
		Status:     "ok",
	}

	assert.Len(t, output.Chunks, 3)
	assert.Equal(t, 3, output.TotalFound)
	assert.Equal(t, "user_request", output.Chunks[0].Type)
	assert.Equal(t, "assistant_response", output.Chunks[1].Type)
	assert.Equal(t, "tool_use", output.Chunks[2].Type)
}
