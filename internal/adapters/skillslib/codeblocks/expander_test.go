package codeblocks

import (
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestGoIndexFindTargets(t *testing.T) {
	inner := goCandidate{startLine: 10, endLine: 20, headerLine: 10, symbolName: "inner", symbolKind: "closure"}
	outer := goCandidate{startLine: 5, endLine: 25, headerLine: 5, symbolName: "outer", symbolKind: "function"}
	typeA := goCandidate{startLine: 0, endLine: 40, headerLine: 0, symbolName: "TypeA", symbolKind: "type"}
	sameClosure := goCandidate{startLine: 100, endLine: 110, headerLine: 100, symbolName: "sameClosure", symbolKind: "closure"}
	sameFunc := goCandidate{startLine: 100, endLine: 110, headerLine: 100, symbolName: "sameFunc", symbolKind: "function"}
	sameType := goCandidate{startLine: 100, endLine: 110, headerLine: 100, symbolName: "sameType", symbolKind: "type"}

	idx := &goIndex{
		closures: []goCandidate{inner, sameClosure},
		funcs:    []goCandidate{outer, sameFunc},
		types:    []goCandidate{typeA, sameType},
	}

	tests := []struct {
		name    string
		lineIdx int
		target  Target
		want    goCandidate
		ok      bool
	}{
		{
			name:    "target-any-prefers-innermost-closure",
			lineIdx: 12,
			target:  TargetAny,
			want:    inner,
			ok:      true,
		},
		{
			name:    "target-block-uses-any-selection",
			lineIdx: 12,
			target:  TargetBlock,
			want:    inner,
			ok:      true,
		},
		{
			name:    "target-function-picks-closure",
			lineIdx: 12,
			target:  TargetFunction,
			want:    inner,
			ok:      true,
		},
		{
			name:    "target-class-ignores-closure",
			lineIdx: 12,
			target:  TargetClass,
			want:    typeA,
			ok:      true,
		},
		{
			name:    "function-boundary-start",
			lineIdx: 5,
			target:  TargetFunction,
			want:    outer,
			ok:      true,
		},
		{
			name:    "closure-boundary-start",
			lineIdx: 10,
			target:  TargetFunction,
			want:    inner,
			ok:      true,
		},
		{
			name:    "closure-boundary-end",
			lineIdx: 20,
			target:  TargetFunction,
			want:    inner,
			ok:      true,
		},
		{
			name:    "type-boundary-end",
			lineIdx: 40,
			target:  TargetClass,
			want:    typeA,
			ok:      true,
		},
		{
			name:    "equal-span-order-any",
			lineIdx: 105,
			target:  TargetAny,
			want:    sameClosure,
			ok:      true,
		},
		{
			name:    "equal-span-order-function",
			lineIdx: 105,
			target:  TargetFunction,
			want:    sameClosure,
			ok:      true,
		},
		{
			name:    "equal-span-order-class",
			lineIdx: 105,
			target:  TargetClass,
			want:    sameType,
			ok:      true,
		},
		{
			name:    "no-match",
			lineIdx: 500,
			target:  TargetAny,
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := idx.find(tt.lineIdx, tt.target)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			assertCandidate(t, got, tt.want)
		})
	}
}

func TestGoFallbackTypeAliasDoesNotExpandToFollowingSymbol(t *testing.T) {
	code := `type UserID = string

func keep() string {
	return "keep"
}`
	lines := strings.Split(code, "\n")
	expander := NewExpander(LangGo, 20)

	blocks := expander.ExpandMatches("snippet.go", lines, []RawMatch{{
		File: "snippet.go",
		Line: 1,
		Text: "UserID",
	}})
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d: %+v", len(blocks), blocks)
	}

	block := blocks[0]
	if block.StartLine != 1 || block.EndLine != 1 {
		t.Fatalf("block range = %d-%d, want 1-1; source:\n%s", block.StartLine, block.EndLine, block.Source)
	}
	if strings.Contains(block.Source, "func keep") {
		t.Fatalf("type alias block included following function:\n%s", block.Source)
	}
}

func TestGoFallbackMultiLineTypeDeclarationStopsBeforeNextSymbol(t *testing.T) {
	code := `type Handler func(
	ctx context.Context,
) error
func keep() string {
	return "keep"
}`
	lines := strings.Split(code, "\n")
	expander := NewExpander(LangGo, 20)

	blocks := expander.ExpandMatches("snippet.go", lines, []RawMatch{{
		File: "snippet.go",
		Line: 2,
		Text: "context.Context",
	}})
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d: %+v", len(blocks), blocks)
	}

	block := blocks[0]
	if block.StartLine != 1 || block.EndLine != 3 {
		t.Fatalf("block range = %d-%d, want 1-3; source:\n%s", block.StartLine, block.EndLine, block.Source)
	}
	if strings.Contains(block.Source, "func keep") {
		t.Fatalf("type declaration block included following function:\n%s", block.Source)
	}
}

func TestExpandMatchesPropertyMatchLinesAreSortedUniqueAndCounted(t *testing.T) {
	lines := []string{
		"one needle",
		"two needle",
		"three needle",
		"four needle",
		"five needle",
	}
	expander := NewExpander(LangGeneric, 20)

	err := quick.Check(func(a, b, c uint8) bool {
		startA := int(a%5) + 1
		startB := int(b%5) + 1
		endA := startA + int(c%3)
		if endA > len(lines) {
			endA = len(lines)
		}

		matches := []RawMatch{
			{File: "notes.txt", Line: startB, Text: "needle"},
			{File: "notes.txt", Line: startA, EndLine: endA, Text: "needle"},
			{File: "notes.txt", Line: startA, Text: "needle"},
		}

		blocks := expander.ExpandMatches("notes.txt", lines, matches)
		if len(blocks) != 1 {
			t.Logf("blocks=%+v", blocks)
			return false
		}

		wantSet := map[int]struct{}{startB: {}, startA: {}}
		for line := startA; line <= endA; line++ {
			wantSet[line] = struct{}{}
		}
		want := make([]int, 0, len(wantSet))
		for line := range wantSet {
			want = append(want, line)
		}
		want = uniqueSortedInts(want)

		got := blocks[0].MatchLines
		return blocks[0].MatchCount == len(got) && reflect.DeepEqual(got, want)
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatalf("match-line invariant failed: %v", err)
	}
}

func FuzzExpandMatchesMaintainsBlockInvariants(f *testing.F) {
	seeds := []struct {
		source     string
		langSeed   uint8
		targetSeed uint8
		maxSeed    uint8
		lineA      uint16
		lineB      uint16
		lineC      uint16
	}{
		{source: "package main\n\nfunc main() {\n\tprintln(\"needle\")\n}\n", langSeed: 0, lineA: 4, lineB: 3, lineC: 1},
		{source: "class Greeter:\n    def hello(self):\n        return 'needle'\n", langSeed: 1, lineA: 3, lineB: 2, lineC: 1},
		{source: "export const fn = () => {\n  return 'needle'\n}\n", langSeed: 3, lineA: 2, lineB: 1, lineC: 3},
		{source: "alpha\n\nbeta needle\ngamma\n", langSeed: 8, targetSeed: 1, lineA: 3, lineB: 1, lineC: 99},
	}
	for _, seed := range seeds {
		f.Add(seed.source, seed.langSeed, seed.targetSeed, seed.maxSeed, seed.lineA, seed.lineB, seed.lineC)
	}

	f.Fuzz(func(t *testing.T, source string, langSeed, targetSeed, maxSeed uint8, lineA, lineB, lineC uint16) {
		const (
			maxSourceBytes = 4096
			maxLines       = 200
		)
		if len(source) > maxSourceBytes {
			t.Skip("source too large for focused codeblock fuzzing")
		}

		lines := strings.Split(source, "\n")
		if len(lines) == 0 {
			return
		}
		if len(lines) > maxLines {
			t.Skip("too many source lines for focused codeblock fuzzing")
		}

		maxBlockLines := int(maxSeed%60) + 1
		expander := NewExpander(
			fuzzLanguage(langSeed),
			maxBlockLines,
			WithTarget(fuzzTarget(targetSeed)),
		)
		validMatches := []RawMatch{
			{File: "fuzz.txt", Line: fuzzLine(lineA, len(lines)), Text: "needle"},
			{File: "fuzz.txt", Line: fuzzLine(lineB, len(lines)), Text: "needle"},
			{File: "fuzz.txt", Line: fuzzLine(lineC, len(lines)), Text: "needle"},
		}
		validMatchLines := make(map[int]struct{}, len(validMatches))
		for _, match := range validMatches {
			validMatchLines[match.Line] = struct{}{}
		}
		matches := append([]RawMatch(nil), validMatches...)
		matches = append(
			matches,
			RawMatch{File: "fuzz.txt", Line: 0, Text: "invalid-zero"},
			RawMatch{File: "fuzz.txt", Line: -1, Text: "invalid-negative"},
			RawMatch{File: "fuzz.txt", Line: len(lines) + 1, Text: "invalid-out-of-range"},
		)

		blocks := expander.ExpandMatches("fuzz.txt", lines, matches)
		if len(blocks) > len(validMatches) {
			t.Fatalf("got %d blocks for %d valid single-line matches", len(blocks), len(validMatches))
		}

		for _, block := range blocks {
			if block.File != "fuzz.txt" {
				t.Fatalf("block file = %q, want fuzz.txt", block.File)
			}
			if block.Language != string(expander.lang) {
				t.Fatalf("block language = %q, want %q", block.Language, expander.lang)
			}
			if block.StartLine < 1 || block.EndLine < block.StartLine || block.EndLine > len(lines) {
				t.Fatalf("invalid block range %d-%d for %d lines", block.StartLine, block.EndLine, len(lines))
			}
			if got, want := block.Source, strings.Join(lines[block.StartLine-1:block.EndLine], "\n"); got != want {
				t.Fatalf("block source = %q, want %q", got, want)
			}
			if block.MatchCount != len(block.MatchLines) {
				t.Fatalf("match count = %d, want len(match lines) %d", block.MatchCount, len(block.MatchLines))
			}
			if !sortedUnique(block.MatchLines) {
				t.Fatalf("match lines are not sorted and unique: %v", block.MatchLines)
			}
			for _, line := range block.MatchLines {
				if line < block.StartLine || line > block.EndLine {
					t.Fatalf("match line %d outside block range %d-%d", line, block.StartLine, block.EndLine)
				}
				if _, ok := validMatchLines[line]; !ok {
					t.Fatalf("invalid or out-of-range match line %d survived into block metadata", line)
				}
			}
		}
	})
}

func fuzzLanguage(seed uint8) Language {
	languages := []Language{LangGo, LangPython, LangJS, LangTS, LangGDScript, LangElixir, LangRuby, LangLua, LangGeneric}
	return languages[int(seed)%len(languages)]
}

func fuzzTarget(seed uint8) Target {
	targets := []Target{TargetAny, TargetBlock, TargetFunction, TargetClass}
	return targets[int(seed)%len(targets)]
}

func fuzzLine(seed uint16, lineCount int) int {
	return int(seed%uint16(lineCount)) + 1
}

func sortedUnique(lines []int) bool {
	previous := 0
	for i, line := range lines {
		if i > 0 && line <= previous {
			return false
		}
		previous = line
	}
	return true
}

func assertCandidate(t *testing.T, got, want goCandidate) {
	t.Helper()
	if got.startLine != want.startLine || got.endLine != want.endLine || got.symbolName != want.symbolName || got.symbolKind != want.symbolKind {
		t.Fatalf("candidate = %+v, want %+v", got, want)
	}
}
