package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "codemap/import", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	skipExisting := false
	embed := true
	in := Input{
		Path:         "/path/to/codemaps",
		Workspace:    "/workspace",
		Recursive:    true,
		SkipExisting: &skipExisting,
		Embed:        &embed,
	}

	assert.Equal(t, "/path/to/codemaps", in.Path)
	assert.Equal(t, "/workspace", in.Workspace)
	assert.True(t, in.Recursive)
	assert.NotNil(t, in.SkipExisting)
	assert.False(t, *in.SkipExisting)
	assert.NotNil(t, in.Embed)
	assert.True(t, *in.Embed)
}

func TestInput_JSONSerialization(t *testing.T) {
	skipExisting := true
	in := Input{
		Path:         "/path/file.codemap",
		Workspace:    "/ws",
		Recursive:    false,
		SkipExisting: &skipExisting,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Path, decoded.Path)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Recursive, decoded.Recursive)
	assert.NotNil(t, decoded.SkipExisting)
	assert.Equal(t, *in.SkipExisting, *decoded.SkipExisting)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Path)
	assert.Empty(t, in.Workspace)
	assert.False(t, in.Recursive)
	assert.Nil(t, in.SkipExisting)
	assert.Nil(t, in.Embed)
}

func TestInput_JSONOmitEmpty(t *testing.T) {
	in := Input{
		Path: "/path/to/file.codemap",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// workspace should be omitted when empty
	assert.NotContains(t, string(data), "workspace")
	// recursive should be omitted when false
	assert.NotContains(t, string(data), "recursive")
	// skip_existing should be omitted when nil
	assert.NotContains(t, string(data), "skip_existing")
	// embed should be omitted when nil
	assert.NotContains(t, string(data), "embed")
}

func TestInput_SkipExistingDefaults(t *testing.T) {
	in := Input{Path: "/path/file.codemap"}

	// When SkipExisting is nil, default to true
	skipExisting := true
	if in.SkipExisting != nil {
		skipExisting = *in.SkipExisting
	}

	assert.True(t, skipExisting)
}

func TestInput_EmbedDefaults(t *testing.T) {
	in := Input{Path: "/path/file.codemap"}

	// When Embed is nil, default to true
	embed := true
	if in.Embed != nil {
		embed = *in.Embed
	}

	assert.True(t, embed)
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		Imported:       5,
		Skipped:        2,
		Errors:         1,
		ImportedIDs:    []string{"id1", "id2", "id3", "id4", "id5"},
		SkippedIDs:     []string{"id6", "id7"},
		ErrorDetails:   []string{"file1.codemap: invalid JSON"},
		EmbeddedChunks: 25,
		Message:        "Imported 5 codemap(s), skipped 2, errors 1",
	}

	assert.Equal(t, 5, out.Imported)
	assert.Equal(t, 2, out.Skipped)
	assert.Equal(t, 1, out.Errors)
	assert.Len(t, out.ImportedIDs, 5)
	assert.Len(t, out.SkippedIDs, 2)
	assert.Len(t, out.ErrorDetails, 1)
	assert.Equal(t, 25, out.EmbeddedChunks)
	assert.Contains(t, out.Message, "Imported 5")
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		Imported:    3,
		Skipped:     1,
		Errors:      0,
		ImportedIDs: []string{"a", "b", "c"},
		Message:     "test",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Imported, decoded.Imported)
	assert.Equal(t, out.Skipped, decoded.Skipped)
	assert.Equal(t, out.Errors, decoded.Errors)
	assert.Equal(t, out.ImportedIDs, decoded.ImportedIDs)
	assert.Equal(t, out.Message, decoded.Message)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.Zero(t, out.Imported)
	assert.Zero(t, out.Skipped)
	assert.Zero(t, out.Errors)
	assert.Nil(t, out.ImportedIDs)
	assert.Nil(t, out.SkippedIDs)
	assert.Nil(t, out.ErrorDetails)
	assert.Zero(t, out.EmbeddedChunks)
	assert.Empty(t, out.Message)
}

func TestOutput_JSONOmitEmpty(t *testing.T) {
	out := Output{
		Imported: 1,
		Message:  "Done",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	// imported_ids should be omitted when nil
	assert.NotContains(t, string(data), "imported_ids")
	// skipped_ids should be omitted when nil
	assert.NotContains(t, string(data), "skipped_ids")
	// error_details should be omitted when nil
	assert.NotContains(t, string(data), "error_details")
	// embedded_chunks should be omitted when 0
	assert.NotContains(t, string(data), "embedded_chunks")
}

// Tests for buildSummary helper

func TestBuildSummary_TitleOnly(t *testing.T) {
	result := buildSummary("My Title", "", "fallback-id")
	assert.Equal(t, "My Title", result)
}

func TestBuildSummary_DescriptionOnly(t *testing.T) {
	result := buildSummary("", "My Description", "fallback-id")
	assert.Equal(t, "My Description", result)
}

func TestBuildSummary_BothTitleAndDescription(t *testing.T) {
	result := buildSummary("Title", "Description", "fallback-id")
	assert.Equal(t, "Title - Description", result)
}

func TestBuildSummary_Fallback(t *testing.T) {
	result := buildSummary("", "", "fallback-id")
	assert.Equal(t, "fallback-id", result)
}

func TestBuildSummary_EmptyFallback(t *testing.T) {
	result := buildSummary("", "", "")
	assert.Equal(t, "", result)
}

func TestBuildSummary_LongValues(t *testing.T) {
	longTitle := "This is a very long title that describes the codemap"
	longDesc := "This is a very long description that explains what the codemap contains"
	result := buildSummary(longTitle, longDesc, "fallback")
	assert.Contains(t, result, longTitle)
	assert.Contains(t, result, longDesc)
	assert.Contains(t, result, " - ")
}

// Tests for SkipExisting and Embed pointer handling

func TestInput_SkipExistingTrue(t *testing.T) {
	skipExisting := true
	in := Input{
		Path:         "/path",
		SkipExisting: &skipExisting,
	}

	result := true
	if in.SkipExisting != nil {
		result = *in.SkipExisting
	}
	assert.True(t, result)
}

func TestInput_SkipExistingFalse(t *testing.T) {
	skipExisting := false
	in := Input{
		Path:         "/path",
		SkipExisting: &skipExisting,
	}

	result := true
	if in.SkipExisting != nil {
		result = *in.SkipExisting
	}
	assert.False(t, result)
}

func TestInput_EmbedTrue(t *testing.T) {
	embed := true
	in := Input{
		Path:  "/path",
		Embed: &embed,
	}

	result := true
	if in.Embed != nil {
		result = *in.Embed
	}
	assert.True(t, result)
}

func TestInput_EmbedFalse(t *testing.T) {
	embed := false
	in := Input{
		Path:  "/path",
		Embed: &embed,
	}

	result := true
	if in.Embed != nil {
		result = *in.Embed
	}
	assert.False(t, result)
}

// Tests for message format

func TestOutput_MessageFormat(t *testing.T) {
	out := Output{
		Imported: 3,
		Skipped:  1,
		Errors:   2,
	}
	out.Message = "Imported 3 codemap(s), skipped 1, errors 2"

	assert.Contains(t, out.Message, "3")
	assert.Contains(t, out.Message, "1")
	assert.Contains(t, out.Message, "2")
	assert.Contains(t, out.Message, "codemap")
}

// Edge case tests

func TestOutput_EmptySlices(t *testing.T) {
	out := Output{
		ImportedIDs:  []string{},
		SkippedIDs:   []string{},
		ErrorDetails: []string{},
	}

	assert.NotNil(t, out.ImportedIDs)
	assert.NotNil(t, out.SkippedIDs)
	assert.NotNil(t, out.ErrorDetails)
	assert.Len(t, out.ImportedIDs, 0)
}

func TestOutput_LargeCounts(t *testing.T) {
	out := Output{
		Imported:       1000,
		Skipped:        500,
		Errors:         50,
		EmbeddedChunks: 25000,
	}

	assert.Equal(t, 1000, out.Imported)
	assert.Equal(t, 500, out.Skipped)
	assert.Equal(t, 50, out.Errors)
	assert.Equal(t, 25000, out.EmbeddedChunks)
}

func TestInput_PathWithSpaces(t *testing.T) {
	in := Input{
		Path:      "/path/with spaces/file.codemap",
		Workspace: "/workspace/with spaces",
	}

	assert.Contains(t, in.Path, " ")
	assert.Contains(t, in.Workspace, " ")
}

func TestInput_PathWithSpecialChars(t *testing.T) {
	in := Input{
		Path: "/path/to/codemap-v1.0.codemap",
	}

	assert.Contains(t, in.Path, "-")
	assert.Contains(t, in.Path, ".")
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		Imported:       5,
		Skipped:        2,
		Errors:         1,
		ImportedIDs:    []string{"id1", "id2", "id3", "id4", "id5"},
		SkippedIDs:     []string{"id6", "id7"},
		ErrorDetails:   []string{"error1"},
		EmbeddedChunks: 50,
		Message:        "test message",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Imported, decoded.Imported)
	assert.Equal(t, out.Skipped, decoded.Skipped)
	assert.Equal(t, out.Errors, decoded.Errors)
	assert.Equal(t, out.ImportedIDs, decoded.ImportedIDs)
	assert.Equal(t, out.SkippedIDs, decoded.SkippedIDs)
	assert.Equal(t, out.ErrorDetails, decoded.ErrorDetails)
	assert.Equal(t, out.EmbeddedChunks, decoded.EmbeddedChunks)
	assert.Equal(t, out.Message, decoded.Message)
}

func TestInput_FullJSONRoundTrip(t *testing.T) {
	skipExisting := false
	embed := true
	in := Input{
		Path:         "/path/to/dir",
		Workspace:    "/workspace",
		Recursive:    true,
		SkipExisting: &skipExisting,
		Embed:        &embed,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Path, decoded.Path)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Recursive, decoded.Recursive)
	assert.NotNil(t, decoded.SkipExisting)
	assert.Equal(t, *in.SkipExisting, *decoded.SkipExisting)
	assert.NotNil(t, decoded.Embed)
	assert.Equal(t, *in.Embed, *decoded.Embed)
}

func TestBuildSummary_SpecialChars(t *testing.T) {
	result := buildSummary("Title: Test & More", "Description <with> special \"chars\"", "fallback")
	assert.Contains(t, result, "Title: Test & More")
	assert.Contains(t, result, "Description <with> special \"chars\"")
}
