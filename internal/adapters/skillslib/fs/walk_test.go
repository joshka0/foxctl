package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		globs []string
		want  bool
	}{
		{name: "matches basename", path: "/path/to/file.txt", globs: []string{"*.txt"}, want: true},
		{name: "no match", path: "/path/to/file.txt", globs: []string{"*.go"}, want: false},
		{name: "empty globs", path: "/path/to/file.txt", globs: []string{}, want: false},
		{name: "matches multiple globs", path: "/path/to/test.go", globs: []string{"*.txt", "*.go"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matches(tt.path, tt.globs)
			if got != tt.want {
				t.Errorf("matches(%q, %v) = %v, want %v", tt.path, tt.globs, got, tt.want)
			}
		})
	}
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		globs []string
		want  bool
	}{
		{name: "skip node_modules", path: "/path/to/node_modules", globs: []string{"node_modules"}, want: true},
		{name: "skip .git", path: "/path/to/.git", globs: []string{".git"}, want: true},
		{name: "don't skip normal dir", path: "/path/to/src", globs: []string{".git", "node_modules"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkip(tt.path, tt.globs)
			if got != tt.want {
				t.Errorf("shouldSkip(%q, %v) = %v, want %v", tt.path, tt.globs, got, tt.want)
			}
		})
	}
}

func TestDepth(t *testing.T) {
	tests := []struct {
		rel  string
		want int
	}{
		{rel: ".", want: 0},
		{rel: "file.txt", want: 1},
		{rel: "dir/file.txt", want: 2},
		{rel: "dir/subdir/file.txt", want: 3},
		{rel: "a/b/c/d/e.txt", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			got := depth(tt.rel)
			if got != tt.want {
				t.Errorf("depth(%q) = %d, want %d", tt.rel, got, tt.want)
			}
		})
	}
}

func TestWalkFiles(t *testing.T) {
	tmp := t.TempDir()
	files := []string{
		filepath.Join(tmp, "keep.txt"),
		filepath.Join(tmp, "ignore.bin"),
		filepath.Join(tmp, "nested", "match.go"),
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	entries, err := WalkFiles(ListOptions{
		BasePath: tmp,
		Include:  []string{"*.txt", "*.go"},
		Exclude:  []string{"ignore.bin"},
		MaxDepth: 2,
	})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}
