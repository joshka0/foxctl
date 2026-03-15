package env

import (
	"reflect"
	"testing"

	"github.com/jkatigb/agentctl/internal/rlm"
)

func TestFilterToolsCodeIntel(t *testing.T) {
	t.Parallel()

	in := []rlm.Tool{
		{Name: "get_top_of_mind"},
		{Name: "semantic_search_code"},
		{Name: "smart_search_code"},
		{Name: "ripgrep_code"},
		{Name: "search_repo"},
		{Name: "load_file"},
		{Name: "read_note"},
	}
	got := FilterTools(in, ToolProfileCodeIntel)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"semantic_search_code", "smart_search_code", "ripgrep_code", "load_file", "read_note"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("FilterTools()=%v want %v", names, want)
	}
}
