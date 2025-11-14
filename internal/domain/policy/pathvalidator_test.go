package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathValidatorBasic(t *testing.T) {
	workspace := t.TempDir()
	pv, err := NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}

	path := filepath.Join("subdir", "file.txt")
	clean, err := pv.ValidatePath(path)
	if err != nil {
		t.Fatalf("validate workspace path: %v", err)
	}
	if !filepath.IsAbs(clean) {
		t.Fatalf("expected absolute path, got %q", clean)
	}
	if pv.Workspace() != canonicalPath(t, workspace) {
		t.Fatalf("Workspace() = %q, want %q", pv.Workspace(), canonicalPath(t, workspace))
	}
}

func TestPathValidatorBlocksTraversal(t *testing.T) {
	workspace := t.TempDir()
	pv, err := NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}
	if _, err := pv.ValidatePath("../../etc/passwd"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestPathValidatorAllowedRoots(t *testing.T) {
	workspace := t.TempDir()
	tmp := t.TempDir()
	pv, err := NewPathValidator(workspace, []string{tmp})
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}
	target := filepath.Join(tmp, "notes.txt")
	if _, err := pv.ValidatePath(target); err != nil {
		t.Fatalf("expected allowed root to pass: %v", err)
	}
}

func TestPathValidatorSymlinkResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires admin on windows")
	}
	workspace := t.TempDir()
	pv, err := NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	link := filepath.Join(workspace, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := pv.ValidatePath("link"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestPathValidatorDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires admin on windows")
	}
	workspace := t.TempDir()
	pv, err := NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}

	outside := t.TempDir()
	danglingTarget := filepath.Join(outside, "future.txt")
	link := filepath.Join(workspace, "dangling")
	if err := os.Symlink(danglingTarget, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := pv.ValidatePath("dangling"); err == nil {
		t.Fatal("expected dangling symlink to be rejected")
	}
}

func TestPathValidatorNonexistentPaths(t *testing.T) {
	workspace := t.TempDir()
	pv, err := NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}
	target := filepath.Join("subdir", "future.txt")
	clean, err := pv.ValidatePath(target)
	if err != nil {
		t.Fatalf("expected future workspace path to pass: %v", err)
	}
	expected := filepath.Join(canonicalPath(t, workspace), "subdir")
	if filepath.Dir(clean) != expected {
		t.Fatalf("unexpected cleaned path: %q", clean)
	}
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}

func TestPathValidatorErrors(t *testing.T) {
	if _, err := NewPathValidator("", nil); err == nil {
		t.Fatal("expected empty workspace to fail")
	}
}
