package toolnames

import "testing"

func TestCanonicalizeToolName(t *testing.T) {
	tests := []struct {
		name       string
		mode       ToolMode
		expected   string
		expectedOK bool
	}{
		{name: "runtime dotted", mode: ToolModeRuntime, expected: "fs_read_file", expectedOK: true},
		{name: "runtime slash", mode: ToolModeRuntime, expected: "code_search", expectedOK: true},
		{name: "runtime multi-part dotted", mode: ToolModeRuntime, expected: "repo_index_search", expectedOK: true},
		{name: "runtime multi-part slash", mode: ToolModeRuntime, expected: "repo_index_dag_grep", expectedOK: true},
		{name: "runtime single word", mode: ToolModeRuntime, expected: "shell", expectedOK: true},
		{name: "legacy underscore", mode: ToolModeLegacy, expected: "context.grep", expectedOK: true},
		{name: "legacy multi-part underscore", mode: ToolModeLegacy, expected: "repo.index.search", expectedOK: true},
		{name: "legacy multi-part first-dot", mode: ToolModeLegacy, expected: "repo.index.search", expectedOK: true},
		{name: "legacy uppercase", mode: ToolModeRuntime, expected: "bb_mark_read", expectedOK: true},
		{name: "unknown", mode: ToolModeLegacy, expectedOK: false},
	}

	for _, tc := range tests {
		var input string
		switch tc.name {
		case "runtime dotted":
			input = "fs.read_file"
		case "runtime slash":
			input = "code/search"
		case "runtime multi-part dotted":
			input = "repo.index.search"
		case "runtime multi-part slash":
			input = "repo/index/dag/grep"
		case "runtime single word":
			input = "shell"
		case "legacy underscore":
			input = "context.grep"
		case "legacy multi-part underscore":
			input = "repo_index_search"
		case "legacy multi-part first-dot":
			input = "repo.index_search"
		case "legacy uppercase":
			input = "BB.MARK_READ"
		case "unknown":
			input = "unknown.tool"
		}

		got, ok := CanonicalizeToolName(tc.mode, input)
		if ok != tc.expectedOK {
			t.Fatalf("%s: expected ok %v, got %v", tc.name, tc.expectedOK, ok)
		}
		if ok && got != tc.expected {
			t.Fatalf("%s: expected %s, got %s", tc.name, tc.expected, got)
		}
	}
}

func TestNormalizeAllowlist(t *testing.T) {
	input := []string{"fs.read_file", "code_search", "code.search", "unknown", "fs/read_file"}
	got := NormalizeAllowlist(ToolModeRuntime, input)
	expected := []string{"fs_read_file", "code_search", "unknown"}

	if len(got) != len(expected) {
		t.Fatalf("expected len %d, got %d", len(expected), len(got))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("expected %s at idx %d, got %s", expected[i], i, got[i])
		}
	}
}

func TestValidateAllowlist(t *testing.T) {
	normalized, unknown := ValidateAllowlist(ToolModeRuntime, []string{"fs.read_file", "unknown", "bb/mark_read", "bad.tool", "fs/list_dir"})
	expectedNormalized := []string{"fs_read_file", "unknown", "bb_mark_read", "bad.tool", "fs_list_dir"}
	expectedUnknown := []string{"unknown", "bad.tool"}

	if len(normalized) != len(expectedNormalized) {
		t.Fatalf("expected normalized len %d, got %d", len(expectedNormalized), len(normalized))
	}
	for i := range expectedNormalized {
		if normalized[i] != expectedNormalized[i] {
			t.Fatalf("expected normalized %s at idx %d, got %s", expectedNormalized[i], i, normalized[i])
		}
	}
	if len(unknown) != len(expectedUnknown) {
		t.Fatalf("expected unknown len %d, got %d", len(expectedUnknown), len(unknown))
	}
	for i := range expectedUnknown {
		if unknown[i] != expectedUnknown[i] {
			t.Fatalf("expected unknown %s at idx %d, got %s", expectedUnknown[i], i, unknown[i])
		}
	}
}
