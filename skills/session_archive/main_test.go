package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/archive", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionID:    "sess-123",
		JSONLPath:    "/path/to/session.jsonl",
		Workspace:    "/workspace",
		Source:       "claude",
		MaxChunkSize: 4096,
		EmbedWindows: true,
		Force:        true,
		DryRun:       true,
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "/path/to/session.jsonl", in.JSONLPath)
	assert.Equal(t, "/workspace", in.Workspace)
	assert.Equal(t, "claude", in.Source)
	assert.Equal(t, 4096, in.MaxChunkSize)
	assert.True(t, in.EmbedWindows)
	assert.True(t, in.Force)
	assert.True(t, in.DryRun)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionID:    "sess-abc",
		JSONLPath:    "/test/session.jsonl",
		Source:       "codex",
		MaxChunkSize: 8192,
		EmbedWindows: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.JSONLPath, decoded.JSONLPath)
	assert.Equal(t, in.Source, decoded.Source)
	assert.Equal(t, in.MaxChunkSize, decoded.MaxChunkSize)
	assert.Equal(t, in.EmbedWindows, decoded.EmbedWindows)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.SessionID)
	assert.Empty(t, in.JSONLPath)
	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.Source)
	assert.Zero(t, in.MaxChunkSize)
	assert.False(t, in.EmbedWindows)
	assert.False(t, in.Force)
	assert.False(t, in.DryRun)
}

func TestInput_SourceValues(t *testing.T) {
	sources := []string{"claude", "codex"}

	for _, source := range sources {
		in := Input{Source: source}
		assert.Equal(t, source, in.Source)
	}
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		SessionID:       "sess-123",
		ArchivePath:     "/archives/sess-123.jsonl.gz",
		OriginalSize:    10000,
		CompressedSize:  2500,
		ChunkCount:      50,
		WindowCount:     5,
		EmbeddedWindows: 5,
		EmbeddingModel:  "text-embedding-qwen3-embedding-8b",
		Status:          "ok",
		Message:         "Archive complete",
	}

	assert.Equal(t, "sess-123", output.SessionID)
	assert.Equal(t, "/archives/sess-123.jsonl.gz", output.ArchivePath)
	assert.Equal(t, int64(10000), output.OriginalSize)
	assert.Equal(t, int64(2500), output.CompressedSize)
	assert.Equal(t, 50, output.ChunkCount)
	assert.Equal(t, 5, output.WindowCount)
	assert.Equal(t, 5, output.EmbeddedWindows)
	assert.Equal(t, "text-embedding-qwen3-embedding-8b", output.EmbeddingModel)
	assert.Equal(t, "ok", output.Status)
	assert.Equal(t, "Archive complete", output.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		SessionID:      "sess-test",
		ArchivePath:    "/test/archive.gz",
		OriginalSize:   5000,
		CompressedSize: 1000,
		ChunkCount:     25,
		WindowCount:    3,
		Status:         "ok",
		Message:        "Test complete",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.SessionID, decoded.SessionID)
	assert.Equal(t, output.ArchivePath, decoded.ArchivePath)
	assert.Equal(t, output.OriginalSize, decoded.OriginalSize)
	assert.Equal(t, output.CompressedSize, decoded.CompressedSize)
	assert.Equal(t, output.ChunkCount, decoded.ChunkCount)
	assert.Equal(t, output.WindowCount, decoded.WindowCount)
}

func TestOutput_EmptyFields(t *testing.T) {
	output := Output{}

	assert.Empty(t, output.SessionID)
	assert.Empty(t, output.ArchivePath)
	assert.Zero(t, output.OriginalSize)
	assert.Zero(t, output.CompressedSize)
	assert.Zero(t, output.ChunkCount)
	assert.Zero(t, output.WindowCount)
}

// Tests for source default logic

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

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		SessionID:    "sess-1",
		JSONLPath:    "/path",
		Workspace:    "/ws",
		Source:       "claude",
		MaxChunkSize: 4096,
		EmbedWindows: true,
		Force:        true,
		DryRun:       true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "session_id")
	assert.Contains(t, jsonStr, "jsonl_path")
	assert.Contains(t, jsonStr, "workspace")
	assert.Contains(t, jsonStr, "source")
	assert.Contains(t, jsonStr, "max_chunk_size")
	assert.Contains(t, jsonStr, "embed_windows")
	assert.Contains(t, jsonStr, "force")
	assert.Contains(t, jsonStr, "dry_run")
}
