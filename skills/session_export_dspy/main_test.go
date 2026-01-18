package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/export-dspy", command)
}

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 1000, defaultLimit)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionIDs:   []string{"sess-1", "sess-2"},
		Project:      "my-project",
		IncludeTools: true,
		IncludeFiles: true,
		OutputFile:   "/path/to/output.json",
		Format:       "jsonl",
		Limit:        500,
	}

	assert.Len(t, in.SessionIDs, 2)
	assert.Equal(t, "my-project", in.Project)
	assert.True(t, in.IncludeTools)
	assert.True(t, in.IncludeFiles)
	assert.Equal(t, "/path/to/output.json", in.OutputFile)
	assert.Equal(t, "jsonl", in.Format)
	assert.Equal(t, 500, in.Limit)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionIDs:   []string{"sess-abc"},
		Format:       "dspy",
		IncludeTools: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionIDs, decoded.SessionIDs)
	assert.Equal(t, in.Format, decoded.Format)
	assert.Equal(t, in.IncludeTools, decoded.IncludeTools)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Nil(t, in.SessionIDs)
	assert.Empty(t, in.Project)
	assert.False(t, in.IncludeTools)
	assert.False(t, in.IncludeFiles)
	assert.Empty(t, in.OutputFile)
	assert.Empty(t, in.Format)
	assert.Zero(t, in.Limit)
}

func TestInput_FormatValues(t *testing.T) {
	formats := []string{"dspy", "jsonl", "csv"}

	for _, format := range formats {
		in := Input{Format: format}
		assert.Equal(t, format, in.Format)
	}
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		ExamplesCount: 10,
		SessionsUsed:  3,
		OutputFile:    "/path/to/output.json",
		Examples: []DSPyExample{
			{Input: ExampleInput{UserRequest: "test"}},
		},
		Status:  "ok",
		Message: "Exported 10 examples from 3 sessions",
	}

	assert.Equal(t, 10, output.ExamplesCount)
	assert.Equal(t, 3, output.SessionsUsed)
	assert.Equal(t, "/path/to/output.json", output.OutputFile)
	assert.Len(t, output.Examples, 1)
	assert.Equal(t, "ok", output.Status)
	assert.Equal(t, "Exported 10 examples from 3 sessions", output.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		ExamplesCount: 5,
		SessionsUsed:  2,
		Status:        "ok",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.ExamplesCount, decoded.ExamplesCount)
	assert.Equal(t, output.SessionsUsed, decoded.SessionsUsed)
	assert.Equal(t, output.Status, decoded.Status)
}

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{"ok", "no_sessions"}

	for _, status := range statuses {
		output := Output{Status: status}
		assert.Equal(t, status, output.Status)
	}
}

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

func TestDSPyExample_AllFields(t *testing.T) {
	example := DSPyExample{
		Input: ExampleInput{
			UserRequest: "Write a function to sort an array",
			Context:     "We need O(n log n) complexity",
			Files:       []string{"sort.go"},
		},
		Output: ExampleOutput{
			Response:    "Here's a merge sort implementation...",
			ToolsUsed:   []string{"Write", "Bash"},
			FilesEdited: []string{"sort.go"},
		},
		Metadata: ExampleMeta{
			SessionID:   "sess-123",
			ProjectName: "algorithms",
			TurnIndex:   5,
			HasError:    false,
		},
	}

	assert.Equal(t, "Write a function to sort an array", example.Input.UserRequest)
	assert.Equal(t, "Here's a merge sort implementation...", example.Output.Response)
	assert.Equal(t, "sess-123", example.Metadata.SessionID)
}

func TestDSPyExample_JSONSerialization(t *testing.T) {
	example := DSPyExample{
		Input: ExampleInput{
			UserRequest: "test request",
		},
		Output: ExampleOutput{
			Response: "test response",
		},
	}

	data, err := json.Marshal(example)
	assert.NoError(t, err)

	var decoded DSPyExample
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, example.Input.UserRequest, decoded.Input.UserRequest)
	assert.Equal(t, example.Output.Response, decoded.Output.Response)
}

// Tests for ExampleInput structure

func TestExampleInput_AllFields(t *testing.T) {
	input := ExampleInput{
		UserRequest: "Create a REST API",
		Context:     "Using Go and chi router",
		Files:       []string{"main.go", "handlers.go"},
	}

	assert.Equal(t, "Create a REST API", input.UserRequest)
	assert.Equal(t, "Using Go and chi router", input.Context)
	assert.Len(t, input.Files, 2)
}

func TestExampleInput_JSONSerialization(t *testing.T) {
	input := ExampleInput{
		UserRequest: "test",
		Files:       []string{"file.go"},
	}

	data, err := json.Marshal(input)
	assert.NoError(t, err)

	var decoded ExampleInput
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, input.UserRequest, decoded.UserRequest)
	assert.Equal(t, input.Files, decoded.Files)
}

// Tests for ExampleOutput structure

func TestExampleOutput_AllFields(t *testing.T) {
	output := ExampleOutput{
		Response:    "Here is the implementation...",
		ToolsUsed:   []string{"Write", "Bash", "Read"},
		FilesEdited: []string{"api.go", "routes.go"},
	}

	assert.Equal(t, "Here is the implementation...", output.Response)
	assert.Len(t, output.ToolsUsed, 3)
	assert.Len(t, output.FilesEdited, 2)
}

func TestExampleOutput_JSONSerialization(t *testing.T) {
	output := ExampleOutput{
		Response:  "response text",
		ToolsUsed: []string{"Edit"},
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded ExampleOutput
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.Response, decoded.Response)
	assert.Equal(t, output.ToolsUsed, decoded.ToolsUsed)
}

// Tests for ExampleMeta structure

func TestExampleMeta_AllFields(t *testing.T) {
	meta := ExampleMeta{
		SessionID:   "sess-abc",
		ProjectName: "my-project",
		TurnIndex:   10,
		HasError:    true,
	}

	assert.Equal(t, "sess-abc", meta.SessionID)
	assert.Equal(t, "my-project", meta.ProjectName)
	assert.Equal(t, 10, meta.TurnIndex)
	assert.True(t, meta.HasError)
}

func TestExampleMeta_JSONSerialization(t *testing.T) {
	meta := ExampleMeta{
		SessionID: "sess-test",
		TurnIndex: 5,
	}

	data, err := json.Marshal(meta)
	assert.NoError(t, err)

	var decoded ExampleMeta
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, meta.SessionID, decoded.SessionID)
	assert.Equal(t, meta.TurnIndex, decoded.TurnIndex)
}

// Tests for escapeCSV helper

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

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		SessionIDs:   []string{"s1"},
		Project:      "p",
		IncludeTools: true,
		IncludeFiles: true,
		OutputFile:   "out",
		Format:       "dspy",
		Limit:        10,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "session_ids")
	assert.Contains(t, jsonStr, "project")
	assert.Contains(t, jsonStr, "include_tools")
	assert.Contains(t, jsonStr, "include_files")
	assert.Contains(t, jsonStr, "output_file")
	assert.Contains(t, jsonStr, "format")
	assert.Contains(t, jsonStr, "limit")
}
