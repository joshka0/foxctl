//go:build cgo

package main

import (
	"testing"

	symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
)

func TestAnalyzeElixirPreloadAfterGetChainsFindsExactLoaderChain(t *testing.T) {
	src := []byte(`def resolve_source(target_id) do
  with %Offering{} = offering <- Repo.get(Offering, target_id) |> Repo.preload(:media_assets) do
    offering
  end
end
`)
	symbols := []symindex.Symbol{{
		Name:      "resolve_source",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirPreloadAfterGetChains("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].RuleID != "preload_after_get_chain" {
		t.Fatalf("rule=%q want preload_after_get_chain", got[0].RuleID)
	}
	if got[0].Evidence["normalized_shape"] != "Repo.get |> Repo.preload" {
		t.Fatalf("normalized_shape=%#v", got[0].Evidence["normalized_shape"])
	}
}

func TestAnalyzeElixirTransactionScriptHotspotsFindsAnonymousTransactionBody(t *testing.T) {
	src := []byte(`def process(id, changeset) do
  Repo.transaction(fn ->
    current = Repo.get(Foo, id)
    Repo.update!(changeset)
    if current do
      Repo.insert!(Audit.log(%{id: id}))
    end
    current
  end)
end
`)
	symbols := []symindex.Symbol{{
		Name:      "process",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirTransactionScriptHotspots("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].RuleID != "transaction_script_hotspot" {
		t.Fatalf("rule=%q want transaction_script_hotspot", got[0].RuleID)
	}
	if got[0].Evidence["repo_call_count"] != 3 {
		t.Fatalf("repo_call_count=%#v want 3", got[0].Evidence["repo_call_count"])
	}
}

func TestAnalyzeElixirPostTransactionPreloadsFindsCaseReloadPattern(t *testing.T) {
	src := []byte(`def process(changeset) do
  Multi.new()
  |> Multi.insert(:reflection, changeset)
  |> Repo.transaction()
  |> case do
    {:ok, %{reflection: reflection}} -> {:ok, Repo.preload(reflection, :author)}
    {:error, _step, reason, _changes} -> {:error, reason}
  end
end
`)
	symbols := []symindex.Symbol{{
		Name:      "process",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirPostTransactionPreloads("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].RuleID != "post_transaction_preload" {
		t.Fatalf("rule=%q want post_transaction_preload", got[0].RuleID)
	}
	targets, ok := got[0].Evidence["preload_targets"].([]string)
	if !ok || len(targets) == 0 {
		t.Fatalf("preload_targets=%#v", got[0].Evidence["preload_targets"])
	}
}

func TestAnalyzeElixirTransactionScriptHotspotsFindsEctoMultiPipeline(t *testing.T) {
	src := []byte(`def process(changeset) do
  Ecto.Multi.new()
  |> Ecto.Multi.insert(:foo, changeset)
  |> Ecto.Multi.run(:audit, fn _repo, _changes -> {:ok, :done} end)
  |> Repo.transaction()
end
`)
	symbols := []symindex.Symbol{{
		Name:      "process",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirTransactionScriptHotspots("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1 (%#v)", len(got), got)
	}
	if got[0].RuleID != "transaction_script_hotspot" {
		t.Fatalf("rule=%q want transaction_script_hotspot", got[0].RuleID)
	}
	if got[0].Evidence["multi_step_count"] != 2 {
		t.Fatalf("multi_step_count=%#v want 2", got[0].Evidence["multi_step_count"])
	}
}

func TestAnalyzeElixirTransactionScriptHotspotsSkipsTrivialEctoMultiPipeline(t *testing.T) {
	src := []byte(`def process(changeset) do
  Ecto.Multi.new()
  |> Ecto.Multi.insert(:foo, changeset)
  |> Repo.transaction()
end
`)
	symbols := []symindex.Symbol{{
		Name:      "process",
		Language:  "elixir",
		Kind:      symindex.KindFunction,
		StartLine: 1,
		StartByte: 0,
		EndByte:   len(src),
	}}

	got := analyzeElixirTransactionScriptHotspots("demo.ex", "demo.ex", "elixir", src, symbols)
	if len(got) != 0 {
		t.Fatalf("expected no transaction_script_hotspot, got %#v", got)
	}
}
