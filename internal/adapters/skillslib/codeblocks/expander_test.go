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

func assertCandidate(t *testing.T, got, want goCandidate) {
	t.Helper()
	if got.startLine != want.startLine || got.endLine != want.endLine || got.symbolName != want.symbolName || got.symbolKind != want.symbolKind {
		t.Fatalf("candidate = %+v, want %+v", got, want)
	}
}
