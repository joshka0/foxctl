package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{"definition", "references", "symbols", "workspace_symbol", "hover", "diagnostics"}
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 6)
}

// Tests for input structure

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestOutput_WithDefinition(t *testing.T) {
	def := Definition{File: "main.py", Line: 10, Column: 5}
	out := output{
		Operation:  "definition",
		Definition: &def,
		Count:      1,
	}

	assert.NotNil(t, out.Definition)
	assert.Equal(t, "main.py", out.Definition.File)
}

func TestOutput_WithReferences(t *testing.T) {
	out := output{
		Operation: "references",
		References: []Reference{
			{File: "a.py", Line: 10, Column: 5},
			{File: "b.py", Line: 20, Column: 3},
		},
		Count: 2,
	}

	assert.Len(t, out.References, 2)
	assert.Equal(t, 2, out.Count)
}

func TestOutput_WithHover(t *testing.T) {
	out := output{
		Operation: "hover",
		Hover:     "def my_function() -> str:\n    '''Docstring'''",
		Count:     1,
	}

	assert.NotEmpty(t, out.Hover)
	assert.Contains(t, out.Hover, "Docstring")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Operation:  "workspace_symbol",
		File:       "full.py",
		Line:       100,
		Column:     50,
		Query:      "FullQuery",
		MaxResults: 200,
		Timeout:    120,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.File, decoded.File)
	assert.Equal(t, in.Line, decoded.Line)
	assert.Equal(t, in.Column, decoded.Column)
	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.MaxResults, decoded.MaxResults)
	assert.Equal(t, in.Timeout, decoded.Timeout)
}

func TestInput_DefinitionOperation(t *testing.T) {
	in := input{
		Operation: "definition",
		File:      "main.py",
		Line:      10,
		Column:    5,
	}

	assert.Equal(t, "definition", in.Operation)
	assert.NotEmpty(t, in.File)
	assert.Greater(t, in.Line, 0)
}

func TestInput_ReferencesOperation(t *testing.T) {
	in := input{
		Operation: "references",
		File:      "util.py",
		Line:      25,
		Column:    10,
	}

	assert.Equal(t, "references", in.Operation)
}

func TestInput_SymbolsOperation(t *testing.T) {
	in := input{
		Operation: "symbols",
		File:      "module.py",
	}

	assert.Equal(t, "symbols", in.Operation)
	assert.NotEmpty(t, in.File)
}

func TestInput_WorkspaceSymbolOperation(t *testing.T) {
	in := input{
		Operation: "workspace_symbol",
		Query:     "MyClass",
	}

	assert.Equal(t, "workspace_symbol", in.Operation)
	assert.NotEmpty(t, in.Query)
}

func TestInput_HoverOperation(t *testing.T) {
	in := input{
		Operation: "hover",
		File:      "api.py",
		Line:      15,
		Column:    8,
	}

	assert.Equal(t, "hover", in.Operation)
}

func TestInput_DiagnosticsOperation(t *testing.T) {
	in := input{
		Operation: "diagnostics",
		File:      "test.py",
	}

	assert.Equal(t, "diagnostics", in.Operation)
}

func TestInput_DefaultTimeout(t *testing.T) {
	// Default is 30 seconds, but struct has 0
	in := input{Operation: "definition"}
	assert.Zero(t, in.Timeout)
}

func TestInput_DefaultMaxResults(t *testing.T) {
	// Default is 50 in run(), but struct has 0
	in := input{Operation: "references"}
	assert.Zero(t, in.MaxResults)
}

func TestInput_DefaultColumn(t *testing.T) {
	// Default is 1 in run(), but struct has 0
	in := input{Operation: "definition", File: "a.py", Line: 10}
	assert.Zero(t, in.Column)
}
