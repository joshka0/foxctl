package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
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

func TestResolveArtifactPathRejectsAbsoluteExecEntry(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-bin")
	if err := os.WriteFile(outside, []byte("outside"), 0o755); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}

	manifest := Manifest{
		Distribution: Distribution{
			Type: "exec",
			Exec: &ExecDistribution{Entry: outside},
		},
	}

	_, err := ResolveArtifactPath(dir, manifest, ArtifactOptions{})
	if !errors.Is(err, ErrArtifactsMissing) {
		t.Fatalf("ResolveArtifactPath absolute entry error = %v, want ErrArtifactsMissing", err)
	}
}

func TestResolveArtifactPathRejectsEntryTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skill")
	entryRoot := filepath.Join(tmpDir, "root")
	outside := filepath.Join(tmpDir, "outside", "tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.MkdirAll(entryRoot, 0o755); err != nil {
		t.Fatalf("mkdir entry root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatalf("mkdir outside dir: %v", err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o755); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}

	manifest := Manifest{
		Distribution: Distribution{
			Type: "exec",
			Exec: &ExecDistribution{Entry: filepath.Join("..", "outside", "tool")},
		},
	}

	for _, opts := range []ArtifactOptions{{}, {EntryRoot: entryRoot}} {
		if _, err := ResolveArtifactPath(skillDir, manifest, opts); !errors.Is(err, ErrArtifactsMissing) {
			t.Fatalf("ResolveArtifactPath(%+v) error = %v, want ErrArtifactsMissing", opts, err)
		}
	}
}

func TestResolveArtifactPathRejectsSymlinkArtifactEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-bin")
	if err := os.WriteFile(outside, []byte("outside"), 0o755); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "bin")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	manifest := Manifest{Distribution: Distribution{Type: "exec"}}
	_, err := ResolveArtifactPath(dir, manifest, ArtifactOptions{})
	if !errors.Is(err, ErrArtifactsMissing) {
		t.Fatalf("ResolveArtifactPath symlink escape error = %v, want ErrArtifactsMissing", err)
	}
}

func TestResolveArtifactPathPropertyRejectsEntryTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skill")
	entryRoot := filepath.Join(tmpDir, "root")
	outsideDir := filepath.Join(tmpDir, "outside")
	for _, dir := range []string{skillDir, entryRoot, outsideDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(raw uint8) bool {
		leaf := fmt.Sprintf("tool%d", raw%64)
		if err := os.WriteFile(filepath.Join(outsideDir, leaf), []byte("outside"), 0o755); err != nil {
			return false
		}

		for _, entry := range []string{
			filepath.Join("..", "outside", leaf),
			filepath.Join("nested", "..", "..", "outside", leaf),
		} {
			manifest := Manifest{
				Distribution: Distribution{
					Type: "exec",
					Exec: &ExecDistribution{Entry: entry},
				},
			}
			for _, opts := range []ArtifactOptions{{}, {EntryRoot: entryRoot}} {
				if _, err := ResolveArtifactPath(skillDir, manifest, opts); !errors.Is(err, ErrArtifactsMissing) {
					return false
				}
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("entry traversal property failed: %v", err)
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
