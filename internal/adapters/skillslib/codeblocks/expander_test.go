package codeblocks

import "testing"

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

func assertCandidate(t *testing.T, got, want goCandidate) {
	t.Helper()
	if got.startLine != want.startLine || got.endLine != want.endLine || got.symbolName != want.symbolName || got.symbolKind != want.symbolKind {
		t.Fatalf("candidate = %+v, want %+v", got, want)
	}
}
