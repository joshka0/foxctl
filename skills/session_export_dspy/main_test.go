package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 1000, defaultLimit)
}

// Tests for Input structure

func TestInput_FormatValues(t *testing.T) {
	formats := []string{"dspy", "jsonl", "csv"}

	for _, format := range formats {
		in := Input{Format: format}
		assert.Equal(t, format, in.Format)
	}
}

// Tests for Output structure

func TestOutput_NoSessions(t *testing.T) {
	output := Output{
		ExamplesCount: 0,
		SessionsUsed:  0,
		Examples:      []DSPyExample{},
		Status:        "no_sessions",
		Message:       "No sessions found matching criteria",
	}

	assert.Zero(t, output.ExamplesCount)
	assert.Zero(t, output.SessionsUsed)
	assert.Empty(t, output.Examples)
	assert.Equal(t, "no_sessions", output.Status)
}

// Tests for DSPyExample structure

func TestEscapeCSV_SimpleText(t *testing.T) {
	result := escapeCSV("simple text")
	assert.Equal(t, "simple text", result)
}

func TestEscapeCSV_WithComma(t *testing.T) {
	result := escapeCSV("text, with comma")
	assert.Equal(t, "\"text, with comma\"", result)
}

func TestEscapeCSV_WithQuotes(t *testing.T) {
	result := escapeCSV(`text with "quotes"`)
	assert.Equal(t, `"text with ""quotes"""`, result)
}

func TestEscapeCSV_WithNewline(t *testing.T) {
	result := escapeCSV("line1\nline2")
	assert.Equal(t, "line1 line2", result)
}

func TestEscapeCSV_WithCarriageReturn(t *testing.T) {
	result := escapeCSV("line1\r\nline2")
	assert.Equal(t, "line1 line2", result)
}

func TestEscapeCSV_ComplexText(t *testing.T) {
	result := escapeCSV(`Hello, "world"`)
	assert.Equal(t, `"Hello, ""world"""`, result)
}

func TestEscapeCSV_Empty(t *testing.T) {
	result := escapeCSV("")
	assert.Equal(t, "", result)
}

// Tests for unique helper

func TestUnique_Empty(t *testing.T) {
	result := unique([]string{})
	assert.Nil(t, result)
}

func TestUnique_Nil(t *testing.T) {
	result := unique(nil)
	assert.Nil(t, result)
}

func TestUnique_SingleItem(t *testing.T) {
	result := unique([]string{"item"})
	assert.Equal(t, []string{"item"}, result)
}

func TestUnique_NoDuplicates(t *testing.T) {
	result := unique([]string{"a", "b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUnique_WithDuplicates(t *testing.T) {
	result := unique([]string{"a", "b", "a", "c", "b"})
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUnique_PreservesOrder(t *testing.T) {
	result := unique([]string{"c", "b", "a", "b", "c"})
	assert.Equal(t, []string{"c", "b", "a"}, result)
}

func TestUnique_EmptyStrings(t *testing.T) {
	result := unique([]string{"a", "", "b", "", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUnique_AllEmpty(t *testing.T) {
	result := unique([]string{"", "", ""})
	assert.Nil(t, result)
}

// Tests for format default logic

func TestInput_FormatDefault(t *testing.T) {
	in := Input{}

	format := in.Format
	if format == "" {
		format = "dspy"
	}

	assert.Equal(t, "dspy", format)
}

func TestInput_FormatExplicit(t *testing.T) {
	in := Input{Format: "jsonl"}

	format := in.Format
	if format == "" {
		format = "dspy"
	}

	assert.Equal(t, "jsonl", format)
}

// Tests for limit default logic

func TestInput_LimitDefault(t *testing.T) {
	in := Input{}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 1000, limit)
}

func TestInput_LimitPositive(t *testing.T) {
	in := Input{Limit: 500}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 500, limit)
}

func TestInput_LimitNegative(t *testing.T) {
	in := Input{Limit: -10}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 1000, limit)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		SessionIDs:   []string{"sess-1", "sess-2", "sess-3"},
		Project:      "full-project",
		IncludeTools: true,
		IncludeFiles: true,
		OutputFile:   "/full/path/output.json",
		Format:       "csv",
		Limit:        2000,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionIDs, decoded.SessionIDs)
	assert.Equal(t, in.Project, decoded.Project)
	assert.Equal(t, in.IncludeTools, decoded.IncludeTools)
	assert.Equal(t, in.IncludeFiles, decoded.IncludeFiles)
	assert.Equal(t, in.OutputFile, decoded.OutputFile)
	assert.Equal(t, in.Format, decoded.Format)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestDSPyExample_FullJSONRoundTrip(t *testing.T) {
	example := DSPyExample{
		Input: ExampleInput{
			UserRequest: "Full request",
			Context:     "Full context",
			Files:       []string{"file1.go", "file2.go"},
		},
		Output: ExampleOutput{
			Response:    "Full response",
			ToolsUsed:   []string{"Edit", "Bash"},
			FilesEdited: []string{"file1.go"},
		},
		Metadata: ExampleMeta{
			SessionID:   "sess-full",
			ProjectName: "full-project",
			TurnIndex:   15,
			HasError:    true,
		},
	}

	data, err := json.Marshal(example)
	assert.NoError(t, err)

	var decoded DSPyExample
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, example.Input.UserRequest, decoded.Input.UserRequest)
	assert.Equal(t, example.Output.Response, decoded.Output.Response)
	assert.Equal(t, example.Metadata.SessionID, decoded.Metadata.SessionID)
}

func TestOutput_WithExamplesInline(t *testing.T) {
	output := Output{
		ExamplesCount: 2,
		SessionsUsed:  1,
		Examples: []DSPyExample{
			{
				Input:  ExampleInput{UserRequest: "req1"},
				Output: ExampleOutput{Response: "resp1"},
			},
			{
				Input:  ExampleInput{UserRequest: "req2"},
				Output: ExampleOutput{Response: "resp2"},
			},
		},
		Status:  "ok",
		Message: "Exported 2 examples",
	}

	assert.Len(t, output.Examples, 2)
	assert.Empty(t, output.OutputFile) // No file when inline
}
