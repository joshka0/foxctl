package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
)

func TestDefaultResolver_FindsArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test", "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Create manifest
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/skill
  version: "1.0.0"
distribution:
  type: exec
  exec:
    entry: bin
io:
  format: json
signature:
  command: test/skill
capabilities:
  network: deny
  filesystem: []
memory:
  recommend: false
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Create bin artifact
	if err := os.WriteFile(filepath.Join(skillDir, "bin"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to write bin: %v", err)
	}

	resolver := NewDefaultResolver(tmpDir)
	m, path, err := resolver.Resolve("test/skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Metadata.Name != "test/skill" {
		t.Errorf("expected name test/skill, got %s", m.Metadata.Name)
	}
	if filepath.Base(path) != "bin" {
		t.Errorf("expected bin, got %s", path)
	}
}

func TestDefaultResolver_PrefersCGO(t *testing.T) {
	if !buildinfo.IsCGO() {
		t.Skip("skipping CGO preference test in non-CGO build")
	}
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test", "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Create manifest
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/skill
  version: "1.0.0"
distribution:
  type: exec
  exec:
    entry: bin
io:
  format: json
signature:
  command: test/skill
capabilities:
  network: deny
  filesystem: []
memory:
  recommend: false
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Create both bin and bin-cgo
	if err := os.WriteFile(filepath.Join(skillDir, "bin"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to write bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "bin-cgo"), []byte("cgo binary"), 0o755); err != nil {
		t.Fatalf("failed to write bin-cgo: %v", err)
	}

	resolver := NewDefaultResolver(tmpDir)
	_, path, err := resolver.Resolve("test/skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should prefer bin-cgo
	if filepath.Base(path) != "bin-cgo" {
		t.Errorf("expected bin-cgo, got %s", path)
	}
}

func TestDefaultResolver_MissingManifest(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test", "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Create bin but no manifest
	if err := os.WriteFile(filepath.Join(skillDir, "bin"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to write bin: %v", err)
	}

	resolver := NewDefaultResolver(tmpDir)
	_, _, err := resolver.Resolve("test/skill")
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestDefaultResolver_MissingArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test", "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Create manifest but no artifact
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/skill
  version: "1.0.0"
distribution:
  type: exec
  exec:
    entry: bin
io:
  format: json
signature:
  command: test/skill
capabilities:
  network: deny
  filesystem: []
memory:
  recommend: false
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	resolver := NewDefaultResolver(tmpDir)
	_, _, err := resolver.Resolve("test/skill")
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
}

func TestDefaultResolver_RejectsTraversalSkillName(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills")
	outsideSkillDir := filepath.Join(tmpDir, "outside", "escape")
	writeResolverSkill(t, outsideSkillDir, "outside/escape")

	resolver := NewDefaultResolver(skillsDir)
	for _, name := range []string{"../outside/escape", "safe/../../outside/escape"} {
		t.Run(name, func(t *testing.T) {
			if _, path, err := resolver.Resolve(name); err == nil {
				t.Fatalf("Resolve(%q) succeeded outside skills dir with path %q", name, path)
			}
		})
	}
}

func TestDefaultResolver_RejectsSymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills")
	outsideSkillDir := filepath.Join(tmpDir, "outside", "escape")
	writeResolverSkill(t, outsideSkillDir, "outside/escape")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.Symlink(outsideSkillDir, filepath.Join(skillsDir, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	resolver := NewDefaultResolver(skillsDir)
	if _, path, err := resolver.Resolve("escape"); err == nil {
		t.Fatalf("Resolve followed symlink outside skills dir with path %q", path)
	}
}

func TestDefaultResolver_AllowsDotDotPrefixInsideRoot(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills")
	skillDir := filepath.Join(skillsDir, "ns", "..cache")
	writeResolverSkill(t, skillDir, "ns/..cache")

	resolver := NewDefaultResolver(skillsDir)
	manifest, path, err := resolver.Resolve("ns/..cache")
	if err != nil {
		t.Fatalf("Resolve rejected non-traversal dot-dot prefix: %v", err)
	}
	if manifest.Metadata.Name != "ns/..cache" {
		t.Fatalf("manifest name=%q want ns/..cache", manifest.Metadata.Name)
	}
	if filepath.Dir(path) != skillDir {
		t.Fatalf("artifact dir=%q want %q", filepath.Dir(path), skillDir)
	}
}

func TestDefaultResolverPropertyRejectsTraversalSegments(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills")
	outsideDir := filepath.Join(tmpDir, "outside")
	resolver := NewDefaultResolver(skillsDir)

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(raw uint8) bool {
		leaf := fmt.Sprintf("skill%d", raw%64)
		writeResolverSkill(t, filepath.Join(outsideDir, leaf), "outside/"+leaf)

		for _, name := range []string{
			"../outside/" + leaf,
			"safe/../../outside/" + leaf,
			"./" + leaf,
			leaf + "/..",
		} {
			if _, _, err := resolver.Resolve(name); err == nil {
				return false
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("traversal segment property failed: %v", err)
	}
}

func TestChainResolver(t *testing.T) {
	// First resolver fails
	failResolver := ResolverFunc(func(name string) (skill.Manifest, string, error) {
		return skill.Manifest{}, "", os.ErrNotExist
	})

	// Second resolver succeeds
	successResolver := ResolverFunc(func(name string) (skill.Manifest, string, error) {
		return skill.Manifest{
			Metadata:     skill.Metadata{Name: name, Version: "1.0.0"},
			Distribution: skill.Distribution{Type: "exec"},
		}, "/found/bin", nil
	})

	chain := NewChainResolver(failResolver, successResolver)
	m, path, err := chain.Resolve("test/skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Metadata.Name != "test/skill" {
		t.Errorf("expected name test/skill, got %s", m.Metadata.Name)
	}
	if path != "/found/bin" {
		t.Errorf("expected /found/bin, got %s", path)
	}
}

func TestChainResolver_AllFail(t *testing.T) {
	resolver1 := ResolverFunc(func(name string) (skill.Manifest, string, error) {
		return skill.Manifest{}, "", os.ErrNotExist
	})
	resolver2 := ResolverFunc(func(name string) (skill.Manifest, string, error) {
		return skill.Manifest{}, "", os.ErrPermission
	})

	chain := NewChainResolver(resolver1, resolver2)
	_, _, err := chain.Resolve("test/skill")
	if err == nil {
		t.Fatal("expected error when all resolvers fail")
	}
}

func TestChainResolver_Empty(t *testing.T) {
	chain := NewChainResolver()
	_, _, err := chain.Resolve("test/skill")
	if err == nil {
		t.Fatal("expected error for empty chain")
	}
}

func TestResolverFunc(t *testing.T) {
	called := false
	resolver := ResolverFunc(func(name string) (skill.Manifest, string, error) {
		called = true
		return skill.Manifest{
			Metadata: skill.Metadata{Name: name},
		}, "/path", nil
	})

	m, path, err := resolver.Resolve("test/skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected function to be called")
	}
	if m.Metadata.Name != "test/skill" {
		t.Errorf("expected test/skill, got %s", m.Metadata.Name)
	}
	if path != "/path" {
		t.Errorf("expected /path, got %s", path)
	}
}

func writeResolverSkill(t *testing.T, skillDir, name string) {
	t.Helper()
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	manifest := fmt.Sprintf(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: %s
  version: "1.0.0"
distribution:
  type: exec
  exec:
    entry: bin
io:
  format: json
signature:
  command: %s
capabilities:
  network: deny
  filesystem: []
memory:
  recommend: false
`, name, name)
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "bin"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to write bin: %v", err)
	}
}
