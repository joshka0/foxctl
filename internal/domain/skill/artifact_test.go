package skill

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveArtifactPathExec(t *testing.T) {
	dir := t.TempDir()
	manifest := Manifest{
		Distribution: Distribution{
			Type: "exec",
			Exec: &ExecDistribution{Entry: "skills/example/bin"},
		},
	}

	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("bin"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	path, err := ResolveArtifactPath(dir, manifest, ArtifactOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "bin" {
		t.Fatalf("expected bin, got %s", path)
	}
}

func TestResolveArtifactPathExecPreferCGO(t *testing.T) {
	dir := t.TempDir()
	manifest := Manifest{Distribution: Distribution{Type: "exec"}}

	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("bin"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin-cgo"), []byte("bin-cgo"), 0o755); err != nil {
		t.Fatalf("write bin-cgo: %v", err)
	}

	path, err := ResolveArtifactPath(dir, manifest, ArtifactOptions{PreferCGO: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "bin-cgo" {
		t.Fatalf("expected bin-cgo, got %s", path)
	}
}

func TestResolveArtifactPathExecEntryRoot(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	entry := filepath.Join("skills", "example", "bin")
	manifest := Manifest{
		Distribution: Distribution{
			Type: "exec",
			Exec: &ExecDistribution{Entry: entry},
		},
	}

	entryPath := filepath.Join(root, entry)
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(entryPath, []byte("entry"), 0o755); err != nil {
		t.Fatalf("write entry: %v", err)
	}

	path, err := ResolveArtifactPath(dir, manifest, ArtifactOptions{EntryRoot: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != entryPath {
		t.Fatalf("expected %s, got %s", entryPath, path)
	}
}

func TestResolveArtifactPathMissing(t *testing.T) {
	dir := t.TempDir()
	manifest := Manifest{Distribution: Distribution{Type: "exec"}}

	_, err := ResolveArtifactPath(dir, manifest, ArtifactOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrArtifactsMissing) {
		t.Fatalf("expected ErrArtifactsMissing, got %v", err)
	}
}
