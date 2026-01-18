package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for detectMode helper

func TestDetectMode_ExplicitRipgrep(t *testing.T) {
	in := input{Mode: ModeRipgrep}
	result := detectMode(in)
	assert.Equal(t, ModeRipgrep, result)
}

func TestDetectMode_ExplicitAST(t *testing.T) {
	in := input{Mode: ModeASTGrep}
	result := detectMode(in)
	assert.Equal(t, ModeASTGrep, result)
}

func TestDetectMode_ExplicitLine(t *testing.T) {
	in := input{Mode: ModeLine}
	result := detectMode(in)
	assert.Equal(t, ModeLine, result)
}

func TestDetectMode_ASTPatternAutoDetect(t *testing.T) {
	in := input{ASTPattern: "func $NAME() { $$$BODY }"}
	result := detectMode(in)
	assert.Equal(t, ModeASTGrep, result)
}

func TestDetectMode_ASTRuleAutoDetect(t *testing.T) {
	in := input{ASTRule: "id: test\nlanguage: go\nrule:\n  pattern: func()"}
	result := detectMode(in)
	assert.Equal(t, ModeASTGrep, result)
}

func TestDetectMode_LineAutoDetect_Start(t *testing.T) {
	in := input{FilePath: "main.go", LineStart: 10}
	result := detectMode(in)
	assert.Equal(t, ModeLine, result)
}

func TestDetectMode_LineAutoDetect_End(t *testing.T) {
	in := input{FilePath: "main.go", LineEnd: 50}
	result := detectMode(in)
	assert.Equal(t, ModeLine, result)
}

func TestDetectMode_LineAutoDetect_Both(t *testing.T) {
	in := input{FilePath: "main.go", LineStart: 10, LineEnd: 50}
	result := detectMode(in)
	assert.Equal(t, ModeLine, result)
}

func TestDetectMode_DefaultRipgrep(t *testing.T) {
	in := input{Pattern: "func.*Test"}
	result := detectMode(in)
	assert.Equal(t, ModeRipgrep, result)
}

func TestDetectMode_EmptyInput(t *testing.T) {
	in := input{}
	result := detectMode(in)
	assert.Equal(t, ModeRipgrep, result)
}

// Tests for parseASTGrepOutput helper

func TestParseASTGrepOutput_Empty(t *testing.T) {
	matches, err := parseASTGrepOutput([]byte{}, "/workspace", 100)
	assert.NoError(t, err)
	assert.Empty(t, matches)
}

func TestParseASTGrepOutput_SingleMatch(t *testing.T) {
	output := `{"file":"/workspace/main.go","range":{"start":{"line":9},"end":{"line":12}},"text":"func main() {}"}`

	matches, err := parseASTGrepOutput([]byte(output), "/workspace", 100)
	assert.NoError(t, err)
	assert.Len(t, matches, 1)
	assert.Equal(t, "main.go", matches[0].File)
	assert.Equal(t, 10, matches[0].Line) // 0-indexed + 1
	assert.Equal(t, "func main() {}", matches[0].Text)
}

func TestParseASTGrepOutput_MultipleMatches(t *testing.T) {
	output := `{"file":"/workspace/main.go","range":{"start":{"line":9}},"text":"func main()"}
{"file":"/workspace/util.go","range":{"start":{"line":4}},"text":"func helper()"}`

	matches, err := parseASTGrepOutput([]byte(output), "/workspace", 100)
	assert.NoError(t, err)
	assert.Len(t, matches, 2)
	assert.Equal(t, "main.go", matches[0].File)
	assert.Equal(t, "util.go", matches[1].File)
}

func TestParseASTGrepOutput_MaxMatches(t *testing.T) {
	output := `{"file":"/workspace/a.go","range":{"start":{"line":0}},"text":"a"}
{"file":"/workspace/b.go","range":{"start":{"line":0}},"text":"b"}
{"file":"/workspace/c.go","range":{"start":{"line":0}},"text":"c"}`

	matches, err := parseASTGrepOutput([]byte(output), "/workspace", 2)
	assert.NoError(t, err)
	assert.Len(t, matches, 2)
}

func TestParseASTGrepOutput_EmptyLines(t *testing.T) {
	output := `{"file":"/workspace/main.go","range":{"start":{"line":0}},"text":"a"}

{"file":"/workspace/util.go","range":{"start":{"line":0}},"text":"b"}`

	matches, err := parseASTGrepOutput([]byte(output), "/workspace", 100)
	assert.NoError(t, err)
	assert.Len(t, matches, 2)
}

func TestParseASTGrepOutput_InvalidJSON(t *testing.T) {
	output := `{"file":"/workspace/main.go","range":{"start":{"line":0}},"text":"a"}
not valid json
{"file":"/workspace/util.go","range":{"start":{"line":0}},"text":"b"}`

	matches, err := parseASTGrepOutput([]byte(output), "/workspace", 100)
	assert.NoError(t, err)
	assert.Len(t, matches, 2) // Invalid line is skipped
}

func TestParseASTGrepOutput_RelativePath(t *testing.T) {
	output := `{"file":"/home/user/project/src/main.go","range":{"start":{"line":0}},"text":"test"}`

	matches, err := parseASTGrepOutput([]byte(output), "/home/user/project", 100)
	assert.NoError(t, err)
	assert.Len(t, matches, 1)
	assert.Equal(t, "src/main.go", matches[0].File)
}

// Tests for mergeOverlappingBlocks helper

func TestMergeOverlappingBlocks_Empty(t *testing.T) {
	result := mergeOverlappingBlocks(nil)
	assert.Nil(t, result)
}

func TestMergeOverlappingBlocks_Single(t *testing.T) {
	blocks := []Block{{StartLine: 1, EndLine: 10}}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 10, result[0].EndLine)
}

func TestMergeOverlappingBlocks_NoOverlap(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 10},
		{StartLine: 20, EndLine: 30},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 2)
}

func TestMergeOverlappingBlocks_Overlapping(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 15},
		{StartLine: 10, EndLine: 20},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 20, result[0].EndLine)
}

func TestMergeOverlappingBlocks_Adjacent(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 10},
		{StartLine: 11, EndLine: 20},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 20, result[0].EndLine)
}

func TestMergeOverlappingBlocks_MultipleOverlaps(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 10},
		{StartLine: 8, EndLine: 15},
		{StartLine: 14, EndLine: 25},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 25, result[0].EndLine)
}

func TestMergeOverlappingBlocks_MixedOverlapAndSeparate(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 10},
		{StartLine: 8, EndLine: 15},
		{StartLine: 30, EndLine: 40},
		{StartLine: 38, EndLine: 50},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 2)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 15, result[0].EndLine)
	assert.Equal(t, 30, result[1].StartLine)
	assert.Equal(t, 50, result[1].EndLine)
}

func TestMergeOverlappingBlocks_UnsortedInput(t *testing.T) {
	blocks := []Block{
		{StartLine: 20, EndLine: 30},
		{StartLine: 1, EndLine: 10},
		{StartLine: 25, EndLine: 35},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 2)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 10, result[0].EndLine)
	assert.Equal(t, 20, result[1].StartLine)
	assert.Equal(t, 35, result[1].EndLine)
}

func TestMergeOverlappingBlocks_MergesMatchLines(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 10, MatchLines: []int{5}, MatchCount: 1},
		{StartLine: 8, EndLine: 20, MatchLines: []int{12}, MatchCount: 1},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, []int{5, 12}, result[0].MatchLines)
	assert.Equal(t, 2, result[0].MatchCount)
}

func TestMergeOverlappingBlocks_ContainedBlock(t *testing.T) {
	blocks := []Block{
		{StartLine: 1, EndLine: 50},
		{StartLine: 10, EndLine: 20},
	}
	result := mergeOverlappingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].StartLine)
	assert.Equal(t, 50, result[0].EndLine)
}

// Tests for mode constants

func TestModeConstants(t *testing.T) {
	assert.Equal(t, Mode("ripgrep"), ModeRipgrep)
	assert.Equal(t, Mode("ast"), ModeASTGrep)
	assert.Equal(t, Mode("line"), ModeLine)
}

// Tests for input defaults

func TestInput_DefaultMaxMatches(t *testing.T) {
	in := input{}
	// Default applied in run() when <= 0
	if in.MaxMatches <= 0 {
		in.MaxMatches = 10000
	}
	assert.Equal(t, 10000, in.MaxMatches)
}

func TestInput_DefaultMaxBlocks(t *testing.T) {
	in := input{}
	// Default applied in run() when <= 0
	if in.MaxBlocks <= 0 {
		in.MaxBlocks = 50
	}
	assert.Equal(t, 50, in.MaxBlocks)
}

func TestInput_DefaultMaxBlockLines(t *testing.T) {
	in := input{}
	// Default applied in run() when <= 0
	if in.MaxBlockLines <= 0 {
		in.MaxBlockLines = 400
	}
	assert.Equal(t, 400, in.MaxBlockLines)
}

func TestInput_PreservesCustomValues(t *testing.T) {
	in := input{
		MaxMatches:    100,
		MaxBlocks:     10,
		MaxBlockLines: 200,
	}

	// Only apply defaults if <= 0
	if in.MaxMatches <= 0 {
		in.MaxMatches = 10000
	}
	if in.MaxBlocks <= 0 {
		in.MaxBlocks = 50
	}
	if in.MaxBlockLines <= 0 {
		in.MaxBlockLines = 400
	}

	assert.Equal(t, 100, in.MaxMatches)
	assert.Equal(t, 10, in.MaxBlocks)
	assert.Equal(t, 200, in.MaxBlockLines)
}

// Tests for input structure

func TestInput_RipgrepOptions(t *testing.T) {
	in := input{
		Pattern:         "func.*Test",
		CaseInsensitive: true,
		Glob:            []string{"*.go"},
		GlobNot:         []string{"*_test.go"},
		Hidden:          true,
	}

	assert.Equal(t, "func.*Test", in.Pattern)
	assert.True(t, in.CaseInsensitive)
	assert.Equal(t, []string{"*.go"}, in.Glob)
	assert.Equal(t, []string{"*_test.go"}, in.GlobNot)
	assert.True(t, in.Hidden)
}

func TestInput_ASTGrepOptions(t *testing.T) {
	in := input{
		ASTPattern: "func $NAME() { $$$BODY }",
		Language:   "go",
		ASTRule:    "id: test\nlanguage: go",
	}

	assert.Equal(t, "func $NAME() { $$$BODY }", in.ASTPattern)
	assert.Equal(t, "go", in.Language)
	assert.Equal(t, "id: test\nlanguage: go", in.ASTRule)
}

func TestInput_LineExpansionOptions(t *testing.T) {
	in := input{
		FilePath:  "main.go",
		LineStart: 10,
		LineEnd:   50,
		ExpandTo:  "function",
	}

	assert.Equal(t, "main.go", in.FilePath)
	assert.Equal(t, 10, in.LineStart)
	assert.Equal(t, 50, in.LineEnd)
	assert.Equal(t, "function", in.ExpandTo)
}
