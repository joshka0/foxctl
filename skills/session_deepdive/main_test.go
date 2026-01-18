package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/deep-dive", command)
}

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 10, defaultLimit)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionID:    "sess-123",
		ChunkIndex:   5,
		ChunkIndices: []int{1, 2, 3},
		Query:        "search query",
		Limit:        20,
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, 5, in.ChunkIndex)
	assert.Equal(t, []int{1, 2, 3}, in.ChunkIndices)
	assert.Equal(t, "search query", in.Query)
	assert.Equal(t, 20, in.Limit)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionID:    "sess-abc",
		ChunkIndex:   10,
		ChunkIndices: []int{5, 10, 15},
		Limit:        50,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.ChunkIndex, decoded.ChunkIndex)
	assert.Equal(t, in.ChunkIndices, decoded.ChunkIndices)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.SessionID)
	assert.Zero(t, in.ChunkIndex)
	assert.Nil(t, in.ChunkIndices)
	assert.Empty(t, in.Query)
	assert.Zero(t, in.Limit)
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		SessionID:   "sess-123",
		ArchivePath: "/archives/session.gz",
		Chunks:      []ChunkDetail{{Index: 1, Type: "user_request"}},
		TotalFound:  1,
		Status:      "ok",
		Message:     "Retrieved 1 chunk",
	}

	assert.Equal(t, "sess-123", output.SessionID)
	assert.Equal(t, "/archives/session.gz", output.ArchivePath)
	assert.Len(t, output.Chunks, 1)
	assert.Equal(t, 1, output.TotalFound)
	assert.Equal(t, "ok", output.Status)
	assert.Equal(t, "Retrieved 1 chunk", output.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		SessionID:   "sess-test",
		ArchivePath: "/test/archive.gz",
		TotalFound:  5,
		Status:      "ok",
		Message:     "Test message",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.SessionID, decoded.SessionID)
	assert.Equal(t, output.ArchivePath, decoded.ArchivePath)
	assert.Equal(t, output.TotalFound, decoded.TotalFound)
	assert.Equal(t, output.Status, decoded.Status)
}

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{"ok", "no_chunks"}

	for _, status := range statuses {
		output := Output{Status: status}
		assert.Equal(t, status, output.Status)
	}
}

// Tests for ChunkDetail structure

func TestChunkDetail_AllFields(t *testing.T) {
	detail := ChunkDetail{
		Index:          5,
		Type:           "tool_use",
		ContentPreview: "Running command...",
		RawContent:     json.RawMessage(`{"type":"tool_use"}`),
		ToolsUsed:      []string{"Bash", "Read"},
		FilesTouched:   []string{"main.go", "test.go"},
		HasError:       true,
		ErrorType:      "exec_error",
		ByteOffset:     1000,
		ByteLength:     500,
	}

	assert.Equal(t, 5, detail.Index)
	assert.Equal(t, "tool_use", detail.Type)
	assert.Equal(t, "Running command...", detail.ContentPreview)
	assert.NotNil(t, detail.RawContent)
	assert.Len(t, detail.ToolsUsed, 2)
	assert.Len(t, detail.FilesTouched, 2)
	assert.True(t, detail.HasError)
	assert.Equal(t, "exec_error", detail.ErrorType)
	assert.Equal(t, int64(1000), detail.ByteOffset)
	assert.Equal(t, int64(500), detail.ByteLength)
}

func TestChunkDetail_JSONSerialization(t *testing.T) {
	detail := ChunkDetail{
		Index:          10,
		Type:           "user_request",
		ContentPreview: "Test preview",
		ToolsUsed:      []string{"Edit"},
		HasError:       false,
		ByteOffset:     500,
		ByteLength:     200,
	}

	data, err := json.Marshal(detail)
	assert.NoError(t, err)

	var decoded ChunkDetail
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, detail.Index, decoded.Index)
	assert.Equal(t, detail.Type, decoded.Type)
	assert.Equal(t, detail.ContentPreview, decoded.ContentPreview)
	assert.Equal(t, detail.ToolsUsed, decoded.ToolsUsed)
	assert.Equal(t, detail.HasError, decoded.HasError)
	assert.Equal(t, detail.ByteOffset, decoded.ByteOffset)
	assert.Equal(t, detail.ByteLength, decoded.ByteLength)
}

func TestChunkDetail_TypeValues(t *testing.T) {
	types := []string{"user_request", "assistant_response", "tool_use", "tool_output", "error", "compact_boundary"}

	for _, chunkType := range types {
		detail := ChunkDetail{Type: chunkType}
		assert.Equal(t, chunkType, detail.Type)
	}
}

func TestChunkDetail_EmptyFields(t *testing.T) {
	detail := ChunkDetail{}

	assert.Zero(t, detail.Index)
	assert.Empty(t, detail.Type)
	assert.Empty(t, detail.ContentPreview)
	assert.Nil(t, detail.RawContent)
	assert.Nil(t, detail.ToolsUsed)
	assert.Nil(t, detail.FilesTouched)
	assert.False(t, detail.HasError)
	assert.Empty(t, detail.ErrorType)
	assert.Zero(t, detail.ByteOffset)
	assert.Zero(t, detail.ByteLength)
}

// Tests for limit default logic

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

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		SessionID:    "sess-1",
		ChunkIndex:   1,
		ChunkIndices: []int{1, 2},
		Query:        "test",
		Limit:        10,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "session_id")
	assert.Contains(t, jsonStr, "chunk_index")
	assert.Contains(t, jsonStr, "chunk_indices")
	assert.Contains(t, jsonStr, "query")
	assert.Contains(t, jsonStr, "limit")
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
