//go:build cgo

package main

import (
	"context"
	"strings"
	"testing"

	symindex "github.com/jkatigb/agentctl/internal/indexing/symbol"
)

func TestAnalyzeTypeScriptDuplicateRecoveryBlocks(t *testing.T) {
	src := []byte(`export async function requestThing(a: boolean, b: boolean) {
  if (a) {
    setHumanError("retry");
    return await fetchThing();
  }
  if (b) {
    setHumanError("retry");
    return await fetchThing();
  }
  return null;
}
`)

	extractor := symindex.NewTypeScriptExtractor()
	symbols, err := extractor.Extract(context.Background(), "requestThing.ts", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeTypeScriptDuplicateRecoveryBlocks("requestThing.ts", "requestThing.ts", "typescript", src, symbols)
	if len(got) == 0 {
		t.Fatal("expected duplicate_recovery_block finding")
	}
	if got[0].RuleID != "duplicate_recovery_block" {
		t.Fatalf("rule=%q want duplicate_recovery_block", got[0].RuleID)
	}
	lines, ok := got[0].Evidence["duplicate_span_lines"].([]int)
	if !ok {
		t.Fatalf("duplicate_span_lines type=%T", got[0].Evidence["duplicate_span_lines"])
	}
	if len(lines) != 2 || lines[0] != 2 || lines[1] != 6 {
		t.Fatalf("duplicate_span_lines=%v want [2 6]", lines)
	}
}

func TestAnalyzeTypeScriptDuplicatedErrorRemaps(t *testing.T) {
	src := []byte(`export async function requestThing() {
  try {
    return await fetchThing("a");
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new ApiError("Request timed out.", 408, null);
    }
    throw error;
  }

  try {
    return await fetchThing("b");
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new ApiError("Request timed out.", 408, null);
    }
    throw error;
  }
}
`)

	extractor := symindex.NewTypeScriptExtractor()
	symbols, err := extractor.Extract(context.Background(), "requestThing.ts", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeTypeScriptDuplicatedErrorRemaps("requestThing.ts", "requestThing.ts", "typescript", src, symbols)
	if len(got) == 0 {
		t.Fatal("expected duplicated_error_remap finding")
	}
	if got[0].RuleID != "duplicated_error_remap" {
		t.Fatalf("rule=%q want duplicated_error_remap", got[0].RuleID)
	}
	lines, ok := got[0].Evidence["duplicate_span_lines"].([]int)
	if !ok {
		t.Fatalf("duplicate_span_lines type=%T", got[0].Evidence["duplicate_span_lines"])
	}
	if len(lines) != 2 || lines[0] != 5 || lines[1] != 14 {
		t.Fatalf("duplicate_span_lines=%v want [5 14]", lines)
	}
}

func TestAnalyzeTypeScriptRepeatedGuardLadders(t *testing.T) {
	src := []byte(`export async function requestThing(response: { status: number }) {
  if (response.status === 401) {
    return retryWithRefresh();
  }
  if (response.status === 401 && shouldFallback()) {
    return retryWithoutAuth();
  }
  return ok();
}
`)

	extractor := symindex.NewTypeScriptExtractor()
	symbols, err := extractor.Extract(context.Background(), "requestThing.ts", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeTypeScriptRepeatedGuardLadders("requestThing.ts", "requestThing.ts", "typescript", src, symbols)
	if len(got) == 0 {
		t.Fatal("expected repeated_guard_ladder finding")
	}
	if got[0].RuleID != "repeated_guard_ladder" {
		t.Fatalf("rule=%q want repeated_guard_ladder", got[0].RuleID)
	}
	lines, ok := got[0].Evidence["duplicate_span_lines"].([]int)
	if !ok {
		t.Fatalf("duplicate_span_lines type=%T", got[0].Evidence["duplicate_span_lines"])
	}
	if len(lines) != 2 || lines[0] != 2 || lines[1] != 5 {
		t.Fatalf("duplicate_span_lines=%v want [2 5]", lines)
	}
	preview, ok := got[0].Evidence["guard_preview"].(string)
	if !ok || !strings.Contains(preview, "response.status") {
		t.Fatalf("guard_preview=%#v", got[0].Evidence["guard_preview"])
	}
}
