package fileio

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadLimitedReadsWorkspaceFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeFile(t, workspace, "nested/file.go", "package main")

	got, err := ReadLimited(workspace, "nested/file.go", DefaultReadLimit)
	if err != nil {
		t.Fatalf("ReadLimited: %v", err)
	}
	if string(got) != "package main" {
		t.Fatalf("content=%q", got)
	}
}

func TestReadLimitedRejectsTraversal(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	for _, path := range []string{"/etc/passwd", "../../../etc/passwd", "foo/../../etc/passwd"} {
		t.Run(path, func(t *testing.T) {
			_, err := ReadLimited(workspace, path, DefaultReadLimit)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("error=%q", err.Error())
			}
		})
	}
}

func TestReadLimitedRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows setups")
	}
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(workspace, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := ReadLimited(workspace, "link.txt", DefaultReadLimit)
	if err == nil {
		t.Fatal("expected symlink escape error")
	}
	if !strings.Contains(err.Error(), "path escapes workspace") {
		t.Fatalf("error=%q", err.Error())
	}
}

func TestReadLimitedRejectsDirectory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := ReadLimited(workspace, "dir", DefaultReadLimit)
	if err == nil {
		t.Fatal("expected directory error")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error=%q", err.Error())
	}
}

func TestReadLimitedRejectsLargeFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeFile(t, workspace, "large.txt", "123456")

	_, err := ReadLimited(workspace, "large.txt", 5)
	if err == nil {
		t.Fatal("expected size error")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("error=%q", err.Error())
	}
}

func writeFile(t *testing.T, workspace, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(workspace, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
