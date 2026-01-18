package main

import (
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/stretchr/testify/assert"
)

// Tests for formatMemoryContent helper

func TestFormatMemoryContent_WithSummary(t *testing.T) {
	entry := storage.NamedEntry{
		Name:      "test-memory",
		Type:      "gotcha",
		Summary:   "Watch out for this edge case",
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "[Jan 2026]")
	assert.Contains(t, result, "[gotcha]")
	assert.Contains(t, result, "Watch out for this edge case")
}

func TestFormatMemoryContent_WithoutSummary(t *testing.T) {
	entry := storage.NamedEntry{
		Name:      "important-memory",
		Type:      "decision",
		CreatedAt: time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "[Dec 2025]")
	assert.Contains(t, result, "[decision]")
	assert.Contains(t, result, "important-memory")
}

func TestFormatMemoryContent_DefaultType(t *testing.T) {
	entry := storage.NamedEntry{
		Name:      "untyped",
		Summary:   "Content",
		CreatedAt: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "[note]") // Default type
}

// Tests for formatSessionContent helper

func TestFormatSessionContent_Basic(t *testing.T) {
	session := storage.Session{
		ID:        "sess-123",
		Summary:   "Implemented new feature",
		StartedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := formatSessionContent(session)

	assert.Contains(t, result, "[Jan 15, 2026]")
	assert.Contains(t, result, "[development]") // Default activity
	assert.Contains(t, result, "Implemented new feature")
}

func TestFormatSessionContent_WithAllFields(t *testing.T) {
	session := storage.Session{
		ID:           "sess-123",
		Summary:      "Fixed auth bug",
		StartedAt:    time.Date(2026, 2, 20, 14, 30, 0, 0, time.UTC),
		Accomplished: []string{"Fixed token refresh", "Added tests"},
		Decisions:    []string{"Use JWT", "Add rate limiting"},
		Gotchas:      []string{"Don't forget to invalidate cache"},
		KeyFiles:     []string{"auth.go", "token.go"},
		Tags:         []string{"bug-fix", "auth"},
	}

	result := formatSessionContent(session)

	assert.Contains(t, result, "[Feb 20, 2026]")
	assert.Contains(t, result, "[bug-fix]")
	assert.Contains(t, result, "Fixed auth bug")
	assert.Contains(t, result, "Accomplished:")
	assert.Contains(t, result, "Fixed token refresh")
	assert.Contains(t, result, "Decisions:")
	assert.Contains(t, result, "Use JWT")
	assert.Contains(t, result, "Gotchas:")
	assert.Contains(t, result, "invalidate cache")
	assert.Contains(t, result, "Files:")
	assert.Contains(t, result, "auth.go")
	assert.Contains(t, result, "Topics:")
	assert.Contains(t, result, "auth")
}

func TestFormatSessionContent_NoSummary(t *testing.T) {
	session := storage.Session{
		ID:        "sess-123",
		StartedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Tags:      []string{"debug"},
	}

	result := formatSessionContent(session)

	assert.Contains(t, result, "[debugging]")
	assert.NotContains(t, result, "\n\n") // No double newlines from empty summary
}

// Tests for inferActivityType helper

func TestInferActivityType_Debug(t *testing.T) {
	assert.Equal(t, "debugging", inferActivityType([]string{"debug"}))
	assert.Equal(t, "debugging", inferActivityType([]string{"debugging-session"}))
	assert.Equal(t, "debugging", inferActivityType([]string{"Debug"}))
}

func TestInferActivityType_BugFix(t *testing.T) {
	assert.Equal(t, "bug-fix", inferActivityType([]string{"fix"}))
	assert.Equal(t, "bug-fix", inferActivityType([]string{"bug"}))
	assert.Equal(t, "bug-fix", inferActivityType([]string{"bugfix"}))
}

func TestInferActivityType_Feature(t *testing.T) {
	assert.Equal(t, "feature", inferActivityType([]string{"feature"}))
	assert.Equal(t, "feature", inferActivityType([]string{"implement"}))
	assert.Equal(t, "feature", inferActivityType([]string{"new-feature"}))
}

func TestInferActivityType_Refactoring(t *testing.T) {
	assert.Equal(t, "refactoring", inferActivityType([]string{"refactor"}))
	assert.Equal(t, "refactoring", inferActivityType([]string{"refactoring"}))
}

func TestInferActivityType_Testing(t *testing.T) {
	assert.Equal(t, "testing", inferActivityType([]string{"test"}))
	assert.Equal(t, "testing", inferActivityType([]string{"testing"}))
}

func TestInferActivityType_Documentation(t *testing.T) {
	assert.Equal(t, "documentation", inferActivityType([]string{"doc"}))
	assert.Equal(t, "documentation", inferActivityType([]string{"docs"}))
	assert.Equal(t, "documentation", inferActivityType([]string{"documentation"}))
}

func TestInferActivityType_CodeReview(t *testing.T) {
	assert.Equal(t, "code-review", inferActivityType([]string{"review"}))
	assert.Equal(t, "code-review", inferActivityType([]string{"code-review"}))
}

func TestInferActivityType_Setup(t *testing.T) {
	assert.Equal(t, "setup", inferActivityType([]string{"setup"}))
	assert.Equal(t, "setup", inferActivityType([]string{"config"}))
	assert.Equal(t, "setup", inferActivityType([]string{"configuration"}))
}

func TestInferActivityType_Default(t *testing.T) {
	assert.Equal(t, "development", inferActivityType([]string{}))
	assert.Equal(t, "development", inferActivityType([]string{"random-tag"}))
	assert.Equal(t, "development", inferActivityType(nil))
}

func TestInferActivityType_FirstMatch(t *testing.T) {
	// Debug should match first even with other tags
	assert.Equal(t, "debugging", inferActivityType([]string{"debug", "feature"}))
}

// Tests for joinStrings helper

func TestJoinStrings_Empty(t *testing.T) {
	assert.Equal(t, "", joinStrings(nil, ", "))
	assert.Equal(t, "", joinStrings([]string{}, ", "))
}

func TestJoinStrings_Single(t *testing.T) {
	assert.Equal(t, "one", joinStrings([]string{"one"}, ", "))
}

func TestJoinStrings_Multiple(t *testing.T) {
	assert.Equal(t, "one, two, three", joinStrings([]string{"one", "two", "three"}, ", "))
}

func TestJoinStrings_DifferentSeparator(t *testing.T) {
	assert.Equal(t, "a; b; c", joinStrings([]string{"a", "b", "c"}, "; "))
	assert.Equal(t, "a\nb\nc", joinStrings([]string{"a", "b", "c"}, "\n"))
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Scope:     "memory",
		Name:      "test-memory",
		Workspace: "/test/workspace",
		DryRun:    true,
	}

	assert.Equal(t, "memory", in.Scope)
	assert.Equal(t, "test-memory", in.Name)
	assert.Equal(t, "/test/workspace", in.Workspace)
	assert.True(t, in.DryRun)
}

func TestInput_ValidScopes(t *testing.T) {
	validScopes := []string{"memory", "symbol", "session"}
	for _, scope := range validScopes {
		in := Input{Scope: scope}
		assert.NotEmpty(t, in.Scope)
	}
}

// Tests for Output structure

func TestOutput_Refreshed(t *testing.T) {
	output := Output{
		Scope:      "memory",
		Name:       "test-mem",
		Status:     "refreshed",
		Dimensions: 1024,
		DurationMs: 500,
		Message:    "Refreshed memory embedding (1024 dimensions)",
	}

	assert.Equal(t, "memory", output.Scope)
	assert.Equal(t, "refreshed", output.Status)
	assert.Equal(t, 1024, output.Dimensions)
}

func TestOutput_NotFound(t *testing.T) {
	output := Output{
		Scope:      "session",
		Name:       "sess-404",
		Status:     "not_found",
		Message:    "session not found: sess-404",
		DurationMs: 50,
	}

	assert.Equal(t, "not_found", output.Status)
	assert.Contains(t, output.Message, "not found")
}

func TestOutput_NoContent(t *testing.T) {
	output := Output{
		Scope:   "memory",
		Name:    "empty-mem",
		Status:  "no_content",
		Message: "memory has no content to embed",
	}

	assert.Equal(t, "no_content", output.Status)
}

func TestOutput_Error(t *testing.T) {
	output := Output{
		Scope:   "symbol",
		Name:    "sym-123",
		Status:  "error",
		Message: "embedding generation failed: API error",
	}

	assert.Equal(t, "error", output.Status)
	assert.Contains(t, output.Message, "failed")
}

func TestOutput_DryRun(t *testing.T) {
	output := Output{
		Scope:   "session",
		Name:    "sess-123",
		Status:  "dry_run",
		Message: "Would generate embedding for session (content length: 500)",
	}

	assert.Equal(t, "dry_run", output.Status)
	assert.Contains(t, output.Message, "Would generate")
}

func TestOutput_WithHint(t *testing.T) {
	output := Output{
		Status:  "error",
		Message: "No embedding API key set",
		Hint:    "Set VOYAGE_API_KEY (preferred) or GEMINI_API_KEY",
	}

	assert.Equal(t, "error", output.Status)
	assert.NotEmpty(t, output.Hint)
	assert.Contains(t, output.Hint, "VOYAGE_API_KEY")
}

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "embedding/refresh", command)
}

func TestGeminiConstants(t *testing.T) {
	assert.Equal(t, "gemini-embedding-001", geminiModel)
	assert.Contains(t, geminiBaseURL, "generativelanguage.googleapis.com")
}

// Tests for errNotFound

func TestErrNotFound(t *testing.T) {
	assert.NotNil(t, errNotFound)
	assert.Equal(t, "not found", errNotFound.Error())
}
