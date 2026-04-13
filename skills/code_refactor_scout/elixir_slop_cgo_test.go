//go:build cgo

package main

import (
	"context"
	"strings"
	"testing"

	symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
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
