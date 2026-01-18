package main

import (
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/sessions"
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

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionID:  "sess-123",
		Force:      true,
		MaxTokens:  4000,
		EpicID:     "epic-456",
		DryRun:     true,
		Mode:       "windows",
		BatchSize:  10,
		ProcessAll: true,
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.True(t, in.Force)
	assert.Equal(t, 4000, in.MaxTokens)
	assert.Equal(t, "epic-456", in.EpicID)
	assert.True(t, in.DryRun)
	assert.Equal(t, "windows", in.Mode)
	assert.Equal(t, 10, in.BatchSize)
	assert.True(t, in.ProcessAll)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionID: "sess-abc",
		Mode:      "session",
		MaxTokens: 8000,
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
	assert.Empty(t, in.EpicID)
	assert.False(t, in.DryRun)
	assert.Empty(t, in.Mode)
	assert.Zero(t, in.BatchSize)
	assert.False(t, in.ProcessAll)
}

func TestInput_ModeValues(t *testing.T) {
	modes := []string{"session", "windows"}

	for _, mode := range modes {
		in := Input{Mode: mode}
		assert.Equal(t, mode, in.Mode)
	}
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		SessionID:        "sess-123",
		EpicID:           "epic-456",
		Gotchas:          []Gotcha{{Summary: "test gotcha"}},
		Decisions:        []Decision{{Summary: "test decision"}},
		UserPreferences:  []Preference{{Summary: "test pref"}},
		AntiPatterns:     []AntiPattern{{Summary: "test anti"}},
		Learnings:        []Learning{{Summary: "test learning"}},
		PersistedCount:   5,
		Provider:         "anthropic",
		Status:           "ok",
		Message:          "Success",
		WindowsProcessed: 10,
		WindowsSkipped:   2,
		WindowsRemaining: 3,
	}

	assert.Equal(t, "sess-123", output.SessionID)
	assert.Equal(t, "epic-456", output.EpicID)
	assert.Len(t, output.Gotchas, 1)
	assert.Len(t, output.Decisions, 1)
	assert.Len(t, output.UserPreferences, 1)
	assert.Len(t, output.AntiPatterns, 1)
	assert.Len(t, output.Learnings, 1)
	assert.Equal(t, 5, output.PersistedCount)
	assert.Equal(t, "anthropic", output.Provider)
	assert.Equal(t, "ok", output.Status)
	assert.Equal(t, "Success", output.Message)
	assert.Equal(t, 10, output.WindowsProcessed)
	assert.Equal(t, 2, output.WindowsSkipped)
	assert.Equal(t, 3, output.WindowsRemaining)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		SessionID:      "sess-test",
		PersistedCount: 10,
		Status:         "ok",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.SessionID, decoded.SessionID)
	assert.Equal(t, output.PersistedCount, decoded.PersistedCount)
	assert.Equal(t, output.Status, decoded.Status)
}

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{"ok", "dry_run", "windows_extracted", "no_windows", "error"}

	for _, status := range statuses {
		output := Output{Status: status}
		assert.Equal(t, status, output.Status)
	}
}

// Tests for Gotcha structure

func TestGotcha_AllFields(t *testing.T) {
	gotcha := Gotcha{
		Summary:  "Connection timeout issue",
		Context:  "During API calls",
		Fix:      "Increase timeout to 30s",
		Tags:     []string{"network", "timeout"},
		Severity: "high",
		Files:    []string{"api/client.go"},
	}

	assert.Equal(t, "Connection timeout issue", gotcha.Summary)
	assert.Equal(t, "During API calls", gotcha.Context)
	assert.Equal(t, "Increase timeout to 30s", gotcha.Fix)
	assert.Len(t, gotcha.Tags, 2)
	assert.Equal(t, "high", gotcha.Severity)
	assert.Len(t, gotcha.Files, 1)
}

func TestGotcha_JSONSerialization(t *testing.T) {
	gotcha := Gotcha{
		Summary:  "Test gotcha",
		Severity: "medium",
	}

	data, err := json.Marshal(gotcha)
	assert.NoError(t, err)

	var decoded Gotcha
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, gotcha.Summary, decoded.Summary)
	assert.Equal(t, gotcha.Severity, decoded.Severity)
}

func TestGotcha_SeverityValues(t *testing.T) {
	severities := []string{"low", "medium", "high"}

	for _, severity := range severities {
		gotcha := Gotcha{Severity: severity}
		assert.Equal(t, severity, gotcha.Severity)
	}
}

// Tests for Decision structure

func TestDecision_AllFields(t *testing.T) {
	decision := Decision{
		Summary:      "Use REST over GraphQL",
		Reasoning:    "Simpler to implement and maintain",
		Alternatives: []string{"GraphQL", "gRPC"},
		Tags:         []string{"api", "architecture"},
		Files:        []string{"api/handlers.go"},
	}

	assert.Equal(t, "Use REST over GraphQL", decision.Summary)
	assert.Equal(t, "Simpler to implement and maintain", decision.Reasoning)
	assert.Len(t, decision.Alternatives, 2)
	assert.Len(t, decision.Tags, 2)
	assert.Len(t, decision.Files, 1)
}

func TestDecision_JSONSerialization(t *testing.T) {
	decision := Decision{
		Summary:   "Test decision",
		Reasoning: "Test reason",
	}

	data, err := json.Marshal(decision)
	assert.NoError(t, err)

	var decoded Decision
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, decision.Summary, decoded.Summary)
	assert.Equal(t, decision.Reasoning, decoded.Reasoning)
}

// Tests for Preference structure

func TestPreference_AllFields(t *testing.T) {
	pref := Preference{
		Summary:  "Prefer functional style",
		Context:  "When writing data transformations",
		Strength: "strong",
		Tags:     []string{"style", "functional"},
		Files:    []string{"utils/transform.go"},
	}

	assert.Equal(t, "Prefer functional style", pref.Summary)
	assert.Equal(t, "When writing data transformations", pref.Context)
	assert.Equal(t, "strong", pref.Strength)
	assert.Len(t, pref.Tags, 2)
	assert.Len(t, pref.Files, 1)
}

func TestPreference_JSONSerialization(t *testing.T) {
	pref := Preference{
		Summary:  "Test preference",
		Strength: "moderate",
	}

	data, err := json.Marshal(pref)
	assert.NoError(t, err)

	var decoded Preference
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, pref.Summary, decoded.Summary)
	assert.Equal(t, pref.Strength, decoded.Strength)
}

func TestPreference_StrengthValues(t *testing.T) {
	strengths := []string{"strong", "moderate", "weak"}

	for _, strength := range strengths {
		pref := Preference{Strength: strength}
		assert.Equal(t, strength, pref.Strength)
	}
}

// Tests for AntiPattern structure

func TestAntiPattern_AllFields(t *testing.T) {
	anti := AntiPattern{
		Summary:     "Storing secrets in code",
		WhyBad:      "Security vulnerability",
		Alternative: "Use environment variables",
		Tags:        []string{"security", "secrets"},
		Files:       []string{"config.go"},
	}

	assert.Equal(t, "Storing secrets in code", anti.Summary)
	assert.Equal(t, "Security vulnerability", anti.WhyBad)
	assert.Equal(t, "Use environment variables", anti.Alternative)
	assert.Len(t, anti.Tags, 2)
	assert.Len(t, anti.Files, 1)
}

func TestAntiPattern_JSONSerialization(t *testing.T) {
	anti := AntiPattern{
		Summary:     "Test anti-pattern",
		Alternative: "Better approach",
	}

	data, err := json.Marshal(anti)
	assert.NoError(t, err)

	var decoded AntiPattern
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, anti.Summary, decoded.Summary)
	assert.Equal(t, anti.Alternative, decoded.Alternative)
}

// Tests for Learning structure

func TestLearning_AllFields(t *testing.T) {
	learning := Learning{
		Summary:  "Use context for cancellation",
		Example:  "ctx, cancel := context.WithTimeout(ctx, 30*time.Second)",
		Reusable: true,
		Tags:     []string{"go", "context"},
		Files:    []string{"handlers/api.go"},
	}

	assert.Equal(t, "Use context for cancellation", learning.Summary)
	assert.Equal(t, "ctx, cancel := context.WithTimeout(ctx, 30*time.Second)", learning.Example)
	assert.True(t, learning.Reusable)
	assert.Len(t, learning.Tags, 2)
	assert.Len(t, learning.Files, 1)
}

func TestLearning_JSONSerialization(t *testing.T) {
	learning := Learning{
		Summary:  "Test learning",
		Reusable: true,
	}

	data, err := json.Marshal(learning)
	assert.NoError(t, err)

	var decoded Learning
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, learning.Summary, decoded.Summary)
	assert.Equal(t, learning.Reusable, decoded.Reusable)
}

// Tests for LLMResponse structure

func TestLLMResponse_AllFields(t *testing.T) {
	resp := LLMResponse{
		Gotchas:         []Gotcha{{Summary: "gotcha"}},
		Decisions:       []Decision{{Summary: "decision"}},
		UserPreferences: []Preference{{Summary: "pref"}},
		AntiPatterns:    []AntiPattern{{Summary: "anti"}},
		Learnings:       []Learning{{Summary: "learning"}},
	}

	assert.Len(t, resp.Gotchas, 1)
	assert.Len(t, resp.Decisions, 1)
	assert.Len(t, resp.UserPreferences, 1)
	assert.Len(t, resp.AntiPatterns, 1)
	assert.Len(t, resp.Learnings, 1)
}

func TestLLMResponse_JSONSerialization(t *testing.T) {
	resp := LLMResponse{
		Gotchas:   []Gotcha{{Summary: "test gotcha"}},
		Learnings: []Learning{{Summary: "test learning"}},
	}

	data, err := json.Marshal(resp)
	assert.NoError(t, err)

	var decoded LLMResponse
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Len(t, decoded.Gotchas, 1)
	assert.Len(t, decoded.Learnings, 1)
}

// Tests for learningsContentBudget helper

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

func TestFormatWindowMetadata_AllFields(t *testing.T) {
	window := sessions.ContextWindow{Trigger: "compact"}
	result := formatWindowMetadata(
		window,
		[]string{"Edit", "Bash"},
		[]string{"main.go"},
		[]string{"syntax error"},
	)
	assert.Contains(t, result, "Trigger: compact")
	assert.Contains(t, result, "Tools: Edit, Bash")
	assert.Contains(t, result, "Files: main.go")
	assert.Contains(t, result, "Errors: syntax error")
}

// Tests for selectLearningsChunks helper

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

func TestLearningsChunkCandidate_AllFields(t *testing.T) {
	candidate := learningsChunkCandidate{
		Chunk:    sessions.SessionChunk{ChunkIndex: 5},
		Preview:  "test preview",
		Tokens:   100,
		HasTools: true,
		HasFiles: true,
		IsError:  false,
	}

	assert.Equal(t, 5, candidate.Chunk.ChunkIndex)
	assert.Equal(t, "test preview", candidate.Preview)
	assert.Equal(t, 100, candidate.Tokens)
	assert.True(t, candidate.HasTools)
	assert.True(t, candidate.HasFiles)
	assert.False(t, candidate.IsError)
}

// Edge case tests

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

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		SessionID:  "sess-1",
		Force:      true,
		MaxTokens:  4000,
		EpicID:     "epic-1",
		DryRun:     true,
		Mode:       "session",
		BatchSize:  5,
		ProcessAll: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "session_id")
	assert.Contains(t, jsonStr, "force")
	assert.Contains(t, jsonStr, "max_tokens")
	assert.Contains(t, jsonStr, "epic_id")
	assert.Contains(t, jsonStr, "dry_run")
	assert.Contains(t, jsonStr, "mode")
	assert.Contains(t, jsonStr, "batch_size")
	assert.Contains(t, jsonStr, "process_all")
}
