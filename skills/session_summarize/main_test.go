package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/summarize", command)
}

func TestDefaultMaxTokens(t *testing.T) {
	assert.Equal(t, 2000, defaultMaxTokens)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionID:           "sess-123",
		Force:               true,
		MaxTokens:           4000,
		Mode:                "windows",
		Query:               "authentication flow",
		SeedMaxChars:        15000,
		SeedTopKWindows:     5,
		SeedChunksPerWindow: 10,
		BatchSize:           3,
		ProcessAll:          true,
		Queue:               true,
		QueueOnly:           false,
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.True(t, in.Force)
	assert.Equal(t, 4000, in.MaxTokens)
	assert.Equal(t, "windows", in.Mode)
	assert.Equal(t, "authentication flow", in.Query)
	assert.Equal(t, 15000, in.SeedMaxChars)
	assert.Equal(t, 5, in.SeedTopKWindows)
	assert.Equal(t, 10, in.SeedChunksPerWindow)
	assert.Equal(t, 3, in.BatchSize)
	assert.True(t, in.ProcessAll)
	assert.True(t, in.Queue)
	assert.False(t, in.QueueOnly)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionID: "sess-test",
		Mode:      "summary",
		MaxTokens: 2000,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Mode, decoded.Mode)
	assert.Equal(t, in.MaxTokens, decoded.MaxTokens)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.SessionID)
	assert.False(t, in.Force)
	assert.Zero(t, in.MaxTokens)
	assert.Empty(t, in.Mode)
	assert.Empty(t, in.Query)
}

func TestInput_ModeValues(t *testing.T) {
	modes := []string{"summary", "windows", "seed"}

	for _, mode := range modes {
		in := Input{Mode: mode}
		assert.Equal(t, mode, in.Mode)
	}
}

// Tests for SummarizeStats structure

func TestSummarizeStats_AllFields(t *testing.T) {
	stats := SummarizeStats{
		InputTokens:        1000,
		OutputTokens:       500,
		TotalTokens:        1500,
		InputCost:          0.001,
		OutputCost:         0.002,
		TotalCost:          0.003,
		Provider:           "cerebras",
		Model:              "llama-3.3-70b",
		Skipped:            true,
		SkipReason:         "content_hash_dedup",
		DedupFromSession:   "sess-456",
		LearningsPersisted: 5,
	}

	assert.Equal(t, 1000, stats.InputTokens)
	assert.Equal(t, 500, stats.OutputTokens)
	assert.Equal(t, 1500, stats.TotalTokens)
	assert.Equal(t, 0.001, stats.InputCost)
	assert.Equal(t, 0.002, stats.OutputCost)
	assert.Equal(t, 0.003, stats.TotalCost)
	assert.Equal(t, "cerebras", stats.Provider)
	assert.Equal(t, "llama-3.3-70b", stats.Model)
	assert.True(t, stats.Skipped)
	assert.Equal(t, "content_hash_dedup", stats.SkipReason)
	assert.Equal(t, "sess-456", stats.DedupFromSession)
	assert.Equal(t, 5, stats.LearningsPersisted)
}

func TestSummarizeStats_SkipReasons(t *testing.T) {
	reasons := []string{"content_hash_dedup", "already_summarized", "no_jsonl"}

	for _, reason := range reasons {
		stats := SummarizeStats{SkipReason: reason}
		assert.Equal(t, reason, stats.SkipReason)
	}
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		SessionID:          "sess-123",
		Summary:            "Test session summary",
		Accomplished:       []string{"task1", "task2"},
		Decisions:          []string{"decision1"},
		Gotchas:            []string{"gotcha1", "gotcha2"},
		UserInsights:       []string{"insight1"},
		UserPreferences:    []string{"pref1"},
		TimeSinks:          []string{"timesink1"},
		KeyQuestions:       []string{"question1"},
		Tags:               []string{"tag1", "tag2"},
		KeyFiles:           []string{"file1.go", "file2.go"},
		ToolsPattern:       "read-edit-test",
		HasEmbedding:       true,
		EmbeddingModel:     "voyage-3.5",
		EmbeddingDims:      1024,
		SeedPrompt:         "Seed prompt content",
		WindowsSummarized:  5,
		WindowsQueued:      3,
		WindowsSkipped:     2,
		WindowsRemaining:   1,
		SessionsReembedded: 10,
		SessionsSkipped:    5,
		WindowsReembedded:  20,
		Stats:              &SummarizeStats{Provider: "cerebras"},
		Status:             "completed",
		Message:            "Summarization complete",
	}

	assert.Equal(t, "sess-123", out.SessionID)
	assert.Equal(t, "Test session summary", out.Summary)
	assert.Len(t, out.Accomplished, 2)
	assert.Len(t, out.Decisions, 1)
	assert.Len(t, out.Gotchas, 2)
	assert.Len(t, out.UserInsights, 1)
	assert.Len(t, out.Tags, 2)
	assert.True(t, out.HasEmbedding)
	assert.Equal(t, "voyage-3.5", out.EmbeddingModel)
	assert.Equal(t, 1024, out.EmbeddingDims)
	assert.Equal(t, 5, out.WindowsSummarized)
	assert.NotNil(t, out.Stats)
	assert.Equal(t, "completed", out.Status)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		SessionID: "sess-test",
		Summary:   "Test summary",
		Status:    "completed",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.SessionID, decoded.SessionID)
	assert.Equal(t, out.Summary, decoded.Summary)
	assert.Equal(t, out.Status, decoded.Status)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.Empty(t, out.SessionID)
	assert.Empty(t, out.Summary)
	assert.Nil(t, out.Accomplished)
	assert.Nil(t, out.Stats)
	assert.Empty(t, out.Status)
}

// Tests for TokenUsage structure

func TestTokenUsage_AllFields(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  1000,
		OutputTokens: 500,
		TotalTokens:  1500,
	}

	assert.Equal(t, 1000, usage.InputTokens)
	assert.Equal(t, 500, usage.OutputTokens)
	assert.Equal(t, 1500, usage.TotalTokens)
}

// Tests for ProviderCost structure

func TestProviderCost_AllFields(t *testing.T) {
	cost := ProviderCost{
		InputPerMillion:  0.60,
		OutputPerMillion: 2.20,
	}

	assert.Equal(t, 0.60, cost.InputPerMillion)
	assert.Equal(t, 2.20, cost.OutputPerMillion)
}

// Tests for calculateCost helper

func TestCalculateCost_Cerebras(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  1000000,
		OutputTokens: 1000000,
	}

	inputCost, outputCost, totalCost := calculateCost("cerebras", usage)

	assert.InDelta(t, 0.60, inputCost, 0.001)
	assert.InDelta(t, 2.20, outputCost, 0.001)
	assert.InDelta(t, 2.80, totalCost, 0.001)
}

func TestCalculateCost_Groq(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  1000000,
		OutputTokens: 1000000,
	}

	inputCost, outputCost, totalCost := calculateCost("groq", usage)

	assert.Equal(t, 0.59, inputCost)
	assert.Equal(t, 0.79, outputCost)
	assert.InDelta(t, 1.38, totalCost, 0.001)
}

func TestCalculateCost_UnknownProvider(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  1000000,
		OutputTokens: 1000000,
	}

	inputCost, outputCost, totalCost := calculateCost("unknown_provider", usage)

	assert.Equal(t, 0.0, inputCost)
	assert.Equal(t, 0.0, outputCost)
	assert.Equal(t, 0.0, totalCost)
}

func TestCalculateCost_SmallTokenCount(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  1000,
		OutputTokens: 500,
	}

	inputCost, outputCost, totalCost := calculateCost("cerebras", usage)

	assert.InDelta(t, 0.0006, inputCost, 0.00001)
	assert.InDelta(t, 0.0011, outputCost, 0.00001)
	assert.InDelta(t, 0.0017, totalCost, 0.00001)
}

func TestCalculateCost_OpenRouterPrefix(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  1000000,
		OutputTokens: 1000000,
	}

	inputCost, outputCost, totalCost := calculateCost("openrouter:mistralai", usage)

	assert.Equal(t, 0.15, inputCost)
	assert.Equal(t, 0.60, outputCost)
	assert.InDelta(t, 0.75, totalCost, 0.001)
}

// Tests for SummaryResponse structure

func TestSummaryResponse_AllFields(t *testing.T) {
	resp := SummaryResponse{
		Summary:         "Session summary",
		Accomplished:    []string{"task1", "task2"},
		Decisions:       []string{"decision1"},
		Gotchas:         []string{"gotcha1"},
		UserInsights:    []string{"insight1"},
		UserPreferences: []string{"pref1"},
		TimeSinks:       []string{"timesink1"},
		Tags:            []string{"tag1", "tag2"},
		KeyFiles:        []string{"file1.go"},
		ToolsPattern:    "read-edit",
		KeyQuestions:    []string{"question1"},
	}

	assert.Equal(t, "Session summary", resp.Summary)
	assert.Len(t, resp.Accomplished, 2)
	assert.Len(t, resp.Decisions, 1)
	assert.Len(t, resp.Gotchas, 1)
	assert.Len(t, resp.Tags, 2)
	assert.Equal(t, "read-edit", resp.ToolsPattern)
}

func TestSummaryResponse_JSONSerialization(t *testing.T) {
	resp := SummaryResponse{
		Summary: "Test summary",
		Tags:    []string{"tag1"},
	}

	data, err := json.Marshal(resp)
	assert.NoError(t, err)

	var decoded SummaryResponse
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, resp.Summary, decoded.Summary)
	assert.Equal(t, resp.Tags, decoded.Tags)
}

// Tests for ClaudeMessage structure

func TestClaudeMessage_AllFields(t *testing.T) {
	msg := ClaudeMessage{
		Type:       "assistant",
		UUID:       "uuid-123",
		ParentUUID: "parent-uuid",
		SessionID:  "sess-456",
		Timestamp:  "2026-01-15T10:00:00Z",
		CWD:        "/path/to/project",
		GitBranch:  "main",
		Version:    "1.0.0",
		Summary:    "Message summary",
	}

	assert.Equal(t, "assistant", msg.Type)
	assert.Equal(t, "uuid-123", msg.UUID)
	assert.Equal(t, "parent-uuid", msg.ParentUUID)
	assert.Equal(t, "sess-456", msg.SessionID)
	assert.Equal(t, "/path/to/project", msg.CWD)
	assert.Equal(t, "main", msg.GitBranch)
}

// Tests for FilteredMessage structure

func TestFilteredMessage_AllFields(t *testing.T) {
	fm := FilteredMessage{
		Role:       "assistant",
		Content:    "This is the content",
		ToolsUsed:  []string{"Read", "Edit"},
		Error:      "Some error",
		Resolution: "Fixed the error",
	}

	assert.Equal(t, "assistant", fm.Role)
	assert.Equal(t, "This is the content", fm.Content)
	assert.Len(t, fm.ToolsUsed, 2)
	assert.Equal(t, "Some error", fm.Error)
	assert.Equal(t, "Fixed the error", fm.Resolution)
}

// Tests for normalizeLearning helper

func TestNormalizeLearning_Empty(t *testing.T) {
	result := normalizeLearning("")
	assert.Empty(t, result)
}

func TestNormalizeLearning_Whitespace(t *testing.T) {
	result := normalizeLearning("   ")
	assert.Empty(t, result)
}

func TestNormalizeLearning_SingleWord(t *testing.T) {
	result := normalizeLearning("  hello  ")
	assert.Equal(t, "hello", result)
}

func TestNormalizeLearning_MultipleWords(t *testing.T) {
	result := normalizeLearning("  hello   world  ")
	assert.Equal(t, "hello world", result)
}

func TestNormalizeLearning_MultipleSpaces(t *testing.T) {
	result := normalizeLearning("a     b     c")
	assert.Equal(t, "a b c", result)
}

func TestNormalizeLearning_WithNewlines(t *testing.T) {
	result := normalizeLearning("hello\n\nworld")
	assert.Equal(t, "hello world", result)
}

func TestNormalizeLearning_WithTabs(t *testing.T) {
	result := normalizeLearning("hello\t\tworld")
	assert.Equal(t, "hello world", result)
}

// Tests for cleanJSONResponse helper

func TestCleanJSONResponse_NoChanges(t *testing.T) {
	input := `{"key": "value"}`
	result := cleanJSONResponse(input)
	assert.Equal(t, input, result)
}

func TestCleanJSONResponse_SingleLineComment(t *testing.T) {
	input := `{"key": "value"} // this is a comment`
	result := cleanJSONResponse(input)
	assert.Equal(t, `{"key": "value"}`, result)
}

func TestCleanJSONResponse_MultiLineComment(t *testing.T) {
	input := `{"key": /* comment */ "value"}`
	result := cleanJSONResponse(input)
	assert.Equal(t, `{"key":  "value"}`, result)
}

func TestCleanJSONResponse_TrailingComma(t *testing.T) {
	input := `{"key": "value",}`
	result := cleanJSONResponse(input)
	assert.Equal(t, `{"key": "value"}`, result)
}

func TestCleanJSONResponse_TrailingCommaArray(t *testing.T) {
	input := `["a", "b",]`
	result := cleanJSONResponse(input)
	assert.Equal(t, `["a", "b"]`, result)
}

func TestCleanJSONResponse_CommentInString(t *testing.T) {
	input := `{"url": "https://example.com/path"}`
	result := cleanJSONResponse(input)
	assert.Equal(t, input, result)
}

// Tests for removeLineComment helper

func TestRemoveLineComment_NoComment(t *testing.T) {
	result := removeLineComment(`{"key": "value"}`)
	assert.Equal(t, `{"key": "value"}`, result)
}

func TestRemoveLineComment_WithComment(t *testing.T) {
	result := removeLineComment(`{"key": "value"} // comment`)
	assert.Equal(t, `{"key": "value"}`, result)
}

func TestRemoveLineComment_CommentInString(t *testing.T) {
	result := removeLineComment(`{"url": "https://example.com"}`)
	assert.Equal(t, `{"url": "https://example.com"}`, result)
}

func TestRemoveLineComment_EscapedQuote(t *testing.T) {
	result := removeLineComment(`{"text": "he said \"hello\""} // comment`)
	assert.Equal(t, `{"text": "he said \"hello\""}`, result)
}

func TestRemoveLineComment_OnlyComment(t *testing.T) {
	result := removeLineComment(`// just a comment`)
	assert.Equal(t, ``, result)
}

// Tests for removeTrailingCommas helper

func TestRemoveTrailingCommas_NoCommas(t *testing.T) {
	input := `{"key": "value"}`
	result := removeTrailingCommas(input)
	assert.Equal(t, input, result)
}

func TestRemoveTrailingCommas_ObjectTrailingComma(t *testing.T) {
	input := `{"key": "value",}`
	result := removeTrailingCommas(input)
	assert.Equal(t, `{"key": "value"}`, result)
}

func TestRemoveTrailingCommas_ArrayTrailingComma(t *testing.T) {
	input := `["a", "b",]`
	result := removeTrailingCommas(input)
	assert.Equal(t, `["a", "b"]`, result)
}

func TestRemoveTrailingCommas_NestedTrailingCommas(t *testing.T) {
	input := `{"arr": [1, 2,], "obj": {"a": 1,},}`
	result := removeTrailingCommas(input)
	assert.Equal(t, `{"arr": [1, 2], "obj": {"a": 1}}`, result)
}

func TestRemoveTrailingCommas_CommaInString(t *testing.T) {
	input := `{"text": "hello, world"}`
	result := removeTrailingCommas(input)
	assert.Equal(t, input, result)
}

// Tests for sampleChunkIndices helper

func TestSampleChunkIndices_InvalidRange(t *testing.T) {
	result := sampleChunkIndices(-1, 5, 3)
	assert.Nil(t, result)

	result = sampleChunkIndices(5, 3, 3)
	assert.Nil(t, result)
}

func TestSampleChunkIndices_AllFit(t *testing.T) {
	result := sampleChunkIndices(0, 4, 10)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, result)
}

func TestSampleChunkIndices_MaxZero(t *testing.T) {
	result := sampleChunkIndices(0, 4, 0)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, result)
}

func TestSampleChunkIndices_Sample(t *testing.T) {
	result := sampleChunkIndices(0, 9, 4)
	// Should take 2 from prefix (0, 1) and 2 from suffix (8, 9)
	// Suffix is added in order: end-(suffix-1), ..., end-0 = 8, 9
	assert.Equal(t, []int{0, 1, 8, 9}, result)
}

func TestSampleChunkIndices_OddMax(t *testing.T) {
	result := sampleChunkIndices(0, 9, 3)
	// prefix = 1, suffix = 2
	assert.Len(t, result, 3)
}

func TestSampleChunkIndices_SingleElement(t *testing.T) {
	result := sampleChunkIndices(5, 5, 10)
	assert.Equal(t, []int{5}, result)
}

// Tests for inferActivityType helper

func TestInferActivityType_Empty(t *testing.T) {
	result := inferActivityType([]string{})
	assert.Equal(t, "development", result)
}

func TestInferActivityType_Debug(t *testing.T) {
	result := inferActivityType([]string{"debugging", "session"})
	assert.Equal(t, "debugging", result)
}

func TestInferActivityType_BugFix(t *testing.T) {
	result := inferActivityType([]string{"bug-fix"})
	assert.Equal(t, "bug-fix", result)

	result = inferActivityType([]string{"fixing-issue"})
	assert.Equal(t, "bug-fix", result)
}

func TestInferActivityType_Feature(t *testing.T) {
	result := inferActivityType([]string{"new-feature"})
	assert.Equal(t, "feature", result)

	result = inferActivityType([]string{"implementing-auth"})
	assert.Equal(t, "feature", result)
}

func TestInferActivityType_Refactor(t *testing.T) {
	result := inferActivityType([]string{"refactoring"})
	assert.Equal(t, "refactoring", result)
}

func TestInferActivityType_Testing(t *testing.T) {
	result := inferActivityType([]string{"unit-testing"})
	assert.Equal(t, "testing", result)
}

func TestInferActivityType_Documentation(t *testing.T) {
	result := inferActivityType([]string{"documentation"})
	assert.Equal(t, "documentation", result)
}

func TestInferActivityType_CodeReview(t *testing.T) {
	result := inferActivityType([]string{"code-review"})
	assert.Equal(t, "code-review", result)
}

func TestInferActivityType_Setup(t *testing.T) {
	result := inferActivityType([]string{"initial-setup"})
	assert.Equal(t, "setup", result)

	result = inferActivityType([]string{"config-changes"})
	assert.Equal(t, "setup", result)
}

func TestInferActivityType_CaseInsensitive(t *testing.T) {
	result := inferActivityType([]string{"DEBUGGING"})
	assert.Equal(t, "debugging", result)
}

// Tests for isPlaceholderSummary helper

func TestIsPlaceholderSummary_Empty(t *testing.T) {
	assert.True(t, isPlaceholderSummary(""))
	assert.True(t, isPlaceholderSummary("   "))
}

func TestIsPlaceholderSummary_NA(t *testing.T) {
	assert.True(t, isPlaceholderSummary("n/a"))
	assert.True(t, isPlaceholderSummary("N/A"))
	assert.True(t, isPlaceholderSummary("na"))
}

func TestIsPlaceholderSummary_None(t *testing.T) {
	assert.True(t, isPlaceholderSummary("none"))
	assert.True(t, isPlaceholderSummary("None"))
}

func TestIsPlaceholderSummary_NotProvided(t *testing.T) {
	assert.True(t, isPlaceholderSummary("not provided"))
	assert.True(t, isPlaceholderSummary("not mentioned"))
}

func TestIsPlaceholderSummary_ValidSummary(t *testing.T) {
	assert.False(t, isPlaceholderSummary("This is a real summary"))
	assert.False(t, isPlaceholderSummary("Implemented authentication flow"))
}

func TestIsPlaceholderSummary_WithLabel(t *testing.T) {
	assert.True(t, isPlaceholderSummary("Summary: none"))
	assert.True(t, isPlaceholderSummary("Summary: n/a"))
}

// Tests for sortedUniqueInts helper

func TestSortedUniqueInts_Empty(t *testing.T) {
	result := sortedUniqueInts([]int{})
	assert.Nil(t, result)
}

func TestSortedUniqueInts_Single(t *testing.T) {
	result := sortedUniqueInts([]int{5})
	assert.Equal(t, []int{5}, result)
}

func TestSortedUniqueInts_Sorted(t *testing.T) {
	result := sortedUniqueInts([]int{3, 1, 4, 1, 5, 9, 2, 6})
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 9}, result)
}

func TestSortedUniqueInts_AllDuplicates(t *testing.T) {
	result := sortedUniqueInts([]int{5, 5, 5, 5})
	assert.Equal(t, []int{5}, result)
}

func TestSortedUniqueInts_AlreadySorted(t *testing.T) {
	result := sortedUniqueInts([]int{1, 2, 3, 4, 5})
	assert.Equal(t, []int{1, 2, 3, 4, 5}, result)
}

// Tests for formatChunkIndices helper

func TestFormatChunkIndices_Empty(t *testing.T) {
	result := formatChunkIndices([]int{}, 10)
	assert.Equal(t, "none", result)
}

func TestFormatChunkIndices_Single(t *testing.T) {
	result := formatChunkIndices([]int{5}, 10)
	assert.Equal(t, "5", result)
}

func TestFormatChunkIndices_Multiple(t *testing.T) {
	result := formatChunkIndices([]int{1, 2, 3}, 10)
	assert.Equal(t, "1, 2, 3", result)
}

func TestFormatChunkIndices_WithLimit(t *testing.T) {
	result := formatChunkIndices([]int{1, 2, 3, 4, 5}, 3)
	assert.Equal(t, "1, 2, 3 (+2 more)", result)
}

func TestFormatChunkIndices_ZeroLimit(t *testing.T) {
	result := formatChunkIndices([]int{1, 2, 3}, 0)
	assert.Equal(t, "1, 2, 3", result)
}

// Tests for sortedSet helper

func TestSortedSet_Empty(t *testing.T) {
	result := sortedSet(map[string]struct{}{}, 10)
	assert.Nil(t, result)
}

func TestSortedSet_Single(t *testing.T) {
	result := sortedSet(map[string]struct{}{"a": {}}, 10)
	assert.Equal(t, []string{"a"}, result)
}

func TestSortedSet_Multiple(t *testing.T) {
	input := map[string]struct{}{
		"c": {},
		"a": {},
		"b": {},
	}
	result := sortedSet(input, 10)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestSortedSet_WithLimit(t *testing.T) {
	input := map[string]struct{}{
		"d": {},
		"c": {},
		"b": {},
		"a": {},
	}
	result := sortedSet(input, 2)
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestSortedSet_ZeroLimit(t *testing.T) {
	input := map[string]struct{}{
		"b": {},
		"a": {},
	}
	result := sortedSet(input, 0)
	assert.Equal(t, []string{"a", "b"}, result)
}

// Tests for uniqueLimited helper

func TestUniqueLimited_Empty(t *testing.T) {
	result := uniqueLimited([]string{}, 10)
	assert.Empty(t, result)
}

func TestUniqueLimited_Single(t *testing.T) {
	result := uniqueLimited([]string{"hello"}, 10)
	assert.Equal(t, []string{"hello"}, result)
}

func TestUniqueLimited_WithDuplicates(t *testing.T) {
	result := uniqueLimited([]string{"a", "b", "a", "c", "b"}, 10)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUniqueLimited_WithLimit(t *testing.T) {
	result := uniqueLimited([]string{"a", "b", "c", "d", "e"}, 3)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUniqueLimited_TrimsWhitespace(t *testing.T) {
	result := uniqueLimited([]string{"  hello  ", "  world  "}, 10)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestUniqueLimited_SkipsEmpty(t *testing.T) {
	result := uniqueLimited([]string{"", "a", "  ", "b"}, 10)
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestUniqueLimited_ZeroLimit(t *testing.T) {
	result := uniqueLimited([]string{"a", "b", "c"}, 0)
	// Zero limit should not apply limit
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

// Edge case tests

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{
		"completed",
		"no_session",
		"error",
		"windows_queued",
		"no_windows",
		"reembed_complete",
	}

	for _, status := range statuses {
		out := Output{Status: status}
		assert.Equal(t, status, out.Status)
	}
}

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		SessionID:           "sess-full",
		Force:               true,
		MaxTokens:           5000,
		Mode:                "windows",
		Query:               "test query",
		SeedMaxChars:        20000,
		SeedTopKWindows:     10,
		SeedChunksPerWindow: 15,
		BatchSize:           5,
		ProcessAll:          true,
		Queue:               true,
		QueueOnly:           false,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Force, decoded.Force)
	assert.Equal(t, in.MaxTokens, decoded.MaxTokens)
	assert.Equal(t, in.Mode, decoded.Mode)
	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.SeedMaxChars, decoded.SeedMaxChars)
	assert.Equal(t, in.SeedTopKWindows, decoded.SeedTopKWindows)
	assert.Equal(t, in.SeedChunksPerWindow, decoded.SeedChunksPerWindow)
	assert.Equal(t, in.BatchSize, decoded.BatchSize)
	assert.Equal(t, in.ProcessAll, decoded.ProcessAll)
	assert.Equal(t, in.Queue, decoded.Queue)
	assert.Equal(t, in.QueueOnly, decoded.QueueOnly)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		SessionID:          "sess-full",
		Summary:            "Full summary",
		Accomplished:       []string{"a1", "a2"},
		Decisions:          []string{"d1"},
		Gotchas:            []string{"g1", "g2"},
		UserInsights:       []string{"i1"},
		UserPreferences:    []string{"p1"},
		TimeSinks:          []string{"t1"},
		KeyQuestions:       []string{"q1"},
		Tags:               []string{"tag1"},
		KeyFiles:           []string{"file1.go"},
		ToolsPattern:       "read-edit-test",
		HasEmbedding:       true,
		EmbeddingModel:     "voyage-3.5",
		EmbeddingDims:      1024,
		SeedPrompt:         "Seed prompt",
		WindowsSummarized:  10,
		WindowsQueued:      5,
		WindowsSkipped:     3,
		WindowsRemaining:   2,
		SessionsReembedded: 15,
		SessionsSkipped:    8,
		WindowsReembedded:  25,
		Stats: &SummarizeStats{
			Provider:    "cerebras",
			InputTokens: 1000,
		},
		Status:  "completed",
		Message: "Done",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.SessionID, decoded.SessionID)
	assert.Equal(t, out.Summary, decoded.Summary)
	assert.Equal(t, out.Accomplished, decoded.Accomplished)
	assert.Equal(t, out.HasEmbedding, decoded.HasEmbedding)
	assert.NotNil(t, decoded.Stats)
	assert.Equal(t, out.Stats.Provider, decoded.Stats.Provider)
}

func TestSummarizeStats_FullJSONRoundTrip(t *testing.T) {
	stats := SummarizeStats{
		InputTokens:        2000,
		OutputTokens:       1000,
		TotalTokens:        3000,
		InputCost:          0.01,
		OutputCost:         0.02,
		TotalCost:          0.03,
		Provider:           "groq",
		Model:              "llama-3.3-70b",
		Skipped:            false,
		SkipReason:         "",
		DedupFromSession:   "",
		LearningsPersisted: 10,
	}

	data, err := json.Marshal(stats)
	assert.NoError(t, err)

	var decoded SummarizeStats
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, stats.InputTokens, decoded.InputTokens)
	assert.Equal(t, stats.OutputTokens, decoded.OutputTokens)
	assert.Equal(t, stats.Provider, decoded.Provider)
	assert.Equal(t, stats.LearningsPersisted, decoded.LearningsPersisted)
}

func TestProviderCostsMap_HasExpectedProviders(t *testing.T) {
	expectedProviders := []string{
		"cerebras",
		"groq",
		"anthropic",
		"openai",
		"gemini",
	}

	for _, provider := range expectedProviders {
		_, ok := providerCosts[provider]
		assert.True(t, ok, "expected provider %s in providerCosts map", provider)
	}
}

func TestCalculateCost_ZeroTokens(t *testing.T) {
	usage := TokenUsage{
		InputTokens:  0,
		OutputTokens: 0,
	}

	inputCost, outputCost, totalCost := calculateCost("cerebras", usage)

	assert.Equal(t, 0.0, inputCost)
	assert.Equal(t, 0.0, outputCost)
	assert.Equal(t, 0.0, totalCost)
}
