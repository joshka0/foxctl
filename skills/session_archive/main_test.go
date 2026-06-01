package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_SourceValues(t *testing.T) {
	sources := []string{"claude", "codex"}

	for _, source := range sources {
		in := Input{Source: source}
		assert.Equal(t, source, in.Source)
	}
}

// Tests for Output structure

func TestInput_SourceDefault(t *testing.T) {
	in := Input{}

	source := in.Source
	if source == "" {
		source = "claude"
	}

	assert.Equal(t, "claude", source)
}

func TestInput_SourceExplicit(t *testing.T) {
	in := Input{Source: "codex"}

	source := in.Source
	if source == "" {
		source = "claude"
	}

	assert.Equal(t, "codex", source)
}

// Tests for filterEmbeddingContent helper
// Note: The filter only removes content if:
// 1. A noise pattern appears more than once OR the content is >500 chars
// 2. The alphabetic ratio is <40% for content >50 chars

func TestFilterEmbeddingContent_Empty(t *testing.T) {
	result := filterEmbeddingContent("")
	assert.Empty(t, result)
}

func TestFilterEmbeddingContent_SimpleText(t *testing.T) {
	text := "This is a simple description of a task"
	result := filterEmbeddingContent(text)
	assert.Equal(t, text, result)
}

func TestFilterEmbeddingContent_LongCodeWithMultiplePatterns(t *testing.T) {
	// Long content with multiple code patterns should be filtered
	// The filter requires pattern count > 1 OR len > 500 with a pattern
	text := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n" +
		"func helper() { fmt.Println(\"helper\") }\n" +
		"func another() { fmt.Println(\"another\") }"
	result := filterEmbeddingContent(text)
	assert.Empty(t, result, "Should filter out content with multiple func patterns")
}

func TestFilterEmbeddingContent_MultipleCodePatterns(t *testing.T) {
	// Multiple occurrences of pattern trigger filtering
	text := "func main() { } func helper() { }"
	result := filterEmbeddingContent(text)
	assert.Empty(t, result, "Should filter out content with multiple func patterns")
}

func TestFilterEmbeddingContent_ShortCodePassesThrough(t *testing.T) {
	// Short code with single pattern occurrence passes through
	text := "function handleClick() { console.log('clicked'); }"
	result := filterEmbeddingContent(text)
	// Short content with single occurrence may pass through
	assert.NotEmpty(t, result, "Short code with single pattern may pass through")
}

func TestFilterEmbeddingContent_JSON(t *testing.T) {
	// JSON pattern with multiple occurrences
	text := "{\"key\": \"value\"} and more {\"nested\": \"data\"}"
	result := filterEmbeddingContent(text)
	assert.Empty(t, result, "Should filter out content with multiple JSON patterns")
}

func TestFilterEmbeddingContent_CodeBlock(t *testing.T) {
	text := "Here is the code:\n```go\npackage main\n```"
	result := filterEmbeddingContent(text)
	assert.Empty(t, result, "Should filter out code blocks")
}

func TestFilterEmbeddingContent_LowAlphaRatio(t *testing.T) {
	text := "123456789012345678901234567890!@#$%^&*()[]{}|;':\",./<>?1234567890"
	result := filterEmbeddingContent(text)
	assert.Empty(t, result, "Should filter out content with low alphabetic ratio")
}

func TestFilterEmbeddingContent_HighAlphaRatio(t *testing.T) {
	text := "This is a sentence with mostly alphabetic content and some numbers like 42"
	result := filterEmbeddingContent(text)
	assert.NotEmpty(t, result, "Should keep content with high alphabetic ratio")
}

func TestFilterEmbeddingContent_TrimsWhitespace(t *testing.T) {
	text := "  Some text with whitespace  "
	result := filterEmbeddingContent(text)
	assert.Equal(t, "Some text with whitespace", result)
}

func TestFilterEmbeddingContent_MultipleImports(t *testing.T) {
	text := "import foo\nimport bar\nimport baz"
	result := filterEmbeddingContent(text)
	assert.Empty(t, result, "Should filter out multiple imports")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		SessionID:    "sess-full",
		JSONLPath:    "/full/path/session.jsonl",
		Workspace:    "/full/workspace",
		Source:       "claude",
		MaxChunkSize: 16384,
		EmbedWindows: true,
		Force:        true,
		DryRun:       false,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.JSONLPath, decoded.JSONLPath)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Source, decoded.Source)
	assert.Equal(t, in.MaxChunkSize, decoded.MaxChunkSize)
	assert.Equal(t, in.EmbedWindows, decoded.EmbedWindows)
	assert.Equal(t, in.Force, decoded.Force)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestOutput_DryRunOutput(t *testing.T) {
	output := Output{
		SessionID:    "sess-dry",
		OriginalSize: 5000,
		ChunkCount:   20,
		WindowCount:  2,
		Status:       "ok",
		Message:      "Dry run: would create 20 chunks",
	}

	assert.Contains(t, output.Message, "Dry run")
	assert.Zero(t, output.CompressedSize)
	assert.Empty(t, output.ArchivePath)
}

func TestOutput_LargeFile(t *testing.T) {
	output := Output{
		SessionID:      "sess-large",
		ArchivePath:    "/archives/large.gz",
		OriginalSize:   1000000000, // 1GB
		CompressedSize: 100000000,  // 100MB
		ChunkCount:     10000,
		WindowCount:    100,
	}

	assert.Equal(t, int64(1000000000), output.OriginalSize)
	assert.Equal(t, int64(100000000), output.CompressedSize)
	assert.Equal(t, 10000, output.ChunkCount)
}

func TestInput_SessionIDOnly(t *testing.T) {
	in := Input{
		SessionID: "sess-only",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "session_id")
	assert.Contains(t, string(data), "sess-only")
}
