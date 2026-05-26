package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{"definition", "references", "symbols", "workspace_symbol"}
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 4)
}

// Tests for input structure

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestOutput_WithDefinition(t *testing.T) {
	def := Definition{File: "main.ts", Line: 10, Column: 5}
	out := output{
		Operation:  "definition",
		Definition: &def,
		Count:      1,
	}

	assert.NotNil(t, out.Definition)
	assert.Equal(t, "main.ts", out.Definition.File)
}

func TestOutput_WithReferences(t *testing.T) {
	out := output{
		Operation: "references",
		References: []Reference{
			{File: "a.ts", Line: 10, Column: 5},
			{File: "b.ts", Line: 20, Column: 3},
		},
		Count: 2,
	}

	assert.Len(t, out.References, 2)
	assert.Equal(t, 2, out.Count)
}

// Tests for detectLanguage helper

func TestDetectLanguage_TypeScript(t *testing.T) {
	result := detectLanguage("file.ts")
	assert.Equal(t, "typescript", result)
}

func TestDetectLanguage_TypeScriptReact(t *testing.T) {
	result := detectLanguage("component.tsx")
	assert.Equal(t, "typescriptreact", result)
}

func TestDetectLanguage_JavaScript(t *testing.T) {
	result := detectLanguage("script.js")
	assert.Equal(t, "javascript", result)
}

func TestDetectLanguage_JavaScriptReact(t *testing.T) {
	result := detectLanguage("component.jsx")
	assert.Equal(t, "javascriptreact", result)
}

func TestDetectLanguage_Unknown(t *testing.T) {
	result := detectLanguage("file.unknown")
	assert.Equal(t, "typescript", result) // defaults to typescript
}

func TestDetectLanguage_NoExtension(t *testing.T) {
	result := detectLanguage("Makefile")
	assert.Equal(t, "typescript", result) // defaults to typescript
}

func TestDetectLanguage_UpperCase(t *testing.T) {
	result := detectLanguage("FILE.TS")
	assert.Equal(t, "typescript", result)
}

func TestDetectLanguage_MixedCase(t *testing.T) {
	result := detectLanguage("Component.TsX")
	assert.Equal(t, "typescriptreact", result)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Operation:  "workspace_symbol",
		File:       "full.ts",
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
		File:      "main.ts",
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
		File:      "util.ts",
		Line:      25,
		Column:    10,
	}

	assert.Equal(t, "references", in.Operation)
}

func TestInput_SymbolsOperation(t *testing.T) {
	in := input{
		Operation: "symbols",
		File:      "module.ts",
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

func TestInput_TypeScriptFileExtensions(t *testing.T) {
	extensions := []string{".ts", ".tsx", ".js", ".jsx"}

	for _, ext := range extensions {
		in := input{
			Operation: "symbols",
			File:      "file" + ext,
		}
		assert.Contains(t, in.File, ext)
	}
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
	in := input{Operation: "definition", File: "a.ts", Line: 10}
	assert.Zero(t, in.Column)
}
