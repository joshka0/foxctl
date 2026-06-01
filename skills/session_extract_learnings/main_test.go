package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommandName(t *testing.T) {
	assert.Equal(t, "session/extract_learnings", commandName)
}

func TestDefaultMaxTokens(t *testing.T) {
	assert.Equal(t, 8000, defaultMaxTokens)
}

func TestLearningsWindowConstants(t *testing.T) {
	assert.Equal(t, 1200, learningsWindowContentMin)
	assert.Equal(t, 6000, learningsWindowContentMax)
	assert.Equal(t, 1200, learningsWindowContentReserve)
	assert.Equal(t, 2, learningsWindowHeadChunks)
	assert.Equal(t, 2, learningsWindowTailChunks)
}

// Tests for Input structure

func TestInput_ModeValues(t *testing.T) {
	modes := []string{"session", "windows"}

	for _, mode := range modes {
		in := Input{Mode: mode}
		assert.Equal(t, mode, in.Mode)
	}
}

// Tests for Output structure

func TestGotcha_SeverityValues(t *testing.T) {
	severities := []string{"low", "medium", "high"}

	for _, severity := range severities {
		gotcha := Gotcha{Severity: severity}
		assert.Equal(t, severity, gotcha.Severity)
	}
}

// Tests for Decision structure

func TestPreference_StrengthValues(t *testing.T) {
	strengths := []string{"strong", "moderate", "weak"}

	for _, strength := range strengths {
		pref := Preference{Strength: strength}
		assert.Equal(t, strength, pref.Strength)
	}
}

// Tests for AntiPattern structure

func TestLearningsContentBudget_Default(t *testing.T) {
	// With 0 input, uses defaultMaxTokens (8000)
	// Budget = 8000 - 1200 (reserve) = 6800
	// But capped at max 6000
	budget := learningsContentBudget(0)
	assert.Equal(t, learningsWindowContentMax, budget)
}

func TestLearningsContentBudget_SmallInput(t *testing.T) {
	// With small input below min threshold
	// 2000 - 1200 (reserve) = 800, capped at min 1200
	budget := learningsContentBudget(2000)
	assert.Equal(t, learningsWindowContentMin, budget)
}

func TestLearningsContentBudget_MediumInput(t *testing.T) {
	// 4000 - 1200 (reserve) = 2800
	// Between min (1200) and max (6000)
	budget := learningsContentBudget(4000)
	assert.Equal(t, 2800, budget)
}

func TestLearningsContentBudget_LargeInput(t *testing.T) {
	// With large input exceeding max
	// 10000 - 1200 (reserve) = 8800, capped at max 6000
	budget := learningsContentBudget(10000)
	assert.Equal(t, learningsWindowContentMax, budget)
}

// Tests for sortedSet helper

func TestSortedSet_Empty(t *testing.T) {
	values := make(map[string]struct{})
	result := sortedSet(values, 10)
	assert.Nil(t, result)
}

func TestSortedSet_SingleItem(t *testing.T) {
	values := map[string]struct{}{
		"alpha": {},
	}
	result := sortedSet(values, 10)
	assert.Equal(t, []string{"alpha"}, result)
}

func TestSortedSet_MultipleItems(t *testing.T) {
	values := map[string]struct{}{
		"charlie": {},
		"alpha":   {},
		"bravo":   {},
	}
	result := sortedSet(values, 10)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, result)
}

func TestSortedSet_WithLimit(t *testing.T) {
	values := map[string]struct{}{
		"charlie": {},
		"alpha":   {},
		"bravo":   {},
		"delta":   {},
	}
	result := sortedSet(values, 2)
	assert.Len(t, result, 2)
	assert.Equal(t, []string{"alpha", "bravo"}, result)
}

func TestSortedSet_LimitZero(t *testing.T) {
	values := map[string]struct{}{
		"alpha": {},
		"bravo": {},
	}
	result := sortedSet(values, 0)
	assert.Len(t, result, 2) // Zero limit means no limit
}

// Tests for uniqueLimited helper

func TestUniqueLimited_Empty(t *testing.T) {
	result := uniqueLimited([]string{}, 10)
	assert.Empty(t, result)
}

func TestUniqueLimited_Nil(t *testing.T) {
	result := uniqueLimited(nil, 10)
	assert.Empty(t, result)
}

func TestUniqueLimited_SingleItem(t *testing.T) {
	result := uniqueLimited([]string{"item"}, 10)
	assert.Equal(t, []string{"item"}, result)
}

func TestUniqueLimited_Duplicates(t *testing.T) {
	result := uniqueLimited([]string{"a", "b", "a", "c", "b"}, 10)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUniqueLimited_WithLimit(t *testing.T) {
	result := uniqueLimited([]string{"a", "b", "c", "d"}, 2)
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestUniqueLimited_EmptyStrings(t *testing.T) {
	result := uniqueLimited([]string{"a", "", "b", "  ", "c"}, 10)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUniqueLimited_TrimWhitespace(t *testing.T) {
	result := uniqueLimited([]string{"  a  ", "b", "  a"}, 10)
	// Trimmed, so "  a  " and "  a" both become "a"
	assert.Equal(t, []string{"a", "b"}, result)
}

// Tests for formatWindowMetadata helper

func TestFormatWindowMetadata_Empty(t *testing.T) {
	window := sessions.ContextWindow{}
	result := formatWindowMetadata(window, nil, nil, nil)
	assert.Empty(t, result)
}

func TestFormatWindowMetadata_TriggerOnly(t *testing.T) {
	window := sessions.ContextWindow{Trigger: "user_request"}
	result := formatWindowMetadata(window, nil, nil, nil)
	assert.Equal(t, "Trigger: user_request", result)
}

func TestFormatWindowMetadata_ToolsOnly(t *testing.T) {
	window := sessions.ContextWindow{}
	result := formatWindowMetadata(window, []string{"Bash", "Read"}, nil, nil)
	assert.Equal(t, "Tools: Bash, Read", result)
}

func TestFormatWindowMetadata_FilesOnly(t *testing.T) {
	window := sessions.ContextWindow{}
	result := formatWindowMetadata(window, nil, []string{"main.go", "test.go"}, nil)
	assert.Equal(t, "Files: main.go, test.go", result)
}

func TestFormatWindowMetadata_ErrorsOnly(t *testing.T) {
	window := sessions.ContextWindow{}
	result := formatWindowMetadata(window, nil, nil, []string{"compile error", "test failed"})
	assert.Equal(t, "Errors: compile error | test failed", result)
}

func TestSelectLearningsChunks_Empty(t *testing.T) {
	result := selectLearningsChunks(nil, 1000)
	assert.Nil(t, result)
}

func TestSelectLearningsChunks_ZeroTokens(t *testing.T) {
	candidates := []learningsChunkCandidate{
		{Chunk: sessions.SessionChunk{ChunkIndex: 1}, Preview: "test", Tokens: 10},
	}
	result := selectLearningsChunks(candidates, 0)
	assert.Nil(t, result)
}

func TestSelectLearningsChunks_SingleChunk(t *testing.T) {
	candidates := []learningsChunkCandidate{
		{Chunk: sessions.SessionChunk{ChunkIndex: 1}, Preview: "test", Tokens: 10},
	}
	result := selectLearningsChunks(candidates, 100)
	assert.Len(t, result, 1)
}

func TestSelectLearningsChunks_ErrorPriority(t *testing.T) {
	candidates := []learningsChunkCandidate{
		{Chunk: sessions.SessionChunk{ChunkIndex: 1}, Preview: "normal", Tokens: 50, IsError: false},
		{Chunk: sessions.SessionChunk{ChunkIndex: 2}, Preview: "error!", Tokens: 50, IsError: true},
		{Chunk: sessions.SessionChunk{ChunkIndex: 3}, Preview: "normal2", Tokens: 50, IsError: false},
	}
	result := selectLearningsChunks(candidates, 60) // Only room for one
	assert.Len(t, result, 1)
	assert.Equal(t, 2, result[0].Chunk.ChunkIndex) // Error chunk prioritized
}

func TestSelectLearningsChunks_ToolAndFilePriority(t *testing.T) {
	candidates := []learningsChunkCandidate{
		{Chunk: sessions.SessionChunk{ChunkIndex: 1}, Preview: "no tools", Tokens: 10, HasTools: false, HasFiles: false},
		{Chunk: sessions.SessionChunk{ChunkIndex: 2}, Preview: "with tools", Tokens: 10, HasTools: true, HasFiles: false},
		{Chunk: sessions.SessionChunk{ChunkIndex: 3}, Preview: "with files", Tokens: 10, HasTools: false, HasFiles: true},
	}
	result := selectLearningsChunks(candidates, 1000)
	// All should be selected since budget is large
	assert.Len(t, result, 3)
}

func TestSelectLearningsChunks_BudgetLimit(t *testing.T) {
	candidates := []learningsChunkCandidate{
		{Chunk: sessions.SessionChunk{ChunkIndex: 1}, Preview: "chunk1", Tokens: 100},
		{Chunk: sessions.SessionChunk{ChunkIndex: 2}, Preview: "chunk2", Tokens: 100},
		{Chunk: sessions.SessionChunk{ChunkIndex: 3}, Preview: "chunk3", Tokens: 100},
	}
	result := selectLearningsChunks(candidates, 150) // Only room for 1
	assert.Len(t, result, 1)
}

// Tests for learningsChunkCandidate structure

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		SessionID:  "sess-full",
		Force:      true,
		MaxTokens:  16000,
		EpicID:     "epic-test",
		DryRun:     true,
		Mode:       "windows",
		BatchSize:  20,
		ProcessAll: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Force, decoded.Force)
	assert.Equal(t, in.MaxTokens, decoded.MaxTokens)
	assert.Equal(t, in.EpicID, decoded.EpicID)
	assert.Equal(t, in.DryRun, decoded.DryRun)
	assert.Equal(t, in.Mode, decoded.Mode)
	assert.Equal(t, in.BatchSize, decoded.BatchSize)
	assert.Equal(t, in.ProcessAll, decoded.ProcessAll)
}

func TestOutput_DryRunResult(t *testing.T) {
	output := Output{
		SessionID: "sess-dry",
		Status:    "dry_run",
		Message:   "dry run: skipped LLM extraction and persistence",
	}

	assert.Equal(t, "dry_run", output.Status)
	assert.Contains(t, output.Message, "dry run")
}

func TestOutput_WindowsModeResult(t *testing.T) {
	output := Output{
		SessionID:        "sess-windows",
		Status:           "windows_extracted",
		WindowsProcessed: 5,
		WindowsSkipped:   2,
		WindowsRemaining: 3,
		Message:          "extracted learnings from 5 windows",
	}

	assert.Equal(t, 5, output.WindowsProcessed)
	assert.Equal(t, 2, output.WindowsSkipped)
	assert.Equal(t, 3, output.WindowsRemaining)
}
