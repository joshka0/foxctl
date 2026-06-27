package env

import (
	"reflect"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/rlm"
)

func TestFilterToolsCodeIntel(t *testing.T) {
	t.Parallel()

	in := readOnlyTools([]rlm.Tool{
		{Name: "plan_context_query"},
		{Name: "gather_context"},
		{Name: "gather_memory_context"},
		{Name: "gather_test_context"},
		{Name: "gather_docs_context"},
		{Name: "expand_context_graph"},
		{Name: "load_evidence_ref"},
		{Name: "aggregate_evidence_refs"},
		{Name: "evidence_ledger"},
		{Name: "code_search_ensemble"},
		{Name: "retrieve_code"},
		{Name: "retrieve_memory"},
		{Name: "retrieve_context"},
		{Name: "retrieve_task"},
		{Name: "retrieve_mixed"},
	})
	got := FilterTools(in, ToolProfileCodeIntel)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"plan_context_query", "gather_context", "gather_memory_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "aggregate_evidence_refs", "evidence_ledger", "code_search_ensemble", "retrieve_code"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterTools()=%v want %v", names, want)
	}
}

func TestFilterToolsGatherContext(t *testing.T) {
	t.Parallel()

	in := readOnlyTools([]rlm.Tool{
		{Name: "plan_context_query"},
		{Name: "gather_context"},
		{Name: "gather_memory_context"},
		{Name: "gather_test_context"},
		{Name: "gather_docs_context"},
		{Name: "expand_context_graph"},
		{Name: "load_evidence_ref"},
		{Name: "aggregate_evidence_refs"},
		{Name: "evidence_ledger"},
		{Name: "retrieve_code"},
		{Name: "retrieve_memory"},
		{Name: "retrieve_context"},
		{Name: "retrieve_task"},
		{Name: "retrieve_mixed"},
	})
	got := FilterTools(in, ToolProfileGatherContext)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"plan_context_query", "gather_context", "gather_memory_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "aggregate_evidence_refs", "evidence_ledger"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterTools()=%v want %v", names, want)
	}
}

func TestFilterToolsNativeExplorer(t *testing.T) {
	t.Parallel()

	in := readOnlyTools([]rlm.Tool{
		{Name: "plan_context_query"},
		{Name: "gather_context"},
		{Name: "gather_memory_context"},
		{Name: "gather_test_context"},
		{Name: "gather_docs_context"},
		{Name: "expand_context_graph"},
		{Name: "load_evidence_ref"},
		{Name: "aggregate_evidence_refs"},
		{Name: "evidence_ledger"},
		{Name: "code_search_ensemble"},
		{Name: "retrieve_code"},
		{Name: "retrieve_mixed"},
	})
	got := FilterTools(in, ToolProfileNativeExplorer)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"plan_context_query", "gather_context", "gather_memory_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "aggregate_evidence_refs", "evidence_ledger"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterTools()=%v want %v", names, want)
	}
}

func TestFilterToolsMemoryRecall(t *testing.T) {
	t.Parallel()

	in := readOnlyTools([]rlm.Tool{
		{Name: "plan_context_query"},
		{Name: "gather_context"},
		{Name: "gather_memory_context"},
		{Name: "gather_test_context"},
		{Name: "gather_docs_context"},
		{Name: "expand_context_graph"},
		{Name: "load_evidence_ref"},
		{Name: "aggregate_evidence_refs"},
		{Name: "evidence_ledger"},
		{Name: "retrieve_code"},
		{Name: "retrieve_memory"},
		{Name: "retrieve_context"},
		{Name: "retrieve_task"},
		{Name: "retrieve_mixed"},
	})
	got := FilterTools(in, ToolProfileMemoryRecall)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"plan_context_query", "gather_context", "gather_memory_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "aggregate_evidence_refs", "evidence_ledger", "retrieve_memory", "retrieve_context"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterTools()=%v want %v", names, want)
	}
}

func TestFilterToolsLongCoTMinimalReturnsNoTools(t *testing.T) {
	t.Parallel()

	in := []rlm.Tool{
		{Name: "retrieve_code"},
		{Name: "retrieve_mixed"},
		{Name: "load_evidence_ref"},
	}
	for _, profile := range []string{ToolProfileLongCoTNoModelTools} {
		if got := FilterTools(in, profile); len(got) != 0 {
			t.Fatalf("FilterTools(%q)=%v want no tools", profile, got)
		}
	}
}

func TestResolveToolProfileUnknownFailsClosed(t *testing.T) {
	t.Parallel()

	in := []rlm.Tool{
		{Name: "retrieve_code"},
		{Name: "load_evidence_ref"},
	}

	if got := FilterTools(in, "longcot-repl"); len(got) != 0 {
		t.Fatalf("FilterTools(unknown)=%v want no tools", got)
	}

	_, err := ResolveToolProfile(in, "longcot-repl")
	if err == nil {
		t.Fatal("ResolveToolProfile() error = nil, want unsupported profile error")
	}
	if !strings.Contains(err.Error(), "unsupported tool profile") {
		t.Fatalf("ResolveToolProfile() error = %v", err)
	}
}

func TestFilterToolsForScoutRoleMemoryFact(t *testing.T) {
	t.Parallel()

	in := []rlm.Tool{
		{Name: "get_top_of_mind"},
		{Name: "search_artifacts"},
		{Name: "load_artifact"},
		{Name: "search_scenes"},
		{Name: "get_scene"},
		{Name: "search_vault"},
		{Name: "read_note"},
	}
	got := FilterToolsForScoutRole(in, ScoutRoleMemoryFact)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"search_artifacts", "load_artifact", "search_scenes", "get_scene", "search_vault", "read_note"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterToolsForScoutRole()=%v want %v", names, want)
	}
}

func TestDecoratePromptForScoutRole(t *testing.T) {
	t.Parallel()

	got := DecoratePromptForScoutRole(ScoutRoleMemoryTimeline, "Find the latest codename update.")
	if got == "Find the latest codename update." {
		t.Fatal("expected decorated prompt")
	}
	if !reflect.DeepEqual(NormalizeScoutRole("memory_timeline_scout"), ScoutRoleMemoryTimeline) {
		t.Fatal("expected normalized scout role")
	}
}

func readOnlyTools(tools []rlm.Tool) []rlm.Tool {
	for i := range tools {
		tools[i].ReadOnly = true
	}
	return tools
}

func TestSelectMemoryScoutRolesDefaultsToDeterministicOrder(t *testing.T) {
	t.Parallel()

	got := selectMemoryScoutRoles(nil, 3)
	want := []string{ScoutRoleMemoryFact, ScoutRoleMemoryTimeline, ScoutRoleContextWiki}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectMemoryScoutRoles()=%v want %v", got, want)
	}
}
