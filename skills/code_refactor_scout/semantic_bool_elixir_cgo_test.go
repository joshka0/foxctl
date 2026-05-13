//go:build cgo

package main

import (
	"testing"

	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func TestAnalyzeElixirSemanticSimplificationsFindsBooleanLiteralComparison(t *testing.T) {
	src := []byte("def should_run(flag) do\n  flag == true\nend\n")
	symbols := []symindex.Symbol{{
		Name:      "should_run",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirSemanticSimplifications("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].RuleID != "semantic_simplification_candidate" {
		t.Fatalf("rule=%q want semantic_simplification_candidate", got[0].RuleID)
	}
	if got[0].Evidence["simplified_form"] != "flag" {
		t.Fatalf("simplified_form=%#v", got[0].Evidence["simplified_form"])
	}
}

func TestAnalyzeElixirSemanticSimplificationsSkipsUnsafeOrTrue(t *testing.T) {
	src := []byte("def keep_call() do\n  expensive() or true\nend\n")
	symbols := []symindex.Symbol{{
		Name:      "keep_call",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirSemanticSimplifications("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 0 {
		t.Fatalf("expected no simplification finding, got %#v", got)
	}
}

func TestAnalyzeElixirSemanticSimplificationsFindsBooleanReturnWrapper(t *testing.T) {
	src := []byte("def should_run(flag) do\n  if flag == true do\n    true\n  else\n    false\n  end\nend\n")
	symbols := []symindex.Symbol{{
		Name:      "should_run",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirSemanticSimplifications("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].Evidence["simplified_form"] != "flag" {
		t.Fatalf("simplified_form=%#v", got[0].Evidence["simplified_form"])
	}
	patterns, ok := got[0].Evidence["pattern_ids"].([]string)
	if !ok || len(patterns) == 0 || patterns[0] != "boolean_return_wrapper" {
		t.Fatalf("pattern_ids=%#v", got[0].Evidence["pattern_ids"])
	}
}
