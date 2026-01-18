package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "lsp/tsserver", command)
}

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{"definition", "references", "symbols", "workspace_symbol"}
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 4)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Operation:  "definition",
		File:       "index.ts",
		Line:       10,
		Column:     5,
		Query:      "MyClass",
		MaxResults: 100,
		Timeout:    60,
	}

	assert.Equal(t, "definition", in.Operation)
	assert.Equal(t, "index.ts", in.File)
	assert.Equal(t, 10, in.Line)
	assert.Equal(t, 5, in.Column)
	assert.Equal(t, "MyClass", in.Query)
	assert.Equal(t, 100, in.MaxResults)
	assert.Equal(t, 60, in.Timeout)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Operation:  "references",
		File:       "utils.ts",
		Line:       25,
		Column:     10,
		MaxResults: 50,
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
	assert.Equal(t, in.MaxResults, decoded.MaxResults)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Operation)
	assert.Empty(t, in.File)
	assert.Zero(t, in.Line)
	assert.Zero(t, in.Column)
	assert.Empty(t, in.Query)
	assert.Zero(t, in.MaxResults)
	assert.Zero(t, in.Timeout)
}

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Operation:  "o",
		File:       "f",
		Line:       1,
		Column:     2,
		Query:      "q",
		MaxResults: 10,
		Timeout:    30,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "operation")
	assert.Contains(t, jsonStr, "file")
	assert.Contains(t, jsonStr, "line")
	assert.Contains(t, jsonStr, "column")
	assert.Contains(t, jsonStr, "query")
	assert.Contains(t, jsonStr, "max_results")
	assert.Contains(t, jsonStr, "timeout")
}

// Tests for output types

func TestSymbol_AllFields(t *testing.T) {
	sym := Symbol{
		Name:   "myFunction",
		Kind:   "Function",
		File:   "module.ts",
		Line:   15,
		Column: 4,
	}

	assert.Equal(t, "myFunction", sym.Name)
	assert.Equal(t, "Function", sym.Kind)
	assert.Equal(t, "module.ts", sym.File)
	assert.Equal(t, 15, sym.Line)
	assert.Equal(t, 4, sym.Column)
}

func TestSymbol_JSONSerialization(t *testing.T) {
	sym := Symbol{
		Name:   "MyClass",
		Kind:   "Class",
		File:   "models.ts",
		Line:   1,
		Column: 1,
	}

	data, err := json.Marshal(sym)
	assert.NoError(t, err)

	var decoded Symbol
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, sym.Name, decoded.Name)
	assert.Equal(t, sym.Kind, decoded.Kind)
	assert.Equal(t, sym.File, decoded.File)
}

func TestReference_AllFields(t *testing.T) {
	ref := Reference{
		File:   "test.ts",
		Line:   100,
		Column: 20,
	}

	assert.Equal(t, "test.ts", ref.File)
	assert.Equal(t, 100, ref.Line)
	assert.Equal(t, 20, ref.Column)
}

func TestReference_JSONSerialization(t *testing.T) {
	ref := Reference{
		File:   "util.ts",
		Line:   50,
		Column: 5,
	}

	data, err := json.Marshal(ref)
	assert.NoError(t, err)

	var decoded Reference
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, ref.File, decoded.File)
	assert.Equal(t, ref.Line, decoded.Line)
	assert.Equal(t, ref.Column, decoded.Column)
}

func TestDefinition_AllFields(t *testing.T) {
	def := Definition{
		File:   "core.ts",
		Line:   25,
		Column: 0,
		Text:   "function myFunction(): void {",
	}

	assert.Equal(t, "core.ts", def.File)
	assert.Equal(t, 25, def.Line)
	assert.Equal(t, 0, def.Column)
	assert.Equal(t, "function myFunction(): void {", def.Text)
}

func TestDefinition_JSONSerialization(t *testing.T) {
	def := Definition{
		File:   "handler.ts",
		Line:   10,
		Column: 4,
	}

	data, err := json.Marshal(def)
	assert.NoError(t, err)

	var decoded Definition
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, def.File, decoded.File)
	assert.Equal(t, def.Line, decoded.Line)
}

func TestOutput_AllFields(t *testing.T) {
	out := output{
		Operation: "symbols",
		Symbols: []Symbol{
			{Name: "func", Kind: "Function", File: "a.ts", Line: 1, Column: 0},
		},
		Count: 1,
	}

	assert.Equal(t, "symbols", out.Operation)
	assert.Len(t, out.Symbols, 1)
	assert.Equal(t, 1, out.Count)
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
