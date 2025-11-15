package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolver_Resolve_AbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "myskill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))

	manifestPath := filepath.Join(skillDir, "skill.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("name: myskill\n"), 0644))

	resolver := NewResolver()

	handle, err := resolver.Resolve(skillDir)

	require.NoError(t, err)
	assert.Equal(t, "myskill", handle.Name)
	assert.Equal(t, manifestPath, handle.ManifestPath)
	assert.Equal(t, "path", handle.Source)
}

func TestResolver_Resolve_AbsolutePathToManifest(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "myskill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))

	manifestPath := filepath.Join(skillDir, "skill.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("name: myskill\n"), 0644))

	resolver := NewResolver()

	// Pass the manifest path directly, not just the directory
	handle, err := resolver.Resolve(manifestPath)

	require.NoError(t, err)
	assert.Equal(t, "myskill", handle.Name)
	assert.Equal(t, manifestPath, handle.ManifestPath)
	assert.Equal(t, "path", handle.Source)
}

func TestResolver_Resolve_RelativePath(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "myskill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))

	manifestPath := filepath.Join(skillDir, "skill.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("name: myskill\n"), 0644))

	// Change to tmpDir so relative path works
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(oldWd)
	require.NoError(t, os.Chdir(tmpDir))

	resolver := NewResolver()

	handle, err := resolver.Resolve("./myskill")

	require.NoError(t, err)
	assert.Equal(t, "myskill", handle.Name)
	assert.Contains(t, handle.ManifestPath, "myskill")
	assert.Equal(t, "path", handle.Source)
}

func TestResolver_Resolve_FromSearchPaths(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "echo")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: echo\n"), 0644))

	resolver := NewResolver(WithSearchPaths(tmpDir))

	handle, err := resolver.Resolve("echo")

	require.NoError(t, err)
	assert.Equal(t, "echo", handle.Name)
	assert.Equal(t, tmpDir, handle.Source)
}

func TestResolver_Resolve_NormalizedName(t *testing.T) {
	tmpDir := t.TempDir()
	// Create skill directory with normalized name
	skillDir := filepath.Join(tmpDir, "text_grep")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: text/grep\n"), 0644))

	resolver := NewResolver(WithSearchPaths(tmpDir))

	// Try to resolve using the original name with slashes
	handle, err := resolver.Resolve("text/grep")

	require.NoError(t, err)
	assert.Equal(t, "text/grep", handle.Name)
	assert.Equal(t, tmpDir, handle.Source)
}

func TestResolver_Resolve_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewResolver(WithSearchPaths(tmpDir))

	_, err := resolver.Resolve("nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill not found")
}

func TestResolver_Resolve_ManifestNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "broken")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	// Don't create skill.yaml

	resolver := NewResolver()

	_, err := resolver.Resolve(skillDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill manifest not found")
}

func TestResolver_List(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple skills
	createSkill(t, tmpDir, "skill1")
	createSkill(t, tmpDir, "skill2")
	createSkill(t, tmpDir, "skill3")

	resolver := NewResolver(WithSearchPaths(tmpDir))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Len(t, handles, 3)

	names := []string{handles[0].Name, handles[1].Name, handles[2].Name}
	assert.Contains(t, names, "skill1")
	assert.Contains(t, names, "skill2")
	assert.Contains(t, names, "skill3")
}

func TestResolver_List_HandlesDuplicates(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Same skill in both directories - should return only first
	createSkill(t, dir1, "echo")
	createSkill(t, dir2, "echo")

	resolver := NewResolver(WithSearchPaths(dir1, dir2))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Len(t, handles, 1)
	assert.Equal(t, "echo", handles[0].Name)
	assert.Equal(t, dir1, handles[0].Source) // First path wins
}

func TestResolver_List_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewResolver(WithSearchPaths(tmpDir))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Empty(t, handles)
}

func TestResolver_List_NonexistentPath(t *testing.T) {
	resolver := NewResolver(WithSearchPaths("/nonexistent/path"))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Empty(t, handles)
}

func TestResolver_List_IgnoresFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a skill
	createSkill(t, tmpDir, "valid_skill")

	// Create a file (not a directory) - should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "not_a_skill.txt"), []byte("test"), 0644))

	resolver := NewResolver(WithSearchPaths(tmpDir))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Len(t, handles, 1)
	assert.Equal(t, "valid_skill", handles[0].Name)
}

func TestResolver_List_IgnoresDirectoriesWithoutManifest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a skill with manifest
	createSkill(t, tmpDir, "valid_skill")

	// Create a directory without skill.yaml
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "invalid_skill"), 0755))

	resolver := NewResolver(WithSearchPaths(tmpDir))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Len(t, handles, 1)
	assert.Equal(t, "valid_skill", handles[0].Name)
}

func TestResolver_WithAdditionalPaths(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	createSkill(t, dir1, "skill1")
	createSkill(t, dir2, "skill2")

	resolver := NewResolver(WithSearchPaths(dir1), WithAdditionalPaths(dir2))

	handles, err := resolver.List()

	require.NoError(t, err)
	assert.Len(t, handles, 2)

	names := []string{handles[0].Name, handles[1].Name}
	assert.Contains(t, names, "skill1")
	assert.Contains(t, names, "skill2")
}

func TestResolver_SearchPaths(t *testing.T) {
	paths := []string{"/path/1", "/path/2", "/path/3"}
	resolver := NewResolver(WithSearchPaths(paths...))

	searchPaths := resolver.SearchPaths()

	assert.Equal(t, paths, searchPaths)
}

func TestResolver_SearchPaths_ReturnsCopy(t *testing.T) {
	paths := []string{"/path/1", "/path/2"}
	resolver := NewResolver(WithSearchPaths(paths...))

	searchPaths := resolver.SearchPaths()
	searchPaths[0] = "/modified"

	// Verify original is not modified
	assert.Equal(t, "/path/1", resolver.SearchPaths()[0])
}

func TestNormalizeSkillName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"text/grep", "text_grep"},
		{"my-skill", "my_skill"},
		{"simple", "simple"},
		{"complex/name-with-both", "complex_name_with_both"},
		{"already_normalized", "already_normalized"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeSkillName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultSearchPaths(t *testing.T) {
	paths := defaultSearchPaths()

	// Should return at least some paths
	assert.NotEmpty(t, paths)

	// Should include development paths (dist/skills and skills)
	hasDistSkills := false
	hasSkills := false
	for _, p := range paths {
		if filepath.Base(p) == "skills" && filepath.Base(filepath.Dir(p)) == "dist" {
			hasDistSkills = true
		}
		if filepath.Base(p) == "skills" {
			hasSkills = true
		}
	}
	assert.True(t, hasDistSkills || hasSkills, "should include development skill paths")
}

func TestResolver_isPath(t *testing.T) {
	resolver := NewResolver()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"absolute unix path", "/usr/local/skills/echo", true},
		{"absolute windows path", "C:\\skills\\echo", true},
		{"relative path with slash", "./skills/echo", true},
		{"relative path with backslash", ".\\skills\\echo", true},
		{"simple name", "echo", false},
		{"name with underscore", "text_grep", false},
		{"namespaced skill name", "text/grep", false},
		{"namespaced skill with dash", "my-ns/my-skill", false},
		{"path with multiple slashes", "path/to/skill", true},
		{"path with yaml extension", "path/skill.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.isPath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function to create a test skill
func createSkill(t *testing.T, base, name string) {
	t.Helper()
	skillDir := filepath.Join(base, name)
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "skill.yaml"),
		[]byte(fmt.Sprintf("name: %s\n", name)),
		0644,
	))
}
