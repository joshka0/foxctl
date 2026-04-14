package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestSkillResolver_Resolve_NotFound(t *testing.T) {
	cfg := config.Config{
		Paths: config.Paths{
			Skills: "/nonexistent/path",
		},
	}

	resolver := NewSkillResolver(cfg)
	_, err := resolver.Resolve("nonexistent_skill")

	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestSkillResolver_Resolve_EmptyName(t *testing.T) {
	cfg := config.Config{}
	resolver := NewSkillResolver(cfg)

	_, err := resolver.Resolve("")

	if err == nil {
		t.Fatal("expected error for empty skill name")
	}
}

func TestSkillResolver_SearchPaths(t *testing.T) {
	// Create temp directory with mock skill
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test", "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write manifest (must be valid per skill.LoadManifest validation)
	manifest := `apiVersion: foxctl.dev/v1
kind: Skill
metadata:
  name: test/skill
  version: 1.0.0
distribution:
  type: exec
  exec:
    entry: bin
signature:
  command: test/skill
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Write fake binary
	if err := os.WriteFile(filepath.Join(skillDir, "bin"), []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	cfg := config.Config{
		Paths: config.Paths{
			Skills: tmpDir,
		},
	}

	resolver := NewSkillResolver(cfg)
	handle, err := resolver.Resolve("test/skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle.Manifest.Metadata.Name != "test/skill" {
		t.Errorf("expected manifest name 'test/skill', got %q", handle.Manifest.Metadata.Name)
	}
	if handle.ArtifactPath == "" {
		t.Error("expected artifact path to be set")
	}
}

func TestNormalizeSearchPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "removes duplicates",
			input:    []string{"/a", "/b", "/a"},
			expected: []string{"/a", "/b"},
		},
		{
			name:     "cleans paths",
			input:    []string{"/a/./b", "/a/../c"},
			expected: []string{"/a/b", "/c"},
		},
		{
			name:     "removes empty",
			input:    []string{"/a", "", "/b"},
			expected: []string{"/a", "/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := skill.NormalizeSearchPaths(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d paths, got %d", len(tt.expected), len(result))
				return
			}
			for i, p := range result {
				if p != tt.expected[i] {
					t.Errorf("path %d: expected %q, got %q", i, tt.expected[i], p)
				}
			}
		})
	}
}

func TestNormalizeSkillCandidate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"test/skill", "test_skill"},
		{"test-skill", "test_skill"},
		{"test/my-skill", "test_my_skill"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeSkillCandidate(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLoadSkillDir_MissingManifest(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := loadSkillDir(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestLoadSkillDir_MissingArtifact(t *testing.T) {
	tmpDir := t.TempDir()

	// Write manifest only (no binary)
	manifest := `apiVersion: foxctl.dev/v1
kind: Skill
metadata:
  name: test/skill
  version: 1.0.0
distribution:
  type: exec
  exec:
    entry: bin
signature:
  command: test/skill
`
	if err := os.WriteFile(filepath.Join(tmpDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := loadSkillDir(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
}
