package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommandName(t *testing.T) {
	assert.Equal(t, "epic/complete", commandName)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		EpicID:        "epic-123",
		Force:         true,
		SkipLearnings: true,
		DryRun:        true,
	}

	assert.Equal(t, "epic-123", in.EpicID)
	assert.True(t, in.Force)
	assert.True(t, in.SkipLearnings)
	assert.True(t, in.DryRun)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		EpicID:        "epic-456",
		Force:         true,
		SkipLearnings: false,
		DryRun:        true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.EpicID, decoded.EpicID)
	assert.Equal(t, in.Force, decoded.Force)
	assert.Equal(t, in.SkipLearnings, decoded.SkipLearnings)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.EpicID)
	assert.False(t, in.Force)
	assert.False(t, in.SkipLearnings)
	assert.False(t, in.DryRun)
}

func TestInput_JSONOmitEmpty(t *testing.T) {
	in := Input{}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// All fields should be omitted when default values
	assert.NotContains(t, string(data), "epic_id")
	assert.NotContains(t, string(data), "force")
	assert.NotContains(t, string(data), "skip_learnings")
	assert.NotContains(t, string(data), "dry_run")
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		EpicID:         "epic-123",
		EpicTitle:      "Test Epic",
		EpicGoal:       "Complete the test",
		TasksTotal:     10,
		TasksCompleted: 8,
		TasksPending:   1,
		TasksInProg:    1,
		TasksBlocked:   0,
		FilesModified:  []string{"file1.go", "file2.go"},
		Gotchas:        []GotchaSummary{{TaskTitle: "Task 1", Gotcha: "Watch out!"}},
		Decisions:      []string{"Decision 1"},
		Learnings:      []string{"Learning 1"},
		Status:         "completed",
		Message:        "Epic completed successfully",
	}

	assert.Equal(t, "epic-123", out.EpicID)
	assert.Equal(t, "Test Epic", out.EpicTitle)
	assert.Equal(t, "Complete the test", out.EpicGoal)
	assert.Equal(t, 10, out.TasksTotal)
	assert.Equal(t, 8, out.TasksCompleted)
	assert.Equal(t, 1, out.TasksPending)
	assert.Equal(t, 1, out.TasksInProg)
	assert.Equal(t, 0, out.TasksBlocked)
	assert.Len(t, out.FilesModified, 2)
	assert.Len(t, out.Gotchas, 1)
	assert.Len(t, out.Decisions, 1)
	assert.Len(t, out.Learnings, 1)
	assert.Equal(t, "completed", out.Status)
	assert.Equal(t, "Epic completed successfully", out.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		EpicID:         "epic-test",
		EpicTitle:      "Test",
		TasksTotal:     5,
		TasksCompleted: 5,
		Status:         "completed",
		Message:        "Done",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.EpicID, decoded.EpicID)
	assert.Equal(t, out.EpicTitle, decoded.EpicTitle)
	assert.Equal(t, out.TasksTotal, decoded.TasksTotal)
	assert.Equal(t, out.TasksCompleted, decoded.TasksCompleted)
	assert.Equal(t, out.Status, decoded.Status)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.Empty(t, out.EpicID)
	assert.Empty(t, out.EpicTitle)
	assert.Empty(t, out.EpicGoal)
	assert.Zero(t, out.TasksTotal)
	assert.Zero(t, out.TasksCompleted)
	assert.Nil(t, out.FilesModified)
	assert.Nil(t, out.Gotchas)
	assert.Nil(t, out.Decisions)
	assert.Nil(t, out.Learnings)
	assert.Empty(t, out.Status)
}

func TestOutput_StatusValues(t *testing.T) {
	validStatuses := []string{"completed", "blocked", "error", "dry_run"}

	for _, status := range validStatuses {
		out := Output{Status: status}
		assert.Equal(t, status, out.Status)
	}
}

// Tests for GotchaSummary structure

func TestGotchaSummary_AllFields(t *testing.T) {
	g := GotchaSummary{
		TaskTitle: "Setup Database",
		Gotcha:    "Remember to run migrations first",
	}

	assert.Equal(t, "Setup Database", g.TaskTitle)
	assert.Equal(t, "Remember to run migrations first", g.Gotcha)
}

func TestGotchaSummary_JSONSerialization(t *testing.T) {
	g := GotchaSummary{
		TaskTitle: "Test Task",
		Gotcha:    "Test gotcha message",
	}

	data, err := json.Marshal(g)
	assert.NoError(t, err)

	var decoded GotchaSummary
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, g.TaskTitle, decoded.TaskTitle)
	assert.Equal(t, g.Gotcha, decoded.Gotcha)
}

func TestGotchaSummary_EmptyFields(t *testing.T) {
	g := GotchaSummary{}

	assert.Empty(t, g.TaskTitle)
	assert.Empty(t, g.Gotcha)
}

// Tests for ExtractedLearnings structure

func TestExtractedLearnings_AllFields(t *testing.T) {
	el := ExtractedLearnings{
		Learnings: []string{"Learning 1", "Learning 2"},
		Decisions: []string{"Decision 1"},
		Gotchas:   []string{"Gotcha 1", "Gotcha 2", "Gotcha 3"},
	}

	assert.Len(t, el.Learnings, 2)
	assert.Len(t, el.Decisions, 1)
	assert.Len(t, el.Gotchas, 3)
}

func TestExtractedLearnings_EmptyFields(t *testing.T) {
	el := ExtractedLearnings{}

	assert.Nil(t, el.Learnings)
	assert.Nil(t, el.Decisions)
	assert.Nil(t, el.Gotchas)
}

func TestExtractedLearnings_EmptySlices(t *testing.T) {
	el := ExtractedLearnings{
		Learnings: []string{},
		Decisions: []string{},
		Gotchas:   []string{},
	}

	assert.NotNil(t, el.Learnings)
	assert.NotNil(t, el.Decisions)
	assert.NotNil(t, el.Gotchas)
	assert.Len(t, el.Learnings, 0)
}

// Tests for sanitizeForName helper

func TestSanitizeForName_Basic(t *testing.T) {
	result := sanitizeForName("Hello World")
	assert.Equal(t, "hello-world", result)
}

func TestSanitizeForName_Lowercase(t *testing.T) {
	result := sanitizeForName("UPPERCASE")
	assert.Equal(t, "uppercase", result)
}

func TestSanitizeForName_MixedCase(t *testing.T) {
	result := sanitizeForName("MixedCase Test")
	assert.Equal(t, "mixedcase-test", result)
}

func TestSanitizeForName_WithNumbers(t *testing.T) {
	result := sanitizeForName("Task 123")
	assert.Equal(t, "task-123", result)
}

func TestSanitizeForName_SpecialChars(t *testing.T) {
	result := sanitizeForName("Task: Update (v1.0)")
	assert.Equal(t, "task-update-v10", result)
}

func TestSanitizeForName_MultipleSpaces(t *testing.T) {
	result := sanitizeForName("Task   with   spaces")
	// Multiple spaces become multiple hyphens
	assert.Equal(t, "task---with---spaces", result)
}

func TestSanitizeForName_LeadingTrailingSpaces(t *testing.T) {
	result := sanitizeForName("  Task Name  ")
	assert.Equal(t, "task-name", result)
}

func TestSanitizeForName_AlreadyHyphens(t *testing.T) {
	result := sanitizeForName("task-with-hyphens")
	assert.Equal(t, "task-with-hyphens", result)
}

func TestSanitizeForName_Empty(t *testing.T) {
	result := sanitizeForName("")
	assert.Equal(t, "", result)
}

func TestSanitizeForName_OnlySpecialChars(t *testing.T) {
	result := sanitizeForName("!@#$%^&*()")
	assert.Equal(t, "", result)
}

func TestSanitizeForName_LongString(t *testing.T) {
	// 50 character string
	long := "this-is-a-very-long-task-name-that-exceeds-forty-chars"
	result := sanitizeForName(long)
	assert.LessOrEqual(t, len(result), 40)
}

func TestSanitizeForName_Truncation(t *testing.T) {
	// Create a string that's exactly at truncation boundary
	input := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 46 chars
	result := sanitizeForName(input)
	assert.Equal(t, 40, len(result))
}

func TestSanitizeForName_Unicode(t *testing.T) {
	result := sanitizeForName("Task café résumé")
	// Non-ascii chars should be removed
	assert.Equal(t, "task-caf-rsum", result)
}

func TestSanitizeForName_LeadingHyphen(t *testing.T) {
	result := sanitizeForName("-leading")
	assert.Equal(t, "leading", result)
}

func TestSanitizeForName_TrailingHyphen(t *testing.T) {
	result := sanitizeForName("trailing-")
	assert.Equal(t, "trailing", result)
}

// Tests for matchesEpic helper

func TestMatchesEpic_EmptyEpicID(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "test summary",
	}
	result := matchesEpic(entry, "")
	assert.False(t, result)
}

func TestMatchesEpic_MatchInName(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "task-gotcha-epic:epic-123",
		Summary: "test summary",
	}
	result := matchesEpic(entry, "epic-123")
	assert.True(t, result)
}

func TestMatchesEpic_MatchInSummary(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "Found issue in epic:epic-456 work",
	}
	result := matchesEpic(entry, "epic-456")
	assert.True(t, result)
}

func TestMatchesEpic_MatchInPayload(t *testing.T) {
	payload := map[string]string{"epic_id": "epic-789"}
	payloadBytes, _ := json.Marshal(payload)

	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "test summary",
		Result:  payloadBytes,
	}
	result := matchesEpic(entry, "epic-789")
	assert.True(t, result)
}

func TestMatchesEpic_NoMatch(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "test summary",
	}
	result := matchesEpic(entry, "epic-999")
	assert.False(t, result)
}

func TestMatchesEpic_PartialMatch(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "epic:epic-12 work", // Should NOT match epic-123
	}
	result := matchesEpic(entry, "epic-123")
	assert.False(t, result)
}

func TestMatchesEpic_InvalidPayload(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "test summary",
		Result:  []byte("invalid json"),
	}
	// Should not crash, just return false
	result := matchesEpic(entry, "epic-123")
	assert.False(t, result)
}

func TestMatchesEpic_EmptyPayload(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "test summary",
		Result:  []byte{},
	}
	result := matchesEpic(entry, "epic-123")
	assert.False(t, result)
}

func TestMatchesEpic_PayloadWithDifferentEpicID(t *testing.T) {
	payload := map[string]string{"epic_id": "epic-different"}
	payloadBytes, _ := json.Marshal(payload)

	entry := storage.NamedEntry{
		Name:    "test-entry",
		Summary: "test summary",
		Result:  payloadBytes,
	}
	result := matchesEpic(entry, "epic-123")
	assert.False(t, result)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		EpicID:        "epic-full-test",
		Force:         true,
		SkipLearnings: true,
		DryRun:        true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.EpicID, decoded.EpicID)
	assert.Equal(t, in.Force, decoded.Force)
	assert.Equal(t, in.SkipLearnings, decoded.SkipLearnings)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		EpicID:         "epic-round-trip",
		EpicTitle:      "Test Epic",
		EpicGoal:       "Test goal",
		TasksTotal:     10,
		TasksCompleted: 7,
		TasksPending:   2,
		TasksInProg:    1,
		TasksBlocked:   0,
		FilesModified:  []string{"a.go", "b.go"},
		Gotchas:        []GotchaSummary{{TaskTitle: "T1", Gotcha: "G1"}},
		Decisions:      []string{"D1"},
		Learnings:      []string{"L1"},
		Status:         "completed",
		Message:        "Done",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.EpicID, decoded.EpicID)
	assert.Equal(t, out.EpicTitle, decoded.EpicTitle)
	assert.Equal(t, out.TasksTotal, decoded.TasksTotal)
	assert.Len(t, decoded.Gotchas, 1)
	assert.Len(t, decoded.Decisions, 1)
	assert.Len(t, decoded.Learnings, 1)
}

func TestOutput_LargeTaskCounts(t *testing.T) {
	out := Output{
		EpicID:         "epic-large",
		EpicTitle:      "Large Epic",
		TasksTotal:     1000,
		TasksCompleted: 800,
		TasksPending:   150,
		TasksInProg:    40,
		TasksBlocked:   10,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, 1000, decoded.TasksTotal)
	assert.Equal(t, 800, decoded.TasksCompleted)
}

func TestOutput_ManyGotchas(t *testing.T) {
	gotchas := make([]GotchaSummary, 50)
	for i := range gotchas {
		gotchas[i] = GotchaSummary{
			TaskTitle: "Task " + string(rune('A'+i%26)),
			Gotcha:    "Gotcha message",
		}
	}

	out := Output{
		EpicID:  "epic-many-gotchas",
		Gotchas: gotchas,
		Status:  "completed",
	}

	assert.Len(t, out.Gotchas, 50)
}

func TestGotchaSummary_SpecialCharacters(t *testing.T) {
	g := GotchaSummary{
		TaskTitle: "Task with \"quotes\" and <brackets>",
		Gotcha:    "Don't use `rm -rf` & be careful!",
	}

	data, err := json.Marshal(g)
	assert.NoError(t, err)

	var decoded GotchaSummary
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, g.TaskTitle, decoded.TaskTitle)
	assert.Equal(t, g.Gotcha, decoded.Gotcha)
}

func TestOutput_EmptySlices(t *testing.T) {
	out := Output{
		EpicID:        "epic-empty",
		EpicTitle:     "Empty",
		FilesModified: []string{},
		Gotchas:       []GotchaSummary{},
		Decisions:     []string{},
		Learnings:     []string{},
		Status:        "completed",
	}

	assert.NotNil(t, out.FilesModified)
	assert.NotNil(t, out.Gotchas)
	assert.NotNil(t, out.Decisions)
	assert.NotNil(t, out.Learnings)
	assert.Len(t, out.FilesModified, 0)
}
