package env

import (
	"reflect"
	"testing"

	"github.com/joshka0/foxctl/internal/rlm"
)

func TestFilterToolsCodeIntel(t *testing.T) {
	t.Parallel()

	in := []rlm.Tool{
		{Name: "get_top_of_mind"},
		{Name: "semantic_search_code"},
		{Name: "smart_search_code"},
		{Name: "ripgrep_code"},
		{Name: "code_search_ensemble"},
		{Name: "search_repo"},
		{Name: "load_file"},
		{Name: "read_note"},
	}
	got := FilterTools(in, ToolProfileCodeIntel)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"semantic_search_code", "smart_search_code", "ripgrep_code", "code_search_ensemble", "load_file", "read_note"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterTools()=%v want %v", names, want)
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

func TestSelectMemoryScoutRolesDefaultsToDeterministicOrder(t *testing.T) {
	t.Parallel()

	got := selectMemoryScoutRoles(nil, 3)
	want := []string{ScoutRoleMemoryFact, ScoutRoleMemoryTimeline, ScoutRoleACAContext}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectMemoryScoutRoles()=%v want %v", got, want)
	}
}
