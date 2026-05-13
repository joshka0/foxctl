package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestCreateSkillResolver_EnvPrecedence(t *testing.T) {
	t.Setenv("FOXCTL_HOME", t.TempDir())

	cwd := t.TempDir()
	// Resolve symlinks to match os.Getwd() behavior (macOS /var -> /private/var)
	realCwd, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	env1 := filepath.Join(realCwd, "env1")
	env2 := filepath.Join(realCwd, "env2")
	cfgSkills := filepath.Join(realCwd, "installed")
	cfg := config.Config{
		Paths: config.Paths{
			Skills: cfgSkills,
		},
	}

	t.Setenv("FOXCTL_SKILLS_PATH", strings.Join([]string{env1, env2}, string(os.PathListSeparator)))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origWD))
	})

	resolver := createSkillResolver(cfg)
	paths := resolver.SearchPaths()

	expected := append([]string{}, skill.EnvSearchPaths()...)
	expected = append(expected, cfgSkills)
	expected = append(expected, skill.UserSearchPaths()...)
	expected = append(expected, skill.BuiltinSearchPaths()...)
	expected = append(expected, skill.DevSearchPaths()...)
	expected = skill.NormalizeSearchPaths(expected)
	require.Equal(t, expected, paths)
}

func TestCreateSkillResolver_PrefersDevSkillsInFoxctlSourceCheckout(t *testing.T) {
	t.Setenv("FOXCTL_HOME", t.TempDir())
	t.Setenv("FOXCTL_SKILLS_PATH", "")

	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	writeFile(t, filepath.Join(realRoot, "go.mod"), "module github.com/joshka0/foxctl\n")
	writeFile(t, filepath.Join(realRoot, "skills", "code_refactor_scout", "skill.yaml"), "apiVersion: foxctl/v1\n")
	cfgSkills := filepath.Join(realRoot, "installed")
	cfg := config.Config{
		Paths: config.Paths{
			Skills: cfgSkills,
		},
	}

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(realRoot))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origWD))
	})

	paths := createSkillResolver(cfg).SearchPaths()
	devPaths := skill.DevSearchPaths()
	require.NotEmpty(t, devPaths)
	require.GreaterOrEqual(t, len(paths), len(devPaths)+1)
	require.Equal(t, devPaths, paths[:len(devPaths)])
	require.Equal(t, cfgSkills, paths[len(devPaths)])
}

func TestLoadSkillDirUsesFoxctlSourceRootForExecEntry(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/joshka0/foxctl\n")
	skillDir := filepath.Join(root, "skills", "code_refactor_scout")
	writeFile(t, filepath.Join(skillDir, "skill.yaml"), `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: code/refactor_scout
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/code_refactor_scout/code_refactor_scout
signature:
  command: code/refactor_scout
`)
	binaryPath := filepath.Join(skillDir, "code_refactor_scout")
	writeFile(t, binaryPath, "#!/bin/sh\n")

	handle, err := loadSkillDir(skillDir)
	require.NoError(t, err)
	require.Equal(t, binaryPath, handle.ArtifactPath)
}
