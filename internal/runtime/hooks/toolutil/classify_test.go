package toolutil

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestIsWriteOperation(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolCanonical string
		toolKind      string
		expected      bool
	}{
		// CC tools
		{name: "CC Edit", toolName: "Edit", expected: true},
		{name: "CC Write", toolName: "Write", expected: true},
		{name: "CC MultiEdit", toolName: "MultiEdit", expected: true},
		{name: "CC NotebookEdit", toolName: "NotebookEdit", expected: true},
		{name: "CC TodoWrite", toolName: "TodoWrite", expected: true},
		{name: "CC Read", toolName: "Read", expected: false},
		{name: "CC Grep", toolName: "Grep", expected: false},

		// Canonical tools
		{name: "canonical edit", toolCanonical: "edit.apply_patch", expected: true},
		{name: "canonical fs.write", toolCanonical: "fs.write_file", expected: true},
		{name: "canonical fs.create", toolCanonical: "fs.create_file", expected: true},
		{name: "canonical fs.read", toolCanonical: "fs.read_file", expected: false},
		{name: "canonical todo", toolCanonical: "todo.add", expected: true},

		// Explicit kind
		{name: "explicit write kind", toolKind: "write", expected: true},
		{name: "explicit read kind", toolKind: "read", expected: false},

		// Mixed
		{name: "CC with kind override", toolName: "Read", toolKind: "write", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWriteOperation(tt.toolName, tt.toolCanonical, tt.toolKind)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsReadOperation(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolCanonical string
		toolKind      string
		expected      bool
	}{
		{name: "CC Read", toolName: "Read", expected: true},
		{name: "CC Edit", toolName: "Edit", expected: false},
		{name: "canonical fs.read", toolCanonical: "fs.read_file", expected: true},
		{name: "canonical fs.list", toolCanonical: "fs.list_dir", expected: true},
		{name: "canonical fs.write", toolCanonical: "fs.write_file", expected: false},
		{name: "explicit read kind", toolKind: "read", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsReadOperation(tt.toolName, tt.toolCanonical, tt.toolKind)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFSWriteCanonicalOperationsNeverClassifyAsReadProperty(t *testing.T) {
	t.Parallel()

	property := func(rawSuffix string) bool {
		suffix := strings.TrimSpace(rawSuffix)
		for _, prefix := range []string{"fs.write", "fs.create"} {
			canonical := prefix + suffix
			if !IsWriteOperation("", canonical, "") {
				return false
			}
			if IsReadOperation("", canonical, "") {
				return false
			}
			if ClassifyTool("", canonical, "") != KindWrite {
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestIsSearchOperation(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolCanonical string
		toolKind      string
		expected      bool
	}{
		{name: "CC Grep", toolName: "Grep", expected: true},
		{name: "CC Glob", toolName: "Glob", expected: true},
		{name: "CC Read", toolName: "Read", expected: false},
		{name: "canonical code.search", toolCanonical: "code.search", expected: true},
		{name: "canonical code.semantic", toolCanonical: "code.semantic_search", expected: true},
		{name: "canonical text.grep", toolCanonical: "text.grep", expected: true},
		{name: "explicit search kind", toolKind: "search", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSearchOperation(tt.toolName, tt.toolCanonical, tt.toolKind)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsExecOperation(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolCanonical string
		toolKind      string
		expected      bool
	}{
		{name: "CC Bash", toolName: "Bash", expected: true},
		{name: "CC Task", toolName: "Task", expected: true},
		{name: "CC Edit", toolName: "Edit", expected: false},
		{name: "canonical tests.run", toolCanonical: "tests.run", expected: true},
		{name: "canonical bash.execute", toolCanonical: "bash.run", expected: true},
		{name: "explicit exec kind", toolKind: "exec", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsExecOperation(tt.toolName, tt.toolCanonical, tt.toolKind)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClassifyTool(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolCanonical string
		toolKind      string
		expected      Kind
	}{
		// CC tools
		{name: "CC Edit", toolName: "Edit", expected: KindWrite},
		{name: "CC Write", toolName: "Write", expected: KindWrite},
		{name: "CC Read", toolName: "Read", expected: KindRead},
		{name: "CC Grep", toolName: "Grep", expected: KindSearch},
		{name: "CC Glob", toolName: "Glob", expected: KindSearch},
		{name: "CC Bash", toolName: "Bash", expected: KindExec},
		{name: "CC Task", toolName: "Task", expected: KindExec},
		{name: "CC TodoWrite", toolName: "TodoWrite", expected: KindWrite},

		// Canonical tools
		{name: "canonical edit", toolCanonical: "edit.apply_patch", expected: KindWrite},
		{name: "canonical fs.read", toolCanonical: "fs.read_file", expected: KindRead},
		{name: "canonical code.search", toolCanonical: "code.search", expected: KindSearch},
		{name: "canonical tests", toolCanonical: "tests.run", expected: KindExec},

		// Explicit kind overrides
		{name: "explicit kind", toolKind: "write", expected: KindWrite},
		{name: "explicit any", toolKind: "any", toolName: "Edit", expected: KindWrite},

		// Unknown
		{name: "unknown tool", toolName: "Unknown", expected: KindAny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyTool(tt.toolName, tt.toolCanonical, tt.toolKind)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestToCanonical(t *testing.T) {
	tests := []struct {
		toolName string
		expected string
	}{
		{"Edit", "edit.apply_patch"},
		{"Write", "fs.write_file"},
		{"Read", "fs.read_file"},
		{"MultiEdit", "edit.multi_patch"},
		{"Grep", "text.grep"},
		{"Bash", "shell.execute"},
		{"TodoWrite", "todo.write"},
		{"edit.apply_patch", "edit.apply_patch"}, // Already canonical
		{"Unknown", "Unknown"},                   // Unknown
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := ToCanonical(tt.toolName)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestToCC(t *testing.T) {
	tests := []struct {
		toolCanonical string
		expected      string
	}{
		{"edit.apply_patch", "Edit"},
		{"fs.read_file", "Read"},
		{"fs.write_file", "Write"},
		{"unknown.tool", ""},
	}

	for _, tt := range tests {
		t.Run(tt.toolCanonical, func(t *testing.T) {
			result := ToCC(tt.toolCanonical)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
