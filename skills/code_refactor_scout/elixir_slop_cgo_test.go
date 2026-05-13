//go:build cgo

package main

import (
	"context"
	"sort"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func TestAnalyzeElixirDuplicateRecoveryBlocks(t *testing.T) {
	src := []byte(`defmodule Demo do
  def run(timeout?, retry?) do
    if timeout? do
      raise TimeoutError
    end

    if retry? do
      raise TimeoutError
    end

    :ok
  end
end
`)

	extractor := symindex.NewElixirExtractor()
	symbols, err := extractor.Extract(context.Background(), "demo.ex", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeElixirDuplicateRecoveryBlocks("demo.ex", "demo.ex", "elixir", src, symbols)
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
	if len(lines) != 2 || lines[0] != 3 || lines[1] != 7 {
		t.Fatalf("duplicate_span_lines=%v want [3 7]", lines)
	}
}

func TestAnalyzeElixirRepeatedGuardLadders(t *testing.T) {
	src := []byte(`defmodule Demo do
  def run(response) do
    if response.status == 401 do
      retry(:refresh)
    end

    if response.status == 401 and fallback?() do
      retry(:anon)
    end

    :ok
  end
end
`)

	extractor := symindex.NewElixirExtractor()
	symbols, err := extractor.Extract(context.Background(), "demo.ex", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeElixirRepeatedGuardLadders("demo.ex", "demo.ex", "elixir", src, symbols)
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
	if len(lines) != 2 || lines[0] != 3 || lines[1] != 7 {
		t.Fatalf("duplicate_span_lines=%v want [3 7]", lines)
	}
	preview, ok := got[0].Evidence["guard_preview"].(string)
	if !ok || !strings.Contains(preview, "response.status") {
		t.Fatalf("guard_preview=%#v", got[0].Evidence["guard_preview"])
	}
}

func TestAnalyzeElixirDuplicatedErrorRemaps(t *testing.T) {
	src := []byte(`defmodule Demo do
  def run do
    try do
      work(:a)
    rescue
      error in RuntimeError ->
        if error.message == "timeout" do
          raise TimeoutError
        end
        reraise error, __STACKTRACE__
    end

    try do
      work(:b)
    rescue
      error in RuntimeError ->
        if error.message == "timeout" do
          raise TimeoutError
        end
        reraise error, __STACKTRACE__
    end
  end
end
`)

	extractor := symindex.NewElixirExtractor()
	symbols, err := extractor.Extract(context.Background(), "demo.ex", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeElixirDuplicatedErrorRemaps("demo.ex", "demo.ex", "elixir", src, symbols)
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
	if len(lines) != 2 || lines[0] != 7 || lines[1] != 17 {
		t.Fatalf("duplicate_span_lines=%v want [7 17]", lines)
	}
}

func TestAnalyzeElixirDuplicatedTupleErrorRemaps(t *testing.T) {
	src := []byte(`defmodule Demo do
  def run(result) do
    case result do
      {:error, :transport_error} ->
        {:error, :moderation_unavailable}

      {:error, :provider_error} ->
        {:error, :moderation_unavailable}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
`)

	extractor := symindex.NewElixirExtractor()
	symbols, err := extractor.Extract(context.Background(), "demo.ex", src)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	got := analyzeElixirDuplicatedErrorRemaps("demo.ex", "demo.ex", "elixir", src, symbols)
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
	if len(lines) != 2 || lines[0] != 4 || lines[1] != 7 {
		t.Fatalf("duplicate_span_lines=%v want [4 7]", lines)
	}
	groupKind, ok := got[0].Evidence["group_kind"].(string)
	if !ok || groupKind != "tuple_clause" {
		t.Fatalf("group_kind=%#v", got[0].Evidence["group_kind"])
	}
}

func TestElixirNodeFingerprintNormalizesIdentifiersInsideSameGuardShape(t *testing.T) {
	root, content := parseElixirRootForTest(t, `defmodule Demo do
  def run(response, payload) do
    if response.status == 401 do
      :ok
    end

    if payload.status == 401 do
      :ok
    end
  end
end
`)

	ifCalls := collectElixirCallsByTarget(root, content, "if")
	if len(ifCalls) != 2 {
		t.Fatalf("len(ifCalls)=%d want 2", len(ifCalls))
	}
	firstArgs := elixirCallArgumentsLocal(&ifCalls[0])
	secondArgs := elixirCallArgumentsLocal(&ifCalls[1])
	firstFP := elixirNodeFingerprint(firstArgs, content)
	secondFP := elixirNodeFingerprint(secondArgs, content)
	if firstFP != secondFP {
		t.Fatalf("fingerprints differ: %q != %q", firstFP, secondFP)
	}
	if strings.Contains(firstFP, "response") || strings.Contains(firstFP, "payload") {
		t.Fatalf("fingerprint leaked identifier names: %q", firstFP)
	}
}

func TestElixirGuardAtomsSplitBooleanGuardsAndRejectBareIdentifier(t *testing.T) {
	root, content := parseElixirRootForTest(t, `defmodule Demo do
  def run(response, ready) do
    if response.status == 401 and fallback?() do
      :ok
    end

    if ready do
      :ok
    end
  end
end
`)

	ifCalls := collectElixirCallsByTarget(root, content, "if")
	if len(ifCalls) != 2 {
		t.Fatalf("len(ifCalls)=%d want 2", len(ifCalls))
	}

	splitAtoms := elixirGuardAtoms(elixirCallArgumentsLocal(&ifCalls[0]), content)
	if len(splitAtoms) != 2 {
		t.Fatalf("len(splitAtoms)=%d want 2 (%#v)", len(splitAtoms), splitAtoms)
	}
	fingerprints := make([]string, 0, len(splitAtoms))
	previews := make([]string, 0, len(splitAtoms))
	for _, atom := range splitAtoms {
		fingerprints = append(fingerprints, atom.Fingerprint)
		previews = append(previews, atom.Preview)
	}
	sort.Strings(fingerprints)
	sort.Strings(previews)
	if !strings.Contains(strings.Join(fingerprints, " "), "dot(var,status)") {
		t.Fatalf("guard fingerprints missing status predicate: %v", fingerprints)
	}
	if !strings.Contains(strings.Join(fingerprints, " "), "call(fallback?)") {
		t.Fatalf("guard fingerprints missing fallback call: %v", fingerprints)
	}
	if !strings.Contains(strings.Join(previews, " | "), "fallback?()") {
		t.Fatalf("guard previews missing fallback call: %v", previews)
	}

	rejected := elixirGuardAtoms(elixirCallArgumentsLocal(&ifCalls[1]), content)
	if len(rejected) != 0 {
		t.Fatalf("expected no atoms for bare identifier guard, got %#v", rejected)
	}
}

func parseElixirRootForTest(t *testing.T, src string) (*sitter.Node, []byte) {
	t.Helper()
	content := []byte(src)
	tree, ok := parseElixirSlopTree(content)
	if !ok || tree == nil {
		t.Fatal("parseElixirSlopTree failed")
	}
	t.Cleanup(func() { tree.Close() })
	return tree.RootNode(), content
}

func collectElixirCallsByTarget(root *sitter.Node, content []byte, target string) []sitter.Node {
	if root == nil {
		return nil
	}
	calls := make([]sitter.Node, 0, 4)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "call" && strings.TrimSpace(elixirCallTargetName(node, content)) == target {
			calls = append(calls, *node)
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)
	return calls
}
