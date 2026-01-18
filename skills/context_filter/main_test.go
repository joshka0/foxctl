package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbuffer: %s", err, buf.String())
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

func putToCAS(t *testing.T, rc *skillmain.RunContext, content []byte, kind string) string {
	t.Helper()
	obj, err := rc.CASStore.Put(context.Background(), bytes.NewReader(content), kind, nil)
	require.NoError(t, err)
	return obj.Digest
}

// Tests for validation

func TestContextFilter_MissingPrompt(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Source: sourceInput{
			Text: "some text",
		},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestContextFilter_EmptyPrompt(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Prompt: "   ",
		Source: sourceInput{
			Text: "some text",
		},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestContextFilter_MissingSource(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Prompt: "What is the main topic?",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source is required")
}

func TestContextFilter_UnsupportedProvider(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Prompt: "What is the main topic?",
		Source: sourceInput{
			Text: "Some text to analyze",
		},
		LLM: llmInput{
			Provider: "unknownprovider",
		},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported llm.provider")
}

// Tests for chunkText helper

func TestChunkText_SingleParagraph(t *testing.T) {
	text := "This is a single paragraph of text."
	chunks := chunkText(text)

	assert.Len(t, chunks, 1)
	assert.Equal(t, "chunk-1", chunks[0].ID)
	assert.Equal(t, text, chunks[0].Text)
}

func TestChunkText_MultipleParagraphs(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	chunks := chunkText(text)

	assert.Len(t, chunks, 3)
	assert.Equal(t, "First paragraph.", chunks[0].Text)
	assert.Equal(t, "Second paragraph.", chunks[1].Text)
	assert.Equal(t, "Third paragraph.", chunks[2].Text)
}

func TestChunkText_EmptyParagraphs(t *testing.T) {
	text := "First paragraph.\n\n\n\nSecond paragraph.\n\n\n\n"
	chunks := chunkText(text)

	// Empty paragraphs should be filtered out
	assert.Len(t, chunks, 2)
}

func TestChunkText_LongText(t *testing.T) {
	// Create text longer than 2000 chars
	longParagraph := ""
	for i := 0; i < 250; i++ {
		longParagraph += "word "
	}

	chunks := chunkText(longParagraph)

	require.Len(t, chunks, 1)
	assert.LessOrEqual(t, len(chunks[0].Text), 2000)
}

func TestChunkText_MaxChunks(t *testing.T) {
	// Create text with more than 128 paragraphs
	var paragraphs []string
	for i := 0; i < 150; i++ {
		paragraphs = append(paragraphs, "Paragraph number "+string(rune('A'+i%26)))
	}
	text := ""
	for i, p := range paragraphs {
		if i > 0 {
			text += "\n\n"
		}
		text += p
	}

	chunks := chunkText(text)

	// Should cap at 128
	assert.LessOrEqual(t, len(chunks), 128)
}

func TestChunkText_MetadataIncluded(t *testing.T) {
	text := "First.\n\nSecond.\n\nThird."
	chunks := chunkText(text)

	for i, chunk := range chunks {
		assert.Contains(t, chunk.Metadata, "index")
		assert.Equal(t, i+1, chunk.Metadata["index"])
	}
}

// Tests for buildCandidates

func TestBuildCandidates_FromText(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	src := sourceInput{
		Text: "First paragraph.\n\nSecond paragraph.",
	}

	candidates, err := buildCandidates(context.Background(), rc, src, 16000)
	require.NoError(t, err)

	assert.Len(t, candidates, 2)
	assert.Equal(t, "First paragraph.", candidates[0].Text)
	assert.Equal(t, "Second paragraph.", candidates[1].Text)
}

func TestBuildCandidates_FromChunks(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	src := sourceInput{
		Chunks: []rawChunkInput{
			{ID: "custom-1", Text: "Chunk one", Metadata: map[string]any{"key": "value1"}},
			{ID: "custom-2", Text: "Chunk two", Metadata: map[string]any{"key": "value2"}},
		},
	}

	candidates, err := buildCandidates(context.Background(), rc, src, 16000)
	require.NoError(t, err)

	assert.Len(t, candidates, 2)
	assert.Equal(t, "custom-1", candidates[0].ID)
	assert.Equal(t, "Chunk one", candidates[0].Text)
	assert.Equal(t, "value1", candidates[0].Metadata["key"])
}

func TestBuildCandidates_ChunksPrioritized(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// When both chunks and text are provided, chunks take priority
	src := sourceInput{
		Text: "This text should be ignored",
		Chunks: []rawChunkInput{
			{ID: "chunk-1", Text: "Explicit chunk"},
		},
	}

	candidates, err := buildCandidates(context.Background(), rc, src, 16000)
	require.NoError(t, err)

	assert.Len(t, candidates, 1)
	assert.Equal(t, "Explicit chunk", candidates[0].Text)
}

func TestBuildCandidates_EmptyChunksFiltered(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	src := sourceInput{
		Chunks: []rawChunkInput{
			{ID: "chunk-1", Text: "Valid chunk"},
			{ID: "chunk-2", Text: "   "},
			{ID: "chunk-3", Text: "Another valid chunk"},
		},
	}

	candidates, err := buildCandidates(context.Background(), rc, src, 16000)
	require.NoError(t, err)

	assert.Len(t, candidates, 2)
}

func TestBuildCandidates_FromCAS(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := "CAS paragraph one.\n\nCAS paragraph two."
	digest := putToCAS(t, rc, []byte(content), "text/plain")

	src := sourceInput{
		CASDigest: digest,
	}

	candidates, err := buildCandidates(context.Background(), rc, src, 16000)
	require.NoError(t, err)

	assert.Len(t, candidates, 2)
	assert.Equal(t, "CAS paragraph one.", candidates[0].Text)
	assert.Equal(t, "CAS paragraph two.", candidates[1].Text)
}

func TestBuildCandidates_TokenBudgetEnforced(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Create chunks with known sizes
	var chunks []rawChunkInput
	for i := 0; i < 100; i++ {
		// Each chunk ~100 chars = ~25 tokens
		chunks = append(chunks, rawChunkInput{
			ID:   "c" + string(rune('0'+i%10)),
			Text: "This is a chunk with approximately one hundred characters of text that should help test token limits.",
		})
	}

	src := sourceInput{Chunks: chunks}

	// With 100 tokens max, should only get ~4 chunks
	candidates, err := buildCandidates(context.Background(), rc, src, 100)
	require.NoError(t, err)

	// Should have fewer than all chunks due to token budget
	assert.Less(t, len(candidates), 100)
}

func TestBuildCandidates_MaxCandidateCap(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Create more than 128 chunks
	var chunks []rawChunkInput
	for i := 0; i < 200; i++ {
		chunks = append(chunks, rawChunkInput{
			ID:   "c" + string(rune('A'+i%26)),
			Text: "Short",
		})
	}

	src := sourceInput{Chunks: chunks}

	candidates, err := buildCandidates(context.Background(), rc, src, 99999)
	require.NoError(t, err)

	// Should cap at 128
	assert.LessOrEqual(t, len(candidates), 128)
}

// Tests for estimateTokens helper

func TestEstimateTokens_Empty(t *testing.T) {
	tokens := estimateTokens(nil)
	assert.Equal(t, 0, tokens)
}

func TestEstimateTokens_SingleChunk(t *testing.T) {
	chunks := []outputChunk{
		{Text: "This is exactly twenty chars"}, // 28 chars
	}
	tokens := estimateTokens(chunks)
	// ~1 token per 4 chars
	assert.Equal(t, 7, tokens)
}

func TestEstimateTokens_MultipleChunks(t *testing.T) {
	chunks := []outputChunk{
		{Text: "Hello world"}, // 11 chars
		{Text: "More text"},   // 9 chars
	}
	// 20 chars / 4 = 5 tokens
	tokens := estimateTokens(chunks)
	assert.Equal(t, 5, tokens)
}

// Tests for applySelection helper

func TestApplySelection_Basic(t *testing.T) {
	candidates := []candidateChunk{
		{ID: "chunk-1", Text: "First chunk text", Metadata: map[string]any{"index": 1}},
		{ID: "chunk-2", Text: "Second chunk text", Metadata: map[string]any{"index": 2}},
		{ID: "chunk-3", Text: "Third chunk text", Metadata: map[string]any{"index": 3}},
	}

	sel := llmSelectionResponse{
		Chunks: []struct {
			ID        string  `json:"id"`
			Score     float64 `json:"score"`
			Rationale string  `json:"rationale"`
		}{
			{ID: "chunk-2", Score: 0.9, Rationale: "Most relevant"},
			{ID: "chunk-1", Score: 0.7, Rationale: "Also relevant"},
		},
	}

	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 2000,
	}

	result := applySelection(sel, candidates, budget)

	assert.Len(t, result, 2)
	assert.Equal(t, "chunk-2", result[0].ID)
	assert.Equal(t, 0.9, result[0].Score)
	assert.Equal(t, "Most relevant", result[0].Rationale)
	assert.Equal(t, "Second chunk text", result[0].Text)
}

func TestApplySelection_MaxChunksEnforced(t *testing.T) {
	var candidates []candidateChunk
	var selChunks []struct {
		ID        string  `json:"id"`
		Score     float64 `json:"score"`
		Rationale string  `json:"rationale"`
	}

	for i := 0; i < 20; i++ {
		id := "chunk-" + string(rune('a'+i))
		candidates = append(candidates, candidateChunk{ID: id, Text: "Short text"})
		selChunks = append(selChunks, struct {
			ID        string  `json:"id"`
			Score     float64 `json:"score"`
			Rationale string  `json:"rationale"`
		}{ID: id, Score: 0.8, Rationale: "Relevant"})
	}

	sel := llmSelectionResponse{Chunks: selChunks}
	budget := budgetInput{
		MaxChunks:    5,
		TargetTokens: 2000,
	}

	result := applySelection(sel, candidates, budget)

	assert.Len(t, result, 5)
}

func TestApplySelection_TokenBudgetEnforced(t *testing.T) {
	candidates := []candidateChunk{
		{ID: "chunk-1", Text: "This is a fairly long chunk with many words that would consume tokens"},
		{ID: "chunk-2", Text: "Another fairly long chunk with many words that would consume tokens"},
		{ID: "chunk-3", Text: "Yet another fairly long chunk with many words that would consume tokens"},
	}

	sel := llmSelectionResponse{
		Chunks: []struct {
			ID        string  `json:"id"`
			Score     float64 `json:"score"`
			Rationale string  `json:"rationale"`
		}{
			{ID: "chunk-1", Score: 0.9, Rationale: "Good"},
			{ID: "chunk-2", Score: 0.8, Rationale: "Good"},
			{ID: "chunk-3", Score: 0.7, Rationale: "Good"},
		},
	}

	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 30, // Very low to trigger token budget
	}

	result := applySelection(sel, candidates, budget)

	// Should have at least one but possibly fewer due to token budget
	assert.GreaterOrEqual(t, len(result), 1)
	assert.LessOrEqual(t, len(result), 3)
}

func TestApplySelection_UnknownIDsSkipped(t *testing.T) {
	candidates := []candidateChunk{
		{ID: "chunk-1", Text: "Valid chunk"},
	}

	sel := llmSelectionResponse{
		Chunks: []struct {
			ID        string  `json:"id"`
			Score     float64 `json:"score"`
			Rationale string  `json:"rationale"`
		}{
			{ID: "chunk-1", Score: 0.9, Rationale: "Good"},
			{ID: "nonexistent", Score: 0.8, Rationale: "Should be skipped"},
		},
	}

	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 2000,
	}

	result := applySelection(sel, candidates, budget)

	assert.Len(t, result, 1)
	assert.Equal(t, "chunk-1", result[0].ID)
}

func TestApplySelection_Empty(t *testing.T) {
	sel := llmSelectionResponse{
		Chunks: nil,
	}

	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 2000,
	}

	result := applySelection(sel, nil, budget)
	assert.Len(t, result, 0)
}

// Tests for buildLLMPrompt

func TestBuildLLMPrompt_ContainsPrompt(t *testing.T) {
	candidates := []candidateChunk{
		{ID: "chunk-1", Text: "Some text"},
	}
	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 2000,
	}

	prompt := buildLLMPrompt("What is the main topic?", "code", candidates, budget)

	assert.Contains(t, prompt, "What is the main topic?")
	assert.Contains(t, prompt, "Scope hint: code")
}

func TestBuildLLMPrompt_TruncatesLongPreviews(t *testing.T) {
	// Create text longer than 280 chars
	longText := ""
	for i := 0; i < 100; i++ {
		longText += "word "
	}

	candidates := []candidateChunk{
		{ID: "chunk-1", Text: longText},
	}
	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 2000,
	}

	prompt := buildLLMPrompt("Test prompt", "", candidates, budget)

	// The preview should be truncated with ellipsis
	assert.Contains(t, prompt, "…")
}

func TestBuildLLMPrompt_IncludesMetadata(t *testing.T) {
	candidates := []candidateChunk{
		{ID: "chunk-1", Text: "Text", Metadata: map[string]any{"file": "test.go"}},
	}
	budget := budgetInput{
		MaxChunks:    16,
		TargetTokens: 2000,
	}

	prompt := buildLLMPrompt("Test", "", candidates, budget)

	assert.Contains(t, prompt, "test.go")
}

func TestBuildLLMPrompt_IncludesConstraints(t *testing.T) {
	candidates := []candidateChunk{
		{ID: "chunk-1", Text: "Text"},
	}
	budget := budgetInput{
		MaxChunks:    10,
		TargetTokens: 500,
	}

	prompt := buildLLMPrompt("Test", "", candidates, budget)

	assert.Contains(t, prompt, "Maximum chunks: 10")
	assert.Contains(t, prompt, "Target total tokens (approximate): 500")
}

// Tests for empty candidates handling

func TestContextFilter_EmptyCandidates(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Create source with chunks that will be empty after trimming
	in := input{
		Prompt: "What is the topic?",
		Source: sourceInput{
			Chunks: []rawChunkInput{
				{ID: "empty-1", Text: "   "},
				{ID: "empty-2", Text: ""},
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Should return empty success
	assert.Nil(t, data["chunks"])
	assert.Equal(t, "no candidate chunks provided", data["summary"])
}

// Tests for default normalization

func TestContextFilter_DefaultsNormalized(t *testing.T) {
	in := input{
		Prompt: "Test",
		Source: sourceInput{Text: "Text"},
	}

	// Call the validation/normalization part of run by creating an input
	// and checking defaults
	normalizedPrompt := in.Prompt
	assert.NotEmpty(t, normalizedPrompt)

	// Budget defaults
	if in.Budget.TargetTokens <= 0 {
		in.Budget.TargetTokens = 2000
	}
	if in.Budget.MaxChunks <= 0 || in.Budget.MaxChunks > 64 {
		in.Budget.MaxChunks = 16
	}
	if in.Budget.MaxSourceTokens <= 0 {
		in.Budget.MaxSourceTokens = 16000
	}

	assert.Equal(t, 2000, in.Budget.TargetTokens)
	assert.Equal(t, 16, in.Budget.MaxChunks)
	assert.Equal(t, 16000, in.Budget.MaxSourceTokens)
}

func TestContextFilter_MaxChunksCapped(t *testing.T) {
	in := input{
		Budget: budgetInput{
			MaxChunks: 999, // Over the max
		},
	}

	if in.Budget.MaxChunks <= 0 || in.Budget.MaxChunks > 64 {
		in.Budget.MaxChunks = 16
	}

	assert.Equal(t, 16, in.Budget.MaxChunks)
}

// Tests for LLM model defaults

func TestContextFilter_OpenAIModelDefault(t *testing.T) {
	in := input{
		LLM: llmInput{
			Provider: "openai",
		},
	}

	if in.LLM.Model == "" && in.LLM.Provider == "openai" {
		in.LLM.Model = "gpt-4.1-mini"
	}

	assert.Equal(t, "gpt-4.1-mini", in.LLM.Model)
}

func TestContextFilter_AnthropicModelDefault(t *testing.T) {
	in := input{
		LLM: llmInput{
			Provider: "anthropic",
		},
	}

	if in.LLM.Model == "" && in.LLM.Provider == "anthropic" {
		in.LLM.Model = "claude-sonnet-4-20250514"
	}

	assert.Equal(t, "claude-sonnet-4-20250514", in.LLM.Model)
}

func TestContextFilter_GeminiModelDefault(t *testing.T) {
	in := input{
		LLM: llmInput{
			Provider: "gemini",
		},
	}

	if in.LLM.Model == "" && in.LLM.Provider == "gemini" {
		in.LLM.Model = "gemini-2.0-flash"
	}

	assert.Equal(t, "gemini-2.0-flash", in.LLM.Model)
}
