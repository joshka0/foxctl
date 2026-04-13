package pathutil

import (
	"encoding/json"
	"testing"
)

func TestExtractPath(t *testing.T) {
	tests := []struct {
		name     string
		input    json.RawMessage
		expected string
	}{
		{
			name:     "file_path field",
			input:    []byte(`{"file_path": "/src/main.go"}`),
			expected: "/src/main.go",
		},
		{
			name:     "path field",
			input:    []byte(`{"path": "/src/other.go"}`),
			expected: "/src/other.go",
		},
		{
			name:     "file field",
			input:    []byte(`{"file": "/src/another.go"}`),
			expected: "/src/another.go",
		},
		{
			name:     "current_path field",
			input:    []byte(`{"current_path": "/src/current.go"}`),
			expected: "/src/current.go",
		},
		{
			name:     "file_path takes precedence over path",
			input:    []byte(`{"file_path": "/first.go", "path": "/second.go"}`),
			expected: "/first.go",
		},
		{
			name:     "empty input",
			input:    nil,
			expected: "",
		},
		{
			name:     "no path fields",
			input:    []byte(`{"other": "value"}`),
			expected: "",
		},
		{
			name:     "invalid json",
			input:    []byte(`not json`),
			expected: "",
		},
		{
			name:     "empty string path",
			input:    []byte(`{"file_path": ""}`),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPath(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    json.RawMessage
		expected []string
	}{
		{
			name:     "single path",
			input:    []byte(`{"file_path": "/src/main.go"}`),
			expected: []string{"/src/main.go"},
		},
		{
			name:     "edits array (MultiEdit)",
			input:    []byte(`{"edits": [{"file_path": "/a.go"}, {"file_path": "/b.go"}]}`),
			expected: []string{"/a.go", "/b.go"},
		},
		{
			name:     "files array",
			input:    []byte(`{"files": ["/x.go", "/y.go"]}`),
			expected: []string{"/x.go", "/y.go"},
		},
		{
			name:     "duplicates removed",
			input:    []byte(`{"file_path": "/a.go", "edits": [{"file_path": "/a.go"}]}`),
			expected: []string{"/a.go"},
		},
		{
			name:     "empty input",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPaths(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d paths, got %d", len(tt.expected), len(result))
				return
			}
			for i, p := range tt.expected {
				if result[i] != p {
					t.Errorf("path[%d]: expected %q, got %q", i, p, result[i])
				}
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		workspace string
		expected  string
	}{
		{
			name:      "absolute path unchanged",
			path:      "/src/main.go",
			workspace: "/workspace",
			expected:  "/src/main.go",
		},
		{
			name:      "relative path made absolute",
			path:      "src/main.go",
			workspace: "/workspace",
			expected:  "/workspace/src/main.go",
		},
		{
			name:      "relative path without workspace",
			path:      "src/main.go",
			workspace: "",
			expected:  "src/main.go",
		},
		{
			name:      "path with .. resolved",
			path:      "/workspace/../other/file.go",
			workspace: "",
			expected:  "/other/file.go",
		},
		{
			name:      "empty path",
			path:      "",
			workspace: "/workspace",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.path, tt.workspace)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRelativePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		workspace string
		expected  string
	}{
		{
			name:      "path under workspace",
			path:      "/workspace/src/main.go",
			workspace: "/workspace",
			expected:  "src/main.go",
		},
		{
			name:      "path not under workspace",
			path:      "/other/src/main.go",
			workspace: "/workspace",
			expected:  "/other/src/main.go",
		},
		{
			name:      "empty workspace",
			path:      "/src/main.go",
			workspace: "",
			expected:  "/src/main.go",
		},
		{
			name:      "empty path",
			path:      "",
			workspace: "/workspace",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RelativePath(tt.path, tt.workspace)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestIsUnderWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		workspace string
		expected  bool
	}{
		{
			name:      "path under workspace",
			path:      "/workspace/src/main.go",
			workspace: "/workspace",
			expected:  true,
		},
		{
			name:      "path not under workspace",
			path:      "/other/src/main.go",
			workspace: "/workspace",
			expected:  false,
		},
		{
			name:      "workspace itself",
			path:      "/workspace",
			workspace: "/workspace",
			expected:  true,
		},
		{
			name:      "empty workspace",
			path:      "/src/main.go",
			workspace: "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnderWorkspace(tt.path, tt.workspace)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExtension(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/src/main.go", "go"},
		{"/src/main.py", "py"},
		{"/src/main", ""},
		{"", ""},
		{"/src/.gitignore", "gitignore"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := Extension(tt.path)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/src/main_test.go", true}, // Go
		{"/src/main.test.js", true}, // JS
		{"/src/main.spec.ts", true}, // TS
		{"/src/test_main.py", true}, // Python
		{"/src/main.go", false},
		{"/src/testing.go", false}, // Not a test file pattern
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsTestFile(tt.path)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
