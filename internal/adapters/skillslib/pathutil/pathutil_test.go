package pathutil

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveSearchPath(t *testing.T) {
	// Create a temp workspace for testing
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		workspace   string
		candidate   string
		wantErr     bool
		checkResult func(t *testing.T, ws, path string)
	}{
		{
			name:      "empty candidate defaults to workspace",
			workspace: tmpDir,
			candidate: "",
			checkResult: func(t *testing.T, ws, path string) {
				if path != tmpDir {
					t.Errorf("expected path = workspace, got %q", path)
				}
			},
		},
		{
			name:      "relative path under workspace",
			workspace: tmpDir,
			candidate: "subdir",
			checkResult: func(t *testing.T, ws, path string) {
				expected := filepath.Join(tmpDir, "subdir")
				if path != expected {
					t.Errorf("expected %q, got %q", expected, path)
				}
			},
		},
		{
			name:      "absolute path under workspace",
			workspace: tmpDir,
			candidate: filepath.Join(tmpDir, "subdir"),
			checkResult: func(t *testing.T, ws, path string) {
				expected := filepath.Join(tmpDir, "subdir")
				if path != expected {
					t.Errorf("expected %q, got %q", expected, path)
				}
			},
		},
		{
			name:      "path outside workspace",
			workspace: tmpDir,
			candidate: "/some/other/path",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, path, err := ResolveSearchPath(tt.workspace, tt.candidate)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if !errors.Is(err, ErrOutsideWorkspace) {
					t.Errorf("expected ErrOutsideWorkspace, got %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.checkResult != nil {
				tt.checkResult(t, ws, path)
			}
		})
	}
}

func TestRelTo(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		target string
		want   string
	}{
		{
			name:   "same directory",
			base:   "/home/user/project",
			target: "/home/user/project/file.go",
			want:   "file.go",
		},
		{
			name:   "subdirectory",
			base:   "/home/user/project",
			target: "/home/user/project/src/main.go",
			want:   "src/main.go",
		},
		{
			name:   "empty base uses current dir",
			base:   "",
			target: "file.go",
			want:   "file.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelTo(tt.base, tt.target)
			if got != tt.want {
				t.Errorf("RelTo(%q, %q) = %q, want %q", tt.base, tt.target, got, tt.want)
			}
		})
	}
}

func TestIsHidden(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"file.go", false},
		{".hidden", true},
		{".git/config", true},
		{"src/.hidden/file.go", true},
		{"src/file.go", false},
		{".", false},
		{"..", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsHidden(tt.path); got != tt.want {
				t.Errorf("IsHidden(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsDotfile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"file.go", false},
		{".hidden", true},
		{".git", true},
		{"src/.hidden", true},
		{"src/file.go", false},
		{".", false},
		{"..", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsDotfile(tt.path); got != tt.want {
				t.Errorf("IsDotfile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			name:  "empty",
			paths: []string{},
			want:  "",
		},
		{
			name:  "single path",
			paths: []string{"/home/user/project/file.go"},
			want:  "/home/user/project",
		},
		{
			name:  "common parent",
			paths: []string{"/home/user/project/a.go", "/home/user/project/b.go"},
			want:  "/home/user/project",
		},
		{
			name:  "different subdirs",
			paths: []string{"/home/user/project/src/a.go", "/home/user/project/test/b.go"},
			want:  "/home/user/project",
		},
		{
			name:  "no common prefix",
			paths: []string{"/home/user/a.go", "/var/log/b.go"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommonPrefix(tt.paths)
			// Normalize for comparison
			if got != "" {
				got = filepath.ToSlash(got)
			}
			want := filepath.ToSlash(tt.want)
			if got != want {
				t.Errorf("CommonPrefix(%v) = %q, want %q", tt.paths, got, want)
			}
		})
	}
}

func TestPathError(t *testing.T) {
	err := &PathError{
		Op:        "resolve",
		Path:      "/some/path",
		Workspace: "/workspace",
		Err:       ErrOutsideWorkspace,
	}

	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}

	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Error("expected error to unwrap to ErrOutsideWorkspace")
	}
}
