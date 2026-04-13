//go:build cgo

package main

import (
	"testing"

	symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
)

func TestAnalyzeTypeScriptSemanticSimplificationsFindsBooleanLiteralComparison(t *testing.T) {
	src := []byte(`function shouldRun(flag: boolean): boolean {
  return flag === true;
}
`)
	symbols := []symindex.Symbol{{
		Name:      "shouldRun",
		Language:  "typescript",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeTypeScriptSemanticSimplifications("shouldRun.ts", "shouldRun.ts", "typescript", src, symbols)
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

func TestAnalyzeTypeScriptSemanticSimplificationsSkipsUnsafeOrTrue(t *testing.T) {
	src := []byte(`function keepCall(): boolean {
  return expensive() || true;
}
`)
	symbols := []symindex.Symbol{{
		Name:      "keepCall",
		Language:  "typescript",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeTypeScriptSemanticSimplifications("keepCall.ts", "keepCall.ts", "typescript", src, symbols)
	if len(got) != 0 {
		t.Fatalf("expected no simplification finding, got %#v", got)
	}
}
