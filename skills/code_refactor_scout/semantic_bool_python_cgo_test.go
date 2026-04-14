//go:build cgo

package main

import (
	"testing"

	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func TestAnalyzePythonSemanticSimplificationsFindsBooleanLiteralComparison(t *testing.T) {
	src := []byte("def should_run(flag):\n    return flag == True\n")
	symbols := []symindex.Symbol{{
		Name:      "should_run",
		Language:  "python",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzePythonSemanticSimplifications("should_run.py", "should_run.py", "python", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].RuleID != "semantic_simplification_candidate" {
		t.Fatalf("rule=%q want semantic_simplification_candidate", got[0].RuleID)
	}
	if got[0].Evidence["simplified_form"] != "return flag" {
		t.Fatalf("simplified_form=%#v", got[0].Evidence["simplified_form"])
	}
}

func TestAnalyzePythonSemanticSimplificationsSkipsUnsafeOrTrue(t *testing.T) {
	src := []byte("def keep_call():\n    return expensive() or True\n")
	symbols := []symindex.Symbol{{
		Name:      "keep_call",
		Language:  "python",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzePythonSemanticSimplifications("keep_call.py", "keep_call.py", "python", src, symbols)
	if len(got) != 0 {
		t.Fatalf("expected no simplification finding, got %#v", got)
	}
}
