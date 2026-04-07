package scope

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveResolvedPathFileAuto(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	file := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveResolvedPath(workspace, file, info, "auto", false)
	if err != nil {
		t.Fatalf("ResolveResolvedPath() error = %v", err)
	}
	if got.Language != "go" {
		t.Fatalf("language=%q want go", got.Language)
	}
	if got.Mode != "auto_file" {
		t.Fatalf("mode=%q want auto_file", got.Mode)
	}
	if got.Path != "main.go" {
		t.Fatalf("path=%q want main.go", got.Path)
	}
}

func TestResolveResolvedPathDirectoryMixedFails(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.ts"), []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ResolveResolvedPath(workspace, workspace, info, "auto", false)
	if err == nil {
		t.Fatal("expected mixed-language directory to fail")
	}
	re, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("err type=%T want *ResolveError", err)
	}
	if re.Hint == "" {
		t.Fatalf("expected hint, got %#v", re)
	}
}

func TestResolveRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ResolveResolvedPath(workspace, file, info, "go", false)
	if err == nil {
		t.Fatal("expected outside path to fail")
	}
}
